package store

import (
	"database/sql"
	"errors"
	"testing"
)

// TestStore_DeleteTaskDetachesChildren pins the no-automatic-cascade
// decision (docs/plans/active/smind-control-parity.md): deleting a parent
// task succeeds (no raw FOREIGN KEY error from tasks.parent_task_id) and
// leaves each child in place as a root task with ParentTaskID NULL --
// children are detached, never deleted with the parent.
func TestStore_DeleteTaskDetachesChildren(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	parent := createTestTaskWithRunData(t, s, ws.ID, nil, "parent")
	child, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &parent.ID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}

	if err := s.DeleteTask(parent.ID); err != nil {
		t.Fatalf("DeleteTask(parent with children) error = %v", err)
	}

	if _, err := s.GetTask(parent.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask(parent) after delete error = %v, want sql.ErrNoRows", err)
	}
	got, err := s.GetTask(child.ID)
	if err != nil {
		t.Fatalf("GetTask(child) after parent delete error = %v, want the child kept", err)
	}
	if got.ParentTaskID != nil {
		t.Fatalf("child ParentTaskID after parent delete = %d, want nil (detached to root)", *got.ParentTaskID)
	}

	// The detached child can still be deleted itself, now that nothing
	// references it.
	if err := s.DeleteTask(child.ID); err != nil {
		t.Fatalf("DeleteTask(detached child) error = %v", err)
	}
}

// TestStore_DeleteTaskRollsBack proves DeleteTask is one transaction: a
// failure at the very last step (the tasks DELETE, blocked by a test
// trigger that raises on it) rolls back the run_events/runs/chats/
// terminal_sessions deletes that already ran inside the transaction, so a
// half-deleted task never escapes.
func TestStore_DeleteTaskRollsBack(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task := createTestTaskWithRunData(t, s, ws.ID, nil, "t1")

	// Same-package tests can reach s.db: a temporary BEFORE DELETE trigger
	// on tasks makes the final statement of DeleteTask's transaction fail
	// deterministically, standing in for any mid-cascade failure.
	if _, err := s.db.Exec(`CREATE TRIGGER block_task_delete BEFORE DELETE ON tasks BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := s.DeleteTask(task.ID); err == nil {
		t.Fatal("DeleteTask() with blocked final delete: error = nil, want the trigger's abort")
	}

	if _, err := s.GetTask(task.ID); err != nil {
		t.Fatalf("GetTask() after rolled-back delete error = %v, want the task intact", err)
	}
	if _, err := s.GetRun("run-t1"); err != nil {
		t.Fatalf("GetRun() after rolled-back delete error = %v, want the run intact", err)
	}
	events, err := s.ListRunEvents("run-t1")
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListRunEvents() after rolled-back delete = %d events, want 1 (intact)", len(events))
	}
	if _, err := s.GetTerminalSession("term-t1"); err != nil {
		t.Fatalf("GetTerminalSession() after rolled-back delete error = %v, want the session intact", err)
	}
}

// TestStore_DeleteSpaceHierarchy proves a space holding a root -> child ->
// grandchild tree deletes cleanly once DeleteTask detaches children --
// previously the middle-of-tree DELETE tripped the parent_task_id foreign
// key -- and removes all three tasks.
func TestStore_DeleteSpaceHierarchy(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	sp, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "feature-x", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	root := createTestTaskWithRunData(t, s, ws.ID, &sp.ID, "root")
	child, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &sp.ID, ParentTaskID: &root.ID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}
	grandchild, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &sp.ID, ParentTaskID: &child.ID, Title: "grandchild", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() grandchild error = %v", err)
	}

	if err := s.DeleteSpace(sp.ID); err != nil {
		t.Fatalf("DeleteSpace() over a task tree error = %v", err)
	}

	for _, id := range []int64{root.ID, child.ID, grandchild.ID} {
		if _, err := s.GetTask(id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetTask(%d) after space delete error = %v, want sql.ErrNoRows", id, err)
		}
	}
}

// TestStore_DeleteWorkspaceHierarchy is TestStore_DeleteSpaceHierarchy for
// the workspace cascade, mixing a spaced tree with an ungrouped child.
func TestStore_DeleteWorkspaceHierarchy(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	sp, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "space-a", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	root := createTestTaskWithRunData(t, s, ws.ID, &sp.ID, "root")
	child, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &sp.ID, ParentTaskID: &root.ID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}
	grandchild, err := s.CreateTask(Task{WorkspaceID: ws.ID, SpaceID: &sp.ID, ParentTaskID: &child.ID, Title: "grandchild", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() grandchild error = %v", err)
	}
	ungroupedRoot, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "ungrouped-root", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() ungrouped-root error = %v", err)
	}
	if _, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &ungroupedRoot.ID, Title: "ungrouped-child", Status: "created"}); err != nil {
		t.Fatalf("CreateTask() ungrouped-child error = %v", err)
	}

	if err := s.DeleteWorkspace(ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace() over task trees error = %v", err)
	}

	if _, err := s.GetWorkspace(ws.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetWorkspace() after delete error = %v, want sql.ErrNoRows", err)
	}
	for _, id := range []int64{root.ID, child.ID, grandchild.ID, ungroupedRoot.ID} {
		if _, err := s.GetTask(id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetTask(%d) after workspace delete error = %v, want sql.ErrNoRows", id, err)
		}
	}
	// The ungrouped child (whose id we never captured) must be gone too:
	// nothing may remain in the workspace.
	remaining, err := s.ListTasksByWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("ListTasksByWorkspace() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("ListTasksByWorkspace() after delete = %d tasks, want 0", len(remaining))
	}
}
