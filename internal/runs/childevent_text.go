package runs

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	childEventTitleMax    = 80
	childEventTextMax     = 1024
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
// newline and stays under 2 KiB.
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

// escapeChildText XML-escapes s rune by rune and stops before the escaped
// output would exceed childEventTextMax bytes (never mid-entity or
// mid-rune), appending the truncation marker after a cut.
func escapeChildText(s string) string {
	var b strings.Builder
	for _, r := range s {
		var piece string
		switch r {
		case '&':
			piece = "&amp;"
		case '<':
			piece = "&lt;"
		case '>':
			piece = "&gt;"
		default:
			piece = string(r)
		}
		if b.Len()+len(piece) > childEventTextMax {
			b.WriteString(childEventTruncMarker)
			return b.String()
		}
		b.WriteString(piece)
	}
	return b.String()
}
