package store

import (
	"database/sql"
	"fmt"
	"time"
)

// StallRow is one live task's OWN facts for the stalled-tree detector
// (internal/stall); the detector aggregates them over subtrees itself.
type StallRow struct {
	TaskID    int64
	ParentID  *int64
	Title     string
	CreatedAt time.Time

	// Running is true when any run of the task has status 'running'.
	Running bool
	// LastActivity is the max of the task's created_at, every run's
	// started_at/finished_at and every chat-queue item's created_at/updated_at
	// over the task's chats.
	LastActivity time.Time
	// HasQueued is true when a chat-queue item is still 'queued'; QueuedHold is
	// the max not_before over those items (nil = none held).
	HasQueued  bool
	QueuedHold *time.Time

	// HasRun and the Latest* fields describe the latest run by started_at.
	HasRun               bool
	LatestStatus         string
	LatestStopReason     string
	LatestFailureKind    string
	LatestFailureResetAt *time.Time
	// ProfileOnRateLimit and ProfileOrchestration come from the latest run's
	// profile (LEFT JOIN: "" / false when it has none or was deleted).
	ProfileOnRateLimit   string
	ProfileOrchestration bool

	// StallAlertedSince is the stalledSince of the last alerted episode.
	StallAlertedSince *time.Time
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// StallSnapshot reads every live (not archived, kind='task') task's own
// stall facts in a fixed number of queries (4), never one per task.
func (s *Store) StallSnapshot() ([]StallRow, error) {
	rows, err := s.db.Query(
		`SELECT id, parent_task_id, title, created_at, stall_alerted_since
		 FROM tasks WHERE archived_at IS NULL AND kind = 'task' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("stall snapshot: tasks: %w", err)
	}
	defer rows.Close()
	var out []StallRow
	byID := make(map[int64]int)
	for rows.Next() {
		var (
			r       StallRow
			parent  sql.NullInt64
			alerted sql.NullTime
		)
		if err := rows.Scan(&r.TaskID, &parent, &r.Title, &r.CreatedAt, &alerted); err != nil {
			return nil, fmt.Errorf("stall snapshot: scan task: %w", err)
		}
		if parent.Valid {
			p := parent.Int64
			r.ParentID = &p
		}
		if alerted.Valid {
			t := alerted.Time
			r.StallAlertedSince = &t
		}
		r.LastActivity = r.CreatedAt
		byID[r.TaskID] = len(out)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stall snapshot: tasks: %w", err)
	}

	// Runs, oldest first so the last row seen per task is its latest run.
	runRows, err := s.db.Query(
		`SELECT r.task_id, r.status, r.started_at, r.finished_at, r.stop_reason,
		        r.failure_kind, r.failure_reset_at,
		        COALESCE(p.on_rate_limit, ''), COALESCE(p.orchestration_tools, 0)
		 FROM runs r
		 JOIN tasks t ON t.id = r.task_id AND t.archived_at IS NULL AND t.kind = 'task'
		 LEFT JOIN agent_profiles p ON p.id = r.profile_id
		 ORDER BY r.task_id, r.started_at, r.id`)
	if err != nil {
		return nil, fmt.Errorf("stall snapshot: runs: %w", err)
	}
	defer runRows.Close()
	for runRows.Next() {
		var (
			taskID             int64
			status, stopReason string
			failureKind        string
			started            time.Time
			finished, resetAt  sql.NullTime
			onRateLimit        string
			orch               int64
		)
		if err := runRows.Scan(&taskID, &status, &started, &finished, &stopReason, &failureKind, &resetAt, &onRateLimit, &orch); err != nil {
			return nil, fmt.Errorf("stall snapshot: scan run: %w", err)
		}
		i, ok := byID[taskID]
		if !ok {
			continue
		}
		r := &out[i]
		if status == "running" {
			r.Running = true
		}
		r.LastActivity = maxTime(r.LastActivity, started)
		if finished.Valid {
			r.LastActivity = maxTime(r.LastActivity, finished.Time)
		}
		r.HasRun = true
		r.LatestStatus, r.LatestStopReason, r.LatestFailureKind = status, stopReason, failureKind
		r.LatestFailureResetAt = nil
		if resetAt.Valid {
			t := resetAt.Time
			r.LatestFailureResetAt = &t
		}
		r.ProfileOnRateLimit, r.ProfileOrchestration = onRateLimit, orch != 0
	}
	if err := runRows.Err(); err != nil {
		return nil, fmt.Errorf("stall snapshot: runs: %w", err)
	}

	qRows, err := s.db.Query(
		`SELECT c.task_id, q.status, q.not_before, q.created_at, q.updated_at
		 FROM chat_queue q
		 JOIN chats c ON c.id = q.chat_id
		 JOIN tasks t ON t.id = c.task_id AND t.archived_at IS NULL AND t.kind = 'task'`)
	if err != nil {
		return nil, fmt.Errorf("stall snapshot: queue: %w", err)
	}
	defer qRows.Close()
	for qRows.Next() {
		var (
			taskID             int64
			status             string
			notBefore          sql.NullTime
			created, updatedAt time.Time
		)
		if err := qRows.Scan(&taskID, &status, &notBefore, &created, &updatedAt); err != nil {
			return nil, fmt.Errorf("stall snapshot: scan queue: %w", err)
		}
		i, ok := byID[taskID]
		if !ok {
			continue
		}
		r := &out[i]
		r.LastActivity = maxTime(r.LastActivity, maxTime(created, updatedAt))
		if status == ChatQueueStatusQueued {
			r.HasQueued = true
			if notBefore.Valid && (r.QueuedHold == nil || notBefore.Time.After(*r.QueuedHold)) {
				t := notBefore.Time
				r.QueuedHold = &t
			}
		}
	}
	if err := qRows.Err(); err != nil {
		return nil, fmt.Errorf("stall snapshot: queue: %w", err)
	}
	return out, nil
}

// MarkStallAlerted records that the stall episode of taskID that began at
// since (the subtree activity time) has been alerted.
func (s *Store) MarkStallAlerted(taskID int64, since time.Time) error {
	res, err := s.db.Exec(`UPDATE tasks SET stall_alerted_since = ? WHERE id = ?`, since.UTC(), taskID)
	if err != nil {
		return fmt.Errorf("mark stall alerted for task %d: %w", taskID, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("mark stall alerted: task %d not found", taskID)
	}
	return nil
}
