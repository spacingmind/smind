package approval

import (
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

// ErrUndoCurrent: undo was asked to go back to the version already current.
var ErrUndoCurrent = errors.New("version is already current")

// ErrNoSuchVersion: the skill has no stored version with that number.
var ErrNoSuchVersion = errors.New("skill has no such version")

func humanOr(actor string) string {
	if actor == "" {
		return "human"
	}
	return actor
}

// Pin keeps the skill out of the curator's reach.
func (s *Service) Pin(skillID int64, actor string) error {
	return s.d.Store.SetSkillPinned(skillID, true, humanOr(actor))
}

// Unpin reverses Pin.
func (s *Service) Unpin(skillID int64, actor string) error {
	return s.d.Store.SetSkillPinned(skillID, false, humanOr(actor))
}

// Disable stops a skill from being offered without archiving it.
func (s *Service) Disable(skillID int64, actor string) error {
	return s.d.Store.SetSkillStatus(skillID, store.SkillDisabled, humanOr(actor), "", "")
}

// Enable reverses Disable.
func (s *Service) Enable(skillID int64, actor string) error {
	return s.d.Store.SetSkillStatus(skillID, store.SkillActive, humanOr(actor), "", "")
}

// Archive retires a skill; it can be restored later.
func (s *Service) Archive(skillID int64, actor, reason string) error {
	return s.d.Store.SetSkillStatus(skillID, store.SkillArchived, humanOr(actor), reason, "")
}

// Restore un-archives a skill after re-running the landing check: the
// library may have gained a colliding or near-identical skill since.
func (s *Service) Restore(skillID int64, actor, reason string) error {
	k, err := s.d.Store.GetLearnedSkill(skillID)
	if err != nil {
		return err
	}
	if k.Status == store.SkillArchived {
		live, err := s.liveSnapshot(k.WorkspaceID)
		if err != nil {
			return err
		}
		final := gate.Skill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body}
		if _, err := s.gateCheck(final, "", live); err != nil {
			return err
		}
	}
	return s.mapErr(0, s.d.Store.SetSkillStatus(skillID, store.SkillActive, humanOr(actor), reason, ""))
}

// Delete hard-deletes the skill and its versions; its ledger rows stay.
func (s *Service) Delete(skillID int64, actor string) error {
	return s.d.Store.DeleteLearnedSkill(skillID, humanOr(actor))
}

// Undo restores an older version's text as a new version. The old text is
// re-verified against the current library first.
func (s *Service) Undo(skillID int64, toVersion int, actor string) (store.LearnedSkill, error) {
	k, err := s.d.Store.GetLearnedSkill(skillID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	if toVersion == k.Version {
		return store.LearnedSkill{}, fmt.Errorf("version %d: %w", toVersion, ErrUndoCurrent)
	}
	versions, err := s.d.Store.ListSkillVersions(skillID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	var old *store.LearnedSkillVersion
	for i := range versions {
		if versions[i].Version == toVersion {
			old = &versions[i]
			break
		}
	}
	if old == nil {
		return store.LearnedSkill{}, fmt.Errorf("version %d: %w", toVersion, ErrNoSuchVersion)
	}
	live, err := s.liveSnapshot(k.WorkspaceID)
	if err != nil {
		return store.LearnedSkill{}, err
	}
	final := gate.Skill{Name: k.Name, Category: k.Category, Description: old.Description, Body: old.Body}
	if _, err := s.gateCheck(final, k.Name, live); err != nil {
		return store.LearnedSkill{}, err
	}
	out, err := s.d.Store.PatchSkill(store.PatchSkillInput{
		ID: k.ID, ExpectVersion: k.Version, Description: old.Description, Body: old.Body,
		Actor: humanOr(actor), Action: "undo", Reason: fmt.Sprintf("restored version %d", toVersion),
	})
	return out, s.mapErr(0, err)
}

// Forget hard-deletes all learning data of the workspace and turns learning
// off (the "forget everything" action).
func (s *Service) Forget(workspaceID int64) error {
	return s.d.Store.ForgetWorkspaceLearning(workspaceID)
}

// SetLearningMode sets the workspace's mode (off|propose). Turning learning
// off keeps every row; only collection and delivery stop.
func (s *Service) SetLearningMode(workspaceID int64, mode string) error {
	return s.d.Store.SetWorkspaceLearningMode(workspaceID, mode)
}
