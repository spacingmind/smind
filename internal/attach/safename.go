package attach

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SafeName reduces a client-supplied file name to a harmless base name of at
// most max bytes. An empty result becomes "file".
func SafeName(name string, max int) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ". ")
	if max > 0 && len(name) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	if name == "" {
		return "file"
	}
	return name
}
