package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// writeDirtyFile writes an uncommitted modification into wt, giving
// gitWorktreeCheckpoint something real to commit.
func writeDirtyFile(t *testing.T, wt, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write README (uncommitted): %v", err)
	}
}

// corruptWorktreeGit overwrites worktreePath's ".git" file (the pointer a
// real `git worktree add` leaves behind, referencing the real repo's
// internal worktree metadata) with garbage, so any git command run with cwd
// = worktreePath fails -- while the directory itself still exists and still
// passes dirExists. This is how these tests force gitWorktreeCheckpoint to
// fail without deleting anything, to exercise the "checkpoint failure
// aborts before any DB row is touched" guarantee.
func corruptWorktreeGit(t *testing.T, worktreePath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("not a real gitdir pointer\n"), 0o644); err != nil {
		t.Fatalf("corrupt worktree .git: %v", err)
	}
}

func TestManager_DeleteTask(t *testing.T) {
	t.Parallel()

	t.Run("checkpoints uncommitted work then removes worktree and row", func(t *testing.T) {
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
		writeDirtyFile(t, wt, "hello\nuncommitted line\n")

		summary, err := m.DeleteTask(task.ID)
		if err != nil {
			t.Fatalf("DeleteTask() error = %v", err)
		}
		if summary != (DeleteSummary{TasksRemoved: 1}) {
			t.Fatalf("DeleteTask() summary = %+v, want {TasksRemoved: 1}", summary)
		}

		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Fatalf("worktree dir %q still exists after delete: err = %v", wt, err)
		}
		if _, err := m.GetTask(task.ID); err == nil {
			t.Fatal("GetTask() after delete error = nil, want not-found error")
		}

		// The branch must outlive the worktree and carry the checkpoint --
		// the same guarantee ArchiveTask's own test asserts.
		if tipAfter := gitRevParse(t, repo, branch); tipAfter == tipBefore {
			t.Fatalf("branch %q did not move after deleting a dirty task (tip %q)", branch, tipAfter)
		}
		subject := strings.TrimSpace(runGitOutputT(t, repo, "log", "-1", "--format=%s", branch))
		if subject != checkpointCommitMessage {
			t.Fatalf("branch tip subject = %q, want %q", subject, checkpointCommitMessage)
		}
		if got := runGitOutputT(t, repo, "show", branch+":README.md"); !strings.Contains(got, "uncommitted line") {
			t.Fatalf("checkpoint commit's README.md = %q, want it to contain the uncommitted change", got)
		}
	})

	t.Run("checkpoint failure aborts before any DB row is touched", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Doomed checkpoint")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		wt := *task.WorktreePath
		writeDirtyFile(t, wt, "hello\nuncommitted line\n")
		corruptWorktreeGit(t, wt)

		if _, err := m.DeleteTask(task.ID); err == nil {
			t.Fatal("DeleteTask() error = nil, want the checkpoint failure to surface")
		}

		// Both the task row and its worktree directory must still exist --
		// removal must never run once the checkpoint step failed.
		if _, err := m.GetTask(task.ID); err != nil {
			t.Fatalf("GetTask() after failed delete error = %v, want the task to still exist", err)
		}
		if info, err := os.Stat(wt); err != nil || !info.IsDir() {
			t.Fatalf("worktree dir %q missing after failed delete: err = %v", wt, err)
		}
	})

	t.Run("already-archived task deletes cleanly, skipped at the git step", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Archive then delete")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		if _, err := m.ArchiveTask(task.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}

		summary, err := m.DeleteTask(task.ID)
		if err != nil {
			t.Fatalf("DeleteTask() on already-archived task error = %v, want nil", err)
		}
		if summary.TasksRemoved != 1 {
			t.Fatalf("DeleteTask() summary = %+v, want TasksRemoved 1", summary)
		}
		if _, err := m.GetTask(task.ID); err == nil {
			t.Fatal("GetTask() after delete error = nil, want not-found error")
		}
	})

	t.Run("missing task is a clear not-found error", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		if _, err := m.DeleteTask(999); err == nil {
			t.Fatal("DeleteTask(999) error = nil, want not-found error")
		}
	})
}

func TestManager_DeleteSpace(t *testing.T) {
	t.Parallel()

	t.Run("checkpoints every task in the space, not just the first", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		sp, err := m.CreateSpace(w.ID, "feature-x", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spID := sp.ID
		task1, err := m.CreateTask(w.ID, &spID, "Task 1")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		task2, err := m.CreateTask(w.ID, &spID, "Task 2")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		tip1Before := gitRevParse(t, repo, *task1.Branch)
		tip2Before := gitRevParse(t, repo, *task2.Branch)
		writeDirtyFile(t, *task1.WorktreePath, "hello\ntask 1 change\n")
		writeDirtyFile(t, *task2.WorktreePath, "hello\ntask 2 change\n")

		summary, err := m.DeleteSpace(sp.ID)
		if err != nil {
			t.Fatalf("DeleteSpace() error = %v", err)
		}
		if summary != (DeleteSummary{TasksRemoved: 2, SpacesRemoved: 1}) {
			t.Fatalf("DeleteSpace() summary = %+v, want {TasksRemoved: 2, SpacesRemoved: 1}", summary)
		}

		if tip1After := gitRevParse(t, repo, *task1.Branch); tip1After == tip1Before {
			t.Fatalf("task 1's branch did not move -- it was not checkpointed")
		}
		if tip2After := gitRevParse(t, repo, *task2.Branch); tip2After == tip2Before {
			t.Fatalf("task 2's branch did not move -- it was not checkpointed")
		}

		if _, err := m.GetSpace(sp.ID); err == nil {
			t.Fatal("GetSpace() after delete error = nil, want not-found error")
		}
		if _, err := m.GetTask(task1.ID); err == nil {
			t.Fatal("GetTask(task1) after delete error = nil, want not-found error")
		}
		if _, err := m.GetTask(task2.ID); err == nil {
			t.Fatal("GetTask(task2) after delete error = nil, want not-found error")
		}
	})

	t.Run("a checkpoint failure on any task leaves the database untouched", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		sp, err := m.CreateSpace(w.ID, "feature-x", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spID := sp.ID
		ok, err := m.CreateTask(w.ID, &spID, "Fine")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		doomed, err := m.CreateTask(w.ID, &spID, "Doomed")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		corruptWorktreeGit(t, *doomed.WorktreePath)

		if _, err := m.DeleteSpace(sp.ID); err == nil {
			t.Fatal("DeleteSpace() error = nil, want the checkpoint failure to surface")
		}

		if _, err := m.GetSpace(sp.ID); err != nil {
			t.Fatalf("GetSpace() after failed delete error = %v, want the space to still exist", err)
		}
		if _, err := m.GetTask(ok.ID); err != nil {
			t.Fatalf("GetTask(ok) after failed delete error = %v, want it to still exist", err)
		}
		if _, err := m.GetTask(doomed.ID); err != nil {
			t.Fatalf("GetTask(doomed) after failed delete error = %v, want it to still exist", err)
		}
	})

	t.Run("an already-archived task in the cascade is skipped at the git step but its row is removed", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		sp, err := m.CreateSpace(w.ID, "feature-x", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spID := sp.ID
		archived, err := m.CreateTask(w.ID, &spID, "Archived already")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		if _, err := m.ArchiveTask(archived.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		live, err := m.CreateTask(w.ID, &spID, "Still live")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		summary, err := m.DeleteSpace(sp.ID)
		if err != nil {
			t.Fatalf("DeleteSpace() error = %v, want nil (no error from the missing worktree)", err)
		}
		if summary.TasksRemoved != 2 {
			t.Fatalf("DeleteSpace() summary = %+v, want TasksRemoved 2", summary)
		}
		if _, err := m.GetTask(archived.ID); err == nil {
			t.Fatal("GetTask(archived) after delete error = nil, want not-found error")
		}
		if _, err := m.GetTask(live.ID); err == nil {
			t.Fatal("GetTask(live) after delete error = nil, want not-found error")
		}
	})

	t.Run("missing space is a clear not-found error", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		if _, err := m.DeleteSpace(999); err == nil {
			t.Fatal("DeleteSpace(999) error = nil, want not-found error")
		}
	})
}

func TestManager_DeleteWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("checkpoints every task across every space plus ungrouped tasks", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		spA, err := m.CreateSpace(w.ID, "space-a", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spAID := spA.ID
		spB, err := m.CreateSpace(w.ID, "space-b", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spBID := spB.ID

		taskA, err := m.CreateTask(w.ID, &spAID, "Task A")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		taskB, err := m.CreateTask(w.ID, &spBID, "Task B")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		ungrouped, err := m.CreateTask(w.ID, nil, "Ungrouped")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		for i, task := range []store.Task{taskA, taskB, ungrouped} {
			writeDirtyFile(t, *task.WorktreePath, "hello\nchange "+string(rune('A'+i))+"\n")
		}
		tipsBefore := map[int64]string{
			taskA.ID:     gitRevParse(t, repo, *taskA.Branch),
			taskB.ID:     gitRevParse(t, repo, *taskB.Branch),
			ungrouped.ID: gitRevParse(t, repo, *ungrouped.Branch),
		}

		summary, err := m.DeleteWorkspace(w.ID)
		if err != nil {
			t.Fatalf("DeleteWorkspace() error = %v", err)
		}
		if summary != (DeleteSummary{TasksRemoved: 3, SpacesRemoved: 2}) {
			t.Fatalf("DeleteWorkspace() summary = %+v, want {TasksRemoved: 3, SpacesRemoved: 2}", summary)
		}

		for _, task := range []store.Task{taskA, taskB, ungrouped} {
			if tipAfter := gitRevParse(t, repo, *task.Branch); tipAfter == tipsBefore[task.ID] {
				t.Fatalf("task %d's branch did not move -- it was not checkpointed", task.ID)
			}
		}

		if _, err := m.GetWorkspace(w.ID); err == nil {
			t.Fatal("GetWorkspace() after delete error = nil, want not-found error")
		}
		for _, id := range []int64{spA.ID, spB.ID} {
			if _, err := m.GetSpace(id); err == nil {
				t.Fatalf("GetSpace(%d) after delete error = nil, want not-found error", id)
			}
		}
		for _, id := range []int64{taskA.ID, taskB.ID, ungrouped.ID} {
			if _, err := m.GetTask(id); err == nil {
				t.Fatalf("GetTask(%d) after delete error = nil, want not-found error", id)
			}
		}
	})

	t.Run("a checkpoint failure anywhere in the cascade leaves the database untouched", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		sp, err := m.CreateSpace(w.ID, "space-a", "{}")
		if err != nil {
			t.Fatalf("CreateSpace() error = %v", err)
		}
		spID := sp.ID
		ok, err := m.CreateTask(w.ID, &spID, "Fine")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		doomed, err := m.CreateTask(w.ID, nil, "Doomed ungrouped")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		corruptWorktreeGit(t, *doomed.WorktreePath)

		if _, err := m.DeleteWorkspace(w.ID); err == nil {
			t.Fatal("DeleteWorkspace() error = nil, want the checkpoint failure to surface")
		}

		if _, err := m.GetWorkspace(w.ID); err != nil {
			t.Fatalf("GetWorkspace() after failed delete error = %v, want it to still exist", err)
		}
		if _, err := m.GetSpace(sp.ID); err != nil {
			t.Fatalf("GetSpace() after failed delete error = %v, want it to still exist", err)
		}
		if _, err := m.GetTask(ok.ID); err != nil {
			t.Fatalf("GetTask(ok) after failed delete error = %v, want it to still exist", err)
		}
		if _, err := m.GetTask(doomed.ID); err != nil {
			t.Fatalf("GetTask(doomed) after failed delete error = %v, want it to still exist", err)
		}
	})

	t.Run("missing workspace is a clear not-found error", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		if _, err := m.DeleteWorkspace(999); err == nil {
			t.Fatal("DeleteWorkspace(999) error = nil, want not-found error")
		}
	})
}
