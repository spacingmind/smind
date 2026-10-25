package plugins

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// themeTokensJSON is the canonical list of CSS custom properties a plugin
// theme must set, per declared scheme. It is the ONE list: the web theme
// check (web/packages/ui/src/test/plugin-theme-tokens.test.ts) reads this very
// file and fails when index.css gains or drops a themeable token, so the
// daemon and the web cannot disagree. Non-colour tokens (font size, shadows,
// durations, content size) are not themeable.
//
//go:embed themetokens.json
var themeTokensJSON []byte

var themeTokens = func() []string {
	var t []string
	if err := json.Unmarshal(themeTokensJSON, &t); err != nil {
		panic("plugins: themetokens.json: " + err.Error())
	}
	return t
}()

// ThemeTokens returns the canonical theme token names, sorted.
func ThemeTokens() []string { return append([]string(nil), themeTokens...) }

// Caps on a theme file.
const (
	maxThemeFileBytes  = 64 << 10
	maxThemeValueBytes = 64
	maxThemeLabelRunes = 48
)

// themeValueRe is the whole grammar a theme value may use: a hex colour, an
// rgb()/rgba()/hsl()/hsla()/oklch() call with 3 numeric channels and an
// optional alpha, a plain length, or the keyword transparent. Nothing else
// is a value: no url(), var(), calc(), attr(), comma lists, quotes,
// backslashes, semicolons, braces, comments or non-ASCII. Only ASCII digits
// and the space and tab separators appear in it, so a lookalike character or
// a newline cannot match.
var themeValueRe = regexp.MustCompile(
	`^(?:` +
		`#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})` +
		`|(?:rgb|rgba|hsl|hsla|oklch)\([ \t]*[+-]?[0-9]*\.?[0-9]+(?:%|deg)?(?:[ \t]*,[ \t]*|[ \t]+)[+-]?[0-9]*\.?[0-9]+(?:%|deg)?(?:[ \t]*,[ \t]*|[ \t]+)[+-]?[0-9]*\.?[0-9]+(?:%|deg)?(?:[ \t]*[,/][ \t]*[+-]?[0-9]*\.?[0-9]+%?)?[ \t]*\)` +
		`|-?[0-9]*\.?[0-9]+(?:px|rem|em|%)?` +
		`|transparent` +
		`)$`)

// ValidThemeValue reports whether v is a safe theme value.
func ValidThemeValue(v string) bool {
	return len(v) <= maxThemeValueBytes && themeValueRe.MatchString(v)
}

// ThemeSpec is one validated theme of a plugin: the token values per scheme
// it declares ("light" and/or "dark").
type ThemeSpec struct {
	ID      string
	Label   string
	Schemes map[string]map[string]string
}

// themeFile is the JSON a theme file holds: one complete token set per scheme.
type themeFile struct {
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
}

// parseThemeFile validates a theme file's bytes: unknown keys are rejected, at
// least one scheme must be declared, each declared scheme must hold every
// canonical token (missing ones are listed) and nothing else, and every value
// must be in the safe grammar. Problems are returned as one message per
// problem, naming the scheme and token.
func parseThemeFile(raw []byte) (map[string]map[string]string, []string) {
	var f themeFile
	if err := strictJSON(raw, &f); err != nil {
		return nil, []string{"is not a valid theme file: " + shortErr(err)}
	}
	schemes := map[string]map[string]string{}
	var probs []string
	for _, s := range []struct {
		name string
		set  map[string]string
	}{{"light", f.Light}, {"dark", f.Dark}} {
		if s.set == nil {
			continue
		}
		known := make(map[string]bool, len(themeTokens))
		for _, t := range themeTokens {
			known[t] = true
		}
		var missing, unknown, bad []string
		for _, t := range themeTokens {
			if _, ok := s.set[t]; !ok {
				missing = append(missing, t)
			}
		}
		for k, v := range s.set {
			switch {
			case !known[k]:
				unknown = append(unknown, quoteTok(k))
			case !ValidThemeValue(v):
				bad = append(bad, k)
			}
		}
		sort.Strings(unknown)
		sort.Strings(bad)
		if len(missing) > 0 {
			probs = append(probs, fmt.Sprintf("%s scheme is missing %d token(s): %s", s.name, len(missing), strings.Join(missing, ", ")))
		}
		if len(unknown) > 0 {
			probs = append(probs, fmt.Sprintf("%s scheme has unknown token(s): %s", s.name, strings.Join(unknown, ", ")))
		}
		if len(bad) > 0 {
			probs = append(probs, fmt.Sprintf("%s scheme has unsafe or invalid value(s) for: %s (use hex, rgb(), hsl(), oklch() or a plain length)", s.name, strings.Join(bad, ", ")))
		}
		schemes[s.name] = s.set
	}
	if len(schemes) == 0 {
		probs = append(probs, `declares no scheme: add a "light" and/or "dark" object holding every token`)
	}
	if len(probs) > 0 {
		return nil, probs
	}
	return schemes, nil
}

// quoteTok renders an arbitrary key for an error message without letting a
// hostile name reshape it.
func quoteTok(k string) string {
	if len(k) > 40 {
		k = k[:40]
	}
	return fmt.Sprintf("%q", k)
}

// strictJSON decodes one JSON value, rejecting unknown fields and trailing data.
func strictJSON(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected data after the JSON value")
	}
	return nil
}
