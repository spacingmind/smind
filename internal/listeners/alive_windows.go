//go:build windows

package listeners

import (
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
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
