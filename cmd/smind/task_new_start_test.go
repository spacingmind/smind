package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Tests for `task new`'s start-point flags (--base/--branch/--pr): they
// ride the daemon's task.create ref modes end to end against a real
// daemon, the same setup TestTaskNewLs_ParentFlag uses for --parent.

// createStartOptionWorkspace creates a git repo with one commit on its
// initial branch, registers it as a workspace against the test daemon,
// and returns the workspace id and the repo's path -- the branch juggling
// these tests need happens with plain git in that repo, so unlike
// createCLIHierarchyWorkspace the path has to escape the helper.
func createStartOptionWorkspace(t *testing.T, srvURL string) (int64, string) {
	t.Helper()

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "init"},
		{"git", "-C", repoDir, "config", "user.email", "test@example.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test"},
		{"git", "-C", repoDir, "add", "README.md"},
		{"git", "-C", repoDir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", args, err, out)
		}
	}

	client := dialTestClient(t, srvURL)
	var ws struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repoDir, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	return ws.ID, repoDir
}

// gitRun runs one git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s (in %s): %v: %s", strings.Join(args, " "), dir, err, out)
	}
}

// gitOutput runs one git command in dir and returns its trimmed stdout.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %s (in %s): %v", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(out))
}

// addFeatureXBranch creates a branch feature-x with one extra commit on
// top of HEAD, then checks the repo back out to the branch it started on:
// a branch already checked out in the main repo cannot be checked out
// again in a worktree, so `task new --branch feature-x` needs the main
// checkout left elsewhere.
func addFeatureXBranch(t *testing.T, repoDir string) {
	t.Helper()
	origBranch := gitOutput(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	gitRun(t, repoDir, "checkout", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	gitRun(t, repoDir, "add", "feature.txt")
	gitRun(t, repoDir, "commit", "-m", "feature")
	gitRun(t, repoDir, "checkout", origBranch)
}

// taskNewLabeledLine returns the value after "label\t" in `task new`'s
// output (its branch/worktree lines), failing the test if absent.
func taskNewLabeledLine(t *testing.T, out, label string) string {
	t.Helper()
	prefix := label + "\t"
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("task new output %q has no %q line", out, prefix)
	return ""
}

// TestTaskNewBase proves `--base <ref>` forks a NEW smind/task- branch at
// that ref's commit: the worktree's HEAD is feature-x's commit.
func TestTaskNewBase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID, repoDir := createStartOptionWorkspace(t, srvURL)
	addFeatureXBranch(t, repoDir)

	code, out := runCaptured(t, []string{"task", "new", strconv.FormatInt(wsID, 10), "t1", "--base", "feature-x"})
	if code != 0 {
		t.Fatalf("run(task new --base) = %d, out %q", code, out)
	}
	if branch := taskNewLabeledLine(t, out, "branch"); !strings.HasPrefix(branch, "smind/task-") {
		t.Fatalf("branch line = %q, want a new smind/task- branch", branch)
	}
	worktree := taskNewLabeledLine(t, out, "worktree")
	want := gitOutput(t, repoDir, "rev-parse", "feature-x")
	if got := gitOutput(t, worktree, "rev-parse", "HEAD"); got != want {
		t.Fatalf("worktree HEAD = %s, want feature-x's commit %s", got, want)
	}
}

// TestTaskNewExistingBranch proves `--branch <name>` checks out the
// existing branch itself (no new branch): the branch line is exactly the
// name passed.
func TestTaskNewExistingBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID, repoDir := createStartOptionWorkspace(t, srvURL)
	addFeatureXBranch(t, repoDir)

	code, out := runCaptured(t, []string{"task", "new", strconv.FormatInt(wsID, 10), "t2", "--branch", "feature-x"})
	if code != 0 {
		t.Fatalf("run(task new --branch) = %d, out %q", code, out)
	}
	if got := taskNewLabeledLine(t, out, "branch"); got != "feature-x" {
		t.Fatalf("branch line = %q, want exactly %q", got, "feature-x")
	}
}

// TestTaskNewStartFlagOrder proves the start flags mix with --parent in
// either order after the title.
func TestTaskNewStartFlagOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID, repoDir := createStartOptionWorkspace(t, srvURL)
	addFeatureXBranch(t, repoDir)
	id := strconv.FormatInt(wsID, 10)

	code, rootOut := runCaptured(t, []string{"task", "new", id, "root"})
	if code != 0 {
		t.Fatalf("run(task new root) = %d, out %q", code, rootOut)
	}
	rootID := firstFieldOfOutputLine(t, rootOut)

	for _, args := range [][]string{
		{"task", "new", id, "t3", "--parent", rootID, "--base", "feature-x"},
		{"task", "new", id, "t4", "--base", "feature-x", "--parent", rootID},
	} {
		code, out := runCaptured(t, args)
		if code != 0 {
			t.Fatalf("run(%v) = %d, out %q", args, code, out)
		}
		if branch := taskNewLabeledLine(t, out, "branch"); !strings.HasPrefix(branch, "smind/task-") {
			t.Fatalf("run(%v): branch line = %q, want a new smind/task- branch", args, branch)
		}
	}
}

// TestTaskNewStartFlagRejection pins the argument contract: more than one
// start flag and a non-positive/non-numeric --pr are usage errors (exit
// 2) before anything dials, and a trailing --pr with no value simply
// stays title text, same as a dangling --space.
func TestTaskNewStartFlagRejection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID, _ := createStartOptionWorkspace(t, srvURL)
	id := strconv.FormatInt(wsID, 10)

	for _, args := range [][]string{
		{"task", "new", id, "x", "--base", "a", "--branch", "b"},
		{"task", "new", id, "x", "--base", "a", "--pr", "1"},
		{"task", "new", id, "x", "--branch", "b", "--pr", "1"},
	} {
		code := -1
		stderr := captureStderr(t, func() { captureStdout(t, func() { code = run(args) }) })
		if code != 2 || !strings.Contains(stderr, "at most one of") {
			t.Errorf("run(%v) = %d, stderr %q; want 2 mentioning %q", args, code, stderr, "at most one of")
		}
	}

	for _, pr := range []string{"0", "abc"} {
		args := []string{"task", "new", id, "x", "--pr", pr}
		code := -1
		stderr := captureStderr(t, func() { captureStdout(t, func() { code = run(args) }) })
		if code != 2 || !strings.Contains(stderr, "task new: invalid --pr value") {
			t.Errorf("run(%v) = %d, stderr %q; want 2 with an invalid --pr value message", args, code, stderr)
		}
	}

	code, out := runCaptured(t, []string{"task", "new", id, "t5", "--pr"})
	if code != 0 {
		t.Fatalf("run(task new t5 --pr) = %d, out %q; want 0 (a valueless --pr is title text)", code, out)
	}
	if !strings.Contains(out, "t5 --pr") {
		t.Fatalf("task new output = %q, want the title to keep the stray --pr", out)
	}
}
