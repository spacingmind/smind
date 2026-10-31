// Package readiness is a static, offline "would an agent be productive in this
// repo?" check. It reads a handful of well-known text files under one
// directory and reports findings with a generic fix hint.
//
// The repo is untrusted input (an agent may have edited it): nothing here
// executes repo code, opens the network, writes, or calls a model. Reads are
// size-capped, directory listings are capped, and a symlink is never followed
// (on any path component), so a link cannot lead the check out of the repo.
package readiness

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/fsutil"
)

// Status of one finding. Info never counts as a problem.
const (
	StatusPass = "pass"
	StatusWarn = "warn"
	StatusInfo = "info"
)

const (
	maxFileBytes   = 64 << 10
	maxDirEntries  = 500
	maxFieldBytes  = 300
	minAgentDocLen = 200
)

// Finding is one check's outcome. Evidence names the path found or the thing
// missing; Fix is one line of generic advice and never a command.
type Finding struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Evidence string `json:"evidence"`
	Fix      string `json:"fix"`
}

// Options carries what the repo's files cannot tell.
type Options struct {
	// HasSetupScript is the workspace record's setup-script flag.
	HasSetupScript bool
	// TrackedEnv returns the git-tracked paths that look like .env files. Nil
	// (or an error) skips the tracked-env check.
	TrackedEnv func(ctx context.Context) ([]string, error)
}

// Check reads repoDir and returns the findings in a fixed order.
func Check(ctx context.Context, repoDir string, opts Options) []Finding {
	r := &repo{root: repoDir}
	steps := []func(context.Context, *repo, Options) Finding{
		checkAgentDoc, checkTest, checkLint, checkSetup, checkGitignore,
		checkTrackedEnv, checkCI, checkDocs, checkLockfiles, checkConventions,
	}
	out := make([]Finding, 0, len(steps))
	for _, s := range steps {
		if ctx.Err() != nil {
			break
		}
		f := s(ctx, r, opts)
		f.Title, f.Evidence, f.Fix = clean(f.Title), clean(f.Evidence), clean(f.Fix)
		out = append(out, f)
	}
	return out
}

// repo is the read-only view of the directory under check.
type repo struct{ root string }

// stat Lstat's every component of rel, so a symlink anywhere on the path (not
// just at the end) makes the entry count as absent.
func (r *repo) stat(rel string) (os.FileInfo, bool) {
	cur := r.root
	var fi os.FileInfo
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		var err error
		fi, err = os.Lstat(cur)
		if err != nil || fsutil.IsLink(fi.Mode()) {
			return nil, false
		}
	}
	return fi, true
}

func (r *repo) isFile(rel string) bool {
	fi, ok := r.stat(rel)
	return ok && fi.Mode().IsRegular()
}

func (r *repo) isDir(rel string) bool {
	fi, ok := r.stat(rel)
	return ok && fi.IsDir()
}

// read returns at most maxFileBytes of a regular, non-symlink file.
func (r *repo) read(rel string) (string, bool) {
	before, ok := r.stat(rel)
	if !ok || !before.Mode().IsRegular() {
		return "", false
	}
	f, err := os.Open(filepath.Join(r.root, rel))
	if err != nil {
		return "", false
	}
	defer f.Close()
	if after, err := f.Stat(); err != nil || !os.SameFile(before, after) {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// list returns up to maxDirEntries names of a real directory.
func (r *repo) list(rel string) []string {
	if !r.isDir(rel) {
		return nil
	}
	d, err := os.Open(filepath.Join(r.root, rel))
	if err != nil {
		return nil
	}
	defer d.Close()
	ents, _ := d.ReadDir(maxDirEntries)
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if !fsutil.IsLink(e.Type()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// firstFile returns the first of names that is a regular file.
func (r *repo) firstFile(names ...string) string {
	for _, n := range names {
		if r.isFile(n) {
			return n
		}
	}
	return ""
}

func clean(s string) string {
	s = strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return ' '
		}
		return c
	}, s)
	if len(s) > maxFieldBytes {
		s = s[:maxFieldBytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func fnd(id, title string, ok bool, evidence, fix string) Finding {
	st := StatusWarn
	if ok {
		st = StatusPass
		fix = ""
	}
	return Finding{ID: id, Status: st, Title: title, Evidence: evidence, Fix: fix}
}

func checkAgentDoc(_ context.Context, r *repo, _ Options) Finding {
	const title = "Agent instructions (AGENTS.md or CLAUDE.md)"
	const fix = "Add an AGENTS.md describing the project layout, how to build and test, and the team's rules."
	for _, n := range []string{"AGENTS.md", "CLAUDE.md"} {
		if fi, ok := r.stat(n); ok && fi.Mode().IsRegular() {
			if fi.Size() < minAgentDocLen {
				return fnd("agent-instructions", title, false, n+" is nearly empty", fix)
			}
			return fnd("agent-instructions", title, true, n, "")
		}
	}
	return fnd("agent-instructions", title, false, "no AGENTS.md or CLAUDE.md at the repo root", fix)
}

var (
	taskfileTest = regexp.MustCompile(`(?m)^\s+(test|tests|check)[\w:-]*:\s*(#.*)?$`)
	taskfileName = regexp.MustCompile(`(?m)^\s+([\w:-]+):\s*(#.*)?$`)
	makeTest     = regexp.MustCompile(`(?m)^(test|tests|check)[\w-]*\s*:`)
	makeName     = regexp.MustCompile(`(?m)^([\w-]+)\s*:`)
	jsonTest     = regexp.MustCompile(`"test[\w:-]*"\s*:`)
	jsonLint     = regexp.MustCompile(`"(lint|format|fmt|vet|eslint|prettier|biome|golangci)[\w:-]*"\s*:`)
	npmNoTest    = "no test specified"
)

var taskfileNames = []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"}
var makefileNames = []string{"Makefile", "makefile", "GNUmakefile"}

// pkgScripts returns package.json's script names to bodies; on a parse
// failure (e.g. a file over the read cap) it falls back to a text match.
func pkgScripts(r *repo, re *regexp.Regexp) (found bool, name string) {
	txt, ok := r.read("package.json")
	if !ok {
		return false, ""
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(txt), &p) != nil {
		return re.MatchString(txt), "package.json"
	}
	for k, v := range p.Scripts {
		if re.MatchString(`"`+k+`":`) && !strings.Contains(v, npmNoTest) {
			return true, "package.json script " + k
		}
	}
	return false, ""
}

// workspaceTest finds a real test script in a package dir's package.json.
func workspaceTest(r *repo) (string, bool) {
	for _, d := range r.packageDirs() {
		txt, ok := r.read(d + "/package.json")
		if !ok {
			continue
		}
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal([]byte(txt), &p) != nil {
			continue
		}
		keys := make([]string, 0, len(p.Scripts))
		for k := range p.Scripts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			body := p.Scripts[k]
			if (k == "test" || strings.HasPrefix(k, "test:")) && strings.TrimSpace(body) != "" && !strings.Contains(body, npmNoTest) {
				return d + "/package.json script " + k, true
			}
		}
	}
	return "", false
}

func checkTest(_ context.Context, r *repo, _ Options) Finding {
	const title = "Test command is discoverable"
	const fix = "Define a test target in a Taskfile or Makefile, or a test script in package.json, so an agent can verify its changes."
	if n := r.firstFile(taskfileNames...); n != "" {
		if t, _ := r.read(n); taskfileTest.MatchString(t) {
			return fnd("test-command", title, true, n+" has a test task", "")
		}
	}
	if n := r.firstFile(makefileNames...); n != "" {
		if t, _ := r.read(n); makeTest.MatchString(t) {
			return fnd("test-command", title, true, n+" has a test target", "")
		}
	}
	if ok, what := pkgScripts(r, jsonTest); ok {
		return fnd("test-command", title, true, what, "")
	}
	if what, ok := workspaceTest(r); ok {
		return fnd("test-command", title, true, what, "")
	}
	if r.isFile("go.mod") {
		return fnd("test-command", title, true, "go.mod (go test ./...)", "")
	}
	if r.isFile("Cargo.toml") {
		return fnd("test-command", title, true, "Cargo.toml (cargo test)", "")
	}
	if t, ok := r.read("pyproject.toml"); ok && strings.Contains(t, "pytest") {
		return fnd("test-command", title, true, "pyproject.toml (pytest)", "")
	}
	return fnd("test-command", title, false, "no Taskfile, Makefile, package.json test script, go.mod, Cargo.toml or pytest config found", fix)
}

// typeName reports a script/task name that only type checks.
func typeName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "type") || strings.Contains(l, "tsc")
}

// lintName reports whether a script/task name denotes linting or formatting.
// Type checking alone (check-types, tsc) does not count.
func lintName(name string) bool {
	l := strings.ToLower(name)
	if typeName(l) {
		return false
	}
	for _, w := range []string{"lint", "format", "fmt", "vet", "eslint", "prettier", "biome", "golangci"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// lintBody reports whether a script or file body invokes a linter.
func lintBody(body string) bool {
	for _, w := range []string{"eslint", "prettier", "biome", "golangci", "oxlint", "ruff", "go vet"} {
		if strings.Contains(body, w) {
			return true
		}
	}
	return false
}

// pkgLintScript finds package.json's lint script (name is "" if none) and the
// first type-only script it saw.
func pkgLintScript(r *repo) (name, typeOnly string) {
	txt, ok := r.read("package.json")
	if !ok {
		return "", ""
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(txt), &p) != nil {
		if jsonLint.MatchString(txt) {
			return "package.json", ""
		}
		return "", ""
	}
	keys := make([]string, 0, len(p.Scripts))
	for k := range p.Scripts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if lintName(k) || (k == "check" && lintBody(p.Scripts[k])) {
			return "package.json script " + k, typeOnly
		}
		if typeOnly == "" && typeName(k) {
			typeOnly = k
		}
	}
	return "", typeOnly
}

// scanLintNames returns the first lint-like task/target name in text and the
// first type-only one.
func scanLintNames(re *regexp.Regexp, text string) (lint, typeOnly string) {
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		n := m[1]
		if lintName(n) || n == "lint" || n == "vet" || (n == "check" && lintBody(text)) {
			return n, typeOnly
		}
		l := strings.ToLower(n)
		if typeOnly == "" && typeName(n) && (strings.HasPrefix(l, "check") || strings.HasPrefix(l, "typecheck") || strings.HasPrefix(l, "tsc")) {
			typeOnly = n
		}
	}
	return "", typeOnly
}

func checkLint(_ context.Context, r *repo, _ Options) Finding {
	const title = "Lint or format command is discoverable"
	const fix = "Define a lint target or script, or add a linter config, so an agent can check style before finishing."
	var typeOnly string
	if n := r.firstFile(taskfileNames...); n != "" {
		t, _ := r.read(n)
		lint, to := scanLintNames(taskfileName, t)
		if lint != "" {
			return fnd("lint-command", title, true, n+" has a lint task", "")
		}
		typeOnly = to
	}
	if n := r.firstFile(makefileNames...); n != "" {
		t, _ := r.read(n)
		lint, to := scanLintNames(makeName, t)
		if lint != "" {
			return fnd("lint-command", title, true, n+" has a lint target", "")
		}
		if typeOnly == "" {
			typeOnly = to
		}
	}
	what, to := pkgLintScript(r)
	if what != "" {
		return fnd("lint-command", title, true, what, "")
	}
	if typeOnly == "" {
		typeOnly = to
	}
	for _, n := range []string{
		".golangci.yml", ".golangci.yaml", ".golangci.toml", ".eslintrc", ".eslintrc.json", ".eslintrc.js", ".eslintrc.cjs",
		"eslint.config.js", "eslint.config.mjs", "eslint.config.ts", "biome.json", "biome.jsonc", ".prettierrc",
		"ruff.toml", ".ruff.toml", ".flake8", ".pre-commit-config.yaml", "rustfmt.toml", ".rustfmt.toml", "clippy.toml",
	} {
		if r.isFile(n) {
			return fnd("lint-command", title, true, n, "")
		}
	}
	if t, ok := r.read("pyproject.toml"); ok && strings.Contains(t, "[tool.ruff") {
		return fnd("lint-command", title, true, "pyproject.toml ([tool.ruff])", "")
	}
	ev := "no lint task, script or linter config found"
	if typeOnly != "" {
		ev += " (type checking alone is not lint or format: " + typeOnly + ")"
	}
	return fnd("lint-command", title, false, ev, fix)
}

func checkSetup(_ context.Context, _ *repo, o Options) Finding {
	const title = "Workspace setup script is configured"
	const fix = "Set a setup script in the workspace settings so each new worktree installs its dependencies."
	if o.HasSetupScript {
		return fnd("setup-script", title, true, "setup script set on the workspace", "")
	}
	return fnd("setup-script", title, false, "no setup script on the workspace", fix)
}

type ignoreRule struct {
	re      *regexp.Regexp
	neg     bool
	dirOnly bool
}

// globRegexp turns a gitignore glob (leading "/" already stripped) into an
// anchored regexp; nil if it does not compile.
func globRegexp(p string, anchored bool) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	i := 0
	if strings.HasPrefix(p, "**/") {
		b.WriteString("(?:.*/)?")
		i = 3
	} else if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i < len(p) {
		switch {
		case strings.HasPrefix(p[i:], "/**/"):
			b.WriteString("/(?:.*/)?")
			i += 4
		case p[i:] == "/**":
			b.WriteString("/.*")
			i += 3
		case p[i] == '*':
			b.WriteString("[^/]*")
			i++
		case p[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

func parseIgnore(lines []string) []ignoreRule {
	var rules []ignoreRule
	for _, l := range lines {
		l = strings.TrimRight(strings.TrimSuffix(l, "\r"), " ")
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		var r ignoreRule
		if r.neg = strings.HasPrefix(l, "!"); r.neg {
			l = l[1:]
		}
		if r.dirOnly = strings.HasSuffix(l, "/"); r.dirOnly {
			l = strings.TrimRight(l, "/")
		}
		anchored := strings.HasPrefix(l, "/") || strings.Contains(strings.TrimPrefix(l, "**/"), "/")
		l = strings.TrimPrefix(l, "/")
		if l == "" {
			continue
		}
		if r.re = globRegexp(l, anchored); r.re != nil {
			rules = append(rules, r)
		}
	}
	return rules
}

// ignoreEval reports whether candidate p is ignored; the last matching rule wins.
func ignoreEval(rules []ignoreRule, p string, isDir bool) bool {
	ignored := false
	for _, r := range rules {
		if r.dirOnly && !isDir {
			continue
		}
		if r.re.MatchString(p) {
			ignored = !r.neg
		}
	}
	return ignored
}

// ignores reports whether .gitignore lines cover the root-level directory
// name, directly or by ignoring everything inside it (dist/*, dist/**).
func ignores(lines []string, name string) bool {
	rules := parseIgnore(lines)
	return ignoreEval(rules, name, true) || ignoreEval(rules, name+"/x", false)
}

// ignoresFile reports whether the lines cover the root-level file name.
func ignoresFile(lines []string, name string) bool {
	return ignoreEval(parseIgnore(lines), name, false)
}

func checkGitignore(_ context.Context, r *repo, _ Options) Finding {
	const title = ".gitignore covers secrets and build output"
	const fix = "Add a .gitignore that excludes .env files, dependency folders and build output."
	txt, ok := r.read(".gitignore")
	if !ok {
		return fnd("gitignore", title, false, "no .gitignore at the repo root", fix)
	}
	lines := strings.Split(txt, "\n")
	var missing []string
	if !ignoresFile(lines, ".env") {
		missing = append(missing, ".env")
	}
	if r.isFile("package.json") && !ignores(lines, "node_modules") {
		missing = append(missing, "node_modules")
	}
	build := false
	for _, b := range []string{"dist", "build", "target", "out", "bin"} {
		build = build || ignores(lines, b)
	}
	if !build {
		missing = append(missing, "a build directory (dist, build, target)")
	}
	if len(missing) > 0 {
		return fnd("gitignore", title, false, ".gitignore does not ignore: "+strings.Join(missing, ", "), fix)
	}
	return fnd("gitignore", title, true, ".gitignore", "")
}

func checkTrackedEnv(ctx context.Context, _ *repo, o Options) Finding {
	const title = "No .env file is tracked by git"
	const fix = "Remove the .env file from version control, rotate its secrets, and ignore it."
	if o.TrackedEnv == nil {
		return Finding{ID: "tracked-env", Status: StatusInfo, Title: title, Evidence: "skipped: git is not available for this check"}
	}
	paths, err := o.TrackedEnv(ctx)
	if err != nil {
		return Finding{ID: "tracked-env", Status: StatusInfo, Title: title, Evidence: "skipped: git could not list tracked files"}
	}
	var bad []string
	for _, p := range paths {
		b := path.Base(p)
		if (b == ".env" || strings.HasPrefix(b, ".env.")) && !templateEnv(b) {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		if len(bad) > 3 {
			bad = append(bad[:3], "...")
		}
		return fnd("tracked-env", title, false, "tracked: "+strings.Join(bad, ", "), fix)
	}
	return fnd("tracked-env", title, true, "no tracked .env file", "")
}

func templateEnv(base string) bool {
	for _, s := range []string{"example", "sample", "template", "dist", "defaults"} {
		if strings.HasSuffix(base, "."+s) {
			return true
		}
	}
	return false
}

func checkCI(_ context.Context, r *repo, _ Options) Finding {
	const title = "CI configuration is present"
	const fix = "Add a CI workflow that runs the tests, so agent changes are checked automatically."
	for _, n := range r.list(".github/workflows") {
		if e := path.Ext(n); e == ".yml" || e == ".yaml" {
			return fnd("ci-config", title, true, ".github/workflows/"+n, "")
		}
	}
	for _, n := range []string{".gitlab-ci.yml", ".circleci/config.yml", "Jenkinsfile", "azure-pipelines.yml", "bitbucket-pipelines.yml", ".drone.yml", ".travis.yml", ".buildkite/pipeline.yml"} {
		if r.isFile(n) {
			return fnd("ci-config", title, true, n, "")
		}
	}
	return fnd("ci-config", title, false, "no .github/workflows, .gitlab-ci.yml or other CI config found", fix)
}

func checkDocs(_ context.Context, r *repo, _ Options) Finding {
	const title = "README or docs folder"
	const fix = "Add a README that says what the project is and how to run it."
	if r.isDir("docs") {
		return fnd("docs", title, true, "docs/", "")
	}
	if n := r.firstFile("README.md", "README", "README.rst", "README.txt", "readme.md", "Readme.md"); n != "" {
		return fnd("docs", title, true, n, "")
	}
	return fnd("docs", title, false, "no README or docs/ folder", fix)
}

const maxPackageDirs = 40

var nodeLocks = []string{"package-lock.json", "npm-shrinkwrap.json", "bun.lock", "bun.lockb", "pnpm-lock.yaml", "yarn.lock"}

// packageDirs returns up to maxPackageDirs sorted, de-duplicated repo-relative
// dirs that hold their own package.json: root workspaces entries, the subdirs
// of apps/ and packages/, and top-level subdirs.
func (r *repo) packageDirs() []string {
	seen := map[string]bool{}
	add := func(dir string) {
		dir = path.Clean(dir)
		if dir == "." || dir == ".." || strings.HasPrefix(dir, "../") || strings.HasPrefix(dir, "/") ||
			strings.Contains(dir, "*") || seen[dir] || len(seen) >= maxPackageDirs {
			return
		}
		if base := path.Base(dir); base == "node_modules" || strings.HasPrefix(base, ".") {
			return
		}
		if r.isFile(dir + "/package.json") {
			seen[dir] = true
		}
	}
	addChildren := func(base string) {
		for _, n := range r.list(base) {
			add(base + "/" + n)
		}
	}
	if txt, ok := r.read("package.json"); ok {
		var p struct {
			Workspaces json.RawMessage `json:"workspaces"`
		}
		var entries []string
		if json.Unmarshal([]byte(txt), &p) == nil && len(p.Workspaces) > 0 {
			if json.Unmarshal(p.Workspaces, &entries) != nil {
				var obj struct {
					Packages []string `json:"packages"`
				}
				if json.Unmarshal(p.Workspaces, &obj) == nil {
					entries = obj.Packages
				}
			}
		}
		for _, e := range entries {
			if e == "" || strings.HasPrefix(e, "!") || strings.HasPrefix(e, "/") || strings.Contains(e, "..") {
				continue
			}
			e = strings.TrimSuffix(e, "/")
			if base, ok := strings.CutSuffix(e, "/*"); ok {
				if !strings.Contains(base, "*") {
					addChildren(path.Clean(base))
				}
			} else if !strings.Contains(e, "*") {
				add(e)
			}
		}
	}
	addChildren("apps")
	addChildren("packages")
	for _, n := range r.list(".") {
		add(n)
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// hasDeps reports whether dir's package.json declares dependencies or
// workspaces; true when it cannot be read or parsed.
func (r *repo) hasDeps(dir string) bool {
	txt, ok := r.read(dir + "/package.json")
	if !ok {
		return true
	}
	var p struct {
		Dependencies         map[string]string
		DevDependencies      map[string]string
		OptionalDependencies map[string]string
		PeerDependencies     map[string]string
		Workspaces           json.RawMessage
	}
	if json.Unmarshal([]byte(txt), &p) != nil {
		return true
	}
	return len(p.Dependencies)+len(p.DevDependencies)+len(p.OptionalDependencies)+len(p.PeerDependencies) > 0 || len(p.Workspaces) > 0
}

func checkLockfiles(_ context.Context, r *repo, _ Options) Finding {
	const title = "Lockfiles for the detected ecosystems"
	const fix = "Commit the lockfile so installs are reproducible."
	type eco struct {
		manifest string
		locks    []string
	}
	ecos := []eco{
		{"package.json", nodeLocks},
		{"go.mod", []string{"go.sum"}},
		{"Cargo.toml", []string{"Cargo.lock"}},
		{"pyproject.toml", []string{"poetry.lock", "uv.lock", "pdm.lock", "Pipfile.lock"}},
		{"Gemfile", []string{"Gemfile.lock"}},
	}
	var missing, found []string
	for _, e := range ecos {
		if !r.isFile(e.manifest) {
			continue
		}
		if e.manifest == "go.mod" && r.firstFile("go.sum") == "" {
			if t, _ := r.read("go.mod"); !strings.Contains(t, "require") {
				continue
			}
		}
		if l := r.firstFile(e.locks...); l != "" {
			found = append(found, l)
		} else {
			missing = append(missing, e.manifest+" has no lockfile")
		}
	}
	if !r.isFile("package.json") {
		for _, d := range r.packageDirs() {
			if !r.hasDeps(d) {
				continue
			}
			var l string
			for _, n := range nodeLocks {
				if r.isFile(d + "/" + n) {
					l = d + "/" + n
					break
				}
			}
			if l != "" {
				found = append(found, l)
			} else {
				missing = append(missing, d+"/package.json has no lockfile")
			}
		}
	}
	switch {
	case len(missing) > 0:
		return fnd("lockfiles", title, false, strings.Join(missing, "; "), fix)
	case len(found) > 0:
		return fnd("lockfiles", title, true, strings.Join(found, ", "), "")
	}
	return Finding{ID: "lockfiles", Status: StatusInfo, Title: title, Evidence: "no package manifest detected"}
}

func checkConventions(_ context.Context, r *repo, _ Options) Finding {
	const title = "Contribution or decision-record conventions"
	var have []string
	for _, n := range []string{"CONTRIBUTING.md", "docs/decisions", "docs/adr", "docs/plans"} {
		if _, ok := r.stat(n); ok {
			have = append(have, n)
		}
	}
	ev := "none found (optional)"
	if len(have) > 0 {
		ev = strings.Join(have, ", ")
	}
	return Finding{ID: "conventions", Status: StatusInfo, Title: title, Evidence: ev}
}
