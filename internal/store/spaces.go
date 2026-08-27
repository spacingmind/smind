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

	var spaces []Space
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.WorkspaceID, &sp.Title, &sp.EnvData, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan space: %w", err)
		}
		spaces = append(spaces, sp)
	}
	return spaces, rows.Err()
}
