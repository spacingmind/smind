package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestMCPCatalog_HasNoPolicyTool pins ADR-0047 §2: no MCP tool names the
// permission policy or the steward, so an agent can neither read the rules
// that judge it nor touch the audit. registerMCPTools has no scope branch:
// a run-scoped `mcp serve` (SMIND_RUN_ID) registers the same catalogue and
// only identifies itself over session.identify, so the live catalogue plus a
// scan of every MCP source file for policy.* / steward.* daemon methods
// covers both scopes.
func TestMCPCatalog_HasNoPolicyTool(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("empty tool catalog")
	}
	for _, tool := range res.Tools {
		name := strings.ToLower(tool.Name)
		for _, bad := range []string{"policy", "steward"} {
			if strings.Contains(name, bad) {
				t.Errorf("MCP tool %q contains %q; the policy engine must not be exposed to agents", tool.Name, bad)
			}
		}
	}

	files, err := filepath.Glob("mcp*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob mcp*.go: %v (%d files)", err, len(files))
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, _ := strconv.Unquote(lit.Value); strings.HasPrefix(s, "policy.") || strings.HasPrefix(s, "steward.") {
				t.Errorf("%s: MCP code references daemon method %q", fset.Position(lit.Pos()), s)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no MCP source files; the scan is vacuous")
	}
}
