package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// fakeAgentPath is the compiled internal/codex/fakeagent binary used by
// every test that needs a live app-server subprocess. Building it once in
// TestMain keeps individual tests fast and avoids depending on a real
// `codex` binary/network access.
var fakeAgentPath string

// crashHelperEnv re-execs the test binary as the "daemon" subprocess of
// TestAgentJob_DaemonCrashKillsTree_Windows (procgroup_windows_test.go):
// its value is the treeagent path to spawn a conn against. Defined here
// so TestMain's guard compiles on every platform.
const crashHelperEnv = "SMIND_CODEX_TEST_CRASH_HELPER"

func TestMain(m *testing.M) {
	if v := os.Getenv(crashHelperEnv); v != "" {
		crashHelper(v)
		return
	}

	dir, err := os.MkdirTemp("", "smind-codex-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeAgentPath = filepath.Join(dir, exeName("fakeagent"))
	build := exec.Command("go", "build", "-o", fakeAgentPath, "./fakeagent")
	build.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fakeagent: %v: %s", err, out))
	}

	os.Exit(m.Run())
}

// newTestClient starts a Client against fakeAgentPath, writing scenario
// (if non-empty) as the "scenario" file in a fresh temp dir -- read by
// fakeagent's runTurnScript, same mechanism as
// internal/taskrunner/fakeagent.
func newTestClient(t *testing.T, scenario string, opts ...Option) (*Client, string) {
	t.Helper()

	cwd := t.TempDir()
	if scenario != "" {
		if err := os.WriteFile(filepath.Join(cwd, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario file: %v", err)
		}
	}

	c, err := New([]string{fakeAgentPath}, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return c, cwd
}
