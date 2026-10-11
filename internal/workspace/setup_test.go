//go:build !windows

package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSetup_RunsInWorktreeWithEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, err := runScript(context.Background(), dir, `pwd; echo "$SMIND_TASK_ID:$SMIND_PORT"; echo err >&2`,
		[]string{"SMIND_TASK_ID=7", "SMIND_PORT=31001"}, 10*time.Second, setupLogCap)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(out, real) || !strings.Contains(out, "7:31001") || !strings.Contains(out, "err") {
		t.Fatalf("out = %q", out)
	}
}

func TestSetup_FailureKeepsWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, err := runScript(context.Background(), dir, `echo hi; exit 3`, nil, 10*time.Second, setupLogCap)
	if err == nil || !strings.Contains(out, "hi") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, serr := os.Stat(dir); serr != nil {
		t.Fatalf("dir gone: %v", serr)
	}
}

func TestSetup_TimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	start := time.Now()
	_, err := runScript(context.Background(), dir, `sleep 300 & echo $! > child.pid; wait`, nil, 500*time.Millisecond, setupLogCap)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
	t.Fatalf("background child %d survived", pid)
}

func TestSetup_LogTailCapped(t *testing.T) {
	t.Parallel()
	out, err := runScript(context.Background(), t.TempDir(),
		`i=0; while [ $i -lt 3000 ]; do echo "line-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"; i=$((i+1)); done; echo THE_END`,
		nil, 20*time.Second, setupLogCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > setupLogCap || !strings.HasSuffix(strings.TrimSpace(out), "THE_END") || strings.Contains(out, "line-0-") {
		t.Fatalf("len=%d tail=%q", len(out), out[max(0, len(out)-30):])
	}
}

func TestSetup_EnvNotInterpolated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	branch := "x$(touch SENTINEL)"
	out, err := runScript(context.Background(), dir, `echo "$SMIND_BRANCH"`, []string{"SMIND_BRANCH=" + branch}, 10*time.Second, setupLogCap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, branch) {
		t.Fatalf("out = %q", out)
	}
	if _, serr := os.Stat(filepath.Join(dir, "SENTINEL")); serr == nil {
		t.Fatal("env value was interpolated: SENTINEL exists")
	}
}
