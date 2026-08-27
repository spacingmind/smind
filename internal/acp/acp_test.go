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

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-acp-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeAgentPath = filepath.Join(dir, "fakeagent")
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
