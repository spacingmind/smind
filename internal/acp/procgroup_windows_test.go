//go:build windows

package acp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestAgentStop_KillsDescendants_Windows: an agent (treeagent) that
// ignores stdin close and has spawned a grandchild is force-killed by
// conn.close's timeout path together with that grandchild -- the Windows
// Job Object equivalent of TestConnClose_KillsGrandchildrenOnTimeout.
func TestAgentStop_KillsDescendants_Windows(t *testing.T) {
	dir := t.TempDir()
	agent := buildTestCmd(t, dir, "treeagent")
	pidsPath := filepath.Join(dir, "pids.json")
	t.Setenv("SMIND_TREEAGENT_OUT", pidsPath)

	c, err := newConn([]string{agent}, nil)
	if err != nil {
		t.Fatalf("newConn() error = %v", err)
	}
	var pids treePids
	readJSONFileWithDeadline(t, pidsPath, &pids)

	_ = c.close(2 * time.Second)

	waitGone(t, "agent", pids.Pid)
	waitGone(t, "grandchild", pids.Child)
}

// TestAgentJob_DaemonCrashKillsTree_Windows: if the process that owns the
// conn (the daemon) dies hard -- here TerminateProcess, which closes its
// handles without running any cleanup -- the agent and its grandchild die
// too, because the kill-on-close Job Object handle went with it.
func TestAgentJob_DaemonCrashKillsTree_Windows(t *testing.T) {
	dir := t.TempDir()
	agent := buildTestCmd(t, dir, "treeagent")

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

	waitGone(t, "agent", pids.Pid)
	waitGone(t, "grandchild", pids.Child)
	_ = daemon.Wait()
}

// crashHelper is the body of the re-exec'd daemon subprocess (see
// crashHelperEnv): it creates a real conn over treeagent, forwards the
// tree pids on stdout, then blocks forever -- the parent test kills it
// with TerminateProcess to prove the job handle's death takes the tree.
func crashHelper(agentPath string) {
	dir, err := os.MkdirTemp("", "smind-acp-crash-")
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
