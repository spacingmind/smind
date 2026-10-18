package gate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/learning/similarity"
)

func stripPrefix(name string) string {
	return strings.TrimPrefix(name, "learned-")
}

func isLearnedStatus(s LiveSkill) bool {
	return !s.Human && (s.Status == "active" || s.Status == "disabled")
}

// Check validates an intent against the library snapshot and the reflected
// window. It is the only path that can bless a skill; a rejected intent
// produces findings and nothing else.
func Check(in Intent, w Window, live []LiveSkill, cfg Config) Verdict {
	cfg = withDefaults(cfg)
	var v Verdict
	add := func(fs ...Finding) { v.Findings = append(v.Findings, fs...) }

	switch in.Action {
	case ActionCreate, ActionPatch, ActionDelete:
	default:
		return Verdict{Findings: []Finding{{Reason: ReasonBadAction, Detail: fmt.Sprintf("unknown action %q", in.Action)}}}
	}
	if cfg.Mode == ModeCurate && in.Action == ActionCreate {
		return Verdict{Findings: []Finding{{Reason: ReasonBadAction, Detail: "curator intents may only patch or delete"}}}
	}

	add(CheckName(in.Name)...)
	if in.Reason == "" {
		add(Finding{Reason: ReasonBadReason, Detail: "reason is empty"})
	} else if utf8.RuneCountInString(in.Reason) > 500 {
		add(Finding{Reason: ReasonBadReason, Detail: "reason exceeds 500 runes"})
	}
	// The reason is never persisted, but it is shown to reviewers and may be
	// echoed into later prompts, so it gets the same hostile-text scans.
	var reasonFindings []Finding
	reasonFindings = append(reasonFindings, CheckLinks(in.Reason)...)
	reasonFindings = append(reasonFindings, CheckHiddenText(in.Reason)...)
	reasonFindings = append(reasonFindings, GuardScan(in.Reason)...)
	for _, f := range dedupeFindings(reasonFindings) {
		add(finding(f.Reason, "reason: %s", f.Detail))
	}

	var learned, pending, human []LiveSkill
	for _, s := range live {
		switch {
		case s.Human:
			human = append(human, s)
		case s.Status == "proposed":
			pending = append(pending, s)
		case isLearnedStatus(s):
			learned = append(learned, s)
		}
	}

	find := func(list []LiveSkill, name string) *LiveSkill {
		for i := range list {
			if list[i].Name == name {
				return &list[i]
			}
		}
		return nil
	}

	var result Skill
	var patchTarget *LiveSkill
	skipContent := false
	switch in.Action {
	case ActionCreate:
		for _, s := range append(append([]LiveSkill{}, learned...), pending...) {
			if s.Name == in.Name {
				add(Finding{Reason: ReasonNameCollision, Detail: fmt.Sprintf("name %q already exists as a learned or pending skill", in.Name)})
				break
			}
		}
		for _, s := range human {
			if strings.EqualFold(s.Name, in.Name) {
				add(Finding{Reason: ReasonNameCollision, Detail: fmt.Sprintf("name %q collides with a human skill", in.Name)})
				break
			}
		}
		result = Skill{Name: in.Name, Category: in.Category, Description: in.Description, Body: in.Body}
	case ActionPatch:
		patchTarget = find(learned, in.Name)
		if patchTarget == nil {
			add(Finding{Reason: ReasonPatchTargetMissing, Detail: fmt.Sprintf("no learned skill named %q to patch", in.Name)})
			skipContent = true
		} else {
			newBody, r, ok := patchTarget.Body, Reason(""), true
			metaOnly := in.OldString == "" && in.NewString == "" && (in.Description != "" || in.Category != "")
			if !metaOnly {
				newBody, r, ok = ApplyPatch(patchTarget.Body, in.OldString, in.NewString)
			}
			if !ok {
				add(Finding{Reason: r, Detail: fmt.Sprintf("patch to %q failed: %s", in.Name, r)})
				skipContent = true
			} else {
				result = Skill{Name: patchTarget.Name, Category: patchTarget.Category, Description: patchTarget.Description, Body: newBody}
				if in.Description != "" {
					result.Description = in.Description
				}
				if in.Category != "" {
					result.Category = in.Category
				}
			}
		}
	case ActionDelete:
		if find(learned, in.Name) == nil {
			add(Finding{Reason: ReasonPatchTargetMissing, Detail: fmt.Sprintf("no learned skill named %q to delete", in.Name)})
		} else if in.AbsorbedInto == in.Name {
			add(Finding{Reason: ReasonBadAbsorbedInto, Detail: "absorbed_into must be a different skill"})
		} else if t := find(learned, in.AbsorbedInto); t == nil {
			add(Finding{Reason: ReasonBadAbsorbedInto, Detail: fmt.Sprintf("absorbed_into %q is not a live learned skill", in.AbsorbedInto)})
		}
		skipContent = true
	}

	if cfg.Mode == ModeCurate && in.Action != ActionCreate {
		add(checkCurateSpans(in, learned)...)
	}

	if !skipContent {
		before := len(v.Findings)
		v.checkContent(result, in, cfg, add)
		if len(v.Findings) == before {
			v.checkSimilarity(result, patchTarget, learned, pending, human, cfg, add)
		}
	}

	add(checkProvenance(in, w, cfg.Mode, proposedTexts(in))...)

	v.Result = result
	v.OK = len(v.Findings) == 0
	return v
}

func (v *Verdict) checkContent(result Skill, in Intent, cfg Config, add func(...Finding)) {
	lim := cfg.Limits
	add(CheckCategory(result.Category)...)
	add(CheckDescription(result.Description, lim)...)
	add(CheckBody(result.Body, lim)...)
	add(CheckLinks(result.Description, result.Body)...)
	add(GuardScan(in.Name, result.Category, result.Description, result.Body)...)
	add(CheckPathsAndHosts(result.Description, result.Body)...)
	add(CheckHiddenText(in.Name, result.Category, result.Description, result.Body)...)
	if cfg.Redact != nil {
		for _, f := range []struct{ field, text string }{
			{"category", result.Category}, {"description", result.Description}, {"body", result.Body},
		} {
			if cfg.Redact(f.text) != f.text {
				add(Finding{Reason: ReasonSecret, Detail: fmt.Sprintf("redactor modified the %s field", f.field)})
			}
		}
	}
}

func (v *Verdict) checkSimilarity(result Skill, patchTarget *LiveSkill, learned, pending, human []LiveSkill, cfg Config, add func(...Finding)) {
	full := similarity.NewMatcher(stripPrefix(result.Name) + " " + result.Description + " " + result.Body)
	humanOnly := similarity.NewMatcher(stripPrefix(result.Name) + " " + result.Description)

	bestName, bestScore := "", 0.0
	consider := func(name string, score float64) {
		if score > bestScore || (score == bestScore && score > 0 && name < bestName) {
			bestName, bestScore = name, score
		}
	}
	for _, s := range learned {
		if patchTarget != nil && s.Name == patchTarget.Name {
			continue
		}
		consider(s.Name, full.Score(stripPrefix(s.Name)+" "+s.Description+" "+s.Body))
	}
	for _, s := range pending {
		consider(s.Name, full.Score(stripPrefix(s.Name)+" "+s.Description+" "+s.Body))
	}
	for _, s := range human {
		consider(s.Name, humanOnly.Score(s.Name+" "+s.Description))
	}

	if bestScore == 0 {
		return
	}
	if bestScore >= cfg.Duplicate {
		add(Finding{Reason: ReasonDuplicate, Detail: fmt.Sprintf("duplicate_of=%s score=%.2f", bestName, bestScore)})
		v.DuplicateOf = bestName
	} else if bestScore >= cfg.Possible {
		v.PossibleDuplicateOf = bestName
	}
	if bestScore >= cfg.Possible {
		v.Similarity = bestScore
	}
}

// proposedTexts are the model-authored pieces an intent would persist:
// create contributes description and body, patch its new description and
// new_string.
func proposedTexts(in Intent) []string {
	switch in.Action {
	case ActionCreate:
		return []string{in.Description, in.Body}
	case ActionPatch:
		return []string{in.Description, in.NewString}
	}
	return nil
}

// withDefaults fills unset tunables so a zero Config fails closed: dedupe is
// never silently disabled and limits are never zero.
func withDefaults(cfg Config) Config {
	d := DefaultLimits()
	if cfg.Limits.MaxBodyLines <= 0 {
		cfg.Limits.MaxBodyLines = d.MaxBodyLines
	}
	if cfg.Limits.MaxBodyBytes <= 0 {
		cfg.Limits.MaxBodyBytes = d.MaxBodyBytes
	}
	if cfg.Limits.MaxDescChars <= 0 {
		cfg.Limits.MaxDescChars = d.MaxDescChars
	}
	if cfg.Duplicate <= 0 {
		cfg.Duplicate = defaultDuplicate
	}
	if cfg.Possible <= 0 {
		cfg.Possible = defaultPossible
	}
	return cfg
}

// checkCurateSpans binds a curator patch to already-approved content: every
// code span in the new text must occur in the body of some live learned skill,
// since curate mode has no evidence to ground it.
func checkCurateSpans(in Intent, learned []LiveSkill) []Finding {
	spans := codeSpans([]string{in.Description, in.NewString})
	if len(spans) == 0 {
		return nil
	}
	bodies := make([]string, 0, len(learned))
	for _, s := range learned {
		bodies = append(bodies, collapseSpace(s.Body))
	}
	var out []Finding
	for _, sp := range spans {
		if !spanInAny(sp, bodies) {
			out = append(out, finding(ReasonUngroundedContent, "code span not in any live learned skill: %q", sp))
		}
	}
	return out
}
