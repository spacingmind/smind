package gitsafe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory to the directory
// holding go.mod.
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
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// The markers a choke point must carry in its source: the import makes the
// hardening reachable, gitsafe.Args (or gitsafe.StrippedArgs, since the
// stripped-mode plan made that the daemon's default) builds the arguments,
// gitsafe.Env (or gitsafe.StrippedEnv) scrubs the environment.
const (
	gitsafeImport      = `"github.com/spacingmind/smind/internal/gitsafe"`
	gitsafeArgs        = `gitsafe.Args(`
	gitsafeStrippedArg = `gitsafe.StrippedArgs(`
	gitsafeEnv         = `gitsafe.Env(`
	gitsafeStrippedEnv = `gitsafe.StrippedEnv(`
)

// chokePoints are the files D2 of the plan lists as the shared choke
// points: every git command the daemon runs goes through one of them. The
// four marked env own their git processes' environment outright, so they
// must scrub it with gitsafe.Env too.
var chokePoints = []struct {
	path string
	env  bool
}{
	{"internal/workspace/git.go", true},
	{"internal/workspace/task_ref.go", false},
	{"internal/steward/git.go", true},
	{"internal/validation/digest.go", true},
	{"internal/gitstate/runner.go", true},
	{"internal/learning/probe/sandbox/tripwire.go", false},
	{"internal/learning/probe/arm/diff.go", false},
	{"internal/taskrunner/gitrules_detail.go", false},
	{"internal/wsapi/fleet_read.go", false},
	{"internal/wsapi/fleet_write.go", false},
	{"internal/wsapi/forge_auth.go", false},
	{"internal/wsapi/steward_collect.go", false},
	{"internal/wsapi/steward_collect_twoaxis.go", false},
}

// Test_audit_choke_points_use_gitsafe fails when one of those files stops
// building its git commands through gitsafe: a choke point edited back to a
// bare exec.Command("git", ...) reopens the doors Args and Env close, and
// the failure names the file so the regression is one glance away.
func Test_audit_choke_points_use_gitsafe(t *testing.T) {
	root := repoRoot(t)
	for _, cp := range chokePoints {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cp.path)))
		if err != nil {
			t.Errorf("%s: could not read the choke-point file: %v", cp.path, err)
			continue
		}
		text := string(src)
		if !strings.Contains(text, gitsafeImport) {
			t.Errorf("%s: does not import gitsafe, so a D2 choke point builds git commands without it", cp.path)
		}
		if !strings.Contains(text, gitsafeArgs) && !strings.Contains(text, gitsafeStrippedArg) {
			t.Errorf("%s: never calls gitsafe.Args or gitsafe.StrippedArgs, so a git command built here runs without the shared hardening", cp.path)
		}
		if cp.env && !strings.Contains(text, gitsafeEnv) && !strings.Contains(text, gitsafeStrippedEnv) {
			t.Errorf("%s: never calls gitsafe.Env or gitsafe.StrippedEnv, so the daemon's own GIT_EXTERNAL_DIFF reaches git", cp.path)
		}
	}
}

// gitSpawn matches a direct spawn of the git binary — exec.Command("git",
// ...) or exec.CommandContext(ctx, "git", ...) with no other string literal
// in between — so a command name held in a variable is not a hit.
var gitSpawn = regexp.MustCompile(`exec\.Command(Context)?\([^"\n]*"git"`)

// auditSkipDirs are never walked: test fixtures and build output hold no
// daemon code, and internal/gitsafe is this audit plus the driver-test
// helper, which is allowed to spawn git directly.
var auditSkipDirs = map[string]bool{
	"testdata":     true,
	"node_modules": true,
	"dist":         true,
}

// Test_audit_every_git_call_site_is_in_the_plan fails when a non-test file
// under internal/ or cmd/ spawns git directly and the plan's audit table
// does not name it: every call site carries a verdict there (safe, fixed,
// out of scope), so an unnamed direct spawn is a hole in the audit. All
// misses are reported at once so the failure is actionable.
func Test_audit_every_git_call_site_is_in_the_plan(t *testing.T) {
	root := repoRoot(t)
	plan, err := os.ReadFile(filepath.Join(root, "docs", "plans", "active", "p0-git-diff-hardening.md"))
	if err != nil {
		plan, err = os.ReadFile(filepath.Join(root, "docs", "plans", "completed", "p0-git-diff-hardening.md"))
	}
	if err != nil {
		t.Fatalf("could not read the plan's audit table: %v", err)
	}
	planText := string(plan)

	var missing []string
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
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
				return readErr
			}
			if !gitSpawn.Match(src) {
				return nil
			}
			if !strings.Contains(planText, strings.TrimPrefix(rel, "internal/")) {
				missing = append(missing, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d git call site(s) the plan's audit table does not name. Every non-test file that spawns git directly needs a row with a verdict in docs/plans/active/p0-git-diff-hardening.md, or its call routed through a D2 choke point; do not silence this by widening the test:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}
