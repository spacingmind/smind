//go:build unix

package host

import "syscall"

// alive reports whether pid is a live (non-zombie-reaped) process.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
