package store

import (
	"fmt"
	"time"
)

// CreateWorkspace inserts a new workspace, stamping created_at/updated_at.
func (s *Store) CreateWorkspace(w Workspace) (Workspace, error) {
	now := time.Now().UTC()
	w.CreatedAt, w.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO workspaces (path, title, routing_policy, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		w.Path, w.Title, w.RoutingPolicy, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, fmt.Errorf("workspace id: %w", err)
	}
	w.ID = id
	return w, nil
}

// GetWorkspace returns the workspace with the given id.
func (s *Store) GetWorkspace(id int64) (Workspace, error) {
	var w Workspace
	err := s.db.QueryRow(
		`SELECT id, path, title, routing_policy, created_at, updated_at
		 FROM workspaces WHERE id = ?`, id,
	).Scan(&w.ID, &w.Path, &w.Title, &w.RoutingPolicy, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Workspace{}, fmt.Errorf("get workspace %d: %w", id, err)
	}
	return w, nil
}

// ListWorkspaces returns all workspaces, ordered by id.
func (s *Store) ListWorkspaces() ([]Workspace, error) {
	rows, err := s.db.Query(
		`SELECT id, path, title, routing_policy, created_at, updated_at
		 FROM workspaces ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Path, &w.Title, &w.RoutingPolicy, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	return workspaces, rows.Err()
}

// AddWorkspaceAccount adds accountID to workspaceID's candidate account pool.
func (s *Store) AddWorkspaceAccount(workspaceID, accountID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO workspace_accounts (workspace_id, account_id) VALUES (?, ?)`,
		workspaceID, accountID,
	)
	if err != nil {
		return fmt.Errorf("add workspace %d account %d: %w", workspaceID, accountID, err)
	}
	return nil
}

// ListWorkspaceAccountIDs returns the candidate account ids for workspaceID,
// ordered by account_id. Suitable to pass directly as routing.Router.Route's
// candidateAccountIDs.
func (s *Store) ListWorkspaceAccountIDs(workspaceID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT account_id FROM workspace_accounts WHERE workspace_id = ? ORDER BY account_id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspace %d account ids: %w", workspaceID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan workspace account id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
