//go:build !windows

package terminal

import (
	"os"
	"sync"
	"syscall"
	"testing"
)

// forceTestShell sets $SHELL to /bin/bash for the whole test process
// (once), so every session these tests spawn runs bash regardless of the
// developer's own $SHELL (interactive zsh, dash, etc. can print different
// job-control chatter that would make output assertions flaky). A plain
// os.Setenv rather than t.Setenv deliberately: t.Setenv forbids
// t.Parallel in the same test, and every test in this file wants to run
// in parallel; $SHELL is only ever read (never restored) by resolveShell,
// and every test here wants the same value anyway, so process-wide is
// fine.
var forceTestShellOnce sync.Once

func forceTestShellImpl() {
	os.Setenv("SHELL", "/bin/bash")
}

func forceTestShell(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	forceTestShellOnce.Do(forceTestShellImpl)
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

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// writeLine returns the line-terminated form of a shell command line for
// the platform's test shell: "\n" here, "\r\n" on Windows (cmd.exe's
// console input requires the carriage return -- see
// helpers_windows_test.go).
func writeLine(cmd string) string {
	return cmd + "\n"
}
