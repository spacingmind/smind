package wsapi

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// tailHistory is a run whose heredoc calls have their input and result in
// different events: calls A and C get their result well after their input.
func tailHistory() []taskrunner.Event {
	in := func(cmd string) json.RawMessage { b, _ := json.Marshal(map[string]string{"command": cmd}); return b }
	res := func(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
	cmdA := "cat > a.txt <<'EOF'\nSECRETBODY-A\nEOF"
	cmdC := "cat > c1 <<'EOF'\nSECRETBODY-C1\nEOF\ncat > c2 <<'EOF'\nSECRETBODY-C2\nEOF"
	call := func(id, status string, input, result json.RawMessage) taskrunner.Event {
		return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: status, ToolInput: input, ToolResult: result}
	}
	return []taskrunner.Event{
		{Type: taskrunner.EventTypeUserMessage, Text: "go"},
		call("A", taskrunner.ToolStatusRunning, in(cmdA), nil),
		{Type: taskrunner.EventTypeText, Text: "one "},
		call("B", taskrunner.ToolStatusRunning, in("echo plain"), nil),
		call("A", taskrunner.ToolStatusSuccess, nil, res("ran: "+cmdA+" -> SECRETBODY-A")),
		{Type: taskrunner.EventTypeText, Text: "two "},
		call("C", taskrunner.ToolStatusRunning, in(cmdC), nil),
		call("C", taskrunner.ToolStatusSuccess, nil, res("ran: "+cmdC)),
		call("B", taskrunner.ToolStatusSuccess, nil, res("plain out")),
		{Type: taskrunner.EventTypeText, Text: "three"},
		{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
	}
}

func seedTailRun(t *testing.T, st *store.Store, id string, hist []taskrunner.Event) {
	t.Helper()
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/x/repo-" + id, Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := st.CreateRun(store.Run{ID: id, TaskID: tk.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: now, FinishedAt: &now}); err != nil {
		t.Fatal(err)
	}
	for i, e := range hist {
		m := map[string]any{"type": e.Type}
		if e.Text != "" {
			m["text"] = e.Text
		}
		if e.StopReason != "" {
			m["stopReason"] = e.StopReason
		}
		if e.ToolCallID != "" {
			m["toolCallId"], m["toolName"], m["toolStatus"] = e.ToolCallID, e.ToolName, e.ToolStatus
		}
		if len(e.ToolInput) > 0 {
			m["toolInput"] = e.ToolInput
		}
		if len(e.ToolResult) > 0 {
			m["toolResult"] = e.ToolResult
		}
		data, _ := json.Marshal(m)
		if _, err := st.AppendRunEvent(id, int64(i), string(data)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunLogsTail_EvictedEqualsResidentEqualsFullConversion(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hist := tailHistory()
	seedTailRun(t, st, "tail-run", hist)

	resident, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	evicted, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	evicted.SetHistoryBudget(1)
	if _, _, err := resident.History("tail-run"); err != nil { // load into the cache
		t.Fatal(err)
	}

	logs := func(reg *runs.Registry, tail int) string {
		t.Helper()
		view, err := reg.Tail("tail-run", tail)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(runLogEventsPrimed(view.Prime, view.Events, tail))
		return string(b)
	}
	for _, tail := range []int{0, 1, 2, 3, 4, 5, 6, 7, 9, 10, 11, 50} {
		want, _ := json.Marshal(runLogEvents(hist, tail))
		if got := logs(resident, tail); got != string(want) {
			t.Fatalf("tail=%d resident differs from full conversion:\n got %s\nwant %s", tail, got, want)
		}
		got := logs(evicted, tail)
		if got != string(want) {
			t.Fatalf("tail=%d evicted differs from full conversion:\n got %s\nwant %s", tail, got, want)
		}
		if strings.Contains(got, "SECRETBODY") {
			t.Fatalf("tail=%d leaks a heredoc body: %s", tail, got)
		}
	}
	// The evicted registry really served these from the store, not memory.
	if st := evicted.HistoryStats(); st.ResidentRuns != 0 || st.StoreReads == 0 {
		t.Fatalf("evicted registry stats = %+v", st)
	}
	// Without priming, call A's result (tail=7 starts at its result) would leak.
	view, _ := evicted.Tail("tail-run", 7)
	b, _ := json.Marshal(runLogEvents(view.Events, 7))
	if !strings.Contains(string(b), "SECRETBODY-A") {
		t.Fatalf("test premise: the unprimed tail should leak the body (call before the tail window)")
	}
}
