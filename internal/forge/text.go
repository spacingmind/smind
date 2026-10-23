package forge

import (
	"strings"
	"unicode/utf8"
)

// LogCutMarker is the first line of a CheckLog tail that had earlier output
// cut off; a gitstate caller strips it and sets CheckLogTail.Truncated.
const LogCutMarker = "[smind: earlier log output omitted]"

// SanitizeText returns s as valid UTF-8 without NULs or any other C0 control
// character except \n and \t. It changes nothing else: third-party text is
// shown as written, never interpreted.
func SanitizeText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	clean := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 && c != '\n' && c != '\t' {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// BoundText sanitises s and cuts it to at most max bytes on a rune boundary,
// reporting whether it cut.
func BoundText(s string, max int) (string, bool) {
	s = SanitizeText(s)
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// TailLog sanitises s and keeps its last CheckLogMaxLines lines and
// CheckLogMaxBytes bytes (the start of the kept text is on a rune boundary).
// It returns the kept text and whether anything was cut.
func TailLog(s string) (string, bool) {
	s = strings.TrimRight(SanitizeText(s), "\n")
	cut := false
	if n := strings.Count(s, "\n") + 1; n > CheckLogMaxLines {
		idx := len(s)
		for i := 0; i < CheckLogMaxLines; i++ {
			idx = strings.LastIndexByte(s[:idx], '\n')
		}
		s = s[idx+1:]
		cut = true
	}
	if len(s) > CheckLogMaxBytes {
		start := len(s) - CheckLogMaxBytes
		for start < len(s) && !utf8.RuneStart(s[start]) {
			start++
		}
		s = s[start:]
		cut = true
		// Begin on a line boundary: a first line cut in the middle is noise.
		if nl := strings.IndexByte(s, '\n'); nl >= 0 && nl < len(s)-1 {
			s = s[nl+1:]
		}
	}
	return s, cut
}
