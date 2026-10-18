// Package redact scrubs secrets and PII from text before it is used as
// learning evidence (ADR-0026 section 2: layered redaction).
package redact

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Placeholder replaces every redacted value.
const Placeholder = "[REDACTED]"

type rule struct {
	name string
	re   *regexp.Regexp
}

var rules = []rule{
	{"awsAccessKeyId", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"pemPrivateKey", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|$)`)},
	{"githubToken", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{30,}\b`)},
	{"githubFineGrainedToken", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)},
	{"slackToken", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{12,}`)},
	{"keyEqualsValue", regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:api[_-]?key|secret|token|passw(?:or)?d|credential|auth)[A-Za-z0-9_.-]*)\s*[:=]\s*("[^"\n]{4,}"|'[^'\n]{4,}'|[^\s"',;]{4,})`)},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)},
	{"cardNumber", regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)},
}

var (
	homeRegexes = []*regexp.Regexp{
		regexp.MustCompile(`/Users/[^/\s]+`),
		regexp.MustCompile(`/home/[^/\s]+`),
		regexp.MustCompile(`(?i)c:\\users\\[^\\\s]+`),
	}
	entropyToken = regexp.MustCompile(`[A-Za-z0-9+/=]{24,}`)
	lowerHex     = regexp.MustCompile(`^[0-9a-f]+$`)
)

// Redactor applies the redaction passes. Build one per run with New; it is
// safe for concurrent use.
type Redactor struct {
	exactSecrets []string
	homeDirs     []string
}

// New builds a Redactor. exactSecrets are values scrubbed by exact match
// (MCP env/header values, run env values); values shorter than 8 bytes are
// ignored. homeDirs are absolute home dirs rewritten to "~".
func New(exactSecrets []string, homeDirs []string) *Redactor {
	var exact []string
	for _, s := range exactSecrets {
		if len(s) >= 8 {
			exact = append(exact, s)
		}
	}
	// Longest first so a secret containing another is replaced whole.
	sort.Slice(exact, func(i, j int) bool { return len(exact[i]) > len(exact[j]) })
	var homes []string
	for _, h := range homeDirs {
		h = strings.TrimSuffix(h, "/")
		if h == "" || h == "/" {
			continue
		}
		homes = append(homes, h)
	}
	return &Redactor{exactSecrets: exact, homeDirs: homes}
}

// String applies all redaction passes, in order: exact secrets, home
// directory prefixes, rule regexes, high-entropy tokens.
func (r *Redactor) String(s string) string {
	for _, sec := range r.exactSecrets {
		s = strings.ReplaceAll(s, sec, Placeholder)
	}
	for _, h := range r.homeDirs {
		s = strings.ReplaceAll(s, h, "~")
	}
	for _, hre := range homeRegexes {
		s = hre.ReplaceAllString(s, "~")
	}
	for _, rl := range rules {
		switch rl.name {
		case "bearer":
			s = rl.re.ReplaceAllString(s, "Bearer "+Placeholder)
		case "keyEqualsValue":
			s = rl.re.ReplaceAllString(s, "$1="+Placeholder)
		case "cardNumber":
			s = rl.re.ReplaceAllStringFunc(s, func(m string) string {
				// The pattern can swallow a trailing separator; keep it so
				// "4111 1111 1111 1111 charged" keeps its space.
				digits := strings.Map(func(c rune) rune {
					if c == ' ' || c == '-' {
						return -1
					}
					return c
				}, m)
				if !luhn(digits) {
					return m
				}
				// The pattern's trailing [ -]? can swallow the separator
				// before the next word; give it back.
				if strings.HasSuffix(m, " ") || strings.HasSuffix(m, "-") {
					return Placeholder + m[len(m)-1:]
				}
				return Placeholder
			})
		default:
			s = rl.re.ReplaceAllString(s, Placeholder)
		}
	}
	s = entropyToken.ReplaceAllStringFunc(s, func(m string) string {
		if isHighEntropy(m) {
			return Placeholder
		}
		return m
	})
	return s
}

// Changed reports whether String would modify s.
func (r *Redactor) Changed(s string) bool { return r.String(s) != s }

// isHighEntropy reports whether token m should be redacted: it mixes digits
// and letters, has Shannon entropy >= 3.5 bits/char, and is not a plain
// lowercase-hex 40/64-char digest (git SHA / sha256).
func isHighEntropy(m string) bool {
	if (len(m) == 40 || len(m) == 64) && lowerHex.MatchString(m) {
		return false
	}
	hasDigit, hasLetter := false, false
	counts := map[rune]int{}
	for _, c := range m {
		counts[c]++
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			hasLetter = true
		}
	}
	if !hasDigit || !hasLetter {
		return false
	}
	var h float64
	for _, n := range counts {
		p := float64(n) / float64(len(m))
		h -= p * math.Log2(p)
	}
	return h >= 3.5
}

// luhn reports whether digits passes the Luhn checksum.
func luhn(digits string) bool {
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}
