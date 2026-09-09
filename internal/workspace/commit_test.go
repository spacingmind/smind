package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// newTaskWithRepo is the shared setup for commit-flow tests: a manager, a
// real repo, a workspace, and a task whose worktree has two changed files
// (README.md modified, notes.txt added) sitting unstaged.
func newTaskWithRepo(t *testing.T) (*Manager, store.Task, string) {
	t.Helper()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Commit flow")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	wt := *task.WorktreePath

	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	return m, task, wt
}

func findFile(files []TaskFile, path string) (TaskFile, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return TaskFile{}, false
}

// TestManager_TaskFiles proves the changed-files list reports the right
// paths, statuses, and staged flags -- including the untracked-as-added
// mapping and staged-state observability (ADR/plan decision).
func TestManager_TaskFiles(t *testing.T) {
	t.Parallel()

	t.Run("empty for no changes", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)
		w, _ := m.CreateWorkspace(repo, "W", "hard", nil)
		task, err := m.CreateTask(w.ID, nil, "empty")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		files, err := m.TaskFiles(task.ID)
		if err != nil {
			t.Fatalf("TaskFiles() error = %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("TaskFiles() = %+v, want empty", files)
		}
	})

	t.Run("statuses and staged state", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		runGitT(t, wt, "add", "notes.txt")

		files, err := m.TaskFiles(task.ID)
		if err != nil {
			t.Fatalf("TaskFiles() error = %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("TaskFiles() = %+v, want 2 entries", files)
		}
		readme, ok := findFile(files, "README.md")
		if !ok {
			t.Fatalf("TaskFiles() = %+v, want README.md", files)
		}
		if readme.Status != "modified" || readme.Staged {
			t.Fatalf("README.md entry = %+v, want modified/unstaged", readme)
		}
		notes, ok := findFile(files, "notes.txt")
		if !ok {
			t.Fatalf("TaskFiles() = %+v, want notes.txt", files)
		}
		if notes.Status != "added" || !notes.Staged {
			t.Fatalf("notes.txt entry = %+v, want added/staged", notes)
		}
	})
}

// TestManager_TaskFileDiff proves the per-file diff is a consistent slice
// of the whole-task diff: only the named path's hunks, empty for an
// unchanged path.
func TestManager_TaskFileDiff(t *testing.T) {
	t.Parallel()
	m, task, _ := newTaskWithRepo(t)

	readmeDiff, err := m.TaskFileDiff(task.ID, "README.md")
	if err != nil {
		t.Fatalf("TaskFileDiff(README.md) error = %v", err)
	}
	if !strings.Contains(readmeDiff, "changed") || strings.Contains(readmeDiff, "brand new") {
		t.Fatalf("TaskFileDiff(README.md) = %q, want only README hunks", readmeDiff)
	}

	notesDiff, err := m.TaskFileDiff(task.ID, "notes.txt")
	if err != nil {
		t.Fatalf("TaskFileDiff(notes.txt) error = %v", err)
	}
	if !strings.Contains(notesDiff, "brand new") {
		t.Fatalf("TaskFileDiff(notes.txt) = %q, want the added file's content", notesDiff)
	}

	none, err := m.TaskFileDiff(task.ID, "nope.txt")
	if err != nil {
		t.Fatalf("TaskFileDiff(nope.txt) error = %v", err)
	}
	if none != "" {
		t.Fatalf("TaskFileDiff(nope.txt) = %q, want empty", none)
	}
}

// TestManager_TaskStage proves stage/unstage round-trips against the real
// index: stage flips TaskFiles' flag and `git status`; unstage flips both
// back.
func TestManager_TaskStage(t *testing.T) {
	t.Parallel()
	m, task, wt := newTaskWithRepo(t)

	if err := m.TaskStage(task.ID, "README.md", true); err != nil {
		t.Fatalf("TaskStage(true) error = %v", err)
	}
	status := runGitOutputT(t, wt, "status", "--porcelain")
	if !strings.Contains(status, "M  README.md") {
		t.Fatalf("git status after stage = %q, want README.md fully staged", status)
	}
	files, err := m.TaskFiles(task.ID)
	if err != nil {
		t.Fatalf("TaskFiles() error = %v", err)
	}
	if readme, _ := findFile(files, "README.md"); !readme.Staged {
		t.Fatalf("README.md entry = %+v after stage, want Staged", readme)
	}

	if err := m.TaskStage(task.ID, "README.md", false); err != nil {
		t.Fatalf("TaskStage(false) error = %v", err)
	}
	status = runGitOutputT(t, wt, "status", "--porcelain")
	if !strings.Contains(status, " M README.md") {
		t.Fatalf("git status after unstage = %q, want README.md unstaged", status)
	}
	files, err = m.TaskFiles(task.ID)
	if err != nil {
		t.Fatalf("TaskFiles() error = %v", err)
	}
	if readme, _ := findFile(files, "README.md"); readme.Staged {
		t.Fatalf("README.md entry = %+v after unstage, want unstaged", readme)
	}
}

// TestManager_CommitTask covers the commit primitive: empty-stage clean
// error, human commits recording exactly the staged set, and agent trailer
// format byte-exact.
func TestManager_CommitTask(t *testing.T) {
	t.Parallel()

	t.Run("nothing staged returns ErrNothingStaged, not a stderr leak", func(t *testing.T) {
		t.Parallel()
		m, task, _ := newTaskWithRepo(t)
		_, err := m.CommitTask(task.ID, "msg", "human", "")
		if !errors.Is(err, ErrNothingStaged) {
			t.Fatalf("CommitTask() error = %v, want ErrNothingStaged", err)
		}
	})

	t.Run("commits only the staged files", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		runGitT(t, wt, "add", "README.md")

		result, err := m.CommitTask(task.ID, "only readme", "human", "")
		if err != nil {
			t.Fatalf("CommitTask() error = %v", err)
		}
		if result.Subject != "only readme" {
			t.Fatalf("Subject = %q, want %q", result.Subject, "only readme")
		}
		if result.Files != 1 {
			t.Fatalf("Files = %d, want 1", result.Files)
		}

		// The new commit records only README.md; notes.txt must remain
		// uncommitted in the working tree.
		inCommit := runGitOutputT(t, wt, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
		if strings.TrimSpace(inCommit) != "README.md" {
			t.Fatalf("commit contains %q, want only README.md", inCommit)
		}
		fullSHA := strings.TrimSpace(runGitOutputT(t, wt, "rev-parse", "HEAD"))
		if result.Commit != fullSHA {
			t.Fatalf("Commit = %q, want %q", result.Commit, fullSHA)
		}
	})

	t.Run("agent commit carries byte-exact trailers", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		runGitT(t, wt, "add", "notes.txt")

		result, err := m.CommitTask(task.ID, "agent work", "agent", "glm")
		if err != nil {
			t.Fatalf("CommitTask() error = %v", err)
		}
		body := runGitOutputT(t, wt, "log", "-1", "--format=%B")
		want := "agent work\n\nSmind-Agent: glm\nSmind-Task: " + strconv.FormatInt(task.ID, 10)
		if strings.TrimRight(body, "\n") != want {
			t.Fatalf("commit message = %q, want %q", body, want)
		}
		_ = result
	})

	t.Run("agent commit without provider name is refused", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		runGitT(t, wt, "add", "notes.txt")
		_, err := m.CommitTask(task.ID, "agent work", "agent", "")
		if err == nil || !strings.Contains(err.Error(), "agent (provider) name") {
			t.Fatalf("CommitTask(agent, no provider) error = %v, want a refusal", err)
		}
		// And nothing was committed.
		if out := runGitOutputT(t, wt, "log", "-1", "--format=%s"); strings.TrimSpace(out) == "agent work" {
			t.Fatal("a commit was created despite the refusal")
		}
	})

	t.Run("invalid author is refused", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		runGitT(t, wt, "add", "notes.txt")
		if _, err := m.CommitTask(task.ID, "msg", "robot", ""); err == nil {
			t.Fatal("CommitTask(author=robot) error = nil, want a refusal")
		}
	})
}
