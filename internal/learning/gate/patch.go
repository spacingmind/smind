package gate

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ApplyPatch replaces exactly one occurrence of oldStr in body with newStr.
// Zero matches -> ReasonPatchTargetMissing, two or more (or an empty oldStr)
// -> ReasonAmbiguousPatch.
func ApplyPatch(body, oldStr, newStr string) (string, Reason, bool) {
	if oldStr == "" {
		return "", ReasonAmbiguousPatch, false
	}
	switch strings.Count(body, oldStr) {
	case 0:
		return "", ReasonPatchTargetMissing, false
	case 1:
		return strings.Replace(body, oldStr, newStr, 1), "", true
	default:
		return "", ReasonAmbiguousPatch, false
	}
}

// CheckHiddenText rejects runes that would bypass the ASCII-only guard
// regexes or hide content from review: Unicode categories Cf, Co, Cs, Cc
// (except \n \t \r), and fullwidth/halfwidth forms (U+FF01-U+FF5E,
// U+FF61-U+FFDC).
func CheckHiddenText(texts ...string) []Finding {
	var out []Finding
	for _, t := range texts {
		for _, r := range t {
			switch {
			case r == '\n' || r == '\t' || r == '\r':
				continue
			case r >= 0xFF01 && r <= 0xFF5E, r >= 0xFF61 && r <= 0xFFDC:
				out = append(out, finding(ReasonHiddenText, "fullwidth or halfwidth rune U+%04X bypasses ASCII guards", r))
				continue
			}
			if !utf8.ValidRune(r) {
				continue
			}
			switch unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs, unicode.Cc) {
			case true:
				out = append(out, finding(ReasonHiddenText, "hidden control or unassigned rune U+%04X", r))
			}
		}
	}
	return out
}
