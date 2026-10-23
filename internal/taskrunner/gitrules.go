package taskrunner

import (
	"errors"
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

func classifySimple(words []shWord, depth int) ([]*GitRule, error) {
	if len(words) == 0 || words[0].dynamic {
		return nil, nil
	}
	if words[0].text == "git" && len(words) > 1 && words[1].text == "push" {
		args := wordTexts(words)
		return []*GitRule{{
			ID:      "git-push",
			Class:   GitClassAlwaysAsk,
			Label:   "git push always needs your approval",
			Argv:    args,
			Command: strings.Join(args, " "),
		}}, nil
	}
	return nil, nil
}
