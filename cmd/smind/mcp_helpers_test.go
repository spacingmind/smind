package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

// The per-tool tests below drive the MCP session from newMCPSession
// against the same fake-agent-backed test daemon the CLI tests use. Since
// every tool is a thin wsapi wrapper, most setup (creating a workspace to
// list tasks in, a task to list chats of) can go through the MCP tools
// themselves; these helpers cover the one thing no tool exposes yet
// (workspace creation) and give the tests typed handles on the wire
// shapes the tools pass through.

// createTestWorkspace creates a git repo with one commit and registers it
// as a workspace over the daemon's own RPCs, returning its id -- the same
// repo/task setup newTestRepoTask performs for the CLI tests, minus the
// task.
func createTestWorkspace(t *testing.T, client *wsclient.Client) int64 {
	t.Helper()
	repo := newTestRepo(t)
	var ws struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repo, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	return ws.ID
}

// newTestRepo is cmd/smind's own minimal git-repo-with-one-commit setup
// (the same sequence newTestRepoTask inlines, without the task).
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "add", "README.md"},
		{"git", "-C", dir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", args, err, out)
		}
	}
	return dir
}

// decodeMCPInto decodes a tool call's structured output into out. The
// tools deliberately pass the daemon's own wire shapes (store.Task,
// store.Chat, ...) through untouched, and those add fields over time, so
// partial-view decoding (a test struct declaring only the fields it
// asserts on) is legal here -- unknown fields are ignored on purpose.
func decodeMCPInto(raw []byte, out any) error {
	return json.Unmarshal(raw, out)
}
