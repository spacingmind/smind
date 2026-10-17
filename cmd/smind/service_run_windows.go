//go:build windows

package main

import (
	"context"
	"errors"
	"os/exec"

	"github.com/spacingmind/smind/internal/winjob"
)

// runDaemonChild runs `smind serve` inside a kill-on-close Job Object, so the
// daemon (and everything it spawned) dies with this supervisor: ending the
// supervisor, as `smind service stop` does in its fallback, can never leave an
// orphaned daemon behind.
func runDaemonChild(ctx context.Context, exe string) (int, error) {
	cmd := daemonCommand(ctx, exe)
	job, err := winjob.Start(cmd, 0)
	if err != nil {
		return 0, err
	}
	defer job.Close()
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}
