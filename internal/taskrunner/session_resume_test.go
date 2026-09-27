package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/workspace"
)

// glmRunnerWithCaps is glmRunner, but spawns fakeACPAgentPath with capsMode
// as its argv[1] -- see internal/taskrunner/fakeagent's capsMode doc
// comment for what each value makes the fake agent's initialize response
// advertise.
func glmRunnerWithCaps(wm *workspace.Manager, capsMode string) *Runner {
	r := New(wm)
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New([]string{fakeACPAgentPath, capsMode}, opts...)
	}
	return r
}

// sessionInitMethod reads back the "session-init-method" marker
// internal/taskrunner/fakeagent writes on every session/new, session/load,
// or session/resume it answers -- the only way a test can tell which one
// Runner actually called, since the wire result looks the same either way.
func sessionInitMethod(t *testing.T, worktreePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, "session-init-method"))
	if err != nil {
		t.Fatalf("read session-init-method: %v", err)
	}
	return string(data)
}

// threadInitMethod is sessionInitMethod's Codex-native counterpart, for
// internal/codex/fakeagent's "thread-init-method" marker.
func threadInitMethod(t *testing.T, worktreePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, "thread-init-method"))
	if err != nil {
		t.Fatalf("read thread-init-method: %v", err)
	}
	return string(data)
}

func hasSessionNote(events []Event) bool {
	for _, e := range events {
		if e.Type == EventTypeSessionNote {
			return true
		}
	}
	return false
}

// TestRunner_RunPrompt_GLM_ResumeCapabilityMatrix proves
// newOrResumeACPSession's three-way branch (P2.3): an agent advertising
// loadSession is resumed via session/load, one advertising only
// sessionCapabilities.resume via session/resume (loadSession still
// preferred when both are offered), and one advertising neither falls back
// to a fresh session/new with a surfaced EventTypeSessionNote (P2.5) --
// never a failed prompt.
func TestRunner_RunPrompt_GLM_ResumeCapabilityMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		capsMode   string
		wantMethod string
		wantNote   bool
	}{
		{"loadSession preferred", "loadSession", "session/load", false},
		{"resume only", "resume", "session/resume", false},
		{"both offered prefers loadSession", "both", "session/load", false},
		{"neither offered falls back with a note", "none", "session/new", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "")
			r := glmRunnerWithCaps(wm, tt.capsMode)

			// First run: no stored handle yet, always session/new.
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
			}()
			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() (first run) error = %v", err)
			}
			if m := sessionInitMethod(t, *task.WorktreePath); m != "session/new" {
				t.Fatalf("first run session-init-method = %q, want %q", m, "session/new")
			}
			if hasSessionNote(got) {
				t.Fatalf("first run events unexpectedly contain an EventTypeSessionNote: %+v", got)
			}

			// Second run: a stored handle now exists (written after the
			// first run's successful prompt) -- proves the resume path.
			events2 := make(chan Event)
			errCh2 := make(chan error, 1)
			go func() {
				errCh2 <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "what did I say before?", nil, PermissionSettings{}, "", events2)
			}()
			got2 := drainEvents(events2)
			if err := <-errCh2; err != nil {
				t.Fatalf("RunPrompt() (second run) error = %v", err)
			}
			if m := sessionInitMethod(t, *task.WorktreePath); m != tt.wantMethod {
				t.Fatalf("second run session-init-method = %q, want %q", m, tt.wantMethod)
			}
			if got := hasSessionNote(got2); got != tt.wantNote {
				t.Fatalf("second run hasSessionNote = %v, want %v (events: %+v)", got, tt.wantNote, got2)
			}
		})
	}
}

// TestRunner_RunPrompt_GLM_StaleSessionFallsBackWithNote proves a stored
// session id the agent doesn't recognize (simulating a session it's
// forgotten, or one from a different agent install) falls back to a fresh
// session/new instead of failing the prompt, with a surfaced
// EventTypeSessionNote -- P2.5.
func TestRunner_RunPrompt_GLM_StaleSessionFallsBackWithNote(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := glmRunnerWithCaps(wm, "loadSession")
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderGLM, SessionID: "stale-session-id"})

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if m := sessionInitMethod(t, *task.WorktreePath); m != "session/new" {
		t.Fatalf("session-init-method = %q, want %q (fallback)", m, "session/new")
	}
	if !hasSessionNote(got) {
		t.Fatalf("events do not contain an EventTypeSessionNote: %+v", got)
	}
	if got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("last event = %+v, want EventTypeDone (prompt must still succeed)", got[len(got)-1])
	}
}

// TestRunner_RunPrompt_ClaudeNative_ResumesSessionAcrossRuns proves the
// second RunPrompt call on the same task passes the first call's
// ResultMessage.SessionID back via claudecode.WithResume (--resume=<id>),
// while the first call passes no --resume flag at all -- P2.2.
func TestRunner_RunPrompt_ClaudeNative_ResumesSessionAcrossRuns(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() (first run) error = %v", err)
	}
	firstArgs := readArgsFile(t, *task.WorktreePath)
	if strings.Contains(firstArgs, "--resume=") {
		t.Fatalf("first run args unexpectedly contain --resume: %q", firstArgs)
	}

	events2 := make(chan Event)
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "what did I say before?", nil, PermissionSettings{}, "", events2)
	}()
	drainEvents(events2)
	if err := <-errCh2; err != nil {
		t.Fatalf("RunPrompt() (second run) error = %v", err)
	}
	secondArgs := readArgsFile(t, *task.WorktreePath)
	if !strings.Contains(secondArgs, "--resume=sess-1") {
		t.Fatalf("second run args = %q, want it to contain --resume=sess-1", secondArgs)
	}
}

func readArgsFile(t *testing.T, worktreePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, "args"))
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	return string(data)
}

// TestRunner_RunPrompt_ClaudeNative_StaleSessionFallsBackWithNote proves a
// stored session id the CLI's own initialize handshake rejects (verified
// live 2026-09-27: a bogus --resume value fails claudecode.New itself,
// "cli connection closed", before any prompt is sent -- see the "reply"
// fallback in runFakeClaudeCLI's "resume-fails" scenario) still completes
// the prompt against a fresh session, with a surfaced EventTypeSessionNote
// -- P2.5.
func TestRunner_RunPrompt_ClaudeNative_StaleSessionFallsBackWithNote(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "resume-fails")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "stale-session-id"})

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if !hasSessionNote(got) {
		t.Fatalf("events do not contain an EventTypeSessionNote: %+v", got)
	}
	if got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("last event = %+v, want EventTypeDone (prompt must still succeed)", got[len(got)-1])
	}
}

// TestRunner_RunPrompt_CodexNative_ResumesSessionAcrossRuns proves the
// second RunPrompt call on the same task resumes the first call's thread
// via thread/resume, while the first call always starts fresh via
// thread/start -- P2.4.
func TestRunner_RunPrompt_CodexNative_ResumesSessionAcrossRuns(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() (first run) error = %v", err)
	}
	if m := threadInitMethod(t, *task.WorktreePath); m != "thread/start" {
		t.Fatalf("first run thread-init-method = %q, want %q", m, "thread/start")
	}

	events2 := make(chan Event)
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "what did I say before?", nil, PermissionSettings{}, "", events2)
	}()
	drainEvents(events2)
	if err := <-errCh2; err != nil {
		t.Fatalf("RunPrompt() (second run) error = %v", err)
	}
	if m := threadInitMethod(t, *task.WorktreePath); m != "thread/resume" {
		t.Fatalf("second run thread-init-method = %q, want %q", m, "thread/resume")
	}
}

// TestRunner_RunPrompt_CodexNative_StaleSessionFallsBackWithNote proves an
// unknown/stale stored thread id falls back to a fresh thread/start
// instead of failing the prompt, with a surfaced EventTypeSessionNote --
// P2.5.
func TestRunner_RunPrompt_CodexNative_StaleSessionFallsBackWithNote(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderCodexNative, SessionID: "no-such-thread"})

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if m := threadInitMethod(t, *task.WorktreePath); m != "thread/start" {
		t.Fatalf("thread-init-method = %q, want %q (fallback)", m, "thread/start")
	}
	if !hasSessionNote(got) {
		t.Fatalf("events do not contain an EventTypeSessionNote: %+v", got)
	}
	if got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("last event = %+v, want EventTypeDone (prompt must still succeed)", got[len(got)-1])
	}
}
