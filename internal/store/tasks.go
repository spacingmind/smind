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

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
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
