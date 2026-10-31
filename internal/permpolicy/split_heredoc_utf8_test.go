package permpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

// A heredoc whose tag is not valid UTF-8 (bash matches the terminator by
// bytes) is a command nothing downstream can agree on: the body must never
// reach Parsed.Normalized or a segment's Raw, which feed approval cards and
// the audit log, and the request must escalate. Regression for the fuzz
// finding A<<\xae\n<<0\n\xae\nB\n0, where the rendered command carried body text.
func TestSplitCommand_HeredocInvalidUTF8TagIsUnparsedAndRedacted(t *testing.T) {
	t.Parallel()
	const secret = "TOPSECRETBODY"
	for _, tc := range []struct{ name, cmd string }{
		{"fuzz finding", "A<<\xae\n<<0\n\xae\nB\n0"},
		{"plain tag", "cat <<\xff\n" + secret + "\n\xff\n"},
		{"quoted tag", "cat <<\"\xc3(\"\n" + secret + "\n\xc3(\n"},
		{"tab-stripping operator", "cat <<-\xfe\n\t" + secret + "\n\t\xfe\n"},
		{"after a valid heredoc", "cat <<OK\nfine\nOK\ncat <<\xff\n" + secret + "\n\xff\n"},
		{"second operator on the line", "cat <<OK <<\xff\nfine\nOK\n" + secret + "\n\xff\n"},
		{"inside bash -c", "bash -c 'cat <<\xff\n" + secret + "\n\xff\n'"},
		{"inside a substitution", "echo $(cat <<\xff\n" + secret + "\n\xff\n)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, win := range []bool{false, true} {
				p := splitCommand(tc.cmd, win)
				if !strings.Contains(strings.Join(p.Unparsed, "; "), "not valid UTF-8") {
					t.Errorf("win=%v: Unparsed = %v, want a reason naming the invalid heredoc tag", win, p.Unparsed)
				}
				if p.Normalized != redactedText {
					t.Errorf("win=%v: Normalized = %q, want the redaction notice", win, p.Normalized)
				}
				if strings.Contains(p.Normalized, secret) {
					t.Errorf("win=%v: Normalized leaks the body: %q", win, p.Normalized)
				}
				for i, s := range p.Segments {
					if strings.Contains(s.Raw, secret) {
						t.Errorf("win=%v: segment %d Raw leaks the body: %q", win, i, s.Raw)
					}
				}
			}
		})
	}
}

// The decision a card or audit row is built from carries no body text either,
// and the request asks.
func TestDecide_HeredocInvalidUTF8TagAsksWithoutLeaking(t *testing.T) {
	t.Parallel()
	const secret = "TOPSECRETBODY"
	req := winFloorEnv(t)
	req.Command = "cat <<\xff\n" + secret + "\n\xff\nB\n"
	d := Decide(req, nil)
	if d.Action != ActionAsk {
		t.Fatalf("Decide = %s (%v); want ask", d.Action, d.Reasons)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("the decision leaks the heredoc body: %s", raw)
	}
	if d.Normalized != redactedText {
		t.Errorf("Normalized = %q, want the redaction notice", d.Normalized)
	}
}

// Valid tags, including a multi-byte one, are unaffected: still parsed, still
// elided to a size marker.
func TestSplitCommand_HeredocValidUTF8TagUnchanged(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"cat <<EOF\nbody\nEOF\n",
		"cat <<'EOF'\nbody\nEOF\n",
		"cat <<é\nbody\né\n",
	} {
		p := SplitCommand(cmd)
		if len(p.Unparsed) != 0 {
			t.Errorf("%q: Unparsed = %v", cmd, p.Unparsed)
		}
		if strings.Contains(p.Normalized, "body") || !strings.Contains(p.Normalized, "[heredoc") {
			t.Errorf("%q: Normalized = %q, want the body elided to a size marker", cmd, p.Normalized)
		}
	}
}

// Text after the bad operator may really be heredoc body (where the body ends
// is not knowable), so its displayed form is redacted; the segments stay, with
// their argv, so the floor and the rules still judge what may run.
func TestSplitCommand_HeredocInvalidUTF8TagRedactsLaterSegmentsButKeepsThem(t *testing.T) {
	t.Parallel()
	p := SplitCommand("A<<\xae\n<<0\n\xae\nB\n0")
	seen := map[string]bool{}
	for _, s := range p.Segments {
		if len(s.Argv) == 0 {
			continue
		}
		seen[s.Argv[0]] = true
		switch s.Argv[0] {
		case "B", "0":
			if s.Raw != redactedText {
				t.Errorf("segment %v after the operator: Raw = %q, want the redaction notice", s.Argv, s.Raw)
			}
		case "A":
			if s.Raw == redactedText {
				t.Errorf("the command before the operator must keep its text, got the redaction notice")
			}
		}
	}
	for _, want := range []string{"A", "B", "0"} {
		if !seen[want] {
			t.Errorf("segment %q was dropped; the floor must still see it (segments: %+v)", want, p.Segments)
		}
	}
}
