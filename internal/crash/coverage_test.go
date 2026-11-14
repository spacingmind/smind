package crash

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scannedDaemonPackages are the packages whose every goroutine must run
// under crash.Go (or be on the allow-list below with a written reason).
var scannedDaemonPackages = []string{
	"internal/runs",
	"internal/wsapi",
	"internal/taskrunner",
	"internal/quota",
	"internal/push",
	"internal/gitstate",
	"internal/terminal",
	"internal/workspace",
	"internal/server",
	"internal/accounts",
	"cmd/smind",
}

// goAllowList holds the go statements that deliberately stay bare, keyed
// <dir>/<file>:<enclosing func name>. Every entry needs a matching go
// statement in the tree; a stale entry fails the test too.
var goAllowList = map[string]string{
	"internal/taskrunner/runner.go:runClaudeNative": "a Close that cannot reach user code",
	"internal/workspace/setup_unix.go:runScript":    "only forwards Wait's result to a channel",
	"internal/workspace/setup_windows.go:runScript": "only forwards Wait's result to a channel",
	"cmd/smind/mcp.go:cmdMcpServe":                  "only forwards srv.Run's result to a channel",
	"cmd/smind/task_wait.go:runTaskWait":            "a CLI command, not the daemon",
}

// TestCrashGoStatementsCovered: crash-go-statements-covered — after the
// conversion a bare go statement in a daemon package is, by definition,
// not under crash.Go, so it fails unless it is on the allow-list with a
// reason (the drift check that holds for new code).
func TestCrashGoStatementsCovered(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	for _, dir := range scannedDaemonPackages {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(root, dir, name)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range f.Decls {
				// A go statement is attributed to the top-level function it
				// sits in ("" outside any function), so nested literals share
				// one key with their enclosing named function.
				fn := ""
				var scope ast.Node = decl
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
					fn = fd.Name.Name
					scope = fd.Body
				}
				ast.Inspect(scope, func(n ast.Node) bool {
					if _, ok := n.(*ast.GoStmt); !ok {
						return true
					}
					key := dir + "/" + name + ":" + fn
					if _, ok := goAllowList[key]; ok && !used[key] {
						used[key] = true
						return true
					}
					t.Errorf("%s: bare go statement not under crash.Go (allow-list key %q)",
						fset.Position(n.Pos()), key)
					return true
				})
			}
		}
	}
	for key, reason := range goAllowList {
		if !used[key] {
			t.Errorf("stale allow-list entry (no matching go statement): %s: %s", key, reason)
		}
	}
}

// recoverSites lists the pre-existing recover() sites that must also record
// (AC4): file -> expected number of recover() calls.
var recoverSites = map[string]int{
	"internal/runs/observer.go":       1,
	"internal/runs/delivery.go":       2,
	"internal/server/proxy.go":        1,
	"internal/push/notifier.go":       1,
	"internal/quota/poller.go":        1,
	"internal/quota/providerusage.go": 1,
}

// TestCrashRecoverSitesRecord: the existing recover() sites keep their own
// handling but must also hand the panic to the recorder: the innermost
// function (literal) enclosing each recover() contains a call to .Record(
// (a syntactic check).
func TestCrashRecoverSitesRecord(t *testing.T) {
	root := filepath.Join("..", "..")
	for file, want := range recoverSites {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, file), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var funcs []ast.Node // every FuncDecl/FuncLit with a body
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					funcs = append(funcs, fn)
				}
			case *ast.FuncLit:
				funcs = append(funcs, fn)
			}
			return true
		})
		got := 0
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "recover" {
				return true
			}
			got++
			enc := innermostEnclosing(funcs, call.Pos())
			body := ast.Node(enc)
			if fd, ok := enc.(*ast.FuncDecl); ok {
				body = fd.Body
			} else if fl, ok := enc.(*ast.FuncLit); ok {
				body = fl.Body
			}
			if body == nil || !callsMethod(body, "Record") {
				t.Errorf("%s: recover() site does not record: %s", file, fset.Position(call.Pos()))
			}
			return true
		})
		if got != want {
			t.Errorf("%s: found %d recover() site(s), want %d", file, got, want)
		}
	}
}

// innermostEnclosing returns the node in funcs whose span contains pos and
// is the smallest, or nil when none does.
func innermostEnclosing(funcs []ast.Node, pos token.Pos) ast.Node {
	var best ast.Node
	for _, fn := range funcs {
		if fn.Pos() <= pos && pos <= fn.End() && (best == nil || fn.End()-fn.Pos() < best.End()-best.Pos()) {
			best = fn
		}
	}
	return best
}

// callsMethod reports whether body contains a call to a .<name>( method.
func callsMethod(body ast.Node, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			found = true
		}
		return true
	})
	return found
}
