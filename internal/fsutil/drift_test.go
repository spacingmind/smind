package fsutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bareSymlinkAllowed lists the only production files that may name
// ModeSymlink directly: the helper itself, and archive extraction, whose
// modes come from tar/zip entry headers rather than from the filesystem.
var bareSymlinkAllowed = map[string]bool{
	"internal/fsutil/link.go":     true,
	"internal/plugins/extract.go": true,
}

// repoRoot walks up from the test's directory to the directory with go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// TestNoBareModeSymlinkInProduction fails when non-test code tests
// os.ModeSymlink / fs.ModeSymlink directly: on Windows that lets a junction
// through (see IsLink). Use fsutil.IsLink instead.
func TestNoBareModeSymlinkInProduction(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "refs", "vendor", "target", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || bareSymlinkAllowed[rel] {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ModeSymlink" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "os" || pkg.Name == "fs") {
				offenders = append(offenders, fset.Position(sel.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offenders {
		t.Errorf("%s: bare ModeSymlink check lets a Windows junction through; use fsutil.IsLink", o)
	}
}
