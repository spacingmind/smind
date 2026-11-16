//go:build windows

package listeners

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code GetExitCodeProcess reports for a process that
// has not exited yet.
const stillActive = 259

// ProcessAlive reports whether pid is a running process (OpenProcess with
// PROCESS_QUERY_LIMITED_INFORMATION, then GetExitCodeProcess == STILL_ACTIVE).
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// ERROR_INVALID_PARAMETER means no such process. Access denied means
		// the process exists but belongs to another user or a service, and any
		// other unexpected error is doubt: a listener file of a live service
		// must not be deleted on a doubt, so both are alive (the unix
		// equivalent treats EPERM as alive too).
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
