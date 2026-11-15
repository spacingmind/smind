//go:build !windows

package browser

import "syscall"

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
