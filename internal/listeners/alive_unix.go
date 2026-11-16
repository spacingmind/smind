//go:build !windows

package listeners

import (
	"errors"
	"syscall"
)

// ProcessAlive reports whether pid is a running process (signal 0; EPERM means
// a process of another user exists).
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
