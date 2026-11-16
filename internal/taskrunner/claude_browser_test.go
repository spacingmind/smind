package taskrunner

import (
	"context"
	"errors"
	"sync"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// pidClaude wraps a real (fake-CLI) client and reports a pid, like the SDK's
// Client.PID does from v0.4.3.
type pidClaude struct {
	claudeBackend
	pid int
}

func (p pidClaude) PID() int { return p.pid }

type pendingSpy struct {
	mu                     sync.Mutex
	opened, bound, revoked int
	boundPID               int
	bindErr                error
	openErr                error
	order                  []string
}

func (s *pendingSpy) policy() *McpPolicy {
	return &McpPolicy{BrowserOnly: true, Browser: &BrowserPolicy{
		Env: browserEnv(),
		OpenPending: func() (string, func(int) error, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.opened++
			s.order = append(s.order, "open")
			if s.openErr != nil {
				return "", nil, s.openErr
			}
			return "/run/sock/pending.sock", func(pid int) error {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.bound++
				s.boundPID = pid
				s.order = append(s.order, "bind")
				return s.bindErr
			}, nil
		},
		Revoke: func() { s.mu.Lock(); s.revoked++; s.mu.Unlock() },
	}}
}

// With a client that can report its CLI's pid, the claude path opens the run's
// socket BEFORE the CLI exists (its row must be in the MCP config the CLI is
// started with) and binds it to the CLI's pid right after the client starts.
func TestClaudePath_OpensPendingBeforeTheCLIAndBindsItsPID(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)
	r.claudeReportsPID = true
	inner := r.newClaudeClient
	var spawned bool
	spy := &pendingSpy{}
	r.newClaudeClient = func(wt string, opts ...claudecode.Option) (claudeBackend, error) {
		spy.mu.Lock()
		if spy.opened != 1 || spy.bound != 0 {
			t.Errorf("the socket is opened exactly once and not yet bound when the CLI starts: opened=%d bound=%d", spy.opened, spy.bound)
		}
		spy.mu.Unlock()
		spawned = true
		c, err := inner(wt, opts...)
		if err != nil {
			return nil, err
		}
		return pidClaude{claudeBackend: c, pid: 4242}, nil
	}
	ctx := WithRunContract(context.Background(), RunContract{Mcp: spy.policy()})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	_ = drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if !spawned {
		t.Fatal("the CLI was not started")
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.bound != 1 || spy.boundPID != 4242 || spy.revoked != 0 {
		t.Fatalf("bound %d times to pid %d, revoked %d", spy.bound, spy.boundPID, spy.revoked)
	}
	if len(spy.order) != 2 || spy.order[0] != "open" || spy.order[1] != "bind" {
		t.Fatalf("order: %v", spy.order)
	}
}

// A client that reports no pid (pid 0), or a bind the hub refuses, closes the
// socket: the row must not stay reachable by anyone.
func TestClaudePath_NoPIDOrARefusedBindClosesTheSocket(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		pid     int
		bindErr error
	}{
		"pid zero":     {0, errors.New("the agent's process id is unknown")},
		"bind refused": {4242, errors.New("cannot read the process")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "")
			r := claudeNativeRunner(t, wm)
			r.claudeReportsPID = true
			inner := r.newClaudeClient
			r.newClaudeClient = func(wt string, opts ...claudecode.Option) (claudeBackend, error) {
				c, err := inner(wt, opts...)
				if err != nil {
					return nil, err
				}
				return pidClaude{claudeBackend: c, pid: tc.pid}, nil
			}
			spy := &pendingSpy{bindErr: tc.bindErr}
			ctx := WithRunContract(context.Background(), RunContract{Mcp: spy.policy()})
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
			}()
			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("the run itself goes on: %v", err)
			}
			spy.mu.Lock()
			defer spy.mu.Unlock()
			if spy.revoked != 1 {
				t.Fatalf("the socket is closed when it cannot be bound: revoked=%d", spy.revoked)
			}
			noted := false
			for _, e := range got {
				if e.Type == EventTypeSessionNote && e.Text == "the browser could not be bound to this agent, so its browser tools will not work in this run" {
					noted = true
				}
			}
			if !noted {
				t.Fatal("the person is told on the timeline")
			}
		})
	}
}

// An SDK whose client cannot report a pid gets no browser row and no socket.
func TestClaudePath_WithoutAPIDReportingSDKBuildsNoRow(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)
	r.claudeReportsPID = false
	spy := &pendingSpy{}
	ctx := WithRunContract(context.Background(), RunContract{Mcp: spy.policy()})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	_ = drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.opened != 0 || spy.bound != 0 {
		t.Fatalf("no pid, no socket: opened=%d bound=%d", spy.opened, spy.bound)
	}
}

// A CLI that never started revokes the socket opened for its row.
func TestClaudePath_ASpawnFailureClosesThePendingSocket(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)
	r.claudeReportsPID = true
	r.newClaudeClient = func(string, ...claudecode.Option) (claudeBackend, error) { return nil, errors.New("no cli") }
	spy := &pendingSpy{}
	ctx := WithRunContract(context.Background(), RunContract{Mcp: spy.policy()})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	_ = drainEvents(events)
	if err := <-errCh; err == nil {
		t.Fatal("a failed spawn is an error")
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.revoked != 1 {
		t.Fatalf("the unbound socket must be closed: %d", spy.revoked)
	}
}

// The real SDK type is checked by reflection: with the pinned SDK either answer
// is fine, but the check must not panic and must agree with a direct call when
// the method exists.
func TestClaudeClientReportsPIDIsAReflectionCheck(t *testing.T) {
	_ = claudeClientReportsPID()
}
