package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestMCPServe_ExitsWhenDaemonConnectionDies pins the daemon-restart
// behavior: `smind mcp serve` must exit non-zero (with the
// lost-connection message on stderr) when its /ws connection to the
// daemon dies, rather than staying up with every tool call failing -- the
// MCP host respawns it against the new daemon (ADR-0017 resolved decision
// 5, read at runtime as well as at startup).
//
// It runs the real binary as a subprocess. The stand-in daemon is an
// httptest server whose /ws accepts the WebSocket upgrade and then drops
// the TCP connection a moment later -- exactly what a daemon restart does
// to an established client connection -- so no wsapi machinery (or MCP
// handshake) is needed to reach the code path under test.
func TestMCPServe_ExitsWhenDaemonConnectionDies(t *testing.T) {
	var upgrader = websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Hold the connection open briefly so the subprocess's dial and
		// MCP startup complete, then break it the way a restart would.
		time.Sleep(300 * time.Millisecond)
		_ = ws.Close()
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	u := srv.URL[len("http://"):] // host:port
	if err := os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte(fmt.Sprintf("server:\n  port: %s\n", portOf(u))), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "smind-test")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build smind: %v: %s", err, out)
	}

	// Never written, never closed: keeps the subprocess's stdin (and so
	// its stdio transport) up until the lost-connection path exits it.
	stdin, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command(bin, "mcp", "serve")
	cmd.Stdin = stdin
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("mcp serve exited 0 when the daemon connection died, want non-zero")
		}
		if want := "lost connection to the smind daemon"; !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("mcp serve still running 15s after the daemon connection died")
	}
}

// portOf splits "host:port" into just the port.
func portOf(hostPort string) string {
	for i := len(hostPort) - 1; i >= 0; i-- {
		if hostPort[i] == ':' {
			return hostPort[i+1:]
		}
	}
	return hostPort
}
