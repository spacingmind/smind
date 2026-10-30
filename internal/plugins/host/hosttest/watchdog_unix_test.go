//go:build unix

package hosttest

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if Main() {
		return
	}
	os.Exit(m.Run())
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// TestHelpers_DieWithTestBinary proves no helper outlives its test binary:
// a stand-in test binary (the parent) starts an ignore-term helper and its
// immune grandchild, then is killed the hard way, the way a timeout or a
// crash ends a go test run. Both descendants, which ignore SIGTERM, must be
// gone shortly after.
func TestHelpers_DieWithTestBinary(t *testing.T) {
	parent := exec.Command(os.Args[0], Flag, "parent-of-ignore-term")
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	var self, helper, grandchild int
	t.Cleanup(func() {
		// Whatever happens below, nothing is left behind.
		for _, pid := range []int{helper, grandchild, parent.Process.Pid} {
			if pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		_ = parent.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("the stand-in parent never reported pids: %v", err)
	}
	if _, err := fmt.Sscanf(line, "pids %d %d %d", &self, &helper, &grandchild); err != nil {
		t.Fatalf("bad pids line %q: %v", line, err)
	}
	if !alive(helper) || !alive(grandchild) {
		t.Fatalf("precondition: helper %d and grandchild %d must be alive", helper, grandchild)
	}
	if err := parent.Process.Kill(); err != nil { // SIGKILL: no cleanup runs
		t.Fatal(err)
	}
	_ = parent.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for alive(helper) || alive(grandchild) {
		if time.Now().After(deadline) {
			t.Fatalf("helper %d (alive=%v) or grandchild %d (alive=%v) survived its test binary", helper, alive(helper), grandchild, alive(grandchild))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
