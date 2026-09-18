package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// denyAllDecider is a PermissionDecider that denies everything; the
// echo-args scenario never raises a can_use_tool request, so its Decide
// never actually runs -- it exists only so RunPrompt takes the
// decider-wired branch this test needs to observe.
type denyAllDecider struct{}

func (denyAllDecider) Decide(_ context.Context, _ string, _ string, _ []PermissionOption) (string, error) {
	return "", nil
}

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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderKimi, "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderCodexNative, "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", nil, "", "", events)
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

// TestRunner_RunPrompt_ClaudeNative_ToolCallEvents proves a Claude Agent
// SDK tool-use message produces a tool-call event with id/name/input, and
// its later tool-result message (success or failure) completes the same
// id in place -- the Go test scenario docs/decisions/0008-structured-run-events.md
// calls for. Also covers ThinkingBlock -> EventTypeThinking, using the
// fake CLI's "tool_call" scenario (see runFakeClaudeCLI).
func TestRunner_RunPrompt_ClaudeNative_ToolCallEvents(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "tool_call")
	r := claudeNativeRunner(t, wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", nil, "", "", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 7 {
		t.Fatalf("got %d events, want 7: %+v", len(got), got)
	}
	if got[0].Type != EventTypeThinking || got[0].Text != "let me check" {
		t.Fatalf("event[0] = %+v, want thinking %q", got[0], "let me check")
	}
	if got[1].Type != EventTypeToolCall || got[1].ToolCallID != "tool-1" || got[1].ToolName != "Bash" || got[1].ToolStatus != ToolStatusRunning {
		t.Fatalf("event[1] = %+v, want a running tool-1/Bash call", got[1])
	}
	if !strings.Contains(string(got[1].ToolInput), `"echo hi"`) {
		t.Fatalf("event[1].ToolInput = %s, want it to carry the command", got[1].ToolInput)
	}
	if got[2].Type != EventTypeToolCall || got[2].ToolCallID != "tool-2" || got[2].ToolStatus != ToolStatusRunning {
		t.Fatalf("event[2] = %+v, want a running tool-2 call", got[2])
	}
	if got[3].Type != EventTypeToolCall || got[3].ToolCallID != "tool-1" || got[3].ToolStatus != ToolStatusSuccess {
		t.Fatalf("event[3] = %+v, want tool-1 to complete as success", got[3])
	}
	if !strings.Contains(string(got[3].ToolResult), "hi") {
		t.Fatalf("event[3].ToolResult = %s, want it to carry the result", got[3].ToolResult)
	}
	if got[4].Type != EventTypeToolCall || got[4].ToolCallID != "tool-2" || got[4].ToolStatus != ToolStatusFailure {
		t.Fatalf("event[4] = %+v, want tool-2 to complete as failure", got[4])
	}
	if got[5].Type != EventTypeText || got[5].Text != "done" {
		t.Fatalf("event[5] = %+v, want text %q", got[5], "done")
	}
	if got[6].Type != EventTypeDone || got[6].StopReason != "end_turn" {
		t.Fatalf("event[6] = %+v, want EventTypeDone/end_turn", got[6])
	}
}

// TestRunner_RunPrompt_GLM_StructuredEvents proves the ACP path produces
// equivalent structured events for GLM: a thought chunk, a user-message
// chunk, and a tool call reported first as running then completed as
// success -- using the fake ACP agent's "structured" scenario. Also
// covers a "plan" update, a kind acpEvent doesn't recognize, surfacing as
// EventTypeRaw instead of being dropped
// (docs/decisions/0010-preserve-unknown-acp-event-kinds.md).
func TestRunner_RunPrompt_GLM_StructuredEvents(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "structured")
	r := glmRunner(wm)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", nil, "", "", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 7 {
		t.Fatalf("got %d events, want 7: %+v", len(got), got)
	}
	if got[0].Type != EventTypeRaw || got[0].RawKind != "plan" || len(got[0].RawPayload) == 0 {
		t.Fatalf("event[0] = %+v, want a raw %q event carrying its payload", got[0], "plan")
	}
	if got[1].Type != EventTypeThinking || got[1].Text != "thinking it over" {
		t.Fatalf("event[1] = %+v, want thinking %q", got[1], "thinking it over")
	}
	if got[2].Type != EventTypeUserMessage || got[2].Text != "a synthesized user turn" {
		t.Fatalf("event[2] = %+v, want user message %q", got[2], "a synthesized user turn")
	}
	if got[3].Type != EventTypeToolCall || got[3].ToolCallID != "tc-1" || got[3].ToolName != "execute" || got[3].ToolTitle != "Run tests" || got[3].ToolStatus != ToolStatusRunning {
		t.Fatalf("event[3] = %+v, want a running tc-1/execute call titled %q", got[3], "Run tests")
	}
	if !strings.Contains(string(got[3].ToolInput), "go test") {
		t.Fatalf("event[3].ToolInput = %s, want it to carry the command", got[3].ToolInput)
	}
	if got[4].Type != EventTypeToolCall || got[4].ToolCallID != "tc-1" || got[4].ToolStatus != ToolStatusSuccess {
		t.Fatalf("event[4] = %+v, want tc-1 to complete as success", got[4])
	}
	if len(got[4].ToolResult) == 0 {
		t.Fatalf("event[4].ToolResult is empty, want the completed call's content")
	}
	if got[5].Type != EventTypeText || got[5].Text != "done" {
		t.Fatalf("event[5] = %+v, want text %q", got[5], "done")
	}
	if got[6].Type != EventTypeDone || got[6].StopReason != "end_turn" {
		t.Fatalf("event[6] = %+v, want EventTypeDone/end_turn", got[6])
	}
}

// TestRunner_RunPrompt_ClaudeNative_AutoSafeAllowedTools proves the
// auto-safe policy reaches the CLI's own permission gate, not just
// smind's decider: RunPrompt must spawn claude with --allowedTools
// carrying exactly SafeBashRules() (see SafeBashRules for why the
// decider-side check alone can't help -- the CLI blocks Bash before
// can_use_tool ever fires). Uses the fake CLI's echo-args scenario to
// capture the actual argv.
func TestRunner_RunPrompt_ClaudeNative_AutoSafeAllowedTools(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		approvalPolicy ApprovalPolicy
		wantRules      bool
	}{
		{name: "auto-safe pre-approves the allowlist at the CLI gate", approvalPolicy: ApprovalPolicyAutoSafe, wantRules: true},
		{name: "manual spawns with no pre-approved tools", approvalPolicy: ApprovalPolicyManual, wantRules: false},
		{name: "full-access spawns with no pre-approved tools either -- bypassPermissions covers everything already", approvalPolicy: ApprovalPolicyFullAccess, wantRules: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)

			decider := denyAllDecider{}
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, tc.approvalPolicy, "", events)
			}()

			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
				t.Fatalf("expected a Done event, got %+v", got)
			}

			data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
			if err != nil {
				t.Fatalf("read args file: %v", err)
			}
			args := strings.Split(string(data), "\n")
			var allowed []string
			for i, a := range args {
				if a == "--allowedTools" && i+1 < len(args) {
					allowed = strings.Split(args[i+1], ",")
				}
			}

			if !tc.wantRules {
				if allowed != nil {
					t.Fatalf("got --allowedTools %v, want none under %q", allowed, tc.approvalPolicy)
				}
				return
			}
			want := SafeBashRules()
			if strings.Join(allowed, ",") != strings.Join(want, ",") {
				t.Fatalf("--allowedTools = %v, want %v", allowed, want)
			}
		})
	}
}

// TestRunner_RunPrompt_ClaudeNative_ThinkingLevel proves each ThinkingLevel
// value maps to the specific claude-agent-sdk-go Option (and therefore CLI
// flags -- see claudecode's own doc comments for WithAdaptiveThinking/
// WithThinkingBudget/WithDisabledThinking) runClaudeNative's doc comment
// promises, using the same "echo-args" observability the AutoSafeAllowedTools
// test above uses for --allowedTools: the fake CLI dumps its real argv to a
// file, which is the only way to observe an Option's effect since
// claudecode.Option values aren't otherwise inspectable from this package.
// ThinkingLevelUnspecified (the zero value, what an older client that never
// set the field gets) must add no thinking flags at all -- proving omitting
// the field doesn't change today's default behavior, per the Test
// Scenarios.
func TestRunner_RunPrompt_ClaudeNative_ThinkingLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		level     ThinkingLevel
		wantFlags []string
	}{
		{name: "unspecified adds no thinking flags", level: ThinkingLevelUnspecified, wantFlags: nil},
		{name: "off sends --thinking disabled", level: ThinkingLevelOff, wantFlags: []string{"--thinking", "disabled"}},
		{name: "standard sends --thinking adaptive", level: ThinkingLevelStandard, wantFlags: []string{"--thinking", "adaptive"}},
		{name: "extended sends --max-thinking-tokens", level: ThinkingLevelExtended, wantFlags: []string{"--max-thinking-tokens", "32000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)

			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", nil, "", tc.level, events)
			}()

			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
				t.Fatalf("expected a Done event, got %+v", got)
			}

			data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
			if err != nil {
				t.Fatalf("read args file: %v", err)
			}
			args := strings.Split(string(data), "\n")

			var gotFlags []string
			for i, a := range args {
				if a == "--thinking" || a == "--max-thinking-tokens" {
					if i+1 < len(args) {
						gotFlags = append(gotFlags, a, args[i+1])
					}
				}
			}
			if strings.Join(gotFlags, ",") != strings.Join(tc.wantFlags, ",") {
				t.Fatalf("thinking flags = %v, want %v", gotFlags, tc.wantFlags)
			}
		})
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, Provider("bogus"), "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hi", nil, "", "", events)
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
		errCh <- r.RunPrompt(ctx, task.ID, ProviderGLM, "hello", nil, "", "", events)
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
	command string
	options []PermissionOption
}

func (d *stubDecider) Decide(_ context.Context, summary, command string, options []PermissionOption) (string, error) {
	d.mu.Lock()
	d.calls++
	d.summary = summary
	d.command = command
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", decider, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, "", "", events)
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

// TestRunner_RunPrompt_ClaudeNative_DialogTimeoutEnv proves
// runClaudeNative's decider branch wires CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS
// onto the CLI subprocess (raising the CLI's own auto-deny deadline above
// smind's 5-minute permission window, see runClaudeNative) and that a value
// already present in the environment wins -- the deployment-wide escape
// hatch. Not parallel: it manipulates the process environment via t.Setenv.
func TestRunner_RunPrompt_ClaudeNative_DialogTimeoutEnv(t *testing.T) {
	for _, tc := range []struct {
		name    string
		preset  string
		wantEnv string
	}{
		{name: "decider-wired run raises the CLI dialog deadline", preset: "", wantEnv: claudeDialogTimeoutMS},
		{name: "preset user value wins", preset: "1200000", wantEnv: "1200000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(claudeDialogTimeoutEnv, tc.preset)
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)

			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, ApprovalPolicyManual, "", events)
			}()

			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
				t.Fatalf("expected a Done event, got %+v", got)
			}

			data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env"))
			if err != nil {
				t.Fatalf("read env file: %v", err)
			}
			if string(data) != tc.wantEnv {
				t.Fatalf("CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS = %q, want %q", data, tc.wantEnv)
			}
		})
	}
}

// TestRunner_RunPrompt_ClaudeNative_FullAccess_NeverAsksDecider proves
// ApprovalPolicyFullAccess installs no decider at all for Claude Code
// native, even when RunPrompt is handed a non-nil one: it reuses the
// "permission" fake-CLI scenario the manual-tier tests above drive through
// claudeDeciderAdapter, but wires a decider that would answer "deny" if
// consulted -- since runClaudeNative's full-access branch never adds
// claudecode.WithPermissionPolicy(claudeDeciderAdapter{...}) at all (only
// WithPermissionMode("bypassPermissions")), the SDK's own can_use_tool
// handling never reaches this decider, so a deny-leaning decider going
// unconsulted is exactly the signal that no permission-request round trip
// (taskrunner.EventTypePermissionRequest, emitted one level up by
// internal/runs' own PermissionDecider wrapper) ever happens under this
// tier -- see docs/plans/active/task-move-approval-thinking.md's Item 2
// Test Scenarios.
func TestRunner_RunPrompt_ClaudeNative_FullAccess_NeverAsksDecider(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := claudeNativeRunner(t, wm)
	decider := &stubDecider{optionID: claudeOptionDeny}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderClaudeNative, "hi", decider, ApprovalPolicyFullAccess, "", events)
	}()

	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if decider.callCount() != 0 {
		t.Fatalf("decider.calls = %d, want 0 (full-access must never consult it)", decider.callCount())
	}
}

// TestRunner_RunPrompt_GLM_FullAccess_InstallsAutoApprove proves
// ApprovalPolicyFullAccess installs acp.AutoApprovePolicy{} for ACP
// (GLM/Kimi), not merely "no decider": the fake agent's "permission"
// scenario offers an allow_once and a reject_once option and echoes back
// whichever optionId the client chose, so seeing "chose:allow-1" (the
// allow option AutoApprovePolicy always selects) rather than the
// deny-leaning stubDecider's answer proves the real auto-approve
// mechanism is actually wired in, and decider.callCount() == 0 proves the
// decider it was handed is never consulted to get there.
func TestRunner_RunPrompt_GLM_FullAccess_InstallsAutoApprove(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := glmRunner(wm)
	decider := &stubDecider{optionID: "deny-1"}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderGLM, "hi", decider, ApprovalPolicyFullAccess, "", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if decider.callCount() != 0 {
		t.Fatalf("decider.calls = %d, want 0 (full-access must never consult it)", decider.callCount())
	}

	var texts []string
	for _, e := range got {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "chose:allow-1" {
		t.Fatalf("got texts %v, want [%q] (proves acp.AutoApprovePolicy{} chose the allow option, not the stub decider's deny)", texts, "chose:allow-1")
	}
}

// TestRunner_RunPrompt_CodexNative_FullAccess_InstallsAutoApprove is the
// Codex-native twin of the GLM test above: the fake app-server's
// "permission" scenario issues a real item/commandExecution/requestApproval
// call and streams back the decision it received, so "decision:accept"
// (what codex.AutoApprovePolicy{} always answers) rather than the
// deny-leaning stubDecider's "decline" proves the real policy is installed,
// and decider.callCount() == 0 proves codexDeciderAdapter is never reached
// to get there.
func TestRunner_RunPrompt_CodexNative_FullAccess_InstallsAutoApprove(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := codexRunner(wm)
	decider := &stubDecider{optionID: codexOptionDecline}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, ProviderCodexNative, "hi", decider, ApprovalPolicyFullAccess, "", events)
	}()

	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if decider.callCount() != 0 {
		t.Fatalf("decider.calls = %d, want 0 (full-access must never consult it)", decider.callCount())
	}

	var texts []string
	for _, e := range got {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "decision:accept" {
		t.Fatalf("got texts %v, want [%q] (proves codex.AutoApprovePolicy{} accepted, not the stub decider's decline)", texts, "decision:accept")
	}
}
