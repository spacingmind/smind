package runs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	childEventTitleMax = 80
	childEventTextMax  = 1024
	// childEventJSONMax bounds a kept fenced json block (a Peer's structured
	// handoff), measured on its escaped bytes.
	childEventJSONMax     = 4096
	childEventTruncMarker = "…[truncated; use task_logs {runId} for the rest]"
)

// ChildEventFacts is everything BuildChildEventBlock renders about one
// finished child run (ADR-0041 B3). Pure data: no registry access.
type ChildEventFacts struct {
	TaskID                                        int64
	Title, RunID, Status, StopReason, Disposition string
	FailureKind, FailureWindow                    string
	FailureResetAt                                *time.Time
	FailureResetInferred                          bool
	CostUSD                                       *float64
	BaseSHA, HeadSHA                              string
	LastText                                      string
}

// BuildChildEventBlock renders the status line plus the untrusted
// <child-response> element for one child run. The result has no trailing
// newline; without a handoff block it stays under 2 KiB, with one under 6 KiB.
func BuildChildEventBlock(f ChildEventFacts) string {
	state := f.Status
	if f.Disposition != "" {
		state += " (needs-continue)"
	}
	if f.StopReason == "daemon_restart" {
		state += " (daemon restart)"
	}
	parts := []string{state}
	if f.FailureKind != "" {
		fail := f.FailureKind
		if f.FailureWindow != "" {
			fail += " (" + f.FailureWindow + " window)"
		}
		if f.FailureResetAt != nil {
			approx := ""
			if f.FailureResetInferred {
				approx = "≈"
			}
			fail += ", resets " + approx + f.FailureResetAt.Format(time.RFC3339)
		} else {
			fail += ", reset time unknown"
		}
		parts = append(parts, fail)
	}
	if f.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("cost $%.2f", *f.CostUSD))
	} else {
		parts = append(parts, "cost not reported")
	}
	switch {
	case f.BaseSHA != "" && f.BaseSHA == f.HeadSHA:
		parts = append(parts, "no new commits")
	case f.HeadSHA != "" && f.BaseSHA != f.HeadSHA:
		parts = append(parts, "head "+shortSHA(f.HeadSHA))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- task #%d \"%s\" run %s: %s", f.TaskID, childEventTitle(f.Title), f.RunID, strings.Join(parts, " · "))
	if text := strings.TrimSpace(f.LastText); text != "" {
		fmt.Fprintf(&b, "\n<child-response task=\"%d\" untrusted=\"true\">%s</child-response>", f.TaskID, escapeChildText(text))
	}
	return b.String()
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// childEventTitle flattens control chars to spaces, swaps double quotes for
// single quotes, and cuts at 80 runes.
func childEventTitle(title string) string {
	var b strings.Builder
	n := 0
	for _, r := range title {
		if n == childEventTitleMax {
			break
		}
		switch {
		case r == '"':
			r = '\''
		case unicode.IsControl(r):
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// escapeChildText renders a child's final text. Ordinary text is XML-escaped
// and cut at childEventTextMax bytes (never mid-entity or mid-rune) with the
// truncation marker. The first well-formed fenced json block (a Peer's
// handoff) is kept whole up to childEventJSONMax escaped bytes, on top of that
// budget: prose before it is cut to the budget, prose after it gets what the
// budget has left, so a block after a long preamble survives but the preamble
// does not. A block over the cap is cut like prose.
func escapeChildText(s string) string {
	pre, block, post, ok := splitJSONBlock(s)
	if !ok {
		out, _ := escapeCut(s, childEventTextMax)
		return out
	}
	esc := escapeXML(block)
	if len(esc) > childEventJSONMax {
		out, _ := escapeCut(s, childEventTextMax)
		return out
	}
	head, _ := escapeCut(pre, childEventTextMax)
	budget := childEventTextMax - len(escapeXML(pre))
	if budget < 0 {
		budget = 0
	}
	tail, _ := escapeCut(post, budget)
	return head + esc + tail
}

// splitJSONBlock finds the first fenced ```json block whose body is valid
// JSON and returns the text before it, the whole fence, and the text after.
func splitJSONBlock(s string) (pre, block, post string, ok bool) {
	const open, closeFence = "```json", "```"
	from := 0
	for {
		i := strings.Index(s[from:], open)
		if i < 0 {
			return "", "", "", false
		}
		i += from
		bodyStart := i + len(open)
		nl := strings.IndexByte(s[bodyStart:], '\n')
		if nl < 0 || strings.TrimSpace(s[bodyStart:bodyStart+nl]) != "" {
			from = bodyStart
			continue
		}
		bodyStart += nl + 1
		j := strings.Index(s[bodyStart:], closeFence)
		if j < 0 {
			return "", "", "", false
		}
		end := bodyStart + j
		if json.Valid([]byte(s[bodyStart:end])) {
			return s[:i], s[i : end+len(closeFence)], s[end+len(closeFence):], true
		}
		from = end + len(closeFence)
	}
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// escapeCut XML-escapes s rune by rune and stops before the escaped output
// would exceed max bytes, appending the truncation marker after a cut.
func escapeCut(s string, max int) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		piece := escapeXML(string(r))
		if b.Len()+len(piece) > max {
			b.WriteString(childEventTruncMarker)
			return b.String(), true
		}
		b.WriteString(piece)
	}
	return b.String(), false
}
