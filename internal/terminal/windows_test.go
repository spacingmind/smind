//go:build windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file holds the Windows-only test scenarios from
// docs/plans/active/windows-native-terminal.md: ConPTY echo round-trip,
// natural shell exit closing the session (the AC4 ConPTY-doesn't-EOF
// gotcha), Job Object tree kill (AC5), concurrent CloseAll, and resize
// (AC7). resolveShell's own table test (AC6) lives here too.
//
// All of them force cmd.exe (helpers_windows_test.go's forceTestShell)
// for deterministic prompts, and send input with \r\n (writeLine).

func TestWindows_CreateEchoRoundTrip(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte(writeLine("echo hello-win"))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "hello-win", 10*time.Second)

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestWindows_ShellExitClosesSession proves AC4: ConPTY does not EOF its
// output pipe when the shell exits, so a session whose shell exits on
// its own (user types `exit`) must still reach StatusClosed -- via
// waitLoop's WaitProcess -> close-the-pty -> readLoop unblocks -> finish
// -- with its final scrollback persisted, and Close afterwards must be
// the documented no-op.
func TestWindows_ShellExitClosesSession(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	newTestTaskID(t, st)
	reg, err := New(st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Write(id, []byte(writeLine("echo before-exit"))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "before-exit", 10*time.Second)

	if err := reg.Write(id, []byte(writeLine("exit"))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// StatusClosed within 10s, without any Close() nudging the pty.
	deadline := time.Now().Add(10 * time.Second)
	closed := false
	for time.Now().Before(deadline) {
		for _, s := range reg.List(1) {
			if s.ID == id && s.Status == StatusClosed {
				closed = true
			}
		}
		if closed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !closed {
		t.Fatal("session did not reach StatusClosed within 10s of `exit` (AC4: ConPTY exit detection)")
	}

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() after natural exit: error = %v, want nil (no-op)", err)
	}

	row, err := st.GetTerminalSession(id)
	if err != nil {
		t.Fatalf("GetTerminalSession() error = %v", err)
	}
	if row.Status != string(StatusClosed) {
		t.Fatalf("persisted Status = %q, want %q", row.Status, StatusClosed)
	}
	if !strings.Contains(row.Scrollback, "before-exit") {
		t.Fatalf("persisted scrollback = %q, want it to contain output written before exit", row.Scrollback)
	}
}

// TestWindows_CloseKillsDescendants proves AC5: Close kills the shell
// AND its descendants. A `ping -n` started from the shell is captured
// via tasklist (its pid printed by a helper `start /b`-free pattern:
// ping itself is the descendant), then Close, then the pid must be gone
// within 5s -- the Job Object's kill-on-close doing the tree kill.
func TestWindows_CloseKillsDescendants(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	// ping -n 300 127.0.0.1 is the long-running descendant; its pid is
	// discovered by asking WMI for the PING process whose parent is the
	// shell.
	if err := reg.Write(id, []byte(writeLine("ping -n 300 127.0.0.1"))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	shellPid := pidOf(t, reg, id)
	var pingPid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := combinedOutput("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`(Get-CimInstance Win32_Process -Filter "Name='PING.EXE'" | Where-Object { $_.ParentProcessId -eq %d }).ProcessId`, shellPid))
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && pid != 0 {
				pingPid = pid
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pingPid == 0 {
		t.Fatal("could not find the shell's ping descendant pid within 10s")
	}
	if !processAlive(pingPid) {
		t.Fatalf("ping pid %d not alive right after starting it", pingPid)
	}

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	waitGone(t, pingPid, 5*time.Second)
	waitGone(t, shellPid, 5*time.Second)
}

func TestWindows_CloseAllConcurrent(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)

	ids := make([]string, 3)
	for i := range ids {
		id, err := reg.Create(1, t.TempDir())
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		ids[i] = id
	}

	done := make(chan struct{})
	go func() {
		reg.CloseAll()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("CloseAll did not return within 30s")
	}

	for _, id := range ids {
		found := false
		for _, s := range reg.List(1) {
			if s.ID == id {
				found = true
				if s.Status != StatusClosed {
					t.Fatalf("session %s status = %q, want %q", id, s.Status, StatusClosed)
				}
			}
		}
		if !found {
			t.Fatalf("session %s not in List(1) after CloseAll", id)
		}
	}
}

func TestWindows_Resize(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t)

	id, err := reg.Create(1, t.TempDir())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, unsubscribe, err := reg.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	if err := reg.Resize(id, 80, 24); err != nil {
		t.Fatalf("Resize(80,24) error = %v", err)
	}
	if err := reg.Resize(id, 120, 40); err != nil {
		t.Fatalf("Resize(120,40) error = %v", err)
	}

	// Prove the resize reached ConPTY by asking the shell its own size:
	// cmd.exe's `mode con` reports the live console dimensions.
	if err := reg.Write(id, []byte(writeLine("mode con"))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	collectUntil(t, events, "120", 10*time.Second)

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := reg.Resize(id, 80, 24); err == nil {
		t.Fatal("Resize() on a closed session: error = nil, want the not-running error")
	}
}

// combinedOutput runs a command and returns its trimmed combined output.
func combinedOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestResolveShell_Windows is the AC6 table test over PATH/COMSPEC
// fixtures, against resolveShellWindows (the testable core of
// resolveShell -- same decision order, injected lookPath/COMSPEC).
func TestResolveShell_Windows(t *testing.T) {
	t.Parallel()

	pwsh := func(string) (string, error) { return `C:\Program Files\PowerShell\7\pwsh.exe`, nil }
	powershell := func(string) (string, error) {
		return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, nil
	}
	none := func(string) (string, error) { return "", os.ErrNotExist }

	tests := []struct {
		name       string
		lookPath   func(string) (string, error)
		comspec    string
		systemRoot string
		want       string
	}{
		{
			name:     "pwsh on PATH",
			lookPath: pwsh,
			want:     `C:\Program Files\PowerShell\7\pwsh.exe`,
		},
		{
			name:     "only powershell",
			lookPath: powershell,
			want:     `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		},
		{
			name:       "neither, COMSPEC set",
			lookPath:   none,
			comspec:    `C:\Windows\System32\cmd.exe`,
			systemRoot: `C:\Windows`,
			want:       `C:\Windows\System32\cmd.exe`,
		},
		{
			name:       "neither, COMSPEC empty",
			lookPath:   none,
			systemRoot: `C:\Windows`,
			want:       `C:\Windows\System32\cmd.exe`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveShellWindows(tt.lookPath, tt.comspec, tt.systemRoot); got != tt.want {
				t.Fatalf("resolveShellWindows() = %q, want %q", got, tt.want)
			}
		})
	}
}
