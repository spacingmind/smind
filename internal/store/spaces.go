package store

import (
	"fmt"
	"time"
)

// CreateSpace inserts a new space, stamping created_at/updated_at.
func (s *Store) CreateSpace(sp Space) (Space, error) {
	now := time.Now().UTC()
	sp.CreatedAt, sp.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO spaces (workspace_id, title, env_data, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		sp.WorkspaceID, sp.Title, sp.EnvData, sp.CreatedAt, sp.UpdatedAt,
	)
	if err != nil {
		return Space{}, fmt.Errorf("insert space: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Space{}, fmt.Errorf("space id: %w", err)
	}
	sp.ID = id
	return sp, nil
}

// GetSpace returns the space with the given id.
func (s *Store) GetSpace(id int64) (Space, error) {
	var sp Space
	err := s.db.QueryRow(
		`SELECT id, workspace_id, title, env_data, created_at, updated_at
		 FROM spaces WHERE id = ?`, id,
	).Scan(&sp.ID, &sp.WorkspaceID, &sp.Title, &sp.EnvData, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		return Space{}, fmt.Errorf("get space %d: %w", id, err)
	}
	return sp, nil
}

// DeleteSpace permanently removes space id and every task within it (via
// DeleteTask, which cascades to each task's runs/run_events/
// terminal_sessions) from smind's own tracking, then the spaces row itself.
// A task in a different space or workspace is untouched. Deleting a
// nonexistent space is a clear not-found error (via GetSpace), never a
// silent no-op.
func (s *Store) DeleteSpace(id int64) error {
	if _, err := s.GetSpace(id); err != nil {
		return fmt.Errorf("delete space %d: %w", id, err)
	}
	tasks, err := s.ListTasksBySpace(id)
	if err != nil {
		return fmt.Errorf("delete space %d: %w", id, err)
	}
	for _, t := range tasks {
		if err := s.DeleteTask(t.ID); err != nil {
			return fmt.Errorf("delete space %d: %w", id, err)
		}
	}
	if _, err := s.db.Exec(`DELETE FROM spaces WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete space %d: %w", id, err)
	}
	return nil
}

// ListSpacesByWorkspace returns all spaces for workspaceID, ordered by id.
func (s *Store) ListSpacesByWorkspace(workspaceID int64) ([]Space, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, title, env_data, created_at, updated_at
		 FROM spaces WHERE workspace_id = ? ORDER BY id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list spaces for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	spaces := make([]Space, 0)
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.WorkspaceID, &sp.Title, &sp.EnvData, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan space: %w", err)
		}
		spaces = append(spaces, sp)
	}
	return spaces, rows.Err()
}
