// Package permpolicy implements the deterministic permission policy engine
// of ADR-0047 §2. The shell splitter is its security-critical entrance:
// anything it cannot fully parse is reported in Parsed.Unparsed so callers
// escalate to a human instead of matching rules against a guess.
package permpolicy

import "strings"

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
}

// Parsed is the split of one command string. A non-empty Unparsed means the
// caller must escalate; Segments are best effort in that case.
type Parsed struct {
	Segments     []Segment
	Normalized   string
	HeredocBytes int
	Unparsed     []string
}

// SplitCommand splits a shell command into simple commands. It never panics
// and never returns an error: doubt goes into Unparsed and the caller asks.
func SplitCommand(cmd string) Parsed {
	p := Parsed{Normalized: strings.TrimSpace(cmd)}
	if len(cmd) > maxCommandBytes {
		p.Unparsed = append(p.Unparsed, "input exceeds 64 KiB")
		return p
	}
	s := scanner{src: cmd, p: &p, segStart: -1}
	s.run()
	p.Segments = s.segs
	return p
}

// word accumulates one shell word. dynamic is set for an unquoted or
// double-quoted $ or backtick and for an unquoted * ? [ — single quotes are
// literal and never expand. quoted records that part of the word was quoted,
// which stops a trailing digit run from reading as a redirect fd.
type word struct {
	val     []byte
	started bool
	quoted  bool
	dynamic bool
}

type scanner struct {
	src      string
	i        int
	p        *Parsed
	w        word
	cur      *Segment
	segs     []Segment
	segStart int
}

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
		case c == '&':
			switch {
			case s.peek(1) == '&':
				s.closeSeg(false)
				s.i += 2
			case s.peek(1) == '>':
				s.redirect()
			default:
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
		case c == '>' || c == '<' || (isDigit(c) && s.peek(1) == '>' && !s.w.started):
			s.redirect()
		default:
			s.wordChar()
		}
	}
	s.closeSeg(false)
}

// wordChar consumes one character that begins or continues a word, so a
// redirect target can reuse the same rules. A '(' in command position is a
// subshell the splitter does not analyse (yet); quotes and escapes are
// stripped from the value.
func (s *scanner) wordChar() {
	c := s.src[s.i]
	if c == '\\' && s.peek(1) == '\n' { // line continuation joins lines
		s.i += 2
		return
	}
	if !s.w.started {
		s.w.started = true
		if s.segStart < 0 {
			s.segStart = s.i
		}
		if c == '(' && s.cur == nil {
			s.unparsed("subshell not supported yet")
		}
	}
	switch c {
	case '\'': // literal until the next single quote
		s.w.quoted = true
		s.i++
		for s.i < len(s.src) && s.src[s.i] != '\'' {
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
			s.i++
			return
		}
		if c == '\\' && s.i+1 < len(s.src) {
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
		if c == '$' || c == '`' {
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
			case strings.HasPrefix(val, "$"), strings.HasPrefix(val, "`"):
				s.unparsed("command name " + val + " is an expansion")
			}
		}
		cur.Argv = append(cur.Argv, val)
	}
	if w.dynamic {
		cur.Dynamic = true
	}
}

// closeSeg finishes the current segment; pipe marks that the next segment
// receives this one's output through | or |&.
func (s *scanner) closeSeg(pipe bool) {
	s.endWord()
	if s.cur != nil {
		if pipe {
			s.cur.PipeNext = true
		}
		if s.segStart >= 0 && s.segStart <= s.i {
			s.cur.Raw = strings.TrimSpace(s.src[s.segStart:s.i])
		}
		if len(s.cur.Argv) > 0 || len(s.cur.Env) > 0 || len(s.cur.Redirects) > 0 {
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

func (s *scanner) peek(k int) byte {
	if s.i+k < len(s.src) {
		return s.src[s.i+k]
	}
	return 0
}

// redirect parses one redirection operator starting at s.i (>, <, >>, <<,
// <<<, &>, fd-prefixed forms and the inline duplication of 2>&1) plus its
// target word, recording it on the current segment instead of Argv. A
// process substitution (<( or >() runs words in a subshell we do not analyse,
// and heredoc bodies are not elided yet, so both are reported as Unparsed
// until the later steps handle them.
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
	switch {
	case strings.HasSuffix(op, "<") && s.peek(0) == '(',
		strings.HasSuffix(op, ">") && s.peek(0) == '(':
		s.unparsed("process substitution cannot be analysed")
	case strings.Contains(op, "<<") && op != "<<<":
		s.unparsed("heredoc not supported yet")
	}
	tok := Redirect{Op: op}
	if !(strings.Contains(op, "&") && isDigit(op[len(op)-1])) {
		for s.peek(0) == ' ' || s.peek(0) == '\t' || s.peek(0) == '\r' {
			s.i++
		}
		s.scanWord()
		if !s.w.started {
			s.unparsed("redirect " + op + " without a target")
		} else {
			tok.Target = string(s.w.val)
			if s.w.dynamic {
				s.seg().Dynamic = true
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
