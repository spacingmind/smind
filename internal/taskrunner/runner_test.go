package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
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

// glmRunner's newACPClient override ignores the command it's given and
// always spawns fakeACPAgentPath, so it works identically for driving
// ProviderKimi turns in tests too -- runACP is provider-agnostic past the
// r.acpCommands lookup (see TestRunner_RunPrompt_Kimi), so this one helper
// covers every ACP-speaking provider rather than needing a kimi-specific
// twin.
func glmRunner(wm *workspace.Manager) *Runner {
	r := New(wm)
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New([]string{fakeACPAgentPath}, opts...)
	}
	return r
}

func codexRunner(wm *workspace.Manager) *Runner {
	r := New(wm)
	r.newCodexClient = func(_ []string, opts ...codex.Option) (codexBackend, error) {
		return codex.New([]string{fakeCodexAgentPath}, opts...)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", nil, events)
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

// TestRunner_RunPrompt_Kimi proves ProviderKimi is driven through the same
// ACP flow as ProviderGLM (runACP), not a separate/duplicated code path --
// it exercises the real r.acpCommands[ProviderKimi] lookup inside RunPrompt,
// then (via glmRunner's newACPClient override) drives the same real fake
// ACP agent GLM's own test drives.
func TestRunner_RunPrompt_Kimi(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := glmRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderKimi, "hi", nil, events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[2].Type != EventTypeDone || got[2].StopReason != "end_turn" {
		t.Fatalf("event[2] = %+v, want EventTypeDone/end_turn", got[2])
	}
}

// TestRunner_WithACPCommand_IsPerProviderIndependent proves overriding one
// ACP provider's command (via the real WithACPCommand option, not the
// test-only newACPClient seam) leaves every other ACP provider's default
// untouched -- a real behavior requirement once acpCommands became a map
// keyed by provider instead of one shared field.
func TestRunner_WithACPCommand_IsPerProviderIndependent(t *testing.T) {
	t.Parallel()
	custom := []string{"custom-glm-binary"}
	r := New(nil, WithACPCommand(ProviderGLM, custom))

	got := r.acpCommands[ProviderGLM]
	if len(got) != 1 || got[0] != custom[0] {
		t.Fatalf("acpCommands[ProviderGLM] = %v, want %v", got, custom)
	}

	wantKimi := acp.KimiCommand()
	gotKimi := r.acpCommands[ProviderKimi]
	if len(gotKimi) != len(wantKimi) {
		t.Fatalf("acpCommands[ProviderKimi] = %v, want unchanged default %v", gotKimi, wantKimi)
	}
	for i := range wantKimi {
		if gotKimi[i] != wantKimi[i] {
			t.Fatalf("acpCommands[ProviderKimi] = %v, want unchanged default %v", gotKimi, wantKimi)
		}
	}
}

// TestRunner_RunPrompt_CodexNative proves RunPrompt drives ProviderCodexNative
// through runCodexNative against a real internal/codex client wired to a
// fake app-server subprocess, mirroring TestRunner_RunPrompt_GLM's shape
// but for Codex's async turn/completed completion signal.
func TestRunner_RunPrompt_CodexNative(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderCodexNative, "hi", nil, events)
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
	if got[1].Type != EventTypeText || got[1].Text != "world!" {
		t.Fatalf("event[1] = %+v, want text %q", got[1], "world!")
	}
	if got[2].Type != EventTypeDone || got[2].StopReason != "completed" {
		t.Fatalf("event[2] = %+v, want EventTypeDone/completed", got[2])
	}
}

func TestRunner_RunPrompt_ClaudeNative(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", nil, events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", nil, events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, Provider("bogus"), "hi", nil, events)
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
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hi", nil, events)
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
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hello", nil, events)
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

// stubDecider is a PermissionDecider that records every Decide call it
// receives and always answers with a fixed optionID, for tests that only
// need to prove RunPrompt actually wires a per-call decider through to the
// provider's own permission callback (and translates its choice back
// correctly) -- not exercise any real blocking/human-in-the-loop behavior,
// which is internal/runs.Registry's job (see internal/runs/runs_test.go).
type stubDecider struct {
	optionID string

	mu      sync.Mutex
	calls   int
	summary string
	options []PermissionOption
}

func (d *stubDecider) Decide(_ context.Context, summary string, options []PermissionOption) (string, error) {
	d.mu.Lock()
	d.calls++
	d.summary = summary
	d.options = options
	d.mu.Unlock()
	return d.optionID, nil
}

func (d *stubDecider) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// TestRunner_RunPrompt_PermissionRequest_GLM proves RunPrompt wires a
// per-call PermissionDecider through to a real ACP session/request_permission
// round trip: the decider sees the fake agent's offered options (translated
// from acp.RequestPermissionParams into taskrunner.PermissionOption), and
// the option it picks is translated back into the real optionId the agent
// receives -- proven observably by the fake agent's own scripted reply
// (see fakeagent's "permission" scenario), which echoes back whichever
// optionId it was told was chosen.
func TestRunner_RunPrompt_PermissionRequest_GLM(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := glmRunner(wm)
	decider := &stubDecider{optionID: "allow-1"}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", decider, events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if decider.callCount() != 1 {
		t.Fatalf("decider.calls = %d, want 1", decider.callCount())
	}
	if len(decider.options) != 2 {
		t.Fatalf("decider saw %d options, want 2: %+v", len(decider.options), decider.options)
	}
	wantOpts := []PermissionOption{
		{ID: "allow-1", Label: "Allow", Kind: "allow_once"},
		{ID: "deny-1", Label: "Deny", Kind: "reject_once"},
	}
	for i, want := range wantOpts {
		if decider.options[i] != want {
			t.Fatalf("decider.options[%d] = %+v, want %+v", i, decider.options[i], want)
		}
	}
	if decider.summary != "Run a risky command" {
		t.Fatalf("decider.summary = %q, want %q", decider.summary, "Run a risky command")
	}

	var texts []string
	for _, e := range got {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "chose:allow-1" {
		t.Fatalf("got texts %v, want [%q]", texts, "chose:allow-1")
	}
}

// TestRunner_RunPrompt_PermissionRequest_ClaudeNative proves the same
// end-to-end wiring as the GLM test above, but for Claude Code native's
// genuinely different can_use_tool control-request shape: the decider is
// offered the synthesized allow/deny PermissionOption pair (there's no
// options list on the wire, just a tool name/input -- see
// claudeDeciderAdapter), and its choice is translated back into the real
// (allow bool, updatedInput, denyMessage) tuple the CLI's control_response
// expects, observably reflected in the fake CLI's own scripted reply.
func TestRunner_RunPrompt_PermissionRequest_ClaudeNative(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := claudeNativeRunner(t, wm)
	decider := &stubDecider{optionID: claudeOptionAllow}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if decider.callCount() != 1 {
		t.Fatalf("decider.calls = %d, want 1", decider.callCount())
	}
	wantOpts := []PermissionOption{
		{ID: "allow", Label: "Allow", Kind: "allow_once"},
		{ID: "deny", Label: "Deny", Kind: "reject_once"},
	}
	if len(decider.options) != 2 || decider.options[0] != wantOpts[0] || decider.options[1] != wantOpts[1] {
		t.Fatalf("decider.options = %+v, want %+v", decider.options, wantOpts)
	}
	if decider.summary != "run Bash" {
		t.Fatalf("decider.summary = %q, want %q", decider.summary, "run Bash")
	}

	var texts []string
	for _, e := range got {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "chose:allow" {
		t.Fatalf("got texts %v, want [%q]", texts, "chose:allow")
	}
}

// TestRunner_RunPrompt_PermissionRequest_ClaudeNative_Deny proves a "deny"
// decision reaches the CLI as behavior "deny", not just that "allow" round
// trips -- the two-way translation (bool in, bool out) is exactly the kind
// of thing that silently inverts if either side of claudeDeciderAdapter's
// mapping is ever wrong.
func TestRunner_RunPrompt_PermissionRequest_ClaudeNative_Deny(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := claudeNativeRunner(t, wm)
	decider := &stubDecider{optionID: claudeOptionDeny}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	var texts []string
	for _, e := range got {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "chose:deny" {
		t.Fatalf("got texts %v, want [%q]", texts, "chose:deny")
	}
}
