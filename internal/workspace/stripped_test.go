package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// The stripped-mode tests: git the daemon starts on its own (a context
// without the human mark, the default everywhere) must run none of the
// programs gitsafetest.NewStrippedRepo armed the repository with, and the
// same call with WithHumanInitiated must keep them (the Human's hooks and
// drivers are the point of human mode). The fixture calls t.Setenv, so no
// test in this file may call t.Parallel.

// writeRepoFile writes content as a file of the armed repository.
func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// assertNoMarkers fails the test when any armed program has run.
func assertNoMarkers(t *testing.T, r *gitsafetest.StrippedRepo) {
	t.Helper()
	if m := r.Markers(); len(m) != 0 {
		t.Fatalf("an armed program ran: %v", m)
	}
}

// The automatic checkpoint (status, add -A, commit) is daemon-initiated:
// no hook, no clean filter, and the commit is still made.
func Test_checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)
	writeRepoFile(t, r.Dir, "a.flt", "a.flt changed\n")
	writeRepoFile(t, r.Dir, "a.fl2", "a.fl2 changed\n")

	if err := gitWorktreeCheckpoint(r.Dir); err != nil {
		t.Fatalf("gitWorktreeCheckpoint() error = %v", err)
	}
	assertNoMarkers(t, r)

	out, err := r.Run("log", "--format=%s", "-n", "1")
	if err != nil {
		t.Fatalf("read the checkpoint commit: %v", err)
	}
	if strings.TrimSpace(string(out)) != checkpointCommitMessage {
		t.Fatalf("git log -n 1 = %q, want the checkpoint message %q", out, checkpointCommitMessage)
	}
}

// The throwaway-index diff snapshot (add -A into the temp index, then the
// cached diff) is a read the daemon makes: the clean and process filters
// must not run, and the diff must still show the change.
func Test_diff_snapshot_runs_no_filter(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")
	writeRepoFile(t, r.Dir, "a.flt", "a.flt changed\n")

	_, cleanup, err := snapshotIndex(r.Dir)
	if err != nil {
		t.Fatalf("snapshotIndex() error = %v", err)
	}
	cleanup()

	diff, err := taskDiff(r.Dir, base)
	if err != nil {
		t.Fatalf("taskDiff() error = %v", err)
	}
	if !strings.Contains(diff, "a.flt") {
		t.Fatalf("taskDiff() = %q, want the a.flt change", diff)
	}
	assertNoMarkers(t, r)
}

// Creating a task's worktree (worktree add) is daemon-initiated: the
// post-checkout hook must not run.
func Test_worktree_add_runs_no_post_checkout_hook(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")

	if err := gitWorktreeAdd(r.Dir, wt, "wt-branch"); err != nil {
		t.Fatalf("gitWorktreeAdd() error = %v", err)
	}
	assertNoMarkers(t, r)
}

// execGit is the choke point that picks the mode from the context: with no
// mark the pre-commit hook does not run (and signing is off, so the commit
// succeeds); with the human mark the repository's pre-commit hook runs.
func Test_execgit_picks_the_mode_from_the_context(t *testing.T) {
	r := gitsafetest.NewStrippedRepo(t)

	if _, _, err := execGit(context.Background(), r.Dir, nil, "commit", "--allow-empty", "-m", "x"); err != nil {
		t.Fatalf("execGit(daemon) error = %v", err)
	}
	assertNoMarkers(t, r)

	r.ClearMarkers()
	// The armed repository sets commit.gpgsign=true with a failing signing
	// program, so this commit fails AFTER the hook has run: the marker is
	// what the human mode is judged on.
	_, _, _ = execGit(WithHumanInitiated(context.Background()), r.Dir, nil, "commit", "--allow-empty", "-m", "x")
	if !slices.Contains(r.Markers(), "hook.ran") {
		t.Fatalf("markers = %v, want hook.ran from the pre-commit hook", r.Markers())
	}
}
