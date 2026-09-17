package main

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
	"github.com/spacingmind/smind/internal/wsclient"
)

// captureStdout runs f with os.Stdout redirected into a buffer and returns
// what it printed. The pattern (pipe + swap + copy after close) is the one
// TestRunAccountAddDispatchesCredentialFromStdin already uses in this
// package; it exists as a helper here because the two task-config-option
// tests below both need to assert on printed output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	prev := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = prev }()

	f()

	if err := write.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, read); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}

// captureStderr is captureStdout for os.Stderr, for asserting the
// daemon's rejection reason reaches the user.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	prev := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = prev }()

	f()

	if err := write.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, read); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return buf.String()
}

// newConfigOptionTestEnv stands up a real wsapi daemon (httptest) with a
// runner whose GLM command points at the compiled
// internal/taskrunner/fakeagent binary, and returns its URL -- the CLI
// tests dial it exactly the way dialDaemon does in production, via a
// config.yaml port. Mirrors the account_test.go setup in this package.
func newConfigOptionTestEnv(t *testing.T, home string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "smind-cli-configopt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	fakeAgent := filepath.Join(dir, "fakeagent")
	buildOut, err := buildFakeAgent(t, fakeAgent)
	if err != nil {
		t.Fatalf("build fakeagent: %v: %s", err, buildOut)
	}

	// Token created before the server (and under this test's SMIND_HOME)
	// so both sides see the same one -- the CLI's dialDaemon reads it from
	// the same location via config.Dir().
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	wm := workspace.New(s)
	handler, err := wsapi.Handler(wm, accounts.New(s), taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeAgent})), s, token)
	if err != nil {
		t.Fatalf("wsapi.Handler() error = %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}

// buildFakeAgent compiles internal/taskrunner/fakeagent to path, returning
// combined output on failure.
func buildFakeAgent(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", path, "../../internal/taskrunner/fakeagent")
	return cmd.CombinedOutput()
}

// TestTaskOptions_PrintsNotSupportedNoteForNonACPProvider proves
// `smind task options <runId>` on a non-ACP provider (claude-native) prints
// the explanatory "no config options" note to stdout rather than a bare
// empty table with no explanation -- the plan's CLI test scenario.
func TestTaskOptions_PrintsNotSupportedNoteForNonACPProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	runID := startTestRun(t, srvURL, "claude-native")

	var code int
	out := captureStdout(t, func() { code = run([]string{"task", "options", runID}) })
	if code != 0 {
		t.Fatalf("run(task options) = %d, want 0 (empty list is not an error); stderr output should have been checked", code)
	}
	if want := "no config options (provider does not support this, or no live session)"; !bytes.Contains([]byte(out), []byte(want)) {
		t.Fatalf("stdout = %q, want it to contain %q", out, want)
	}
}

// TestTaskSetOption_PrintsDaemonRejection proves `smind task set-option`
// on an unknown configId prints the daemon's rejection reason to stderr
// (not a bare non-zero exit with nothing printed) -- the plan's CLI test
// scenario. Drives a real ACP (fake-agent) run held open with the "hang"
// scenario so its session is live, then asks for an option id the agent
// will reject.
func TestTaskSetOption_PrintsDaemonRejection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	runID := startTestRun(t, srvURL, "glm", "hang")

	var code int
	out := captureStderr(t, func() { code = run([]string{"task", "set-option", runID, "no-such-option", "low"}) })
	if code == 0 {
		t.Fatalf("run(task set-option) = 0, want non-zero for a rejected option id (stderr = %q)", out)
	}
	if want := "task set-option:"; !bytes.Contains([]byte(out), []byte(want)) {
		t.Fatalf("stderr = %q, want it to start with the command-prefixed rejection", out)
	}
	if want := "no such config option"; !bytes.Contains([]byte(out), []byte(want)) {
		t.Fatalf("stderr = %q, want it to carry the daemon's rejection reason", out)
	}
}

// writeTestConfig points SMIND_HOME's config.yaml at srvURL's port, the
// same way dialDaemon finds the daemon in production.
func writeTestConfig(t *testing.T, home, srvURL string) {
	t.Helper()
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: "+u.Port()+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// startTestRun starts a real run on the test daemon via run.start over the
// same wsclient the CLI uses, returning its runId. scenarios, if non-empty,
// is written into the task worktree before starting (the fake agent reads
// it to pick its script). The run's task/worktree setup goes through the
// daemon's own task.create so the whole path is end to end.
func startTestRun(t *testing.T, srvURL, provider string, scenarios ...string) string {
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

	// A workspace + task to hang the run on, via the daemon's own RPCs.
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
	if err := client.Call(context.Background(), "workspace.create", map[string]any{"path": repoDir, "title": "W"}, &ws); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(context.Background(), "task.create", map[string]any{"workspaceId": ws.ID, "title": "T"}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}

	if len(scenarios) > 0 && scenarios[0] != "" {
		var got store.Task
		if err := client.Call(context.Background(), "task.get", map[string]any{"id": task.ID}, &got); err != nil {
			t.Fatalf("task.get: %v", err)
		}
		if got.WorktreePath == nil {
			t.Fatal("task.get returned no worktreePath")
		}
		if err := os.WriteFile(filepath.Join(*got.WorktreePath, "scenario"), []byte(scenarios[0]), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
	}

	var start struct {
		RunID string `json:"runId"`
	}
	if err := client.Call(context.Background(), "run.start", map[string]any{"taskId": task.ID, "provider": provider, "prompt": "hi"}, &start); err != nil {
		t.Fatalf("run.start: %v", err)
	}
	if start.RunID == "" {
		t.Fatal("run.start returned no runId")
	}

	// For a "hang" run, wait until its first chunk lands in the run's
	// history -- cheap proxy for "the session exists" (the chunk can only
	// come from a live session).
	if len(scenarios) > 0 && scenarios[0] == "hang" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			var logs struct {
				Events []struct {
					Type string `json:"type"`
				} `json:"events"`
			}
			if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": start.RunID}, &logs); err == nil && len(logs.Events) > 0 {
				return start.RunID
			}
			if time.Now().After(deadline) {
				t.Fatal("hang run never produced its first chunk")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return start.RunID
}
