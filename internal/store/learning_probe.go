package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LearningProbe is one A/A2/B experiment run before a skill proposal is
// approved (ADR-0037). Contrast is JSON the store never interprets.
type LearningProbe struct {
	ID            int64
	WorkspaceID   int64
	ProposalID    int64
	Quick         bool
	Status        string
	Verdict       string
	VerdictReason string
	EstUSD        float64
	CanaryCostUSD float64
	SourceRunID   string
	BaseSHA       string
	Contrast      string
	Error         string
	StartedAt     time.Time
	FinishedAt    *time.Time
}

// LearningProbeArm is one arm (A, A2 or B) of a probe. Metrics, diff,
// final message and check result are stored uninterpreted (redacted and
// clipped by the caller).
type LearningProbeArm struct {
	ProbeID        int64
	Role           string
	Seq            int
	Status         string
	RunID          string
	ProjectionHash string
	Metrics        string
	Diff           string
	FinalMessage   string
	CheckResult    string
	CostUSD        float64
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

const probeColumns = `id, workspace_id, proposal_id, quick, status, verdict, verdict_reason,
	est_usd, canary_cost_usd, source_run_id, base_sha, contrast, error, started_at, finished_at`

func scanProbe(row interface{ Scan(...any) error }) (LearningProbe, error) {
	var p LearningProbe
	var quick int
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.ProposalID, &quick, &p.Status, &p.Verdict, &p.VerdictReason,
		&p.EstUSD, &p.CanaryCostUSD, &p.SourceRunID, &p.BaseSHA, &p.Contrast, &p.Error, &p.StartedAt, &p.FinishedAt)
	p.Quick = quick != 0
	return p, err
}

// CreateProbe inserts a probe row as given (caller sets status).
func (s *Store) CreateProbe(p LearningProbe) (LearningProbe, error) {
	if p.StartedAt.IsZero() {
		p.StartedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO learning_probes
		(workspace_id, proposal_id, quick, status, verdict, verdict_reason,
		 est_usd, canary_cost_usd, source_run_id, base_sha, contrast, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.WorkspaceID, p.ProposalID, b2i(p.Quick), p.Status, p.Verdict, p.VerdictReason,
		p.EstUSD, p.CanaryCostUSD, p.SourceRunID, p.BaseSHA, p.Contrast, p.Error, p.StartedAt, p.FinishedAt)
	if err != nil {
		return LearningProbe{}, fmt.Errorf("create learning probe: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearningProbe{}, fmt.Errorf("create learning probe: id: %w", err)
	}
	p.ID = id
	return p, nil
}

// UpdateProbe replaces the mutable fields of probe p.ID: status, verdict,
// verdict_reason, canary_cost_usd, contrast, error and finished_at.
func (s *Store) UpdateProbe(p LearningProbe) error {
	res, err := s.db.Exec(`UPDATE learning_probes SET
		status = ?, verdict = ?, verdict_reason = ?, canary_cost_usd = ?, contrast = ?, error = ?, finished_at = ?
		WHERE id = ?`,
		p.Status, p.Verdict, p.VerdictReason, p.CanaryCostUSD, p.Contrast, p.Error, p.FinishedAt, p.ID)
	if err != nil {
		return fmt.Errorf("update learning probe %d: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update learning probe %d: %w", p.ID, sql.ErrNoRows)
	}
	return nil
}

// GetProbe returns the probe with the given id.
func (s *Store) GetProbe(id int64) (LearningProbe, error) {
	p, err := scanProbe(s.db.QueryRow(`SELECT `+probeColumns+` FROM learning_probes WHERE id = ?`, id))
	if err != nil {
		return LearningProbe{}, fmt.Errorf("get learning probe %d: %w", id, err)
	}
	return p, nil
}

// LatestProbeForProposal returns the proposal's newest probe, if any.
func (s *Store) LatestProbeForProposal(proposalID int64) (LearningProbe, bool, error) {
	p, err := scanProbe(s.db.QueryRow(
		`SELECT `+probeColumns+` FROM learning_probes WHERE proposal_id = ? ORDER BY id DESC LIMIT 1`, proposalID))
	if errors.Is(err, sql.ErrNoRows) {
		return LearningProbe{}, false, nil
	}
	if err != nil {
		return LearningProbe{}, false, fmt.Errorf("latest learning probe for proposal %d: %w", proposalID, err)
	}
	return p, true, nil
}

// ListProbes returns the workspace's probes, newest first, up to limit
// (limit <= 0 means 20).
func (s *Store) ListProbes(workspaceID int64, limit int) ([]LearningProbe, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT `+probeColumns+`
		FROM learning_probes WHERE workspace_id = ? ORDER BY id DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list learning probes for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	probes := make([]LearningProbe, 0)
	for rows.Next() {
		p, err := scanProbe(rows)
		if err != nil {
			return nil, fmt.Errorf("scan learning probe: %w", err)
		}
		probes = append(probes, p)
	}
	return probes, rows.Err()
}

// UpsertProbeArm inserts or replaces the arm row of (probe_id, role).
func (s *Store) UpsertProbeArm(a LearningProbeArm) error {
	_, err := s.db.Exec(`INSERT INTO learning_probe_arms
		(probe_id, role, seq, status, run_id, projection_hash, metrics, diff, final_message, check_result, cost_usd, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(probe_id, role) DO UPDATE SET
			seq = excluded.seq, status = excluded.status, run_id = excluded.run_id,
			projection_hash = excluded.projection_hash, metrics = excluded.metrics, diff = excluded.diff,
			final_message = excluded.final_message, check_result = excluded.check_result,
			cost_usd = excluded.cost_usd, started_at = excluded.started_at, finished_at = excluded.finished_at`,
		a.ProbeID, a.Role, a.Seq, a.Status, a.RunID, a.ProjectionHash, orEmptyJSON(a.Metrics), a.Diff,
		a.FinalMessage, a.CheckResult, a.CostUSD, a.StartedAt, a.FinishedAt)
	if err != nil {
		return fmt.Errorf("upsert learning probe arm %d/%s: %w", a.ProbeID, a.Role, err)
	}
	return nil
}

// ListProbeArms returns the probe's arms ordered by execution order (seq).
func (s *Store) ListProbeArms(probeID int64) ([]LearningProbeArm, error) {
	rows, err := s.db.Query(`SELECT probe_id, role, seq, status, run_id, projection_hash, metrics, diff,
		final_message, check_result, cost_usd, started_at, finished_at
		FROM learning_probe_arms WHERE probe_id = ? ORDER BY seq`, probeID)
	if err != nil {
		return nil, fmt.Errorf("list learning probe arms of probe %d: %w", probeID, err)
	}
	defer rows.Close()

	arms := make([]LearningProbeArm, 0)
	for rows.Next() {
		var a LearningProbeArm
		if err := rows.Scan(&a.ProbeID, &a.Role, &a.Seq, &a.Status, &a.RunID, &a.ProjectionHash, &a.Metrics,
			&a.Diff, &a.FinalMessage, &a.CheckResult, &a.CostUSD, &a.StartedAt, &a.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan learning probe arm: %w", err)
		}
		arms = append(arms, a)
	}
	return arms, rows.Err()
}

// ActiveProbe reports any queued or running probe: at most one probe may
// be in flight per daemon (ADR-0037).
func (s *Store) ActiveProbe() (LearningProbe, bool, error) {
	p, err := scanProbe(s.db.QueryRow(
		`SELECT ` + probeColumns + ` FROM learning_probes WHERE status IN ('queued','running') ORDER BY id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return LearningProbe{}, false, nil
	}
	if err != nil {
		return LearningProbe{}, false, fmt.Errorf("active learning probe: %w", err)
	}
	return p, true, nil
}

// FailInterruptedProbes marks every queued or running probe failed with
// error=reason and a finish time, and marks their non-terminal arms
// 'killed'; called once at daemon start, when no probe can still be
// running. Returns the number of probes changed.
func (s *Store) FailInterruptedProbes(reason string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("fail interrupted learning probes: begin: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	res, err := tx.Exec(
		`UPDATE learning_probes SET status = 'failed', error = ?, finished_at = ? WHERE status IN ('queued', 'running')`,
		reason, now)
	if err != nil {
		return 0, fmt.Errorf("fail interrupted learning probes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("fail interrupted learning probes: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE learning_probe_arms SET status = 'killed', finished_at = ?
		 WHERE status IN ('pending', 'running') AND probe_id IN
		 	(SELECT id FROM learning_probes WHERE finished_at = ? AND status = 'failed' AND error = ?)`,
		now, now, reason); err != nil {
		return 0, fmt.Errorf("fail interrupted learning probe arms: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("fail interrupted learning probes: commit: %w", err)
	}
	return int(n), nil
}

// SumProbeUsageForWorkspace totals the workspace's probe runs started at or
// after since (whatever their final status) plus the probes' canary spend;
// unreportedRuns counts runs the budget cannot see a cost figure for.
func (s *Store) SumProbeUsageForWorkspace(workspaceID int64, since time.Time) (costUSD float64, unreportedRuns int, err error) {
	err = s.db.QueryRow(
		`SELECT COALESCE(SUM(u.cost_usd), 0)
			+ COALESCE((SELECT SUM(canary_cost_usd) FROM learning_probes WHERE workspace_id = ? AND started_at >= ?), 0),
			COALESCE(SUM(CASE WHEN u.run_id IS NULL OR u.cost_usd IS NULL THEN 1 ELSE 0 END), 0)
		 FROM runs r JOIN tasks t ON t.id = r.task_id LEFT JOIN run_usage u ON u.run_id = r.id
		 WHERE t.workspace_id = ? AND r.purpose = 'probe' AND r.started_at >= ?`,
		workspaceID, since.UTC(), workspaceID, since.UTC(),
	).Scan(&costUSD, &unreportedRuns)
	if err != nil {
		err = fmt.Errorf("sum probe usage for workspace %d: %w", workspaceID, err)
	}
	return
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PurgeDecidedProbes deletes every probe whose proposal is decided --
// learned_skill_proposals.status != 'pending' with decided_at < cutoff;
// probes of a missing proposal row also count as decided -- together with
// their arm rows and their runs (+ run_events, run_usage), in one
// transaction. A pending proposal's probe is never purged. Returns the
// number of probes deleted and the ids of the runs deleted.
func (s *Store) PurgeDecidedProbes(cutoff time.Time) (probes int, runIDs []string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("purge decided probes: begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT p.id FROM learning_probes p
		LEFT JOIN learned_skill_proposals pr ON pr.id = p.proposal_id
		WHERE (pr.id IS NULL OR (pr.status != 'pending' AND pr.decided_at < ?))`, cutoff.UTC())
	if err != nil {
		return 0, nil, fmt.Errorf("purge decided probes: select: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, nil, fmt.Errorf("purge decided probes: scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("purge decided probes: %w", err)
	}

	for _, id := range ids {
		armRows, err := tx.Query(`SELECT run_id FROM learning_probe_arms WHERE probe_id = ? AND run_id != ''`, id)
		if err != nil {
			return 0, nil, fmt.Errorf("purge decided probes: arms of probe %d: %w", id, err)
		}
		var runs []string
		for armRows.Next() {
			var r string
			if err := armRows.Scan(&r); err != nil {
				armRows.Close()
				return 0, nil, fmt.Errorf("purge decided probes: scan arm: %w", err)
			}
			runs = append(runs, r)
		}
		armRows.Close()

		for _, r := range runs {
			for _, q := range []string{
				`DELETE FROM run_events WHERE run_id = ?`,
				`DELETE FROM run_usage WHERE run_id = ?`,
				`DELETE FROM runs WHERE id = ?`,
			} {
				if _, err := tx.Exec(q, r); err != nil {
					return 0, nil, fmt.Errorf("purge decided probes: delete run %s: %w", r, err)
				}
			}
			runIDs = append(runIDs, r)
		}
		if _, err := tx.Exec(`DELETE FROM learning_probe_arms WHERE probe_id = ?`, id); err != nil {
			return 0, nil, fmt.Errorf("purge decided probes: arms of probe %d: %w", id, err)
		}
		if _, err := tx.Exec(`DELETE FROM learning_probes WHERE id = ?`, id); err != nil {
			return 0, nil, fmt.Errorf("purge decided probes: probe %d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("purge decided probes: commit: %w", err)
	}
	return len(ids), runIDs, nil
}
