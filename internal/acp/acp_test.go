package acp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeAgentPath is the compiled internal/acp/fakeagent binary used by every
// test that needs a live ACP subprocess. Building it once in TestMain
// keeps individual tests fast and avoids depending on npx/network access.
var fakeAgentPath string

// crashHelperEnv re-execs the test binary as the "daemon" subprocess of
// TestAgentJob_DaemonCrashKillsTree_Windows (procgroup_windows_test.go):
// its value is the treeagent path to spawn a conn against. Defined here
// so TestMain's guard compiles on every platform.
const crashHelperEnv = "SMIND_ACP_TEST_CRASH_HELPER"

func TestMain(m *testing.M) {
	if v := os.Getenv(crashHelperEnv); v != "" {
		crashHelper(v)
		return
	}

	dir, err := os.MkdirTemp("", "smind-acp-test-")
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
