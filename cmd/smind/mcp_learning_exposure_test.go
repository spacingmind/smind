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

// TestLearning_NotExposedOnMcp pins ADR-0026 §3/§8: no MCP tool manages the
// learning loop (recursion guard, "humans only"). Two layers: the live tool
// catalog names/descriptions, and the source of every MCP tool file, since
// all tools reach the daemon through callWS with a literal method name.
func TestLearning_NotExposedOnMcp(t *testing.T) {
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
		for _, bad := range []string{"learn", "skill_", "reflect", "curate", "learning."} {
			if strings.Contains(text, bad) {
				t.Errorf("MCP tool %q mentions %q; learning must not be exposed to agents", tool.Name, bad)
			}
		}
	}

	files, err := filepath.Glob("mcp*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob mcp*.go: %v (%d files)", err, len(files))
	}
	fset := token.NewFileSet()
	calls := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, _ := strconv.Unquote(x.Value); strings.HasPrefix(s, "learning.") {
						t.Errorf("%s: MCP code references daemon method %q", fset.Position(x.Pos()), s)
					}
				}
			case *ast.CallExpr:
				// callWS(ctx, client, tool, method, ...): the method must be a
				// literal, so no tool can forward a caller-chosen method name.
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "callWS" && len(x.Args) >= 4 {
					calls++
					if lit, ok := x.Args[3].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
						t.Errorf("%s: callWS method is not a string literal", fset.Position(x.Pos()))
					}
				}
			}
			return true
		})
	}
	if calls == 0 {
		t.Fatal("found no callWS calls; the scan is vacuous")
	}
}
