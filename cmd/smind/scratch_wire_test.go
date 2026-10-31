package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// newScratchStore opens a temp store with one workspace.
func newScratchStore(t *testing.T) (*store.Store, store.Workspace) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	w, err := db.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	return db, w
}

// scratchCachesOff is config.Default with agents.sharedCaches off, so the
// tests never resolve (or exec) real shared caches.
func scratchCachesOff() config.Config {
	off := false
	cfg := config.Default()
	cfg.Agents.SharedCaches = &off
	return cfg
}

// TestScratchWiring_LiveFailSafe: a task that exists un-archived is live, an
// archived or unknown one is not, and a broken store read keeps everything
// (fail-safe, never delete).
func TestScratchWiring_LiveFailSafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	db, w := newScratchStore(t)
	mkTask := func(title string) store.Task {
		t.Helper()
		task, err := db.CreateTask(store.Task{WorkspaceID: w.ID, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	live := mkTask("live")
	done := mkTask("done")
	if _, err := db.ArchiveTask(done.ID); err != nil {
		t.Fatal(err)
	}
	_, sw := wireScratch(scratchCachesOff(), db, workspace.New(db))
	if !sw.live(live.ID) {
		t.Errorf("live(%d) = false, want true", live.ID)
	}
	if sw.live(done.ID) {
		t.Errorf("live(%d) = true for an archived task, want false", done.ID)
	}
	if sw.live(9999) {
		t.Error("live(unknown) = true, want false")
	}
	// A closed store errors out of TaskLifecycle: everything stays.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if !sw.live(live.ID) || !sw.live(done.ID) {
		t.Error("live() = false on a store error; a broken read must keep every dir")
	}
}

// TestScratchWiring_CleanerRemovesDir: the cleaner wireScratch registers
// removes exactly the closed task's dir and leaves every other entry.
func TestScratchWiring_CleanerRemovesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	db, w := newScratchStore(t)
	closed, err := db.CreateTask(store.Task{WorkspaceID: w.ID, Title: "closed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ArchiveTask(closed.ID); err != nil {
		t.Fatal(err)
	}
	opts, sw := wireScratch(scratchCachesOff(), db, workspace.New(db))
	if len(opts) == 0 {
		t.Fatal("wireScratch returned no runner options")
	}
	if got, want := sw.mgr.Root(), filepath.Join(home, "scratch"); got != want {
		t.Fatalf("scratch root = %q, want %q", got, want)
	}
	seed := func(id int64) string {
		t.Helper()
		p := filepath.Join(sw.mgr.Dir(id), "f.txt")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, 3), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	closedFile := seed(closed.ID)
	other := seed(4242)
	sw.cleaner(closed.ID)
	if _, err := os.Stat(closedFile); !os.IsNotExist(err) {
		t.Fatalf("the archived task's scratch dir survived the cleaner: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another task's scratch dir was removed: %v", err)
	}
}
