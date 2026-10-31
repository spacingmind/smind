package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/scratch"
)

// newScratchManager returns a workspace Manager whose scratch cleaner
// removes dirs under a temp root via internal/scratch.
func newScratchManager(t *testing.T) (*Manager, *scratch.Manager) {
	t.Helper()
	m := newTestManager(t)
	sm := scratch.New(t.TempDir())
	m.SetScratchCleaner(func(taskID int64) { _, _ = sm.Remove(taskID) })
	return m, sm
}

// writeScratchFile creates <root>/<id>/<name> so removal has something to
// delete; it stands in for the daemon's Ensure-then-agent-writes flow.
func writeScratchFile(t *testing.T, sm *scratch.Manager, id int64, name string) {
	t.Helper()
	dir := sm.Dir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Join(dir, name), err)
	}
}

func TestArchive_RemovesScratch(t *testing.T) {
	t.Parallel()
	m, sm := newScratchManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Archived task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	other, err := m.CreateTask(w.ID, nil, "Live task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	writeScratchFile(t, sm, task.ID, "f.txt")
	writeScratchFile(t, sm, other.ID, "g.txt")

	if _, err := m.ArchiveTask(task.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	if _, err := os.Stat(sm.Dir(task.ID)); !os.IsNotExist(err) {
		t.Fatalf("scratch dir of archived task %d still exists: err = %v", task.ID, err)
	}
	if _, err := os.Stat(sm.Dir(other.ID)); err != nil {
		t.Fatalf("scratch dir of live task %d gone: err = %v", other.ID, err)
	}
}

func TestDelete_RemovesScratch(t *testing.T) {
	t.Parallel()
	m, sm := newScratchManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Deleted task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	other, err := m.CreateTask(w.ID, nil, "Live task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	writeScratchFile(t, sm, task.ID, "f.txt")
	writeScratchFile(t, sm, other.ID, "g.txt")

	if _, err := m.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if _, err := os.Stat(sm.Dir(task.ID)); !os.IsNotExist(err) {
		t.Fatalf("scratch dir of deleted task %d still exists: err = %v", task.ID, err)
	}
	if _, err := os.Stat(sm.Dir(other.ID)); err != nil {
		t.Fatalf("scratch dir of live task %d gone: err = %v", other.ID, err)
	}
}

func TestScratchCleaner_NotCalledOnFailedArchive(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)

	called := false
	m.SetScratchCleaner(func(taskID int64) { called = true })

	if _, err := m.ArchiveTask(999); err == nil {
		t.Fatal("ArchiveTask(999) error = nil, want not-found error")
	}
	if called {
		t.Fatal("scratch cleaner called although the archive failed")
	}
}
