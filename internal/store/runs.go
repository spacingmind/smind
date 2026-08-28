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
	_, err := s.db.Exec(
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.Provider, r.Prompt, r.Status, r.StartedAt,
		timePtrToNull(r.FinishedAt), r.StopReason, r.ErrMsg,
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
		`SELECT id, task_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg
		 FROM runs WHERE id = ?`, id,
	)
	r, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("get run %q: %w", id, err)
	}
	return r, nil
}

// ListRecentRuns returns up to limit runs, most recently started first --
// used to rehydrate internal/runs.Registry's in-memory map at startup.
func (s *Store) ListRecentRuns(limit int) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, provider, prompt, status, started_at, finished_at, stop_reason, err_msg
		 FROM runs ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent runs: %w", err)
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
	var finishedAt sql.NullTime
	if err := row.Scan(&r.ID, &r.TaskID, &r.Provider, &r.Prompt, &r.Status,
		&r.StartedAt, &finishedAt, &r.StopReason, &r.ErrMsg); err != nil {
		return Run{}, err
	}
	r.FinishedAt = nullToTimePtr(finishedAt)
	return r, nil
}
