//go:build !windows

package main

import (
	"context"
	"errors"
	"os/exec"
)

// runDaemonChild runs `smind serve` and returns its exit code.
func runDaemonChild(ctx context.Context, exe string) (int, error) {
	cmd := daemonCommand(ctx, exe)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}
