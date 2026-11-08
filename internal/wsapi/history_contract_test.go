package wsapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// The history read contract (ADR-0053 section 5), end to end through the
// run.logs handler: the label on a skeleton run, the human-only rebuild, the
// degradation, and the guarantee that a caller who sends no `detail` reads what
// it always read.

const (
	hcSession = "11111111-2222-4333-8444-555555555555"
	hcPrompt  = "Fix the failing test"
	hcSecret  = "secret-token-123"
)

type hcEnv struct {
	db    *store.Store
	reg   *runs.Registry
	root  string
	file  string
	calls atomic.Int32 // how many times the rebuilder was asked for a root
}

// newHCEnv stores the Claude fixture turn as finished runs: "skel" (collapsed to
// skeleton form, native_ref -> the recorded session) and "full" (never
// converted, native_ref none: a run stored the way every run used to be).
func newHCEnv(t *testing.T) *hcEnv {
	t.Helper()
	_, db := newTestWorkspaceManager(t)
	reg, err := runs.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	e := &hcEnv{db: db, reg: reg, root: t.TempDir()}
	e.file = filepath.Join(e.root, "projects", native.ClaudeProjectDir("/work/repo"), hcSession+".jsonl")
	data, err := os.ReadFile("../history/native/testdata/claude/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(e.file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.SetRebuilder(native.New(native.Config{Roots: func(string, string) []string { e.calls.Add(1); return []string{e.root} }}))

	ws, err := db.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 5, 9, 10, 0, 0, time.UTC)
	fin := started.Add(10 * time.Second)
	sum := sha256.Sum256([]byte(hcPrompt))
	ref, _ := json.Marshal(native.Ref{Provider: "claude-native", Harness: native.HarnessClaudeCode, SessionID: hcSession, ConfigDir: e.root, Cwd: "/work/repo",
		PromptSha256: hex.EncodeToString(sum[:]), TurnWindow: native.TurnWindow{StartedAt: started, FinishedAt: fin}})
	for _, id := range []string{"skel", "full"} {
		chat, err := db.CreateChat(task.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateRun(store.Run{ID: id, TaskID: task.ID, ChatID: chat.ID, Provider: "claude-native", Prompt: hcPrompt, Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
			t.Fatal(err)
		}
		for i, ev := range hcLiveTurn() {
			d, err := runs.EncodeStoredEvent(ev)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendRunEvent(id, int64(i), d); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.SetRunNativeRef("skel", string(ref)); err != nil {
		t.Fatal(err)
	}
	if err := runs.Skeletonize(db, "skel"); err != nil {
		t.Fatal(err)
	}
	return e
}

// hcLiveTurn is what the live path stored for the fixture's target turn.
func hcLiveTurn() []taskrunner.Event {
	heredoc := "cat > notes.txt <<'EOF'\n" + hcSecret + "\nEOF\necho written"
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
	var out []taskrunner.Event
	for _, m := range msgs {
		out = append(out, taskrunner.ClaudeMessageEvents(m)...)
	}
	return append(out, taskrunner.Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

func (e *hcEnv) logs(t *testing.T, rc *requestContext, params map[string]any) (runLogsResult, map[string]json.RawMessage) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := handleRunLogs(e.reg)(context.Background(), rc, raw)
	if err != nil {
		t.Fatalf("run.logs %v: %v", params, err)
	}
	b, _ := json.Marshal(out)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(b, &keys)
	return out.(runLogsResult), keys
}

var (
	hcHuman = &requestContext{local: true}
	hcAgent = &requestContext{runScoped: true}
)

func eventsText(res runLogsResult) string {
	b, _ := json.Marshal(res.Events)
	return string(b)
}

// A caller that sends no detail on a run that is not in skeleton form gets the
// result it always got: no new key, same events.
func TestRunLogsDefaultReadIsUnchanged(t *testing.T) {
	e := newHCEnv(t)
	res, keys := e.logs(t, hcHuman, map[string]any{"runId": "full"})
	for _, k := range []string{"historySource", "skeletonReason", "canLoadFull"} {
		if _, ok := keys[k]; ok {
			t.Errorf("a run stored the old way grew a %q key", k)
		}
	}
	view, err := e.reg.Tail("full", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := eventsText(res), eventsJSON(runLogEventsPrimed(view.Prime, view.Events, 0)); got != want {
		t.Fatalf("events changed\n got %s\nwant %s", got, want)
	}
	// the same read with an explicit detail of skeleton/auto serves the same events
	for _, d := range []string{"skeleton", "auto"} {
		r2, _ := e.logs(t, hcHuman, map[string]any{"runId": "full", "detail": d})
		if eventsText(r2) != eventsText(res) || r2.HistorySource != "full" {
			t.Errorf("detail %q on a full run: source %q", d, r2.HistorySource)
		}
	}
}

func eventsJSON(evs []runLogEvent) string { b, _ := json.Marshal(evs); return string(b) }

func TestRunLogsSkeletonRunIsLabelled(t *testing.T) {
	e := newHCEnv(t)
	res, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel"})
	if res.HistorySource != "skeleton" || res.SkeletonReason != "rebuild_not_requested" || !res.CanLoadFull {
		t.Fatalf("human default read: %+v", res.historyFields)
	}
	if strings.Contains(eventsText(res), "need the failing test first") {
		t.Fatal("the stored skeleton carries thinking text it dropped")
	}
	agent, _ := e.logs(t, hcAgent, map[string]any{"runId": "skel"})
	if agent.HistorySource != "skeleton" || agent.CanLoadFull {
		t.Fatalf("agent default read: %+v (an agent can never load the full history)", agent.historyFields)
	}
	if e.calls.Load() != 0 {
		t.Fatalf("a read without detail touched the provider session roots %d times", e.calls.Load())
	}
}

// A human's detail=full rebuilds, merges, and goes through the same elider a live
// read does.
func TestRunLogsFullRebuildsForAHuman(t *testing.T) {
	e := newHCEnv(t)
	res, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel", "detail": "full"})
	if res.HistorySource != "rebuilt" || res.SkeletonReason != "" {
		t.Fatalf("label = %+v", res.historyFields)
	}
	text := eventsText(res)
	if !strings.Contains(text, "need the failing test first") {
		t.Fatal("the rebuilt history lacks the thinking text the skeleton dropped")
	}
	if strings.Contains(text, hcSecret) {
		t.Fatal("a heredoc body reached a rebuilt run.logs event")
	}
	if !strings.Contains(text, `"type":"done"`) {
		t.Fatal("the stored done row was not merged back in")
	}
	// the tool call is elided exactly as in a live read of the stored-full run
	full, _ := e.logs(t, hcHuman, map[string]any{"runId": "full"})
	toolOf := func(r runLogsResult) []string {
		var out []string
		for _, ev := range r.Events {
			if ev.Type == "tool_call" {
				b, _ := json.Marshal(ev)
				out = append(out, string(b))
			}
		}
		return out
	}
	if strings.Join(toolOf(res), "\n") != strings.Join(toolOf(full), "\n") {
		t.Fatalf("rebuilt tool calls differ from the live read of the same run\n got %v\nwant %v", toolOf(res), toolOf(full))
	}
	// tail applies to the merged history
	tail, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel", "detail": "full", "tail": 2})
	if len(tail.Events) != 2 || tail.HistorySource != "rebuilt" {
		t.Fatalf("tail 2 of the rebuilt history: %d events, %q", len(tail.Events), tail.HistorySource)
	}
}

// H7: an agent asking for the full history is told why not and never reaches the
// provider's session.
func TestRunLogsAgentCannotRebuild(t *testing.T) {
	e := newHCEnv(t)
	res, _ := e.logs(t, hcAgent, map[string]any{"runId": "skel", "detail": "full"})
	if res.HistorySource != "skeleton" || res.SkeletonReason != string(native.ReasonHumanOnly) || res.CanLoadFull {
		t.Fatalf("agent detail=full: %+v", res.historyFields)
	}
	if strings.Contains(eventsText(res), "need the failing test first") {
		t.Fatal("an agent read the rebuilt history")
	}
	if e.calls.Load() != 0 {
		t.Fatalf("an agent's request reached the provider session %d times", e.calls.Load())
	}
	// and a missing principal is not a human either
	nilRes, _ := e.logs(t, nil, map[string]any{"runId": "skel", "detail": "full"})
	if nilRes.HistorySource != "skeleton" || e.calls.Load() != 0 {
		t.Fatalf("no principal: %+v, %d calls", nilRes.historyFields, e.calls.Load())
	}
}

// Failure degrades to the skeleton with the reason: never an error.
func TestRunLogsFullDegrades(t *testing.T) {
	e := newHCEnv(t)
	if err := os.Remove(e.file); err != nil {
		t.Fatal(err)
	}
	res, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel", "detail": "full"})
	if res.HistorySource != "skeleton" || res.SkeletonReason != string(native.ReasonSessionMissing) || len(res.Events) == 0 {
		t.Fatalf("missing session: %+v with %d events", res.historyFields, len(res.Events))
	}
	// an unknown run is still the old error, not a label
	raw, _ := json.Marshal(map[string]any{"runId": "nope", "detail": "full"})
	if _, err := handleRunLogs(e.reg)(context.Background(), hcHuman, raw); err == nil {
		t.Fatal("an unknown run must stay an error")
	}
}

func TestRunLogsRejectsAnUnknownDetail(t *testing.T) {
	e := newHCEnv(t)
	raw, _ := json.Marshal(map[string]any{"runId": "skel", "detail": "everything"})
	_, err := handleRunLogs(e.reg)(context.Background(), hcHuman, raw)
	wantErrCode(t, err, codeInvalid)
}

// The attach terminal result carries the label only for a skeleton run.
func TestAttachResultCarriesTheLabelOnlyWhenThereIsOne(t *testing.T) {
	plain, _ := json.Marshal(taskPromptResult{RunID: "r", StopReason: "end_turn"})
	if strings.Contains(string(plain), "historySource") || strings.Contains(string(plain), "canLoadFull") {
		t.Fatalf("an unlabelled result grew keys: %s", plain)
	}
	e := newHCEnv(t)
	label := historyLabel(e.reg, "skel", "", true)
	b, _ := json.Marshal(taskPromptResult{historyFields: label, RunID: "skel", StopReason: "end_turn"})
	for _, want := range []string{`"historySource":"skeleton"`, `"skeletonReason":"rebuild_not_requested"`, `"canLoadFull":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("result lacks %s: %s", want, b)
		}
	}
	if l := historyLabel(e.reg, "full", "", true); l != (historyFields{}) {
		t.Errorf("a run stored the old way is labelled: %+v", l)
	}
}

// run.revealToolHeredoc falls back to the provider's session for a human when
// the stored input is only head and tail; an agent never gets there.
type rebuildingHeredocs struct {
	fakeHeredocs
	calls int
	body  string
	err   error
}

func (f *rebuildingHeredocs) ToolCallHeredoc(run, call string, i int) (runs.HeredocReveal, error) {
	return runs.HeredocReveal{}, runs.ErrHeredocUnavailable
}

func (f *rebuildingHeredocs) RebuildHeredoc(_ context.Context, run, call string, i int, who runs.Requester) (runs.HeredocReveal, error) {
	f.calls++
	if f.err != nil || who != runs.RequesterHuman {
		return runs.HeredocReveal{}, runs.ErrHeredocUnavailable
	}
	return runs.HeredocReveal{RunID: run, Text: f.body}, nil
}

func TestRevealToolHeredocFallsBackToTheProviderSession(t *testing.T) {
	_, db := newTestWorkspaceManager(t)
	src := &rebuildingHeredocs{body: "the rebuilt body\n"}
	h := heredocRevealHandlers(src, db)["run.revealToolHeredoc"]
	res, err := revealCall(t, h, hcHuman, map[string]any{"runId": "run-1", "toolCallId": "call-1", "index": 0})
	if err != nil || !strings.Contains(res.Text, "rebuilt body") || src.calls != 1 {
		t.Fatalf("fallback: %+v, %v, %d rebuild calls", res, err, src.calls)
	}
	// an agent is refused before any of it
	_, err = revealCall(t, h, hcAgent, map[string]any{"runId": "run-1", "toolCallId": "call-1", "index": 0})
	if err == nil || src.calls != 1 {
		t.Fatalf("agent reveal: err %v, %d rebuild calls (want refused, still 1)", err, src.calls)
	}
	// a body that cannot be rebuilt is the typed not-found, never a body
	src.err = runs.ErrHeredocUnavailable
	_, err = revealCall(t, h, hcHuman, map[string]any{"runId": "run-1", "toolCallId": "call-1", "index": 0})
	wantErrCode(t, err, codeNotFound)
}

// H5: a human's detail=auto open rebuilds a small claude-native session by
// itself, from the cache after the first read.
func TestRunLogsAutoRebuildsOnOpenForAHuman(t *testing.T) {
	e := newHCEnv(t)
	e.reg.SetRebuildCacheBudget(16 << 20)
	res, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel", "detail": "auto"})
	if res.HistorySource != "rebuilt" || res.SkeletonReason != "" {
		t.Fatalf("label = %+v", res.historyFields)
	}
	if !strings.Contains(eventsText(res), "need the failing test first") || strings.Contains(eventsText(res), hcSecret) {
		t.Fatal("auto did not return the rebuilt, elided history")
	}
	again, _ := e.logs(t, hcHuman, map[string]any{"runId": "skel", "detail": "auto"})
	if eventsText(again) != eventsText(res) || e.reg.RebuildCacheStats().Hits != 1 {
		t.Fatalf("second open: hits %d", e.reg.RebuildCacheStats().Hits)
	}
}

// H7: an agent, an automation or a caller with no principal that sends auto reads
// exactly what it reads with no detail, and never reaches the provider session.
func TestRunLogsAutoNeverRebuildsForAMachine(t *testing.T) {
	e := newHCEnv(t)
	e.reg.SetRebuildCacheBudget(16 << 20)
	plain, _ := e.logs(t, hcAgent, map[string]any{"runId": "skel"})
	for _, rc := range []*requestContext{hcAgent, nil} {
		res, _ := e.logs(t, rc, map[string]any{"runId": "skel", "detail": "auto"})
		if res.HistorySource != "skeleton" || res.CanLoadFull || eventsText(res) != eventsText(plain) {
			t.Fatalf("machine auto: %+v", res.historyFields)
		}
	}
	if e.calls.Load() != 0 || e.reg.RebuildCacheStats().Entries != 0 {
		t.Fatalf("a machine's auto reached the provider session (%d root lookups)", e.calls.Load())
	}
}
