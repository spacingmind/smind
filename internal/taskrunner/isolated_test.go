package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
)

// runIsolated runs RunPromptIsolated to completion and returns the events
// and the error.
func runIsolated(t *testing.T, r *Runner, task store.Task, provider Provider, decider PermissionDecider, iso IsolationOptions) ([]Event, error) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPromptIsolated(context.Background(), task.ID, task.ID, provider, "hi", decider, "", iso, events)
	}()
	got := drainEvents(events)
	return got, <-errCh
}

func readArgs(t *testing.T, task store.Task) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func assertIsolatedFlags(t *testing.T, args []string, systemPrompt string) {
	t.Helper()
	if v, ok := flagValue(args, "--tools"); !ok || v != "" {
		t.Errorf("--tools = %q (present %v), want present and empty; args %q", v, ok, args)
	}
	for _, f := range []string{"--strict-mcp-config", "--setting-sources", "--disable-slash-commands"} {
		if _, ok := flagValue(args, f); !ok {
			t.Errorf("%s missing from args %q", f, args)
		}
	}
	if v, _ := flagValue(args, "--setting-sources"); v != "" {
		t.Errorf("--setting-sources = %q, want empty", v)
	}
	if v, _ := flagValue(args, "--max-turns"); v != "1" {
		t.Errorf("--max-turns = %q, want 1", v)
	}
	if v, _ := flagValue(args, "--permission-mode"); v != ClaudeModePlan {
		t.Errorf("--permission-mode = %q, want plan", v)
	}
	if v, _ := flagValue(args, "--system-prompt"); v != systemPrompt {
		t.Errorf("--system-prompt = %q, want %q", v, systemPrompt)
	}
	for _, f := range []string{"--mcp-config", "--add-dir", "--allowedTools", "--disallowedTools", "--allow-dangerously-skip-permissions", "--resume", "--plugin-dir"} {
		if v, ok := flagValue(args, f); ok {
			t.Errorf("%s = %q present, want absent; args %q", f, v, args)
		}
	}
}

func TestReflector_HasNoTools(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	got, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "you reflect"})
	if err != nil {
		t.Fatalf("RunPromptIsolated() error = %v", err)
	}
	if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("want a final Done event, got %+v", got)
	}
	args := readArgs(t, task)
	t.Logf("emitted args: %q", args)
	assertIsolatedFlags(t, args, "you reflect")
}

func TestReflector_NoMcpNoAddDir(t *testing.T) {
	t.Parallel()
	r, st, task := newClaudeMcpEnv(t)
	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Args: `["-y"]`, Enabled: true}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	// Sanity: a normal run on this fixture does carry --mcp-config.
	if _, ok := flagValue(runClaudeMcp(t, r, task), "--mcp-config"); !ok {
		t.Fatal("fixture broken: normal run has no --mcp-config")
	}
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "p", MaxTurns: 0}); err != nil {
		t.Fatalf("RunPromptIsolated() error = %v", err)
	}
	assertIsolatedFlags(t, readArgs(t, task), "p")
}

func TestReflector_MaxTurnsOverride(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "p", MaxTurns: 3}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if v, _ := flagValue(readArgs(t, task), "--max-turns"); v != "3" {
		t.Fatalf("--max-turns = %q, want 3", v)
	}
}

func TestReflector_UnchangedRunPromptFlags(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	args := readArgs(t, task)
	for _, f := range []string{"--tools", "--strict-mcp-config", "--setting-sources", "--max-turns", "--disable-slash-commands"} {
		if v, ok := flagValue(args, f); ok {
			t.Errorf("normal RunPrompt leaked %s=%q; args %q", f, v, args)
		}
	}
	if _, ok := flagValue(args, "--allow-dangerously-skip-permissions"); !ok {
		t.Errorf("normal RunPrompt lost --allow-dangerously-skip-permissions; args %q", args)
	}
}

func TestRunPromptIsolated_RefusesAcpProviders(t *testing.T) {
	t.Parallel()
	for _, p := range []Provider{ProviderGLM, ProviderKimi, ProviderCodexNative} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)
			got, err := runIsolated(t, r, task, p, nil, IsolationOptions{SystemPrompt: "p"})
			if !errors.Is(err, ErrIsolationUnsupported) {
				t.Fatalf("error = %v, want ErrIsolationUnsupported", err)
			}
			if len(got) != 0 {
				t.Fatalf("events = %+v, want none (channel closed)", got)
			}
			if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); !os.IsNotExist(statErr) {
				t.Fatalf("a subprocess was started (args file exists, stat err %v)", statErr)
			}
		})
	}
}

func TestRunPromptIsolated_RequiresSystemPromptAndTask(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{}); err == nil {
		t.Fatal("empty SystemPrompt: want error")
	}
	if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); !os.IsNotExist(statErr) {
		t.Fatal("subprocess started despite invalid options")
	}
	task.ID = 99999
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "p"}); err == nil {
		t.Fatal("unknown task: want error")
	}
}

func TestRunPromptIsolated_DoesNotTouchSessionStore(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if h, ok := r.sessionStore.Get(task.ID); ok {
		t.Fatalf("session handle stored after isolated run: %+v", h)
	}

	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "stale-sess"})
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, nil, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("error = %v", err)
	}
	if v, ok := flagValue(readArgs(t, task), "--resume"); ok {
		t.Fatalf("--resume = %q passed to isolated run", v)
	}
	if h, _ := r.sessionStore.Get(task.ID); h.SessionID != "stale-sess" {
		t.Fatalf("stored handle overwritten: %+v", h)
	}
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if len(r.claudeClients) != 0 {
		t.Fatalf("claudeClients = %d entries, want none tracked", len(r.claudeClients))
	}
}
