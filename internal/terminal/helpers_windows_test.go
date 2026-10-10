//go:build windows

package terminal

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// init points every session the Windows tests spawn at cmd.exe for
// determinism (the spec's Test Scenarios note): a fixed, prompt-stable
// shell whose `echo` and `exit` semantics the shared tests rely on.
// Without it the tests ran whatever resolveShell found -- pwsh on CI,
// Windows PowerShell 5.1 elsewhere -- whose PSReadLine redraws and
// drops early keystrokes, which made the persistence tests flaky. Set in
// init rather than in forceTestShell because not every test calls
// forceTestShell, and a write racing parallel tests' reads would be a
// data race; init runs before any test.
func init() {
	shellOverride = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}

// forceTestShell is a no-op on Windows: init above already forced
// cmd.exe for the whole test binary.
func forceTestShell(t *testing.T) {
	t.Helper()
}

// writeLine returns the line-terminated form of a shell command line for
// the platform's test shell: \n on Unix, \r on Windows -- what a real
// terminal (xterm.js) sends for Enter. A trailing \n there would reach
// the shell as a second keystroke (an extra empty command in cmd.exe, a
// `>>` continuation line in PSReadLine).
func writeLine(cmd string) string {
	return cmd + "\r"
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
