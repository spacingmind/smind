package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProviderSetDefaultModel_NotExposedOnMcp: provider.setDefaultModel is for
// the human UI; no MCP tool lists or forwards it.
func TestProviderSetDefaultModel_NotExposedOnMcp(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("empty tool catalog")
	}
	for _, tool := range res.Tools {
		text := strings.ToLower(tool.Name + " " + tool.Description)
		if strings.Contains(text, "default_model") || strings.Contains(text, "setdefaultmodel") || strings.Contains(text, "default model") {
			t.Errorf("MCP tool %q mentions the default model setter", tool.Name)
		}
	}
	files, _ := filepath.Glob("mcp*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "provider.setDefaultModel") {
			t.Errorf("%s references provider.setDefaultModel", name)
		}
	}
}
