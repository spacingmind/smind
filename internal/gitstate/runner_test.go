package gitstate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGitRunner_HostileFsmonitorNotExecuted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh hook")
	}
	f := newFixture(t)
	marker := filepath.Join(t.TempDir(), "marker")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\nprintf '\\0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, f.repo, "config", "core.fsmonitor", hook)
	write(t, f.worktree, "new.txt", "x\n")

	g := Compute(context.Background(), DefaultRunner, f.input())
	if g.Worktree == nil || g.Worktree.Untracked != 1 {
		t.Fatalf("worktree = %+v, want status to work", g.Worktree)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("core.fsmonitor hook ran: marker file exists")
	}
}

func TestGitRunner_DeadlineKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh git stand-in")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := DefaultRunner(ctx, t.TempDir(), "status"); err == nil {
		t.Fatal("want an error from a killed git")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("returned after %s, want it bounded by the deadline + WaitDelay", took)
	}
}

func TestGitRunner_ArgvOnly(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(f.repo, "x")
	_, err := DefaultRunner(context.Background(), f.repo, "rev-parse", "--verify", "--end-of-options", "HEAD; touch "+marker)
	if err == nil {
		t.Fatal("want rev-parse to reject the odd revision")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("shell metacharacters were interpreted: marker exists")
	}
}

func TestGitRunner_ExitCodeAndStdoutOnFailure(t *testing.T) {
	f := newFixture(t)
	out, err := DefaultRunner(context.Background(), f.repo, "merge-base", "--is-ancestor", "HEAD", "HEAD~0")
	if err != nil || out != "" {
		t.Fatalf("ancestor of itself: out=%q err=%v", out, err)
	}
	commit(t, f.repo, "z.txt", "z\n", "z")
	_, err = DefaultRunner(context.Background(), f.repo, "merge-base", "--is-ancestor", "HEAD", "HEAD~1")
	if exitCode(err) != 1 {
		t.Fatalf("exit code = %d (%v), want 1", exitCode(err), err)
	}
}
