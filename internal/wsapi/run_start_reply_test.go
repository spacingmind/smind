package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/workspace"
)

// TestRunStart_ChildEventSourceRejected: child_event is daemon-authored
// only (ADR-0041 B3); a wire caller may not claim it.
func TestRunStart_ChildEventSourceRejected(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "ce", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "x", "source": "child_event",
	})
	resp := readEnvelopeFor(t, ws, "ce", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "invalid source") {
		t.Fatalf("run.start(source=child_event) error = %v, want \"invalid source\"", resp.Error)
	}
}

// TestRunStart_NotifyOnFinishParam: notifyOnFinish=false is accepted and
// the run gets no reply chat; omitted/true on the same agent send does.
func TestRunStart_NotifyOnFinishParam(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	parent := newTestTask(t, wm, "")
	child, err := wm.CreateTask(parent.WorkspaceID, nil, "Child", workspace.WithParentTask(parent.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	pc, err := wm.DefaultChat(parent.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	start := func(id string, extra map[string]any) int64 {
		t.Helper()
		params := map[string]any{
			"taskId": child.ID, "provider": "glm", "prompt": "work",
			"source": "agent", "fromTaskId": parent.ID, "fromChatId": pc.ID,
		}
		for k, v := range extra {
			params[k] = v
		}
		sendRequest(t, ws, id, "run.start", params)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("run.start(%s) error = %v", id, resp.Error.Message)
		}
		var out runStartResult
		if err := json.Unmarshal(resp.Result, &out); err != nil {
			t.Fatalf("decode run.start(%s): %v", id, err)
		}
		waitRunDone(t, ws, out.RunID)
		row, err := db.GetRun(out.RunID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		return row.ReplyChatID
	}

	if got := start("off", map[string]any{"notifyOnFinish": false}); got != 0 {
		t.Fatalf("notifyOnFinish=false: ReplyChatID = %d, want 0", got)
	}
	if got := start("on", nil); got != pc.ID {
		t.Fatalf("notifyOnFinish omitted: ReplyChatID = %d, want %d", got, pc.ID)
	}
}
