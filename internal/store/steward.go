package store

import (
	"errors"
	"fmt"
	"time"
)

// StewardReview is one Steward review of {task, head commit} (ADR-0047 §8).
// Verdict is write-once: it stays "" while the review is open and is never
// rewritten once set.
type StewardReview struct {
	ID             int64
	TaskID         int64
	TriggerKind    string
	HeadSHA        string
	ContractSha256 string
	ValidationID   int64
	CIState        string
	DigestJSON     string
	DigestSha256   string
	PolicySha      string
	SeatVerdict    string
	Verdict        string
	FindingsJSON   string
	GatesJSON      string
	Summary        string
	RunID          string
	StewardTaskID  int64
	ScratchDir     string
	TokenHash      string
	TokenUsed      bool
	Status         string
	BlockedReason  string
	ScopeViolation bool
	CostUSD        float64
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

// ErrStewardReviewOpen is returned when a task already has a steward review
// in status pending or running (one open review at a time per task).
var ErrStewardReviewOpen = errors.New("task already has an open steward review")

// ErrStewardReviewClosed is returned by every write of an open review
// (verdict, blocked, run, digest, status, token, cost) once the review is
// finished: the row is immutable from then on, except the scope-violation
// flag.
var ErrStewardReviewClosed = errors.New("steward review is already finished")

// ErrStewardTokenUsed is returned by ConsumeStewardToken on its second call:
// the verdict token is single use.
var ErrStewardTokenUsed = errors.New("steward verdict token already used")

// stewardSummaryMax bounds the stored summary (bytes, rune safe); the digest
// and findings/gates JSON are kept as given.
const stewardSummaryMax = 800

const stewardReviewCols = `id, task_id, trigger_kind, head_sha, contract_sha256, validation_id, ci_state, digest_json,
	digest_sha256, policy_sha, seat_verdict, verdict, findings_json, gates_json, summary, run_id, steward_task_id,
	scratch_dir, token_hash, token_used, status, blocked_reason, scope_violation, cost_usd, created_at, finished_at`

func scanStewardReview(row rowScanner) (StewardReview, error) {
	var r StewardReview
	var tokenUsed, scope int
	if err := row.Scan(&r.ID, &r.TaskID, &r.TriggerKind, &r.HeadSHA, &r.ContractSha256, &r.ValidationID, &r.CIState, &r.DigestJSON,
		&r.DigestSha256, &r.PolicySha, &r.SeatVerdict, &r.Verdict, &r.FindingsJSON, &r.GatesJSON, &r.Summary, &r.RunID, &r.StewardTaskID,
		&r.ScratchDir, &r.TokenHash, &tokenUsed, &r.Status, &r.BlockedReason, &scope, &r.CostUSD, &r.CreatedAt, &r.FinishedAt); err != nil {
		return StewardReview{}, err
	}
	r.TokenUsed = tokenUsed != 0
	r.ScopeViolation = scope != 0
	r.CreatedAt = r.CreatedAt.UTC()
	if r.FinishedAt != nil {
		f := r.FinishedAt.UTC()
		r.FinishedAt = &f
	}
	return r, nil
}

// InsertStewardReview stores a review (ID and CreatedAt are assigned) with
// the status the caller passes ("pending" or "running"). It fails with
// ErrStewardReviewOpen when the task already has one in pending or running.
func (s *Store) InsertStewardReview(r StewardReview, now time.Time) (StewardReview, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return StewardReview{}, fmt.Errorf("insert steward review: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var open int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM steward_reviews WHERE task_id = ? AND status IN ('pending','running')`, r.TaskID).Scan(&open); err != nil {
		return StewardReview{}, fmt.Errorf("insert steward review: %w", err)
	}
	if open > 0 {
		return StewardReview{}, ErrStewardReviewOpen
	}
	res, err := tx.Exec(`INSERT INTO steward_reviews
		(task_id, trigger_kind, head_sha, contract_sha256, validation_id, ci_state, digest_json, digest_sha256, policy_sha,
		seat_verdict, verdict, findings_json, gates_json, summary, run_id, steward_task_id, scratch_dir, token_hash,
		token_used, status, blocked_reason, scope_violation, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, 0, ?, ?)`,
		r.TaskID, r.TriggerKind, r.HeadSHA, r.ContractSha256, r.ValidationID, r.CIState, r.DigestJSON, r.DigestSha256, r.PolicySha,
		r.SeatVerdict, r.Verdict, r.FindingsJSON, r.GatesJSON, capUTF8(r.Summary, stewardSummaryMax), r.RunID, r.StewardTaskID,
		r.ScratchDir, r.TokenHash, r.Status, r.BlockedReason, r.CostUSD, now.UTC())
	if err != nil {
		return StewardReview{}, fmt.Errorf("insert steward review: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return StewardReview{}, fmt.Errorf("insert steward review: %w", err)
	}
	return s.GetStewardReview(id)
}

// GetStewardReview returns review id, or sql.ErrNoRows.
func (s *Store) GetStewardReview(id int64) (StewardReview, error) {
	return scanStewardReview(s.db.QueryRow(`SELECT `+stewardReviewCols+` FROM steward_reviews WHERE id = ?`, id))
}

// ListStewardReviews returns reviews newest first. TaskID 0 lists every
// task's; limit defaults to 50 and is capped at 200.
func (s *Store) ListStewardReviews(taskID int64, limit int) ([]StewardReview, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + stewardReviewCols + ` FROM steward_reviews`
	var args []any
	if taskID != 0 {
		q += ` WHERE task_id = ?`
		args = append(args, taskID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list steward reviews: %w", err)
	}
	defer rows.Close()
	var out []StewardReview
	for rows.Next() {
		r, err := scanStewardReview(rows)
		if err != nil {
			return nil, fmt.Errorf("list steward reviews: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestStewardReview returns taskID's newest review, or sql.ErrNoRows.
func (s *Store) LatestStewardReview(taskID int64) (StewardReview, error) {
	return scanStewardReview(s.db.QueryRow(`SELECT `+stewardReviewCols+` FROM steward_reviews WHERE task_id = ? ORDER BY id DESC LIMIT 1`, taskID))
}

// FindStewardReviewByKey returns the newest review of
// {task, head, contract, CI state}, or sql.ErrNoRows. It is the dedupe
// lookup, and only an open or finished review counts; a blocked one
// (timeout, budget, crashed, cancelled) never blocks a retry of the same
// head.
func (s *Store) FindStewardReviewByKey(taskID int64, headSHA, contractSha, ciState string) (StewardReview, error) {
	return scanStewardReview(s.db.QueryRow(`SELECT `+stewardReviewCols+` FROM steward_reviews
		WHERE task_id = ? AND head_sha = ? AND contract_sha256 = ? AND ci_state = ? AND status IN ('pending','running','done')
		ORDER BY id DESC LIMIT 1`, taskID, headSHA, contractSha, ciState))
}

// stewardOpen updates review id while it is open (status pending or
// running); a finished row matches no rows and reads as closed.
func (s *Store) stewardOpen(q string, args ...any) error {
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardReviewClosed
	}
	return nil
}

// SetStewardReviewRun records the steward run, its task and scratch
// directory once the seat has started it, while the review is open.
func (s *Store) SetStewardReviewRun(id int64, runID string, stewardTaskID int64, scratchDir string) error {
	if err := s.stewardOpen(`UPDATE steward_reviews SET run_id = ?, steward_task_id = ?, scratch_dir = ? WHERE id = ? AND status IN ('pending','running')`,
		runID, stewardTaskID, scratchDir, id); err != nil {
		return fmt.Errorf("set steward review run: %w", err)
	}
	return nil
}

// SetStewardReviewDigest records what the review judged: digest, CI state,
// policy sha, contract sha and validation id, while the review is open.
func (s *Store) SetStewardReviewDigest(id int64, digestJSON, digestSha, ciState, policySha, contractSha string, validationID int64) error {
	if err := s.stewardOpen(`UPDATE steward_reviews SET digest_json = ?, digest_sha256 = ?, ci_state = ?, policy_sha = ?, contract_sha256 = ?, validation_id = ? WHERE id = ? AND status IN ('pending','running')`,
		digestJSON, digestSha, ciState, policySha, contractSha, validationID, id); err != nil {
		return fmt.Errorf("set steward review digest: %w", err)
	}
	return nil
}

// SetStewardReviewStatus moves an open review between pending and running.
func (s *Store) SetStewardReviewStatus(id int64, status string) error {
	if status != "pending" && status != "running" {
		return fmt.Errorf("set steward review status: unknown status %q", status)
	}
	if err := s.stewardOpen(`UPDATE steward_reviews SET status = ? WHERE id = ? AND status IN ('pending','running')`, status, id); err != nil {
		return fmt.Errorf("set steward review status: %w", err)
	}
	return nil
}

// SetStewardReviewToken records the sha256 of the one-time verdict token,
// while the review is open.
func (s *Store) SetStewardReviewToken(id int64, tokenHash string) error {
	if err := s.stewardOpen(`UPDATE steward_reviews SET token_hash = ? WHERE id = ? AND status IN ('pending','running')`, tokenHash, id); err != nil {
		return fmt.Errorf("set steward review token: %w", err)
	}
	return nil
}

// ConsumeStewardToken marks the verdict token used. The WHERE on token_used
// makes it single use and atomic: a second call affects no rows and returns
// ErrStewardTokenUsed.
func (s *Store) ConsumeStewardToken(id int64) error {
	res, err := s.db.Exec(`UPDATE steward_reviews SET token_used = 1 WHERE id = ? AND token_used = 0`, id)
	if err != nil {
		return fmt.Errorf("consume steward token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardTokenUsed
	}
	return nil
}

// SetStewardReviewVerdict writes the one verdict of review id: seat verdict
// as submitted, verdict as clamped and stored. Write-once; it closes the
// review (status done) and a second call returns ErrStewardReviewClosed.
func (s *Store) SetStewardReviewVerdict(id int64, seatVerdict, verdict, findingsJSON, gatesJSON, summary string, costUSD float64, now time.Time) error {
	res, err := s.db.Exec(`UPDATE steward_reviews SET seat_verdict = ?, verdict = ?, findings_json = ?, gates_json = ?, summary = ?,
		cost_usd = ?, status = 'done', finished_at = ? WHERE id = ? AND status IN ('pending','running') AND verdict = ''`,
		seatVerdict, verdict, findingsJSON, gatesJSON, capUTF8(summary, stewardSummaryMax), costUSD, now.UTC(), id)
	if err != nil {
		return fmt.Errorf("set steward review verdict: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardReviewClosed
	}
	return nil
}

// SetStewardReviewBlocked ends an open review without a seat verdict: the
// run timed out, blew its budget, crashed, was cancelled or violated scope.
// Write-once like the verdict; a second call returns ErrStewardReviewClosed.
func (s *Store) SetStewardReviewBlocked(id int64, reason, summary string, costUSD float64, now time.Time) error {
	res, err := s.db.Exec(`UPDATE steward_reviews SET verdict = 'BLOCKED', blocked_reason = ?, summary = ?, cost_usd = ?,
		status = 'blocked', finished_at = ? WHERE id = ? AND status IN ('pending','running') AND verdict = ''`,
		reason, capUTF8(summary, stewardSummaryMax), costUSD, now.UTC(), id)
	if err != nil {
		return fmt.Errorf("set steward review blocked: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStewardReviewClosed
	}
	return nil
}

// SetStewardReviewScopeViolation flags review id as having touched outside
// its scratch scope. Allowed after the review finished: the post-check runs
// once the steward's run ends.
func (s *Store) SetStewardReviewScopeViolation(id int64) error {
	if _, err := s.db.Exec(`UPDATE steward_reviews SET scope_violation = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("flag steward review scope violation: %w", err)
	}
	return nil
}

// SetStewardReviewCost records the accumulated cost so far, while the review
// is open.
func (s *Store) SetStewardReviewCost(id int64, costUSD float64) error {
	if err := s.stewardOpen(`UPDATE steward_reviews SET cost_usd = ? WHERE id = ? AND status IN ('pending','running')`, costUSD, id); err != nil {
		return fmt.Errorf("set steward review cost: %w", err)
	}
	return nil
}

// RaiseStewardReviewCost records the seat run's cost on review id in any
// status: the verdict usually lands while the run is still going, and its
// usage row is only complete once the run ends. Cost only ever rises.
func (s *Store) RaiseStewardReviewCost(id int64, costUSD float64) error {
	if _, err := s.db.Exec(`UPDATE steward_reviews SET cost_usd = ? WHERE id = ? AND cost_usd < ?`, costUSD, id, costUSD); err != nil {
		return fmt.Errorf("raise steward review cost: %w", err)
	}
	return nil
}

// CountStewardReviewsSince counts non-cancelled reviews created at or after
// since (the per-hour budget check).
func (s *Store) CountStewardReviewsSince(since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM steward_reviews WHERE created_at >= ? AND blocked_reason != 'cancelled'`, since.UTC()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count steward reviews since: %w", err)
	}
	return n, nil
}

// CountStewardReviewsForTask counts taskID's non-cancelled reviews (the
// per-task cap check).
func (s *Store) CountStewardReviewsForTask(taskID int64) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM steward_reviews WHERE task_id = ? AND blocked_reason != 'cancelled'`, taskID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count steward reviews for task: %w", err)
	}
	return n, nil
}

// ConsecutiveStewardFailures counts the crash/timeout run of blocked reviews
// ending at the newest row: it walks rows newest first, skipping open
// (pending/running) and cancelled rows, counting status blocked with reason
// crashed or timeout, and stops at the first row that is neither (a done
// review, or a blocked one for another reason).
func (s *Store) ConsecutiveStewardFailures() (int, error) {
	rows, err := s.db.Query(`SELECT status, blocked_reason FROM steward_reviews ORDER BY id DESC`)
	if err != nil {
		return 0, fmt.Errorf("consecutive steward failures: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var status, reason string
		if err := rows.Scan(&status, &reason); err != nil {
			return 0, fmt.Errorf("consecutive steward failures: %w", err)
		}
		if status == "pending" || status == "running" || reason == "cancelled" {
			continue
		}
		if status == "blocked" && (reason == "crashed" || reason == "timeout") {
			n++
			continue
		}
		break
	}
	return n, rows.Err()
}

// ListStaleOpenStewardReviews returns every open review, for the daemon to
// resume or block them after a restart.
func (s *Store) ListStaleOpenStewardReviews() ([]StewardReview, error) {
	rows, err := s.db.Query(`SELECT ` + stewardReviewCols + ` FROM steward_reviews WHERE status IN ('pending','running') ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list stale open steward reviews: %w", err)
	}
	defer rows.Close()
	var out []StewardReview
	for rows.Next() {
		r, err := scanStewardReview(rows)
		if err != nil {
			return nil, fmt.Errorf("list stale open steward reviews: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
