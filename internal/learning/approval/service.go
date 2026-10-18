// Package approval is everything a human can do to learned-skill proposals
// and skills (ADR-0026 §8): approve, reject, edit, merge into a skill, pin,
// disable, archive, undo, delete, forget. Every change that writes skill text
// is re-verified with gate.CheckLanding against a fresh library snapshot.
package approval

import (
	"fmt"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

// Deps are the service's collaborators.
type Deps struct {
	Store *store.Store
	// Human lists the workspace's human/third-party skills (Human=true,
	// Name+Description only); may be nil.
	Human func(workspaceID int64) []gate.LiveSkill
	// Redact is the secret scrubber; may be nil.
	Redact func(string) string
	// GateConfig supplies thresholds/limits, read at call time.
	GateConfig func() gate.Config
}

// Service performs approval actions.
type Service struct{ d Deps }

// New builds a Service.
func New(d Deps) *Service { return &Service{d: d} }

func (s *Service) gateConfig() gate.Config {
	cfg := gate.DefaultConfig()
	if s.d.GateConfig != nil {
		cfg = s.d.GateConfig()
	}
	if cfg.Redact == nil {
		cfg.Redact = s.d.Redact
	}
	return cfg
}

// liveSnapshot is the library as the gate sees it: learned active/disabled
// skills with bodies, pending create proposals (status "proposed") and the
// human skills.
func (s *Service) liveSnapshot(workspaceID int64) ([]gate.LiveSkill, error) {
	skills, err := s.d.Store.ListLearnedSkills(workspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		return nil, err
	}
	live := make([]gate.LiveSkill, 0, len(skills))
	for _, k := range skills {
		live = append(live, gate.LiveSkill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body, Status: k.Status})
	}
	pending, err := s.d.Store.ListProposals(workspaceID, "pending")
	if err != nil {
		return nil, err
	}
	for _, p := range pending {
		if p.Kind != "create" {
			continue
		}
		items, err := DecodeItems(p.Intents)
		if err != nil || len(items) == 0 {
			continue
		}
		r := items[0].Result
		live = append(live, gate.LiveSkill{Name: r.Name, Category: r.Category, Description: r.Description, Body: r.Body, Status: "proposed"})
	}
	if s.d.Human != nil {
		live = append(live, s.d.Human(workspaceID)...)
	}
	return live, nil
}

// NewProposal is what the collector hands over after a passing gate verdict.
type NewProposal struct {
	WorkspaceID, JobID  int64
	Source              string // reflector|curator
	Kind                string // create|patch|bundle
	Name                string
	Items               []Item
	Evidence            string // redacted JSON
	PossibleDuplicateOf string
	Similarity          float64
}

// CreateProposal stores a pending proposal and its "propose" ledger row.
func (s *Service) CreateProposal(np NewProposal) (store.LearnedProposal, error) {
	intents, err := EncodeItems(np.Items)
	if err != nil {
		return store.LearnedProposal{}, err
	}
	p := store.LearnedProposal{
		WorkspaceID: np.WorkspaceID, Kind: np.Kind, Source: np.Source, Name: np.Name,
		Intents: intents, Evidence: np.Evidence,
		PossibleDuplicateOf: np.PossibleDuplicateOf, Similarity: np.Similarity,
	}
	if np.JobID != 0 {
		j := np.JobID
		p.JobID = &j
	}
	if len(np.Items) > 0 && np.Items[0].TargetSkillID != 0 {
		t := np.Items[0].TargetSkillID
		p.TargetSkillID = &t
	}
	created, err := s.d.Store.CreateProposal(p)
	if err != nil {
		return store.LearnedProposal{}, err
	}
	if err := s.d.Store.AppendLedger(store.LearnedLedgerEntry{
		WorkspaceID: np.WorkspaceID, SkillName: np.Name, Action: "propose",
		Reason: fmt.Sprintf("%s proposal #%d", np.Kind, created.ID), Evidence: np.Evidence, Actor: np.Source,
	}); err != nil {
		return store.LearnedProposal{}, err
	}
	return created, nil
}
