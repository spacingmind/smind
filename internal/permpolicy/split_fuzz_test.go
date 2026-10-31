package permpolicy

import (
	"strings"
	"testing"
)

// FuzzSplitCommand checks the splitter's safety properties on arbitrary
// input: it must never panic, an input with content but no segments must
// carry a reason (so the caller asks), and a heredoc body must never survive
// into the normalized text that lands on a card or in the audit.
func FuzzSplitCommand(f *testing.F) {
	for _, seed := range []string{
		"echo hello",
		`echo 'a b' "c d" e\'f`,
		"A=1 B=2 cmd arg",
		"a; b && c || d | e & f",
		"cmd > out 2>> err 2>&1 <<< word",
		"echo $(git push origin x)",
		"echo `git stash`",
		"(cd x && git stash)",
		"{ a; b; }",
		"bash -c 'git stash'",
		`sh -c "$x"`,
		"echo <(ls)",
		"git commit -F - <<'EOF'\n$(rm -rf /)\nEOF",
		"cat <<EOF | sh\nrm -rf /\nEOF",
		"echo \"unclosed",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		if len(cmd) > maxCommandBytes {
			return // over the cap the splitter refuses by design
		}
		p := SplitCommand(cmd) // must not panic
		if len(p.Segments) == 0 {
			if trimmed := strings.TrimSpace(cmd); trimmed != "" && len(p.Unparsed) == 0 {
				t.Fatalf("no segments and no reason for %q", cmd)
			}
		}
		// The elision property has teeth only for a command the splitter fully
		// accepted: anything Unparsed is `ask` regardless, and bash -n
		// rejects those inputs before reading a heredoc body at all.
		// A line with an odd quote count means a quote can span lines, which
		// the oracle's line scan cannot see through: `'\n<<EOF\n'\nEOF` and
		// `"\n<<0\nA"` contain no heredoc.
		// Likewise a backslash-newline joins lines before the heredoc starts.
		balanced := !strings.Contains(cmd, "\\\n")
		for _, line := range strings.Split(cmd, "\n") {
			for _, q := range []string{"'", `"`, "`"} {
				if strings.Count(line, q)%2 != 0 {
					balanced = false
				}
			}
		}
		if len(p.Unparsed) == 0 && balanced {
			for _, body := range heredocBodies(cmd) {
				// A whitespace-only body is indistinguishable from the
				// newlines of any compound command; only meaningful text counts.
				if strings.TrimSpace(body) == "" {
					continue
				}
				// Text that also appears elsewhere in the command can show up
				// in the normalized form without being the body (the words
				// around an elided body are joined), so only a byte that the
				// rest of the command lacks proves a leak.
				rest := strings.Replace(cmd, body, "", 1)
				for i := 0; i < len(body); i++ {
					c := body[i]
					if c == '\n' || strings.IndexByte(rest, c) >= 0 || strings.IndexByte(elisionMarkerBytes, c) >= 0 {
						continue
					}
					if strings.IndexByte(p.Normalized, c) >= 0 {
						t.Fatalf("normalized leaks heredoc body %q: %q", body, p.Normalized)
					}
				}
			}
		}
	})
}

// elisionMarkerBytes are the bytes of the `<<TAG [heredoc N bytes]` marker
// the splitter writes in place of a body.
const elisionMarkerBytes = "[] <heredocbyts0123456789"

// heredocBodies is a test-side oracle: it finds <<TAG heredocs with a plain
// line scan — skipping any line that contains a quote, where the real lexer
// would not see an operator — and returns each body between the delimiter
// lines, newlines included, so the leak check compares whole bodies.
func heredocBodies(cmd string) []string {
	var bodies []string
	lines := strings.Split(cmd, "\n")
	skipTo := 0 // lines inside an earlier heredoc body are text, not commands
	for i, line := range lines {
		if i < skipTo {
			continue
		}
		if strings.ContainsAny(line, "'\"`") {
			continue
		}
		fields := strings.Fields(line)
		for k, fld := range fields {
			fld = strings.TrimLeft(fld, "0123456789") // 0<<EOF names the fd
			if !strings.HasPrefix(fld, "<<") || fld == "<<<" || len(fld) <= 2 {
				continue
			}
			tag := strings.TrimPrefix(fld, "<<")
			dash := strings.HasPrefix(tag, "-")
			tag = strings.TrimPrefix(tag, "-")
			if tag == "" && k+1 < len(fields) {
				tag = strings.TrimPrefix(fields[k+1], "-")
			}
			if tag == "" || strings.ContainsAny(tag, "'\"`<>&|;$ \t") {
				continue
			}
			var body []string
			found := false
			for j := i + 1; j < len(lines); j++ {
				l := lines[j]
				if dash {
					l = strings.TrimLeft(l, "\t")
				}
				if l == tag {
					found = true
					skipTo = j + 1
					break
				}
				body = append(body, lines[j])
			}
			if found && len(body) > 0 {
				bodies = append(bodies, strings.Join(body, "\n")+"\n")
			}
		}
	}
	return bodies
}
