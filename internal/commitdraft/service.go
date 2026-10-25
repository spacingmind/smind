package commitdraft

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/store"
)

// Typed failures; the wsapi layer maps each to a wire code.
var (
	ErrRateLimited     = errors.New("commitdraft: too many drafts, try again shortly")
	ErrBusy            = errors.New("commitdraft: the draft runner is busy")
	ErrNoDraftProvider = errors.New("commitdraft: no usable claude-native provider")
	ErrDraftFailed     = errors.New("commitdraft: drafting failed")
)

// Defaults of Deps.
const (
	DefaultFloor     = 3 * time.Second
	DefaultHourlyCap = 30
	DefaultTimeout   = 60 * time.Second
)

// Executor is the isolated, no-tools model runner (internal/learning/executor).
type Executor interface {
	Run(ctx context.Context, r executor.Request) (executor.Result, error)
}

// Deps wires a Service.
type Deps struct {
	Exec Executor
	// Worktree resolves a task to its worktree and workspace.
	Worktree func(taskID int64) (path string, workspaceID int64, err error)
	// Style returns the workspace's commit-message style text ("" = none).
	Style func(workspaceID int64) string
	Now   func() time.Time // default time.Now
	// Floor is the minimum gap between two model calls for one task.
	Floor time.Duration
	// HourlyCap is the most model calls per workspace per hour.
	HourlyCap int
	Timeout   time.Duration
	Budget    int // diff byte budget
}

// Result is a drafted message.
type Result struct {
	Message   string   `json:"message"`
	Model     string   `json:"model"`
	Truncated bool     `json:"truncated"`
	Omitted   []string `json:"omitted"`
}

// Service drafts commit messages. Safe for concurrent use.
type Service struct {
	d Deps

	mu       sync.Mutex
	inflight map[int64]*call
	lastCall map[int64]time.Time   // by task
	calls    map[int64][]time.Time // by workspace, model calls in the last hour
}

type call struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	res     Result
	err     error
}

// New returns a Service, filling Deps defaults.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Floor == 0 {
		d.Floor = DefaultFloor
	}
	if d.HourlyCap == 0 {
		d.HourlyCap = DefaultHourlyCap
	}
	if d.Timeout == 0 {
		d.Timeout = DefaultTimeout
	}
	if d.Budget == 0 {
		d.Budget = DefaultBudget
	}
	if d.Style == nil {
		d.Style = func(int64) string { return "" }
	}
	return &Service{d: d, inflight: map[int64]*call{}, lastCall: map[int64]time.Time{}, calls: map[int64][]time.Time{}}
}

// Draft drafts a message for the task's staged diff. The staged diff is read
// first, so an empty index costs no model call and no rate budget. A second
// concurrent call for the same task joins the in-flight one.
func (s *Service) Draft(ctx context.Context, taskID int64) (Result, error) {
	path, wsID, err := s.d.Worktree(taskID)
	if err != nil {
		return Result{}, err
	}
	in, err := BuildInput(ctx, path, s.d.Budget)
	if err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	c := s.inflight[taskID]
	if c == nil {
		if err := s.admitLocked(taskID, wsID); err != nil {
			s.mu.Unlock()
			return Result{}, err
		}
		// The call outlives any single waiter; it is cancelled when the
		// last waiter leaves or the timeout passes.
		runCtx, cancel := context.WithTimeout(context.Background(), s.d.Timeout)
		c = &call{done: make(chan struct{}), cancel: cancel}
		s.inflight[taskID] = c
		c.waiters = 1
		go s.lead(runCtx, taskID, wsID, in, c)
	} else {
		c.waiters++
	}
	s.mu.Unlock()

	select {
	case <-c.done:
		s.leave(c, false)
		return c.res, c.err
	case <-ctx.Done():
		s.leave(c, true)
		return Result{}, ctx.Err()
	}
}

// leave removes a waiter; the call is cancelled when none are left.
func (s *Service) leave(c *call, cancelIfLast bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.waiters--
	if cancelIfLast && c.waiters == 0 {
		c.cancel()
	}
}

// admitLocked applies the per-task floor and the per-workspace hourly cap and,
// when the call is admitted, records it.
func (s *Service) admitLocked(taskID, wsID int64) error {
	now := s.d.Now()
	if last, ok := s.lastCall[taskID]; ok && now.Sub(last) < s.d.Floor {
		return ErrRateLimited
	}
	recent := s.calls[wsID][:0:0]
	for _, t := range s.calls[wsID] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= s.d.HourlyCap {
		s.calls[wsID] = recent
		return ErrRateLimited
	}
	s.lastCall[taskID] = now
	s.calls[wsID] = append(recent, now)
	return nil
}

// lead runs the model call and always releases the in-flight slot.
func (s *Service) lead(ctx context.Context, taskID, wsID int64, in Input, c *call) {
	defer func() {
		s.mu.Lock()
		delete(s.inflight, taskID)
		s.mu.Unlock()
		c.cancel()
		close(c.done)
	}()
	c.res, c.err = s.run(ctx, wsID, in)
}

func (s *Service) run(ctx context.Context, wsID int64, in Input) (Result, error) {
	res, err := s.d.Exec.Run(ctx, executor.Request{
		WorkspaceID:  wsID,
		Purpose:      store.RunPurposeCommitMsg,
		Model:        config.DefaultHaikuModel,
		SystemPrompt: SystemPrompt,
		UserPrompt:   UserPrompt(in, s.d.Style(wsID)),
		MaxTurns:     1,
		Timeout:      s.d.Timeout,
	})
	if err != nil {
		switch {
		case errors.Is(err, executor.ErrBusy):
			return Result{}, ErrBusy
		case noProvider(err):
			return Result{}, fmt.Errorf("%w: %v", ErrNoDraftProvider, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrDraftFailed, err)
	}
	msg, err := Normalize(res.FinalText)
	if err != nil {
		return Result{}, err
	}
	return Result{Message: msg, Model: config.DefaultHaikuModel, Truncated: in.Truncated, Omitted: nonNil(in.Omitted)}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// noProvider reports whether err says the claude CLI or its login is
// unusable. The registry has no typed "no account" error for claude-native
// runs (they use the CLI's own login), so this reads the failure text.
func noProvider(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, m := range []string{"not logged in", "please run /login", "invalid api key", "could not find a native claude", "executable file not found", "no account", "no claude"} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
