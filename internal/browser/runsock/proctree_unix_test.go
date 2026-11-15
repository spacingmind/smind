//go:build linux || darwin

package runsock

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestSystemProcTreeThisProcess(t *testing.T) {
	tree := SystemProcTree()
	info, err := tree.Info(os.Getpid())
	if err != nil {
		t.Fatalf("Info(os.Getpid()) error = %v, want nil", err)
	}
	if info.PPID != os.Getppid() {
		t.Errorf("Info(os.Getpid()).PPID = %d, want %d", info.PPID, os.Getppid())
	}
	if info.Start == 0 {
		t.Error("Info(os.Getpid()).Start = 0, want non-zero")
	}
	again, err := tree.Info(os.Getpid())
	if err != nil {
		t.Fatalf("second Info(os.Getpid()) error = %v, want nil", err)
	}
	if again.Start != info.Start {
		t.Errorf("Start changed between reads: %d then %d", info.Start, again.Start)
	}
}

// waitGone polls until the kernel forgets the reaped child.
func waitGone(t *testing.T, tree ProcTree, pid int) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if _, err := tree.Info(pid); errors.Is(err, ErrNoProcess) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("Info(%d) still succeeds after Wait, want ErrNoProcess", pid)
}

func TestSystemProcTreeChild(t *testing.T) {
	tree := SystemProcTree()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	child := cmd.Process.Pid

	info, err := tree.Info(child)
	if err != nil {
		t.Fatalf("Info(child) error = %v, want nil", err)
	}
	if info.PPID != os.Getpid() {
		t.Errorf("Info(child).PPID = %d, want %d", info.PPID, os.Getpid())
	}

	selfStart, err := StartOf(tree, os.Getpid())
	if err != nil {
		t.Fatalf("StartOf(self) error = %v, want nil", err)
	}
	in, err := InTree(tree, child, os.Getpid(), selfStart)
	if err != nil {
		t.Fatalf("InTree(child, self) error = %v, want nil", err)
	}
	if !in {
		t.Error("InTree(child, self) = false, want true")
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	waitGone(t, tree, child)
}

func TestSystemProcTreeNotUnderChild(t *testing.T) {
	tree := SystemProcTree()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	child := cmd.Process.Pid

	childStart, err := StartOf(tree, child)
	if err != nil {
		t.Fatalf("StartOf(child) error = %v, want nil", err)
	}
	in, err := InTree(tree, os.Getpid(), child, childStart)
	if err != nil {
		t.Fatalf("InTree(self, child) error = %v, want nil", err)
	}
	if in {
		t.Error("InTree(self, child) = true, want false: this process is not under the child")
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
