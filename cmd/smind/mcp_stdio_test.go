package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServe_StdioRoundTrip is the plan's dedicated MCP protocol
// round-trip scenario (step 7): unlike every other MCP test in this
// package, which drives newMCPServer directly over an in-memory transport,
// this one runs the actual compiled `smind mcp serve` binary as a
// subprocess and speaks real newline-delimited-JSON stdio to it (the SDK's
// CommandTransport), against a real in-process fake-agent daemon -- the
// same framing/schema path an orchestrating agent's MCP client uses in
// production, catching subprocess/stdio-wiring bugs the in-memory-transport
// tests can't.
func TestMCPServe_StdioRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	client := dialTestClient(t, srvURL)
	wsID := createTestWorkspace(t, client)

	bin := filepath.Join(t.TempDir(), "smind-test")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build smind: %v: %s", err, out)
	}

	// cmd.Env is left nil, so the subprocess inherits this process's
	// environment -- including the SMIND_HOME t.Setenv set above -- the
	// same way TestMCPServe_ExitsWhenDaemonConnectionDies's subprocess does.
	cmd := exec.Command(bin, "mcp", "serve")

	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("Connect() (initialize) error = %v", err)
	}
	defer func() { _ = cs.Close() }()

	// tools/list.
	names := mcpToolNames(t, cs)
	found := false
	for _, n := range names {
		if n == "task_new" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tools/list over stdio = %v, want it to contain task_new", names)
	}

	// tools/call: task_new against the real daemon, over real stdio.
	var created struct {
		Task taskForTest `json:"task"`
	}
	if isErr := callMCPTool(t, cs, "task_new", map[string]any{
		"workspaceId": wsID, "title": "Stdio",
	}, &created); isErr {
		t.Fatal("task_new over stdio returned an error result")
	}
	if created.Task.ID == 0 || created.Task.Title != "Stdio" {
		t.Fatalf("task_new over stdio = %+v, want a created task", created.Task)
	}
}
