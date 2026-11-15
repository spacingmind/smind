package hygiene

// A fixed time.Sleep in a test is a guess about how long something takes: too short and the test fails on a busy CI machine,
// too long and the suite crawls. The timing-heavy packages (wsapi, server) are where CI has failed a different test on
// unrelated pull requests, so fixed sleeps there are counted against a baseline that can only shrink.
//
// What counts: a time.Sleep call that is NOT inside a for or range loop of its function. A sleep inside a loop is the
// step of a poll-with-deadline (a condition wait) and is fine. What is exempt: a sleep that is the point of the test
// (asserting that nothing happens within a window, or holding a fake open for a window) says so with `// sleep-ok: <reason>`
// on its line; an empty reason fails.
//
// The baseline (testdata/sleep-baseline.txt, one "path count" line per file) is a ratchet in both directions: more sleeps
// than the baseline fails, and fewer also fails until the baseline is lowered, so a fixed test cannot silently regrow.

import (
	"fmt"
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

// sleepGuardDirs are the packages whose tests are guarded (repo-relative, slash form).
var sleepGuardDirs = []string{"internal/wsapi", "internal/server"}

const sleepBaselinePath = "internal/hygiene/testdata/sleep-baseline.txt"

// scanFixedSleeps returns the number of counted fixed sleeps per _test.go file under dirs (keys are repo-relative with
// forward slashes, files with none are absent) and the problems found (a `sleep-ok` with no reason).
func scanFixedSleeps(root string, dirs []string) (map[string]int, []string) {
	counts := map[string]int{}
	var problems []string
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*_test.go"))
		for _, f := range files {
			rel := filepath.ToSlash(strings.TrimPrefix(f, root+string(filepath.Separator)))
			src, err := os.ReadFile(f)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			lines := strings.Split(string(src), "\n")
			fset := token.NewFileSet()
			af, err := parser.ParseFile(fset, f, src, 0)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			var walk func(n ast.Node, inLoop bool)
			walk = func(n ast.Node, inLoop bool) {
				ast.Inspect(n, func(c ast.Node) bool {
					switch v := c.(type) {
					case *ast.ForStmt:
						if c != n {
							walk(v.Body, true)
							return false
						}
					case *ast.RangeStmt:
						if c != n {
							walk(v.Body, true)
							return false
						}
					case *ast.FuncLit:
						if c != n {
							walk(v.Body, false) // a closure starts a new function: its own loop state
							return false
						}
					case *ast.CallExpr:
						sel, ok := v.Fun.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "Sleep" {
							return true
						}
						if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "time" {
							return true
						}
						line := fset.Position(v.Pos()).Line
						text := lines[line-1]
						if i := strings.Index(text, "sleep-ok:"); i >= 0 {
							if strings.TrimSpace(text[i+len("sleep-ok:"):]) == "" {
								problems = append(problems, fmt.Sprintf("%s:%d: `sleep-ok:` needs a reason", rel, line))
							}
							return true
						}
						if !inLoop {
							counts[rel]++
						}
					}
					return true
				})
			}
			walk(af, false)
		}
	}
	return counts, problems
}

// parseSleepBaseline reads "path count" lines (blank lines and # comments skipped).
func parseSleepBaseline(text string) (map[string]int, error) {
	out := map[string]int{}
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		if len(f) != 2 {
			return nil, fmt.Errorf("baseline line %d: want `path count`, got %q", i+1, l)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n < 1 {
			return nil, fmt.Errorf("baseline line %d: count %q is not a positive number", i+1, f[1])
		}
		out[f[0]] = n
	}
	return out, nil
}

// compareSleepBaseline reports every file whose count differs from its baseline, in both directions.
func compareSleepBaseline(counts, baseline map[string]int) []string {
	seen := map[string]bool{}
	var keys []string
	for k := range counts {
		seen[k] = true
		keys = append(keys, k)
	}
	for k := range baseline {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var bad []string
	for _, k := range keys {
		got, want := counts[k], baseline[k]
		switch {
		case got > want:
			bad = append(bad, fmt.Sprintf("%s: %d fixed time.Sleep (baseline %d). Wait on a condition instead (poll with a deadline, a channel, or a started/released gate); a sleep that is the point of the test takes `// sleep-ok: <reason>` on its line", k, got, want))
		case got < want:
			bad = append(bad, fmt.Sprintf("%s: %d fixed time.Sleep, baseline says %d. The baseline only shrinks: lower it to %d (delete the line when it reaches 0) in %s", k, got, want, got, sleepBaselinePath))
		}
	}
	return bad
}

func TestNoNewFixedSleepsInTimingTests(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sleepBaselinePath)))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := parseSleepBaseline(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	counts, problems := scanFixedSleeps(root, sleepGuardDirs)
	for _, p := range problems {
		t.Error(p)
	}
	for _, p := range compareSleepBaseline(counts, baseline) {
		t.Error(p)
	}
}

// TestSleepBaselineCanOnlyShrink: a file below its baseline fails (so the number is lowered), above fails (new debt),
// equal and a file with no sleeps pass; a file that vanished from the tree but stays in the baseline fails too.
func TestSleepBaselineCanOnlyShrink(t *testing.T) {
	base := map[string]int{"a_test.go": 3, "gone_test.go": 1}
	if got := compareSleepBaseline(map[string]int{"a_test.go": 3}, map[string]int{"a_test.go": 3}); len(got) != 0 {
		t.Fatalf("equal counts must pass: %v", got)
	}
	if got := compareSleepBaseline(map[string]int{"a_test.go": 4}, map[string]int{"a_test.go": 3}); len(got) != 1 || !strings.Contains(got[0], "Wait on a condition") {
		t.Fatalf("above baseline must fail with the how-to: %v", got)
	}
	if got := compareSleepBaseline(map[string]int{"a_test.go": 2}, map[string]int{"a_test.go": 3}); len(got) != 1 || !strings.Contains(got[0], "lower it to 2") {
		t.Fatalf("below baseline must demand a lower baseline: %v", got)
	}
	if got := compareSleepBaseline(map[string]int{"new_test.go": 1}, map[string]int{}); len(got) != 1 {
		t.Fatalf("a new file with a sleep must fail: %v", got)
	}
	if got := compareSleepBaseline(map[string]int{"a_test.go": 3}, base); len(got) != 1 || !strings.Contains(got[0], "gone_test.go") {
		t.Fatalf("a baseline line for a file with no sleeps must fail: %v", got)
	}
}

func writeSleepTree(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestFixedSleepGuardFiresOnSeededTree: a bare sleep counts, a poll-loop sleep (also inside a closure within a loop's
// function) does not, a closure inside a loop starts fresh, and sleep-ok exempts.
func TestFixedSleepGuardFiresOnSeededTree(t *testing.T) {
	root := writeSleepTree(t, `package pkg

import (
	"testing"
	"time"
)

func TestA(t *testing.T) {
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 3; i++ {
		time.Sleep(time.Millisecond)
	}
	for range []int{1} {
		go func() { time.Sleep(time.Millisecond) }()
	}
	time.Sleep(time.Millisecond) // sleep-ok: nothing may arrive in this window
}
`)
	counts, problems := scanFixedSleeps(root, []string{"pkg"})
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if counts["pkg/x_test.go"] != 2 {
		t.Fatalf("counted %d fixed sleeps, want 2 (the bare one and the closure inside the loop); counts=%v", counts["pkg/x_test.go"], counts)
	}
}

func TestSleepOKNeedsAReason(t *testing.T) {
	root := writeSleepTree(t, `package pkg

import "time"

func f() {
	time.Sleep(time.Second) // sleep-ok:
}
`)
	_, problems := scanFixedSleeps(root, []string{"pkg"})
	if len(problems) != 1 || !strings.Contains(problems[0], "needs a reason") {
		t.Fatalf("an empty sleep-ok reason must be a problem: %v", problems)
	}
}
