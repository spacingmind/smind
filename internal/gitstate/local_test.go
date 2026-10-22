package gitstate

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestGitStatus_DirtyCounts(t *testing.T) {
	f := newFixture(t)
	// staged: new file added; unstaged: README modified; untracked x2.
	write(t, f.worktree, "staged.txt", "s\n")
	git(t, f.worktree, "add", "staged.txt")
	write(t, f.worktree, "README.md", "changed\n")
	write(t, f.worktree, "u1.txt", "1\n")
	write(t, f.worktree, "dir/u2.txt", "2\n")

	g := Compute(context.Background(), DefaultRunner, f.input())
	want := Worktree{Dirty: true, Staged: 1, Unstaged: 1, Untracked: 2}
	if g.Worktree == nil || *g.Worktree != want {
		t.Fatalf("worktree = %+v, want %+v", g.Worktree, want)
	}

	clean := newFixture(t)
	g = Compute(context.Background(), DefaultRunner, clean.input())
	if g.Worktree == nil || g.Worktree.Dirty {
		t.Fatalf("clean worktree = %+v, want dirty=false", g.Worktree)
	}
}

func TestGitStatus_AheadBehindBase(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	commit(t, f.worktree, "b.txt", "b\n", "task 2")
	f.advanceBase(t, "base1.txt", "1\n")
	f.advanceBase(t, "base2.txt", "2\n")
	f.advanceBase(t, "base3.txt", "3\n")

	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Commits == nil || g.Commits.AheadOfBase == nil || g.Commits.BehindBase == nil || g.Commits.SinceFork == nil {
		t.Fatalf("commits = %+v, want all three present", g.Commits)
	}
	if *g.Commits.AheadOfBase != 2 || *g.Commits.BehindBase != 3 || *g.Commits.SinceFork != 2 {
		t.Fatalf("ahead/behind/sinceFork = %d/%d/%d, want 2/3/2",
			*g.Commits.AheadOfBase, *g.Commits.BehindBase, *g.Commits.SinceFork)
	}
	if g.Base == nil || !g.Base.Known || g.Base.Sha != f.forkSha || g.Base.Tip == "" || g.Base.Tip == f.forkSha {
		t.Fatalf("base = %+v, want known, sha=fork, tip=moved origin/develop", g.Base)
	}
	if g.Conflict == nil || g.Conflict.Against != "origin/develop" {
		t.Fatalf("conflict = %+v, want against origin/develop", g.Conflict)
	}
}

func TestGitStatus_LocalBaseWhenNoOrigin(t *testing.T) {
	f := newFixture(t)
	git(t, f.repo, "remote", "remove", "origin")
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Commits == nil || g.Commits.AheadOfBase == nil || *g.Commits.AheadOfBase != 1 || *g.Commits.BehindBase != 0 {
		t.Fatalf("commits = %+v, want ahead 1 behind 0 against the local base", g.Commits)
	}
}

func TestGitStatus_OriginUnpublished(t *testing.T) {
	f := newFixture(t)
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Origin == nil || g.Origin.Published || g.Origin.Upstream != "" || g.Origin.Ahead != nil || g.Origin.Behind != nil {
		t.Fatalf("origin = %+v, want unpublished with no counts", g.Origin)
	}
}

func TestGitStatus_OriginAheadBehind(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	git(t, f.worktree, "push", "origin", "smind/task-x")
	commit(t, f.worktree, "b.txt", "b\n", "task 2")
	// A second clone pushes two commits to the branch on origin.
	other := t.TempDir()
	git(t, other, "clone", f.bare, ".")
	git(t, other, "config", "user.email", "o@example.com")
	git(t, other, "config", "user.name", "O")
	git(t, other, "checkout", "-b", "tmp", "origin/smind/task-x")
	commit(t, other, "c.txt", "c\n", "remote 1")
	commit(t, other, "d.txt", "d\n", "remote 2")
	git(t, other, "push", "origin", "tmp:smind/task-x")
	git(t, f.repo, "fetch", "origin")

	g := Compute(context.Background(), DefaultRunner, f.input())
	o := g.Origin
	if o == nil || !o.Published || o.Upstream != "origin/smind/task-x" || o.Ahead == nil || o.Behind == nil {
		t.Fatalf("origin = %+v, want published with counts", o)
	}
	if *o.Ahead != 1 || *o.Behind != 2 {
		t.Fatalf("origin ahead/behind = %d/%d, want 1/2", *o.Ahead, *o.Behind)
	}
}

func TestGitStatus_AbsentNotZero(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	if err := os.RemoveAll(f.worktree); err != nil {
		t.Fatal(err)
	}
	in := f.input()
	in.DiffStat = func() (int, int, int, error) { return 0, 0, 0, errors.New("boom") }

	g := Compute(context.Background(), DefaultRunner, in)
	if g.Worktree != nil {
		t.Errorf("worktree = %+v, want absent for a missing worktree dir", g.Worktree)
	}
	if g.Diff != nil {
		t.Errorf("diff = %+v, want absent when DiffStat fails", g.Diff)
	}
	if g.Commits == nil || g.Commits.AheadOfBase == nil || *g.Commits.AheadOfBase != 1 {
		t.Errorf("commits = %+v, want the shared-repo fields still present", g.Commits)
	}
	if g.Branch == nil || g.Base == nil {
		t.Errorf("branch/base = %+v/%+v, want present", g.Branch, g.Base)
	}
}

func TestGitStatus_AllGitFailing_AllGitFieldsAbsent(t *testing.T) {
	f := newFixture(t)
	failing := func(context.Context, string, ...string) (string, error) { return "", errors.New("no git") }
	g := Compute(context.Background(), failing, f.input())
	if g.Worktree != nil || g.Commits != nil || g.Origin != nil || g.Conflict != nil {
		t.Fatalf("got %+v, want every git-derived object absent", g)
	}
	if g.Base.Tip != "" {
		t.Fatalf("tip = %q, want empty", g.Base.Tip)
	}
}

func TestCompute_NoWorktree(t *testing.T) {
	g := Compute(context.Background(), DefaultRunner, Input{TaskID: 7, BaseRef: "develop"})
	if g.Branch != nil || g.Worktree != nil || g.Commits != nil || g.Merged != nil {
		t.Fatalf("got %+v, want only base", g)
	}
	if a := g.Actions["createPr"]; a.Enabled || a.Code != "task_no_worktree" {
		t.Fatalf("createPr = %+v, want task_no_worktree", a)
	}
}

func TestCompute_DiffStatPassedThrough(t *testing.T) {
	f := newFixture(t)
	in := f.input()
	in.DiffStat = func() (int, int, int, error) { return 3, 22, 2, nil }
	g := Compute(context.Background(), DefaultRunner, in)
	if g.Diff == nil || *g.Diff != (Diff{3, 22, 2}) {
		t.Fatalf("diff = %+v", g.Diff)
	}
}
