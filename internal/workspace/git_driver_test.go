package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// The git diff and status calls the daemon makes in a task's worktree run
// with gitsafe's hardening, so an agent who poisoned the repository's
// shared git state (a textconv driver in .gitattributes, diff.external and
// core.fsmonitor in .git/config) cannot make the daemon execute a program
// of its choosing. Each test below runs one call site for real in
// gitsafetest's armed repository and asserts two things: the repo-config
// programs did not run (no marker file), and the output is still the
// correct, raw diff content (AC3).
//
// These tests use NewDriverRepo, which calls t.Setenv: they must never
// call t.Parallel.

// assertNoDriversRan fails the test when any armed program (textconv,
// external diff, fsmonitor) has left its marker file in r.
func assertNoDriversRan(t *testing.T, r *gitsafetest.Repo) {
	t.Helper()
	if m := r.Markers(); len(m) != 0 {
		t.Fatalf("a repo-config program ran: %v", m)
	}
}

func Test_taskdiff_does_not_run_textconv_or_external_diff(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")

	diff, err := taskDiff(r.Dir, base)
	if err != nil {
		t.Fatalf("taskDiff() error = %v", err)
	}
	// Raw content, not the textconv driver's output and not the external
	// diff program's placeholder line.
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("taskDiff() = %q, want the raw a.txt hunk (-one/+two)", diff)
	}
	if !strings.Contains(diff, "Binary files") && !strings.Contains(diff, "GIT binary patch") {
		t.Fatalf("taskDiff() = %q, want a binary-diff line for bin.dat", diff)
	}
	assertNoDriversRan(t, r)
}

func Test_task_name_status_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")

	files, err := taskChangedFiles(r.Dir, base)
	if err != nil {
		t.Fatalf("taskChangedFiles() error = %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if got["a.txt"] != "modified" || got["bin.dat"] != "modified" {
		t.Fatalf("taskChangedFiles() = %v, want a.txt and bin.dat modified", files)
	}
	assertNoDriversRan(t, r)
}

func Test_task_file_diff_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")

	diff, err := taskFileDiff(r.Dir, base, "a.txt")
	if err != nil {
		t.Fatalf("taskFileDiff() error = %v", err)
	}
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("taskFileDiff() = %q, want the raw a.txt hunk (-one/+two)", diff)
	}
	assertNoDriversRan(t, r)
}

func Test_staged_names_do_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	r.Git(t, "add", "--", "a.txt") // staging is setup; it may run fsmonitor
	r.ClearMarkers()

	staged, err := stagedPaths(r.Dir)
	if err != nil {
		t.Fatalf("stagedPaths() error = %v", err)
	}
	if !staged["a.txt"] {
		t.Fatalf("stagedPaths() = %v, want a.txt staged", staged)
	}
	assertNoDriversRan(t, r)

	// The same staged-names read as a `git diff --cached --name-only`
	// (gitTaskCommit), one of the plan's audited diff call sites.
	res, err := gitTaskCommit(r.Dir, "staged names")
	if err != nil {
		t.Fatalf("gitTaskCommit() error = %v", err)
	}
	if res.Files != 1 {
		t.Fatalf("gitTaskCommit() counted %d files, want 1 (a.txt)", res.Files)
	}
	assertNoDriversRan(t, r)
}

func Test_task_shortstat_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")

	files, insertions, _, err := taskDiffStat(r.Dir, base)
	if err != nil {
		t.Fatalf("taskDiffStat() error = %v", err)
	}
	if files != 2 || insertions == 0 {
		t.Fatalf("taskDiffStat() = %d files, %d insertions; want 2 files, non-zero insertions", files, insertions)
	}
	assertNoDriversRan(t, r)
}

func Test_archive_numstat_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)

	files, _, _, err := uncommittedStat(r.Dir)
	if err != nil {
		t.Fatalf("uncommittedStat() error = %v", err)
	}
	if files != 2 {
		t.Fatalf("uncommittedStat() = %d files, want 2 (a.txt and bin.dat)", files)
	}
	assertNoDriversRan(t, r)
}

func Test_pr_shortstat_does_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(r.Dir, "W", "gitsafe", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "PR stat")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	wt := *task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	runGitT(t, wt, "add", "--", "a.txt")
	runGitT(t, wt, "commit", "-m", "change a.txt")
	r.ClearMarkers()

	info, err := m.PRPreview(context.Background(), task.ID, "")
	if err != nil {
		t.Fatalf("PRPreview() error = %v", err)
	}
	if info.Diff.Files != 1 || info.Diff.Insertions != 1 {
		t.Fatalf("PRPreview() diff = %+v, want 1 file, 1 insertion (a.txt)", info.Diff)
	}
	assertNoDriversRan(t, r)
}

func Test_sync_conflict_names_do_not_run_drivers(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)

	files, truncated := unmergedFiles(context.Background(), r.Dir)
	if len(files) != 0 || truncated {
		t.Fatalf("unmergedFiles() = %v (truncated %t), want none", files, truncated)
	}
	assertNoDriversRan(t, r)
}

func Test_workspace_status_does_not_run_fsmonitor(t *testing.T) {
	r := gitsafetest.NewDriverRepo(t)
	base := r.Git(t, "rev-parse", "HEAD")

	out, err := runGitOutput(r.Dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("status --porcelain error = %v", err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("status --porcelain = %q, want a.txt listed", out)
	}
	if _, _, _, err := taskDiffStat(r.Dir, base); err != nil {
		t.Fatalf("taskDiffStat() error = %v", err)
	}
	assertNoDriversRan(t, r)
}
