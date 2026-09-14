package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func findStat(stats []TaskStat, taskID int64) (TaskStat, bool) {
	for _, s := range stats {
		if s.TaskID == taskID {
			return s, true
		}
	}
	return TaskStat{}, false
}

// TestManager_TaskStats covers the sidebar's per-row git signal: the
// counts match the diff task.diff renders (untracked files included), a
// task with no worktree is absent rather than zeroed, and the list keeps
// ListTasks' id order despite being computed by a worker pool.
func TestManager_TaskStats(t *testing.T) {
	t.Parallel()

	t.Run("counts the same diff task.diff renders, untracked files included", func(t *testing.T) {
		t.Parallel()
		m, task, _ := newTaskWithRepo(t)

		stats, err := m.TaskStats(task.WorkspaceID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		got, ok := findStat(stats, task.ID)
		if !ok {
			t.Fatalf("TaskStats() = %+v, want an entry for task %d", stats, task.ID)
		}
		// README.md modified (1 insertion) + notes.txt created untracked
		// (1 insertion). The untracked file is the point: a plain
		// `git diff --shortstat` would report only README.md.
		if got.FilesChanged != 2 {
			t.Fatalf("FilesChanged = %d, want 2 (stat = %+v)", got.FilesChanged, got)
		}
		if got.Insertions != 2 {
			t.Fatalf("Insertions = %d, want 2 (stat = %+v)", got.Insertions, got)
		}
		if got.Branch != *task.Branch {
			t.Fatalf("Branch = %q, want %q", got.Branch, *task.Branch)
		}
	})

	t.Run("a deletion is counted as a deletion, not an insertion", func(t *testing.T) {
		t.Parallel()
		m, task, wt := newTaskWithRepo(t)
		if err := os.Remove(filepath.Join(wt, "notes.txt")); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove notes.txt: %v", err)
		}
		if err := os.Remove(filepath.Join(wt, "README.md")); err != nil {
			t.Fatalf("remove README.md: %v", err)
		}

		stats, err := m.TaskStats(task.WorkspaceID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		got, ok := findStat(stats, task.ID)
		if !ok {
			t.Fatalf("TaskStats() = %+v, want an entry for task %d", stats, task.ID)
		}
		if got.Deletions == 0 || got.Insertions != 0 {
			t.Fatalf("stat = %+v, want deletions only", got)
		}
	})

	t.Run("a task with no changes reports zeroes, not an error", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)
		w, _ := m.CreateWorkspace(repo, "W", "hard", nil)
		task, err := m.CreateTask(w.ID, nil, "clean")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}

		stats, err := m.TaskStats(w.ID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		got, ok := findStat(stats, task.ID)
		if !ok {
			t.Fatalf("TaskStats() = %+v, want an entry for task %d", stats, task.ID)
		}
		if got.FilesChanged != 0 || got.Insertions != 0 || got.Deletions != 0 {
			t.Fatalf("stat = %+v, want all zero", got)
		}
	})

	t.Run("a broken worktree is omitted and does not fail the other tasks", func(t *testing.T) {
		t.Parallel()
		m, good, _ := newTaskWithRepo(t)
		broken, err := m.CreateTask(good.WorkspaceID, nil, "broken")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		// Pull the worktree out from under the daemon, as a user deleting
		// a directory would.
		if err := os.RemoveAll(*broken.WorktreePath); err != nil {
			t.Fatalf("remove worktree: %v", err)
		}

		stats, err := m.TaskStats(good.WorkspaceID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		if _, ok := findStat(stats, good.ID); !ok {
			t.Fatalf("TaskStats() = %+v, want the healthy task still reported", stats)
		}
		if _, ok := findStat(stats, broken.ID); ok {
			t.Fatalf("TaskStats() = %+v, want the broken task omitted, not zeroed", stats)
		}
	})

	t.Run("keeps ListTasks' id order across the worker pool", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)
		w, _ := m.CreateWorkspace(repo, "W", "hard", nil)
		// More tasks than taskStatWorkers, so completion order genuinely
		// differs from submission order.
		var want []int64
		for i := range taskStatWorkers * 2 {
			task, err := m.CreateTask(w.ID, nil, "task "+string(rune('a'+i)))
			if err != nil {
				t.Fatalf("CreateTask() error = %v", err)
			}
			want = append(want, task.ID)
		}

		stats, err := m.TaskStats(w.ID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		if len(stats) != len(want) {
			t.Fatalf("TaskStats() returned %d entries, want %d", len(stats), len(want))
		}
		for i, id := range want {
			if stats[i].TaskID != id {
				t.Fatalf("stats[%d].TaskID = %d, want %d (full = %+v)", i, stats[i].TaskID, id, stats)
			}
		}
	})

	t.Run("an archived task leaves the list, like it leaves ListTasks", func(t *testing.T) {
		t.Parallel()
		m, task, _ := newTaskWithRepo(t)
		if _, err := m.ArchiveTask(task.ID); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}

		stats, err := m.TaskStats(task.WorkspaceID)
		if err != nil {
			t.Fatalf("TaskStats() error = %v", err)
		}
		if _, ok := findStat(stats, task.ID); ok {
			t.Fatalf("TaskStats() = %+v, want the archived task absent", stats)
		}
	})
}
