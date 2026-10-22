package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// forcedAtKeep bounds TaskPRState.ForcedAt: the explicit-refresh floor is
// 12 per hour (ADR-0039 section 4), so the newest 12 stamps are enough.
const forcedAtKeep = 12

// TaskPRState is the forge poller's per-task cache and call ledger
// (ADR-0039 section 2). One row per task; an absent row means the task has
// never been polled.
type TaskPRState struct {
	TaskID int64
	// Snapshot is the forge.PR JSON last fetched ("" = none yet); the store
	// treats it as opaque text.
	Snapshot            string
	FetchedAt           time.Time // zero = never (NULL)
	NextAllowedAt       time.Time // zero = no backoff (NULL)
	ConsecutiveFailures int
	LastErrorCode       string
	// ForcedAt holds unix-millis timestamps of explicit refreshes, oldest
	// first. Save keeps only the newest 12.
	ForcedAt []int64
}

// GetTaskPRState returns the task's poller row, reporting false when none
// exists.
func (s *Store) GetTaskPRState(taskID int64) (TaskPRState, bool, error) {
	var st TaskPRState
	var fetched, next sql.NullTime
	var forcedJSON string
	err := s.db.QueryRow(
		`SELECT task_id, snapshot, fetched_at, next_allowed_at, consecutive_failures, last_error_code, forced_at
		 FROM task_pr_state WHERE task_id = ?`, taskID,
	).Scan(&st.TaskID, &st.Snapshot, &fetched, &next, &st.ConsecutiveFailures, &st.LastErrorCode, &forcedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskPRState{}, false, nil
	}
	if err != nil {
		return TaskPRState{}, false, fmt.Errorf("get task pr state for task %d: %w", taskID, err)
	}
	if fetched.Valid {
		st.FetchedAt = fetched.Time
	}
	if next.Valid {
		st.NextAllowedAt = next.Time
	}
	if err := json.Unmarshal([]byte(forcedJSON), &st.ForcedAt); err != nil {
		return TaskPRState{}, false, fmt.Errorf("get task pr state for task %d: decode forced_at: %w", taskID, err)
	}
	return st, true, nil
}

// SaveTaskPRState upserts the task's poller row. The task must exist
// (foreign key). Times are stored as UTC; zero times become NULL.
func (s *Store) SaveTaskPRState(st TaskPRState) error {
	if len(st.ForcedAt) > forcedAtKeep {
		st.ForcedAt = st.ForcedAt[len(st.ForcedAt)-forcedAtKeep:]
	}
	if st.ForcedAt == nil {
		st.ForcedAt = []int64{}
	}
	forcedJSON, err := json.Marshal(st.ForcedAt)
	if err != nil {
		return fmt.Errorf("save task pr state for task %d: encode forced_at: %w", st.TaskID, err)
	}
	var fetched, next sql.NullTime
	if !st.FetchedAt.IsZero() {
		fetched = sql.NullTime{Time: st.FetchedAt.UTC(), Valid: true}
	}
	if !st.NextAllowedAt.IsZero() {
		next = sql.NullTime{Time: st.NextAllowedAt.UTC(), Valid: true}
	}
	if _, err := s.db.Exec(
		`INSERT INTO task_pr_state (task_id, snapshot, fetched_at, next_allowed_at, consecutive_failures, last_error_code, forced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(task_id) DO UPDATE SET
		   snapshot = excluded.snapshot,
		   fetched_at = excluded.fetched_at,
		   next_allowed_at = excluded.next_allowed_at,
		   consecutive_failures = excluded.consecutive_failures,
		   last_error_code = excluded.last_error_code,
		   forced_at = excluded.forced_at`,
		st.TaskID, st.Snapshot, fetched, next, st.ConsecutiveFailures, st.LastErrorCode, string(forcedJSON),
	); err != nil {
		return fmt.Errorf("save task pr state for task %d: %w", st.TaskID, err)
	}
	return nil
}

// DeleteTaskPRState removes the task's poller row; a missing row is not an
// error.
func (s *Store) DeleteTaskPRState(taskID int64) error {
	if _, err := s.db.Exec(`DELETE FROM task_pr_state WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("delete task pr state for task %d: %w", taskID, err)
	}
	return nil
}
