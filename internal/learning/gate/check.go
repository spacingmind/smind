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
			newBody, r, ok := ApplyPatch(patchTarget.Body, in.OldString, in.NewString)
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

	if !skipContent {
		v.checkContent(result, in, cfg, add)
		v.checkSimilarity(result, patchTarget, learned, pending, human, cfg, add)
	}

	add(checkProvenance(in, w, cfg.Mode)...)

	v.Result = result
	v.OK = len(v.Findings) == 0
	return v
}

func (v *Verdict) checkContent(result Skill, in Intent, cfg Config, add func(...Finding)) {
	lim := cfg.Limits
	if lim == (Limits{}) {
		lim = DefaultLimits()
	}
	add(CheckCategory(result.Category)...)
	add(CheckDescription(result.Description, lim)...)
	add(CheckBody(result.Body, lim)...)
	add(CheckLinks(result.Description, result.Body)...)
	add(GuardScan(in.Name, result.Category, result.Description, result.Body)...)
	add(CheckPathsAndHosts(result.Description, result.Body)...)
	add(CheckHiddenText(in.Name, result.Category, result.Description, result.Body)...)
	if cfg.Redact != nil {
		for field, text := range map[string]string{"category": result.Category, "description": result.Description, "body": result.Body} {
			if cfg.Redact(text) != text {
				add(Finding{Reason: ReasonSecret, Detail: fmt.Sprintf("redactor modified the %s field", field)})
			}
		}
	}
}

func (v *Verdict) checkSimilarity(result Skill, patchTarget *LiveSkill, learned, pending, human []LiveSkill, cfg Config, add func(...Finding)) {
	proposalFull := stripPrefix(result.Name) + " " + result.Description + " " + result.Body
	proposalHuman := stripPrefix(result.Name) + " " + result.Description

	bestName, bestScore := "", 0.0
	consider := func(name, otherFull, otherHuman string) {
		var score float64
		if otherHuman == "" {
			score = similarity.Score(proposalFull, otherFull)
		} else {
			score = similarity.Score(proposalHuman, otherHuman)
		}
		if score > bestScore || (score == bestScore && name < bestName) || bestName == "" && score > 0 {
			if score > bestScore || (score == bestScore && name < bestName) {
				bestName, bestScore = name, score
			}
		}
	}
	for _, s := range learned {
		if patchTarget != nil && s.Name == patchTarget.Name {
			continue
		}
		consider(s.Name, stripPrefix(s.Name)+" "+s.Description+" "+s.Body, "")
	}
	for _, s := range pending {
		consider(s.Name, stripPrefix(s.Name)+" "+s.Description+" "+s.Body, "")
	}
	for _, s := range human {
		consider(s.Name, "", s.Name+" "+s.Description)
	}

	if bestScore == 0 {
		return
	}
	if cfg.Duplicate > 0 && bestScore >= cfg.Duplicate {
		add(Finding{Reason: ReasonDuplicate, Detail: fmt.Sprintf("duplicate_of=%s score=%.2f", bestName, bestScore)})
		v.DuplicateOf = bestName
	} else if cfg.Possible > 0 && bestScore >= cfg.Possible {
		v.PossibleDuplicateOf = bestName
	}
	if cfg.Possible > 0 && bestScore >= cfg.Possible {
		v.Similarity = bestScore
	}
}
