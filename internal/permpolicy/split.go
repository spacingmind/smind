// Package permpolicy implements the deterministic permission policy engine
// of ADR-0047 §2. The shell splitter is its security-critical entrance:
// anything it cannot fully parse is reported in Parsed.Unparsed so callers
// escalate to a human instead of matching rules against a guess.
package permpolicy

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxCommandBytes caps what the splitter will scan at all; a larger request
// is reported unparseable rather than trusted after truncation.
const maxCommandBytes = 64 << 10

// Redirect is one redirection operator and its target word. Forms whose
// target is inline (2>&1) carry it inside Op and leave Target empty.
type Redirect struct{ Op, Target string }

// Segment is one simple command of a compound line. Dynamic marks a word the
// engine cannot match literally (variable, glob, substitution); PipeNext is
// set only when this segment's output feeds the next one through | or |&.
type Segment struct {
	Argv      []string
	Env       []string
	Redirects []Redirect
	Dynamic   bool
	PipeNext  bool
	Raw       string
	// Inspected marks a PowerShell or cmd wrapper (powershell -Command, pwsh -c,
	// cmd /c) whose literal payload the splitter parsed completely; its inner
	// commands are separate segments. A wrapper without it still takes the
	// "runs text the engine cannot see" ask on Windows.
	Inspected bool
}

// Parsed is the split of one command string. A non-empty Unparsed means the
// caller must escalate; Segments are best effort in that case.
type Parsed struct {
	Segments     []Segment
	Normalized   string
	HeredocBytes int
	Unparsed     []string
}

// maxNestDepth caps substitution, subshell and shell -c recursion; past it
// the text is still consumed but reported instead of analysed.
const maxNestDepth = 8

// SplitCommand splits a shell command into simple commands. It never panics
// and never returns an error: doubt goes into Unparsed and the caller asks.
func SplitCommand(cmd string) Parsed { return splitCommand(cmd, false) }

// splitCommand is SplitCommand with an optional Windows mode, where an unquoted
// backslash before an ordinary path character stays literal.
func splitCommand(cmd string, win bool) Parsed {
	p := Parsed{Normalized: strings.TrimSpace(cmd)}
	if len(cmd) > maxCommandBytes {
		p.Unparsed = append(p.Unparsed, "input exceeds 64 KiB")
		return p
	}
	s := scanner{src: cmd, p: &p, segStart: -1, win: win}
	s.run()
	s.finish()
	if len(s.segs)+len(s.inner) == 0 && len(p.Unparsed) == 0 && strings.TrimSpace(cmd) != "" {
		// Nothing parseable came out (operator-only input such as ";;" or
		// "&"): bash would reject it, and the caller must ask, not allow.
		p.Unparsed = append(p.Unparsed, "no command found")
	}
	p.Segments = append(s.segs, s.inner...)
	p.Normalized = s.normalized()
	return p
}

// finish consumes any heredoc bodies left on the last line and re-renders
// every segment's Raw now that all body sizes are known.
func (s *scanner) finish() {
	s.consumeHeredocBodies()
	for i := range s.segs {
		s.segs[i].Raw = s.elided(s.spans[i].start, s.spans[i].end)
		if s.redacted && s.spans[i].start >= s.redactAt {
			// Where the heredoc body ends is not knowable here, so a segment
			// after the operator may really be body text: never show it.
			s.segs[i].Raw = redactedText
		}
	}
}

// redactedText replaces text the splitter cannot vouch for as body-free.
const redactedText = "[redacted: the command has a heredoc whose tag is not valid UTF-8]"

// word accumulates one shell word. dynamic is set for an unquoted or
// double-quoted $ or backtick and for an unquoted * ? [ — single quotes are
// literal and never expand. quoted records that part of the word was quoted,
// which stops a trailing digit run from reading as a redirect fd.
type word struct {
	val     []byte
	started bool
	quoted  bool
	dynamic bool
	brace   bool // unquoted { seen
}

// heredoc records one <<TAG operator and, once its body has been consumed,
// the span that body occupied, so Raw and Normalized can replace it with a
// size marker instead of the text (bodies are never matched as commands).
type heredoc struct {
	opStart, opEnd     int // the operator and tag as written
	bodyStart, bodyEnd int
	tag                string
	stripTabs          bool // <<- strips leading tabs from body lines and the terminator
	n                  int  // body length in bytes
}

func (h *heredoc) marker() string {
	return "<<" + h.tag + " [heredoc " + strconv.Itoa(h.n) + " bytes]"
}

// dialect is the shell syntax a nested script is written in. The zero value
// is POSIX sh; PowerShell and cmd differ in exactly the places that could hide
// a command from a bash-flavoured scan (see the scanner cases that test it).
type dialect uint8

const (
	dialectNone dialect = iota
	dialectPS
	dialectCmd
)

func (d dialect) String() string {
	switch d {
	case dialectPS:
		return "PowerShell"
	case dialectCmd:
		return "cmd"
	}
	return "sh"
}

type scanner struct {
	src      string
	i        int
	p        *Parsed
	w        word
	cur      *Segment
	segs     []Segment
	spans    []span // source range of each segment, for the final Raw pass
	segStart int
	pending  []*heredoc // heredoc operators on the line being scanned
	heredocs []*heredoc // every heredoc, in operator order, once consumed
	win      bool       // Windows mode: see winLiteralBackslashNext
	dialect  dialect    // syntax of this script: sh, PowerShell or cmd
	depth    int        // nesting level of substitutions and shells
	redacted bool       // a heredoc tag was not valid UTF-8: see redactedText
	redactAt int        // source offset of that heredoc operator
	inner    []Segment  // segments of substituted/nested text, appended after segs
}

// span is the source range a segment was built from; Raw is re-rendered from
// it after every heredoc body is known, because a segment can close (at a
// pipe, a ';' or a newline) before its own heredoc body is consumed.
type span struct{ start, end int }

func (s *scanner) run() {
	for s.i < len(s.src) {
		c := s.src[s.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			s.endWord()
			s.i++
		case c == '\n' || c == ';':
			s.closeSeg(false)
			s.i++
			if c == '\n' {
				s.consumeHeredocBodies()
			}
		case c == '&':
			switch {
			case s.peek(1) == '&':
				s.closeSeg(false)
				s.i += 2
			case s.peek(1) == '>':
				s.redirect()
			default:
				if s.dialect == dialectPS && (s.w.started || (s.cur != nil && len(s.cur.Argv) > 0)) {
					// A leading & is the call operator; after a command it
					// starts a background job (PowerShell 7), which is not modelled.
					s.unparsed("PowerShell background operator")
				}
				s.closeSeg(false)
				s.i++
			}
		case c == '|' && s.peek(1) == '|':
			s.closeSeg(false)
			s.i += 2
		case c == '|': // | and |& both pipe stdout to the next segment
			s.closeSeg(true)
			if s.peek(1) == '&' {
				s.i += 2
			} else {
				s.i++
			}
		case c == '$' && s.peek(1) == '(':
			s.substitution('$')
		case c == '`' && s.dialect == dialectPS:
			// A PowerShell escape (`n, `", `$), not a substitution: it can spell a
			// character the rules never see, so the script is not analysable.
			s.unparsed("PowerShell backtick escape")
			s.wordChar()
		case c == '`':
			s.substitution('`')
		case s.dialect == dialectPS && !s.w.started && (c == '#' || c == '@'):
			// A comment (<# #> and # hide text from PowerShell but not from this
			// scan) or a splat/hashtable (@args, @{}): not modelled.
			s.unparsed("PowerShell comment or splat")
			s.wordChar()
		case s.dialect == dialectPS && c == '<':
			s.unparsed("PowerShell has no < redirection (or this is a <# comment)")
			s.wordChar()
		case s.dialect == dialectPS && (c == '{' || c == '}'):
			// A script block (ForEach-Object { ... }) holds commands that are
			// arguments of another command here.
			s.unparsed("PowerShell script block")
			s.wordChar()
		case c == '(' && !s.w.started && s.cur == nil:
			s.subshellGroup()
		case (c == '(' || c == ')') && s.dialect != dialectNone:
			// (Remove-Item x) and if exist x (del y) run a command inside an
			// argument list; only a leading group is parsed.
			s.unparsed(s.dialect.String() + " parenthesised expression")
			s.wordChar()
		case c == '{' && !s.w.started && s.cur == nil && isSpaceOrEOL(s.peek(1)):
			s.braceGroup()
		case c == '>' || c == '<' || (isDigit(c) && s.peek(1) == '>' && !s.w.started):
			s.redirect()
		default:
			s.wordChar()
		}
	}
	s.closeSeg(false)
}

// wordChar consumes one character that begins or continues a word, so a
// redirect target can reuse the same rules; quotes and escapes are stripped
// from the value.
func (s *scanner) wordChar() {
	c := s.src[s.i]
	if c == '\\' && s.peek(1) == '\n' && s.dialect == dialectNone { // line continuation joins lines
		s.i += 2
		return
	}
	if !s.w.started {
		s.w.started = true
		if s.segStart < 0 {
			s.segStart = s.i
		}
	}
	switch c {
	case '\'': // literal until the next single quote
		if s.dialect == dialectCmd { // cmd has no single quotes: ' & del x' still splits at &
			s.w.val = append(s.w.val, c)
			s.i++
			return
		}
		s.w.quoted = true
		s.i++
		for s.i < len(s.src) {
			if s.src[s.i] == '\'' {
				if s.dialect == dialectPS && s.peek(1) == '\'' { // 'it''s' is it's
					s.w.val = append(s.w.val, '\'')
					s.i += 2
					continue
				}
				break
			}
			s.w.val = append(s.w.val, s.src[s.i])
			s.i++
		}
		if s.i >= len(s.src) {
			s.unparsed("unbalanced single quote")
			return
		}
		s.i++
	case '"':
		s.w.quoted = true
		s.i++
		s.scanDoubleQuoted()
	case '\\':
		if s.dialect != dialectNone || (s.win && winLiteralBackslashNext(s.peek(1))) {
			s.w.val = append(s.w.val, '\\')
			s.i++
			return
		}
		s.i++
		if s.i < len(s.src) {
			s.w.val = append(s.w.val, s.src[s.i])
			s.i++
		} else {
			s.w.val = append(s.w.val, '\\')
		}
	default:
		if c == '$' || c == '`' || c == '*' || c == '?' || c == '[' {
			s.w.dynamic = true
		}
		if s.dialect == dialectCmd {
			switch c {
			case '%', '!': // %VAR% and delayed !VAR! expand before cmd parses the line
				s.w.dynamic = true
			case '^':
				s.unparsed("cmd caret escape")
			}
		}
		if c == '{' {
			s.w.brace = true
		}
		s.w.val = append(s.w.val, c)
		s.i++
	}
}

// scanDoubleQuoted consumes a double-quoted section. \" \\ \$ and \` collapse
// to the literal character and backslash-newline is a continuation; any other
// backslash stays. $ and ` still expand inside double quotes, so they mark
// the word dynamic; glob characters do not.
func (s *scanner) scanDoubleQuoted() {
	for s.i < len(s.src) {
		c := s.src[s.i]
		if c == '"' {
			if s.dialect == dialectPS && s.peek(1) == '"' { // "a""b" is a"b
				s.w.val = append(s.w.val, '"')
				s.i += 2
				continue
			}
			s.i++
			return
		}
		if c == '\\' && s.i+1 < len(s.src) && s.dialect == dialectNone {
			switch s.src[s.i+1] {
			case '"', '\\', '$', '`':
				s.w.val = append(s.w.val, s.src[s.i+1])
				s.i += 2
				continue
			case '\n':
				s.i += 2
				continue
			}
		}
		if c == '$' && s.i+1 < len(s.src) && s.src[s.i+1] == '(' {
			s.substitution('$')
			continue
		}
		if c == '`' && s.dialect == dialectPS {
			s.unparsed("PowerShell backtick escape")
			s.w.dynamic = true
			s.w.val = append(s.w.val, c)
			s.i++
			continue
		}
		if c == '`' {
			s.substitution('`')
			continue
		}
		if c == '$' || (s.dialect == dialectCmd && (c == '%' || c == '!')) {
			s.w.dynamic = true
		}
		s.w.val = append(s.w.val, c)
		s.i++
	}
	s.unparsed("unbalanced double quote")
}

// endWord closes the word being built and classifies it: a leading NAME=value
// becomes an Env entry, the first remaining word is argv0 (checked for
// commands the engine could never resolve), the rest are arguments.
func (s *scanner) endWord() {
	w := s.w
	s.w = word{}
	if !w.started {
		return
	}
	cur := s.seg()
	val := string(w.val)
	if len(cur.Argv) == 0 && !w.quoted && isEnvAssign(val) {
		cur.Env = append(cur.Env, val)
	} else {
		if len(cur.Argv) == 0 {
			switch {
			case val == "eval":
				s.unparsed("eval re-parses its argument as a command")
			case val == "source" || val == ".":
				s.unparsed(val + " runs text the splitter cannot see")
			case strings.HasPrefix(val, "$"), strings.HasPrefix(val, "`"):
				s.unparsed("command name " + val + " is an expansion")
			case s.dialect == dialectPS && psRunsText[strings.ToLower(val)]:
				s.unparsed("PowerShell " + val + " runs text the splitter cannot see")
			case s.dialect == dialectCmd && (strings.HasPrefix(val, "%") || strings.HasPrefix(val, "!")):
				s.unparsed("cmd command name " + val + " is an expansion")
			case s.dialect == dialectCmd && cmdRunsText[strings.ToLower(val)]:
				s.unparsed("cmd " + val + " runs a nested command the splitter cannot see")
			}
		}
		cur.Argv = append(cur.Argv, val)
		if len(cur.Argv) > 1 && effectiveIndex(cur.Argv) == len(cur.Argv)-1 && isShellEval(val) {
			s.unparsed(val + " runs text the splitter cannot see")
		}
	}
	// An unquoted { can open a brace expansion (--forc{e,}) that builds a word
	// the rules never see; only the group keywords { and } and the find
	// placeholder {} are inert.
	if w.dynamic || (w.brace && val != "{" && val != "}" && val != "{}") {
		cur.Dynamic = true
	}
}

// effectiveIndex is the index of the word that actually names the command:
// leading wrappers (command, builtin, nohup, time, exec, env), their flags and
// env's NAME=val words are skipped. It returns len(argv) when only wrappers
// are present.
func effectiveIndex(argv []string) int {
	i := 0
	for i < len(argv) {
		w := argv[i]
		switch w {
		case "command", "builtin", "nohup", "time", "exec", "env":
			i++
			for i < len(argv) && (strings.HasPrefix(argv[i], "-") || (w == "env" && isEnvAssign(argv[i]))) {
				i++
			}
			continue
		}
		break
	}
	return i
}

func isShellEval(w string) bool { return w == "eval" || w == "source" || w == "." }

// closeSeg finishes the current segment; pipe marks that the next segment
// receives this one's output through | or |&.
func (s *scanner) closeSeg(pipe bool) {
	s.endWord()
	if s.cur != nil {
		s.shellScript()
		if pipe {
			s.cur.PipeNext = true
		}
		if len(s.cur.Argv) > 0 || len(s.cur.Env) > 0 || len(s.cur.Redirects) > 0 {
			s.cur.Raw = s.elided(s.segStart, s.i)
			s.spans = append(s.spans, span{s.segStart, s.i})
			s.segs = append(s.segs, *s.cur)
		}
		s.cur = nil
	}
	s.segStart = -1
}

func (s *scanner) seg() *Segment {
	if s.cur == nil {
		s.cur = &Segment{}
	}
	return s.cur
}

// unparsed records why the command could not be fully analysed; the cap keeps
// adversarial input from turning it into an amplification.
func (s *scanner) unparsed(reason string) {
	if len(s.p.Unparsed) < 8 {
		s.p.Unparsed = append(s.p.Unparsed, reason)
	}
}

// winLiteralBackslashNext reports whether an unquoted backslash before c stays a
// literal backslash in Windows mode: only before an ordinary path character. Before
// any shell metacharacter the POSIX escape applies, so a command can never hide
// inside a quote or after an operator.
func winLiteralBackslashNext(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '.', '_', '-', ':', '%', '@', '+', ',', '=':
		return true
	}
	return false
}

func (s *scanner) peek(k int) byte {
	if s.i+k < len(s.src) {
		return s.src[s.i+k]
	}
	return 0
}

// redirect parses one redirection operator starting at s.i (>, <, >>, <<,
// <<<, &>, fd-prefixed forms and the inline duplication of 2>&1) plus its
// target word, recording it on the current segment instead of Argv. A
// process substitution (<( or >() runs a subshell we do not analyse, so it
// is reported as Unparsed.
func (s *scanner) redirect() {
	start := s.i
	op := ""
	if s.w.started && !s.w.quoted && allDigits(s.w.val) {
		op = string(s.w.val) // an unquoted digit run before > is the fd
		s.w = word{}
	} else {
		s.endWord()
		for isDigit(s.peek(0)) {
			op += string(s.src[s.i])
			s.i++
		}
	}
	switch {
	case s.peek(0) == '&': // &> and &>>
		op += "&"
		s.i++
		if s.peek(0) == '>' {
			op += ">"
			s.i++
			if s.peek(0) == '>' {
				op += ">"
				s.i++
			}
		}
	case s.peek(0) == '<':
		op += "<"
		s.i++
		for k := 0; k < 2 && s.peek(0) == '<'; k++ {
			op += "<"
			s.i++
		}
		if strings.HasSuffix(op, "<<") && s.peek(0) == '-' {
			op += "-"
			s.i++
		}
	case s.peek(0) == '>':
		op += ">"
		s.i++
		if s.peek(0) == '>' {
			op += ">"
			s.i++
		}
	}
	if strings.HasSuffix(op, ">") && s.peek(0) == '&' { // >&2, 2>&1
		op += "&"
		s.i++
		if isDigit(s.peek(0)) {
			op += string(s.src[s.i])
			s.i++
		}
	}
	if strings.HasSuffix(op, "<") && s.peek(0) == '(' || strings.HasSuffix(op, ">") && s.peek(0) == '(' {
		s.unparsed("process substitution")
	}
	// A heredoc's body starts after the line ends; its target is the
	// delimiter word (quotes stripped, never expanded by the shell).
	isHere := strings.Contains(op, "<<") && op != "<<<"
	tok := Redirect{Op: op}
	if isHere {
		tok.Op = "<<"
	}
	if !(strings.Contains(op, "&") && isDigit(op[len(op)-1])) {
		for s.peek(0) == ' ' || s.peek(0) == '\t' || s.peek(0) == '\r' {
			s.i++
		}
		s.scanWord()
		if !s.w.started {
			s.unparsed("redirect " + op + " without a target")
		} else {
			tok.Target = string(s.w.val)
			if s.w.dynamic && !isHere {
				s.seg().Dynamic = true
			}
			if isHere && !utf8.ValidString(tok.Target) {
				// bash matches the terminator by bytes, but nothing downstream (card,
				// audit, the body-elision oracle) can agree on where such a body ends,
				// so the body could leak into the rendered command. Escalate and redact.
				s.unparsed("heredoc tag is not valid UTF-8")
				if !s.redacted {
					s.redacted, s.redactAt = true, start
				}
			}
			if isHere {
				s.pending = append(s.pending, &heredoc{
					opStart:   start,
					opEnd:     s.i,
					tag:       tok.Target,
					stripTabs: strings.Contains(op, "<<-"),
				})
			}
			s.w = word{}
		}
	}
	if s.segStart < 0 {
		s.segStart = start
	}
	s.seg().Redirects = append(s.seg().Redirects, tok)
}

// scanWord consumes exactly one word (a redirect target) with the same quote
// and escape rules as the main loop.
func (s *scanner) scanWord() {
	for s.i < len(s.src) {
		c := s.src[s.i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ';' || c == '&' || c == '|' || c == '<' || c == '>' {
			return
		}
		s.wordChar()
	}
}

// substitution consumes a $( ... ) command substitution (kind '$') or a
// backtick substitution at s.i: the enclosing word becomes dynamic (its
// value is whatever the inner commands print), the literal source text stays
// in the word for display, and the inner commands are parsed so a floor
// check still sees them.
func (s *scanner) substitution(kind byte) {
	if !s.w.started {
		s.w.started = true
		if s.segStart < 0 {
			s.segStart = s.i
		}
	}
	s.w.dynamic = true
	start := s.i
	var inner string
	var after int
	var ok bool
	if kind == '$' {
		inner, after, ok = s.captureGroup(2)
	} else {
		inner, after, ok = s.captureBacktick()
	}
	s.w.val = append(s.w.val, s.src[start:after]...)
	s.i = after
	if !ok {
		if kind == '$' {
			s.unparsed("unclosed $(")
		} else {
			s.unparsed("unclosed backtick")
		}
		return
	}
	s.recurse(inner)
}

// subshellGroup consumes a ( ... ) subshell at s.i: its commands run in a
// child shell, so only the inner segments matter — the parens contribute no
// segment of their own.
func (s *scanner) subshellGroup() {
	inner, after, ok := s.captureGroup(1)
	s.i = after
	if !ok {
		s.unparsed("unclosed (")
	}
	s.recurse(strings.TrimSpace(inner))
}

// braceGroup consumes a { ...; } group at s.i like a subshell: braces and
// parens nest, quotes are respected, and the group's commands are appended
// after the outer list.
func (s *scanner) braceGroup() {
	i := s.i + 1
	start := i
	depth := 0
	for i < len(s.src) {
		switch s.src[i] {
		case '\\':
			i += 2
		case '\'':
			j := strings.IndexByte(s.src[i+1:], '\'')
			if j < 0 {
				s.unparsed("unclosed {")
				s.recurse(strings.TrimSpace(s.src[start:]))
				s.i = len(s.src)
				return
			}
			i += j + 2
		case '"':
			i++
			for i < len(s.src) && s.src[i] != '"' {
				if s.src[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case '{', '(':
			depth++
			i++
		case '}', ')':
			depth--
			if depth < 0 {
				s.recurse(s.src[start:i])
				s.i = i + 1
				return
			}
			i++
		default:
			i++
		}
	}
	s.unparsed("unclosed {")
	s.recurse(strings.TrimSpace(s.src[start:]))
	s.i = len(s.src)
}

// captureGroup returns the text of the balanced parenthesised group starting
// at s.i (openLen 1 for '(', 2 for '$('), the offset just past the closing
// ')', and whether the group closed. Quotes and escapes inside are honoured
// so a quoted paren cannot close the group early.
func (s *scanner) captureGroup(openLen int) (inner string, after int, ok bool) {
	i := s.i + openLen
	start := i
	depth := 1
	for i < len(s.src) {
		switch s.src[i] {
		case '\\':
			i += 2
		case '\'':
			j := strings.IndexByte(s.src[i+1:], '\'')
			if j < 0 {
				return s.src[start:], len(s.src), false
			}
			i += j + 2
		case '"':
			i++
			for i < len(s.src) && s.src[i] != '"' {
				if s.src[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case '(':
			depth++
			i++
		case ')':
			depth--
			if depth == 0 {
				return s.src[start:i], i + 1, true
			}
			i++
		default:
			i++
		}
	}
	return s.src[start:], len(s.src), false
}

// captureBacktick returns the text of a backtick substitution starting at
// s.i, the offset just past the closing backtick, and whether it closed.
func (s *scanner) captureBacktick() (inner string, after int, ok bool) {
	i := s.i + 1
	start := i
	for i < len(s.src) {
		switch s.src[i] {
		case '\\':
			i += 2
		case '`':
			return s.src[start:i], i + 1, true
		default:
			i++
		}
	}
	return s.src[start:], len(s.src), false
}

// recurse parses the text of a substitution, subshell, group or shell -c
// script and defers its segments to appear after the outer command list, so
// a rule or floor check sees commands the outer words merely mention. The
// inner scanner shares Parsed, so its heredoc byte count and its own doubt
// propagate to the parent.
func (s *scanner) recurse(inner string) {
	if s.depth >= maxNestDepth {
		s.unparsed("nesting too deep")
		return
	}
	s.recurseAs(inner, s.dialect)
}

// recurseAs is recurse for a script written in dialect d. It returns how many
// segments the script contributed (0 also when nesting was too deep, which is
// reported as Unparsed).
func (s *scanner) recurseAs(inner string, d dialect) int {
	if s.depth >= maxNestDepth {
		s.unparsed("nesting too deep")
		return 0
	}
	in := scanner{src: inner, p: s.p, segStart: -1, depth: s.depth + 1, win: s.win, dialect: d}
	in.run()
	in.finish()
	if in.redacted && !s.redacted {
		// The inner text sits inside this command's own text: redact all of it.
		s.redacted, s.redactAt = true, 0
	}
	s.inner = append(s.inner, in.segs...)
	s.inner = append(s.inner, in.inner...)
	return len(in.segs) + len(in.inner)
}

// shellScript recurses into the literal script word of bash|sh|zsh|dash|ksh
// -c so the floor sees the inner commands; a missing or non-literal script
// word cannot be known in advance, so it is reported instead.
func (s *scanner) shellScript() {
	argv := s.cur.Argv
	if len(argv) > 0 {
		if d := winShellDialect(argv[0]); d != dialectNone {
			s.winShellScript(argv, d)
			return
		}
	}
	if len(argv) == 0 || !isShell(argv[0]) {
		return
	}
	for i := 1; i < len(argv); i++ {
		if !isCFlag(argv[i]) {
			continue
		}
		j := i + 1
		for j < len(argv) && strings.HasPrefix(argv[j], "-") {
			j++
		}
		if j >= len(argv) || strings.ContainsAny(argv[j], "$`") {
			s.unparsed("shell -c with non-literal command")
			return
		}
		s.recurse(argv[j])
		return
	}
}

// isCFlag reports whether a shell option word carries -c: a lone -c or a
// cluster such as -lc, -xc or -ic, where the script is the next non-flag word.
func isCFlag(w string) bool {
	return len(w) > 1 && w[0] == '-' && w[1] != '-' && strings.ContainsRune(w, 'c')
}

func isShell(name string) bool {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	switch name {
	case "bash", "sh", "zsh", "dash", "ksh":
		return true
	}
	return false
}

func isSpaceOrEOL(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ';' || c == 0
}

// consumeHeredocBodies swallows the bodies of every heredoc operator on the
// line that just ended, so they are never scanned as commands, and records
// their spans for the elided Raw and Normalized forms. Bash reads the bodies
// in operator order, one after the other.
func (s *scanner) consumeHeredocBodies() {
	for _, h := range s.pending {
		h.bodyStart = s.i
		var ok bool
		h.n, ok = s.heredocBody(h.tag, h.stripTabs)
		h.bodyEnd = s.i
		s.p.HeredocBytes += h.n
		if !ok {
			s.unparsed("heredoc terminator not found")
		}
		s.heredocs = append(s.heredocs, h)
	}
	s.pending = nil
}

// heredocBody advances s.i past the body of one heredoc, up to and including
// the line equal to tag (tabs stripped for <<-), and returns the body length.
// ok is false when the terminator never appears; the remainder is still
// treated as body, exactly as bash would read it.
func (s *scanner) heredocBody(tag string, stripTabs bool) (n int, ok bool) {
	bodyStart := s.i
	for {
		lineStart := s.i
		lineEnd := strings.IndexByte(s.src[s.i:], '\n')
		var line string
		var next int
		if lineEnd < 0 {
			line = s.src[s.i:]
			next = len(s.src)
		} else {
			line = s.src[s.i : s.i+lineEnd]
			next = s.i + lineEnd + 1
		}
		s.i = next
		if stripTabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == tag {
			return lineStart - bodyStart, true
		}
		if next >= len(s.src) {
			return len(s.src) - bodyStart, false
		}
	}
}

// elided renders src[base:end) with every heredoc operator replaced by its
// size marker, so a segment's Raw never carries body text.
func (s *scanner) elided(base, end int) string {
	var b strings.Builder
	pos := base
	for _, h := range s.heredocs {
		if h.opStart < pos || h.opStart >= end {
			continue
		}
		b.WriteString(s.src[pos:h.opStart])
		b.WriteString(h.marker())
		pos = h.opEnd
	}
	b.WriteString(s.src[pos:end])
	return strings.TrimSpace(b.String())
}

// normalized renders the whole command with every heredoc body (and its
// terminator line) replaced by the size marker inline where the operator
// was, so no body text survives into an audit row or a permission card.
// The operator spans and body spans of several heredocs on one line
// interleave, so the edits are spliced in start order.
func (s *scanner) normalized() string {
	if s.redacted {
		return redactedText
	}
	if len(s.heredocs) == 0 {
		return strings.TrimSpace(s.src)
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, h := range s.heredocs {
		edits = append(edits, edit{h.opStart, h.opEnd, h.marker()})
		if h.bodyStart > h.opEnd && h.bodyStart <= len(s.src) {
			// The deletion spans the newline that opened the body through the
			// terminator's own newline, so one newline is put back: without it
			// the next line's text would glue onto the operator's target.
			edits = append(edits, edit{h.bodyStart - 1, h.bodyEnd, "\n"})
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			// Two bodies on one line make the deletions overlap; a deletion
			// clamps into the union, an insertion never overlaps in practice.
			if e.end <= pos || e.text != "\n" {
				continue
			}
			e.start, e.text = pos, ""
		}
		if e.end > len(s.src) {
			continue
		}
		b.WriteString(s.src[pos:e.start])
		b.WriteString(e.text)
		pos = e.end
	}
	b.WriteString(s.src[pos:])
	return strings.TrimSpace(b.String())
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func allDigits(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if !isDigit(c) {
			return false
		}
	}
	return true
}

// isEnvAssign reports whether word is a leading NAME=value assignment.
func isEnvAssign(word string) bool {
	i := strings.IndexByte(word, '=')
	if i <= 0 {
		return false
	}
	for j := 0; j < i; j++ {
		c := word[j]
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
