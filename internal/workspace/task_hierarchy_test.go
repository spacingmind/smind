package workspace

import (
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// TestManager_CreateTask_WithParent proves WithParentTask both stamps the
// child's ParentTaskID and rejects a nonexistent or cross-workspace
// parent -- the daemon-facing half of what internal/store's own
// CreateTask validation already covers at the lower layer (see
// tasks_parent_test.go), now reachable through the Manager path
// task.create actually uses.
func TestManager_CreateTask_WithParent(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repoA, repoB := newTestRepo(t), newTestRepo(t)

	wsA, err := m.CreateWorkspace(repoA, "A", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace(A) error = %v", err)
	}
	wsB, err := m.CreateWorkspace(repoB, "B", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace(B) error = %v", err)
	}

	root, err := m.CreateTask(wsA.ID, nil, "root")
	if err != nil {
		t.Fatalf("CreateTask(root) error = %v", err)
	}

	t.Run("valid same-workspace parent succeeds", func(t *testing.T) {
		t.Parallel()
		child, err := m.CreateTask(wsA.ID, nil, "child", WithParentTask(root.ID))
		if err != nil {
			t.Fatalf("CreateTask(child) error = %v", err)
		}
		if child.ParentTaskID == nil || *child.ParentTaskID != root.ID {
			t.Fatalf("CreateTask(child).ParentTaskID = %v, want %d", child.ParentTaskID, root.ID)
		}
	})

	t.Run("nonexistent parent rejected", func(t *testing.T) {
		t.Parallel()
		_, err := m.CreateTask(wsA.ID, nil, "orphan", WithParentTask(999999))
		if !errors.Is(err, store.ErrParentTaskNotFound) {
			t.Fatalf("CreateTask() error = %v, want ErrParentTaskNotFound", err)
		}
	})

	t.Run("cross-workspace parent rejected", func(t *testing.T) {
		t.Parallel()
		_, err := m.CreateTask(wsB.ID, nil, "cross", WithParentTask(root.ID))
		if !errors.Is(err, store.ErrParentTaskMismatch) {
			t.Fatalf("CreateTask() error = %v, want ErrParentTaskMismatch", err)
		}
	})
}

// TestManager_CreateTask_DepthLimit pins the O2 depth guard: a root task's
// grandchild (depth 2) is allowed under the default maxDepth (2), but its
// great-grandchild (depth 3) is rejected with the model-readable message
// the plan specifies, before any worktree is created for it.
func TestManager_CreateTask_DepthLimit(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	ws, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	root, err := m.CreateTask(ws.ID, nil, "root")
	if err != nil {
		t.Fatalf("CreateTask(root) error = %v", err)
	}
	child, err := m.CreateTask(ws.ID, nil, "child", WithParentTask(root.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	grandchild, err := m.CreateTask(ws.ID, nil, "grandchild", WithParentTask(child.ID))
	if err != nil {
		t.Fatalf("CreateTask(grandchild) at depth 2 error = %v, want success (default maxDepth is 2)", err)
	}

	_, err = m.CreateTask(ws.ID, nil, "great-grandchild", WithParentTask(grandchild.ID))
	if err == nil {
		t.Fatal("CreateTask(great-grandchild) at depth 3 error = nil, want a depth-limit rejection")
	}
	const wantMsg = "task depth limit reached (2): do this work yourself or ask the user"
	if got := err.Error(); !strings.Contains(got, wantMsg) {
		t.Fatalf("CreateTask(great-grandchild) error = %q, want it to contain %q", got, wantMsg)
	}
}

// TestManager_CreateTask_DepthLimitConfigurable proves SetMaxDepth actually
// changes the enforced limit, not just the default -- e.g. maxDepth=1
// rejects a task's own grandchild.
func TestManager_CreateTask_DepthLimitConfigurable(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	m.SetMaxDepth(1)
	repo := newTestRepo(t)
	ws, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	root, err := m.CreateTask(ws.ID, nil, "root")
	if err != nil {
		t.Fatalf("CreateTask(root) error = %v", err)
	}
	child, err := m.CreateTask(ws.ID, nil, "child", WithParentTask(root.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) at depth 1 error = %v, want success (maxDepth is 1)", err)
	}

	_, err = m.CreateTask(ws.ID, nil, "grandchild", WithParentTask(child.ID))
	if err == nil {
		t.Fatal("CreateTask(grandchild) at depth 2 with maxDepth=1: error = nil, want a depth-limit rejection")
	}
}

// TestManager_ListTasks_WithParentFilter proves WithParentFilter narrows
// ListTasks to a parent's direct children (grandchildren excluded), and
// rejects a nonexistent or cross-workspace parent the same way CreateTask
// does.
func TestManager_ListTasks_WithParentFilter(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repoA, repoB := newTestRepo(t), newTestRepo(t)
	wsA, err := m.CreateWorkspace(repoA, "A", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace(A) error = %v", err)
	}
	wsB, err := m.CreateWorkspace(repoB, "B", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace(B) error = %v", err)
	}

	root, err := m.CreateTask(wsA.ID, nil, "root")
	if err != nil {
		t.Fatalf("CreateTask(root) error = %v", err)
	}
	child, err := m.CreateTask(wsA.ID, nil, "child", WithParentTask(root.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	if _, err := m.CreateTask(wsA.ID, nil, "grandchild", WithParentTask(child.ID)); err != nil {
		t.Fatalf("CreateTask(grandchild) error = %v", err)
	}
	if _, err := m.CreateTask(wsA.ID, nil, "other-root"); err != nil {
		t.Fatalf("CreateTask(other-root) error = %v", err)
	}

	kids, err := m.ListTasks(wsA.ID, WithParentFilter(root.ID))
	if err != nil {
		t.Fatalf("ListTasks(WithParentFilter) error = %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("ListTasks(WithParentFilter(root)) = %+v, want exactly [child %d]", kids, child.ID)
	}

	if _, err := m.ListTasks(wsA.ID, WithParentFilter(999999)); !errors.Is(err, store.ErrParentTaskNotFound) {
		t.Fatalf("ListTasks(nonexistent parent) error = %v, want ErrParentTaskNotFound", err)
	}
	if _, err := m.ListTasks(wsB.ID, WithParentFilter(root.ID)); !errors.Is(err, store.ErrParentTaskMismatch) {
		t.Fatalf("ListTasks(cross-workspace parent) error = %v, want ErrParentTaskMismatch", err)
	}
}
