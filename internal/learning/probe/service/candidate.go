package probesvc

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/store"
)

// errNoChange is returned when applying the proposal leaves the skill set as
// it was: there would be nothing for arm B to differ by.
var errNoChange = errors.New("the proposal does not change the active skill set")

func asProjection(k store.LearnedSkill) projection.Skill {
	return projection.Skill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body}
}

// activeSkills is the production projection set of a workspace.
func activeSkills(active []store.LearnedSkill) []projection.Skill {
	out := make([]projection.Skill, 0, len(active))
	for _, k := range active {
		out = append(out, asProjection(k))
	}
	return out
}

// candidateSkills is the active set with the proposal applied (the gate
// approved Item.Result is the final text): create appends, patch replaces
// the target, delete removes it, and a bundle patches its umbrella (item 0)
// and removes the skills it absorbs. The active set is never mutated, and
// the result is only ever handed to projection.ProjectProbe.
func candidateSkills(active []store.LearnedSkill, kind string, items []approval.Item) ([]projection.Skill, error) {
	if len(items) == 0 {
		return nil, errors.New("proposal has no items")
	}
	type entry struct {
		id    int64
		skill projection.Skill
	}
	set := make([]entry, 0, len(active)+len(items))
	for _, k := range active {
		set = append(set, entry{k.ID, asProjection(k)})
	}
	find := func(id int64) int {
		return slices.IndexFunc(set, func(e entry) bool { return id != 0 && e.id == id })
	}
	for i, it := range items {
		action := it.Intent.Action
		switch {
		case kind == "bundle" && i == 0:
			action = gate.ActionPatch
		case kind == "bundle":
			action = gate.ActionDelete
		case action == "":
			switch kind {
			case "create":
				action = gate.ActionCreate
			case "patch":
				action = gate.ActionPatch
			}
		}
		res := it.Result
		switch action {
		case gate.ActionCreate:
			if res.Name == "" {
				return nil, fmt.Errorf("item %d: create without a name", i)
			}
			set = append(set, entry{0, projection.Skill{Name: res.Name, Category: res.Category, Description: res.Description, Body: res.Body}})
		case gate.ActionPatch:
			at := find(it.TargetSkillID)
			if at < 0 {
				return nil, fmt.Errorf("item %d: patch target %d is not an active skill", i, it.TargetSkillID)
			}
			sk := set[at].skill
			if res.Name != "" {
				sk.Name = res.Name
			}
			if res.Category != "" {
				sk.Category = res.Category
			}
			sk.Description, sk.Body = res.Description, res.Body
			set[at].skill = sk
		case gate.ActionDelete:
			at := find(it.TargetSkillID)
			if at < 0 {
				return nil, fmt.Errorf("item %d: delete target %d is not an active skill", i, it.TargetSkillID)
			}
			set = slices.Delete(set, at, at+1)
		default:
			return nil, fmt.Errorf("item %d: unknown action %q", i, action)
		}
	}
	out := make([]projection.Skill, 0, len(set))
	for _, e := range set {
		out = append(out, e.skill)
	}
	if slices.Equal(out, activeSkills(active)) {
		return nil, errNoChange
	}
	return out, nil
}

// candidateName is the skill arm B is expected to load: the first item's
// final name, falling back to the proposal's.
func candidateName(p store.LearnedProposal, items []approval.Item) string {
	if len(items) > 0 && items[0].Result.Name != "" {
		return items[0].Result.Name
	}
	return p.Name
}

// probeProjections are the two distinct skill sets of one probe: A (and its
// replicate A2) get the production active set, B the candidate set.
type probeProjections struct {
	a, b projection.Projection
	name string // the candidate skill arm B should load
}

func (p probeProjections) of(role string) projection.Projection {
	if role == roleB {
		return p.b
	}
	return p.a
}

func (p probeProjections) hashFor(role string) string { return p.of(role).Hash }

// projections builds both sets through projection.ProjectProbe, the only
// path by which a proposed skill reaches an agent. At most two distinct
// hashes exist (ProjectProbe's GC keeps the newest two per workspace), A
// first so B's build leaves it in place.
func (s *Service) projections(prop store.LearnedProposal, att sandbox.Attestation) (probeProjections, error) {
	live, err := s.d.Store.ListLearnedSkills(prop.WorkspaceID, store.SkillActive)
	if err != nil {
		return probeProjections{}, err
	}
	items, err := approval.DecodeItems(prop.Intents)
	if err != nil {
		return probeProjections{}, err
	}
	cand, err := candidateSkills(live, prop.Kind, items)
	if err != nil {
		return probeProjections{}, err
	}
	root := filepath.Join(s.d.Home, "learning", "probe-projections")
	now := s.d.Now()
	a, err := projection.ProjectProbe(att, now, root, prop.WorkspaceID, activeSkills(live))
	if err != nil {
		return probeProjections{}, err
	}
	b, err := projection.ProjectProbe(att, now, root, prop.WorkspaceID, cand)
	if err != nil {
		return probeProjections{}, err
	}
	if a.Hash == b.Hash {
		return probeProjections{}, errNoChange
	}
	return probeProjections{a: a, b: b, name: candidateName(prop, items)}, nil
}
