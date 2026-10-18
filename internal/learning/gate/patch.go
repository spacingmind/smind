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
// (except \n \t \r), line/paragraph separators (Zl, Zp), unassigned runes,
// combining marks that follow an ASCII letter (a base letter plus a mark is a
// homoglyph trick; precomposed accented letters are fine), mixed-script words
// (Cyrillic, Greek or Armenian letters next to ASCII letters in one word) and
// fullwidth/halfwidth forms (U+FF01-U+FF5E, U+FF61-U+FFDC).
func CheckHiddenText(texts ...string) []Finding {
	var out []Finding
	for _, t := range texts {
		var prev rune
		for _, r := range t {
			p := prev
			prev = r
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
			switch {
			case unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs, unicode.Cc):
				out = append(out, finding(ReasonHiddenText, "hidden control or unassigned rune U+%04X", r))
			case unicode.In(r, unicode.Zl, unicode.Zp):
				out = append(out, finding(ReasonHiddenText, "line or paragraph separator U+%04X", r))
			case unicode.In(r, unicode.Mn, unicode.Me) && isASCIILetter(p):
				out = append(out, finding(ReasonHiddenText, "combining mark U+%04X after an ASCII letter", r))
			case !unicode.IsGraphic(r) && !unicode.In(r, unicode.Zs):
				out = append(out, finding(ReasonHiddenText, "unassigned rune U+%04X", r))
			}
		}
		out = append(out, mixedScriptWords(t)...)
	}
	return dedupeFindings(out)
}

func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isConfusableScript(r rune) bool {
	return unicode.In(r, unicode.Cyrillic, unicode.Greek, unicode.Armenian)
}

// mixedScriptWords reports each maximal run of letters that mixes ASCII
// letters with Cyrillic, Greek or Armenian letters.
func mixedScriptWords(t string) []Finding {
	var out []Finding
	flush := func(word []rune) {
		var ascii, odd bool
		for _, r := range word {
			ascii = ascii || isASCIILetter(r)
			odd = odd || isConfusableScript(r)
		}
		if ascii && odd {
			out = append(out, finding(ReasonHiddenText, "mixed-script word %q", string(word)))
		}
	}
	var word []rune
	for _, r := range t {
		if unicode.IsLetter(r) {
			word = append(word, r)
			continue
		}
		flush(word)
		word = word[:0]
	}
	flush(word)
	return out
}
