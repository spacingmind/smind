package gate

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	contLineRe = regexp.MustCompile(`\\\n[ \t]*`)
	spaceRunRe = regexp.MustCompile(`[ \t]+`)
)

// canonical returns the text the way a shell or a language model reads it,
// for scanning only (never stored): compatibility-folded (fullwidth and
// math-alphanumeric letters become ASCII), diacritics stripped, invisible and
// control runes dropped, every space-like rune a plain space, every line
// ending "\n", backslash-continued lines joined and space runs collapsed.
// It decomposes with NFKD rather than NFKC so precomposed look-alikes such as
// "ćurl" fold to "curl" as well.
func canonical(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevCR := false
	for _, r := range norm.NFKD.String(s) {
		if r == '\n' && prevCR {
			prevCR = false
			continue
		}
		prevCR = r == '\r'
		switch {
		case r == '\r':
			b.WriteByte('\n')
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t' || unicode.Is(unicode.Zs, r):
			b.WriteByte(' ')
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Cc):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	out := contLineRe.ReplaceAllString(b.String(), " ")
	return spaceRunRe.ReplaceAllString(out, " ")
}

// scanTexts returns the texts a scanner must run on: each raw text plus its
// canonical form when that differs.
func scanTexts(texts []string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t)
		if c := canonical(t); c != t {
			out = append(out, c)
		}
	}
	return out
}

func dedupeFindings(fs []Finding) []Finding {
	seen := make(map[Finding]struct{}, len(fs))
	var out []Finding
	for _, f := range fs {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

// normalizeNewlines maps \r\n, lone \r, U+2028 and U+2029 to \n.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.NewReplacer("\r", "\n", " ", "\n", " ", "\n").Replace(s)
}
