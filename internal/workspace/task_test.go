package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestManager_ListTasks(t *testing.T) {
	t.Parallel()

	t.Run("excludes archived tasks", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		live, err := m.CreateTask(w.ID, nil, "Live")
		if err != nil {
			t.Fatalf("CreateTask(Live) error = %v", err)
		}
		doomed, err := m.CreateTask(w.ID, nil, "Doomed")
		if err != nil {
			t.Fatalf("CreateTask(Doomed) error = %v", err)
		}
		if _, err := m.ArchiveTask(doomed.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}

		tasks, err := m.ListTasks(w.ID)
		if err != nil {
			t.Fatalf("ListTasks() error = %v", err)
		}
		if len(tasks) != 1 || tasks[0].ID != live.ID {
			t.Fatalf("ListTasks() = %+v, want only the non-archived task %d", tasks, live.ID)
		}
	})
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

	t.Run("checkpoints uncommitted and untracked work onto the task branch", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Dirty work")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		wt, branch := *task.WorktreePath, *task.Branch
		tipBefore := gitRevParse(t, repo, branch)

		// An uncommitted modification to a tracked file...
		readme := filepath.Join(wt, "README.md")
		if err := os.WriteFile(readme, []byte("hello\nuncommitted line\n"), 0o644); err != nil {
			t.Fatalf("write README (uncommitted): %v", err)
		}
		// ...and a brand new untracked file.
		notes := filepath.Join(wt, "notes.txt")
		if err := os.WriteFile(notes, []byte("untracked content\n"), 0o644); err != nil {
			t.Fatalf("write notes.txt: %v", err)
		}

		archived, err := m.ArchiveTask(task.ID)
		if err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if archived.Status != "archived" {
			t.Fatalf("ArchiveTask() status = %q, want \"archived\"", archived.Status)
		}
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Fatalf("worktree dir %q still exists after archive: err = %v", wt, err)
		}

		// The branch must outlive the worktree and carry both changes: its
		// tip moved, is the machine-generated checkpoint commit, and its
		// tree contains the uncommitted modification and the untracked
		// file.
		if tipAfter := gitRevParse(t, repo, branch); tipAfter == tipBefore {
			t.Fatalf("branch %q did not move after archiving a dirty worktree (tip %q)", branch, tipAfter)
		}
		subject := strings.TrimSpace(runGitOutputT(t, repo, "log", "-1", "--format=%s", branch))
		if subject != checkpointCommitMessage {
			t.Fatalf("branch tip subject = %q, want %q", subject, checkpointCommitMessage)
		}
		if got := runGitOutputT(t, repo, "show", branch+":README.md"); !strings.Contains(got, "uncommitted line") {
			t.Fatalf("checkpoint commit's README.md = %q, want it to contain the uncommitted change", got)
		}
		if got, want := runGitOutputT(t, repo, "show", branch+":notes.txt"), "untracked content\n"; got != want {
			t.Fatalf("checkpoint commit's notes.txt = %q, want %q", got, want)
		}
	})

	t.Run("clean worktree archives without a checkpoint commit", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Clean work")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		branch := *task.Branch
		tipBefore := gitRevParse(t, repo, branch)

		if _, err := m.ArchiveTask(task.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if tipAfter := gitRevParse(t, repo, branch); tipAfter != tipBefore {
			t.Fatalf("branch %q moved after archiving a clean worktree: %q -> %q", branch, tipBefore, tipAfter)
		}
	})

	t.Run("worktree with only committed changes archives without a checkpoint commit", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Committed work")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		wt, branch := *task.WorktreePath, *task.Branch

		// Real committed work, but nothing uncommitted: the archive must
		// not add an empty checkpoint commit on top of it.
		readme := filepath.Join(wt, "README.md")
		if err := os.WriteFile(readme, []byte("hello\ncommitted work\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
		runGitT(t, wt, "add", "README.md")
		runGitT(t, wt, "commit", "-m", "real committed work")
		tipBefore := gitRevParse(t, repo, branch)

		if _, err := m.ArchiveTask(task.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		if tipAfter := gitRevParse(t, repo, branch); tipAfter != tipBefore {
			t.Fatalf("branch %q moved after archiving a fully committed worktree: %q -> %q", branch, tipBefore, tipAfter)
		}
		if got := runGitOutputT(t, repo, "show", branch+":README.md"); !strings.Contains(got, "committed work") {
			t.Fatalf("branch's README.md = %q, want the pre-archive commit's content", got)
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

// gitRevParse resolves a single rev to its full commit hash, failing the
// test if the rev does not resolve.
func gitRevParse(t *testing.T, dir, rev string) string {
	t.Helper()
	return strings.TrimSpace(runGitOutputT(t, dir, "rev-parse", rev))
}
