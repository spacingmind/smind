//go:build windows

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"github.com/spacingmind/smind/internal/winjob"
)

// shCandidates lists sh.exe locations relative to the directory holding
// git.exe, covering the standard Git for Windows layouts (git.exe in
// <root>/cmd, <root>/bin or <root>/mingw64/bin) without hardcoding an
// install path.
var shCandidates = []string{"sh.exe", "../bin/sh.exe", "../usr/bin/sh.exe", "../../usr/bin/sh.exe"}

// findSh locates the POSIX shell: `sh` on PATH first, else Git for
// Windows' sh.exe derived from git.exe's location (ADR-0029 D1).
func findSh() (string, error) {
	if sh, err := exec.LookPath("sh"); err == nil {
		return sh, nil
	}
	// Derive Git for Windows' install dir from git.exe on PATH.
	git, err := exec.LookPath("git")
	if err != nil {
		return "", ErrSetupUnsupported
	}
	dir := filepath.Dir(git)
	for _, rel := range shCandidates {
		p := filepath.Join(dir, rel)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", ErrSetupUnsupported
}

// defaultShLookup is the Windows flavor of the shell seam; Manager tests
// inject a failing lookup to simulate a machine without one.
func defaultShLookup() (string, error) {
	return findSh()
}

// runScript is the Windows mirror of setup_unix.go's contract: the same
// POSIX script runs as `sh -c <script>` with cwd = worktree, and env
// entries ("K=V") are appended to the daemon's environment and only ever
// passed as environment variables, never interpolated into the script
// text. Note the env values are native Windows paths (e.g.
// C:\Users\...\worktrees\1\task); scripts that need POSIX paths use
// cygpath. It returns the tail (<= logCap bytes) of merged stdout+stderr.
// A timeout or cancellation kills the whole tree via a kill-on-close Job
// Object. checkSetupSupported has already verified sh exists; a cache
// miss (sh uninstalled mid-run) surfaces its error here.
func runScript(ctx context.Context, worktree, script string, env []string, timeout time.Duration, logCap int) (string, error) {
	sh, err := defaultShLookup()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(sh, "-c", script)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), env...)
	out := &tailBuffer{cap: logCap}
	cmd.Stdout, cmd.Stderr = out, out
	// Bound the wait for output pipes held open by orphaned grandchildren.
	cmd.WaitDelay = 2 * time.Second

	// Own process group (Ctrl+C isolation) and a kill-on-close Job Object, see internal/winjob.
	job, err := winjob.Start(cmd, windows.CREATE_NEW_PROCESS_GROUP)
	if err != nil {
		return "", fmt.Errorf("start script: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case err := <-done:
		// Reap any stragglers left in the job (an orphaned grandchild).
		job.Close()
		if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
			return out.String(), fmt.Errorf("script failed: %w", err)
		}
		return out.String(), nil
	case <-timer:
		job.Close()
		<-done
		return out.String(), fmt.Errorf("script timed out after %s", timeout)
	case <-ctx.Done():
		job.Close()
		<-done
		return out.String(), fmt.Errorf("script cancelled: %w", ctx.Err())
	}
}
