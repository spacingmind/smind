package gitsafe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// auditStripped is the stripped-mode audit (the plan's D5) over a
// repository root (a directory holding internal/ and cmd/) and the text of
// the stripped-mode plan. It returns three lists, all empty when the audit
// is clean:
//
//   - unclassified: every non-test file under internal/ or cmd/ that spawns
//     git directly but has no row in the plan's classification table, plus
//     every table row whose class cell does not read as one of daemon,
//     human, mixed, out ("bad class <row>");
//   - notStripped: every file a daemon or mixed row names whose source
//     never calls gitsafe.StrippedArgs(, and every file only human rows
//     name whose source never calls gitsafe.Args(;
//   - humanUnnamed: every call of WithHumanInitiated or humanContext that
//     sits in a function the plan text does not name in backticks, as
//     "rel:qualifiedName" (the receiver type and the method for a method,
//     as in "prHost.push", else the plain function name).
//
// The classification table is the pipe lines between the plan's
// "## Classification of every git call site" heading and the next "## "
// heading, minus its header and separator rows; the first word of a row's
// class cell (emphasis marks and spaces stripped) is the class.
func auditStripped(root, plan string) (unclassified, notStripped, humanUnnamed []string) {
	// classWord is the class of a class cell: its text with the emphasis
	// marks and spaces removed, then its first word, lowercased, so
	// "**mixed** (human: the Stage button)" reads as "mixed" and the
	// header's own "Class" cell reads as "class".
	classWord := func(cell string) string {
		var flat strings.Builder
		for _, r := range cell {
			if r == '*' || unicode.IsSpace(r) {
				continue
			}
			flat.WriteRune(r)
		}
		var word []rune
		for _, r := range flat.String() {
			if !unicode.IsLetter(r) {
				break
			}
			word = append(word, r)
		}
		return strings.ToLower(string(word))
	}

	// (1) the classification table.
	type row struct{ site, class string }
	var rows []row
	inTable := false
	for _, line := range strings.Split(plan, "\n") {
		if inTable && strings.HasPrefix(line, "## ") {
			break
		}
		if !inTable {
			if strings.HasPrefix(line, "## Classification of every git call site") {
				inTable = true
			}
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			unclassified = append(unclassified, "bad class "+strings.TrimSpace(line))
			continue
		}
		switch class := classWord(cells[3]); class {
		case "", "class": // the separator and header rows are not data
		case "daemon", "human", "mixed", "out":
			rows = append(rows, row{site: cells[1], class: class})
		default:
			unclassified = append(unclassified, "bad class "+strings.TrimSpace(line))
		}
	}

	// Every non-test .go file under root/internal and root/cmd (test
	// fixtures, build output and this package skipped), read once for both
	// halves of the audit.
	type srcFile struct{ rel, path, src string }
	var files []srcFile
	for _, top := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if err != nil {
				if d == nil {
					return nil // the top directory itself is absent
				}
				unclassified = append(unclassified, rel)
				return nil
			}
			if d.IsDir() {
				if rel == "internal/gitsafe" || auditSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				unclassified = append(unclassified, rel)
				return nil
			}
			files = append(files, srcFile{rel: rel, path: path, src: string(src)})
			return nil
		})
	}

	// (2) every direct git spawn needs a row, and the row's class decides
	// the hardening the file must carry: daemon or mixed means stripped
	// mode, human-only means the baseline Args (the repository's own hooks
	// and drivers keep working), out means nothing.
	for _, f := range files {
		if !gitSpawn.MatchString(f.src) {
			continue
		}
		key := strings.TrimPrefix(f.rel, "internal/")
		var naming []row
		for _, r := range rows {
			if strings.Contains(r.site, key) {
				naming = append(naming, r)
			}
		}
		if len(naming) == 0 {
			unclassified = append(unclassified, f.rel)
			continue
		}
		daemonOrMixed, allHuman := false, true
		for _, r := range naming {
			if r.class == "daemon" || r.class == "mixed" {
				daemonOrMixed = true
			}
			if r.class != "human" {
				allHuman = false
			}
		}
		switch {
		case daemonOrMixed && !strings.Contains(f.src, "gitsafe.StrippedArgs("):
			notStripped = append(notStripped, f.rel)
		case allHuman && !strings.Contains(f.src, "gitsafe.Args("):
			notStripped = append(notStripped, f.rel)
		}
	}

	// (3) a human-mode variant may only sit in a function the plan names.
	funcName := func(fd *ast.FuncDecl) string {
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			t := fd.Recv.List[0].Type
			if star, ok := t.(*ast.StarExpr); ok {
				t = star.X
			}
			if id, ok := t.(*ast.Ident); ok {
				return id.Name + "." + fd.Name.Name
			}
		}
		return fd.Name.Name
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, f := range files {
		if f.rel == "internal/workspace/initiator.go" || f.rel == "internal/wsapi/initiator.go" {
			continue // the helpers' own definitions, not call sites
		}
		file, err := parser.ParseFile(fset, f.path, f.src, parser.SkipObjectResolution)
		if err != nil {
			continue // an unparsable file fails the build long before this audit
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := funcName(fd)
			ast.Inspect(fd, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var called string
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					called = fun.Name
				case *ast.SelectorExpr:
					called = fun.Sel.Name
				}
				if called != "WithHumanInitiated" && called != "humanContext" {
					return true
				}
				if !strings.Contains(plan, "`"+name+"`") && !seen[f.rel+":"+name] {
					seen[f.rel+":"+name] = true
					humanUnnamed = append(humanUnnamed, f.rel+":"+name)
				}
				return true
			})
		}
	}
	return unclassified, notStripped, humanUnnamed
}

// readStrippedPlan reads the stripped-mode plan, from the active plans tree
// with the completed one as the fallback, the way the A2 audit reads its
// own plan.
func readStrippedPlan(t *testing.T, root string) string {
	t.Helper()
	plan, err := os.ReadFile(filepath.Join(root, "docs", "plans", "active", "p0-git-stripped-mode.md"))
	if err != nil {
		plan, err = os.ReadFile(filepath.Join(root, "docs", "plans", "completed", "p0-git-stripped-mode.md"))
	}
	if err != nil {
		t.Fatalf("could not read the stripped-mode plan: %v", err)
	}
	return string(plan)
}

// Test_audit_every_git_spawn_is_classified fails when a non-test file under
// internal/ or cmd/ spawns git directly and the plan's classification table
// has no row naming it (or a row's class is not one of the four): every
// spawn carries a verdict there, so an unnamed or ill-classed row is a hole
// in the audit. All misses are reported at once so the failure is
// actionable.
func Test_audit_every_git_spawn_is_classified(t *testing.T) {
	root := repoRoot(t)
	unclassified, _, _ := auditStripped(root, readStrippedPlan(t, root))
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Errorf("%d git call site(s) the stripped-mode plan's classification table does not name (or whose class does not read as daemon, human, mixed or out). Every non-test file that spawns git directly needs a row with a class; do not silence this by widening the audit:\n  %s", len(unclassified), strings.Join(unclassified, "\n  "))
	}
}

// Test_audit_daemon_files_use_stripped_args fails when a file a daemon or
// mixed row names never calls gitsafe.StrippedArgs (or a human-only file
// never calls gitsafe.Args): the class recorded in the plan is then not the
// mode the code runs, and the failure names the file so the regression is
// one glance away.
func Test_audit_daemon_files_use_stripped_args(t *testing.T) {
	root := repoRoot(t)
	_, notStripped, _ := auditStripped(root, readStrippedPlan(t, root))
	if len(notStripped) > 0 {
		sort.Strings(notStripped)
		t.Errorf("%d file(s) run git without the mode their class requires (daemon or mixed without gitsafe.StrippedArgs, human without gitsafe.Args):\n  %s", len(notStripped), strings.Join(notStripped, "\n  "))
	}
}

// Test_audit_human_variants_only_in_named_functions fails when a call of
// WithHumanInitiated or humanContext sits in a function the plan does not
// name in backticks: the human mode is a narrow exception the plan
// enumerates, so an unnamed function reaching it is an unreviewed escape
// from stripped mode.
func Test_audit_human_variants_only_in_named_functions(t *testing.T) {
	root := repoRoot(t)
	_, _, humanUnnamed := auditStripped(root, readStrippedPlan(t, root))
	if len(humanUnnamed) > 0 {
		sort.Strings(humanUnnamed)
		t.Errorf("%d human-mode call(s) outside a function the plan names in backticks; name the function in its row before using a human variant:\n  %s", len(humanUnnamed), strings.Join(humanUnnamed, "\n  "))
	}
}

// Test_audit_flags_a_new_unclassified_spawn feeds the checker a synthetic
// repository and plan, one miss at a time: a spawn whose row is missing, a
// daemon row whose file never calls gitsafe.StrippedArgs, a human variant
// in a function the plan does not name, and the same call once the plan
// names the function.
func Test_audit_flags_a_new_unclassified_spawn(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/demo/spawn.go", "package demo\n\nimport \"os/exec\"\n\nfunc status() bool {\n\treturn exec.Command(\"git\", \"status\").Run() == nil\n}\n")
	write("internal/demo/human.go", "package demo\n\nimport (\n\t\"context\"\n\n\t\"github.com/spacingmind/smind/internal/workspace\"\n)\n\nfunc rogue(ctx context.Context) {\n\t_ = workspace.WithHumanInitiated(ctx)\n}\n")
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A valid one-row table that names some OTHER file.
	planOther := "## Classification of every git call site\n\n" +
		"| Call site (file: functions) | Commands | Class | Why |\n" +
		"|---|---|---|---|\n" +
		"| `other/file.go`: `f` | `status` | **daemon** | why |\n"
	// The same table, naming the spawn with class daemon.
	planDaemon := "## Classification of every git call site\n\n" +
		"| Call site (file: functions) | Commands | Class | Why |\n" +
		"|---|---|---|---|\n" +
		"| `demo/spawn.go`: `status` | `status` | **daemon** | why |\n"
	// A plan that names the human-variant function in backticks.
	planRogue := planOther + "\nThe named human function is `rogue`.\n"

	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}

	unclassified, notStripped, humanUnnamed := auditStripped(root, planOther)
	if !has(unclassified, "internal/demo/spawn.go") {
		t.Errorf("unclassified = %q, want it to flag internal/demo/spawn.go (no row names it)", unclassified)
	}
	if has(notStripped, "internal/demo/spawn.go") {
		t.Errorf("notStripped = %q, want no daemon claim for a file no row names", notStripped)
	}

	_, notStripped, _ = auditStripped(root, planDaemon)
	if !has(notStripped, "internal/demo/spawn.go") {
		t.Errorf("notStripped = %q, want it to flag internal/demo/spawn.go (a daemon row, no gitsafe.StrippedArgs in the source)", notStripped)
	}

	_, _, humanUnnamed = auditStripped(root, planOther)
	if !has(humanUnnamed, "internal/demo/human.go:rogue") {
		t.Errorf("humanUnnamed = %q, want it to flag internal/demo/human.go:rogue (the plan does not name rogue)", humanUnnamed)
	}

	_, _, humanUnnamed = auditStripped(root, planRogue)
	if len(humanUnnamed) != 0 {
		t.Errorf("humanUnnamed = %q, want empty once the plan names `rogue` in backticks", humanUnnamed)
	}
}
