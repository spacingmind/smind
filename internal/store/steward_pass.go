package store

import (
	"errors"
	"fmt"
	"time"
)

// The two review axes of ADR-0054 §2: each is one seat run of one review.
// The steward package aliases these; they are the only place the strings are
// written.
const (
	StewardAxisStandards = "standards"
	StewardAxisSpec      = "spec"
)

// The statuses of a pass, and the blocked_reason a skipped pass carries.
const (
	StewardPassPending = "pending"
	StewardPassRunning = "running"
	StewardPassDone    = "done"
	StewardPassBlocked = "blocked"
	StewardPassSkipped = "skipped"

	stewardPassNoSpec = "no_spec"
)

// stewardPassOpenCond is the WHERE clause of an open pass.
const stewardPassOpenCond = `status IN ('` + StewardPassPending + `','` + StewardPassRunning + `')`

// StewardReviewPass is one seat run of one steward review on one axis
// (ADR-0054 §2). SeatVerdict is write-once: it stays "" while the pass is
// open and is never rewritten once set.
type StewardReviewPass struct {
	ID            int64
	ReviewID      int64
	Axis          string
	Status        string
	Profile       string
	Model         string
	RunID         string
	StewardTaskID int64
	ScratchDir    string
	TokenHash     string
	TokenUsed     bool
	SeatVerdict   string
	Summary       string
	FindingsJSON  string
	CriteriaJSON  string
	BlockedReason string
	CostUSD       float64
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// ErrStewardPassClosed is returned by every write of an open pass (verdict,
// blocked, skipped, run, status, token) once the pass is finished: the row
// is immutable from then on, except the cost.
var ErrStewardPassClosed = errors.New("steward review pass is already finished")

const stewardPassCols = `id, review_id, axis, status, profile, model, run_id, steward_task_id, scratch_dir,
	token_hash, token_used, seat_verdict, summary, findings_json, criteria_json, blocked_reason, cost_usd,
	created_at, finished_at`

func scanStewardReviewPass(row rowScanner) (StewardReviewPass, error) {
	var p StewardReviewPass
	var tokenUsed int
	if err := row.Scan(&p.ID, &p.ReviewID, &p.Axis, &p.Status, &p.Profile, &p.Model, &p.RunID, &p.StewardTaskID, &p.ScratchDir,
		&p.TokenHash, &tokenUsed, &p.SeatVerdict, &p.Summary, &p.FindingsJSON, &p.CriteriaJSON, &p.BlockedReason, &p.CostUSD,
		&p.CreatedAt, &p.FinishedAt); err != nil {
		return StewardReviewPass{}, err
	}
	p.TokenUsed = tokenUsed != 0
	p.CreatedAt = p.CreatedAt.UTC()
	if p.FinishedAt != nil {
		f := p.FinishedAt.UTC()
		p.FinishedAt = &f
	}
	return p, nil
}

// InsertStewardReviewPass stores one pass (ID and CreatedAt are assigned)
// with the status the caller passes ("pending" or "running"). A second
// insert for the same (review, axis) fails on the UNIQUE constraint.
func (s *Store) InsertStewardReviewPass(p StewardReviewPass, now time.Time) (StewardReviewPass, error) {
	_, err := s.db.Exec(`INSERT INTO steward_review_passes
		(review_id, axis, status, profile, model, run_id, steward_task_id, scratch_dir, token_hash, token_used,
		seat_verdict, summary, findings_json, criteria_json, blocked_reason, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		p.ReviewID, p.Axis, p.Status, p.Profile, p.Model, p.RunID, p.StewardTaskID, p.ScratchDir, p.TokenHash,
		p.SeatVerdict, capUTF8(p.Summary, stewardSummaryMax), p.FindingsJSON, p.CriteriaJSON, p.BlockedReason, p.CostUSD, now.UTC())
	if err != nil {
		return StewardReviewPass{}, fmt.Errorf("insert steward review pass: %w", err)
	}
	return s.GetStewardReviewPass(p.ReviewID, p.Axis)
}

// GetStewardReviewPass returns the review's pass on one axis, or
// sql.ErrNoRows.
func (s *Store) GetStewardReviewPass(reviewID int64, axis string) (StewardReviewPass, error) {
	return scanStewardReviewPass(s.db.QueryRow(`SELECT `+stewardPassCols+` FROM steward_review_passes WHERE review_id = ? AND axis = ?`, reviewID, axis))
}

// ListStewardReviewPasses returns the review's passes, standards first then
// spec, oldest first; an empty slice when the review has none (it ran
// single-pass).
func (s *Store) ListStewardReviewPasses(reviewID int64) ([]StewardReviewPass, error) {
	rows, err := s.db.Query(`SELECT `+stewardPassCols+` FROM steward_review_passes
		WHERE review_id = ? ORDER BY CASE axis WHEN ? THEN 0 ELSE 1 END, id`, reviewID, StewardAxisStandards)
	if err != nil {
		return nil, fmt.Errorf("list steward review passes: %w", err)
	}
	defer rows.Close()
	out := []StewardReviewPass{}
	for rows.Next() {
		p, err := scanStewardReviewPass(rows)
		if err != nil {
			return nil, fmt.Errorf("list steward review passes: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// stewardPassOpen updates one pass while it is open (status pending or
// running); a finished row matches no rows and reads as closed.
func (s *Store) stewardPassOpen(q string, args ...any) error {
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardPassClosed
	}
	return nil
}

// SetStewardPassRun records the pass's steward run, its task and scratch
// directory once the seat has started it, while the pass is open.
func (s *Store) SetStewardPassRun(reviewID int64, axis, runID string, stewardTaskID int64, scratchDir string) error {
	if err := s.stewardPassOpen(`UPDATE steward_review_passes SET run_id = ?, steward_task_id = ?, scratch_dir = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+``,
		runID, stewardTaskID, scratchDir, reviewID, axis); err != nil {
		return fmt.Errorf("set steward review pass run: %w", err)
	}
	return nil
}

// SetStewardPassStatus moves an open pass between pending and running.
func (s *Store) SetStewardPassStatus(reviewID int64, axis, status string) error {
	if status != StewardPassPending && status != StewardPassRunning {
		return fmt.Errorf("set steward review pass status: unknown status %q", status)
	}
	if err := s.stewardPassOpen(`UPDATE steward_review_passes SET status = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+``, status, reviewID, axis); err != nil {
		return fmt.Errorf("set steward review pass status: %w", err)
	}
	return nil
}

// SetStewardPassToken records the sha256 of the pass's one-time verdict
// token, while the pass is open.
func (s *Store) SetStewardPassToken(reviewID int64, axis, tokenHash string) error {
	if err := s.stewardPassOpen(`UPDATE steward_review_passes SET token_hash = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+``, tokenHash, reviewID, axis); err != nil {
		return fmt.Errorf("set steward review pass token: %w", err)
	}
	return nil
}

// ConsumeStewardPassToken marks the pass's verdict token used. The WHERE on
// token_used makes it single use and atomic: a second call affects no rows
// and returns ErrStewardTokenUsed.
func (s *Store) ConsumeStewardPassToken(reviewID int64, axis string) error {
	res, err := s.db.Exec(`UPDATE steward_review_passes SET token_used = 1 WHERE review_id = ? AND axis = ? AND token_used = 0`, reviewID, axis)
	if err != nil {
		return fmt.Errorf("consume steward pass token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardTokenUsed
	}
	return nil
}

// SetStewardPassVerdict writes the one verdict the pass's seat submitted.
// Write-once; it finishes the pass (status done) and a second call returns
// ErrStewardPassClosed.
func (s *Store) SetStewardPassVerdict(reviewID int64, axis, seatVerdict, summary, findingsJSON, criteriaJSON string, costUSD float64, now time.Time) error {
	res, err := s.db.Exec(`UPDATE steward_review_passes SET seat_verdict = ?, summary = ?, findings_json = ?, criteria_json = ?,
		cost_usd = ?, status = '`+StewardPassDone+`', finished_at = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+` AND seat_verdict = ''`,
		seatVerdict, capUTF8(summary, stewardSummaryMax), findingsJSON, criteriaJSON, costUSD, now.UTC(), reviewID, axis)
	if err != nil {
		return fmt.Errorf("set steward review pass verdict: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardPassClosed
	}
	return nil
}

// SetStewardPassBlocked ends an open pass without a seat verdict: its run
// timed out, blew its budget, crashed, was cancelled or violated scope.
// Write-once like the verdict; a second call returns ErrStewardPassClosed.
func (s *Store) SetStewardPassBlocked(reviewID int64, axis, reason, summary string, costUSD float64, now time.Time) error {
	res, err := s.db.Exec(`UPDATE steward_review_passes SET blocked_reason = ?, summary = ?, cost_usd = ?,
		status = '`+StewardPassBlocked+`', finished_at = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+` AND seat_verdict = ''`,
		reason, capUTF8(summary, stewardSummaryMax), costUSD, now.UTC(), reviewID, axis)
	if err != nil {
		return fmt.Errorf("set steward review pass blocked: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardPassClosed
	}
	return nil
}

// SetStewardPassSkipped ends an open pass that never ran because no spec
// was resolvable (ADR-0054 §6): skipped, no cost, no seat verdict.
func (s *Store) SetStewardPassSkipped(reviewID int64, axis, summary string, now time.Time) error {
	res, err := s.db.Exec(`UPDATE steward_review_passes SET blocked_reason = ?, summary = ?, cost_usd = 0,
		status = ?, finished_at = ? WHERE review_id = ? AND axis = ? AND `+stewardPassOpenCond+` AND seat_verdict = ''`,
		stewardPassNoSpec, capUTF8(summary, stewardSummaryMax), StewardPassSkipped, now.UTC(), reviewID, axis)
	if err != nil {
		return fmt.Errorf("set steward review pass skipped: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardPassClosed
	}
	return nil
}

// RaiseStewardPassCost records the pass's seat run cost in any status: the
// verdict usually lands while the run is still going, and its usage row is
// only complete once the run ends. Cost only ever rises.
func (s *Store) RaiseStewardPassCost(reviewID int64, axis string, costUSD float64) error {
	if _, err := s.db.Exec(`UPDATE steward_review_passes SET cost_usd = ? WHERE review_id = ? AND axis = ? AND cost_usd < ?`, costUSD, reviewID, axis, costUSD); err != nil {
		return fmt.Errorf("raise steward pass cost: %w", err)
	}
	return nil
}
