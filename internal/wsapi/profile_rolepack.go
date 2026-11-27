package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

// The role-pack sync RPCs (ADR-0058 PR 2): profile.rolePack.check is the
// read-only report the Settings UI renders, profile.rolePack.apply is the
// CLI's --apply over the same engine (PlanApply then ApplyRoles, written
// through Registry.UpdateIf with the updatedAt the check read). No logic of
// the engine is copied here; the daemon only adapts shapes and injects the
// registry.

// rolePackCheck and rolePackBackupRoot are the two effects of these handlers
// the tests swap: the check is staged without writing real profiles, and the
// backup root points somewhere disposable.
var (
	rolePackCheck      = profiles.CheckRoles
	rolePackBackupRoot = func() string { return filepath.Join(config.Dir(), "backups", "role-pack") }
)

// rolePackFieldView is one synced field of one seat as the wire carries it.
// Stored and Want are filled for capability fields only (short values); a
// prompt field carries its diff only.
type rolePackFieldView struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Diff   string `json:"diff"`
	Stored string `json:"stored,omitempty"`
	Want   string `json:"want,omitempty"`
}

// rolePackSeatView is one seat of the pack: one entry per pack seat in pack
// order, fields empty unless the seat's status is ok.
type rolePackSeatView struct {
	Name       string              `json:"name"`
	ID         int64               `json:"id"`
	Status     string              `json:"status"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	Untouched  bool                `json:"untouched"`
	HasUpdates bool                `json:"hasUpdates"`
	Fields     []rolePackFieldView `json:"fields"`
}

// rolePackCheckView is the whole answer of profile.rolePack.check.
type rolePackCheckView struct {
	PackVersion      int                `json:"packVersion"`
	SeatsWithUpdates int                `json:"seatsWithUpdates"`
	Seats            []rolePackSeatView `json:"seats"`
}

// rolePackApplyResultView is one profile's outcome of profile.rolePack.apply.
type rolePackApplyResultView struct {
	Seat    string   `json:"seat"`
	ID      int64    `json:"id"`
	Fields  []string `json:"fields"`
	Outcome string   `json:"outcome"`
	Error   string   `json:"error,omitempty"`
}

// rolePackApplyView is the answer of profile.rolePack.apply.
type rolePackApplyView struct {
	BackupDir string                    `json:"backupDir"`
	DryRun    bool                      `json:"dryRun"`
	Results   []rolePackApplyResultView `json:"results"`
}

// buildRolePackView adapts the engine's check to the wire shape.
func buildRolePackView(checks []profiles.SeatSync) rolePackCheckView {
	v := rolePackCheckView{PackVersion: profiles.PackVersion, Seats: make([]rolePackSeatView, 0, len(checks))}
	for _, c := range checks {
		s := rolePackSeatView{
			Name:       c.Name,
			ID:         c.ID,
			Status:     c.Status,
			UpdatedAt:  c.UpdatedAt,
			Untouched:  c.Untouched(),
			HasUpdates: c.HasUpdates(),
			Fields:     []rolePackFieldView{},
		}
		for _, f := range c.Fields {
			fv := rolePackFieldView{Name: f.Name, Kind: f.Kind, State: f.State}
			if f.State != profiles.StateCurrent {
				fv.Diff = profiles.UnifiedDiff(f.Stored, f.Want)
			}
			if f.Kind == profiles.KindCapability {
				fv.Stored, fv.Want = f.Stored, f.Want
			}
			s.Fields = append(s.Fields, fv)
		}
		if s.HasUpdates {
			v.SeatsWithUpdates++
		}
		v.Seats = append(v.Seats, s)
	}
	return v
}

// handleProfileRolePackCheck reports every seat's field states against the
// current pack. Read-only: it writes nothing, not even a backup directory.
func handleProfileRolePackCheck(profReg *profiles.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		existing, err := profReg.List()
		if err != nil {
			return nil, fmt.Errorf("profile.rolePack.check: %w", err)
		}
		return buildRolePackView(rolePackCheck(existing)), nil
	}
}

// handleProfileRolePackApply writes the updates the check planned (the CLI's
// --apply over the same engine). A malformed take is refused, naming it,
// before anything is read or written.
func handleProfileRolePackApply(profReg *profiles.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Take      []string `json:"take"`
			DryRun    bool     `json:"dryRun"`
			BackupDir string   `json:"backupDir"`
		}
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("profile.rolePack.apply: invalid params: %w", err)
			}
		}
		takes := make([]profiles.Take, 0, len(p.Take))
		for _, t := range p.Take {
			take, err := profiles.ParseTake(t)
			if err != nil {
				return nil, fmt.Errorf("profile.rolePack.apply: invalid params: %w", err)
			}
			takes = append(takes, take)
		}
		existing, err := profReg.List()
		if err != nil {
			return nil, fmt.Errorf("profile.rolePack.apply: %w", err)
		}
		items := profiles.PlanApply(rolePackCheck(existing), existing, takes)
		backupDir := p.BackupDir
		if backupDir == "" {
			backupDir = rolePackBackupRoot()
		}
		update := func(next store.AgentProfile, expected time.Time) (store.AgentProfile, error) {
			return profReg.UpdateIf(next, expected)
		}
		dir, results, err := profiles.ApplyRoles(items, profiles.ApplyOptions{BackupDir: backupDir, Update: update, DryRun: p.DryRun})
		if err != nil {
			return nil, fmt.Errorf("profile.rolePack.apply: %w", err)
		}
		v := rolePackApplyView{BackupDir: dir, DryRun: p.DryRun, Results: make([]rolePackApplyResultView, 0, len(results))}
		for _, r := range results {
			rv := rolePackApplyResultView{Seat: r.Seat, ID: r.ID, Fields: r.Fields, Outcome: r.Outcome}
			if r.Err != nil {
				rv.Error = fmt.Sprint(r.Err)
			}
			v.Results = append(v.Results, rv)
		}
		return v, nil
	}
}
