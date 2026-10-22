package gitstate

import (
	"context"
	"testing"
)

func TestGitStatus_MergedByAncestor(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task work")
	// Land it on develop with a merge commit and publish.
	git(t, f.repo, "merge", "--no-ff", "-m", "merge task", "smind/task-x")
	git(t, f.repo, "push", "origin", "develop")
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Merged == nil || g.Merged.State != MergedMerged || g.Merged.Via != MergedViaAncestor {
		t.Fatalf("merged = %+v, want merged/ancestor", g.Merged)
	}
}

func TestGitStatus_EmptyBranchNotMerged(t *testing.T) {
	f := newFixture(t)
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Merged == nil || g.Merged.State != MergedUnmerged {
		t.Fatalf("merged = %+v, want unmerged for a branch with no commits", g.Merged)
	}
}

func TestGitStatus_UnmergedWhenAhead(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task work")
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Merged == nil || g.Merged.State != MergedUnmerged {
		t.Fatalf("merged = %+v, want unmerged", g.Merged)
	}
}

func TestGitStatus_MergedByPRHeadSha(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task work")
	tip := git(t, f.worktree, "rev-parse", "HEAD")
	// Squash-merge on develop: the branch is not an ancestor.
	git(t, f.repo, "merge", "--squash", "smind/task-x")
	git(t, f.repo, "commit", "-m", "squashed")
	git(t, f.repo, "push", "origin", "develop")

	in := f.input()
	if g := Compute(context.Background(), DefaultRunner, in); g.Merged.State != MergedUnmerged {
		t.Fatalf("without a PR: %+v, want unmerged", g.Merged)
	}
	in.PR = &PRInfo{State: "merged", HeadSha: tip}
	if g := Compute(context.Background(), DefaultRunner, in); g.Merged.State != MergedMerged || g.Merged.Via != MergedViaPR {
		t.Fatalf("merged PR at tip: %+v, want merged/pr", g.Merged)
	}
	// The branch moved after the PR merged: no longer merged.
	commit(t, f.worktree, "b.txt", "b\n", "after merge")
	if g := Compute(context.Background(), DefaultRunner, in); g.Merged.State != MergedUnmerged {
		t.Fatalf("tip != pr.headSha: %+v, want unmerged", g.Merged)
	}
}

func TestGitStatus_MergedUnknown(t *testing.T) {
	f := newFixture(t)
	m := ComputeMerged(context.Background(), DefaultRunner, f.repo, "", "refs/heads/nope", nil, nil)
	if m.State != MergedUnknown {
		t.Fatalf("missing branch: %+v, want unknown", m)
	}
	m = ComputeMerged(context.Background(), DefaultRunner, f.repo, "refs/heads/develop", "refs/heads/smind/task-x", nil, nil)
	if m.State != MergedUnknown {
		t.Fatalf("ancestor with unknown sinceFork: %+v, want unknown", m)
	}
}
