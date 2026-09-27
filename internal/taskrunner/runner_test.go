package taskrunner

import (
	"context"
	"errors"
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

func (denyAllDecider) Decide(_ context.Context, _ string, _ []PermissionOption) (string, error) {
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
		opts = append(opts, claudecode.WithCLIPath(self))
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderKimi, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
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

// flagValue returns the value of --name in args (either "--name value" or
// "--name=value"), and whether the flag was present at all.
func flagValue(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// TestRunner_RunPrompt_ClaudeNative_PermissionModeFlags is S1: the run's
// permission mode goes straight to --permission-mode (acceptEdits when
// unset), --allow-dangerously-skip-permissions is always passed so a
// mid-run switch into bypass works, and no Bash(...) --allowedTools rules
// exist any more (ADR-0019).
func TestRunner_RunPrompt_ClaudeNative_PermissionModeFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode, want string
	}{
		{"", ClaudeModeAcceptEdits},
		{ClaudeModeDefault, ClaudeModeDefault},
		{ClaudeModePlan, ClaudeModePlan},
		{ClaudeModeAuto, ClaudeModeAuto},
		{ClaudeModeBypass, ClaudeModeBypass},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)

			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{Mode: tc.mode}, "", events)
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
			if v, _ := flagValue(args, "--permission-mode"); v != tc.want {
				t.Fatalf("--permission-mode = %q, want %q (args %v)", v, tc.want, args)
			}
			if _, ok := flagValue(args, "--"+claudeAllowBypassFlag); !ok {
				t.Fatalf("--%s missing from args %v", claudeAllowBypassFlag, args)
			}
			if v, ok := flagValue(args, "--allowedTools"); ok {
				t.Fatalf("--allowedTools = %q, want none (no smind allowlist)", v)
			}
		})
	}
}

// TestRunner_RunPrompt_ClaudeNative_ThinkingLevel proves each ThinkingLevel
// value maps to the specific claude-agent-sdk-go Option (and therefore CLI
// flags -- see claudecode's own doc comments for WithAdaptiveThinking/
// WithThinkingBudget/WithDisabledThinking) runClaudeNative's doc comment
// promises, using the same "echo-args" observability the PermissionModeFlags
// test above uses for --permission-mode: the fake CLI dumps its real argv to a
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
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, tc.level, events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, Provider("bogus"), "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderGLM, "hello", nil, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", decider, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", decider, PermissionSettings{}, "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", decider, PermissionSettings{}, "", events)
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
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", events)
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

// TestRunner_RunPrompt_GLM_AutoAcceptWithoutDecider proves AutoAccept
// with no decider installs acp.AutoApprovePolicy{}: the fake agent's
// "permission" scenario echoes back the allow option AutoApprovePolicy
// always picks.
func TestRunner_RunPrompt_GLM_AutoAcceptWithoutDecider(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := glmRunner(wm)
	r.acpPermissionPolicy = acp.AutoDenyPolicy{}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{AutoAccept: true}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if texts := eventTexts(got); len(texts) != 1 || texts[0] != "chose:allow-1" {
		t.Fatalf("got texts %v, want [chose:allow-1]", texts)
	}
}

func eventTexts(events []Event) []string {
	var texts []string
	for _, e := range events {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	return texts
}

// TestRunner_RunPrompt_CodexNative_ModePresets is S9: each Codex mode
// sends its own approvalPolicy/sandbox pair on thread/start, and whatever
// Codex still escalates reaches the decider (here: declined).
func TestRunner_RunPrompt_CodexNative_ModePresets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode, want string
	}{
		{"", "on-request workspace-write"},
		{CodexModeAuto, "on-request workspace-write"},
		{CodexModeFullAccess, "never danger-full-access"},
	} {
		t.Run(tc.want+"/"+tc.mode, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "permission")
			r := codexRunner(wm)
			decider := &stubDecider{optionID: codexOptionDecline}

			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", decider, PermissionSettings{Mode: tc.mode}, "", events)
			}()
			got := drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			policy, err := os.ReadFile(filepath.Join(*task.WorktreePath, "thread-policy"))
			if err != nil {
				t.Fatalf("read thread-policy: %v", err)
			}
			if string(policy) != tc.want {
				t.Fatalf("thread/start policy = %q, want %q", policy, tc.want)
			}
			if decider.callCount() != 1 {
				t.Fatalf("decider.calls = %d, want 1", decider.callCount())
			}
			if texts := eventTexts(got); len(texts) != 1 || texts[0] != "decision:decline" {
				t.Fatalf("got texts %v, want [decision:decline]", texts)
			}
		})
	}

	t.Run("unknown mode fails the run", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "reply")
		r := codexRunner(wm)
		events := make(chan Event)
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{Mode: "auto-safe"}, "", events)
		}()
		drainEvents(events)
		if err := <-errCh; err == nil || !strings.Contains(err.Error(), "auto-safe") {
			t.Fatalf("RunPrompt() error = %v, want unknown-mode error", err)
		}
	})
}

// sessionModeLog reads the fake ACP agent's "session-mode" log: one line
// per session/set_mode or mode config option it was sent.
func sessionModeLog(t *testing.T, worktreePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, "session-mode"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read session-mode: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// TestRunner_RunPrompt_ACP_AppliesPermissionMode is S7 (plus the S5
// config-option route): a non-current mode is applied before the prompt
// by whichever mechanism the agent advertised; the current mode or an
// empty one sends nothing; a mode the agent rejects, or any non-default
// mode on an agent with no modes at all, fails the run.
func TestRunner_RunPrompt_ACP_AppliesPermissionMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, agentModes, mode, wantLog string
		wantErr                         bool
	}{
		{"set_mode", "modes:session", "accept_edits", "set_mode:accept_edits", false},
		{"already current", "modes:session", "default", "", false},
		{"empty mode", "modes:session", "", "", false},
		{"agent rejects", "modes:session", "nope", "", true},
		{"config option", "modes:config", "bypass_permissions", "set_config_option:bypass_permissions", false},
		{"no modes, default", "", ACPModeDefault, "", false},
		{"no modes, other", "", "accept_edits", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "reply")
			r := glmRunnerWithCaps(wm, tc.agentModes)
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{Mode: tc.mode}, "", events)
			}()
			got := drainEvents(events)
			err := <-errCh
			if tc.wantErr {
				if err == nil {
					t.Fatalf("RunPrompt() error = nil, want a mode error")
				}
				if texts := eventTexts(got); len(texts) != 0 {
					t.Fatalf("prompt ran despite mode failure: %v", texts)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			if log := sessionModeLog(t, *task.WorktreePath); log != tc.wantLog {
				t.Fatalf("session-mode log = %q, want %q", log, tc.wantLog)
			}
		})
	}
}

// switchingDecider calls Runner.SetPermissionMode from inside Decide --
// i.e. while the run's session is provably live -- then allows.
type switchingDecider struct {
	r        *Runner
	chatID   int64
	provider Provider
	mode     string
	err      error
}

func (d *switchingDecider) Decide(ctx context.Context, _ string, options []PermissionOption) (string, error) {
	d.err = d.r.SetPermissionMode(ctx, d.chatID, d.provider, d.mode)
	return options[0].ID, nil
}

// TestRunner_SetPermissionMode is S11 at the Runner layer: a live ACP
// session switches via session/set_mode; Codex is ErrModeSwitchNotSupported;
// a chat with no live session fails for Claude and ACP alike.
func TestRunner_SetPermissionMode(t *testing.T) {
	t.Parallel()

	t.Run("live ACP session", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "permission")
		r := glmRunnerWithCaps(wm, "modes:session")
		d := &switchingDecider{r: r, chatID: task.ID, provider: ProviderGLM, mode: "bypass_permissions"}
		events := make(chan Event)
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", d, PermissionSettings{}, "", events)
		}()
		drainEvents(events)
		if err := <-errCh; err != nil {
			t.Fatalf("RunPrompt() error = %v", err)
		}
		if d.err != nil {
			t.Fatalf("SetPermissionMode() mid-run error = %v", d.err)
		}
		if log := sessionModeLog(t, *task.WorktreePath); log != "set_mode:bypass_permissions" {
			t.Fatalf("session-mode log = %q", log)
		}
		// The turn is over: no live session any more.
		if err := r.SetPermissionMode(context.Background(), task.ID, ProviderGLM, "default"); err == nil {
			t.Fatal("SetPermissionMode() after the turn = nil, want no-live-session error")
		}
	})

	t.Run("live Claude session", func(t *testing.T) {
		t.Parallel()
		r := New(nil)
		fake := &fakeClaudeModeClient{}
		r.trackClaudeClient(7, fake)
		if err := r.SetPermissionMode(context.Background(), 7, ProviderClaudeNative, ClaudeModeDefault); err != nil {
			t.Fatalf("SetPermissionMode() error = %v", err)
		}
		if fake.mode != ClaudeModeDefault {
			t.Fatalf("claude client mode = %q, want %q", fake.mode, ClaudeModeDefault)
		}
		r.untrackClaudeClient(7)
		if err := r.SetPermissionMode(context.Background(), 7, ProviderClaudeNative, ClaudeModePlan); err == nil {
			t.Fatal("SetPermissionMode() with no live claude session = nil, want error")
		}
	})

	t.Run("codex unsupported", func(t *testing.T) {
		t.Parallel()
		err := New(nil).SetPermissionMode(context.Background(), 1, ProviderCodexNative, CodexModeFullAccess)
		if !errors.Is(err, ErrModeSwitchNotSupported) {
			t.Fatalf("err = %v, want ErrModeSwitchNotSupported", err)
		}
	})
}

// fakeClaudeModeClient is a claudeBackend that only records
// SetPermissionMode calls.
type fakeClaudeModeClient struct{ mode string }

func (f *fakeClaudeModeClient) Prompt(context.Context, string, chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	return claudecode.ResultMessage{}, nil
}
func (f *fakeClaudeModeClient) SetPermissionMode(_ context.Context, mode string) error {
	f.mode = mode
	return nil
}
func (f *fakeClaudeModeClient) Close() error { return nil }

// sequenceDecider runs each of modes through Runner.SetPermissionMode from
// inside Decide (the session is provably live), then allows.
type sequenceDecider struct {
	r        *Runner
	chatID   int64
	provider Provider
	modes    []string
	errs     []error
}

func (d *sequenceDecider) Decide(ctx context.Context, _ string, options []PermissionOption) (string, error) {
	for _, m := range d.modes {
		d.errs = append(d.errs, d.r.SetPermissionMode(ctx, d.chatID, d.provider, m))
	}
	return options[0].ID, nil
}

// TestRunner_SetPermissionMode_ConfigOptionAgentRoundTrip is the regression
// test for the stale config-option currentValue: on an agent exposing its
// modes as a category-"mode" config option, default -> bypass_permissions
// -> default must send *both* switches (the second used to be skipped as
// "already current", leaving the agent in bypass).
func TestRunner_SetPermissionMode_ConfigOptionAgentRoundTrip(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "permission")
	r := glmRunnerWithCaps(wm, "modes:config")
	d := &sequenceDecider{r: r, chatID: task.ID, provider: ProviderGLM, modes: []string{"bypass_permissions", "default"}}
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", d, PermissionSettings{}, "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	for i, err := range d.errs {
		if err != nil {
			t.Fatalf("SetPermissionMode #%d error = %v", i, err)
		}
	}
	want := "set_config_option:bypass_permissions\nset_config_option:default"
	if log := sessionModeLog(t, *task.WorktreePath); log != want {
		t.Fatalf("session-mode log = %q, want %q", log, want)
	}
}

// TestRunner_SetPermissionMode_FollowsCurrentModeUpdate is the regression
// test for a stale CurrentModeID: after the agent reports its own switch
// to accept_edits (current_mode_update), asking for "default" must
// actually send session/set_mode -- not be skipped because the client
// still thinks the session is in its start mode.
func TestRunner_SetPermissionMode_FollowsCurrentModeUpdate(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "mode-update")
	r := glmRunnerWithCaps(wm, "modes:session")
	d := &sequenceDecider{r: r, chatID: task.ID, provider: ProviderGLM, modes: []string{"default"}}
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", d, PermissionSettings{}, "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if len(d.errs) != 1 || d.errs[0] != nil {
		t.Fatalf("SetPermissionMode errors = %v", d.errs)
	}
	if log := sessionModeLog(t, *task.WorktreePath); log != "set_mode:default" {
		t.Fatalf("session-mode log = %q, want set_mode:default", log)
	}
}

// TestRunner_StartModeAppliedBeforeSessionIsSwitchable is the regression
// test for a mid-run switch racing the start-up mode: the session must not
// be reachable by SetPermissionMode until runACP has applied the run's
// start mode, or a quick switch could land first and then be overwritten
// by the start-up set_mode. While the start-up set_mode is still in flight
// (the fake answers it after 400ms), SetPermissionMode must fail with "no
// live session" rather than send its own switch.
func TestRunner_StartModeAppliedBeforeSessionIsSwitchable(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	r := glmRunnerWithCaps(wm, "modes:slow")
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{Mode: "accept_edits"}, "", events)
	}()
	go drainEvents(events)

	deadline := time.Now().Add(5 * time.Second)
	for sessionModeLog(t, *task.WorktreePath) == "" {
		if time.Now().After(deadline) {
			t.Fatal("start-up set_mode never reached the agent")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The start-up set_mode is now in flight.
	if err := r.SetPermissionMode(context.Background(), task.ID, ProviderGLM, "bypass_permissions"); err == nil {
		t.Fatal("SetPermissionMode during the start-up set_mode succeeded; want no-live-session until the start mode is applied")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if log := sessionModeLog(t, *task.WorktreePath); log != "set_mode:accept_edits" {
		t.Fatalf("session-mode log = %q, want only the start-up set_mode", log)
	}
}
