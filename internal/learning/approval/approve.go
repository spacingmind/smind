package approval

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/similarity"
	"github.com/spacingmind/smind/internal/store"
)

var (
	// ErrNotPending: the proposal was already approved or rejected.
	ErrNotPending = errors.New("proposal is not pending")
	// ErrStale: the skill a proposal or undo was computed against changed
	// since (version moved, or it is no longer live).
	ErrStale = errors.New("learned skill changed since the proposal was made")
	// ErrMergeIntoHuman: merge-into targets must be learned skills.
	ErrMergeIntoHuman = errors.New("cannot merge into a human skill")
	// ErrMergeTargetUnknown: no live learned skill has the merge target name.
	ErrMergeTargetUnknown = errors.New("merge target is not a live learned skill")
	// ErrMergeKind: only a pending create proposal can be merged into a skill.
	ErrMergeKind = errors.New("only a create proposal can be merged into a skill")
	// ErrMergeNeedsBody: the human must supply the merged text.
	ErrMergeNeedsBody = errors.New("merging into a skill requires the merged body")
	// ErrBadPayload: the stored proposal items do not fit the proposal kind.
	ErrBadPayload = errors.New("malformed proposal payload")
)

// GateError reports the gate findings that blocked a landing. Nothing was
// written when it is returned.
type GateError struct{ Findings []gate.Finding }

func (e *GateError) Error() string {
	parts := make([]string, 0, len(e.Findings))
	for _, f := range e.Findings {
		parts = append(parts, string(f.Reason)+": "+f.Detail)
	}
	return "gate rejected landing: " + strings.Join(parts, "; ")
}

// ApproveOptions are the human's choices on an approval.
type ApproveOptions struct {
	Actor string
	// Description and Body, when non-nil, replace the proposed text
	// (edit-then-approve); the edited text is re-verified.
	Description, Body *string
	// MergeInto, for a create proposal, names a learned skill the proposal is
	// merged into instead of landing as a new skill; Body must be supplied.
	MergeInto string
}

func (o ApproveOptions) actor() string {
	if o.Actor == "" {
		return "human"
	}
	return o.Actor
}

func (o ApproveOptions) edit(sk gate.Skill) gate.Skill {
	if o.Description != nil {
		sk.Description = *o.Description
	}
	if o.Body != nil {
		sk.Body = *o.Body
	}
	return sk
}

func (s *Service) pending(id int64) (store.LearnedProposal, error) {
	p, err := s.d.Store.GetProposal(id)
	if err != nil {
		return store.LearnedProposal{}, err
	}
	if p.Status != "pending" {
		return store.LearnedProposal{}, fmt.Errorf("proposal %d is %s: %w", id, p.Status, ErrNotPending)
	}
	return p, nil
}

// mapErr translates store sentinels into the service's.
func (s *Service) mapErr(proposalID int64, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrSkillVersionConflict), errors.Is(err, store.ErrSkillNotLive):
		return fmt.Errorf("%w: %w", ErrStale, err)
	case errors.Is(err, sql.ErrNoRows) && proposalID != 0:
		if p, gerr := s.d.Store.GetProposal(proposalID); gerr == nil && p.Status != "pending" {
			return fmt.Errorf("%w: %w", ErrNotPending, err)
		}
	}
	return err
}

func (s *Service) gateCheck(final gate.Skill, selfName string, live []gate.LiveSkill) (gate.Verdict, error) {
	v := gate.CheckLanding(final, selfName, live, s.gateConfig())
	if !v.OK {
		return v, &GateError{Findings: v.Findings}
	}
	return v, nil
}

// Approve lands a pending proposal after re-verifying the final text against
// a fresh library snapshot. Any gate finding returns a *GateError and writes
// nothing; the proposal stays pending.
func (s *Service) Approve(proposalID int64, o ApproveOptions) (store.LearnedSkill, error) {
	p, err := s.pending(proposalID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	items, err := DecodeItems(p.Intents)
	if err != nil {
		return store.LearnedSkill{}, fmt.Errorf("%w: %w", ErrBadPayload, err)
	}
	if len(items) == 0 {
		return store.LearnedSkill{}, fmt.Errorf("%w: no items", ErrBadPayload)
	}
	if o.MergeInto != "" {
		return s.approveMerge(p, items, o)
	}
	switch p.Kind {
	case "create":
		return s.approveCreate(p, items[0], o)
	case "patch":
		return s.approvePatch(p, items[0], o)
	case "bundle":
		return s.approveBundle(p, items, o)
	}
	return store.LearnedSkill{}, fmt.Errorf("%w: unknown kind %q", ErrBadPayload, p.Kind)
}

func (s *Service) approveCreate(p store.LearnedProposal, it Item, o ApproveOptions) (store.LearnedSkill, error) {
	live, err := s.liveSnapshot(p.WorkspaceID, p.ID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	final := o.edit(it.Result)
	v, err := s.gateCheck(final, "", live)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	k, err := s.d.Store.LandSkill(store.LandSkillInput{
		Skill: store.LearnedSkill{
			WorkspaceID: p.WorkspaceID, Name: final.Name, Category: final.Category,
			Description: final.Description, Body: final.Body, Source: p.Source,
			CreatedByRun: s.jobRun(p), Evidence: p.Evidence,
			PossibleDuplicateOf: v.PossibleDuplicateOf, Similarity: v.Similarity,
		},
		Actor: o.actor(), Reason: "approved proposal: " + p.Name, Action: "approve", ProposalID: p.ID,
	})
	return k, s.mapErr(p.ID, err)
}

// jobRun is the run id of the job that produced the proposal ("" if none).
func (s *Service) jobRun(p store.LearnedProposal) string {
	if p.JobID == nil {
		return ""
	}
	j, err := s.d.Store.GetLearningJob(*p.JobID)
	if err != nil {
		return ""
	}
	return j.RunID
}

// liveTarget loads a patch/bundle target and checks it still matches what
// the proposal was computed against.
func (s *Service) liveTarget(p store.LearnedProposal, id int64, baseVersion int) (store.LearnedSkill, error) {
	k, err := s.d.Store.GetLearnedSkill(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.LearnedSkill{}, fmt.Errorf("skill %d no longer exists: %w", id, ErrStale)
		}
		return store.LearnedSkill{}, err
	}
	if k.WorkspaceID != p.WorkspaceID {
		return store.LearnedSkill{}, fmt.Errorf("skill %d: %w", id, store.ErrProposalWorkspaceMismatch)
	}
	if k.Status != store.SkillActive && k.Status != store.SkillDisabled {
		return store.LearnedSkill{}, fmt.Errorf("skill %q is %s: %w", k.Name, k.Status, ErrStale)
	}
	if baseVersion != anyVersion && k.Version != baseVersion {
		return store.LearnedSkill{}, fmt.Errorf("skill %q is at version %d, proposal was made against %d: %w", k.Name, k.Version, baseVersion, ErrStale)
	}
	return k, nil
}

func (s *Service) approvePatch(p store.LearnedProposal, it Item, o ApproveOptions) (store.LearnedSkill, error) {
	target, err := s.liveTarget(p, it.TargetSkillID, it.BaseVersion)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	live, err := s.liveSnapshot(p.WorkspaceID, p.ID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	final := o.edit(it.Result)
	if _, err := s.gateCheck(final, target.Name, live); err != nil {
		return store.LearnedSkill{}, err
	}
	k, err := s.d.Store.PatchSkill(store.PatchSkillInput{
		ID: target.ID, ExpectVersion: target.Version, Description: final.Description, Body: final.Body,
		Actor: o.actor(), Action: "patch", Reason: "approved proposal: " + p.Name, ProposalID: p.ID,
	})
	return k, s.mapErr(p.ID, err)
}

func (s *Service) approveBundle(p store.LearnedProposal, items []Item, o ApproveOptions) (store.LearnedSkill, error) {
	if len(items) < 2 {
		return store.LearnedSkill{}, fmt.Errorf("%w: bundle needs an umbrella and absorbed skills", ErrBadPayload)
	}
	umbrella, err := s.liveTarget(p, items[0].TargetSkillID, items[0].BaseVersion)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	absorbed := make([]int64, 0, len(items)-1)
	absorbedNames := make([]string, 0, len(items)-1)
	for _, it := range items[1:] {
		if it.TargetSkillID == umbrella.ID {
			return store.LearnedSkill{}, fmt.Errorf("%w: %w", ErrBadPayload, store.ErrBundleSelfAbsorb)
		}
		a, err := s.liveTarget(p, it.TargetSkillID, anyVersion)
		if err != nil {
			return store.LearnedSkill{}, err
		}
		absorbed = append(absorbed, a.ID)
		absorbedNames = append(absorbedNames, a.Name)
	}
	live, err := s.liveSnapshot(p.WorkspaceID, p.ID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	// The absorbed skills are archived by this very landing, so the merged
	// text must not be compared against them.
	live = slices.DeleteFunc(live, func(l gate.LiveSkill) bool {
		return !l.Human && slices.Contains(absorbedNames, l.Name)
	})
	final := o.edit(items[0].Result)
	if _, err := s.gateCheck(final, umbrella.Name, live); err != nil {
		return store.LearnedSkill{}, err
	}
	err = s.d.Store.ApplyBundle(store.ApplyBundleInput{
		ProposalID: p.ID, UmbrellaID: umbrella.ID, ExpectVersion: umbrella.Version,
		Description: final.Description, Body: final.Body, Actor: o.actor(),
		Reason: "approved merge: " + p.Name, AbsorbedIDs: absorbed,
	})
	if err != nil {
		return store.LearnedSkill{}, s.mapErr(p.ID, err)
	}
	return s.d.Store.GetLearnedSkill(umbrella.ID)
}

// resolveMergeTarget finds the live learned skill a create proposal would be
// merged into; a human skill or an unknown name is an error.
func (s *Service) resolveMergeTarget(workspaceID int64, name string) (store.LearnedSkill, error) {
	skills, err := s.d.Store.ListLearnedSkills(workspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	for _, k := range skills {
		if k.Name == name {
			return k, nil
		}
	}
	if s.d.Human != nil {
		for _, h := range s.d.Human(workspaceID) {
			if strings.EqualFold(h.Name, name) {
				return store.LearnedSkill{}, fmt.Errorf("%q: %w", name, ErrMergeIntoHuman)
			}
		}
	}
	return store.LearnedSkill{}, fmt.Errorf("%q: %w", name, ErrMergeTargetUnknown)
}

func (s *Service) approveMerge(p store.LearnedProposal, items []Item, o ApproveOptions) (store.LearnedSkill, error) {
	if p.Kind != "create" {
		return store.LearnedSkill{}, ErrMergeKind
	}
	if o.Body == nil {
		return store.LearnedSkill{}, ErrMergeNeedsBody
	}
	target, err := s.resolveMergeTarget(p.WorkspaceID, o.MergeInto)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	live, err := s.liveSnapshot(p.WorkspaceID, p.ID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	merged := gate.Skill{Name: target.Name, Category: target.Category, Description: target.Description, Body: *o.Body}
	if o.Description != nil {
		merged.Description = *o.Description
	}
	if _, err := s.gateCheck(merged, target.Name, live); err != nil {
		return store.LearnedSkill{}, err
	}
	k, err := s.d.Store.PatchSkill(store.PatchSkillInput{
		ID: target.ID, ExpectVersion: target.Version, Description: merged.Description, Body: merged.Body,
		Actor: o.actor(), Action: "merge", Reason: fmt.Sprintf("merged proposal %q", p.Name), ProposalID: p.ID,
	})
	return k, s.mapErr(p.ID, err)
}

// Reject settles a pending proposal as rejected; the note is kept as a
// negative example for the reflector.
func (s *Service) Reject(proposalID int64, note, actor string) error {
	if _, err := s.pending(proposalID); err != nil {
		return err
	}
	if actor == "" {
		actor = "human"
	}
	return s.mapErr(0, s.d.Store.DecideProposal(proposalID, "rejected", note, actor))
}

// MergeDraft is the read-only data for the UI's side-by-side merge view.
type MergeDraft struct {
	Proposed   gate.Skill
	Target     store.LearnedSkill
	Similarity float64
}

// MergeDraft returns the proposal's text next to the learned skill it would
// be merged into. A human or unknown target is an error.
func (s *Service) MergeDraft(proposalID int64, target string) (MergeDraft, error) {
	p, err := s.pending(proposalID)
	if err != nil {
		return MergeDraft{}, err
	}
	if p.Kind != "create" {
		return MergeDraft{}, ErrMergeKind
	}
	items, err := DecodeItems(p.Intents)
	if err != nil || len(items) == 0 {
		return MergeDraft{}, fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	t, err := s.resolveMergeTarget(p.WorkspaceID, target)
	if err != nil {
		return MergeDraft{}, err
	}
	r := items[0].Result
	trim := func(n string) string { return strings.TrimPrefix(n, "learned-") }
	m := similarity.NewMatcher(trim(r.Name) + " " + r.Description + " " + r.Body)
	return MergeDraft{
		Proposed: r, Target: t,
		Similarity: m.Score(trim(t.Name) + " " + t.Description + " " + t.Body),
	}, nil
}

// anyVersion makes liveTarget skip the version check (absorbed skills).
const anyVersion = -1
