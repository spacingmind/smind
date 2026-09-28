package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// newTestTaskInWorkspace creates a bare task in wsID's workspace, for tests
// that need a workspace-valid anchor task without caring about its fields.
func newTestTaskInWorkspace(t *testing.T, s *Store, wsID int64, title string) Task {
	t.Helper()
	task, err := s.CreateTask(Task{WorkspaceID: wsID, Title: title, Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask(%q) error = %v", title, err)
	}
	return task
}

func TestStore_CreateTaskParentValidation(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	wsA, err := s.CreateWorkspace(Workspace{Path: "/repo-a", Title: "repo-a", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	wsB, err := s.CreateWorkspace(Workspace{Path: "/repo-b", Title: "repo-b", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	parentInA := newTestTaskInWorkspace(t, s, wsA.ID, "parent")
	parentInB := newTestTaskInWorkspace(t, s, wsB.ID, "parent-b")

	t.Run("nonexistent parent rejected", func(t *testing.T) {
		t.Parallel()
		bad := int64(9999)
		_, err := s.CreateTask(Task{WorkspaceID: wsA.ID, ParentTaskID: &bad, Title: "child", Status: "created"})
		if err == nil {
			t.Fatal("CreateTask() with nonexistent parent error = nil, want error")
		}
		if !errors.Is(err, ErrParentTaskNotFound) {
			t.Errorf("CreateTask() error = %v, want ErrParentTaskNotFound", err)
		}
	})

	t.Run("cross-workspace parent rejected", func(t *testing.T) {
		t.Parallel()
		_, err := s.CreateTask(Task{WorkspaceID: wsA.ID, ParentTaskID: &parentInB.ID, Title: "child", Status: "created"})
		if err == nil {
			t.Fatal("CreateTask() with cross-workspace parent error = nil, want error")
		}
		if !errors.Is(err, ErrParentTaskMismatch) {
			t.Errorf("CreateTask() error = %v, want ErrParentTaskMismatch", err)
		}
	})

	t.Run("valid same-workspace parent succeeds", func(t *testing.T) {
		t.Parallel()
		child, err := s.CreateTask(Task{WorkspaceID: wsA.ID, ParentTaskID: &parentInA.ID, Title: "child", Status: "created"})
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		if child.ParentTaskID == nil || *child.ParentTaskID != parentInA.ID {
			t.Fatalf("CreateTask() ParentTaskID = %v, want %d", child.ParentTaskID, parentInA.ID)
		}

		got, err := s.GetTask(child.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if got.ParentTaskID == nil || *got.ParentTaskID != parentInA.ID {
			t.Errorf("GetTask() ParentTaskID = %v, want %d", got.ParentTaskID, parentInA.ID)
		}
	})
}

func TestStore_ListTasksByParent(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	root := newTestTaskInWorkspace(t, s, ws.ID, "root")
	child, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &root.ID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}
	grandchild, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &child.ID, Title: "grandchild", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() grandchild error = %v", err)
	}
	// A root-level task and a task under a different parent must not leak
	// into root's child list.
	newTestTaskInWorkspace(t, s, ws.ID, "other-root")

	list, err := s.ListTasksByParent(root.ID)
	if err != nil {
		t.Fatalf("ListTasksByParent(%d) error = %v", root.ID, err)
	}
	if len(list) != 1 || list[0].ID != child.ID {
		t.Fatalf("ListTasksByParent(%d) = %+v, want exactly [task %d] (direct children only, no grandchild, no other roots)", root.ID, list, child.ID)
	}
	if len(list) == 1 {
		if got := list[0].ParentTaskID; got == nil || *got != root.ID {
			t.Errorf("listed child ParentTaskID = %v, want %d", got, root.ID)
		}
	}

	// The filter walks one level: grandchild shows up under child, not root.
	underChild, err := s.ListTasksByParent(child.ID)
	if err != nil {
		t.Fatalf("ListTasksByParent(%d) error = %v", child.ID, err)
	}
	if len(underChild) != 1 || underChild[0].ID != grandchild.ID {
		t.Fatalf("ListTasksByParent(%d) = %+v, want exactly [task %d]", child.ID, underChild, grandchild.ID)
	}

	// A parent with no children lists empty.
	none, err := s.ListTasksByParent(grandchild.ID)
	if err != nil {
		t.Fatalf("ListTasksByParent(%d) error = %v", grandchild.ID, err)
	}
	if len(none) != 0 {
		t.Fatalf("ListTasksByParent(%d) = %+v, want empty", grandchild.ID, none)
	}
}

// TestStore_TaskDepth proves TaskDepth walks the parent_task_id chain
// correctly: a root task is depth 0, and each hop down adds 1.
func TestStore_TaskDepth(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	root := newTestTaskInWorkspace(t, s, ws.ID, "root")
	child, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &root.ID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}
	grandchild, err := s.CreateTask(Task{WorkspaceID: ws.ID, ParentTaskID: &child.ID, Title: "grandchild", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() grandchild error = %v", err)
	}

	for _, tt := range []struct {
		name string
		id   int64
		want int
	}{
		{"root", root.ID, 0},
		{"child", child.ID, 1},
		{"grandchild", grandchild.ID, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := s.TaskDepth(tt.id)
			if err != nil {
				t.Fatalf("TaskDepth(%d) error = %v", tt.id, err)
			}
			if got != tt.want {
				t.Errorf("TaskDepth(%d) = %d, want %d", tt.id, got, tt.want)
			}
		})
	}
}

// TestStore_MigrateAddsParentTaskID proves a database created before the
// hierarchy shipped (tasks table without parent_task_id -- seedPreChatsDB's
// shape) gets the column from the migration path, with existing tasks
// reading back as roots (nil parent) and new parent/child writes working.
func TestStore_MigrateAddsParentTaskID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pre-hierarchy.db")
	seedPreChatsDB(t, path) // its tasks table predates parent_task_id

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on pre-hierarchy database error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	var name string
	if err := s.db.QueryRow(`SELECT name FROM pragma_table_info('tasks') WHERE name = 'parent_task_id'`).Scan(&name); err != nil {
		t.Fatalf("tasks.parent_task_id column not found after migration: %v", err)
	}

	pre, err := s.GetTask(1)
	if err != nil {
		t.Fatalf("GetTask(1) error = %v", err)
	}
	if pre.ParentTaskID != nil {
		t.Fatalf("pre-existing task ParentTaskID = %v, want nil (root)", pre.ParentTaskID)
	}

	parentID := pre.ID
	child, err := s.CreateTask(Task{WorkspaceID: pre.WorkspaceID, ParentTaskID: &parentID, Title: "child", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() child after migration error = %v", err)
	}
	kids, err := s.ListTasksByParent(parentID)
	if err != nil {
		t.Fatalf("ListTasksByParent() after migration error = %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("ListTasksByParent(%d) = %+v, want exactly [task %d]", parentID, kids, child.ID)
	}
}
