package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CreateRun inserts a new run row exactly as given -- unlike CreateTask,
// this does not stamp StartedAt itself: the caller (internal/runs.Registry)
// already has its own precise start time for the in-memory run this row
// mirrors, and the two must agree.
func (s *Store) CreateRun(r Run) (Run, error) {
	// A zero ChatID is stored as NULL, not the literal (nonexistent) chat id
	// 0 -- runs.chat_id's FK would reject that outright, and a caller that
	// predates chats entirely (or a test simulating one) has no chat to
	// name. See Run.ChatID's doc comment.
	if r.Purpose == "" {
		r.Purpose = RunPurposeUser
	}
	var chatID sql.NullInt64
	if r.ChatID != 0 {
		chatID = sql.NullInt64{Int64: r.ChatID, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT INTO runs (id, task_id, chat_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg, permission_mode, auto_accept, approval_policy, model, purpose, origin, interrupt, attachments, reply_chat_id, profile_id, failover_of)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, chatID, r.Provider, r.Prompt, r.Status, r.StartedAt,
		timePtrToNull(r.FinishedAt), r.StopReason, r.ErrMsg, r.PermissionMode, r.AutoAccept, nullString(r.Model), r.Purpose, r.Origin, r.Interrupt, r.Attachments, nullID(r.ReplyChatID), nullID(r.ProfileID), r.FailoverOf,
	)
	if err != nil {
		return Run{}, fmt.Errorf("insert run: %w", err)
	}
	return r, nil
}

// ClaimRunParentNotified atomically marks a run's parent notification as
// claimed at the given time. It reports true only for the call that set the
// marker; later calls (and runs already claimed) report false. This is the
// idempotency gate of ADR-0041 B3.
func (s *Store) ClaimRunParentNotified(id string, at time.Time) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE runs SET parent_notified_at = ? WHERE id = ? AND parent_notified_at IS NULL`,
		at, id,
	)
	if err != nil {
		return false, fmt.Errorf("claim run %q parent notified: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim run %q parent notified: %w", id, err)
	}
	return n == 1, nil
}

// ListUnnotifiedChildRuns returns finished (non-running) runs that carry a
// reply chat, have not yet been claimed as notified, and finished (or, when
// unfinished, started) after since, oldest first.
func (s *Store) ListUnnotifiedChildRuns(since time.Time) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT `+runColumns+`
		 FROM runs
		 WHERE reply_chat_id IS NOT NULL AND parent_notified_at IS NULL
		   AND status != 'running' AND COALESCE(finished_at, started_at) > ?
		 ORDER BY started_at, id`, since,
	)
	if err != nil {
		return nil, fmt.Errorf("list unnotified child runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRunStatus sets a run's terminal (or reconciled) status and returns
// the updated row.
func (s *Store) UpdateRunStatus(id, status string, finishedAt *time.Time, stopReason, errMsg string) (Run, error) {
	_, err := s.db.Exec(
		`UPDATE runs SET status = ?, finished_at = ?, stop_reason = ?, err_msg = ? WHERE id = ?`,
		status, timePtrToNull(finishedAt), stopReason, errMsg, id,
	)
	if err != nil {
		return Run{}, fmt.Errorf("update run %q status: %w", id, err)
	}
	return s.GetRun(id)
}

// UpdateRunPermission persists a run's live permission settings after a
// mid-run switch (internal/runs.Registry.SetPermissionMode /
// SetAutoAccept, ADR-0019) so the row a daemon restart rehydrates from
// reflects the mode the run ended in, not the one it started in.
func (s *Store) UpdateRunPermission(id, permissionMode string, autoAccept bool) error {
	_, err := s.db.Exec(
		`UPDATE runs SET permission_mode = ?, auto_accept = ? WHERE id = ?`,
		permissionMode, autoAccept, id,
	)
	if err != nil {
		return fmt.Errorf("update run %q permission: %w", id, err)
	}
	return nil
}

// RunProvenance is the git state of a run's worktree at a capture point
// (O1): BaseSHA/HeadSHA are the full commit SHAs at the start and at the
// terminal state, Dirty the worktree's dirty state at the latest
// successful capture. Every field is nil ("unknown") rather than a zero
// value when git couldn't be read.
type RunProvenance struct {
	BaseSHA *string
	HeadSHA *string
	Dirty   *bool
}

// UpdateRunProvenance persists a run's provenance capture (O1). Only the
// non-nil fields are written, so a start-time capture (base_sha + dirty)
// and a later terminal-state capture (head_sha + dirty) each leave the
// other's columns alone: COALESCE keeps the stored value when p's field is
// nil. A run id that doesn't exist is an error, not a silent no-op.
func (s *Store) UpdateRunProvenance(id string, p RunProvenance) error {
	var dirty any
	if p.Dirty != nil {
		dirty = *p.Dirty
	}
	res, err := s.db.Exec(
		`UPDATE runs SET
			base_sha = COALESCE(?, base_sha),
			head_sha = COALESCE(?, head_sha),
			dirty = COALESCE(?, dirty)
		 WHERE id = ?`,
		p.BaseSHA, p.HeadSHA, dirty, id,
	)
	if err != nil {
		return fmt.Errorf("update run %q provenance: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update run %q provenance: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("update run %q provenance: no such run", id)
	}
	return nil
}

// MarkRunningRunsInterrupted transitions every run row still status =
// "running" to "interrupted" -- called once at daemon startup, before any
// new Run exists, since a "running" row surviving to a fresh process start
// means the subprocess that was driving it is definitely gone (see
// internal/runs.Registry.CloseAll's doc comment on why nothing else
// guarantees that subprocess died with the old daemon process). stopReason,
// when non-nil, picks each row's stop_reason from its chat id (so the run
// says why it ended instead of leaving the column empty). Returns the number
// of rows transitioned.
func (s *Store) MarkRunningRunsInterrupted(interruptedStatus string, stopReason func(chatID int64) string) (int64, error) {
	rows, err := s.db.Query(`SELECT id, chat_id FROM runs WHERE status = 'running'`)
	if err != nil {
		return 0, fmt.Errorf("mark running runs interrupted: %w", err)
	}
	type pending struct {
		id     string
		chatID sql.NullInt64
	}
	var running []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.chatID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("mark running runs interrupted: scan: %w", err)
		}
		running = append(running, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("mark running runs interrupted: %w", err)
	}
	_ = rows.Close()

	var n int64
	for _, p := range running {
		reason := ""
		if stopReason != nil {
			reason = stopReason(p.chatID.Int64)
		}
		res, err := s.db.Exec(
			`UPDATE runs SET status = ?, stop_reason = CASE WHEN stop_reason = '' THEN ? ELSE stop_reason END WHERE id = ? AND status = 'running'`,
			interruptedStatus, reason, p.id)
		if err != nil {
			return n, fmt.Errorf("mark running runs interrupted: %w", err)
		}
		c, err := res.RowsAffected()
		if err != nil {
			return n, fmt.Errorf("mark running runs interrupted: rows affected: %w", err)
		}
		n += c
	}
	return n, nil
}

// UpdateRunFailure records (or, with kind "" and nil resetAt, clears) the
// typed terminal failure of a run (ADR-0041). resetAt is stored as UTC.
func (s *Store) UpdateRunFailure(id, kind, window string, resetAt *time.Time) error {
	if resetAt != nil {
		u := resetAt.UTC()
		resetAt = &u
	}
	if _, err := s.db.Exec(
		`UPDATE runs SET failure_kind = ?, failure_window = ?, failure_reset_at = ? WHERE id = ?`,
		kind, window, timePtrToNull(resetAt), id); err != nil {
		return fmt.Errorf("update run %q failure: %w", id, err)
	}
	return nil
}

// GetRun returns the run with the given id.
func (s *Store) GetRun(id string) (Run, error) {
	row := s.db.QueryRow(
		`SELECT `+runColumns+`
		 FROM runs WHERE id = ?`, id,
	)
	r, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("get run %q: %w", id, err)
	}
	return r, nil
}

// ListRecentRuns returns up to limit user runs (purpose 'user'), most
// recently started first -- used to rehydrate internal/runs.Registry's
// in-memory map at startup. System runs are excluded so they never occupy
// the retention cap; see ListRunsByPurpose.
func (s *Store) ListRecentRuns(limit int) ([]Run, error) {
	return s.listRuns(`WHERE purpose = 'user'`, limit)
}

// ListRunsByPurpose returns up to limit runs with the given purpose, most
// recently started first.
func (s *Store) ListRunsByPurpose(purpose string, limit int) ([]Run, error) {
	return s.listRuns(`WHERE purpose = ?`, limit, purpose)
}

func (s *Store) listRuns(where string, limit int, args ...any) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT `+runColumns+`
		 FROM runs `+where+` ORDER BY started_at DESC LIMIT ?`, append(args, limit)...,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// AppendRunEvent inserts one run_events row, stamping created_at.
func (s *Store) AppendRunEvent(runID string, seq int64, eventData string) (RunEvent, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO run_events (run_id, seq, event_data, created_at) VALUES (?, ?, ?, ?)`,
		runID, seq, eventData, now,
	)
	if err != nil {
		return RunEvent{}, fmt.Errorf("append run event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RunEvent{}, fmt.Errorf("run event id: %w", err)
	}
	return RunEvent{ID: id, RunID: runID, Seq: seq, EventData: eventData, CreatedAt: now}, nil
}

// ListRunEvents returns runID's events in recorded order.
func (s *Store) ListRunEvents(runID string) ([]RunEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, seq, event_data, created_at FROM run_events WHERE run_id = ? ORDER BY seq`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list run events for %q: %w", runID, err)
	}
	defer rows.Close()

	var events []RunEvent
	for rows.Next() {
		var e RunEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan run event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// runColumns is the run SELECT column list; scanRun's Scan argument order
// must stay in lockstep with it.
const runColumns = "id, task_id, chat_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg, permission_mode, auto_accept, model, base_sha, head_sha, dirty, purpose, origin, interrupt, attachments, failure_kind, failure_window, failure_reset_at, reply_chat_id, parent_notified_at, profile_id, failover_of"

// runColumnsAs is runColumns with every column qualified by alias.
func runColumnsAs(alias string) string {
	parts := strings.Split(runColumns, ", ")
	for i, c := range parts {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}

// scanRun scans one row selected with runColumns (same order).
func scanRun(row rowScanner) (Run, error) {
	var r Run
	var chatID sql.NullInt64
	var finishedAt sql.NullTime
	var model sql.NullString
	var baseSHA, headSHA sql.NullString
	var dirty sql.NullBool
	var failureResetAt sql.NullTime
	var replyChatID sql.NullInt64
	var parentNotifiedAt sql.NullTime
	var profileID sql.NullInt64
	if err := row.Scan(&r.ID, &r.TaskID, &chatID, &r.Provider, &r.Prompt, &r.Status,
		&r.StartedAt, &finishedAt, &r.StopReason, &r.ErrMsg, &r.PermissionMode, &r.AutoAccept, &model,
		&baseSHA, &headSHA, &dirty, &r.Purpose, &r.Origin, &r.Interrupt, &r.Attachments,
		&r.FailureKind, &r.FailureWindow, &failureResetAt, &replyChatID, &parentNotifiedAt, &profileID, &r.FailoverOf); err != nil {
		return Run{}, err
	}
	// NULL decodes as ChatID 0 ("unknown"/pre-chats) -- see CreateRun's
	// symmetric write side and Run.ChatID's doc comment. Every run the
	// application itself creates post-migration has a real chat_id; only a
	// caller bypassing internal/runs.Registry.Start entirely (raw store
	// access, e.g. a test simulating a legacy row) can produce this.
	r.ChatID = chatID.Int64
	r.FinishedAt = nullToTimePtr(finishedAt)
	r.Model = model.String
	r.BaseSHA = nullToStringPtr(baseSHA)
	r.HeadSHA = nullToStringPtr(headSHA)
	r.Dirty = nullBoolPtr(dirty)
	r.FailureResetAt = nullToTimePtr(failureResetAt)
	r.ReplyChatID = replyChatID.Int64
	r.ParentNotifiedAt = nullToTimePtr(parentNotifiedAt)
	r.ProfileID = profileID.Int64
	return r, nil
}

func nullBoolPtr(n sql.NullBool) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Bool
	return &v
}
