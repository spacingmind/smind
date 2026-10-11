package terminal

import (
	"os/exec"
	"testing"
	"time"
)

// TestKillAndReap_NoZombieLeft is a direct, deterministic test of the
// killAndReap seam method (ptySession.killAndReap, see pty.go) backing
// Create's error paths (a newSessionID failure, a CreateTerminalSession
// failure), both of which run after a real shell has already been
// spawned: they used to kill it without ever waiting -- leaving a zombie
// process under the daemon, since nothing else ever reaps it. This
// spawns a real long-running process the same way Create does (through
// startPty, so the platform seam -- including Windows's Job Object
// assignment -- is exercised), calls killAndReap directly, and confirms
// both that the wait actually happened (cmd.ProcessState is only ever
// set once the wait has completed) and that the process is genuinely
// gone, not just a lingering zombie.
//
// Cross-platform by construction: the spawned command is the resolved
// shell with a `-c`-style sleep that works there too. On Unix that's
// `<shell> -c "sleep 300"`; build tags keep the bash-flavored spawn in
// killreap_unix_test.go and a cmd.exe-flavored one in
// killreap_windows_test.go, both funneling into this function.
func TestKillAndReap_NoZombieLeft(t *testing.T) {
	t.Parallel()

	cmd, pty, err := spawnSleepTestProcess(t, "300")
	if err != nil {
		t.Fatalf("spawnSleepTestProcess() error = %v", err)
	}
	pid := cmd.Process.Pid
	if !processAlive(pid) {
		t.Fatalf("pid %d not alive right after Start", pid)
	}

	pty.killAndReap()

	if cmd.ProcessState == nil {
		t.Fatal("cmd.ProcessState = nil, want set -- killAndReap did not wait")
	}
	waitGone(t, pid, 2*time.Second)
}

// spawnSleepTestProcessForCommand starts cmd through startPty (the real
// platform seam, exactly as Create would) and returns it with its
// ptySession. Shared body of the per-OS spawnSleepTestProcess helpers.
func spawnSleepTestProcessForCommand(t *testing.T, cmd *exec.Cmd) (*exec.Cmd, *ptySession, error) {
	t.Helper()
	pty, err := startPty(cmd)
	if err != nil {
		return nil, nil, err
	}
	return cmd, pty, nil
}
