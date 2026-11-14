package crash

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	usedCallbacks := map[string]bool{}
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
					if call, ok := n.(*ast.CallExpr); ok && isDeferredGoCall(call) {
						key := dir + "/" + name + ":" + fn
						if recordsAndRepanics(call) {
							return true
						}
						if _, ok := callbackAllowList[key]; ok {
							usedCallbacks[key] = true
							return true
						}
						t.Errorf("%s: time.AfterFunc / errgroup .Go callback neither starts with `defer crash.RecordAndRepanic(...)` nor is on callbackAllowList (key %q)",
							fset.Position(n.Pos()), key)
						return true
					}
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
	for key, reason := range callbackAllowList {
		if !usedCallbacks[key] {
			t.Errorf("stale callback allow-list entry: %s: %s", key, reason)
		}
	}
}

// callbackAllowList holds the callbacks that the runtime runs on its own
// goroutine (time.AfterFunc, errgroup Go) and that stay bare: a panic in
// them is not recorded and, as always, ends the process. Keyed like
// goAllowList. Callbacks that change state start with
// `defer crash.RecordAndRepanic(...)` instead and need no entry.
var callbackAllowList = map[string]string{
	"internal/quota/passive.go:NewPassive":      "a clock default that passes the caller's function through; the callers' functions are not user code",
	"internal/gitstate/service.go:New":          "a clock default that passes the caller's function through",
	"internal/server/proxy.go:newUpstreamWatch": "cancels a context; no user code",
	"cmd/smind/run_viewer.go:permissionRequest": "releases a viewer slot; a CLI path, not the daemon",
}

// isDeferredGoCall reports whether call hands a function to the runtime to
// run on its own goroutine: time.AfterFunc(d, f) or <x>.Go(f) other than the
// crash package's own helpers.
func isDeferredGoCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, _ := sel.X.(*ast.Ident)
	switch sel.Sel.Name {
	case "AfterFunc":
		return x != nil && x.Name == "time"
	case "Go":
		return x == nil || x.Name != "crash"
	}
	return false
}

// recordsAndRepanics reports whether the function literal argument of call
// starts with `defer crash.RecordAndRepanic(...)`.
func recordsAndRepanics(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		fl, ok := a.(*ast.FuncLit)
		if !ok || len(fl.Body.List) == 0 {
			continue
		}
		d, ok := fl.Body.List[0].(*ast.DeferStmt)
		if !ok {
			continue
		}
		if sel, ok := d.Call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "RecordAndRepanic" {
			return true
		}
	}
	return false
}

// recoverSites lists the pre-existing recover() sites that must also record
// (AC4): file -> the site labels its recover()s hand to the recorder.
var recoverSites = map[string][]string{
	"internal/runs/observer.go":       {"runs.observer"},
	"internal/runs/delivery.go":       {"runs.deliveryPrepare", "runs.pluginSkillDelivery"},
	"internal/server/proxy.go":        {"server.quotaObserve"},
	"internal/push/notifier.go":       {"push.notify"},
	"internal/quota/poller.go":        {"quota.pollerFetch"},
	"internal/quota/providerusage.go": {"quota.providerUsageFetch"},
}

// TestCrashRecoverSitesRecord: the existing recover() sites keep their own
// handling but must hand the recovered VALUE to the recorder with the stack:
// every `if v := recover(); v != nil { ... }` in these files contains
// `<x>.Record("<site>", v, debug.Stack())` with the same v, and the set of
// site labels is exactly the expected one (so a new recover() or a renamed
// label is a deliberate edit here).
func TestCrashRecoverSitesRecord(t *testing.T) {
	root := filepath.Join("..", "..")
	for file, want := range recoverSites {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, file), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		recovers := 0
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "recover" {
					recovers++
				}
			}
			return true
		})
		var got []string
		ast.Inspect(f, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			as, ok := ifs.Init.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			rc, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := rc.Fun.(*ast.Ident); !ok || id.Name != "recover" {
				return true
			}
			v, _ := as.Lhs[0].(*ast.Ident)
			if v == nil {
				t.Errorf("%s: recover() result not bound to a name", fset.Position(ifs.Pos()))
				return true
			}
			site := recordedSite(ifs.Body, v.Name)
			if site == "" {
				t.Errorf("%s: recover() site does not call Record(\"<site>\", %s, debug.Stack())", file, v.Name)
				return true
			}
			got = append(got, site)
			return true
		})
		sort.Strings(got)
		if recovers != len(want) || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: recover() sites %d, recorded labels %v; want %v", file, recovers, got, want)
		}
	}
}

// recordedSite returns the site label of a `<x>.Record("<site>", <v>, debug.Stack())`
// call inside body, or "".
func recordedSite(body ast.Node, v string) string {
	site := ""
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 3 {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Record" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		arg, ok2 := call.Args[1].(*ast.Ident)
		st, ok3 := call.Args[2].(*ast.CallExpr)
		if !ok || !ok2 || !ok3 || arg.Name != v {
			return true
		}
		if sel, ok := st.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Stack" {
			return true
		}
		site, _ = strconv.Unquote(lit.Value)
		return true
	})
	return site
}
