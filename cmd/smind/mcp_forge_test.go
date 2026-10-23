package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestForgeNoMcpTool pins ADR-0039 section 8: nothing in the MCP catalog can
// set, change or remove the daemon host's forge credential -- agents never set
// credentials. No tool is named for it, and no non-test source in this package
// calls forge.setToken or forge.logout.
func TestForgeNoMcpTool(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		low := strings.ToLower(name)
		for _, bad := range []string{"forge", "token", "login", "logout", "credential", "gh_auth"} {
			if strings.Contains(low, bad) {
				t.Fatalf("tools/list contains %q (matches %q): agents must not touch credentials", name, bad)
			}
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{"forge.setToken", "forge.logout"} {
			if strings.Contains(string(b), m) {
				t.Errorf("%s calls %s", f, m)
			}
		}
	}
}
