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

// tokenJaccard returns |A∩B|/|A∪B| over whitespace-split word sets.
func tokenJaccard(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)
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

// trigramDice returns 2|A∩B|/(|A|+|B|) over sets of 3-rune windows of s
// (a string with fewer than 3 runes is one gram).
func trigramDice(a, b string) float64 {
	setA := gramSet(a)
	setB := gramSet(b)
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

// Score returns max(tokenJaccard, trigramDice) of Normalize(a) and
// Normalize(b). It is in [0,1], 0 if either input is empty, symmetric, and
// deterministic.
func Score(a, b string) float64 {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	j := tokenJaccard(na, nb)
	d := trigramDice(na, nb)
	if d > j {
		return d
	}
	return j
}
