package gitstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestActionsPush(t *testing.T) {
	branch := &Branch{Name: "smind/task-x"}
	tests := []struct {
		name string
		g    TaskGit
		c    ActionCtx
		code string // "" = enabled
		msg  string
	}{
		{"no worktree", TaskGit{}, ActionCtx{}, "task_no_worktree", "Push isn't available because this task has no worktree. Start a new task from this branch to push changes."},
		{"no origin", TaskGit{Branch: branch, Origin: &Origin{}}, ActionCtx{NoOrigin: true}, "forge_no_remote", "Push isn't available because this workspace has no `origin` remote, so there's nowhere to push. Add one with `git remote add origin <url>`."},
		{"origin unknown", TaskGit{Branch: branch}, ActionCtx{}, "status_unknown", "Checking this branch's remote state…"},
		{"behind origin", TaskGit{Branch: branch, Origin: &Origin{Published: true, Ahead: ip(1), Behind: ip(2)}}, ActionCtx{}, "non_fast_forward", "Push isn't available because origin has newer changes on this branch. Bring those changes in first (pull in the worktree, or ask the agent)."},
		{"published, nothing ahead", TaskGit{Branch: branch, Origin: &Origin{Published: true, Ahead: ip(0), Behind: ip(0)}}, ActionCtx{}, "nothing_to_push", "Nothing to push."},
		{"unpublished, no commits", TaskGit{Branch: branch, Origin: &Origin{}, Commits: &Commits{SinceFork: ip(0)}}, ActionCtx{}, "nothing_to_push", "Nothing to push. Commit your changes first."},
		{"unpublished with commits", TaskGit{Branch: branch, Origin: &Origin{}, Commits: &Commits{SinceFork: ip(2)}}, ActionCtx{}, "", ""},
		{"unpublished, count unknown", TaskGit{Branch: branch, Origin: &Origin{}}, ActionCtx{}, "", ""},
		{"rebased and diverged", TaskGit{Branch: branch, Origin: &Origin{Published: true, Ahead: ip(2), Behind: ip(2)}}, ActionCtx{RebasedDiverged: true}, "non_fast_forward", "This branch was rebased and has diverged from origin; smind can't force-push. Push it manually (`git push --force-with-lease`) or reset to origin."},
		{"rebased marker without divergence", TaskGit{Branch: branch, Origin: &Origin{Published: true, Ahead: ip(2), Behind: ip(0)}}, ActionCtx{RebasedDiverged: true}, "", ""},
		{"fork pull request", TaskGit{Branch: branch, Origin: &Origin{}, Commits: &Commits{SinceFork: ip(1)}}, ActionCtx{ForkPR: true}, "fork_pr_readonly", "This task was started from a pull request on a fork, so smind can't push to it. Push it yourself or open a new pull request from your own branch."},
		{"published and ahead", TaskGit{Branch: branch, Origin: &Origin{Published: true, Ahead: ip(3), Behind: ip(0)}}, ActionCtx{}, "", ""},
		{"local path origin does not block", TaskGit{Branch: branch, Origin: &Origin{}, Commits: &Commits{SinceFork: ip(1)}}, ActionCtx{OriginNotForge: true}, "", ""},
		{"run active does not block", TaskGit{Branch: branch, Origin: &Origin{}, Commits: &Commits{SinceFork: ip(1)}}, ActionCtx{RunActive: true}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkAction(t, Actions(tc.g, tc.c)["push"], tc.code, tc.msg)
		})
	}
}

func TestActionsUpdateFromBase(t *testing.T) {
	branch := &Branch{Name: "smind/task-x"}
	known := &Base{Ref: "develop", Sha: "x", Tip: "y", Known: true}
	behind := &Commits{BehindBase: ip(3)}
	clean := &Worktree{}
	tests := []struct {
		name string
		g    TaskGit
		c    ActionCtx
		code string
		msg  string
	}{
		{"no worktree", TaskGit{Base: known}, ActionCtx{}, "task_no_worktree", "Update from base isn't available because this task has no worktree. Start a new task from this branch to keep it current."},
		{"base unknown", TaskGit{Branch: branch, Base: &Base{Ref: "develop"}}, ActionCtx{}, "base_unknown", "Update from base isn't available because this task's base commit is unknown. Set the base branch first."},
		{"base not found", TaskGit{Branch: branch, Base: &Base{Ref: "develop", Sha: "x", Known: true}}, ActionCtx{}, "base_not_found", "Update from base isn't available because `develop` wasn't found. Pick an existing base branch."},
		{"run active", TaskGit{Branch: branch, Base: known, Worktree: clean, Commits: behind}, ActionCtx{RunActive: true}, "run_active", "Update from base isn't available yet because an agent is still working in this task. Wait for the run to finish, or stop it."},
		{"mid merge", TaskGit{Branch: branch, Base: known, Worktree: &Worktree{Dirty: true, Unstaged: 1}, Commits: behind}, ActionCtx{MidOperation: "merge"}, "merge_in_progress", "Update from base isn't available because this worktree is in the middle of a merge. Finish it, or undo it with `git merge --abort` in the worktree."},
		{"mid rebase", TaskGit{Branch: branch, Base: known, Worktree: clean, Commits: behind}, ActionCtx{MidOperation: "rebase"}, "merge_in_progress", "Update from base isn't available because this worktree is in the middle of a rebase. Finish it, or undo it with `git rebase --abort` in the worktree."},
		{"dirty", TaskGit{Branch: branch, Base: known, Worktree: &Worktree{Dirty: true, Untracked: 1}, Commits: behind}, ActionCtx{}, "worktree_dirty", "Update from base isn't available because this worktree has uncommitted changes. Commit or stash them first."},
		{"up to date", TaskGit{Branch: branch, Base: known, Worktree: clean, Commits: &Commits{BehindBase: ip(0)}}, ActionCtx{}, "up_to_date", "Already up to date with `develop`."},
		{"behind base", TaskGit{Branch: branch, Base: known, Worktree: clean, Commits: behind}, ActionCtx{}, "", ""},
		{"counts unknown", TaskGit{Branch: branch, Base: known}, ActionCtx{}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkAction(t, Actions(tc.g, tc.c)["updateFromBase"], tc.code, tc.msg)
		})
	}
}

func checkAction(t *testing.T, a Action, code, msg string) {
	t.Helper()
	if code == "" {
		if !a.Enabled || a.Code != "" || a.Reason != "" {
			t.Fatalf("action = %+v, want enabled", a)
		}
		return
	}
	if a.Enabled || a.Code != code || a.Reason != msg {
		t.Fatalf("action = %+v, want disabled %q %q", a, code, msg)
	}
}

func TestComputeMidOperation(t *testing.T) {
	f := newFixture(t)
	if got := midOperation(context.Background(), DefaultRunner, f.worktree); got != "" {
		t.Fatalf("clean worktree: %q", got)
	}
	// A conflicting merge leaves MERGE_HEAD.
	commit(t, f.worktree, "a.txt", "task\n", "task a")
	f.advanceBase(t, "a.txt", "base\n")
	cmd := func(args ...string) { _ = runIgnore(f.worktree, args...) }
	cmd("merge", "--no-edit", "origin/develop")
	if got := midOperation(context.Background(), DefaultRunner, f.worktree); got != "merge" {
		t.Fatalf("mid merge: %q", got)
	}
	g := Compute(context.Background(), DefaultRunner, f.input())
	if a := g.Actions["updateFromBase"]; a.Enabled || a.Code != "merge_in_progress" {
		t.Fatalf("updateFromBase = %+v", a)
	}
	git(t, f.worktree, "merge", "--abort")
	// A stopped rebase leaves rebase-merge.
	cmd("rebase", "origin/develop")
	if got := midOperation(context.Background(), DefaultRunner, f.worktree); got != "rebase" {
		t.Fatalf("mid rebase: %q", got)
	}
	git(t, f.worktree, "rebase", "--abort")
	if got := midOperation(context.Background(), DefaultRunner, f.worktree); got != "" {
		t.Fatalf("after abort: %q", got)
	}
	// A fake git dir path that is relative resolves against the worktree.
	if _, err := os.Stat(filepath.Join(f.repo, ".git")); err != nil {
		t.Fatal(err)
	}
}

func runIgnore(dir string, args ...string) error {
	_, err := DefaultRunner(context.Background(), dir, args...)
	return err
}
