package gitstate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

// publishedDiverged is a fixture whose task branch is published, then falls
// behind the base and is rebased onto it: ahead and behind origin/<branch>.
func publishedRebased(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	git(t, f.worktree, "config", "user.email", "t@example.com")
	git(t, f.worktree, "config", "user.name", "T")
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	git(t, f.worktree, "push", "-q", "origin", "smind/task-x")
	f.advanceBase(t, "base.txt", "b\n")
	git(t, f.worktree, "rebase", "origin/develop")
	return f
}

func pushReason(t *testing.T, f fixture, run Runner) Action {
	t.Helper()
	g := Compute(context.Background(), run, f.input())
	if g.Origin == nil || g.Worktree == nil || g.Base == nil {
		t.Fatalf("status not computed: %+v", g)
	}
	return g.Actions["push"]
}

func TestRebasedDivergedFromReflog(t *testing.T) {
	f := publishedRebased(t)
	a := pushReason(t, f, DefaultRunner)
	if a.Enabled || a.Code != forge.CodeNonFastForward || a.Reason != forge.MsgRebasedDiverged {
		t.Fatalf("after a rebase: push action = %+v", a)
	}

	// (b) A later commit puts another subject first: the generic reason.
	commit(t, f.worktree, "later.txt", "l\n", "after the rebase")
	a = pushReason(t, f, DefaultRunner)
	if a.Code != forge.CodeNonFastForward || a.Reason == forge.MsgRebasedDiverged || !strings.Contains(a.Reason, "origin has newer changes") {
		t.Fatalf("after rebase then commit: push action = %+v", a)
	}
}

// (c) Origin moved with no rebase: the generic reason.
func TestRebasedDivergedAbsentWhenOriginMoved(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "a.txt", "a\n", "task 1")
	git(t, f.worktree, "push", "-q", "origin", "smind/task-x")
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", f.bare, other)
	git(t, other, "config", "user.email", "o@example.com")
	git(t, other, "config", "user.name", "O")
	git(t, other, "checkout", "-q", "smind/task-x")
	commit(t, other, "theirs.txt", "t\n", "theirs")
	git(t, other, "push", "-q", "origin", "smind/task-x")
	commit(t, f.worktree, "b.txt", "b\n", "mine")
	git(t, f.worktree, "fetch", "-q", "origin")
	a := pushReason(t, f, DefaultRunner)
	if a.Code != forge.CodeNonFastForward || a.Reason == forge.MsgRebasedDiverged {
		t.Fatalf("push action = %+v", a)
	}
}

// (d) A reflog that errors, is empty or does not exist is "unknown": generic
// reason, and the status is still computed.
func TestRebasedDivergedReflogUnknown(t *testing.T) {
	f := publishedRebased(t)
	failing := func(ctx context.Context, dir string, args ...string) (string, error) {
		for _, a := range args {
			if a == "reflog" {
				return "", errors.New("reflog unavailable")
			}
		}
		return DefaultRunner(ctx, dir, args...)
	}
	a := pushReason(t, f, failing)
	if a.Code != forge.CodeNonFastForward || a.Reason == forge.MsgRebasedDiverged {
		t.Fatalf("reflog error: push action = %+v", a)
	}
	empty := func(ctx context.Context, dir string, args ...string) (string, error) {
		for _, a := range args {
			if a == "reflog" {
				return "\n", nil
			}
		}
		return DefaultRunner(ctx, dir, args...)
	}
	if a := pushReason(t, f, empty); a.Reason == forge.MsgRebasedDiverged {
		t.Fatalf("empty reflog: push action = %+v", a)
	}
	ahead, behind := 1, 1
	o := &Origin{Published: true, Ahead: &ahead, Behind: &behind}
	if rebasedDiverged(context.Background(), DefaultRunner, f.repo, "no/such-branch", o) {
		t.Fatal("unknown branch reported as rebased")
	}
	// Expired reflog: no entries left.
	git(t, f.repo, "reflog", "expire", "--expire=now", "--all")
	if a := pushReason(t, f, DefaultRunner); a.Reason == forge.MsgRebasedDiverged {
		t.Fatalf("expired reflog: push action = %+v", a)
	}
}

func TestRebasedDivergedNeedsBothAheadAndBehind(t *testing.T) {
	f := publishedRebased(t)
	zero, one := 0, 1
	for _, o := range []*Origin{
		nil, {Published: false, Ahead: &one, Behind: &one},
		{Published: true, Ahead: &zero, Behind: &one}, {Published: true, Ahead: &one, Behind: &zero},
		{Published: true},
	} {
		if rebasedDiverged(context.Background(), DefaultRunner, f.worktree, "smind/task-x", o) {
			t.Fatalf("origin %+v reported as rebased and diverged", o)
		}
	}
	if !rebasedDiverged(context.Background(), DefaultRunner, f.worktree, "smind/task-x", &Origin{Published: true, Ahead: &one, Behind: &one}) {
		t.Fatal("rebased branch with both counts > 0 not reported")
	}
}

// (f) The reflog subject git writes for a finished rebase, as this git emits it.
func TestRebaseReflogSubjectsPinned(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		f := newFixture(t)
		git(t, f.worktree, "config", "user.email", "t@example.com")
		git(t, f.worktree, "config", "user.name", "T")
		commit(t, f.worktree, "a.txt", "a\n", "task 1")
		f.advanceBase(t, "base.txt", "b\n")
		if interactive {
			t.Setenv("GIT_SEQUENCE_EDITOR", "true")
			git(t, f.worktree, "rebase", "-i", "origin/develop")
		} else {
			git(t, f.worktree, "rebase", "origin/develop")
		}
		out := git(t, f.worktree, "reflog", "-n", "20", "--format=%gs", "refs/heads/smind/task-x")
		first, _, _ := strings.Cut(out, "\n")
		if !strings.HasPrefix(first, "rebase (finish): refs/heads/smind/task-x onto ") && !strings.HasPrefix(first, "rebase -i (finish): refs/heads/smind/task-x onto ") {
			t.Fatalf("interactive=%v: newest reflog subject = %q", interactive, first)
		}
		if !isRebaseFinish(first) {
			t.Fatalf("isRebaseFinish(%q) = false", first)
		}
	}
	for _, s := range []string{"commit: x", "merge origin/develop: Fast-forward", "rebase (start): checkout origin/develop", "rebase (pick): x", "", "commit (amend): x"} {
		if isRebaseFinish(s) {
			t.Fatalf("isRebaseFinish(%q) = true", s)
		}
	}
}
