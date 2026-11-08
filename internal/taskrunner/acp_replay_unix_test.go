//go:build !windows

package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A load that outlasts its context ends at the deadline and the agent process is
// gone when the call returns.
func TestReplayACPSessionKillsAgentOnTimeout(t *testing.T) {
	var closed atomic.Int32
	r := replayRunner("loadSession", &closed)
	dir := replayDir(t)
	if err := os.WriteFile(filepath.Join(dir, "replay-hang"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.ReplayACPSession(ctx, ProviderGLM, "fake-session-1", dir, 1<<20)
	if err == nil {
		t.Fatal("a hung load succeeded")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the call outlived its deadline by %v", time.Since(start))
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "agent-pid"))
	if rerr != nil {
		t.Fatalf("the agent never reached session/load: %v", rerr)
	}
	pid, _ := strconv.Atoi(string(raw))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // gone
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent process %d still alive after the call returned", pid)
}
