package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// freePort asks the OS for a currently unused TCP port on 127.0.0.1, for
// a test daemon to listen on (the listen-then-close race is the same one
// every such test accepts; the window is a single test's lifetime).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen :0: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// writePortConfig writes a minimal config.yaml with just the daemon port,
// the same shape writeTestConfig produces for the httptest-based CLI
// tests, but for a port no in-process server holds.
func writePortConfig(t *testing.T, home string, port int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("server:\n  port: "+strconv.Itoa(port)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// buildSmind compiles this package's binary once per test (the go build
// cache makes the second and later builds cheap), the way
// TestMCPServe_StdioRoundTrip does.
func buildSmind(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), exeName("smind-serve-test"))
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build smind: %v: %s", err, out)
	}
	return bin
}

// smindEnv returns the subprocess environment that pins SMIND_HOME to
// home, matching the parent test's t.Setenv for child processes.
func smindEnv(home string) []string {
	return append(os.Environ(), "SMIND_HOME="+home)
}

// waitDaemonHealthy polls the daemon's unauthenticated /healthz until it
// answers, so a test never races the serve subprocess's startup.
func waitDaemonHealthy(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	for {
		resp, err := http.Get(url) //nolint:gosec // fixed test port
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon on port %d never became healthy within %s", port, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// newShutdownRepo is the graceful-shutdown tests' git repo: a temp dir
// with core.autocrlf pinned false (the daemon's worktree add would
// otherwise be subject to the checkout's autocrlf) and one commit.
func newShutdownRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "-C", repo, "init"},
		{"git", "-C", repo, "config", "core.autocrlf", "false"},
		{"git", "-C", repo, "config", "user.email", "test@example.com"},
		{"git", "-C", repo, "config", "user.name", "Test"},
		{"git", "-C", repo, "add", "README.md"},
		{"git", "-C", repo, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", args, err, out)
		}
	}
	return repo
}

// dialShutdownDaemon dials a serve subprocess's /ws with the token under
// home -- the same shape dialTestClient uses for the httptest daemons.
func dialShutdownDaemon(t *testing.T, home string, port int) *wsclient.Client {
	t.Helper()
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	client, err := wsclient.Dial(context.Background(), fmt.Sprintf("127.0.0.1:%d", port), token)
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// openHomeStore reopens the smind.db a serve subprocess left behind in
// home, to assert on what its shutdown path actually persisted.
func openHomeStore(t *testing.T, home string) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// stdBuffer is a minimal sink for a subprocess's combined output, so
// failure messages can show the daemon's own log.
type stdBuffer struct {
	data []byte
}

func (b *stdBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *stdBuffer) String() string { return string(b.data) }

// TestServe_GracefulShutdown is AC3's mandatory scenario: a real
// `smind serve` subprocess with a live terminal session, stopped by the
// real `smind stop` (ADR-0028 D2's POST /api/shutdown), must run the same
// shutdown path as SIGTERM -- the daemon process exits and the terminal
// session is persisted `closed`, not `interrupted`.
func TestServe_GracefulShutdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	port := freePort(t)
	writePortConfig(t, home, port)
	bin := buildSmind(t)

	serve := exec.Command(bin, "serve")
	serve.Env = smindEnv(home)
	var serveOut stdBuffer
	serve.Stdout = &serveOut
	serve.Stderr = &serveOut
	if err := serve.Start(); err != nil {
		t.Fatalf("start smind serve: %v", err)
	}
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = serve.Process.Kill()
		}
		_, _ = serve.Process.Wait()
	})

	waitDaemonHealthy(t, port, 20*time.Second)

	// Workspace + task via the daemon's own RPCs, then a terminal session
	// in the task's worktree -- the thing whose `closed` (not
	// `interrupted`) persistence is the whole point of a graceful stop.
	client := dialShutdownDaemon(t, home, port)
	ctx := context.Background()
	var wsID struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(ctx, "workspace.create", map[string]any{"path": newShutdownRepo(t), "title": "W"}, &wsID); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	var task struct {
		ID int64 `json:"id"`
	}
	if err := client.Call(ctx, "task.create", map[string]any{"workspaceId": wsID.ID, "title": "T"}, &task); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	var term struct {
		TerminalID string `json:"terminalId"`
	}
	if err := client.Call(ctx, "terminal.create", map[string]any{"taskId": task.ID}, &term); err != nil {
		t.Fatalf("terminal.create: %v", err)
	}
	if term.TerminalID == "" {
		t.Fatal("terminal.create returned an empty session id")
	}

	// The real `smind stop` against the real daemon, same SMIND_HOME.
	stop := exec.Command(bin, "stop")
	stop.Env = smindEnv(home)
	stopOut, err := stop.CombinedOutput()
	if err != nil {
		t.Fatalf("smind stop: %v: %s (serve log: %s)", err, stopOut, serveOut.String())
	}

	waitProc := make(chan error, 1)
	go func() { waitProc <- serve.Wait() }()
	select {
	case <-waitProc:
		exited = true
	case <-time.After(30 * time.Second):
		t.Fatalf("daemon still running 30s after smind stop (serve log: %s)", serveOut.String())
	}

	row, err := openHomeStore(t, home).GetTerminalSession(term.TerminalID)
	if err != nil {
		t.Fatalf("GetTerminalSession(%q) error = %v", term.TerminalID, err)
	}
	if row.Status != "closed" {
		t.Fatalf("terminal session status after smind stop = %q, want %q", row.Status, "closed")
	}
}

// TestStop_NotRunning: `smind stop` with a configured port nothing
// listens on exits non-zero with a message that says what's wrong.
func TestStop_NotRunning(t *testing.T) {
	home := t.TempDir()
	writePortConfig(t, home, freePort(t))
	if _, err := auth.LoadOrCreateToken(home); err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	bin := buildSmind(t)

	stop := exec.Command(bin, "stop")
	stop.Env = smindEnv(home)
	out, err := stop.CombinedOutput()
	if err == nil {
		t.Fatalf("smind stop against no daemon: exit 0, want non-zero (output: %s)", out)
	}
	if want := "serve"; !strings.Contains(string(out), want) {
		t.Errorf("smind stop output = %q, want it to mention %q", out, want)
	}
}

// TestStop_WrongToken: a stop whose token file doesn't match the running
// daemon's is rejected (401) and exits non-zero -- the shutdown endpoint
// must be unreachable without the credential even though the port is.
func TestStop_WrongToken(t *testing.T) {
	daemonHome := t.TempDir()
	stopHome := t.TempDir()
	port := freePort(t)
	writePortConfig(t, daemonHome, port)
	writePortConfig(t, stopHome, port)
	if _, err := auth.LoadOrCreateToken(daemonHome); err != nil {
		t.Fatalf("LoadOrCreateToken(daemon) error = %v", err)
	}
	if _, err := auth.LoadOrCreateToken(stopHome); err != nil {
		t.Fatalf("LoadOrCreateToken(stop) error = %v", err)
	}
	bin := buildSmind(t)

	serve := exec.Command(bin, "serve")
	serve.Env = smindEnv(daemonHome)
	if err := serve.Start(); err != nil {
		t.Fatalf("start smind serve: %v", err)
	}
	t.Cleanup(func() {
		_ = serve.Process.Kill()
		_, _ = serve.Process.Wait()
	})
	waitDaemonHealthy(t, port, 20*time.Second)

	stop := exec.Command(bin, "stop")
	stop.Env = smindEnv(stopHome)
	out, err := stop.CombinedOutput()
	if err == nil {
		t.Fatalf("smind stop with wrong token: exit 0, want non-zero (output: %s)", out)
	}

	// The daemon must still be running afterwards.
	resp, herr := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port)) //nolint:gosec // fixed test port
	if herr != nil {
		t.Fatalf("daemon not answering after a rejected stop: %v", herr)
	}
	_ = resp.Body.Close()
}
