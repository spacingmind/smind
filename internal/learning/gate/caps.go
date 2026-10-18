// Package gate implements the deterministic validation gate that is the only
// code path allowed to turn a proposed skill into a stored one (ADR-0026).
// The gate is pure: no database, filesystem, network, or other smind imports.
package gate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Reason string

const (
	ReasonBadName             Reason = "bad_name"
	ReasonBodyTooLong         Reason = "body_too_long"
	ReasonBadDescription      Reason = "bad_description"
	ReasonBadCategory         Reason = "bad_category"
	ReasonBadFrontmatter      Reason = "bad_frontmatter"
	ReasonURLLink             Reason = "url_link"
	ReasonGuard               Reason = "guard"
	ReasonSecret              Reason = "secret_hit"
	ReasonAbsPath             Reason = "abs_path"
	ReasonHostName            Reason = "host_name"
	ReasonBadAction           Reason = "bad_action"
	ReasonNameCollision       Reason = "name_collision"
	ReasonBadAbsorbedInto     Reason = "bad_absorbed_into"
	ReasonAmbiguousPatch      Reason = "ambiguous_patch"
	ReasonPatchTargetMissing  Reason = "patch_target_missing"
	ReasonDuplicate           Reason = "duplicate"
	ReasonNoProvenance        Reason = "no_provenance"
	ReasonBadEvidence         Reason = "bad_evidence"
	ReasonEvidenceNotVerbatim Reason = "evidence_not_verbatim"
	ReasonHiddenText          Reason = "hidden_text"
	ReasonBadReason           Reason = "bad_reason"
)

// Finding is one gate rejection reason. Detail is short and never echoes more
// than 80 characters of the input.
type Finding struct {
	Reason Reason
	Detail string
}

// Limits are the size caps the gate enforces.
type Limits struct {
	MaxBodyLines int
	MaxBodyBytes int
	MaxDescChars int
}

func DefaultLimits() Limits {
	return Limits{MaxBodyLines: 25, MaxBodyBytes: 4096, MaxDescChars: 160}
}

func finding(r Reason, format string, args ...any) Finding {
	d := fmt.Sprintf(format, args...)
	if utf8.RuneCountInString(d) > 80 {
		d = string([]rune(d)[:80])
	}
	return Finding{Reason: r, Detail: d}
}

var (
	nameRe     = regexp.MustCompile(`^learned-[a-z0-9]+(-[a-z0-9]+)*$`)
	categoryRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	cueRe      = regexp.MustCompile(`(?i)\b(use when|when|whenever|if|before|after)\b`)
	sentenceRe = regexp.MustCompile(`[.!?](\s|$)`)
	fmKeyRe    = regexp.MustCompile(`^(name|description|category|allowed-tools|model|tools)\s*:`)

	urlRe     = regexp.MustCompile(`(?i)(https?://|ftp://|file://|www\.|\]\([^)]*\)|<https?:)`)
	htmlTagRe = regexp.MustCompile(`(?i)</?(a|img|script|iframe|style|div|span|svg|link|meta|object|embed|form|input|details|summary)\b[^>]*>`)

	absPathRe  = regexp.MustCompile("(^|[\\s`\"'(=])(/(?:Users|home|var|etc|opt|tmp|usr|private|Volumes|root|srv|mnt)/[^\\s`\"')]+)")
	winPathRe  = regexp.MustCompile(`[A-Za-z]:\\[^\s]+`)
	ipv4Re     = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	hostNameRe = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(\.[a-z0-9-]+)*\.(com|net|org|io|dev|ai|local|internal|lan|home|cloud|app)\b`)
)

// CheckName validates the skill name: ^learned-[a-z0-9]+(-[a-z0-9]+)*$, total
// length <= 64.
func CheckName(name string) []Finding {
	if len(name) > 64 {
		return []Finding{finding(ReasonBadName, "name length %d > 64", len(name))}
	}
	if !nameRe.MatchString(name) {
		return []Finding{finding(ReasonBadName, "name must match learned-<kebab>: %q", name)}
	}
	return nil
}

// CheckCategory validates the category: empty is OK, otherwise one
// ^[a-z][a-z0-9-]{0,31}$ word (no commas or spaces).
func CheckCategory(cat string) []Finding {
	if cat == "" {
		return nil
	}
	if !categoryRe.MatchString(cat) {
		return []Finding{finding(ReasonBadCategory, "category must be a single lowercase word: %q", cat)}
	}
	return nil
}

// CheckDescription validates the description: non-empty after trim, no
// newline, rune length <= lim.MaxDescChars, at most one sentence, and a
// trigger cue (use when / when / whenever / if / before / after).
func CheckDescription(d string, lim Limits) []Finding {
	var out []Finding
	trimmed := strings.TrimSpace(d)
	if trimmed == "" {
		return []Finding{finding(ReasonBadDescription, "description is empty")}
	}
	if strings.ContainsAny(d, "\n\r") {
		out = append(out, finding(ReasonBadDescription, "description must not contain a newline"))
	}
	if n := utf8.RuneCountInString(trimmed); n > lim.MaxDescChars {
		out = append(out, finding(ReasonBadDescription, "description is %d runes, max %d", n, lim.MaxDescChars))
	}
	if len(sentenceRe.FindAllStringIndex(trimmed, -1)) > 1 {
		out = append(out, finding(ReasonBadDescription, "description must be one sentence"))
	}
	if !cueRe.MatchString(trimmed) {
		out = append(out, finding(ReasonBadDescription, "description lacks a trigger cue (use when / when / whenever / if / before / after)"))
	}
	return out
}

// CheckBody validates the skill body: non-empty, <= lim.MaxBodyBytes bytes,
// at most lim.MaxBodyLines non-blank lines, and no frontmatter (skills get
// their frontmatter from the gate, never from the model).
func CheckBody(body string, lim Limits) []Finding {
	var out []Finding
	if strings.TrimSpace(body) == "" {
		return []Finding{finding(ReasonBodyTooLong, "body is empty")}
	}
	if len(body) > lim.MaxBodyBytes {
		out = append(out, finding(ReasonBodyTooLong, "body is %d bytes, max %d", len(body), lim.MaxBodyBytes))
	}
	lines := strings.Split(body, "\n")
	nonBlank := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			nonBlank++
		}
	}
	if nonBlank > lim.MaxBodyLines {
		out = append(out, finding(ReasonBodyTooLong, "body has %d non-blank lines, max %d", nonBlank, lim.MaxBodyLines))
	}
	if strings.HasPrefix(strings.TrimLeft(body, " \t"), "---") {
		out = append(out, finding(ReasonBadFrontmatter, "body must not start with a frontmatter fence"))
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "---" {
			continue
		}
		for j := i + 1; j <= i+6 && j < len(lines); j++ {
			if fmKeyRe.MatchString(lines[j]) {
				out = append(out, finding(ReasonBadFrontmatter, "frontmatter key found after a --- fence near line %d", j+1))
				break
			}
		}
	}
	return out
}

// CheckLinks rejects URLs, links, and HTML tags in the given texts.
func CheckLinks(texts ...string) []Finding {
	var out []Finding
	for _, t := range texts {
		if m := urlRe.FindString(t); m != "" {
			out = append(out, finding(ReasonURLLink, "link or URL not allowed: %q", m))
		}
		if m := htmlTagRe.FindString(t); m != "" {
			out = append(out, finding(ReasonURLLink, "html tag not allowed: %q", m))
		}
	}
	return out
}

// CheckPathsAndHosts rejects absolute paths and host names / IP addresses in
// the given texts. Relative paths and ~/ prefixes are allowed.
func CheckPathsAndHosts(texts ...string) []Finding {
	var out []Finding
	for _, t := range texts {
		if m := absPathRe.FindStringSubmatch(t); m != nil {
			out = append(out, finding(ReasonAbsPath, "absolute path not allowed: %q", m[1]))
		}
		if m := winPathRe.FindString(t); m != "" {
			out = append(out, finding(ReasonAbsPath, "absolute path not allowed: %q", m))
		}
		if m := ipv4Re.FindString(t); m != "" {
			out = append(out, finding(ReasonHostName, "IP address not allowed: %q", m))
		}
		if m := hostNameRe.FindString(t); m != "" {
			out = append(out, finding(ReasonHostName, "host name not allowed: %q", m))
		}
	}
	return out
}
