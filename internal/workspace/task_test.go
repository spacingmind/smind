package workspace

import (
	"os"
	"os/exec"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// TestMain points config.Dir() (via SMIND_HOME) at a scratch directory for
// the whole package, so CreateTask's worktrees land under a throwaway path
// instead of the real ~/.spacingmind.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-workspace-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	os.Setenv("SMIND_HOME", dir)
	os.Exit(m.Run())
}

func TestManager_CreateTask(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	task, err := m.CreateTask(w.ID, nil, "Fix the bug")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != "created" {
		t.Fatalf("CreateTask() status = %q, want \"created\"", task.Status)
	}
	if task.WorktreePath == nil || task.Branch == nil {
		t.Fatalf("CreateTask() worktree_path/branch not populated: %+v", task)
	}

	info, err := os.Stat(*task.WorktreePath)
	if err != nil || !info.IsDir() {
		t.Fatalf("worktree dir %q does not exist: %v", *task.WorktreePath, err)
	}

	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = *task.WorktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse in worktree failed: %v: %s", err, out)
	}

	got, err := m.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.ID != task.ID {
		t.Fatalf("GetTask() = %+v, want id %d", got, task.ID)
	}

	tasks, err := m.ListTasks(w.ID)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("ListTasks() = %+v, want single task %d", tasks, task.ID)
	}
}

func TestManager_RunTask(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Run me")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	running, err := m.RunTask(task.ID)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if running.Status != "running" {
		t.Fatalf("RunTask() status = %q, want \"running\"", running.Status)
	}

	if _, err := m.RunTask(task.ID); err == nil {
		t.Fatalf("RunTask() second call error = nil, want error rejecting double-run")
	}
}

func TestManager_ArchiveTask(t *testing.T) {
	t.Parallel()

	t.Run("removes worktree", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Archive me")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		worktreePath := *task.WorktreePath

		archived, err := m.ArchiveTask(task.ID)
		if err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if archived.Status != "archived" {
			t.Fatalf("ArchiveTask() status = %q, want \"archived\"", archived.Status)
		}
		if archived.ArchivedAt == nil {
			t.Fatalf("ArchiveTask() ArchivedAt not set")
		}
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Fatalf("worktree dir %q still exists after archive: err = %v", worktreePath, err)
		}
	})

	t.Run("task with no worktree_path archives cleanly", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.store.CreateTask(store.Task{
			WorkspaceID: w.ID,
			Title:       "No worktree",
			Status:      "created",
		})
		if err != nil {
			t.Fatalf("store.CreateTask() error = %v", err)
		}

		archived, err := m.ArchiveTask(task.ID)
		if err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if archived.Status != "archived" {
			t.Fatalf("ArchiveTask() status = %q, want \"archived\"", archived.Status)
		}
	})

	t.Run("safe when worktree already externally deleted", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Delete me externally")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		if err := os.RemoveAll(*task.WorktreePath); err != nil {
			t.Fatalf("os.RemoveAll(worktree) error = %v", err)
		}

		archived, err := m.ArchiveTask(task.ID)
		if err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if archived.Status != "archived" {
			t.Fatalf("ArchiveTask() status = %q, want \"archived\"", archived.Status)
		}
	})
}
