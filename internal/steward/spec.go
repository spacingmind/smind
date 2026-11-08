package steward

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Kinds of a spec source and its statuses (ADR-0054 §3).
const (
	SpecSourceContract = "contract"
	SpecSourcePlan     = "plan"
	SpecSourcePRBody   = "pr_body"
	SpecSourceIssue    = "issue"
	SpecSourceADR      = "adr"

	SpecStatusFound       = "found"
	SpecStatusMissing     = "missing"
	SpecStatusUnavailable = "unavailable"
)

// Caps of the spec section.
const (
	specSourcesMax  = 20
	specCriteriaMax = 60
	specTextMax     = 300
	specRefMax      = 200
	specNamedMax    = 10
)

var (
	specLineRe    = regexp.MustCompile(`(?i)^spec:`)
	issueRefRe    = regexp.MustCompile(`(?i)\b(?:closes|fixes|refs)\s+#([0-9]+)`)
	adrMentionRe  = regexp.MustCompile(`(?i)\badr-([0-9]{4})\b`)
	planLineRe    = regexp.MustCompile(`^\s*- (AC[0-9]+[a-z]?) (.+)$`)
	testTokenRe   = regexp.MustCompile(`\bTest[A-Za-z0-9_]+\b`)
	criterionIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)
	testNameRe    = regexp.MustCompile(`^[A-Za-z0-9_]{1,100}$`)
)

// resolveSpec is the daemon's deterministic spec resolution (ADR-0054 §3):
// the sources in fixed order — the validation contract, the plan, then the
// PR body's Spec: line, issue refs and ADR mentions — and the criteria, the
// contract's first, then the plan's ## Acceptance Criteria bullets; the
// contract wins on the same id. It returns the section and the criterion
// texts for the untrusted block. Pure: no clock, no I/O. in.TwoAxis must be
// non-nil.
func resolveSpec(in Inputs) (SpecSection, map[string]string) {
	si := in.TwoAxis.Spec
	s := SpecSection{
		Sources:     []SpecSource{},
		Criteria:    []SpecCriterion{},
		TestsInTree: map[string]bool{},
	}
	texts := map[string]string{}

	addSource := func(kind, ref, status, sha string) {
		if len(s.Sources) >= specSourcesMax {
			return
		}
		for _, src := range s.Sources {
			if src.Kind == kind && src.Ref == ref {
				return
			}
		}
		s.Sources = append(s.Sources, SpecSource{Kind: kind, Ref: ref, Status: status, SHA256: sha})
	}

	if len(si.ContractCriteria) > 0 {
		addSource(SpecSourceContract, "validation-contract", SpecStatusFound, "")
	}
	if si.PlanPath != "" {
		status, sha := SpecStatusMissing, ""
		if si.PlanText != "" {
			sum := sha256.Sum256([]byte(si.PlanText))
			status, sha = SpecStatusFound, hex.EncodeToString(sum[:])
		}
		addSource(SpecSourcePlan, si.PlanPath, status, sha)
	}
	if in.PR != nil {
		for _, ref := range specLineRefs(in.PR.Body) {
			addSource(SpecSourcePRBody, ref, SpecStatusFound, "")
		}
		for _, m := range issueRefRe.FindAllStringSubmatch(in.PR.Body, -1) {
			addSource(SpecSourceIssue, "#"+m[1], SpecStatusUnavailable, "")
		}
		for _, m := range adrMentionRe.FindAllStringSubmatch(in.PR.Body, -1) {
			addSource(SpecSourceADR, "ADR-"+m[1], adrStatus(si.ADRFiles, m[1]), "")
		}
	}

	addCriterion := func(id, text string, named []string) {
		if len(s.Criteria) >= specCriteriaMax || !criterionIDRe.MatchString(id) {
			return
		}
		for _, c := range s.Criteria {
			if c.ID == id {
				return
			}
		}
		names := []string{}
		seen := map[string]bool{}
		for _, n := range named {
			if len(names) >= specNamedMax {
				break
			}
			if !testNameRe.MatchString(n) || seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
		s.Criteria = append(s.Criteria, SpecCriterion{ID: id, NamedTests: names})
		if text != "" {
			texts[id] = sanitize(text, specTextMax)
		}
	}

	for _, c := range si.ContractCriteria {
		addCriterion(c.ID, c.Text, c.NamedTests)
	}
	for _, pc := range planCriteria(si.PlanText) {
		addCriterion(pc.id, pc.text, testTokenRe.FindAllString(pc.text, -1))
	}

	for _, c := range s.Criteria {
		for _, n := range c.NamedTests {
			s.TestsInTree[n] = si.TestsInTree[n]
		}
	}
	return s, texts
}

// HasSpec reports whether the daemon resolved anything for the Spec pass to
// judge against: a found source or at least one criterion.
func (s SpecSection) HasSpec() bool {
	if len(s.Criteria) > 0 {
		return true
	}
	for _, src := range s.Sources {
		if src.Status == SpecStatusFound {
			return true
		}
	}
	return false
}

// specLineRefs reads the PR body's `Spec: <ref>` lines; the ref is the first
// token of the rest of the line, sanitised.
func specLineRefs(body string) []string {
	var refs []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if !specLineRe.MatchString(t) {
			continue
		}
		fields := strings.Fields(t[len("spec:"):])
		if len(fields) == 0 {
			continue
		}
		if ref := sanitize(fields[0], specRefMax); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// adrStatus resolves an ADR number against the decision files at the head.
func adrStatus(adrFiles []string, num string) string {
	for _, name := range adrFiles {
		if strings.HasPrefix(name, num+"-") {
			return SpecStatusFound
		}
	}
	return SpecStatusMissing
}

// planIDText is one `## Acceptance Criteria` bullet.
type planIDText struct {
	id   string
	text string
}

// planCriteria reads the bullets under the plan's ## Acceptance Criteria
// heading; the section ends at the next ## heading. Every `- ACn text` line
// is a criterion.
func planCriteria(planText string) []planIDText {
	var out []planIDText
	inSection := false
	for _, line := range strings.Split(planText, "\n") {
		t := strings.TrimSpace(line)
		if inSection && strings.HasPrefix(t, "## ") {
			break
		}
		if t == "## Acceptance Criteria" {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if m := planLineRe.FindStringSubmatch(line); m != nil {
			out = append(out, planIDText{id: m[1], text: m[2]})
		}
	}
	return out
}
