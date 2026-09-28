package main

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// newTestMcpDaemon mirrors newTestProfileDaemon's setup exactly: a real
// wsapi.Handler-backed test server, with dialDaemon pointed at it via a
// written config.yaml. Returns the store (for asserting on stored servers
// directly).
func newTestMcpDaemon(t *testing.T) (home string, s *store.Store) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("SMIND_HOME", home)

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	registry := accounts.New(s)
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	handler, err := wsapi.Handler(workspace.New(s), registry, nil, s, token)
	if err != nil {
		t.Fatalf("wsapi.Handler() error = %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: "+u.Port()+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home, s
}

func TestRunMcpAddPrintsCreatedRow(t *testing.T) {
	_, s := newTestMcpDaemon(t)

	var code int
	out := captureStdout(t, func() {
		code = run([]string{
			"mcp", "add", "playwright", "stdio",
			"--command", "npx", "--arg", "-y", "--arg", "@playwright/mcp@latest",
			"--env", "TOKEN=top-secret",
		})
	})
	if code != 0 {
		t.Fatalf("run(mcp add) = %d, want 0; stdout: %s", code, out)
	}
	if !bytes.Contains([]byte(out), []byte("playwright")) {
		t.Fatalf("stdout = %q, want it to contain the created server's name", out)
	}
	if bytes.Contains([]byte(out), []byte("top-secret")) {
		t.Fatalf("stdout = %q, must never contain the raw secret", out)
	}

	list, err := mcpservers.New(s).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "playwright" || list[0].Transport != "stdio" || list[0].Command != "npx" {
		t.Fatalf("stored servers = %+v, want one playwright/stdio/npx server", list)
	}
	if list[0].Args != `["-y","@playwright/mcp@latest"]` {
		t.Fatalf("stored args = %q, want the canonical JSON array", list[0].Args)
	}
	if list[0].Env != `{"TOKEN":"top-secret"}` {
		t.Fatalf("stored env = %q, want the real secret preserved server-side", list[0].Env)
	}
}

func TestRunMcpAddInvalidTransportExitsNonzero(t *testing.T) {
	newTestMcpDaemon(t)

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"mcp", "add", "x", "not-a-real-transport"})
	})
	if code == 0 {
		t.Fatalf("run(mcp add, bad transport) = 0, want nonzero; stdout: %s", out)
	}
}

func TestRunMcpLsShowsRedactedValues(t *testing.T) {
	_, s := newTestMcpDaemon(t)

	if _, err := mcpservers.New(s).Create(store.McpServer{
		Name: "secretive", Transport: "stdio", Command: "npx",
		Env: `{"TOKEN":"do-not-leak"}`,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"mcp", "ls"})
	})
	if code != 0 {
		t.Fatalf("run(mcp ls) = %d, want 0", code)
	}
	if !bytes.Contains([]byte(out), []byte("secretive")) {
		t.Fatalf("mcp ls output = %q, want it to list the added server", out)
	}
	if !bytes.Contains([]byte(out), []byte("TOKEN=")) {
		t.Fatalf("mcp ls output = %q, want the TOKEN key to still be shown", out)
	}
	if bytes.Contains([]byte(out), []byte("do-not-leak")) {
		t.Fatalf("mcp ls output = %q, must never contain the raw secret value", out)
	}
}

func TestRunMcpRmRemovesServer(t *testing.T) {
	_, s := newTestMcpDaemon(t)

	var code int
	addOut := captureStdout(t, func() {
		code = run([]string{"mcp", "add", "throwaway", "stdio", "--command", "npx"})
	})
	if code != 0 {
		t.Fatalf("run(mcp add) = %d, want 0", code)
	}
	list, err := mcpservers.New(s).List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %+v, %v, want one server; add output: %s", list, err, addOut)
	}
	id := list[0].ID

	if code := run([]string{"mcp", "rm", strconv.FormatInt(id, 10)}); code != 0 {
		t.Fatalf("run(mcp rm) = %d, want 0", code)
	}

	after, err := mcpservers.New(s).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("stored servers after rm = %+v, want none", after)
	}
}

func TestRunMcpEnableDisableToggles(t *testing.T) {
	_, s := newTestMcpDaemon(t)

	if code := run([]string{"mcp", "add", "toggle", "stdio", "--command", "npx"}); code != 0 {
		t.Fatalf("run(mcp add) = %d, want 0", code)
	}
	list, err := mcpservers.New(s).List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %+v, %v, want one server", list, err)
	}
	id := strconv.FormatInt(list[0].ID, 10)

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"mcp", "disable", id})
	})
	if code != 0 {
		t.Fatalf("run(mcp disable) = %d, want 0; stdout: %s", code, out)
	}
	after, err := mcpservers.New(s).List()
	if err != nil || len(after) != 1 || after[0].Enabled {
		t.Fatalf("List() after disable = %+v, %v, want the server disabled", after, err)
	}

	if code := run([]string{"mcp", "enable", id}); code != 0 {
		t.Fatalf("run(mcp enable) = %d, want 0", code)
	}
	after, err = mcpservers.New(s).List()
	if err != nil || len(after) != 1 || !after[0].Enabled {
		t.Fatalf("List() after enable = %+v, %v, want the server enabled", after, err)
	}
}

// TestRunMcpAddDuplicateNamePrintsConflictError pins the conflict path: a
// second add with the same name exits nonzero and surfaces the daemon's
// store.ErrMcpServerNameConflict message (via mcp.create's passthrough),
// not a generic failure.
func TestRunMcpAddDuplicateNamePrintsConflictError(t *testing.T) {
	newTestMcpDaemon(t)

	if code := run([]string{"mcp", "add", "dupe", "stdio", "--command", "npx"}); code != 0 {
		t.Fatalf("run(mcp add first) = %d, want 0", code)
	}

	var code int
	stderr := captureStderr(t, func() {
		code = run([]string{"mcp", "add", "dupe", "stdio", "--command", "npx"})
	})
	if code == 0 {
		t.Fatalf("run(mcp add duplicate) = 0, want nonzero; stderr: %s", stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte("already exists")) {
		t.Fatalf("stderr = %q, want the daemon's name-conflict error", stderr)
	}
}
