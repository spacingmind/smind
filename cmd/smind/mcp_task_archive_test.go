package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTaskViaMCP creates a task through task_new in a fresh workspace,
// returning the workspace id and the created task's wire fields.
func createTaskViaMCP(t *testing.T, env *mcpTestEnv, title string) (wsID int64, wtPath, branch string) {
	t.Helper()
	client := dialTestClient(t, env.srvURL)
	wsID = createTestWorkspace(t, client)
	var out taskNewOutput
	if callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": wsID, "title": title}, &out) {
		t.Fatal("task_new IsError")
	}
	wtPath, _ = out.Task["WorktreePath"].(string)
	branch, _ = out.Task["Branch"].(string)
	if wtPath == "" || branch == "" {
		t.Fatalf("task_new output missing WorktreePath/Branch: %+v", out.Task)
	}
	return wsID, wtPath, branch
}

// TestMCPTools_TaskArchivePreservesUncommittedWork proves task_archive on
// a dirty worktree lands the uncommitted file on the branch as the
// checkpoint commit and removes the worktree directory.
func TestMCPTools_TaskArchivePreservesUncommittedWork(t *testing.T) {
	env := newMCPSession(t)
	_, wtPath, branch := createTaskViaMCP(t, env, "Dirty work")

	if err := os.WriteFile(filepath.Join(wtPath, "uncommitted.txt"), []byte("precious work\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted file: %v", err)
	}

	var out taskArchiveOutput
	if callMCPTool(t, env.cs, "task_archive", map[string]any{"taskId": taskIDOf(t, env, branch)}, &out) {
		t.Fatal("task_archive IsError")
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree dir %s still exists (err=%v), want it removed", wtPath, err)
	}

	// The checkpoint commit carrying the file must be reachable on the
	// branch. Use the daemon's own client to find the workspace repo.
	repo := workspaceRepoForBranch(t, env, branch)
	if got := mcpGit(t, repo, "show", branch+":uncommitted.txt"); got != "precious work" {
		t.Fatalf("branch %s does not contain the uncommitted work: %q", branch, got)
	}
	if subject := mcpGit(t, repo, "log", "-1", "--format=%s", branch); !strings.HasPrefix(subject, "smind: checkpoint") {
		t.Fatalf("branch tip subject = %q, want the checkpoint commit", subject)
	}
}

// TestMCPTools_TaskArchiveCheckpointFailureKeepsWorktree proves a failing
// checkpoint (a failing pre-commit hook) aborts the archive leaving both
// the worktree directory and the task intact.
func TestMCPTools_TaskArchiveCheckpointFailureKeepsWorktree(t *testing.T) {
	env := newMCPSession(t)
	_, wtPath, branch := createTaskViaMCP(t, env, "Hooked")
	taskID := taskIDOf(t, env, branch)

	// A pre-commit hook that always fails makes the checkpoint commit
	// fail, which must abort the archive before any removal.
	// Hooks live in the common git dir, which a worktree shares.
	commonDir := mcpGit(t, wtPath, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(wtPath, commonDir)
	}
	if err := os.MkdirAll(filepath.Join(commonDir, "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commonDir, "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	if !callMCPTool(t, env.cs, "task_archive", map[string]any{"taskId": taskID}, nil) {
		t.Fatal("task_archive with a failing checkpoint: IsError = false, want the failure surfaced")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree dir %s gone after a failed checkpoint, want it kept: %v", wtPath, err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "dirty.txt")); err != nil {
		t.Fatalf("dirty.txt gone: %v", err)
	}

	// The task must still be there (task_get succeeds, status not archived).
	var task map[string]any
	client := dialTestClient(t, env.srvURL)
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
		t.Fatalf("task.get after failed archive: %v, want the row intact", err)
	}
	if st, _ := task["Status"].(string); st == "archived" {
		t.Fatalf("task.Status = %q after a failed checkpoint, want it not archived", st)
	}
}

// taskIDOf looks the task id up via task_list, matching by branch.
func taskIDOf(t *testing.T, env *mcpTestEnv, branch string) int64 {
	t.Helper()
	client := dialTestClient(t, env.srvURL)
	var wsList []struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.list", nil, &wsList); err != nil {
		t.Fatalf("workspace.list: %v", err)
	}
	for _, w := range wsList {
		var out taskListOutput
		if callMCPTool(t, env.cs, "task_list", map[string]any{"workspaceId": w.ID}, &out) {
			continue
		}
		for _, task := range out.Tasks {
			if b, _ := task["Branch"].(string); b == branch {
				id, _ := task["ID"].(float64)
				return int64(id)
			}
		}
	}
	t.Fatalf("no task with branch %q", branch)
	return 0
}

// workspaceRepoForBranch finds the workspace repo path whose git dir holds
// the branch (there is exactly one workspace in these tests).
func workspaceRepoForBranch(t *testing.T, env *mcpTestEnv, branch string) string {
	t.Helper()
	client := dialTestClient(t, env.srvURL)
	var wsList []struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if err := client.Call(context.Background(), "workspace.list", nil, &wsList); err != nil {
		t.Fatalf("workspace.list: %v", err)
	}
	for _, w := range wsList {
		if repoHasRef(t, w.Path, branch) {
			return w.Path
		}
	}
	t.Fatalf("no workspace repo holds branch %q", branch)
	return ""
}

func repoHasRef(t *testing.T, dir, ref string) bool {
	t.Helper()
	return mcpGit(t, dir, "rev-parse", "--verify", "--quiet", ref) != ""
}
