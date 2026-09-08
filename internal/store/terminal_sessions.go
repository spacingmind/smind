package store

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateTerminalSession inserts a new terminal session row, stamping
// created_at/updated_at -- StartedAt itself is not stamped here, same as
// CreateRun: the caller (internal/terminal.Registry.Create) already has its
// own precise start time for the in-memory session this row mirrors, and
// the two must agree.
func (s *Store) CreateTerminalSession(row TerminalSession) (TerminalSession, error) {
	now := time.Now().UTC()
	row.CreatedAt, row.UpdatedAt = now, now

	_, err := s.db.Exec(
		`INSERT INTO terminal_sessions (id, task_id, status, started_at, closed_at, scrollback, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.TaskID, row.Status, row.StartedAt, timePtrToNull(row.ClosedAt), row.Scrollback,
		row.CreatedAt, row.UpdatedAt,
	)
	if err != nil {
		return TerminalSession{}, fmt.Errorf("insert terminal session: %w", err)
	}
	return row, nil
}

// GetTerminalSession returns the terminal session with the given id.
func (s *Store) GetTerminalSession(id string) (TerminalSession, error) {
	row := s.db.QueryRow(
		`SELECT id, task_id, status, started_at, closed_at, scrollback, created_at, updated_at
		 FROM terminal_sessions WHERE id = ?`, id,
	)
	ts, err := scanTerminalSession(row)
	if err != nil {
		return TerminalSession{}, fmt.Errorf("get terminal session %q: %w", id, err)
	}
	return ts, nil
}

// UpdateTerminalSessionScrollback overwrites a session's checkpointed
// scrollback and bumps updated_at -- called on internal/terminal.Registry's
// bounded-cadence checkpoint, not on every PTY chunk (see that package's
// doc comment on why raw terminal output isn't write-through persisted like
// run_events).
func (s *Store) UpdateTerminalSessionScrollback(id, scrollback string) (TerminalSession, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE terminal_sessions SET scrollback = ?, updated_at = ? WHERE id = ?`,
		scrollback, now, id,
	)
	if err != nil {
		return TerminalSession{}, fmt.Errorf("update terminal session %q scrollback: %w", id, err)
	}
	return s.GetTerminalSession(id)
}

// UpdateTerminalSessionStatus sets a session's terminal (or reconciled)
// status, closedAt, and final scrollback together -- used by
// internal/terminal.Registry.finish so a graceful close (or the shell
// exiting on its own) persists its last-known-good scrollback atomically
// with the status transition, rather than racing a separate checkpoint
// write.
func (s *Store) UpdateTerminalSessionStatus(id, status string, closedAt *time.Time, scrollback string) (TerminalSession, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE terminal_sessions SET status = ?, closed_at = ?, scrollback = ?, updated_at = ? WHERE id = ?`,
		status, timePtrToNull(closedAt), scrollback, now, id,
	)
	if err != nil {
		return TerminalSession{}, fmt.Errorf("update terminal session %q status: %w", id, err)
	}
	return s.GetTerminalSession(id)
}

// MarkRunningTerminalSessionsInterrupted transitions every terminal session
// row still status = "running" to interruptedStatus -- called once at
// daemon startup, before any new session exists, since a "running" row
// surviving to a fresh process start means the PTY subprocess that was
// driving it is definitely gone (see internal/terminal.New's doc comment;
// mirrors store.MarkRunningRunsInterrupted's identical reasoning for
// internal/runs). Returns the number of rows transitioned.
func (s *Store) MarkRunningTerminalSessionsInterrupted(interruptedStatus string) (int64, error) {
	res, err := s.db.Exec(`UPDATE terminal_sessions SET status = ? WHERE status = 'running'`, interruptedStatus)
	if err != nil {
		return 0, fmt.Errorf("mark running terminal sessions interrupted: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mark running terminal sessions interrupted: rows affected: %w", err)
	}
	return n, nil
}

// ListRecentTerminalSessions returns up to limit terminal sessions, most
// recently started first -- used to rehydrate internal/terminal.Registry's
// in-memory map at startup.
func (s *Store) ListRecentTerminalSessions(limit int) ([]TerminalSession, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, status, started_at, closed_at, scrollback, created_at, updated_at
		 FROM terminal_sessions ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent terminal sessions: %w", err)
	}
	defer rows.Close()

	var sessions []TerminalSession
	for rows.Next() {
		ts, err := scanTerminalSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan terminal session: %w", err)
		}
		sessions = append(sessions, ts)
	}
	return sessions, rows.Err()
}

func scanTerminalSession(row rowScanner) (TerminalSession, error) {
	var ts TerminalSession
	var closedAt sql.NullTime
	if err := row.Scan(&ts.ID, &ts.TaskID, &ts.Status, &ts.StartedAt, &closedAt,
		&ts.Scrollback, &ts.CreatedAt, &ts.UpdatedAt); err != nil {
		return TerminalSession{}, err
	}
	ts.ClosedAt = nullToTimePtr(closedAt)
	return ts, nil
}
