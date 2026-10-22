package gitstate

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func conflictFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	commit(t, f.worktree, "shared.txt", "task line\n", "task edits shared")
	commit(t, f.worktree, "only-task.txt", "t\n", "task only")
	f.advanceBase(t, "shared.txt", "base line\n")
	return f
}

func TestGitStatus_MergeTreeConflict_Files(t *testing.T) {
	f := conflictFixture(t)
	g := Compute(context.Background(), DefaultRunner, f.input())
	c := g.Conflict
	if c == nil || c.State != ConflictConflicts || !reflect.DeepEqual(c.Files, []string{"shared.txt"}) || c.Against != "origin/develop" {
		t.Fatalf("conflict = %+v, want conflicts [shared.txt] against origin/develop", c)
	}
	if c.CheckedAt.IsZero() {
		t.Fatal("checkedAt unset")
	}
}

func TestGitStatus_MergeTreeNoConflict(t *testing.T) {
	f := newFixture(t)
	commit(t, f.worktree, "t.txt", "t\n", "task")
	f.advanceBase(t, "b.txt", "b\n")
	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Conflict == nil || g.Conflict.State != ConflictNone || len(g.Conflict.Files) != 0 || g.Conflict.Files == nil {
		t.Fatalf("conflict = %+v, want none with an empty (non-nil) file list", g.Conflict)
	}
}

func TestGitStatus_MergeTreeSkippedWhenNotBothAheadAndBehind(t *testing.T) {
	f := newFixture(t)
	cr := &countingRunner{}
	// Neither ahead nor behind.
	Compute(context.Background(), cr.run, f.input())
	// Ahead only.
	commit(t, f.worktree, "a.txt", "a\n", "ahead")
	Compute(context.Background(), cr.run, f.input())
	// Behind only.
	g2 := newFixture(t)
	g2.advanceBase(t, "b.txt", "b\n")
	Compute(context.Background(), cr.run, g2.input())
	if n := cr.count("merge-tree"); n != 0 {
		t.Fatalf("merge-tree ran %d times, want 0", n)
	}
}

func TestGitStatus_MergeTreeCachedByTips(t *testing.T) {
	f := conflictFixture(t)
	cr := &countingRunner{}
	cache := NewConflictCache()
	in := f.input()
	in.Conflicts = cache
	Compute(context.Background(), cr.run, in)
	Compute(context.Background(), cr.run, in)
	if n := cr.count("merge-tree"); n != 1 {
		t.Fatalf("merge-tree ran %d times over two computes, want 1", n)
	}
	// Moving the branch tip re-runs it.
	commit(t, f.worktree, "more.txt", "m\n", "more")
	Compute(context.Background(), cr.run, in)
	if n := cr.count("merge-tree"); n != 2 {
		t.Fatalf("after branch moved: %d runs, want 2", n)
	}
	// Moving the base tip re-runs it.
	f.advanceBase(t, "other.txt", "o\n")
	Compute(context.Background(), cr.run, in)
	if n := cr.count("merge-tree"); n != 3 {
		t.Fatalf("after base moved: %d runs, want 3", n)
	}
}

func TestGitStatus_GitTooOld_Unknown(t *testing.T) {
	f := conflictFixture(t)
	calls := 0
	old := func(ctx context.Context, dir string, args ...string) (string, error) {
		for _, a := range args {
			if a == "merge-tree" {
				calls++
				return "", &RunError{Args: args, ExitCode: 129, Stderr: "error: unknown option `write-tree'\nusage: git merge-tree", Err: context.Canceled}
			}
		}
		return DefaultRunner(ctx, dir, args...)
	}
	in := f.input()
	in.Conflicts = NewConflictCache()
	for range 2 {
		g := Compute(context.Background(), old, in)
		c := g.Conflict
		if c == nil || c.State != ConflictUnknown || c.Reason != ReasonGitTooOld {
			t.Fatalf("conflict = %+v, want unknown/git_too_old", c)
		}
	}
	if calls != 1 {
		t.Fatalf("merge-tree attempted %d times, want the too-old verdict cached after 1", calls)
	}
}

func TestGitStatus_ConflictCheckedAtUsesClock(t *testing.T) {
	f := conflictFixture(t)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	in := f.input()
	in.Conflicts = &ConflictCache{Now: func() time.Time { return at }}
	g := Compute(context.Background(), DefaultRunner, in)
	if !g.Conflict.CheckedAt.Equal(at) {
		t.Fatalf("checkedAt = %v, want %v", g.Conflict.CheckedAt, at)
	}
}

func TestParseConflictFiles_CapsAtMax(t *testing.T) {
	var b strings.Builder
	b.WriteString("deadbeef\n")
	for i := 0; i < maxConflictFiles+50; i++ {
		fmt.Fprintf(&b, "f%d.txt\n", i)
	}
	if got := parseConflictFiles(b.String()); len(got) != maxConflictFiles {
		t.Fatalf("len = %d, want %d", len(got), maxConflictFiles)
	}
}
