package wsapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
)

func TestTaskArchive_RefusesActiveRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	if env := readEnvelopeFor(t, ws, "1", 5*time.Second); env.Event != "chunk" {
		t.Fatalf("first message = %+v, want a chunk (the run is live)", env)
	}

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d runs, want 1", len(summaries))
	}
	runID := summaries[0].ID

	sendRequest(t, ws, "arch", "task.archive", map[string]any{"id": task.ID})
	resp := readEnvelopeFor(t, ws, "arch", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("task.archive under a running run: error = nil, want a refusal naming the run")
	}
	if !strings.Contains(resp.Error.Message, runID) {
		t.Fatalf("error = %q, want it to name the running run %q", resp.Error.Message, runID)
	}

	// The task and its worktree must be untouched.
	got, err := wm.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.ArchivedAt != nil {
		t.Fatal("task was archived despite the refusal")
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": runID})
	if r := readEnvelopeFor(t, ws, "stop", 5*time.Second); r.Error != nil {
		t.Fatalf("run.stop error = %v", r.Error.Message)
	}
	readEnvelopeFor(t, ws, "1", 5*time.Second) // prompt terminal
}

func TestTaskArchive_RefusesQueuedRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// A queued item with no run in flight (a real busy-queue would start
	// delivering the moment the running run finished, so seed the queue
	// directly -- the exact persisted state a live task has between runs).
	chats, err := wm.ListChats(task.ID, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v, want the default chat", chats, err)
	}
	if _, err := db.EnqueueChatPrompt(chats[0].ID, "waiting prompt", "{}", "human", 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}

	sendRequest(t, ws, "arch", "task.archive", map[string]any{"id": task.ID})
	resp := readEnvelopeFor(t, ws, "arch", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("task.archive with a queued prompt: error = nil, want a refusal")
	}
	if !strings.Contains(resp.Error.Message, "queued") {
		t.Fatalf("error = %q, want it to mention the queued prompt", resp.Error.Message)
	}
	if got, err := wm.GetTask(task.ID); err != nil || got.ArchivedAt != nil {
		t.Fatalf("task was archived despite the queued refusal: %+v %v", got, err)
	}
}

func TestTaskArchive_AllowedAfterStop(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	readEnvelopeFor(t, ws, "1", 5*time.Second)

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": runID})
	if r := readEnvelopeFor(t, ws, "stop", 5*time.Second); r.Error != nil {
		t.Fatalf("run.stop error = %v", r.Error.Message)
	}
	readEnvelopeFor(t, ws, "1", 5*time.Second)

	sendRequest(t, ws, "arch", "task.archive", map[string]any{"id": task.ID})
	resp := readEnvelopeFor(t, ws, "arch", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.archive after run.stop error = %v, want success", resp.Error.Message)
	}
	var archived struct {
		ArchivedAt *time.Time `json:"ArchivedAt"`
	}
	if err := json.Unmarshal(resp.Result, &archived); err != nil {
		t.Fatalf("decode task.archive result: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("task.archive result has no ArchivedAt")
	}
}

// TestTaskArchive_FailsClosedOnStoreError proves the archive guard fails
// closed: with the store unreadable (closed DB), task.archive errors
// instead of answering "no active runs" -- and the worktree survives.
func TestTaskArchive_FailsClosedOnStoreError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// Break the store read the guard performs: closing db makes
	// ListChatsByTask (and everything else) fail.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	sendRequest(t, ws, "arch", "task.archive", map[string]any{"id": task.ID})
	resp := readEnvelopeFor(t, ws, "arch", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("task.archive with an unreadable store: error = nil, want a fail-closed refusal")
	}
	if !strings.Contains(resp.Error.Message, "task.archive") {
		t.Fatalf("error = %q, want a task.archive-labeled refusal", resp.Error.Message)
	}
	if _, err := os.Stat(*task.WorktreePath); err != nil {
		t.Fatalf("worktree dir gone after a failed-closed archive, want it kept: %v", err)
	}
}
