package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The behavioural counterpart of Test_initiator_marks_every_human_rpc_
// manager_call: the human RPC handlers are called for real against a real
// task worktree whose repository is armed (a pre-commit hook, a clean
// filter), and the marker proves the person's own git ran. These tests use
// t.Setenv, so none of them calls t.Parallel.

// isolateGitEnv keeps the machine's own git configuration out of the test.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// commonGitDir returns the repository's common git dir for a worktree.
func commonGitDir(t *testing.T, wt string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = wt
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse --git-common-dir: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// callHandler runs a handler as a normal (not run-scoped) caller.
func callHandler(t *testing.T, h handlerFunc, params map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h(context.Background(), &requestContext{runScoped: false}, raw)
	return err
}

func Test_task_commit_handler_runs_the_pre_commit_hook_for_a_human_caller(t *testing.T) {
	isolateGitEnv(t)
	wm, _ := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	wt := *task.WorktreePath
	common := commonGitDir(t, wt)
	runGitT(t, wt, "config", "commit.gpgsign", "false")
	runGitT(t, wt, "config", "user.email", "test@example.com")
	runGitT(t, wt, "config", "user.name", "Test")

	marker := filepath.Join(t.TempDir(), "hook.ran")
	hook := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 0\n", marker)
	if err := os.MkdirAll(filepath.Join(common, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "hooks", "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, wt, "add", "README.md")

	err := callHandler(t, handleTaskCommit(wm), map[string]any{
		"taskId": task.ID, "message": "human commit", "author": "human",
	})
	if err != nil {
		t.Fatalf("task.commit: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the repository's pre-commit hook did not run for a human caller: %v", err)
	}
}

func Test_task_stage_handler_runs_the_clean_filter_for_a_human_caller(t *testing.T) {
	isolateGitEnv(t)
	wm, _ := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	wt := *task.WorktreePath
	common := commonGitDir(t, wt)

	marker := filepath.Join(t.TempDir(), "filter.ran")
	script := filepath.Join(t.TempDir(), "clean.sh")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\ntouch %q\ncat\n", marker)), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitT(t, wt, "config", "filter.x.clean", script)
	if err := os.MkdirAll(filepath.Join(common, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "info", "attributes"), []byte("*.flt filter=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "a.flt"), []byte("filtered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := callHandler(t, handleTaskStage(wm), map[string]any{
		"taskId": task.ID, "path": "a.flt", "staged": true,
	})
	if err != nil {
		t.Fatalf("task.stage: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the repository's clean filter did not run for a human caller: %v", err)
	}
}
