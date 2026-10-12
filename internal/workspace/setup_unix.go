//go:build !windows

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// runScript runs script via `sh -c` with cwd = worktree in its own process
// group, so a timeout or cancellation kills the whole tree. env entries
// ("K=V") are appended to the daemon's environment and are only ever passed
// as environment variables, never interpolated into the script text. It
// returns the tail (<= logCap bytes) of merged stdout+stderr.
func runScript(ctx context.Context, worktree, script string, env []string, timeout time.Duration, logCap int) (string, error) {
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out := &tailBuffer{cap: logCap}
	cmd.Stdout, cmd.Stderr = out, out
	// Bound the wait for output pipes held open by orphaned grandchildren.
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start script: %w", err)
	}
	pgid := cmd.Process.Pid
	killGroup := func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) }

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
		// Reap any stragglers left in the group.
		killGroup()
		if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
			return out.String(), fmt.Errorf("script failed: %w", err)
		}
		return out.String(), nil
	case <-timer:
		killGroup()
		<-done
		return out.String(), fmt.Errorf("script timed out after %s", timeout)
	case <-ctx.Done():
		killGroup()
		<-done
		return out.String(), fmt.Errorf("script cancelled: %w", ctx.Err())
	}
}

// defaultShLookup is the Unix flavor of the shell seam: scripts run under
// sh, assumed present on every supported Unix.
func defaultShLookup() (string, error) {
	return "sh", nil
}
