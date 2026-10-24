package taskrunner

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

// GitRuleClass says how a git/gh operation is treated by the approval policy.
type GitRuleClass string

const (
	GitClassReadOnly    GitRuleClass = "read-only"
	GitClassCommit      GitRuleClass = "commit"
	GitClassAlwaysAsk   GitRuleClass = "always-ask"
	GitClassUnparsedGit GitRuleClass = "unparsed-git"
)

// GitRule is one classified git/gh operation found in a shell command.
type GitRule struct {
	ID      string
	Class   GitRuleClass
	Label   string
	Argv    []string
	Command string
	Remote  string
	Branch  string
}

var errGitParse = errors.New("unparsable shell command")

const maxGitParseDepth = 8

// ClassifyGitCommand returns the most severe git/gh rule found in cmd
// (always-ask/unparsed-git > commit > read-only), or nil when cmd has no
// git/gh operation. It is a parser, not a prefix match: quoting, separators,
// subshells, command substitution and common wrappers are understood, and a
// command that mentions git/gh as a word but cannot be parsed fails closed as
// unparsed-git. Aliases from gitconfig files and scripts that run git are not
// visible to it.
func ClassifyGitCommand(cmd string) *GitRule {
	rules, err := classifyShell(cmd, 0)
	if err != nil {
		if gitWordRe.MatchString(cmd) {
			return unparsedGitRule(cmd)
		}
		return nil
	}
	var best *GitRule
	for _, r := range rules {
		if best == nil || gitSeverity(r.Class) > gitSeverity(best.Class) {
			best = r
		}
	}
	return best
}

func gitSeverity(c GitRuleClass) int {
	switch c {
	case GitClassAlwaysAsk:
		return 4
	case GitClassUnparsedGit:
		return 3
	case GitClassCommit:
		return 2
	case GitClassReadOnly:
		return 1
	}
	return 0
}

func unparsedGitRule(cmd string) *GitRule {
	c := strings.TrimSpace(cmd)
	return &GitRule{
		ID:      "unparsed-git",
		Class:   GitClassUnparsedGit,
		Label:   "git command could not be parsed and always needs your approval",
		Argv:    strings.Fields(c),
		Command: c,
	}
}

// gitWordRe matches git or gh as a standalone word (also as a path basename).
var gitWordRe = regexp.MustCompile(`(^|[^\w.-])(git|gh)($|[^\w.-])`)

type shWord struct {
	text    string
	dynamic bool // contains a parameter or command expansion
}

func wordTexts(ws []shWord) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.text
	}
	return out
}

// shScanner is a small POSIX-ish tokenizer: it splits a command string into
// simple commands and collects the bodies of $( ) and backtick substitutions.
type shScanner struct {
	s      string
	i      int
	cmds   [][]shWord
	subs   []string
	cur    []shWord
	buf    []byte
	inWord bool
	dyn    bool
	depth  int
}

func scanShell(s string) ([][]shWord, []string, error) {
	sc := &shScanner{s: s}
	if err := sc.run(); err != nil {
		return nil, nil, err
	}
	return sc.cmds, sc.subs, nil
}

func (sc *shScanner) endWord() {
	if sc.inWord {
		sc.cur = append(sc.cur, shWord{text: string(sc.buf), dynamic: sc.dyn})
		sc.buf = sc.buf[:0]
		sc.inWord = false
		sc.dyn = false
	}
}

func (sc *shScanner) endCmd() {
	sc.endWord()
	if len(sc.cur) > 0 {
		sc.cmds = append(sc.cmds, sc.cur)
		sc.cur = nil
	}
}

func (sc *shScanner) put(b ...byte) {
	sc.buf = append(sc.buf, b...)
	sc.inWord = true
}

func (sc *shScanner) run() error {
	s := sc.s
	for sc.i < len(s) {
		c := s[sc.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			sc.endWord()
			sc.i++
		case c == '\n' || c == ';' || c == '|' || c == '&':
			sc.endCmd()
			sc.i++
		case c == '(':
			sc.endCmd()
			sc.depth++
			sc.i++
		case c == ')':
			sc.endCmd()
			sc.depth--
			if sc.depth < 0 {
				return errGitParse
			}
			sc.i++
		case c == '#' && !sc.inWord:
			for sc.i < len(s) && s[sc.i] != '\n' {
				sc.i++
			}
		case c == '\\':
			switch {
			case sc.i+1 >= len(s):
				sc.i++
			case s[sc.i+1] == '\n':
				sc.i += 2
			default:
				sc.put(s[sc.i+1])
				sc.i += 2
			}
		case c == '\'':
			j := strings.IndexByte(s[sc.i+1:], '\'')
			if j < 0 {
				return errGitParse
			}
			sc.inWord = true
			sc.put([]byte(s[sc.i+1 : sc.i+1+j])...)
			sc.i += j + 2
		case c == '"':
			if err := sc.doubleQuoted(); err != nil {
				return err
			}
		case c == '$':
			if err := sc.dollar(); err != nil {
				return err
			}
		case c == '`':
			if err := sc.backtick(); err != nil {
				return err
			}
		default:
			sc.put(c)
			sc.i++
		}
	}
	if sc.depth != 0 {
		return errGitParse
	}
	sc.endCmd()
	return nil
}

func (sc *shScanner) doubleQuoted() error {
	s := sc.s
	sc.i++
	sc.inWord = true
	for {
		if sc.i >= len(s) {
			return errGitParse
		}
		c := s[sc.i]
		switch {
		case c == '"':
			sc.i++
			return nil
		case c == '\\' && sc.i+1 < len(s):
			n := s[sc.i+1]
			if n == '"' || n == '\\' || n == '$' || n == '`' {
				sc.put(n)
			} else if n != '\n' {
				sc.put('\\', n)
			}
			sc.i += 2
		case c == '$':
			if err := sc.dollar(); err != nil {
				return err
			}
		case c == '`':
			if err := sc.backtick(); err != nil {
				return err
			}
		default:
			sc.put(c)
			sc.i++
		}
	}
}

func (sc *shScanner) dollar() error {
	s := sc.s
	if sc.i+1 >= len(s) {
		sc.put('$')
		sc.i++
		return nil
	}
	n := s[sc.i+1]
	switch {
	case n == '(':
		end, err := matchParen(s, sc.i+2)
		if err != nil {
			return err
		}
		sc.subs = append(sc.subs, s[sc.i+2:end])
		sc.put([]byte(s[sc.i : end+1])...)
		sc.dyn = true
		sc.i = end + 1
	case n == '{':
		j := strings.IndexByte(s[sc.i:], '}')
		if j < 0 {
			return errGitParse
		}
		sc.put([]byte(s[sc.i : sc.i+j+1])...)
		sc.dyn = true
		sc.i += j + 1
	case isNameByte(n):
		j := sc.i + 1
		for j < len(s) && isNameByte(s[j]) {
			j++
		}
		sc.put([]byte(s[sc.i:j])...)
		sc.dyn = true
		sc.i = j
	case n == '@' || n == '*' || n == '#' || n == '?' || n == '$' || n == '!' || n == '-':
		sc.put('$', n)
		sc.dyn = true
		sc.i += 2
	default:
		sc.put('$')
		sc.i++
	}
	return nil
}

func isNameByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func (sc *shScanner) backtick() error {
	s := sc.s
	var inner []byte
	j := sc.i + 1
	for {
		if j >= len(s) {
			return errGitParse
		}
		if s[j] == '\\' && j+1 < len(s) && s[j+1] == '`' {
			inner = append(inner, '`')
			j += 2
			continue
		}
		if s[j] == '`' {
			break
		}
		inner = append(inner, s[j])
		j++
	}
	sc.subs = append(sc.subs, string(inner))
	sc.put([]byte(s[sc.i : j+1])...)
	sc.dyn = true
	sc.i = j + 1
	return nil
}

// matchParen returns the index of the ')' closing a "(" whose body starts at
// start, skipping quoted text and nested parentheses.
func matchParen(s string, start int) (int, error) {
	depth := 1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return 0, errGitParse
			}
			i += j + 1
		case '"':
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(s) {
				return 0, errGitParse
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errGitParse
}

func classifyShell(cmd string, depth int) ([]*GitRule, error) {
	if depth > maxGitParseDepth {
		return nil, errGitParse
	}
	cmds, subs, err := scanShell(cmd)
	if err != nil {
		return nil, err
	}
	var rules []*GitRule
	for _, sub := range subs {
		rs, err := classifyShell(sub, depth+1)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rs...)
	}
	for _, words := range cmds {
		rs, err := classifySimple(words, depth)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rs...)
	}
	return rules, nil
}

var (
	assignRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	varHintRe = regexp.MustCompile(`(?i)\$\{?(\w*git\w*|(\w*_)?gh(_\w*)?)\b`)
)

var shellKeywords = map[string]bool{
	"!": true, "{": true, "}": true, "then": true, "do": true, "else": true,
	"elif": true, "if": true, "while": true, "until": true, "fi": true,
	"done": true, "esac": true,
}

// wrapperArgFlags lists, per wrapper command, the options that consume the
// next word. Other leading options are skipped as plain flags.
var wrapperArgFlags = map[string]map[string]bool{
	"env":   {"-u": true, "-C": true, "-S": true},
	"sudo":  {"-u": true, "-g": true, "-h": true, "-p": true, "-C": true, "-r": true, "-t": true, "-U": true, "-D": true, "-R": true, "-T": true},
	"time":  {"-f": true, "-o": true},
	"nohup": {},
	"exec":  {"-a": true},
	"nice":  {"-n": true},
}

func classifySimple(words []shWord, depth int) ([]*GitRule, error) {
	for range maxGitParseDepth * 2 {
		for len(words) > 0 && !words[0].dynamic && assignRe.MatchString(words[0].text) {
			words = words[1:]
		}
		if len(words) == 0 {
			return nil, nil
		}
		w0 := words[0]
		if w0.dynamic {
			if gitWordRe.MatchString(w0.text) || varHintRe.MatchString(w0.text) || mentionsGit(words[1:]) {
				return []*GitRule{unparsedGitRule(strings.Join(wordTexts(words), " "))}, nil
			}
			return nil, nil
		}
		if shellKeywords[w0.text] {
			words = words[1:]
			continue
		}
		base := path.Base(w0.text)
		switch base {
		case "git":
			return classifyGit(words, depth)
		case "gh":
			return classifyGh(words), nil
		case "sh", "bash", "zsh", "dash", "ksh":
			script, ok := shellDashC(words[1:])
			if !ok {
				return nil, nil
			}
			return classifyShell(script, depth+1)
		case "env", "sudo", "time", "nohup", "exec", "nice":
			words = skipWrapperOptions(words[1:], wrapperArgFlags[base])
		case "command":
			rest := skipWrapperOptions(words[1:], nil)
			if hasArg(wordTexts(words[1:len(words)-len(rest)]), "-v") || hasArg(wordTexts(words[1:len(words)-len(rest)]), "-V") {
				return nil, nil
			}
			words = rest
		case "eval", "xargs":
			if mentionsGit(words[1:]) {
				return []*GitRule{unparsedGitRule(strings.Join(wordTexts(words), " "))}, nil
			}
			return nil, nil
		case "find":
			if mentionsGit(words[1:]) && hasExecPrimary(words[1:]) {
				return []*GitRule{unparsedGitRule(strings.Join(wordTexts(words), " "))}, nil
			}
			return nil, nil
		default:
			return nil, nil
		}
	}
	return nil, errGitParse
}

func mentionsGit(words []shWord) bool {
	for _, w := range words {
		if gitWordRe.MatchString(w.text) {
			return true
		}
	}
	return false
}

func hasExecPrimary(words []shWord) bool {
	for _, w := range words {
		switch w.text {
		case "-exec", "-execdir", "-ok", "-okdir":
			return true
		}
	}
	return false
}

// skipWrapperOptions drops a wrapper's leading options and VAR=val words.
func skipWrapperOptions(words []shWord, argFlags map[string]bool) []shWord {
	i := 0
	for i < len(words) {
		t := words[i].text
		switch {
		case t == "--":
			return words[i+1:]
		case argFlags[t]:
			i += 2
		case len(t) > 1 && t[0] == '-':
			i++
		case !words[i].dynamic && assignRe.MatchString(t):
			i++
		default:
			return words[min(i, len(words)):]
		}
	}
	return nil
}

// shellDashC returns the script of `sh -c <script>` style arguments,
// including combined flags such as -lc.
func shellDashC(args []shWord) (string, bool) {
	for i, a := range args {
		t := a.text
		if len(t) < 2 || t[0] != '-' || t == "--" {
			return "", false
		}
		if t[1] != '-' && strings.IndexByte(t, 'c') > 0 {
			if i+1 < len(args) {
				return args[i+1].text, true
			}
			return "", false
		}
	}
	return "", false
}

func newGitRule(id string, class GitRuleClass, label string, words []shWord) *GitRule {
	argv := wordTexts(words)
	return &GitRule{ID: id, Class: class, Label: label, Argv: argv, Command: strings.Join(argv, " ")}
}

func askRule(id, what string, words []shWord) []*GitRule {
	return []*GitRule{newGitRule(id, GitClassAlwaysAsk, what+" always needs your approval", words)}
}

// gitGlobalFlags are git options that may precede the subcommand and take no
// separate value; gitGlobalValueFlags consume the next word.
var (
	gitGlobalFlags = map[string]bool{
		"--no-pager": true, "-P": true, "-p": true, "--paginate": true,
		"--no-optional-locks": true, "--exec-path": true, "--bare": true,
		"--no-replace-objects": true, "--literal-pathspecs": true,
		"--glob-pathspecs": true, "--noglob-pathspecs": true,
		"--icase-pathspecs": true, "--no-advice": true,
	}
	gitGlobalValueFlags = map[string]bool{
		"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--super-prefix": true, "--config-env": true,
	}
)

// splitGitGlobals returns the index of the subcommand word in words (words[0]
// is git), skipping global options, plus the inline aliases set via -c.
func splitGitGlobals(words []shWord) (int, map[string]string) {
	var aliases map[string]string
	i := 1
	for i < len(words) {
		t := words[i].text
		switch {
		case t == "-c" && i+1 < len(words):
			if k, v, ok := strings.Cut(words[i+1].text, "="); ok && strings.HasPrefix(strings.ToLower(k), "alias.") {
				if aliases == nil {
					aliases = map[string]string{}
				}
				aliases[k[len("alias."):]] = v
			}
			i += 2
		case gitGlobalValueFlags[t]:
			i += 2
		case gitGlobalFlags[t]:
			i++
		case strings.HasPrefix(t, "-"):
			i++ // --opt=value, --version, ... : never the subcommand
		default:
			return i, aliases
		}
	}
	return len(words), aliases
}

func classifyGit(words []shWord, depth int) ([]*GitRule, error) {
	si, aliases := splitGitGlobals(words)
	if si >= len(words) {
		return nil, nil
	}
	if words[si].dynamic {
		return []*GitRule{unparsedGitRule(strings.Join(wordTexts(words), " "))}, nil
	}
	sub := words[si].text
	if target, ok := aliases[sub]; ok {
		if depth > maxGitParseDepth {
			return nil, errGitParse
		}
		var rules []*GitRule
		var err error
		if script, shell := strings.CutPrefix(target, "!"); shell {
			rules, err = classifyShell(script, depth+1)
		} else {
			cmds, _, serr := scanShell(target)
			err = serr
			switch {
			case err != nil || len(cmds) == 0:
			case cmds[0][0].text == sub:
				rules = classifyGitSub(words, sub, wordTexts(words[si+1:]))
			default:
				expanded := append([]shWord{}, words[:si]...)
				expanded = append(expanded, cmds[0]...)
				expanded = append(expanded, words[si+1:]...)
				rules, err = classifyGit(expanded, depth+1)
			}
		}
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			r.Argv = wordTexts(words)
			r.Command = strings.Join(r.Argv, " ")
		}
		return rules, nil
	}
	return classifyGitSub(words, sub, wordTexts(words[si+1:])), nil
}

func classifyGitSub(words []shWord, sub string, args []string) []*GitRule {
	switch sub {
	case "status", "diff", "log", "show", "fetch", "rev-parse", "ls-files":
		return []*GitRule{newGitRule("git-"+sub, GitClassReadOnly, "git "+sub, words)}
	case "push":
		r := newGitRule("git-push", GitClassAlwaysAsk, "git push always needs your approval", words)
		pos := gitPositionals(args, "-o", "--push-option", "--repo", "--receive-pack", "--exec")
		if len(pos) > 0 {
			r.Remote = pos[0]
		}
		if len(pos) > 1 {
			r.Branch = strings.TrimPrefix(pos[1], "+")
		}
		return []*GitRule{r}
	case "commit":
		if hasArg(args, "--amend") {
			return askRule("git-commit-amend", "git commit --amend", words)
		}
		return []*GitRule{newGitRule("git-commit", GitClassCommit, "git commit", words)}
	case "reset":
		if hasArg(args, "--hard") {
			return askRule("git-reset-hard", "git reset --hard", words)
		}
	case "branch":
		return classifyGitBranch(words, args)
	case "clean":
		if hasArg(args, "--force") || hasShortFlag(args, 'f') {
			return askRule("git-clean", "git clean -f", words)
		}
	case "checkout":
		if hasArg(args, "--") {
			return askRule("git-checkout-discard", "git checkout --", words)
		}
	case "restore":
		pos := gitPositionals(args, "-s", "--source")
		if i := indexArg(args, "--"); i >= 0 {
			pos = append(pos, args[i+1:]...)
		}
		if len(pos) == 0 || hasArg(pos, ".") || hasArg(pos, ":/") {
			return askRule("git-restore", "git restore", words)
		}
	case "stash":
		pos := gitPositionals(args)
		if len(pos) > 0 && (pos[0] == "drop" || pos[0] == "clear") {
			return askRule("git-stash-"+pos[0], "git stash "+pos[0], words)
		}
	case "remote":
		for _, a := range args {
			if a != "-v" && a != "--verbose" {
				return nil
			}
		}
		return []*GitRule{newGitRule("git-remote", GitClassReadOnly, "git remote", words)}
	}
	return nil
}

func classifyGitBranch(words []shWord, args []string) []*GitRule {
	del := hasArg(args, "--delete") || hasShortFlag(args, 'd') || hasShortFlag(args, 'D')
	force := hasArg(args, "--force") || hasShortFlag(args, 'f') || hasShortFlag(args, 'D')
	if del && force {
		return askRule("git-branch-delete", "git branch -D", words)
	}
	if len(args) == 0 || hasArg(args, "--list") || hasShortFlag(args, 'l') {
		return []*GitRule{newGitRule("git-branch", GitClassReadOnly, "git branch", words)}
	}
	return nil
}

func classifyGh(words []shWord) []*GitRule {
	var pos []string
	for i := 1; i < len(words) && len(pos) < 2; i++ {
		t := words[i].text
		switch {
		case t == "-R" || t == "--repo":
			i++
		case strings.HasPrefix(t, "-"):
		default:
			pos = append(pos, t)
		}
	}
	if len(pos) == 2 && pos[0] == "pr" && (pos[1] == "create" || pos[1] == "merge") {
		return askRule("gh-pr-"+pos[1], "gh pr "+pos[1], words)
	}
	return nil
}

// gitPositionals returns the non-option arguments before any "--", skipping
// the values of the given value-taking options.
func gitPositionals(args []string, valueFlags ...string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return pos
		case hasArg(valueFlags, a):
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			pos = append(pos, a)
		}
	}
	return pos
}

func hasArg(args []string, want string) bool { return indexArg(args, want) >= 0 }

func indexArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// hasShortFlag reports whether a single-dash flag bundle (-f, -fd, -fdx)
// before any "--" contains the letter.
func hasShortFlag(args []string, letter byte) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.IndexByte(a, letter) > 0 {
			return true
		}
	}
	return false
}

// claudeAskRule says how the Claude CLI's own permission rules reach one
// always-ask rule ID. Words is the command prefix ("git push"); Contains are
// extra substrings that must follow it in any order (a flag such as --hard).
// A rule with a "git" first word is also written with a wildcard in the
// middle so global options (-C dir, -c k=v, --no-pager) between git and the
// subcommand still match, and with a leading wildcard so the command inside
// `sh -c '...'`/`bash -lc "..."` matches: the CLI splits compounds and
// strips env/VAR= prefixes itself but does not unwrap shells, and
// `Bash(git push:*)` alone is not enough for `git -C . push`
// (docs/plans/active/agent-git-approvals.md D3, verified on claude 2.1.303).
type claudeAskRule struct {
	ID       string
	Words    string
	Contains []string
}

// claudeAskRules is keyed by the always-ask GitRule IDs classifyGitSub and
// classifyGh emit; TestClaudeAskRulesCoverEveryAlwaysAskClass keeps the two
// in step. Force variants (--force, -f, --force-with-lease, +refspec) are all
// `git push`, so the git-push entry covers them.
var claudeAskRules = []claudeAskRule{
	{ID: "git-push", Words: "git push"},
	{ID: "gh-pr-create", Words: "gh pr create"},
	{ID: "gh-pr-merge", Words: "gh pr merge"},
	{ID: "git-reset-hard", Words: "git reset", Contains: []string{"--hard"}},
	{ID: "git-branch-delete", Words: "git branch", Contains: []string{"-D"}},
	{ID: "git-clean", Words: "git clean"},
	{ID: "git-checkout-discard", Words: "git checkout", Contains: []string{" -- "}},
	{ID: "git-restore", Words: "git restore"},
	{ID: "git-stash-drop", Words: "git stash drop"},
	{ID: "git-stash-clear", Words: "git stash clear"},
	{ID: "git-commit-amend", Words: "git commit", Contains: []string{"--amend"}},
}

// ClaudeAlwaysAskRules returns the Claude CLI `permissions.ask` patterns for
// every always-ask git/gh operation, in table order.
func ClaudeAlwaysAskRules() []string {
	var out []string
	add := func(p string) { out = append(out, "Bash("+p+")") }
	for _, r := range claudeAskRules {
		tail := strings.Join(r.Contains, "*")
		if len(r.Contains) == 0 {
			add(r.Words + ":*")
		}
		cmd, sub, _ := strings.Cut(r.Words, " ")
		wild := func(prefix string) string {
			p := prefix + r.Words
			if tail != "" {
				p += "*" + tail
			}
			return p + "*"
		}
		add(wild("*"))
		// A wildcard after the binary name lets global options sit between
		// the binary and its subcommand (git -C d push, gh -R o/r pr create).
		p := "*" + cmd + " * " + sub
		if tail != "" {
			p += "*" + tail
		}
		add(p + "*")
	}
	return out
}
