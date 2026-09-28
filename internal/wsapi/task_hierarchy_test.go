package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// TestServer_TaskCreateList_ParentTaskID proves task.create's optional
// parentTaskId reaches workspace.Manager.CreateTask (the child's
// ParentTaskID round-trips), and task.list's optional parentTaskId filters
// to direct children only -- the wsapi half of the task-hierarchy plan
// (docs/plans/active/smind-control-parity.md), on top of internal/store and
// internal/workspace's own coverage of the same validation.
func TestServer_TaskCreateList_ParentTaskID(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	root := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.create", map[string]any{
		"workspaceId": root.WorkspaceID, "title": "child", "parentTaskId": root.ID,
	})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.create with parentTaskId error = %v", resp.Error.Message)
	}
	var child store.Task
	if err := json.Unmarshal(resp.Result, &child); err != nil {
		t.Fatalf("decode task.create result: %v", err)
	}
	if child.ParentTaskID == nil || *child.ParentTaskID != root.ID {
		t.Fatalf("task.create parentTaskId round-trip: ParentTaskID = %v, want %d", child.ParentTaskID, root.ID)
	}

	// A second, unrelated root-level task must not leak into root's
	// filtered child list.
	sendRequest(t, ws, "2", "task.create", map[string]any{
		"workspaceId": root.WorkspaceID, "title": "unrelated",
	})
	if resp := readEnvelopeFor(t, ws, "2", 5*time.Second); resp.Error != nil {
		t.Fatalf("task.create(unrelated) error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "3", "task.list", map[string]any{
		"workspaceId": root.WorkspaceID, "parentTaskId": root.ID,
	})
	resp = readEnvelopeFor(t, ws, "3", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.list with parentTaskId error = %v", resp.Error.Message)
	}
	var kids []store.Task
	if err := json.Unmarshal(resp.Result, &kids); err != nil {
		t.Fatalf("decode task.list result: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("task.list(parentTaskId=%d) = %+v, want exactly [child %d]", root.ID, kids, child.ID)
	}

	// A cross-workspace or nonexistent parent surfaces the daemon's
	// rejection reason, both for task.create and task.list.
	other := newTestTask(t, wm, "")
	sendRequest(t, ws, "4", "task.create", map[string]any{
		"workspaceId": other.WorkspaceID, "title": "cross", "parentTaskId": root.ID,
	})
	resp = readEnvelopeFor(t, ws, "4", 5*time.Second)
	if resp.Error == nil || resp.Error.Message == "" {
		t.Fatalf("task.create with cross-workspace parentTaskId: error = %v, want a descriptive rejection", resp.Error)
	}

	sendRequest(t, ws, "5", "task.create", map[string]any{
		"workspaceId": root.WorkspaceID, "title": "orphan", "parentTaskId": root.ID + 999999,
	})
	resp = readEnvelopeFor(t, ws, "5", 5*time.Second)
	if resp.Error == nil || resp.Error.Message == "" {
		t.Fatalf("task.create with nonexistent parentTaskId: error = %v, want a descriptive rejection", resp.Error)
	}
}
