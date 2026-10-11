//go:build !unix

package acp

import "os/exec"

// setProcessGroup is a no-op where process groups aren't available.
func setProcessGroup(*exec.Cmd) {}

// killProcessGroup kills just the agent process where process groups
// aren't available.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
