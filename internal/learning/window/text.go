package window

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// preClip cuts s to at most n bytes on a rune boundary, without a marker.
func preClip(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// clipRunes cuts s to at most n bytes on a rune boundary, appending an
// ellipsis when it cut anything. The result is never longer than n.
func clipRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < len(ellipsis) {
		return ""
	}
	return preClip(s, n-len(ellipsis)) + ellipsis
}

// delimRun matches any run of three or more angle brackets; the transcript
// delimiters are built from exactly such runs.
var delimRun = regexp.MustCompile("<{3,}|>{3,}")

// neutralise breaks every run of three or more `<` or `>` by spacing the
// characters apart, so untrusted text cannot close or reopen the wrapper
// blocks however many brackets it stacks.
func neutralise(s string) string {
	if !strings.Contains(s, "<<<") && !strings.Contains(s, ">>>") {
		return s
	}
	return delimRun.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Join(strings.Split(m, ""), " ")
	})
}

func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// renderer applies the security-critical text pipeline: (1) pre-clip to 4x the
// cap so redaction stays cheap, (2) redact the whole pre-clipped string so a
// secret is never cut in half before redaction, (3) clip to the real cap on a
// rune boundary.
type renderer struct {
	redact func(string) string
	caps   Caps
}

func (r renderer) pipeline(s string, cap int, single bool) string {
	if cap <= 0 || s == "" {
		return ""
	}
	pre := preClip(s, cap*4)
	clipped := len(pre) < len(s)
	out := r.redact(pre)
	if clipped {
		// The cut may have split a token (possibly a secret) that no redactor
		// can recognise; drop the trailing partial token.
		if i := strings.LastIndexAny(out, " \t\r\n"); i >= 0 {
			out = out[:i]
		}
	}
	if single {
		out = collapseWS(out)
	}
	return clipRunes(neutralise(out), cap)
}

// text renders s multi-line, redacted and clipped to cap bytes.
func (r renderer) text(s string, cap int) string { return r.pipeline(s, cap, false) }

// line renders s on a single line, redacted and clipped to cap bytes.
func (r renderer) line(s string, cap int) string { return r.pipeline(s, cap, true) }
