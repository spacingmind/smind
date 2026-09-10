package store

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateTask inserts a new task, stamping created_at/updated_at.
func (s *Store) CreateTask(t Task) (Task, error) {
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO tasks (workspace_id, space_id, title, status, worktree_path, branch, created_at, updated_at, archived_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.WorkspaceID, int64PtrToNull(t.SpaceID), t.Title, t.Status,
		stringPtrToNull(t.WorktreePath), stringPtrToNull(t.Branch),
		t.CreatedAt, t.UpdatedAt, timePtrToNull(t.ArchivedAt),
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, fmt.Errorf("task id: %w", err)
	}
	t.ID = id
	return t, nil
}

// GetTask returns the task with the given id.
func (s *Store) GetTask(id int64) (Task, error) {
	row := s.db.QueryRow(
		`SELECT id, workspace_id, space_id, title, status, worktree_path, branch, created_at, updated_at, archived_at
		 FROM tasks WHERE id = ?`, id,
	)
	t, err := scanTask(row)
	if err != nil {
		return Task{}, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// ListTasksByWorkspace returns all tasks for workspaceID, ordered by id.
func (s *Store) ListTasksByWorkspace(workspaceID int64) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, space_id, title, status, worktree_path, branch, created_at, updated_at, archived_at
		 FROM tasks WHERE workspace_id = ? ORDER BY id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tasks for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListTasksBySpace returns all tasks in spaceID, ordered by id.
func (s *Store) ListTasksBySpace(spaceID int64) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, space_id, title, status, worktree_path, branch, created_at, updated_at, archived_at
		 FROM tasks WHERE space_id = ? ORDER BY id`,
		spaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tasks for space %d: %w", spaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListUngroupedTasksByWorkspace returns all tasks directly in workspaceID
// (i.e. with no space), ordered by id.
func (s *Store) ListUngroupedTasksByWorkspace(workspaceID int64) ([]Task, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, space_id, title, status, worktree_path, branch, created_at, updated_at, archived_at
		 FROM tasks WHERE workspace_id = ? AND space_id IS NULL ORDER BY id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ungrouped tasks for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus sets a task's status and returns the updated task.
func (s *Store) UpdateTaskStatus(id int64, status string) (Task, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task %d status: %w", id, err)
	}
	return s.GetTask(id)
}

// ArchiveTask sets a task's status to "archived" and stamps archived_at, then
// returns the updated task. Calling it again on an already-archived task is a
// no-op: the WHERE clause matches no rows, so archived_at is left as the
// first archival time rather than being bumped forward on a repeated or
// retried call.
func (s *Store) ArchiveTask(id int64) (Task, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = 'archived', archived_at = ? WHERE id = ? AND status != 'archived'`,
		now, id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("archive task %d: %w", id, err)
	}
	return s.GetTask(id)
}

// DeleteTask permanently removes task id and everything scoped to it from
// smind's own tracking: run_events (via each of its runs), runs,
// terminal_sessions, and finally the tasks row itself, in that FK-safe
// child-before-parent order (matching the schema's foreign keys, enforced
// by _pragma=foreign_keys(1) -- see store.sqliteDSN). It never touches
// anything on disk; git worktree cleanup is workspace.Manager's job (see
// workspace.Manager.DeleteTask), which calls this only after that succeeds.
// Deleting a nonexistent task is a clear not-found error (via GetTask),
// never a silent no-op.
func (s *Store) DeleteTask(id int64) error {
	if _, err := s.GetTask(id); err != nil {
		return fmt.Errorf("delete task %d: %w", id, err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM run_events WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`, id,
	); err != nil {
		return fmt.Errorf("delete task %d: delete run events: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM runs WHERE task_id = ?`, id); err != nil {
		return fmt.Errorf("delete task %d: delete runs: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM terminal_sessions WHERE task_id = ?`, id); err != nil {
		return fmt.Errorf("delete task %d: delete terminal sessions: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete task %d: %w", id, err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var spaceID sql.NullInt64
	var worktreePath, branch sql.NullString
	var archivedAt sql.NullTime

	if err := row.Scan(&t.ID, &t.WorkspaceID, &spaceID, &t.Title, &t.Status,
		&worktreePath, &branch, &t.CreatedAt, &t.UpdatedAt, &archivedAt); err != nil {
		return Task{}, err
	}

	t.SpaceID = nullToInt64Ptr(spaceID)
	t.WorktreePath = nullToStringPtr(worktreePath)
	t.Branch = nullToStringPtr(branch)
	t.ArchivedAt = nullToTimePtr(archivedAt)
	return t, nil
}

func int64PtrToNull(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func stringPtrToNull(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func timePtrToNull(p *time.Time) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *p, Valid: true}
}

func nullToInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullToStringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func nullToTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	v := n.Time
	return &v
}
