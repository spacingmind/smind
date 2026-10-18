// Package similarity provides a deterministic text-similarity score used by
// the learning gate's dedupe check (ADR-0026 D12).
package similarity

import (
	"strings"
	"unicode"
)

// Normalize lowercases s, replaces every run of non-letter/non-digit runes
// with one space, and trims.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// jaccard returns |A∩B|/|A∪B| over word sets.
func jaccard(setA, setB map[string]struct{}) float64 {
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for w := range setA {
		if _, ok := setB[w]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	return float64(inter) / float64(union)
}

func wordSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, w := range strings.Fields(s) {
		set[w] = struct{}{}
	}
	return set
}

// dice returns 2|A∩B|/(|A|+|B|) over gram sets.
func dice(setA, setB map[string]struct{}) float64 {
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for g := range setA {
		if _, ok := setB[g]; ok {
			inter++
		}
	}
	return float64(2*inter) / float64(len(setA)+len(setB))
}

// gramSet returns the set of 3-rune windows of s (a string with fewer than 3
// runes is one gram).
func gramSet(s string) map[string]struct{} {
	runes := []rune(s)
	set := make(map[string]struct{})
	if len(runes) == 0 {
		return set
	}
	if len(runes) < 3 {
		set[string(runes)] = struct{}{}
		return set
	}
	for i := 0; i+3 <= len(runes); i++ {
		set[string(runes[i:i+3])] = struct{}{}
	}
	return set
}

func best(j, d float64) float64 {
	if d > j {
		return d
	}
	return j
}

// Score returns max(tokenJaccard, trigramDice) of Normalize(a) and
// Normalize(b). It is in [0,1], 0 if either input is empty, symmetric, and
// deterministic.
func Score(a, b string) float64 {
	return NewMatcher(a).Score(b)
}

// Matcher scores many texts against one fixed text without re-normalising
// and re-gramming it each time. Score(a, b) == NewMatcher(a).Score(b).
type Matcher struct {
	norm  string
	words map[string]struct{}
	grams map[string]struct{}
}

// NewMatcher precomputes the normalised text and word/gram sets of text.
func NewMatcher(text string) *Matcher {
	n := Normalize(text)
	m := &Matcher{norm: n}
	if n != "" {
		m.words = wordSet(n)
		m.grams = gramSet(n)
	}
	return m
}

// Score returns the similarity of the matcher's text and other.
func (m *Matcher) Score(other string) float64 {
	no := Normalize(other)
	if m.norm == "" || no == "" {
		return 0
	}
	return best(jaccard(m.words, wordSet(no)), dice(m.grams, gramSet(no)))
}
