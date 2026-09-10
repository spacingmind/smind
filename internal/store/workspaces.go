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

	workspaces := make([]Workspace, 0)
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Path, &w.Title, &w.RoutingPolicy, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	return workspaces, rows.Err()
}

// DeleteWorkspace permanently removes workspace id and everything under it
// from smind's own tracking: workspace_accounts rows, every space in the
// workspace (via DeleteSpace, cascading to their tasks), every
// workspace-level task with no space (via DeleteTask), and finally the
// workspaces row -- in that FK-safe child-before-parent order. A second,
// unrelated workspace's rows are completely untouched. Deleting a
// nonexistent workspace is a clear not-found error (via GetWorkspace),
// never a silent no-op.
func (s *Store) DeleteWorkspace(id int64) error {
	if _, err := s.GetWorkspace(id); err != nil {
		return fmt.Errorf("delete workspace %d: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM workspace_accounts WHERE workspace_id = ?`, id); err != nil {
		return fmt.Errorf("delete workspace %d: delete workspace accounts: %w", id, err)
	}
	spaces, err := s.ListSpacesByWorkspace(id)
	if err != nil {
		return fmt.Errorf("delete workspace %d: %w", id, err)
	}
	for _, sp := range spaces {
		if err := s.DeleteSpace(sp.ID); err != nil {
			return fmt.Errorf("delete workspace %d: %w", id, err)
		}
	}
	ungrouped, err := s.ListUngroupedTasksByWorkspace(id)
	if err != nil {
		return fmt.Errorf("delete workspace %d: %w", id, err)
	}
	for _, t := range ungrouped {
		if err := s.DeleteTask(t.ID); err != nil {
			return fmt.Errorf("delete workspace %d: %w", id, err)
		}
	}
	if _, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete workspace %d: %w", id, err)
	}
	return nil
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
