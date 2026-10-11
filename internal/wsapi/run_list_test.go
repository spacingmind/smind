package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// runListForTest decodes just the fields these tests check off a
// run.list result -- same convention as runStatusForTest in chat_test.go
// (run.list returns runs.RunSummary, whose Go field names are the wire
// keys verbatim, no json tags).
type runListForTest struct {
	ID     string
	TaskID int64
	ChatID int64
	Status string
}

func listRuns(t *testing.T, ws *websocket.Conn, id string, params any) []runListForTest {
	t.Helper()
	sendRequest(t, ws, id, "run.list", params)
	resp := readEnvelopeFor(t, ws, id, 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.list error = %v", resp.Error.Message)
	}
	var runs []runListForTest
	if err := json.Unmarshal(resp.Result, &runs); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	return runs
}

// promptFinishedRun starts a run (non-blocking run.start) on
// taskID/chatID and polls run.list until that run reaches a terminal
// status, returning its id. chatID 0 means the task's default chat.
func promptFinishedRun(t *testing.T, ws *websocket.Conn, taskID, chatID int64) string {
	t.Helper()
	params := map[string]any{"taskId": taskID, "provider": "glm", "prompt": "hi"}
	if chatID != 0 {
		params["chatId"] = chatID
	}
	sendRequest(t, ws, "start", "run.start", params)
	resp := readEnvelopeFor(t, ws, "start", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var start struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(resp.Result, &start); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		runs := listRuns(t, ws, "poll", map[string]any{})
		for _, r := range runs {
			if r.ID == start.RunID && r.Status != "running" {
				return start.RunID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never reached a terminal status", start.RunID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// startHangingRun starts (non-blocking) a run whose fake agent hangs, so
// a genuinely still-running run exists for status=running assertions.
func startHangingRun(t *testing.T, ws *websocket.Conn, taskID int64) string {
	t.Helper()
	sendRequest(t, ws, "start", "run.start", map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "hi",
	})
	resp := readEnvelopeFor(t, ws, "start", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var start struct {
		RunID string `json:"RunID"`
	}
	if err := json.Unmarshal(resp.Result, &start); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}
	return start.RunID
}

// TestRunList_ByTaskAndStatus proves run.list's additive discovery
// filters: taskId returns that task's runs across all of its chats (both
// chats of the task show up, another task's run doesn't), and
// status=running returns only live runs.
func TestRunList_ByTaskAndStatus(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	// A hanging-scenario task, so its run stays StatusRunning.
	other := newTestTask(t, wm, "hang")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// The default chat's id, and a second chat under the same task.
	sendRequest(t, ws, "chats", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "chats", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.list error = %v", resp.Error.Message)
	}
	var chats []struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	sendRequest(t, ws, "create", "chat.create", map[string]any{"taskId": task.ID, "title": "Second"})
	resp = readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.create error = %v", resp.Error.Message)
	}
	var second struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &second); err != nil {
		t.Fatalf("decode chat.create result: %v", err)
	}

	// A finished run on each of the task's two chats, plus a hanging run
	// on the other task.
	runDefault := promptFinishedRun(t, ws, task.ID, 0)
	runSecond := promptFinishedRun(t, ws, task.ID, second.ID)
	hangRun := startHangingRun(t, ws, other.ID)

	// taskId filter: both of the task's chats' finished runs, not the
	// other task's run.
	runs := listRuns(t, ws, "bytask", map[string]any{"taskId": task.ID})
	if len(runs) != 2 {
		t.Fatalf("run.list(taskId=%d) = %+v, want the task's 2 runs across its chats", task.ID, runs)
	}
	for _, r := range runs {
		if r.TaskID != task.ID {
			t.Fatalf("run.list(taskId=%d) returned run of task %d", task.ID, r.TaskID)
		}
		if r.ID != runDefault && r.ID != runSecond {
			t.Fatalf("run.list(taskId=%d) returned unexpected run %s", task.ID, r.ID)
		}
	}

	// taskId + status=running: the task has no live runs, so nothing.
	runs = listRuns(t, ws, "bytask-running", map[string]any{"taskId": task.ID, "status": "running"})
	if len(runs) != 0 {
		t.Fatalf("run.list(taskId, status=running) = %+v, want none", runs)
	}

	// taskId + status=done: both finished runs again.
	runs = listRuns(t, ws, "bytask-done", map[string]any{"taskId": task.ID, "status": "done"})
	if len(runs) != 2 {
		t.Fatalf("run.list(taskId, status=done) = %+v, want the 2 finished runs", runs)
	}

	// status=running alone: exactly the hanging run.
	runs = listRuns(t, ws, "running", map[string]any{"status": "running"})
	if len(runs) != 1 || runs[0].ID != hangRun {
		t.Fatalf("run.list(status=running) = %+v, want exactly the hanging run %s", runs, hangRun)
	}

	// Unknown task: empty list, not an error.
	runs = listRuns(t, ws, "unknown", map[string]any{"taskId": 99999})
	if len(runs) != 0 {
		t.Fatalf("run.list(taskId=unknown) = %+v, want empty", runs)
	}

	// Stop the hanging run so it can't outlive the test.
	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": hangRun})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunList_LegacyChatIDUnchanged pins the pre-filter contract: a call
// carrying only chatId (or no params at all) keeps returning exactly what
// reg.List always returned -- same rows, newest first, bare-array wire
// shape -- because with no taskId/status the handler still takes the
// original reg.List(chatId) path.
func TestRunList_LegacyChatIDUnchanged(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "chats", "chat.list", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "chats", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.list error = %v", resp.Error.Message)
	}
	var chats []struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &chats); err != nil {
		t.Fatalf("decode chat.list result: %v", err)
	}
	defaultChatID := chats[0].ID

	runA := promptFinishedRun(t, ws, task.ID, 0)
	runB := promptFinishedRun(t, ws, task.ID, 0)

	// Unfiltered: both runs, newest first.
	all := listRuns(t, ws, "all", map[string]any{})
	if len(all) != 2 {
		t.Fatalf("run.list() = %+v, want 2 runs", all)
	}
	if all[0].ID != runB || all[1].ID != runA {
		t.Fatalf("run.list() not newest-first: %+v", all)
	}

	// chatId-only: exactly the default chat's runs, same order.
	byChat := listRuns(t, ws, "bychat", map[string]any{"chatId": defaultChatID})
	if len(byChat) != 2 || byChat[0].ID != runB || byChat[1].ID != runA {
		t.Fatalf("run.list(chatId=%d) = %+v, want the same 2 runs newest-first", defaultChatID, byChat)
	}

	// chatId=0 explicitly: unfiltered, identical to no params.
	zero := listRuns(t, ws, "zero", map[string]any{"chatId": 0})
	if len(zero) != 2 || zero[0].ID != runB {
		t.Fatalf("run.list(chatId=0) = %+v, want the same as unfiltered", zero)
	}

	// And the result is still a bare JSON array (the pre-filter shape).
	sendRequest(t, ws, "raw", "run.list", map[string]any{"chatId": defaultChatID})
	resp = readEnvelopeFor(t, ws, "raw", 5*time.Second)
	if len(resp.Result) == 0 || resp.Result[0] != '[' {
		t.Fatalf("run.list result = %s, want a bare JSON array", resp.Result)
	}
}

// TestRunList_UnknownStatusRejected proves an unrecognized status value
// is rejected with an error naming the valid statuses, rather than
// silently matching nothing and returning an empty list.
func TestRunList_UnknownStatusRejected(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	run := promptFinishedRun(t, ws, task.ID, 0)
	_ = run

	sendRequest(t, ws, "bogus", "run.list", map[string]any{"status": "bogus"})
	resp := readEnvelopeFor(t, ws, "bogus", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("run.list(status=bogus) succeeded with result %s, want an error", resp.Result)
	}
	for _, want := range []string{"bogus", "running", "done", "error", "stopped", "interrupted"} {
		if !strings.Contains(resp.Error.Message, want) {
			t.Fatalf("error %q does not mention %q", resp.Error.Message, want)
		}
	}

	// Sanity: a valid status still works.
	runs := listRuns(t, ws, "done", map[string]any{"status": "done"})
	if len(runs) != 1 {
		t.Fatalf("run.list(status=done) = %+v, want the 1 finished run", runs)
	}
}
