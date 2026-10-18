package gate

import (
	"fmt"
	"strings"
)

// CheckLanding re-runs, on the final skill text (create result, patch
// result, human-edited text or merge result), every gate check that does not
// need the transcript: name, caps, links, guard, paths/hosts, hidden text,
// secrets and similarity. Provenance is not re-checked: it was verified when
// the proposal was made. selfName is the skill being rewritten ("" for a
// create) and is excluded from similarity; a create also checks name
// collisions. cfg.Mode is ignored.
func CheckLanding(result Skill, selfName string, live []LiveSkill, cfg Config) Verdict {
	cfg = withDefaults(cfg)
	var v Verdict
	add := func(fs ...Finding) { v.Findings = append(v.Findings, fs...) }

	add(CheckName(result.Name)...)

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

	if selfName == "" {
		for _, s := range append(append([]LiveSkill{}, learned...), pending...) {
			if s.Name == result.Name {
				add(Finding{Reason: ReasonNameCollision, Detail: fmt.Sprintf("name %q already exists as a learned or pending skill", result.Name)})
				break
			}
		}
		for _, s := range human {
			if strings.EqualFold(s.Name, result.Name) {
				add(Finding{Reason: ReasonNameCollision, Detail: fmt.Sprintf("name %q collides with a human skill", result.Name)})
				break
			}
		}
	}

	before := len(v.Findings)
	v.checkContent(result, Intent{Name: result.Name}, cfg, add)
	if len(v.Findings) == before {
		var self *LiveSkill
		if selfName != "" {
			self = &LiveSkill{Name: selfName}
		}
		v.checkSimilarity(result, self, learned, pending, human, cfg, add)
	}

	v.Result = result
	v.OK = len(v.Findings) == 0
	return v
}
