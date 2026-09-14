package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestManager_TaskSearchIndex proves the quick-open index (Item 18) is
// git's own notion of "the files in this worktree" -- committed, staged,
// and untracked-but-not-ignored paths all included, gitignored ones
// excluded -- without smind parsing .gitignore itself.
func TestManager_TaskSearchIndex(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Search index")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	wt := *task.WorktreePath

	// A brand new, untracked-but-not-ignored file.
	if err := os.MkdirAll(filepath.Join(wt, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write src/main.go: %v", err)
	}
	// A gitignored file and the .gitignore that excludes it -- the
	// .gitignore itself is untracked-but-not-ignored, so it should still
	// appear.
	if err := os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "ignored.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("write ignored.txt: %v", err)
	}

	paths, err := m.TaskSearchIndex(task.ID)
	if err != nil {
		t.Fatalf("TaskSearchIndex() error = %v", err)
	}
	sort.Strings(paths)

	want := []string{".gitignore", "README.md", "src/main.go"}
	if len(paths) != len(want) {
		t.Fatalf("TaskSearchIndex() = %v, want %v", paths, want)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Fatalf("TaskSearchIndex()[%d] = %q, want %q", i, paths[i], p)
		}
	}
}

// TestManager_TaskSearchIndex_NoChanges proves a freshly created task (no
// untracked files at all yet) still reports its committed files, not an
// error or an empty list.
func TestManager_TaskSearchIndex_NoChanges(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "No changes")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	paths, err := m.TaskSearchIndex(task.ID)
	if err != nil {
		t.Fatalf("TaskSearchIndex() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != "README.md" {
		t.Fatalf("TaskSearchIndex() = %v, want [README.md]", paths)
	}
}

func TestManager_TaskSearchIndex_UnknownTask(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)
	if _, err := m.TaskSearchIndex(999); err == nil {
		t.Fatal("TaskSearchIndex(999) error = nil, want an error for an unknown task")
	}
}
