package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeGitHoldingPipes puts a `git` first on PATH that prints "out", exits 0
// and leaves a background child holding its stdout/stderr open for 1.2 s --
// the shape of a successful git whose output pipes stay busy (a hook, an
// auto-gc, or a copy goroutine a starved CPU has not run yet).
func fakeGitHoldingPipes(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n(sleep 1.2 &)\necho out\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRunGit_UncancellableContextDoesNotAbandonLingeringPipes: with no way to
// cancel, a git that exited 0 is a success however long its pipes linger. The
// 500 ms WaitDelay used to apply here too and turned it into an error.
func TestRunGit_UncancellableContextDoesNotAbandonLingeringPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh git stand-in")
	}
	fakeGitHoldingPipes(t)
	out, err := runGitOutputEnvCtx(context.Background(), t.TempDir(), nil, "status")
	if err != nil {
		t.Fatalf("runGitOutputEnvCtx(Background) error = %v, want success", err)
	}
	if out != "out\n" {
		t.Fatalf("stdout = %q, want %q", out, "out\n")
	}
}

// TestRunGit_CancellableContextStillBoundsLingeringPipes: a cancellable ctx
// keeps the WaitDelay bound, so a caller with a deadline gets control back
// even when a grandchild holds the pipes.
func TestRunGit_CancellableContextStillBoundsLingeringPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh git stand-in")
	}
	fakeGitHoldingPipes(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := time.Now()
	_, err := runGitOutputEnvCtx(ctx, t.TempDir(), nil, "status")
	if err == nil {
		t.Fatal("want an ErrWaitDelay error once the pipes outlive gitWaitDelay")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("returned after %s, want it bounded by gitWaitDelay (%s)", took, gitWaitDelay)
	}
}
