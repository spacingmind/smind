package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

func drainEvents(events <-chan Event) []Event {
	var got []Event
	for e := range events {
		got = append(got, e)
	}
	return got
}

func glmRunner(wm *workspace.Manager) *Runner {
	r := New(wm)
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New([]string{fakeACPAgentPath}, opts...)
	}
	return r
}

func claudeNativeRunner(t *testing.T, wm *workspace.Manager) *Runner {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	r := New(wm)
	r.newClaudeClient = func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error) {
		opts = append(opts, claudecode.WithCLIPath(self), claudecode.WithPermissionMode("bypassPermissions"))
		return claudecode.New(worktreePath, opts...)
	}
	return r
}

func TestRunner_RunPrompt_GLM(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := glmRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Type != EventTypeText || got[0].Text != "Hello, " {
		t.Fatalf("event[0] = %+v, want text %q", got[0], "Hello, ")
	}
	if _, ok := got[0].Raw.(acp.SessionUpdate); !ok {
		t.Fatalf("event[0].Raw = %#v, want acp.SessionUpdate", got[0].Raw)
	}
	if got[1].Type != EventTypeText || got[1].Text != "world!" {
		t.Fatalf("event[1] = %+v, want text %q", got[1], "world!")
	}
	if got[2].Type != EventTypeDone || got[2].StopReason != "end_turn" {
		t.Fatalf("event[2] = %+v, want EventTypeDone/end_turn", got[2])
	}
}

func TestRunner_RunPrompt_ClaudeNative(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Type != EventTypeText || got[0].Text != "hello " {
		t.Fatalf("event[0] = %+v, want text %q", got[0], "hello ")
	}
	if got[1].Type != EventTypeText || got[1].Text != "from claude" {
		t.Fatalf("event[1] = %+v, want text %q", got[1], "from claude")
	}
	if got[2].Type != EventTypeDone || got[2].StopReason != "end_turn" {
		t.Fatalf("event[2] = %+v, want EventTypeDone/end_turn", got[2])
	}
	if _, ok := got[2].Raw.(claudecode.ResultMessage); !ok {
		t.Fatalf("event[2].Raw = %#v, want claudecode.ResultMessage", got[2].Raw)
	}
}

// TestRunner_RunPrompt_NoWorktree covers a task that's never had CreateTask
// materialize a worktree for it (or one that's been archived, which leaves
// WorktreePath pointing at a now-removed directory -- either way RunPrompt
// must fail on the nil case rather than trying to spawn an agent rooted at
// nothing). workspace.Manager.CreateTask always creates a real worktree, so
// producing this state means inserting the row directly via the store, the
// same way internal/workspace's own tests do for this case.
func TestRunner_RunPrompt_NoWorktree(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "smind.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	wm := workspace.New(s)

	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	task, err := s.CreateTask(store.Task{
		WorkspaceID: ws.ID,
		Title:       "No worktree",
		Status:      "created",
	})
	if err != nil {
		t.Fatalf("store.CreateTask() error = %v", err)
	}

	r := New(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", events)
	}()

	got := drainEvents(events)
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
	if err := <-errCh; err == nil {
		t.Fatal("RunPrompt() error = nil, want error for task with no worktree")
	}
}

func TestRunner_RunPrompt_UnknownProvider(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := New(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, Provider("bogus"), "hi", events)
	}()

	got := drainEvents(events)
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
	if err := <-errCh; err == nil {
		t.Fatal("RunPrompt() error = nil, want error for unknown provider")
	}
}

// TestRunner_RunPrompt_ContextCancellationStopsSubprocess proves cancelling
// the context passed to RunPrompt both aborts the in-flight turn and kills
// the agent subprocess, rather than leaving it running in the background.
// The fake agent's "hang" scenario streams one chunk and then blocks
// forever (would run for an hour without being force-killed); observing
// events close and RunPrompt return promptly after cancel is only possible
// if Close() actually force-killed it.
func TestRunner_RunPrompt_ContextCancellationStopsSubprocess(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "hang")
	r := glmRunner(wm)

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hi", events)
	}()

	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("events closed before the first chunk arrived")
		}
		if e.Type != EventTypeText || e.Text != "before hang" {
			t.Fatalf("first event = %+v, want text %q", e, "before hang")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the fake agent's first chunk")
	}

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("received an unexpected second event after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events to close after cancellation")
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("RunPrompt() error = nil, want context cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for RunPrompt() to return after cancellation")
	}
}

// TestRunner_RunPrompt_DoneEventDoesNotBlockAfterCallerStopsReading proves
// RunPrompt returns (and so releases its backend client via defer Close)
// even if the caller stops reading events after the turn's last text chunk
// but before the final EventTypeDone -- e.g. because the caller's own
// context was cancelled independently. Without a ctx-guarded send on that
// final event, RunPrompt would block forever on it, leaking the subprocess.
func TestRunner_RunPrompt_DoneEventDoesNotBlockAfterCallerStopsReading(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := glmRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hello", events)
	}()

	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("events closed before the first chunk arrived")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first chunk")
	}

	// Stop reading events entirely and cancel, simulating a caller that gave
	// up right as the turn was finishing. RunPrompt must still return
	// promptly instead of blocking on the now-unread final Done event.
	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("RunPrompt() did not return within 5s after the caller stopped reading events -- likely blocked sending the final Done event")
	}
}
