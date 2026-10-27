//go:build !windows

package qaframes

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func signalPID(pid int, group bool, sig syscall.Signal) {
	if pid <= 1 {
		return
	}
	if group {
		_ = syscall.Kill(-pid, sig)
		return
	}
	_ = syscall.Kill(pid, sig)
}

func terminate(pid int, group bool) { signalPID(pid, group, syscall.SIGTERM) }
func forceKill(pid int, group bool) { signalPID(pid, group, syscall.SIGKILL) }
func pidAlive(pid int) bool         { return pid > 1 && syscall.Kill(pid, 0) == nil }
