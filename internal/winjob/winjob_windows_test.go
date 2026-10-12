//go:build windows

package winjob

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func cmdExe() string { return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe") }

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == 259 // STILL_ACTIVE
}

func waitGone(t *testing.T, what string, pid int, d time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if !alive(pid) {
			return
		}
	}
	t.Fatalf("%s (pid %d) still alive after %s", what, pid, d)
}

// grandchildOf returns the pid of the PING.EXE whose parent is parent.
func grandchildOf(t *testing.T, parent int) int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		out, err := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf(
			`(Get-CimInstance Win32_Process -Filter "Name='PING.EXE'" | Where-Object { $_.ParentProcessId -eq %d }).ProcessId`, parent)).Output()
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid != 0 {
				return pid
			}
		}
	}
	t.Fatalf("no ping grandchild found under pid %d", parent)
	return 0
}

// A child started through Start really runs: it is created suspended, and
// Attach must resume it. If resume were skipped or ignored this would hang.
func TestStart_ResumesChild(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(cmdExe(), "/c", "echo winjob-ran")
	var out bytes.Buffer
	cmd.Stdout = &out
	job, err := Start(cmd, 0)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer job.Close()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child never ran (still suspended?)")
	}
	if !strings.Contains(out.String(), "winjob-ran") {
		t.Fatalf("output = %q, want it to contain winjob-ran", out.String())
	}
}

// Close takes the whole tree down: the direct child and its grandchild.
func TestClose_KillsTree(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(cmdExe(), "/c", "ping -n 300 127.0.0.1")
	job, err := Start(cmd, 0)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	child := cmd.Process.Pid
	grandchild := grandchildOf(t, child)
	if !alive(child) || !alive(grandchild) {
		t.Fatalf("tree not running before Close (child %v, grandchild %v)", alive(child), alive(grandchild))
	}

	job.Close()
	waitGone(t, "child", child, 5*time.Second)
	waitGone(t, "grandchild", grandchild, 5*time.Second)
	_ = cmd.Wait()
}

func TestClose_IdempotentAndNilSafe(t *testing.T) {
	t.Parallel()
	var nilJob *Job
	nilJob.Close() // must not panic

	cmd := exec.Command(cmdExe(), "/c", "exit 0")
	job, err := Start(cmd, 0)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_ = cmd.Wait()
	job.Close()
	job.Close() // second close must not CloseHandle a recycled handle
}

// Extra creation flags are combined with CREATE_SUSPENDED, not replacing it.
func TestPrepare_KeepsSysProcAttrAndAddsFlags(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(cmdExe(), "/c", "exit 0")
	Prepare(cmd, windows.CREATE_NEW_PROCESS_GROUP)
	want := uint32(windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP)
	if got := cmd.SysProcAttr.CreationFlags; got&want != want {
		t.Fatalf("CreationFlags = %#x, want it to include %#x", got, want)
	}
}
