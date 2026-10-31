package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Watch states (fleet_watches.state, ADR-0048 F1a).
const (
	FleetWatchOpen   = "open"
	FleetWatchClosed = "closed"
)

// FleetWatch is a local subscription to a remote task's runs: what to
// poll (host, remote task, last-seen run), where to deliver (local task,
// chat) and how far along the cursor is. A watch has no foreign key to
// fleet_hosts on purpose: it must survive host removal so it can still
// deliver "host unavailable" and then be closed.
type FleetWatch struct {
	ID           int64
	HostID       string
	RemoteTaskID string
	RemoteRunID  string
	LocalTaskID  int64
	ChatID       int64
	Cursor       string
	State        string
	Failures     int
	Notify       bool
	ClosedReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// FleetRemoteTask is the local mirror of a remote host's task. CostUSD is
// nil when the remote did not report a cost -- unknown, never estimated.
type FleetRemoteTask struct {
	HostID       string
	RemoteTaskID string
	LocalTaskID  int64
	Title        string
	Branch       string
	State        string
	CostUSD      *float64
	UpdatedAt    time.Time
}

// FleetIdem is a stored idempotent request: the response a host already
// got for (host_id, idem_key), replayed instead of re-executed.
type FleetIdem struct {
	HostID    string
	Key       string
	Method    string
	TaskID    string
	RunID     string
	Response  string
	CreatedAt time.Time
}

// PutFleetRemoteTaskAndWatch upserts a remote-task mirror and, when w is
// non-nil, opens a watch on it -- in one transaction, so a poll either
// records the run it is watching or nothing at all. A watch conflicting
// with an existing open one for the same remote run is not duplicated:
// the existing watch's id is returned unchanged. An incomplete watch
// (empty host or remote task) is rejected before anything is written.
func (s *Store) PutFleetRemoteTaskAndWatch(t FleetRemoteTask, w *FleetWatch) (int64, error) {
	if w != nil && (w.HostID == "" || w.RemoteTaskID == "") {
		return 0, fmt.Errorf("put fleet remote task %s/%s: watch needs a host and a remote task", t.HostID, t.RemoteTaskID)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("put fleet remote task %s/%s: begin: %w", t.HostID, t.RemoteTaskID, err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if _, err := tx.Exec(`INSERT INTO fleet_remote_tasks (host_id, remote_task_id, local_task_id, title, branch, state, cost_usd, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id, remote_task_id) DO UPDATE SET
			local_task_id = excluded.local_task_id,
			title = excluded.title,
			branch = excluded.branch,
			state = excluded.state,
			cost_usd = excluded.cost_usd,
			updated_at = excluded.updated_at`,
		t.HostID, t.RemoteTaskID, t.LocalTaskID, t.Title, t.Branch, t.State, float64PtrToNull(t.CostUSD), t.UpdatedAt); err != nil {
		return 0, fmt.Errorf("put fleet remote task %s/%s: mirror: %w", t.HostID, t.RemoteTaskID, err)
	}

	watchID := int64(0)
	if w != nil {
		res, err := tx.Exec(`INSERT INTO fleet_watches (host_id, remote_task_id, remote_run_id, local_task_id, chat_id, cursor, state, failures, notify, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(host_id, remote_task_id, remote_run_id) DO NOTHING`,
			w.HostID, w.RemoteTaskID, w.RemoteRunID, w.LocalTaskID, w.ChatID, w.Cursor, FleetWatchOpen, w.Failures, w.Notify, now, now)
		if err != nil {
			return 0, fmt.Errorf("put fleet remote task %s/%s: watch: %w", t.HostID, t.RemoteTaskID, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return 0, fmt.Errorf("put fleet remote task %s/%s: watch: %w", t.HostID, t.RemoteTaskID, err)
		} else if n == 0 {
			// Already watching this remote run: keep the existing watch,
			// with its cursor, rather than stacking a second one.
			err := tx.QueryRow(
				`SELECT id FROM fleet_watches WHERE host_id = ? AND remote_task_id = ? AND remote_run_id = ?`,
				w.HostID, w.RemoteTaskID, w.RemoteRunID,
			).Scan(&watchID)
			if err != nil {
				return 0, fmt.Errorf("put fleet remote task %s/%s: existing watch: %w", t.HostID, t.RemoteTaskID, err)
			}
		} else if watchID, err = res.LastInsertId(); err != nil {
			return 0, fmt.Errorf("put fleet remote task %s/%s: watch id: %w", t.HostID, t.RemoteTaskID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("put fleet remote task %s/%s: commit: %w", t.HostID, t.RemoteTaskID, err)
	}
	return watchID, nil
}

const fleetRemoteTaskColumns = `host_id, remote_task_id, local_task_id, title, branch, state, cost_usd, updated_at`

func scanFleetRemoteTask(sc rowScanner) (FleetRemoteTask, error) {
	var t FleetRemoteTask
	var cost sql.NullFloat64
	if err := sc.Scan(&t.HostID, &t.RemoteTaskID, &t.LocalTaskID, &t.Title, &t.Branch, &t.State, &cost, &t.UpdatedAt); err != nil {
		return FleetRemoteTask{}, err
	}
	if cost.Valid {
		v := cost.Float64
		t.CostUSD = &v
	}
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

// GetFleetRemoteTask returns one mirrored remote task.
func (s *Store) GetFleetRemoteTask(hostID, remoteTaskID string) (FleetRemoteTask, error) {
	t, err := scanFleetRemoteTask(s.db.QueryRow(
		`SELECT `+fleetRemoteTaskColumns+` FROM fleet_remote_tasks WHERE host_id = ? AND remote_task_id = ?`, hostID, remoteTaskID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return FleetRemoteTask{}, fmt.Errorf("get fleet remote task %s/%s: %w", hostID, remoteTaskID, ErrFleetNotFound)
	}
	if err != nil {
		return FleetRemoteTask{}, fmt.Errorf("get fleet remote task %s/%s: %w", hostID, remoteTaskID, err)
	}
	return t, nil
}

// ListFleetRemoteTasks returns the mirrors of one host, or of every host
// when hostID is empty.
func (s *Store) ListFleetRemoteTasks(hostID string) ([]FleetRemoteTask, error) {
	q := `SELECT ` + fleetRemoteTaskColumns + ` FROM fleet_remote_tasks`
	var args []any
	if hostID != "" {
		q += ` WHERE host_id = ?`
		args = append(args, hostID)
	}
	q += ` ORDER BY host_id, remote_task_id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list fleet remote tasks %s: %w", hostID, err)
	}
	defer rows.Close()
	var out []FleetRemoteTask
	for rows.Next() {
		t, err := scanFleetRemoteTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list fleet remote tasks %s: %w", hostID, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteFleetRemoteTask drops one mirror. Deleting a mirror that is not
// stored is not an error.
func (s *Store) DeleteFleetRemoteTask(hostID, remoteTaskID string) error {
	if _, err := s.db.Exec(
		`DELETE FROM fleet_remote_tasks WHERE host_id = ? AND remote_task_id = ?`, hostID, remoteTaskID,
	); err != nil {
		return fmt.Errorf("delete fleet remote task %s/%s: %w", hostID, remoteTaskID, err)
	}
	return nil
}

const fleetWatchColumns = `id, host_id, remote_task_id, remote_run_id, local_task_id, chat_id, cursor, state, failures, notify, closed_reason, created_at, updated_at`

func scanFleetWatch(sc rowScanner) (FleetWatch, error) {
	var w FleetWatch
	if err := sc.Scan(&w.ID, &w.HostID, &w.RemoteTaskID, &w.RemoteRunID, &w.LocalTaskID, &w.ChatID,
		&w.Cursor, &w.State, &w.Failures, &w.Notify, &w.ClosedReason, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return FleetWatch{}, err
	}
	w.CreatedAt = w.CreatedAt.UTC()
	w.UpdatedAt = w.UpdatedAt.UTC()
	return w, nil
}

// GetFleetWatch returns one watch by id.
func (s *Store) GetFleetWatch(id int64) (FleetWatch, error) {
	w, err := scanFleetWatch(s.db.QueryRow(`SELECT `+fleetWatchColumns+` FROM fleet_watches WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return FleetWatch{}, fmt.Errorf("get fleet watch %d: %w", id, ErrFleetNotFound)
	}
	if err != nil {
		return FleetWatch{}, fmt.Errorf("get fleet watch %d: %w", id, err)
	}
	return w, nil
}

// ListOpenFleetWatches returns every watch still being polled, oldest
// first.
func (s *Store) ListOpenFleetWatches() ([]FleetWatch, error) {
	rows, err := s.db.Query(`SELECT `+fleetWatchColumns+` FROM fleet_watches WHERE state = ? ORDER BY id`, FleetWatchOpen)
	if err != nil {
		return nil, fmt.Errorf("list open fleet watches: %w", err)
	}
	defer rows.Close()
	var out []FleetWatch
	for rows.Next() {
		w, err := scanFleetWatch(rows)
		if err != nil {
			return nil, fmt.Errorf("list open fleet watches: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpdateFleetWatch advances a watch's cursor and failure count.
func (s *Store) UpdateFleetWatch(id int64, cursor string, failures int) error {
	res, err := s.db.Exec(
		`UPDATE fleet_watches SET cursor = ?, failures = ?, updated_at = ? WHERE id = ?`,
		cursor, failures, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update fleet watch %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update fleet watch %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("update fleet watch %d: %w", id, ErrFleetNotFound)
	}
	return nil
}

// CloseFleetWatch stops a watch, recording why. Closing an already-closed
// watch keeps its first reason: the second close is a late duplicate of
// the same shutdown, and the first reason is the one that happened.
func (s *Store) CloseFleetWatch(id int64, reason string) error {
	res, err := s.db.Exec(
		`UPDATE fleet_watches SET state = ?, closed_reason = CASE WHEN closed_reason = '' THEN ? ELSE closed_reason END, updated_at = ? WHERE id = ?`,
		FleetWatchClosed, reason, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("close fleet watch %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("close fleet watch %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("close fleet watch %d: %w", id, ErrFleetNotFound)
	}
	return nil
}

// PutFleetIdem stores a response under (host_id, idem_key) and reports
// whether this call created it. When a row already exists it is returned
// with created=false and nothing is written: the caller replays it
// instead of re-executing the request.
func (s *Store) PutFleetIdem(i FleetIdem) (FleetIdem, bool, error) {
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: begin: %w", i.HostID, i.Key, err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT OR IGNORE INTO fleet_idem (host_id, idem_key, method, task_id, run_id, response, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.HostID, i.Key, i.Method, i.TaskID, i.RunID, i.Response, i.CreatedAt)
	if err != nil {
		return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: %w", i.HostID, i.Key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: %w", i.HostID, i.Key, err)
	}
	if n == 1 {
		if err := tx.Commit(); err != nil {
			return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: commit: %w", i.HostID, i.Key, err)
		}
		return i, true, nil
	}

	var existing FleetIdem
	err = tx.QueryRow(`SELECT host_id, idem_key, method, task_id, run_id, response, created_at
		FROM fleet_idem WHERE host_id = ? AND idem_key = ?`, i.HostID, i.Key,
	).Scan(&existing.HostID, &existing.Key, &existing.Method, &existing.TaskID, &existing.RunID, &existing.Response, &existing.CreatedAt)
	if err != nil {
		return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: existing: %w", i.HostID, i.Key, err)
	}
	existing.CreatedAt = existing.CreatedAt.UTC()
	if err := tx.Commit(); err != nil {
		return FleetIdem{}, false, fmt.Errorf("put fleet idem %s/%s: commit: %w", i.HostID, i.Key, err)
	}
	return existing, false, nil
}

// SweepFleetIdem deletes idempotent responses stamped strictly before
// olderThan and returns how many went.
func (s *Store) SweepFleetIdem(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM fleet_idem WHERE created_at < ?`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("sweep fleet idem: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sweep fleet idem: %w", err)
	}
	return n, nil
}

// SetTaskOrigin records which host and trace a task came from (its
// fleet provenance, ADR-0048 F1a).
func (s *Store) SetTaskOrigin(taskID int64, originHost, originTrace string) error {
	res, err := s.db.Exec(`UPDATE tasks SET origin_host = ?, origin_trace = ? WHERE id = ?`, originHost, originTrace, taskID)
	if err != nil {
		return fmt.Errorf("set task %d origin: %w", taskID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set task %d origin: %w", taskID, err)
	}
	if n == 0 {
		return fmt.Errorf("set task %d origin: %w", taskID, ErrFleetNotFound)
	}
	return nil
}

// TaskOrigin returns the host and trace a task came from; both are
// empty for a local task.
func (s *Store) TaskOrigin(taskID int64) (string, string, error) {
	var host, trace string
	err := s.db.QueryRow(`SELECT origin_host, origin_trace FROM tasks WHERE id = ?`, taskID).Scan(&host, &trace)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("task %d origin: %w", taskID, ErrFleetNotFound)
	}
	if err != nil {
		return "", "", fmt.Errorf("task %d origin: %w", taskID, err)
	}
	return host, trace, nil
}

// SetRunOrigin records which host a run arrived from.
func (s *Store) SetRunOrigin(runID, originHost string) error {
	res, err := s.db.Exec(`UPDATE runs SET origin_host = ? WHERE id = ?`, originHost, runID)
	if err != nil {
		return fmt.Errorf("set run %s origin: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set run %s origin: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("set run %s origin: %w", runID, ErrFleetNotFound)
	}
	return nil
}

// RunOrigin returns the host a run arrived from; empty for a local run.
func (s *Store) RunOrigin(runID string) (string, error) {
	var host string
	err := s.db.QueryRow(`SELECT origin_host FROM runs WHERE id = ?`, runID).Scan(&host)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("run %s origin: %w", runID, ErrFleetNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("run %s origin: %w", runID, err)
	}
	return host, nil
}
