//go:build windows

package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// buildTestCmd compiles this package's treeagent test helper into dir and
// returns its path.
func buildTestCmd(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, exeName("treeagent"))
	_, thisFile, _, _ := runtime.Caller(0)
	build := exec.Command("go", "build", "-o", bin, "./treeagent")
	build.Dir = filepath.Dir(thisFile)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build treeagent: %v: %s", err, out)
	}
	return bin
}

// TestSpawnShim_UnsafeArgRejected_Windows: an argument carrying a cmd.exe
// metacharacter must be refused before anything is spawned when the
// command resolves to an npm-style .cmd shim, with the error naming the
// offending argument (see internal/acp's TestSpawnShim_* for the measured
// background and the round-trip half).
func TestSpawnShim_UnsafeArgRejected_Windows(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shim.cmd"), []byte("@echo off\r\n"), 0o666); err != nil {
		t.Fatalf("write shim.cmd: %v", err)
	}
	t.Setenv("PATH", dir)

	for _, arg := range []string{"a&b", "a|b", "a^b", "%PATH%"} {
		if _, err := newConn([]string{"shim", arg}, nil); err == nil {
			t.Fatalf("newConn(shim, %q) succeeded, want refusal", arg)
		} else if !strings.Contains(err.Error(), strconv.Quote(arg)) {
			t.Fatalf("newConn(shim, %q) error %q does not name the argument", arg, err)
		}
	}
}

// TestAgentStop_KillsDescendants_Windows: a CLI (treeagent) that ignores
// stdin close and has spawned a grandchild is force-killed by conn.close's
// timeout path together with that grandchild.
func TestAgentStop_KillsDescendants_Windows(t *testing.T) {
	dir := t.TempDir()
	agent := buildTestCmd(t, dir)
	pidsPath := filepath.Join(dir, "pids.json")
	t.Setenv("SMIND_TREEAGENT_OUT", pidsPath)

	c, err := newConn([]string{agent}, nil)
	if err != nil {
		t.Fatalf("newConn() error = %v", err)
	}
	var pids treePids
	readJSONFileWithDeadline(t, pidsPath, &pids)

	_ = c.close(2 * time.Second)

	waitGone(t, "cli", pids.Pid)
	waitGone(t, "grandchild", pids.Child)
}

// TestAgentJob_DaemonCrashKillsTree_Windows: if the process that owns the
// conn (the daemon) dies hard -- here TerminateProcess, which closes its
// handles without running any cleanup -- the CLI and its grandchild die
// too, because the kill-on-close Job Object handle went with it.
func TestAgentJob_DaemonCrashKillsTree_Windows(t *testing.T) {
	dir := t.TempDir()
	agent := buildTestCmd(t, dir)

	daemon := exec.Command(os.Args[0])
	daemon.Env = append(os.Environ(), crashHelperEnv+"="+agent)
	daemon.Stderr = os.Stderr
	stdout, err := daemon.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon subprocess: %v", err)
	}

	var pids treePids
	if err := json.NewDecoder(stdout).Decode(&pids); err != nil {
		_ = daemon.Process.Kill()
		t.Fatalf("read pids from daemon subprocess: %v", err)
	}

	// Hard-kill the "daemon": TerminateProcess, no tree walk, no cleanup.
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(daemon.Process.Pid))
	if err != nil {
		t.Fatalf("open daemon process: %v", err)
	}
	if err := windows.TerminateProcess(h, 1); err != nil {
		t.Fatalf("terminate daemon process: %v", err)
	}
	_ = windows.CloseHandle(h)

	waitGone(t, "cli", pids.Pid)
	waitGone(t, "grandchild", pids.Child)
	_ = daemon.Wait()
}

// crashHelper is the body of the re-exec'd daemon subprocess (see
// crashHelperEnv): it creates a real conn over treeagent, forwards the
// tree pids on stdout, then blocks forever -- the parent test kills it
// with TerminateProcess to prove the job handle's death takes the tree.
func crashHelper(agentPath string) {
	dir, err := os.MkdirTemp("", "smind-codex-crash-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "crashHelper: %v\n", err)
		os.Exit(1)
	}
	pidsPath := filepath.Join(dir, "pids.json")
	// newConn inherits this process's env, so the conn's treeagent
	// learns where to report its pids.
	os.Setenv("SMIND_TREEAGENT_OUT", pidsPath)

	c, err := newConn([]string{agentPath}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crashHelper: newConn: %v\n", err)
		os.Exit(1)
	}
	var pids treePids
	readJSONFileWithDeadline(fatalWriter{}, pidsPath, &pids)
	b, _ := json.Marshal(pids)
	fmt.Println(string(b))
	_ = c     // the conn is never closed on purpose: the daemon is supposed to crash
	select {} // die only via the parent's TerminateProcess
}

type fatalWriter struct{}

func (fatalWriter) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "crashHelper: "+format+"\n", args...)
	os.Exit(1)
}

func (fatalWriter) Helper() {}

// treePids is treeagent's one-file report of its own and its grandchild's
// pid.
type treePids struct {
	Pid   int `json:"pid"`
	Child int `json:"child"`
}

// readJSONFileWithDeadline polls for path to appear (the spawned helper
// writes it once ready) and unmarshals it into v, bounded so a broken
// spawn fails the test instead of hanging it. Accepts any Fatalf-capable
// reporter so the re-exec'd crash-helper subprocess can use it too.
func readJSONFileWithDeadline(t testFatal, path string, v any) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, v); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// testFatal is the subset of *testing.T readJSONFileWithDeadline needs.
type testFatal interface {
	Fatalf(format string, args ...any)
	Helper()
}

// waitGone asserts pid's process is gone within 5 seconds: a handle to a
// dead-but-unreaped process still satisfies the wait, and a pid that no
// longer exists can't be opened at all -- both count as gone.
func waitGone(t *testing.T, label string, pid int) {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	if s, err := windows.WaitForSingleObject(h, 5000); err == nil && s == windows.WAIT_OBJECT_0 {
		return
	}
	t.Fatalf("%s pid %d still alive after 5s", label, pid)
}
