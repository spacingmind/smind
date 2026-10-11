package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mcpGit runs git in dir and returns trimmed stdout.
func mcpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

// TestMCPTools_TaskNewBaseRef proves task_new's baseRef argument reaches
// the daemon and creates a real worktree at that ref's commit rather than
// the workspace checkout's HEAD.
func TestMCPTools_TaskNewBaseRef(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	repo := newTestRepo(t)
	mcpGit(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	mcpGit(t, bare, "init", "--bare")
	mcpGit(t, repo, "remote", "add", "origin", bare)
	mcpGit(t, repo, "push", "origin", "develop")
	headSHA := mcpGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpGit(t, repo, "add", "second.txt")
	mcpGit(t, repo, "commit", "-m", "second")
	mcpGit(t, repo, "push", "origin", "develop")
	baseSHA := mcpGit(t, repo, "rev-parse", "HEAD")
	mcpGit(t, repo, "reset", "--hard", headSHA)

	var ws struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repo, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}

	var out taskNewOutput
	if callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": ws.ID, "title": "Off develop", "baseRef": "origin/develop"}, &out) {
		t.Fatal("task_new IsError")
	}
	wt, _ := out.Task["WorktreePath"].(string)
	if wt == "" {
		t.Fatalf("task_new output has no WorktreePath: %+v", out.Task)
	}
	if got := mcpGit(t, wt, "rev-parse", "HEAD"); got != baseSHA {
		t.Fatalf("worktree HEAD = %s, want the origin/develop commit %s (not the checkout HEAD %s)", got, baseSHA, headSHA)
	}
	if branch, _ := out.Task["Branch"].(string); !strings.HasPrefix(branch, "smind/task-") {
		t.Fatalf("Branch = %q, want a smind/task-<slug> branch", branch)
	}
}

// TestMCPTools_TaskNewPRNumberZeroRejected proves task_new rejects a
// supplied prNumber <= 0 (the schema makes prNumber optional, so 0 can
// arrive explicitly and must not be silently treated as unset).
func TestMCPTools_TaskNewPRNumberZeroRejected(t *testing.T) {
	env := newMCPSession(t)
	wsID := createTestWorkspace(t, dialTestClient(t, env.srvURL))
	if !callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": wsID, "title": "T", "prNumber": 0}, nil) {
		t.Fatal("task_new with prNumber=0: IsError = false, want rejection")
	}
}
