package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestManager_Diff proves Diff reflects everything a task's worktree has
// changed relative to the commit its branch was created from: a real commit
// made after the task started, a staged change on top of that, an unstaged
// change on top of the staged one, and a brand new untracked file -- all in
// one diff, matching the real-worktree behavior this feature's git
// invocation was chosen against (see git.go's taskDiff doc comment and
// docs/plans/active/web-ui-diff-viewer.md's Decisions section).
func TestManager_Diff(t *testing.T) {
	t.Parallel()

	t.Run("no changes returns an empty diff, not an error", func(t *testing.T) {
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

		diff, err := m.Diff(task.ID)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if diff != "" {
			t.Fatalf("Diff() = %q, want empty for a freshly created, unmodified worktree", diff)
		}
	})

	t.Run("reflects a real commit plus staged, unstaged, and untracked changes together", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)
		w, err := m.CreateWorkspace(repo, "W", "hard", nil)
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		task, err := m.CreateTask(w.ID, nil, "Real changes")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		wt := *task.WorktreePath

		// A real commit made in the worktree, ahead of base.
		readme := filepath.Join(wt, "README.md")
		if err := os.WriteFile(readme, []byte("hello\ncommitted line\n"), 0o644); err != nil {
			t.Fatalf("write README (committed): %v", err)
		}
		runGitT(t, wt, "add", "README.md")
		runGitT(t, wt, "commit", "-m", "a real commit ahead of base")

		// A staged change on top of that commit.
		if err := os.WriteFile(readme, []byte("hello\ncommitted line\nstaged line\n"), 0o644); err != nil {
			t.Fatalf("write README (staged): %v", err)
		}
		runGitT(t, wt, "add", "README.md")

		// An unstaged change on top of the staged one.
		if err := os.WriteFile(readme, []byte("hello\ncommitted line\nstaged line\nunstaged line\n"), 0o644); err != nil {
			t.Fatalf("write README (unstaged): %v", err)
		}

		// A brand new untracked file.
		newFile := filepath.Join(wt, "new.txt")
		if err := os.WriteFile(newFile, []byte("brand new untracked content\n"), 0o644); err != nil {
			t.Fatalf("write new.txt: %v", err)
		}

		wantStatus := gitStatusPorcelain(t, wt)

		diff, err := m.Diff(task.ID)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}

		for _, want := range []string{
			"README.md",
			"committed line",
			"staged line",
			"unstaged line",
			"new.txt",
			"brand new untracked content",
		} {
			if !strings.Contains(diff, want) {
				t.Fatalf("Diff() = %q, want it to contain %q", diff, want)
			}
		}

		// The base's own unmodified content (the initial commit's line, and
		// the "hello" line still present in every version of README.md
		// above) must not show up as removed -- Diff should show only what
		// actually changed since base.
		if strings.Contains(diff, "-hello") {
			t.Fatalf("Diff() = %q, unexpectedly shows the unmodified first line as removed", diff)
		}

		// Diff must never mutate the worktree's real index or working tree:
		// git status immediately after must be identical to right before.
		gotStatus := gitStatusPorcelain(t, wt)
		if gotStatus != wantStatus {
			t.Fatalf("git status after Diff() = %q, want unchanged from before Diff() (%q)", gotStatus, wantStatus)
		}
	})
}

func gitStatusPorcelain(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain: %v: %s", err, out)
	}
	return string(out)
}
