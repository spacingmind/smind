//go:build unix

package acp

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestConnClose_KillsGrandchildrenOnTimeout: an agent that ignores stdin
// close and has spawned a grandchild (like an MCP server's npx -> node)
// is force-killed on the close timeout together with that grandchild.
func TestConnClose_KillsGrandchildrenOnTimeout(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	c, err := newConn([]string{"sh", "-c", `sleep 300 & echo $! > "$1"; wait`, "sh", pidFile}, nil)
	if err != nil {
		t.Fatalf("newConn() error = %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				pid = n
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("grandchild pid never written")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	_ = c.close(200 * time.Millisecond)

	for time.Now().Before(deadline.Add(5 * time.Second)) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive after close timeout", pid)
}
