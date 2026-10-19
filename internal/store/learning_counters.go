package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Load channels recorded by RecordLoad.
const (
	LoadViaSkill = "skill" // the Skill tool
	LoadViaRead  = "read"  // a Read of the projected SKILL.md
)

// RecordOffered counts one offer of each skill to the run (ADR-0026 section
// 6): offered_runs+1 and last_offered_at only the first time a (skill, run)
// pair is seen, so a re-delivered notification for the same run is a no-op.
// measured is sticky-OR: a skill becomes measured the first time it is
// offered on a provider that exposes a load signal, and never flips back.
func (s *Store) RecordOffered(runID string, skillIDs []int64, measured bool) error {
	if runID == "" || len(skillIDs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("record offered for run %s: begin: %w", runID, err)
	}
	defer tx.Rollback()
	m := 0
	if measured {
		m = 1
	}
	for _, id := range skillIDs {
		res, err := tx.Exec(`INSERT OR IGNORE INTO learned_skill_offers (skill_id, run_id, created_at)
			SELECT id, ?, ? FROM learned_skills WHERE id = ?`, runID, now, id)
		if err != nil {
			return fmt.Errorf("record offered for run %s: %w", runID, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if _, err := tx.Exec(`UPDATE learned_skills SET offered_runs = offered_runs + 1, last_offered_at = ?,
			measured = MAX(measured, ?) WHERE id = ?`, now, m, id); err != nil {
			return fmt.Errorf("record offered for run %s: %w", runID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record offered for run %s: commit: %w", runID, err)
	}
	return nil
}

// RecordLoad counts a load of the skill by the run through via ("skill" or
// "read"). It is idempotent per (skill, run, via): loads and loads_via_*
// count distinct (run, via) loads, NOT raw tool calls, so an agent that
// re-reads a skill ten times in one run counts once. loaded_runs counts
// once per run (the skill's first load row for that run). A load for a
// skill the run was not offered is ignored: a run can only load what it was
// offered. reused_after_patch increments on the first load after the
// skill's latest patch.
func (s *Store) RecordLoad(runID string, skillID int64, via string) error {
	if via != LoadViaSkill && via != LoadViaRead {
		return fmt.Errorf("record load for run %s: unknown via %q", runID, via)
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("record load for run %s: begin: %w", runID, err)
	}
	defer tx.Rollback()

	var offered int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM learned_skill_offers WHERE skill_id = ? AND run_id = ?`, skillID, runID).Scan(&offered); err != nil {
		return fmt.Errorf("record load for run %s: %w", runID, err)
	}
	if offered == 0 {
		return nil
	}
	var prior int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM learned_skill_loads WHERE skill_id = ? AND run_id = ?`, skillID, runID).Scan(&prior); err != nil {
		return fmt.Errorf("record load for run %s: %w", runID, err)
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO learned_skill_loads (skill_id, run_id, via, created_at) VALUES (?, ?, ?, ?)`, skillID, runID, via, now)
	if err != nil {
		return fmt.Errorf("record load for run %s: %w", runID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}

	var patched int
	if err := tx.QueryRow(`SELECT patched FROM learned_skills WHERE id = ?`, skillID).Scan(&patched); err != nil {
		return fmt.Errorf("record load for run %s: %w", runID, err)
	}
	reused := 0
	if patched > 0 && prior == 0 {
		// First load of this run; it counts as a reuse only when no other
		// load (of any run) already happened since the latest patch.
		var patchedAt time.Time
		if err := tx.QueryRow(`SELECT created_at FROM learned_skill_versions WHERE skill_id = ? ORDER BY version DESC LIMIT 1`, skillID).Scan(&patchedAt); err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("record load for run %s: %w", runID, err)
		} else if err == nil {
			rows, err := tx.Query(`SELECT created_at FROM learned_skill_loads WHERE skill_id = ? AND NOT (run_id = ? AND via = ?)`, skillID, runID, via)
			if err != nil {
				return fmt.Errorf("record load for run %s: %w", runID, err)
			}
			since := false
			for rows.Next() {
				var at time.Time
				if err := rows.Scan(&at); err != nil {
					rows.Close()
					return fmt.Errorf("record load for run %s: %w", runID, err)
				}
				if at.After(patchedAt) {
					since = true
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return fmt.Errorf("record load for run %s: %w", runID, err)
			}
			if !since && !now.Before(patchedAt) {
				reused = 1
			}
		}
	}
	viaSkill, viaRead := 0, 0
	if via == LoadViaSkill {
		viaSkill = 1
	} else {
		viaRead = 1
	}
	firstForRun := 0
	if prior == 0 {
		firstForRun = 1
	}
	if _, err := tx.Exec(`UPDATE learned_skills SET loads = loads + 1, loads_via_skill = loads_via_skill + ?,
		loads_via_read = loads_via_read + ?, loaded_runs = loaded_runs + ?, reused_after_patch = reused_after_patch + ?,
		last_loaded_at = ? WHERE id = ?`, viaSkill, viaRead, firstForRun, reused, now, skillID); err != nil {
		return fmt.Errorf("record load for run %s: %w", runID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record load for run %s: commit: %w", runID, err)
	}
	return nil
}
