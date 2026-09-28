package main

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/wsclient"
)

// TestTaskNewLs_ParentFlag proves the CLI half of the task-hierarchy plan
// (docs/plans/active/smind-control-parity.md): `smind task new --parent`
// stamps the child's parent task id, and `smind task ls --parent` narrows
// the listing to that parent's direct children -- both end to end against
// a real daemon, the same newConfigOptionTestEnv setup the task
// config-option CLI tests use.
func TestTaskNewLs_ParentFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID := createCLIHierarchyWorkspace(t, srvURL)

	var code int
	rootOut := captureStdout(t, func() { code = run([]string{"task", "new", strconv.FormatInt(wsID, 10), "root"}) })
	if code != 0 {
		t.Fatalf("run(task new root) = %d, stdout = %q", code, rootOut)
	}
	rootID := firstFieldOfOutputLine(t, rootOut)

	childOut := captureStdout(t, func() {
		code = run([]string{"task", "new", strconv.FormatInt(wsID, 10), "child", "--parent", rootID})
	})
	if code != 0 {
		t.Fatalf("run(task new --parent) = %d, stdout = %q", code, childOut)
	}
	if got := firstFieldOfOutputLine(t, childOut); got == rootID {
		t.Fatalf("task new --parent returned the parent's id %s back", got)
	}

	// An unrelated root-level task must not leak into --parent's listing.
	unrelatedOut := captureStdout(t, func() { code = run([]string{"task", "new", strconv.FormatInt(wsID, 10), "unrelated"}) })
	if code != 0 {
		t.Fatalf("run(task new unrelated) = %d, stdout = %q", code, unrelatedOut)
	}

	lsOut := captureStdout(t, func() { code = run([]string{"task", "ls", strconv.FormatInt(wsID, 10), "--parent", rootID}) })
	if code != 0 {
		t.Fatalf("run(task ls --parent) = %d, stdout = %q", code, lsOut)
	}
	childID := firstFieldOfOutputLine(t, childOut)
	if !bytes.Contains([]byte(lsOut), []byte(childID)) {
		t.Fatalf("task ls --parent output = %q, want it to list child %s", lsOut, childID)
	}
	if bytes.Contains([]byte(lsOut), []byte("unrelated")) {
		t.Fatalf("task ls --parent output = %q, want unrelated tasks excluded", lsOut)
	}
}

// TestTaskNewLs_ParentFlagRejection proves a daemon rejection (here: a
// nonexistent parent id) reaches the CLI user as a printed reason with a
// non-zero exit, not a bare failure.
func TestTaskNewLs_ParentFlagRejection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID := createCLIHierarchyWorkspace(t, srvURL)

	var code int
	out := captureStderr(t, func() {
		code = run([]string{"task", "new", strconv.FormatInt(wsID, 10), "orphan", "--parent", "999999"})
	})
	if code == 0 {
		t.Fatalf("run(task new --parent 999999) = 0, want non-zero (stderr = %q)", out)
	}
	if want := "task new:"; !bytes.Contains([]byte(out), []byte(want)) {
		t.Fatalf("stderr = %q, want it to carry the daemon's rejection (prefix %q)", out, want)
	}

	out = captureStderr(t, func() {
		code = run([]string{"task", "ls", strconv.FormatInt(wsID, 10), "--parent", "999999"})
	})
	if code == 0 {
		t.Fatalf("run(task ls --parent 999999) = 0, want non-zero (stderr = %q)", out)
	}
	if want := "task ls:"; !bytes.Contains([]byte(out), []byte(want)) {
		t.Fatalf("stderr = %q, want it to carry the daemon's rejection (prefix %q)", out, want)
	}
}

// createCLIHierarchyWorkspace registers a git workspace against the test
// daemon and returns its id -- a trimmed-down version of startTestRun's
// workspace setup, without the task/run.
func createCLIHierarchyWorkspace(t *testing.T, srvURL string) int64 {
	t.Helper()

	token, err := auth.LoadOrCreateToken(os.Getenv("SMIND_HOME"))
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client, err := wsclient.Dial(context.Background(), "127.0.0.1:"+u.Port(), token)
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	defer client.Close()

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "test@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "Test").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}

	var ws struct {
		ID int64 `json:"id"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "workspace.create", map[string]any{"path": repoDir, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	return ws.ID
}

// firstFieldOfOutputLine returns the first tab-separated field of the
// first line of out, which task new prints as "<id>\t<title>\t<status>".
func firstFieldOfOutputLine(t *testing.T, out string) string {
	t.Helper()
	for _, line := range bytes.Split([]byte(out), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		return string(bytes.SplitN(line, []byte("\t"), 2)[0])
	}
	t.Fatalf("output %q has no non-empty line to take an id from", out)
	return ""
}
