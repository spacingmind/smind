//go:build unix

package plugins

import "syscall"

// alive reports whether pid is a live (non-zombie-reaped) process.
func procAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
