//go:build unix

package acp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group, so the agent and
// every grandchild it spawns (e.g. an MCP server run via npx -> node ->
// chromium) can be killed together by killProcessGroup.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup force-kills cmd's whole process group, falling back to
// the agent process alone if signalling the group fails. Only call it
// while cmd has not yet been waited on, so its pid (the group id) can't
// have been reused.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
