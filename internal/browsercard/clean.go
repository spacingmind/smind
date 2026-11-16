package browsercard

import (
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// daemonCheckMarker is the daemon's own suffix; variable text must never be
// able to forge it, so cleanText strips its parentheses.
const daemonCheckMarker = "(daemon check)"

// cleanText strips control characters (and U+2028/U+2029), collapses every
// whitespace run into a single space, neutralises the daemon-check marker,
// trims, and caps the result at max runes, replacing the overflow with a
// trailing ellipsis.
func cleanText(s string, max int) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		// Control characters, line/paragraph separators and every format character
		// (Cf: bidirectional overrides and isolates, zero-width joiners) are dropped:
		// none belongs in a line a person reads to decide.
		if r == '\u2028' || r == '\u2029' || unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
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
	out := neutraliseDaemonCheck(b.String())
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

// neutraliseDaemonCheck rewrites every case-insensitive "(daemon check)" to
// "daemon check", so only the suffix the daemon itself appends can carry it.
func neutraliseDaemonCheck(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '(' && i+len(daemonCheckMarker) <= len(s) && strings.EqualFold(s[i:i+len(daemonCheckMarker)], daemonCheckMarker) {
			b.WriteString("daemon check")
			i += len(daemonCheckMarker)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// redactURL reduces a raw URL to scheme://host/path, dropping userinfo,
// query and fragment. Non-web inputs collapse to a placeholder. A non-ASCII
// host is shown next to the punycode form the policy matches on.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "(not a web address)"
	}
	path := neutraliseQuotes(cleanText(u.Path, 120))
	if path == "" {
		path = "/"
	}
	display, _ := hostDisplay(cleanText(u.Host, 120))
	return u.Scheme + "://" + display + path
}

// hostDisplay renders a host for the card. A pure-ASCII host is shown as is;
// a non-ASCII host gets the canonical punycode form next to it (only the host
// part is converted, the :port stays). It returns the display text and the
// ASCII form, empty when the host is ASCII or has no valid one.
func hostDisplay(host string) (display, ascii string) {
	hostname, port := splitHostPort(host)
	suffix := ""
	if port != "" {
		suffix = ":" + port
	}
	if isASCII(hostname) {
		return host, ""
	}
	punycode, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return hostname + " (invalid name)" + suffix, ""
	}
	return hostname + " (" + punycode + ")" + suffix, punycode + suffix
}

func splitHostPort(host string) (hostname, port string) {
	if h, p, err := net.SplitHostPort(host); err == nil {
		return h, p
	}
	return host, ""
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// quoteLike folds every double-quote-like rune into a straight apostrophe.
var quoteLike = strings.NewReplacer(
	`"`, "'",
	"\u201C", "'",
	"\u201D", "'",
	"\u201E", "'",
	"\u201F", "'",
	"\u00AB", "'",
	"\u00BB", "'",
	"\u2033", "'",
	"\uFF02", "'",
)

// neutraliseQuotes is the shared quote folding for quoted fields and URL
// paths: a decoded %22 must not open or close the card's own quotes either.
func neutraliseQuotes(s string) string {
	return quoteLike.Replace(s)
}

// quoted is cleanText for text that is shown inside double quotes: a double quote
// in it would end the quote early and let a title pose as the card's own words.
func quoted(s string, max int) string {
	return neutraliseQuotes(cleanText(s, max))
}
