package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// lenientFloat decodes a JSON number or a numeric string. JSON null decodes
// to nil. Anything else (an object, a non-numeric string, NaN/Inf) is a decode
// error, which callers treat as "this entry is malformed, skip it".
type lenientFloat struct{ v *float64 }

func (f *lenientFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		f.v = nil
		return nil
	}
	s := string(data)
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		s = strings.TrimSpace(str)
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("not a number")
	}
	f.v = &n
	return nil
}

// ptr returns the decoded value, or nil when absent or null.
func (f lenientFloat) ptr() *float64 { return f.v }

// parseRFC3339 parses an RFC 3339 timestamp, returning nil when s is nil,
// empty or unparseable.
func parseRFC3339(s *string) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		return nil
	}
	t = t.UTC()
	if y := t.Year(); y < 1 || y > 9999 {
		return nil // time.Time cannot marshal it, which would lose the whole report
	}
	return &t
}

// maxTextLen caps provider-supplied display text copied into a Report.
const maxTextLen = 128

// capText trims s and truncates it to maxTextLen runes.
func capText(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxTextLen {
		return string(r[:maxTextLen])
	}
	return s
}

// looksLikeHTML reports whether body starts (after whitespace) with '<'.
func looksLikeHTML(body []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(body), []byte("<"))
}
