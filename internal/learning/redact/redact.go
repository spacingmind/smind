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
	{"authorization", regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*(?:(?:basic|token|bearer|digest|negotiate|apikey)\s+)?)[^\s"',;]{6,}`)},
	{"quotedKeyValue", regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*?(?:api[_-]?key|apikey|secret|passw(?:or)?d|passphrase|credentials?|private[_-]?key|access[_-]?key|token|auth|authorization|signature|sig|key)[A-Za-z0-9_.-]*)((?:\\)?["']?\s*[:=]\s*(?:\\)?["'])([^"'\n]{4,}?)((?:\\)?["'])`)},
	{"bareKeyValue", regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*?(?:api[_-]?key|apikey|secret|passw(?:or)?d|passphrase|credentials?|private[_-]?key|access[_-]?key|token|auth|authorization|signature|sig|key)[A-Za-z0-9_.-]*)(\s*[:=]\s*)([^\s"',;&\\]{4,})`)},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)},
	{"cardNumber", regexp.MustCompile(`[0-9](?:[0-9 -]*[0-9])?`)},
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
		case "authorization":
			s = rl.re.ReplaceAllString(s, "${1}"+Placeholder)
		case "quotedKeyValue":
			s = replaceKeyValue(s, rl.re, true)
		case "bareKeyValue":
			s = replaceKeyValue(s, rl.re, false)
		case "cardNumber":
			s = rl.re.ReplaceAllStringFunc(s, redactCards)
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

var (
	secretKeyParts = map[string]bool{
		"apikey": true, "secret": true, "password": true, "passwd": true,
		"passphrase": true, "credential": true, "credentials": true,
		"token": true, "tokens": true, "auth": true, "authorization": true,
		"signature": true, "sig": true, "key": true, "secretkey": true,
	}
	// weakKeyParts are words that also name counters and states
	// ("max_tokens: 4096", "credentials: none").
	weakKeyParts   = map[string]bool{"token": true, "tokens": true, "auth": true, "credential": true, "credentials": true}
	benignValues   = map[string]bool{"none": true, "null": true, "false": true, "true": true, "failed": true, "unknown": true}
	authSchemeWord = map[string]bool{"basic": true, "token": true, "bearer": true, "digest": true, "negotiate": true, "apikey": true}
	allDigits      = regexp.MustCompile(`^[0-9]+$`)
	keySubstrings  = []string{"api_key", "api-key", "private_key", "access_key"}
)

// keyParts splits a key on [_.-] and camelCase boundaries, lowercased.
func keyParts(key string) []string {
	var parts []string
	for _, piece := range strings.FieldsFunc(key, func(c rune) bool { return c == '_' || c == '.' || c == '-' }) {
		rs := []rune(piece)
		start := 0
		for i := 1; i < len(rs); i++ {
			cur, prev := rs[i], rs[i-1]
			upper := cur >= 'A' && cur <= 'Z'
			lowerPrev := prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
			acronymEnd := prev >= 'A' && prev <= 'Z' && i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z'
			if upper && (lowerPrev || acronymEnd) {
				parts = append(parts, strings.ToLower(string(rs[start:i])))
				start = i
			}
		}
		parts = append(parts, strings.ToLower(string(rs[start:])))
	}
	return parts
}

// isSecretKeyValue decides whether key=value really carries a secret: the
// key must contain a secret word as a whole part, and counter-like or
// state-like values of weak words ("max_tokens: 4096") are left alone.
func isSecretKeyValue(key, value string) bool {
	lower := strings.ToLower(key)
	strong, weak := false, false
	for _, sub := range keySubstrings {
		if strings.Contains(lower, sub) {
			strong = true
		}
	}
	for _, p := range keyParts(key) {
		switch {
		case !secretKeyParts[p]:
		case weakKeyParts[p]:
			weak = true
		default:
			strong = true
		}
	}
	if !strong && !weak {
		return false
	}
	if !strong {
		if allDigits.MatchString(value) || benignValues[strings.ToLower(value)] {
			return false
		}
	}
	if strings.HasSuffix(lower, "authorization") && authSchemeWord[strings.ToLower(value)] {
		return false
	}
	return true
}

// replaceKeyValue redacts only the value of secret-looking key/value pairs,
// keeping key, quotes and separator.
func replaceKeyValue(s string, re *regexp.Regexp, quoted bool) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		if g == nil || !isSecretKeyValue(g[1], g[3]) {
			return m
		}
		if quoted {
			return g[1] + g[2] + Placeholder + g[4]
		}
		return g[1] + g[2] + Placeholder
	})
}

// redactCards scans one run of digits (with space/dash separators) for
// 13-19 digit windows that start like a card (3..6) and pass Luhn, longest
// window first at each start, left to right. Only the window's span is
// replaced, so trailing digits such as an expiry date survive.
func redactCards(run string) string {
	var pos []int
	for i := 0; i < len(run); i++ {
		if run[i] >= '0' && run[i] <= '9' {
			pos = append(pos, i)
		}
	}
	var out strings.Builder
	last, start := 0, 0
	for start+13 <= len(pos) {
		if c := run[pos[start]]; c < '3' || c > '6' {
			start++
			continue
		}
		matched := false
		for n := min(19, len(pos)-start); n >= 13; n-- {
			digits := make([]byte, n)
			for k := 0; k < n; k++ {
				digits[k] = run[pos[start+k]]
			}
			if luhn(string(digits)) {
				out.WriteString(run[last:pos[start]])
				out.WriteString(Placeholder)
				last = pos[start+n-1] + 1
				start += n
				matched = true
				break
			}
		}
		if !matched {
			start++
		}
	}
	out.WriteString(run[last:])
	return out.String()
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
