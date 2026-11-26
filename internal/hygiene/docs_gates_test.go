package hygiene

// Public-text gates (E9 P0, docs/plans/completed/e9-p0-ci-guards.md part B). Three checks over the text people outside the
// team read:
//
//   - no other product's name (the list is committed only as SHA-256 hashes, see testdata/foreign-products.sha256);
//   - no local filesystem paths (/Users/<name>, /home/<name>, C:\Users\, /Volumes/);
//   - every backticked repo path (`internal/foo/bar.go`) exists, in the top-level docs; committed ADRs are history, so
//     theirs are held to a shrinking baseline instead.
//
// Findings that existed before the gates are listed in testdata/docs-gates-baseline.txt. Like the sleep guard it is a
// ratchet in both directions: a new finding fails, and a fixed one fails until its line is deleted.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	foreignProductsPath = "internal/hygiene/testdata/foreign-products.sha256"
	docsBaselinePath    = "internal/hygiene/testdata/docs-gates-baseline.txt"
)

// publicTextFiles lists the markdown a reader outside the team sees (repo-relative, slash form): the top-level
// documents, the user docs, the docs site pages and the committed ADRs.
func publicTextFiles(root string) []string {
	var out []string
	for _, f := range []string{"README.md", "CONTRIBUTING.md", "CHANGELOG.md", "SECURITY.md", "CODE_OF_CONDUCT.md"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			out = append(out, f)
		}
	}
	for _, dir := range []string{"public-docs", "docs-site", "docs/decisions"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", "cache", "dist", ".vitepress":
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".md") {
				rel, _ := filepath.Rel(root, p)
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// pathCheckedFiles are the documents whose backticked repo paths must exist now (the ADRs are baselined, plans are
// not scanned: both cite files that were later moved or removed).
func pathCheckedFiles(root string) []string {
	var out []string
	for _, f := range publicTextFiles(root) {
		if strings.HasPrefix(f, "docs/decisions/") || f == "CHANGELOG.md" {
			continue
		}
		out = append(out, f)
	}
	for _, f := range []string{"AGENTS.md"} {
		out = append(out, f)
	}
	if ents, err := os.ReadDir(filepath.Join(root, "docs")); err == nil {
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, "docs/"+e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

var wordRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)

func hashWord(w string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(w)))
	return hex.EncodeToString(sum[:])
}

// parseHashList reads one lowercase hex SHA-256 per line (blank lines and # comments skipped).
func parseHashList(text string) (map[string]bool, error) {
	out := map[string]bool{}
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if len(l) != 64 {
			return nil, fmt.Errorf("line %d: want a 64-character SHA-256 hex digest", i+1)
		}
		out[l] = true
	}
	return out, nil
}

// productFindings returns "file:line: <word>" for each word in text whose hash is on the list. It prints the word found
// (that is the point of the message) and nothing about the list.
func productFindings(file, text string, list map[string]bool) []string {
	var out []string
	for i, l := range strings.Split(text, "\n") {
		for _, w := range wordRe.FindAllString(l, -1) {
			if list[hashWord(w)] {
				out = append(out, fmt.Sprintf("%s:%d: names another product (%q)", file, i+1, w))
			}
		}
	}
	return out
}

var localPathRes = []*regexp.Regexp{
	// A path segment before the slash means a repo path that merely contains "home/" (web/.../home/x.tsx), not a home directory.
	regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])/Users/[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])/home/[A-Za-z0-9._-]+`),
	regexp.MustCompile(`[A-Za-z]:\\Users\\[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])/Volumes/[A-Za-z0-9._-]+`),
}

// placeholderHomes are clearly-an-example account names that may follow /home/ or /Users/ ("smind" is the dedicated service
// account the homelab docs create).
var placeholderHomes = map[string]bool{"user": true, "you": true, "me": true, "name": true, "username": true, "example": true, "yourname": true, "someone": true, "x": true, "alice": true, "bob": true, "smind": true}

const docsGateAllow = "<!-- docs-gate: allow -->"

// pathFindings returns "file:line: <path>" for each local filesystem path in text (a line right after the allow marker
// is exempt, as are placeholder account names).
func pathFindings(file, text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(lines[i-1]) == docsGateAllow {
			continue
		}
		for _, re := range localPathRes {
			for _, m := range re.FindAllString(l, -1) {
				m = strings.TrimLeft(m, " \t(`'\"[=")
				seg := m[strings.LastIndexAny(m, "/\\")+1:]
				if placeholderHomes[strings.ToLower(seg)] {
					continue
				}
				out = append(out, fmt.Sprintf("%s:%d: local path %q", file, i+1, m))
			}
		}
	}
	return out
}

var (
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
	symbolRe     = regexp.MustCompile(`\.[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	repoTops     = []string{"cmd/", "internal/", "web/", "docs/", "desktop/", "mobile/", "scripts/", "public-docs/", "docs-site/", ".github/", "plugin-examples/", "docker/"}
	// ignoredDirsForPaths are gitignored or generated, so they legitimately do not exist in a checkout.
	ignoredDirsForPaths = []string{"docs/research/local"}
)

// stripCodeFences blanks the lines of fenced blocks (keeping the line count): only inline spans are path claims.
func stripCodeFences(md string) string {
	lines := strings.Split(md, "\n")
	in := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") || strings.HasPrefix(strings.TrimSpace(l), "~~~") {
			in = !in
			lines[i] = ""
			continue
		}
		if in {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// missingPaths returns "file:line: `path`" for each backticked repo path in md that does not exist (relative to the repo
// root or to the file's own directory).
func missingPaths(root, file, md string) []string {
	var out []string
	for i, l := range strings.Split(stripCodeFences(md), "\n") {
		for _, m := range inlineCodeRe.FindAllStringSubmatch(l, -1) {
			p := strings.TrimRight(strings.TrimSpace(m[1]), ".,:;")
			if c := strings.Index(p, ":"); c > 0 {
				p = p[:c] // `file.go:12`, `file.go:12-20,30`, `file.go:Symbol`
			}
			if strings.ContainsAny(p, " *<>{}$|\t") {
				continue
			}
			top := false
			for _, t := range repoTops {
				if strings.HasPrefix(p, t) {
					top = true
					break
				}
			}
			if !top {
				continue
			}
			skip := false
			for _, ig := range ignoredDirsForPaths {
				if p == ig || strings.HasPrefix(p, ig+"/") {
					skip = true
				}
			}
			if skip {
				continue
			}
			clean := strings.TrimSuffix(p, "/")
			if pathExists(root, file, clean) {
				continue
			}
			// `internal/pkg.Symbol` names a Go symbol: the package directory must exist ("internal/pkg.Type.Method" too).
			// Only Go package paths (cmd/, internal/) name symbols; `mobile/App.tsx` is a file, never `mobile/App` + `.tsx`.
			isGoPkg := strings.HasPrefix(clean, "cmd/") || strings.HasPrefix(clean, "internal/")
			if m := symbolRe.FindString(clean); m != "" && isGoPkg && !strings.Contains(filepath.Base(clean[:len(clean)-len(m)]), ".") {
				if pkg := clean[:len(clean)-len(m)]; pathExists(root, file, pkg) {
					continue
				}
			}
			out = append(out, fmt.Sprintf("%s:%d: `%s` does not exist", file, i+1, p))
		}
	}
	return out
}

func pathExists(root, file, clean string) bool {
	return existsExactCase(root, filepath.FromSlash(clean)) ||
		existsExactCase(root, filepath.Join(filepath.Dir(filepath.FromSlash(file)), filepath.FromSlash(clean)))
}

// existsExactCase reports whether rel exists under root with exactly this
// spelling. os.Stat alone is case-insensitive on the default macOS and Windows
// file systems, so `mobile/App` would match `mobile/app` there and fail only on
// Linux CI.
func existsExactCase(root, rel string) bool {
	rel = filepath.Clean(rel)
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		return false
	}
	dir := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		if part == ".." {
			dir = filepath.Dir(dir)
			continue
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		found := false
		for _, e := range ents {
			if e.Name() == part {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		dir = filepath.Join(dir, part)
	}
	return true
}

// baselineKey drops the line number so a finding survives edits above it: "file: <what>".
func baselineKey(finding string) string {
	i := strings.Index(finding, ":")
	rest := finding[i+1:]
	j := strings.Index(rest, ": ")
	return finding[:i] + ": " + rest[j+2:]
}

// compareDocsBaseline is the two-way ratchet: findings not in the baseline fail, baseline lines no longer found fail.
func compareDocsBaseline(findings []string, baseline map[string]bool) []string {
	seen := map[string]bool{}
	var bad []string
	for _, f := range findings {
		k := baselineKey(f)
		seen[k] = true
		if !baseline[k] {
			bad = append(bad, f)
		}
	}
	var gone []string
	for k := range baseline {
		if !seen[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		bad = append(bad, fmt.Sprintf("baseline line is fixed, delete it from %s: %s", docsBaselinePath, k))
	}
	return bad
}

func parseDocsBaseline(text string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out[l] = true
		}
	}
	return out
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPublicTextHasNoForeignProductNames(t *testing.T) {
	root := repoRoot(t)
	list, err := parseHashList(readRepoFile(t, root, foreignProductsPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the product list is empty: the gate checks nothing")
	}
	var findings []string
	for _, f := range publicTextFiles(root) {
		findings = append(findings, productFindings(f, readRepoFile(t, root, f), list)...)
	}
	baseline := parseDocsBaseline(readRepoFile(t, root, docsBaselinePath))
	for _, b := range compareDocsBaseline(onlyKind(findings, "names another product"), filterBaseline(baseline, "names another product")) {
		t.Error(b)
	}
}

func TestPublicTextHasNoLocalPaths(t *testing.T) {
	root := repoRoot(t)
	var findings []string
	for _, f := range publicTextFiles(root) {
		findings = append(findings, pathFindings(f, readRepoFile(t, root, f))...)
	}
	baseline := parseDocsBaseline(readRepoFile(t, root, docsBaselinePath))
	for _, b := range compareDocsBaseline(onlyKind(findings, "local path"), filterBaseline(baseline, "local path")) {
		t.Error(b)
	}
}

func TestBacktickedRepoPathsExist(t *testing.T) {
	root := repoRoot(t)
	var findings []string
	for _, f := range pathCheckedFiles(root) {
		findings = append(findings, missingPaths(root, f, readRepoFile(t, root, f))...)
	}
	// Committed ADRs are history: their findings are baselined, not hard failures.
	for _, f := range publicTextFiles(root) {
		if strings.HasPrefix(f, "docs/decisions/") {
			findings = append(findings, missingPaths(root, f, readRepoFile(t, root, f))...)
		}
	}
	baseline := parseDocsBaseline(readRepoFile(t, root, docsBaselinePath))
	for _, b := range compareDocsBaseline(onlyKind(findings, "does not exist"), filterBaseline(baseline, "does not exist")) {
		t.Error(b)
	}
}

func onlyKind(findings []string, kind string) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f, kind) {
			out = append(out, f)
		}
	}
	return out
}

func filterBaseline(b map[string]bool, kind string) map[string]bool {
	out := map[string]bool{}
	for k := range b {
		if strings.Contains(k, kind) {
			out[k] = true
		}
	}
	return out
}

// TestForeignProductListIsHashedOnly: the committed list holds digests, never words (a 64-hex line per entry), and the
// checker prints the word it found but nothing about the list.
func TestForeignProductListIsHashedOnly(t *testing.T) {
	root := repoRoot(t)
	raw := readRepoFile(t, root, foreignProductsPath)
	for i, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(l) {
			t.Errorf("%s line %d is not a lowercase SHA-256 digest", foreignProductsPath, i+1)
		}
	}
	list := map[string]bool{hashWord("Acme"): true}
	got := productFindings("x.md", "we beat acme today", list)
	if len(got) != 1 || strings.Contains(got[0], "baseline") || strings.Contains(got[0], "list") {
		t.Fatalf("finding should name the word only: %v", got)
	}
}

// TestDocsGatesFireOnSeededInput: every gate fires on a bad input and stays quiet on a clean one.
func TestDocsGatesFireOnSeededInput(t *testing.T) {
	list := map[string]bool{hashWord("Acme"): true}
	if got := productFindings("a.md", "clean text\nthe ACME tool", list); len(got) != 1 || !strings.Contains(got[0], "a.md:2") {
		t.Errorf("product name must fire on line 2 only (case-insensitive): %v", got)
	}
	if got := productFindings("a.md", "nothing here, acmeish is a different word", list); len(got) != 0 {
		t.Errorf("a different word must not fire: %v", got)
	}
	for _, bad := range []string{"cd /Users/jane/code", "see /home/jane/x", `C:\Users\jane`, "/Volumes/Disk/x"} {
		if got := pathFindings("a.md", bad); len(got) != 1 {
			t.Errorf("%q must fire: %v", bad, got)
		}
	}
	for _, ok := range []string{"cd /home/user/code", "/Users/you/x", "no path here", "~/.smind/config.yaml"} {
		if got := pathFindings("a.md", ok); len(got) != 0 {
			t.Errorf("%q must not fire: %v", ok, got)
		}
	}
	if got := pathFindings("a.md", docsGateAllow+"\n/Users/jane/x"); len(got) != 0 {
		t.Errorf("the allow marker must exempt the next line: %v", got)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "real", "x.go"), []byte("package real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "see `internal/real/x.go:12` and `internal/real/x.go:3-9` and `internal/nosuch/y.go`, `docs/research/local/` and `web/*.ts`\n```\n`internal/in/fence.go`\n```\n"
	got := missingPaths(root, "README.md", md)
	if len(got) != 1 || !strings.Contains(got[0], "internal/nosuch/y.go") || !strings.Contains(got[0], "README.md:1") {
		t.Errorf("only the missing path outside fences must fire: %v", got)
	}
}

// TestDocsPathBaselineCanOnlyShrink: a finding missing from the baseline fails, a baseline line with no finding fails.
func TestDocsPathBaselineCanOnlyShrink(t *testing.T) {
	f := []string{"docs/decisions/0001.md:4: `internal/old.go` does not exist"}
	if got := compareDocsBaseline(f, map[string]bool{baselineKey(f[0]): true}); len(got) != 0 {
		t.Errorf("a baselined finding passes: %v", got)
	}
	if got := compareDocsBaseline(f, map[string]bool{}); len(got) != 1 {
		t.Errorf("a new finding fails: %v", got)
	}
	if got := compareDocsBaseline(nil, map[string]bool{"x.md: `a/b` does not exist": true}); len(got) != 1 || !strings.Contains(got[0], "delete it") {
		t.Errorf("a fixed finding demands deleting its line: %v", got)
	}
	if baselineKey("docs/a.md:12: `p/q` does not exist") != baselineKey("docs/a.md:99: `p/q` does not exist") {
		t.Error("the baseline key must not depend on the line number")
	}
}

// TestPathExistsIsCaseExact pins the Linux behaviour on case-insensitive file
// systems: a differently cased path does not exist, and a non-Go path is never
// read as a package plus a symbol.
func TestPathExistsIsCaseExact(t *testing.T) {
	root := repoRoot(t)
	if !existsExactCase(root, filepath.FromSlash("mobile/app")) {
		t.Fatal("mobile/app should exist")
	}
	if existsExactCase(root, filepath.FromSlash("mobile/App")) {
		t.Fatal("mobile/App must not match mobile/app")
	}
	if got := missingPaths(root, "README.md", "see `mobile/App.tsx`\n"); len(got) != 1 {
		t.Fatalf("`mobile/App.tsx` must be a missing path, got %v", got)
	}
}
