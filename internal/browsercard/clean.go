package browsercard

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// cleanText strips control characters (and U+2028/U+2029), collapses every
// whitespace run into a single space, trims, and caps the result at max runes,
// replacing the overflow with a trailing ellipsis.
func cleanText(s string, max int) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		if r == '\u2028' || r == '\u2029' || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			continue
		}
		if unicode.IsSpace(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteRune(r)
	}
	out := b.String()
	if utf8.RuneCountInString(out) > max {
		runes := []rune(out)
		n := max - 1
		if n < 0 {
			n = 0
		}
		out = string(runes[:n]) + "…"
	}
	return out
}

// redactURL reduces a raw URL to scheme://host/path, dropping userinfo,
// query and fragment. Non-web inputs collapse to a placeholder.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "(not a web address)"
	}
	path := cleanText(u.Path, 120)
	if path == "" {
		path = "/"
	}
	return u.Scheme + "://" + u.Host + path
}
