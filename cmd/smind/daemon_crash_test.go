package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
)

// TestDaemonLogCrashHelperProcess is the child side of the re-exec test
// below. It does nothing unless SMIND_CRASH_HELPER is set.
func TestDaemonLogCrashHelperProcess(t *testing.T) {
	if os.Getenv("SMIND_CRASH_HELPER") != "daemonlog" {
		return
	}
	closeLog := setupDaemonLog(config.LogConfig{MaxSizeMB: 1, MaxFiles: 3}, io.Discard)
	defer closeLog()
	panic("boom-crash-test")
}

func TestDaemonLogCrashLandsInDaemonLog(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonLogCrashHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(),
		"SMIND_CRASH_HELPER=daemonlog",
		"SMIND_HOME="+home,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("the panicking child must exit non-zero")
	}
	b, err := os.ReadFile(filepath.Join(home, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "panic: ") {
		t.Errorf("daemon.log has no panic line: %q", got)
	}
	if !strings.Contains(got, "goroutine ") {
		t.Errorf("daemon.log has no goroutine trace: %q", got)
	}
}
