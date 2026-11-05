package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	rebuildSession = "11111111-2222-4333-8444-555555555555"
	rebuildPrompt  = "Fix the failing test"
	secretBody     = "secret-token-123"
)

// liveTurn is what the live path stored for the fixture's target turn: the SDK
// messages the CLI streamed, through taskrunner's mapping, plus the run's done.
func liveTurn(filler string) []Event {
	heredoc := "cat > notes.txt <<'EOF'\n" + secretBody + "\n" + filler + "EOF\necho written"
	msgs := []claudecode.Message{
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ThinkingBlock{Thinking: "need the failing test first"}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "I will run the tests. "}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B1", Name: "Bash", Input: map[string]any{"command": heredoc, "description": "write notes"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B1", Content: json.RawMessage(`"written"`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B2", Name: "Read", Input: map[string]any{"file_path": "/work/repo/main.go"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B2", Content: json.RawMessage(`[{"type":"text","text":"package main"}]`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B3", Name: "Bash", Input: map[string]any{"command": "go test ./..."}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B3", Content: json.RawMessage(`"FAIL: boom"`), IsError: true}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "Done: fixed."}}},
	}
	var out []Event
	for _, m := range msgs {
		out = append(out, taskrunner.ClaudeMessageEvents(m)...)
	}
	return append(out, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

type rebuildEnv struct {
	e    *histEnv
	reg  *Registry
	root string
	file string
}

// newRebuildEnv stores the live turn as finished run "r" (chat 5) with a
// native_ref pointing at a copy of the recorded Claude session, and converts it
// to skeleton form as finish does.
func newRebuildEnv(t *testing.T, convert bool) *rebuildEnv { return newRebuildEnvFiller(t, convert, 0) }

// newRebuildEnvFiller is newRebuildEnv with a heredoc body padded by n lines, so
// the call's input is over 512 B (n = 30) or over 8 KiB (n = 600).
func newRebuildEnvFiller(t *testing.T, convert bool, n int) *rebuildEnv {
	t.Helper()
	e := newHistEnv(t)
	reg := newTestRegistry(t, e.st)
	root := t.TempDir()
	file := filepath.Join(root, "projects", native.ClaudeProjectDir("/work/repo"), rebuildSession+".jsonl")
	data, err := os.ReadFile("../history/native/testdata/claude/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	filler := strings.Repeat("filler line of the heredoc body\n", n)
	// the same padding goes into the recorded session (JSON-escaped newlines)
	data = []byte(strings.Replace(string(data), secretBody+`\n`, secretBody+`\n`+strings.ReplaceAll(filler, "\n", `\n`), 1))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.SetRebuilder(native.New(native.Config{Roots: func(string, string) []string { return []string{root} }}))

	started := time.Date(2026, 11, 5, 9, 10, 0, 0, time.UTC)
	fin := started.Add(10 * time.Second)
	chat, err := e.st.CreateChat(e.task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateRun(store.Run{ID: "r", TaskID: e.task.ID, ChatID: chat.ID, Provider: "claude-native", Prompt: rebuildPrompt, Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
		t.Fatal(err)
	}
	for i, ev := range liveTurn(filler) {
		d, err := encodeEvent(ev)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.AppendRunEvent("r", int64(i), d); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(rebuildPrompt))
	ref := native.Ref{Provider: "claude-native", Harness: native.HarnessClaudeCode, SessionID: rebuildSession, ConfigDir: root, Cwd: "/work/repo",
		PromptSha256: hex.EncodeToString(sum[:]), TurnWindow: native.TurnWindow{StartedAt: started, FinishedAt: fin}}
	b, _ := json.Marshal(ref)
	if err := e.st.SetRunNativeRef("r", string(b)); err != nil {
		t.Fatal(err)
	}
	if convert {
		if err := Skeletonize(e.st, "r"); err != nil {
			t.Fatal(err)
		}
	}
	return &rebuildEnv{e: e, reg: reg, root: root, file: file}
}

func clientView(evs []Event, elide bool) []string {
	el := taskrunner.NewToolCallElider()
	var out []string
	for _, ev := range evs {
		if elide && ev.Type == taskrunner.EventTypeToolCall {
			ev.ToolInput, ev.ToolTitle, ev.ToolResult, _ = el.Elide(ev.ToolCallID, ev.ToolInput, ev.ToolTitle, ev.ToolResult)
		}
		if ev.Type == taskrunner.EventTypeDone {
			continue // the stop reason is not in the provider's session
		}
		norm := func(r json.RawMessage) string {
			var v any
			if json.Unmarshal(r, &v) != nil {
				return string(r)
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
		out = append(out, fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s", ev.Type, ev.Text, ev.ToolCallID, ev.ToolName, ev.ToolStatus, norm(ev.ToolInput), norm(ev.ToolResult)))
	}
	return out
}

// What each consumer sees: a human request gets the turn's text, thinking and tool
// calls from the provider session in client form; the stored run (full or
// skeleton) is what everyone else sees. For the recorded session the rebuilt
// events equal the stored-full ones, so the two paths agree.
func TestRebuildHistoryEquivalentToStoredFull(t *testing.T) {
	env := newRebuildEnv(t, false)
	stored, _, err := env.reg.History("r")
	if err != nil {
		t.Fatal(err)
	}
	got := env.reg.RebuildHistory(context.Background(), "r", RequesterHuman)
	if !got.OK() {
		t.Fatalf("rebuild refused: %q (%s)", got.Reason, got.Detail)
	}
	if fmt.Sprint(clientView(got.Events, false)) != fmt.Sprint(clientView(stored, true)) {
		t.Fatalf("rebuilt differs from stored-full\n got %q\nwant %q", clientView(got.Events, false), clientView(stored, true))
	}
	for _, line := range clientView(got.Events, false) {
		if strings.Contains(line, secretBody) {
			t.Fatalf("a heredoc body reached a rebuilt event: %s", line)
		}
	}
}

// A skeleton run rebuilds too, and the rebuilt turn restores what the skeleton
// dropped (the thinking text).
func TestRebuildHistoryOfASkeletonRun(t *testing.T) {
	env := newRebuildEnv(t, true)
	got := env.reg.RebuildHistory(context.Background(), "r", RequesterHuman)
	if !got.OK() {
		t.Fatalf("rebuild refused: %q (%s)", got.Reason, got.Detail)
	}
	if !strings.Contains(fmt.Sprint(clientView(got.Events, false)), "need the failing test first") {
		t.Fatalf("the rebuilt history lacks the thinking text the skeleton dropped")
	}
}

func TestRebuildHistoryRefusals(t *testing.T) {
	env := newRebuildEnv(t, true)
	ctx := context.Background()
	if r := env.reg.RebuildHistory(ctx, "r", RequesterAgent); r.Reason != native.ReasonHumanOnly || len(r.Events) != 0 {
		t.Fatalf("agent: %q with %d events", r.Reason, len(r.Events))
	}
	if r := env.reg.RebuildHistory(ctx, "missing", RequesterHuman); r.Reason != native.ReasonCannotRebuild {
		t.Fatalf("unknown run: %q", r.Reason)
	}
	if err := env.e.st.SetRunNativeRef("r", ""); err != nil {
		t.Fatal(err)
	}
	if r := env.reg.RebuildHistory(ctx, "r", RequesterHuman); r.Reason != native.ReasonCannotRebuild {
		t.Fatalf("no native_ref: %q", r.Reason)
	}
	none := newTestRegistry(t, env.e.st) // no rebuilder wired
	if r := none.RebuildHistory(ctx, "r", RequesterHuman); r.Reason != native.ReasonCannotRebuild {
		t.Fatalf("no rebuilder: %q", r.Reason)
	}
}

// The session must not be read while its chat is running another turn.
func TestRebuildSkippedWhileChatRunning(t *testing.T) {
	env := newRebuildEnv(t, true)
	row, _ := env.e.st.GetRun("r")
	started := time.Now().UTC()
	if _, err := env.e.st.CreateRun(store.Run{ID: "busy", TaskID: env.e.task.ID, ChatID: row.ChatID, Provider: "claude-native", Prompt: "p", Status: "running", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	registerRunning(env.reg, "busy", env.e.task.ID, row.ChatID, started)
	if r := env.reg.RebuildHistory(context.Background(), "r", RequesterHuman); r.Reason != native.ReasonSessionInUse {
		t.Fatalf("chat busy: %q, want native_session_in_use", r.Reason)
	}
}

func registerRunning(reg *Registry, id string, taskID, chatID int64, started time.Time) {
	r := &run{id: id, taskID: taskID, chatID: chatID, status: StatusRunning, startedAt: started, subscribers: map[int]*subQueue{}, pendingPermissions: map[string]chan string{}, closedCh: make(chan struct{})}
	r.cancel = func() {
		r.mu.Lock()
		r.status = StatusStopped
		r.mu.Unlock()
		select {
		case <-r.closedCh:
		default:
			close(r.closedCh)
		}
	}
	reg.mu.Lock()
	reg.runs[id] = r
	reg.mu.Unlock()
}

// A rebuilt heredoc body is shown only if it hashes to the descriptor the
// skeleton kept for that index; otherwise the typed unavailable error.
func TestRebuildHeredocHashChecked(t *testing.T) {
	env := newRebuildEnvFiller(t, true, 600) // the input is over 8 KiB: only head and tail are stored
	ctx := context.Background()
	if _, err := env.reg.ToolCallHeredoc("r", "toolu_B1", 0); err != ErrHeredocUnavailable {
		t.Fatalf("setup: the stored input should no longer be whole: %v", err)
	}
	rev, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B1", 0, RequesterHuman)
	if err != nil || !strings.Contains(rev.Text, secretBody) {
		t.Fatalf("reveal = %q, %v", rev.Text, err)
	}
	if _, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B1", 0, RequesterAgent); err != ErrHeredocUnavailable {
		t.Fatalf("agent: %v, want ErrHeredocUnavailable", err)
	}
	if _, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B1", 3, RequesterHuman); err != ErrHeredocUnavailable {
		t.Fatalf("an index the skeleton has no descriptor for: %v", err)
	}
	if _, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B3", 0, RequesterHuman); err != ErrHeredocUnavailable {
		t.Fatalf("a call without a heredoc: %v", err)
	}
	// the session was edited since: the body no longer matches the stored hash
	b, _ := os.ReadFile(env.file)
	os.WriteFile(env.file, []byte(strings.ReplaceAll(string(b), secretBody, "tampered-body-456")), 0o600)
	if _, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B1", 0, RequesterHuman); err != ErrHeredocUnavailable {
		t.Fatalf("a body with a different hash was shown: %v", err)
	}
}

// The self-check compares the session's tool calls with the stored run's.
func TestRebuildDriftWhenToolCountsDisagree(t *testing.T) {
	env := newRebuildEnv(t, true)
	if _, err := env.e.st.DB().Exec(`DELETE FROM run_events WHERE run_id = 'r' AND json_extract(event_data, '$.toolCallId') IN ('toolu_B2', 'toolu_B3')`); err != nil {
		t.Fatal(err)
	}
	r := env.reg.RebuildHistory(context.Background(), "r", RequesterHuman)
	if r.Reason != native.ReasonFormatDrift || len(r.Events) != 0 {
		t.Fatalf("stored run has 1 call, session has 3: %q (%s) with %d events, want format_drift", r.Reason, r.Detail, len(r.Events))
	}
}
