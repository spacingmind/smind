package store

import (
	"database/sql"
	"fmt"
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
		`INSERT INTO runs (id, task_id, chat_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg, permission_mode, auto_accept, approval_policy, model, purpose, origin, interrupt)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
		r.ID, r.TaskID, chatID, r.Provider, r.Prompt, r.Status, r.StartedAt,
		timePtrToNull(r.FinishedAt), r.StopReason, r.ErrMsg, r.PermissionMode, r.AutoAccept, nullString(r.Model), r.Purpose, r.Origin, r.Interrupt,
	)
	if err != nil {
		return Run{}, fmt.Errorf("insert run: %w", err)
	}
	return r, nil
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
// guarantees that subprocess died with the old daemon process). Returns the
// number of rows transitioned.
func (s *Store) MarkRunningRunsInterrupted(interruptedStatus string) (int64, error) {
	res, err := s.db.Exec(`UPDATE runs SET status = ? WHERE status = 'running'`, interruptedStatus)
	if err != nil {
		return 0, fmt.Errorf("mark running runs interrupted: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mark running runs interrupted: rows affected: %w", err)
	}
	return n, nil
}

// GetRun returns the run with the given id.
func (s *Store) GetRun(id string) (Run, error) {
	row := s.db.QueryRow(
		`SELECT id, task_id, chat_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg, permission_mode, auto_accept, model, base_sha, head_sha, dirty, purpose, origin, interrupt
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
		`SELECT id, task_id, chat_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg, permission_mode, auto_accept, model, base_sha, head_sha, dirty, purpose, origin, interrupt
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

func scanRun(row rowScanner) (Run, error) {
	var r Run
	var chatID sql.NullInt64
	var finishedAt sql.NullTime
	var model sql.NullString
	var baseSHA, headSHA sql.NullString
	var dirty sql.NullBool
	if err := row.Scan(&r.ID, &r.TaskID, &chatID, &r.Provider, &r.Prompt, &r.Status,
		&r.StartedAt, &finishedAt, &r.StopReason, &r.ErrMsg, &r.PermissionMode, &r.AutoAccept, &model,
		&baseSHA, &headSHA, &dirty, &r.Purpose, &r.Origin, &r.Interrupt); err != nil {
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
	return r, nil
}

func nullBoolPtr(n sql.NullBool) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Bool
	return &v
}
