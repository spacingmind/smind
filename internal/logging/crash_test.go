package logging

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runCrashHelper runs the test binary as a child that (depending on
// SMIND_CRASH_HELPER) installs crash output and panics; returns the child's
// exit error.
func runCrashHelper(t *testing.T, mode string, extraEnv ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "SMIND_CRASH_HELPER="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// TestCrashHelperProcess is the child side of the re-exec tests above. It
// does nothing unless SMIND_CRASH_HELPER is set.
func TestCrashHelperProcess(t *testing.T) {
	mode := os.Getenv("SMIND_CRASH_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "plain":
		path := os.Getenv("SMIND_CRASH_LOG")
		if _, err := InstallCrashOutput(path); err != nil {
			t.Fatal(err)
		}
	case "rotate":
		path := os.Getenv("SMIND_CRASH_LOG")
		rot, err := OpenRotating(path, 200, 3)
		if err != nil {
			t.Fatal(err)
		}
		co, err := InstallCrashOutput(path)
		if err != nil {
			t.Fatal(err)
		}
		rot.OnRotate(func() { _ = co.Reopen() })
		line := strings.Repeat("x", 49) + "\n"
		for i := 0; i < 20; i++ {
			if _, err := rot.Write([]byte(line)); err != nil {
				t.Fatal(err)
			}
		}
	}
	panic("boom-crash-test")
}

func TestCrashOutputLandsInTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	err := runCrashHelper(t, "plain", "SMIND_CRASH_LOG="+path)
	if err == nil {
		t.Fatal("the panicking child must exit non-zero")
	}
	got := readFile(t, path)
	if !strings.Contains(got, "panic: boom-crash-test") {
		t.Errorf("daemon.log has no panic line: %q", got)
	}
	if !strings.Contains(got, "goroutine ") {
		t.Errorf("daemon.log has no goroutine trace: %q", got)
	}
}

func TestCrashOutputFollowsRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	err := runCrashHelper(t, "rotate", "SMIND_CRASH_LOG="+path)
	if err == nil {
		t.Fatal("the panicking child must exit non-zero")
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("no rotation happened (%v): daemon.log = %q", err, readFile(t, path))
	}
	got := readFile(t, path)
	if !strings.Contains(got, "panic: ") {
		t.Errorf("the ACTIVE daemon.log must hold the crash trace, got %q", got)
	}
}
