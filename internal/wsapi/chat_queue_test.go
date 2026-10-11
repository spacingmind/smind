package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestChatQueue_ListCancelAndEvent pins ADR-0021 §8's wire surface:
// run.start with whenBusy=queue on a busy chat returns {queued,
// queueItemId}; chat.queueList returns the items; chat.queueCancel works
// on a queued item and errors on a delivered one; and the
// chat.queueUpdated event carries a full snapshot on enqueue, deliver,
// and cancel.
func TestChatQueue_ListCancelAndEvent(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	ec := newEventConn(t, ws)
	ec.subscribe("sub", "chat.queueUpdated")

	// A running run to make the chat busy.
	sendRequest(t, ws, "a", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "a",
	})
	resp := ec.nextResponse("a", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start(a) error = %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode run.start(a): %v", err)
	}
	t.Cleanup(func() {
		sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
		ec.nextResponse("stop", 5*time.Second)
	})
	var chatID int64
	{
		var listed []struct {
			ChatID int64 `json:"ChatID"`
		}
		sendRequest(t, ws, "runs", "run.list", map[string]any{})
		r := ec.nextResponse("runs", 5*time.Second)
		if err := json.Unmarshal(r.Result, &listed); err != nil {
			t.Fatalf("decode run.list: %v", err)
		}
		chatID = listed[0].ChatID
	}

	// Queue two items on the busy chat.
	sendRequest(t, ws, "q1", "run.start", map[string]any{
		"taskId": task.ID, "chatId": chatID, "provider": "glm", "prompt": "q1", "whenBusy": "queue",
	})
	resp = ec.nextResponse("q1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start(whenBusy=queue) error = %v", resp.Error.Message)
	}
	var queued runQueuedResult
	if err := json.Unmarshal(resp.Result, &queued); err != nil {
		t.Fatalf("decode queued result: %v", err)
	}
	if !queued.Queued || queued.QueueItemID == 0 {
		t.Fatalf("run.start(whenBusy=queue) = %+v, want {queued, queueItemId}", queued)
	}

	sendRequest(t, ws, "q2", "run.start", map[string]any{
		"taskId": task.ID, "chatId": chatID, "provider": "glm", "prompt": "q2", "whenBusy": "queue",
	})
	resp = ec.nextResponse("q2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start(whenBusy=queue, 2) error = %v", resp.Error.Message)
	}
	var queued2 runQueuedResult
	_ = json.Unmarshal(resp.Result, &queued2)

	// Enqueue emitted chat.queueUpdated snapshots with the full list.
	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != "chat.queueUpdated" {
		t.Fatalf("first event topic = %q, want chat.queueUpdated", ev.Topic)
	}
	var snap chatQueueUpdatedPayload
	decodePayload(t, ev, &snap)
	if snap.ChatID != chatID || len(snap.Items) != 1 || snap.Items[0].Status != "queued" {
		t.Fatalf("first snapshot = %+v, want one queued item for chat %d", snap, chatID)
	}
	ev = ec.nextEvent(5 * time.Second)
	decodePayload(t, ev, &snap)
	if len(snap.Items) != 2 {
		t.Fatalf("second snapshot = %+v, want both queued items", snap)
	}

	// chat.queueList returns the items, newest last.
	sendRequest(t, ws, "ls", "chat.queueList", map[string]any{"chatId": chatID})
	resp = ec.nextResponse("ls", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.queueList error = %v", resp.Error.Message)
	}
	var list chatQueueListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("decode chat.queueList: %v", err)
	}
	if len(list.Items) != 2 || list.Items[0].ID != queued.QueueItemID || list.Items[1].ID != queued2.QueueItemID {
		t.Fatalf("chat.queueList = %+v, want both items oldest-first", list)
	}

	// Cancel q2: works on a queued item, and emits the snapshot.
	sendRequest(t, ws, "c", "chat.queueCancel", map[string]any{"itemId": queued2.QueueItemID})
	resp = ec.nextResponse("c", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("chat.queueCancel(queued) error = %v", resp.Error.Message)
	}
	ev = ec.nextEvent(5 * time.Second)
	decodePayload(t, ev, &snap)
	if len(snap.Items) != 2 || snap.Items[1].Status != "cancelled" {
		t.Fatalf("post-cancel snapshot = %+v, want q2 cancelled", snap)
	}

	// Deliver q1 by stopping the running run, then cancel it again:
	// delivered items are not cancellable.
	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	ec.nextResponse("stop", 5*time.Second)

	// Wait for q1's delivery snapshot (status delivered with a run id).
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev = ec.nextEvent(time.Until(deadline))
		decodePayload(t, ev, &snap)
		if len(snap.Items) == 2 && snap.Items[0].Status == "delivered" && snap.Items[0].RunID != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("q1 never delivered: %+v", snap)
		}
	}

	sendRequest(t, ws, "c2", "chat.queueCancel", map[string]any{"itemId": queued.QueueItemID})
	resp = ec.nextResponse("c2", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("chat.queueCancel on a delivered item: error = nil, want an error")
	}
}

// TestRunStart_AgentProvenanceHeaderOverWire pins AC4 at the wire layer:
// run.start with source=agent and fromTaskId/fromChatId enqueues an item
// whose delivered prompt starts with the [message from task #T, chat #C]
// header; an invalid source is rejected outright.
func TestRunStart_AgentProvenanceHeaderOverWire(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// Bind the chat with a first run so a provider exists, then queue an
	// agent-sourced item on the now-idle chat (it starts immediately with
	// its provenance-stamped prompt).
	sendRequest(t, ws, "a", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "a",
	})
	resp := readEnvelopeFor(t, ws, "a", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start(a) error = %v", resp.Error.Message)
	}
	var started runStartResult
	_ = json.Unmarshal(resp.Result, &started)
	waitRunDone(t, ws, started.RunID)

	// An invalid source is rejected.
	sendRequest(t, ws, "bad", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "x", "source": "skynet",
	})
	resp = readEnvelopeFor(t, ws, "bad", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("run.start(source=skynet) error = nil, want rejected")
	}

	// source=agent without from ids is rejected.
	sendRequest(t, ws, "nofrom", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "x", "source": "agent",
	})
	resp = readEnvelopeFor(t, ws, "nofrom", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("run.start(source=agent, no from ids) error = nil, want rejected")
	}

	// An agent-sourced send is delivered with the header line.
	sendRequest(t, ws, "ag", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "peer escalation",
		"source": "agent", "fromTaskId": 7, "fromChatId": 9,
	})
	resp = readEnvelopeFor(t, ws, "ag", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start(agent) error = %v", resp.Error.Message)
	}
	var agentRun runStartResult
	if err := json.Unmarshal(resp.Result, &agentRun); err != nil {
		t.Fatalf("decode run.start(agent): %v", err)
	}
	waitRunDone(t, ws, agentRun.RunID)

	// The persisted run row carries the header before the prompt.
	var logs runLogsResult
	sendRequest(t, ws, "lg", "run.logs", map[string]any{"runId": agentRun.RunID})
	resp = readEnvelopeFor(t, ws, "lg", 5*time.Second)
	if err := json.Unmarshal(resp.Result, &logs); err != nil {
		t.Fatalf("decode run.logs: %v", err)
	}
	// The agent run's own history can't show its prompt; assert on the
	// store row instead via the runs registry -- simplest is run.list's
	// Prompt field.
	var listed []struct {
		ID     string `json:"ID"`
		Prompt string `json:"Prompt"`
	}
	sendRequest(t, ws, "rl", "run.list", map[string]any{})
	resp = readEnvelopeFor(t, ws, "rl", 5*time.Second)
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		t.Fatalf("decode run.list: %v", err)
	}
	found := false
	for _, r := range listed {
		if r.ID == agentRun.RunID {
			found = true
			want := "[message from task #7, chat #9]\npeer escalation"
			if r.Prompt != want {
				t.Fatalf("delivered prompt = %q, want %q", r.Prompt, want)
			}
		}
	}
	if !found {
		t.Fatal("agent run not in run.list")
	}
}

// waitRunDone polls run.logs until the run reaches a terminal status.
func waitRunDone(t *testing.T, ws *websocket.Conn, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sendRequest(t, ws, "w"+runID[:4], "run.logs", map[string]any{"runId": runID})
		resp := readEnvelopeFor(t, ws, "w"+runID[:4], 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("run.logs(%s): %v", runID, resp.Error.Message)
		}
		var logs runLogsResult
		if err := json.Unmarshal(resp.Result, &logs); err == nil && logs.Status != "running" {
			if logs.Status != "done" {
				t.Fatalf("run %s ended %q (%s)", runID, logs.Status, logs.Err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s still running", runID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
