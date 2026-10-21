//go:build !windows

package probesvc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func pidAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// TestRecover_KillsStaleLaunchProcesses: a launch dir whose pidfile names a
// live arm CLI (a group leader, child of this process, armDir on its command
// line) is killed, with its background child, before the dir is removed.
func TestRecover_KillsStaleLaunchProcesses(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	s := f.svc(nil)
	armDir := filepath.Join(s.armsRoot(), "arm-77-A")
	launch := filepath.Join(s.launchRoot(), "77-A")
	for _, d := range []string{armDir, launch} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	armDir, _ = filepath.EvalSymlinks(armDir)
	pidFile := filepath.Join(launch, "claude.pid")
	childFile := filepath.Join(t.TempDir(), "child")
	script := fmt.Sprintf(`echo $$ > %q; sleep 300 & echo $! > %q; sleep 301`, pidFile, childFile)
	cli := exec.Command("sh", "-c", script, "fake-claude", "--settings", armDir)
	cli.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cli.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cli.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = syscall.Kill(-cli.Process.Pid, syscall.SIGKILL) })
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && child == 0 {
		b, _ := os.ReadFile(childFile)
		child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		time.Sleep(20 * time.Millisecond)
	}
	if child == 0 || !pidAlive(child) {
		t.Fatal("stand-in CLI did not start its child")
	}

	s.Recover(context.Background())

	select {
	case <-reaped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stand-in leader survived Recover")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && pidAlive(child) {
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(child) {
		t.Fatal("the leader's background child survived Recover")
	}
	for _, d := range []string{launch, filepath.Join(s.armsRoot(), "arm-77-A")} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s survived Recover", d)
		}
	}
}

// A pidfile that names an unrelated process is not signalled.
func TestRecover_LeavesUnrelatedPidAlone(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	s := f.svc(nil)
	launch := filepath.Join(s.launchRoot(), "78-A")
	if err := os.MkdirAll(launch, 0o700); err != nil {
		t.Fatal(err)
	}
	other := exec.Command("sleep", "300")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _, _ = other.Process.Wait() })
	if err := os.WriteFile(filepath.Join(launch, "claude.pid"), []byte(strconv.Itoa(other.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Recover(context.Background())
	if !pidAlive(other.Process.Pid) {
		t.Fatal("an unrelated process was killed")
	}
}
