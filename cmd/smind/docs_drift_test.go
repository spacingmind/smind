package main

// Drift check for the user documentation (public-docs/ and README.md). It
// runs in the normal `go test ./...`, needs no network and no daemon, and
// fails when a page:
//   - links to a file or heading that does not exist,
//   - lacks its front matter,
//   - mentions a `smind ...` command the CLI no longer has,
//   - lets the generated CLI summary fall behind `smind --help`,
//   - leaks internal references (ADR/PR numbers, personal paths, key-like strings).
//
// Regenerate the CLI summary block with `task docs:cli`.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	cliPage        = "cli-reference.md"
	helpBeginMark  = "<!-- BEGIN GENERATED: smind --help -->"
	helpEndMark    = "<!-- END GENERATED: smind --help -->"
	relayBeginMark = "<!-- BEGIN GENERATED: smind relay --help -->"
	relayEndMark   = "<!-- END GENERATED: smind relay --help -->"
	// readmeForbidden turns the forbidden-string rule on for README.md too.
	// The README move (last docs PR) flips it.
	readmeForbidden = false
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func docPages(t *testing.T, root string) []string {
	t.Helper()
	pages, err := filepath.Glob(filepath.Join(root, "public-docs", "*.md"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no pages under public-docs/: %v", err)
	}
	sort.Strings(pages)
	return pages
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stripFences blanks the lines of fenced code blocks (keeping line count).
func stripFences(md string) string {
	var out []string
	in := false
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			in = !in
			out = append(out, "")
			continue
		}
		if in {
			out = append(out, "")
		} else {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

var (
	linkRe      = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	inlineCode  = regexp.MustCompile("`[^`\n]+`")
	headingRe   = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*#*\s*$`)
	slugDropRe  = regexp.MustCompile(`[^\p{L}\p{N}\s-]`)
	frontKeyRe  = regexp.MustCompile(`^([a-z]+):\s*(.*)$`)
	smindCmdRe  = regexp.MustCompile(`(?:^|[\s$("'])smind((?:\s+[^\s"'` + "`" + `)|;&<>]+){0,3})`)
	bareWordRe  = regexp.MustCompile(`^[a-z][a-z-]*$`)
	helpLineRe  = regexp.MustCompile(`(?m)^\s+smind((?: [a-z][a-z-]*){1,3})`)
	commentTail = regexp.MustCompile(`\s#\s.*$`)
)

// slug mimics GitHub's heading anchors: lowercase, punctuation dropped,
// spaces to hyphens.
func slug(h string) string {
	h = inlineCode.ReplaceAllStringFunc(h, func(s string) string { return strings.Trim(s, "`") })
	h = strings.ToLower(strings.TrimSpace(h))
	h = slugDropRe.ReplaceAllString(h, "")
	return strings.ReplaceAll(h, " ", "-")
}

func anchors(md string) map[string]bool {
	set := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(stripFences(md), -1) {
		set[slug(m[2])] = true
	}
	return set
}

// checkLinks returns one message per broken relative link in the file.
func checkLinks(root, path, md string) []string {
	var bad []string
	body := stripFences(md)
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		file, frag, _ := strings.Cut(target, "#")
		dest := path
		if file != "" {
			dest = filepath.Join(filepath.Dir(path), filepath.FromSlash(file))
		}
		st, err := os.Stat(dest)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: link %q: no such file", rel(root, path), target))
			continue
		}
		if frag != "" && !st.IsDir() && strings.HasSuffix(dest, ".md") {
			b, _ := os.ReadFile(dest)
			if !anchors(string(b))[frag] {
				bad = append(bad, fmt.Sprintf("%s: link %q: no heading #%s in %s", rel(root, path), target, frag, rel(root, dest)))
			}
		}
	}
	return bad
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

func TestDocsLinksResolve(t *testing.T) {
	root := repoRoot(t)
	files := append(docPages(t, root), filepath.Join(root, "README.md"))
	for _, f := range files {
		for _, msg := range checkLinks(root, f, readFile(t, f)) {
			t.Error(msg)
		}
	}
}

// parseFrontMatter returns the key/values of a leading ---...--- block.
func parseFrontMatter(md string) (map[string]string, bool) {
	lines := strings.Split(md, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, false
	}
	kv := map[string]string{}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			return kv, true
		}
		if m := frontKeyRe.FindStringSubmatch(l); m != nil {
			kv[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `"`)
		}
	}
	return nil, false
}

// frontMatterProblems validates one page; orders collects the order values
// seen so far to catch duplicates.
func frontMatterProblems(name, md string, orders map[int]string) []string {
	kv, ok := parseFrontMatter(md)
	if !ok {
		return []string{name + ": missing front matter (--- ... ---)"}
	}
	var bad []string
	for _, k := range []string{"title", "description", "order"} {
		if kv[k] == "" {
			bad = append(bad, fmt.Sprintf("%s: front matter lacks %q", name, k))
		}
	}
	if o, err := strconv.Atoi(kv["order"]); kv["order"] != "" {
		if err != nil || o < 1 {
			bad = append(bad, fmt.Sprintf("%s: order %q is not a positive integer", name, kv["order"]))
		} else if prev, dup := orders[o]; dup {
			bad = append(bad, fmt.Sprintf("%s: order %d already used by %s", name, o, prev))
		} else {
			orders[o] = name
		}
	}
	return bad
}

func TestDocsFrontMatter(t *testing.T) {
	root := repoRoot(t)
	orders := map[int]string{}
	for _, f := range docPages(t, root) {
		for _, msg := range frontMatterProblems(rel(root, f), readFile(t, f), orders) {
			t.Error(msg)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "public-docs", "index.md")); err != nil {
		t.Error("public-docs/index.md is missing")
	}
}

// cliTree maps a function name (cmdTask, cmdTaskChat, run, ...) to the
// non-flag string literals in its switch cases, read from the real dispatch
// code of this package.
func cliTree(t *testing.T) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tree := map[string]map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil {
					continue
				}
				if fn.Name.Name != "run" && !strings.HasPrefix(fn.Name.Name, "cmd") {
					continue
				}
				set := map[string]bool{}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					cc, ok := n.(*ast.CaseClause)
					if !ok {
						return true
					}
					for _, e := range cc.List {
						if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if s, err := strconv.Unquote(lit.Value); err == nil && bareWordRe.MatchString(strings.TrimLeft(s, "-")) {
								set[s] = true
							}
						}
					}
					return true
				})
				if len(set) > 0 {
					tree[fn.Name.Name] = set
				}
			}
		}
	}
	return tree
}

func titleCase(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(s, "-") {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

// commandProblem reports why the words after "smind" are not a command of
// the CLI, or "" when they are. Words that are not bare lowercase words
// (flags, <placeholders>, paths) end the check.
func commandProblem(tree map[string]map[string]bool, words []string) string {
	if len(words) == 0 {
		return ""
	}
	if strings.HasPrefix(words[0], "-") {
		if tree["run"][words[0]] {
			return ""
		}
		return fmt.Sprintf("unknown flag %q", words[0])
	}
	if !bareWordRe.MatchString(words[0]) {
		return ""
	}
	if !tree["run"][words[0]] {
		return fmt.Sprintf("unknown command %q", words[0])
	}
	fn := "cmd" + titleCase(words[0])
	for _, w := range words[1:] {
		subs := tree[fn]
		if subs == nil || !bareWordRe.MatchString(w) {
			return ""
		}
		if !subs[w] {
			return fmt.Sprintf("unknown subcommand %q of %q", w, strings.TrimPrefix(strings.ToLower(fn), "cmd"))
		}
		fn += titleCase(w)
	}
	return ""
}

// codeLines yields the lines a `smind ...` command can appear on: fenced
// code lines (comments cut) and inline code spans.
func codeLines(md string) []string {
	var out []string
	in, prose := false, false
	for _, l := range strings.Split(md, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") {
			in = !in
			// A ```text fence holds program output (the generated help),
			// not commands to run.
			prose = in && strings.TrimSpace(strings.TrimPrefix(t, "```")) == "text"
			continue
		}
		if in && prose {
			continue
		}
		if in {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "#") {
				continue
			}
			out = append(out, commentTail.ReplaceAllString(l, ""))
			continue
		}
		for _, c := range inlineCode.FindAllString(l, -1) {
			out = append(out, strings.Trim(c, "`"))
		}
	}
	return out
}

func commandMentions(md string) [][]string {
	var found [][]string
	for _, l := range codeLines(md) {
		for _, m := range smindCmdRe.FindAllStringSubmatch(l, -1) {
			found = append(found, strings.Fields(m[1]))
		}
	}
	return found
}

func TestDocsCommandsExist(t *testing.T) {
	root := repoRoot(t)
	tree := cliTree(t)
	for _, want := range []string{"serve", "task", "relay"} {
		if !tree["run"][want] {
			t.Fatalf("command tree extraction is broken: run() has no %q case", want)
		}
	}
	files := append(docPages(t, root), filepath.Join(root, "README.md"))
	for _, f := range files {
		for _, words := range commandMentions(readFile(t, f)) {
			if p := commandProblem(tree, words); p != "" {
				t.Errorf("%s: `smind %s`: %s", rel(root, f), strings.Join(words, " "), p)
			}
		}
	}
	// The generated help blocks (```text fences are skipped above) must still
	// name only commands that exist: a help line left behind a removed
	// command fails here.
	page := readFile(t, filepath.Join(root, "public-docs", cliPage))
	for _, m := range helpLineRe.FindAllStringSubmatch(page, -1) {
		if p := commandProblem(tree, strings.Fields(m[1])); p != "" {
			t.Errorf("public-docs/%s: help line `smind %s`: %s", cliPage, strings.TrimSpace(m[1]), p)
		}
	}
	// Every top-level command is documented on the CLI reference page.
	for cmd := range tree["run"] {
		if strings.HasPrefix(cmd, "-") || cmd == "help" || cmd == "version" {
			continue
		}
		if !regexp.MustCompile(`smind ` + regexp.QuoteMeta(cmd) + `\b`).MatchString(page) {
			t.Errorf("public-docs/%s does not mention `smind %s`", cliPage, cmd)
		}
	}
}

func TestCommandProblemRules(t *testing.T) {
	tree := cliTree(t)
	ok := [][]string{
		{"serve"}, {"task", "send", "<taskId>"}, {"task", "chat", "ls"}, {"relay", "host", "new"},
		{"relay", "devices", "approve"}, {"workspace", "setup", "show"}, {"config", "get", "server.port"},
		{"mcp", "serve"}, {"profile", "seed", "roles"}, {"--version"}, {"service", "install", "--system"},
	}
	for _, w := range ok {
		if p := commandProblem(tree, w); p != "" {
			t.Errorf("smind %s: unexpected problem %q", strings.Join(w, " "), p)
		}
	}
	bad := [][]string{{"nosuchcmd"}, {"task", "nosuchsub"}, {"task", "chat", "nosuch"}, {"relay", "host", "nosuch"}, {"--nosuchflag"}}
	for _, w := range bad {
		if commandProblem(tree, w) == "" {
			t.Errorf("smind %s: expected a problem", strings.Join(w, " "))
		}
	}
}

var forbidden = []*regexp.Regexp{
	regexp.MustCompile(`/Users/`),
	regexp.MustCompile(`\bsk-`),
	regexp.MustCompile(`ADR-`),
	regexp.MustCompile(`PR #`),
	regexp.MustCompile(`#[0-9]{3}`),
}

// forbiddenHits scans md outside "Further reading" sections.
func forbiddenHits(name, md string) []string {
	var bad []string
	skip := false
	skipLevel := 0
	for i, l := range strings.Split(md, "\n") {
		if m := headingRe.FindStringSubmatch(l); m != nil && !strings.HasPrefix(l, "    ") {
			level := len(m[1])
			if strings.EqualFold(strings.TrimSpace(m[2]), "Further reading") {
				skip, skipLevel = true, level
				continue
			}
			if skip && level <= skipLevel {
				skip = false
			}
		}
		if skip {
			continue
		}
		for _, re := range forbidden {
			if re.MatchString(l) {
				bad = append(bad, fmt.Sprintf("%s:%d: forbidden %s in user text", name, i+1, re))
			}
		}
	}
	return bad
}

func TestDocsForbiddenStrings(t *testing.T) {
	root := repoRoot(t)
	files := docPages(t, root)
	if readmeForbidden {
		files = append(files, filepath.Join(root, "README.md"))
	}
	for _, f := range files {
		for _, msg := range forbiddenHits(rel(root, f), readFile(t, f)) {
			t.Error(msg)
		}
	}
}

// helpBlock renders the fenced block for a generated section.
func helpBlock(begin, end string, render func(*bytes.Buffer)) string {
	var b bytes.Buffer
	b.WriteString(begin + "\n```text\n")
	render(&b)
	b.WriteString("```\n" + end)
	return b.String()
}

// replaceBlock swaps the text from begin to end (inclusive) for block.
func replaceBlock(md, begin, end, block string) (string, bool) {
	i := strings.Index(md, begin)
	j := strings.Index(md, end)
	if i < 0 || j < i {
		return md, false
	}
	return md[:i] + block + md[j+len(end):], true
}

func TestCLIReferenceMatchesHelp(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "public-docs", cliPage)
	md := readFile(t, path)
	want := md
	for _, blk := range []struct {
		begin, end string
		render     func(*bytes.Buffer)
	}{
		{helpBeginMark, helpEndMark, func(b *bytes.Buffer) { printUsage(b) }},
		{relayBeginMark, relayEndMark, func(b *bytes.Buffer) { printRelayUsage(b) }},
	} {
		var ok bool
		want, ok = replaceBlock(want, blk.begin, blk.end, helpBlock(blk.begin, blk.end, blk.render))
		if !ok {
			t.Fatalf("public-docs/%s lacks the %q ... %q markers", cliPage, blk.begin, blk.end)
		}
	}
	if os.Getenv("SMIND_UPDATE_DOCS") == "1" {
		if want != md {
			if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	if want != md {
		t.Errorf("public-docs/%s is out of date with `smind --help`; run `task docs:cli`", cliPage)
	}
}
