//go:build windows

package terminal

import (
	"testing"

	"golang.org/x/sys/windows"
)

// forceTestShell points the Windows tests at cmd.exe for determinism
// (the spec's Test Scenarios note): a fixed, prompt-stable shell whose
// `echo` and `exit` semantics the shared tests rely on. Unlike the Unix
// flavor it sets nothing in the environment -- resolveShell ignores
// $SHELL on Windows -- so it's just a skip guard plus documentation of
// the invariant, and the per-test write helper below appends \r\n.
func forceTestShell(t *testing.T) {
	t.Helper()
}

// writeLine returns the line-terminated form of a shell command line for
// the platform's test shell: \n on Unix, \r\n on Windows (cmd.exe's
// console input requires the carriage return).
func writeLine(cmd string) string {
	return cmd + "\r\n"
}

func pidOf(t *testing.T, reg *Registry, id string) int {
	t.Helper()
	reg.mu.Lock()
	s, ok := reg.sessions[id]
	reg.mu.Unlock()
	if !ok {
		t.Fatalf("session %s not found", id)
	}
	return s.pty.pid()
}

// stillActive is GetExitCodeProcess's sentinel for "no exit code yet"
// (STATUS_PENDING); x/sys doesn't export it.
const stillActive = 259

// processAlive on Windows: signal-0 doesn't exist, so open the pid with
// PROCESS_QUERY_LIMITED_INFORMATION (denied/absent for a dead pid --
// pids get recycled, so this is best-effort like the Unix flavor, good
// enough for the few-seconds windows the tests assert over) and check
// the exit code via GetExitCodeProcess (the 259 "still active" sentinel
// means alive).
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}
