package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPServe_FailsFastWhenDaemonUnreachable pins AC2 (ADR-0017 resolved
// decision 5): with no daemon listening on the configured port, `smind mcp
// serve` exits non-zero with a stderr message naming the cause -- before
// speaking any MCP -- instead of starting a stdio server whose every tool
// call would fail, and instead of hanging.
func TestMCPServe_FailsFastWhenDaemonUnreachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	// A config pointing at a port nothing listens on (60000 is in the
	// ephemeral range but very unlikely to be bound in the sandbox; a
	// failed dial is what's being asserted either way -- a successful one
	// would only be possible if a real daemon was already running there).
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: 60000\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var code int
	stderr := captureStderr(t, func() { code = run([]string{"mcp", "serve"}) })

	if code == 0 {
		t.Fatalf("run(mcp serve) = 0 with no daemon, want non-zero (stderr = %q)", stderr)
	}
	if want := "is `smind serve` running?"; !contains(stderr, want) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
	}
	if want := "mcp serve:"; !contains(stderr, want) {
		t.Fatalf("stderr = %q, want it to carry the %q prefix", stderr, want)
	}
}

// TestMcp_Usage pins the `mcp` group's dispatch contract: unknown or
// missing subcommands print usage and exit 2. add/ls/rm/enable/disable
// (ADR-0018) now share the group with serve (ADR-0017), hence the usage
// line naming every subcommand.
func TestMcp_Usage(t *testing.T) {
	for _, args := range [][]string{{"mcp"}, {"mcp", "bogus"}, {"mcp", "serve", "extra"}} {
		code := -1
		stderr := captureStderr(t, func() { code = run(args) })
		if code != 2 {
			t.Errorf("run(%v) = %d, want 2 (stderr = %q)", args, code, stderr)
		}
		if !contains(stderr, "usage: smind mcp <serve|add|ls|rm|enable|disable>") {
			t.Errorf("run(%v) stderr = %q, want the usage line", args, stderr)
		}
	}
}

func contains(s, substr string) bool { return strings.Contains(s, substr) }
