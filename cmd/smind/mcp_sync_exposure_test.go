package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoMCPToolForPushOrUpdate pins ADR-0039 sections 3-6: Push and Update from
// base are human-only. No MCP tool is named for them, and no non-test source in
// this package calls task.push or task.updateFromBase.
func TestNoMCPToolForPushOrUpdate(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		low := strings.ToLower(name)
		if low == "task_push" || strings.Contains(low, "updatefrombase") || strings.Contains(low, "update_from_base") || strings.Contains(low, "git_push") || strings.Contains(low, "push_branch") {
			t.Fatalf("tools/list contains %q: Push and Update from base are human-only", name)
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
		for _, m := range []string{"task.push", "task.updateFromBase"} {
			if strings.Contains(string(b), m) {
				t.Errorf("%s mentions %s", f, m)
			}
		}
	}
}
