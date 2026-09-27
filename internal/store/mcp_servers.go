package store

import (
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrMcpServerNameConflict reports a create/update whose name collides with
// a different row's -- the UNIQUE constraint on mcp_servers.name, mapped to
// a domain error so callers see a clear conflict rather than the raw
// driver error (the plan's Store CRUD scenario: "a clear conflict error,
// not a silent overwrite or a generic SQL error leaking to the caller").
var ErrMcpServerNameConflict = errors.New("mcp server name already exists")

// isMcpServerNameConflict reports whether err is the sqlite driver's
// unique-constraint error, after any amount of fmt.Errorf %w wrapping.
func isMcpServerNameConflict(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	// SQLITE_CONSTRAINT_UNIQUE, from modernc.org/sqlite/lib; the driver's
	// own Error type doesn't re-export the constraint codes.
	return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

const mcpServerColumns = `id, name, transport, command, args, env, url, headers, enabled, created_at, updated_at`

// CreateMcpServer inserts a new MCP server, stamping
// created_at/updated_at. m.ID is ignored on input and set to the assigned
// row id on return, and m.Enabled is ignored: a server is always created
// enabled (the column's default) and disabled afterwards via
// SetMcpServerEnabled -- matching ADR-0018, whose mcp.create carries no
// enabled field and whose setEnabled is the dedicated toggle. A name
// colliding with an existing row returns ErrMcpServerNameConflict.
func (s *Store) CreateMcpServer(m McpServer) (McpServer, error) {
	now := time.Now().UTC()
	m.CreatedAt, m.UpdatedAt, m.Enabled = now, now, true

	res, err := s.db.Exec(
		`INSERT INTO mcp_servers (name, transport, command, args, env, url, headers, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Transport, m.Command, m.Args, m.Env, m.URL, m.Headers, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		if isMcpServerNameConflict(err) {
			return McpServer{}, fmt.Errorf("insert mcp server %q: %w", m.Name, ErrMcpServerNameConflict)
		}
		return McpServer{}, fmt.Errorf("insert mcp server: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return McpServer{}, fmt.Errorf("mcp server id: %w", err)
	}
	m.ID = id
	return m, nil
}

// GetMcpServer returns the MCP server with the given id.
func (s *Store) GetMcpServer(id int64) (McpServer, error) {
	var m McpServer
	err := s.db.QueryRow(
		`SELECT `+mcpServerColumns+` FROM mcp_servers WHERE id = ?`, id,
	).Scan(&m.ID, &m.Name, &m.Transport, &m.Command, &m.Args, &m.Env, &m.URL, &m.Headers, &m.Enabled, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return McpServer{}, fmt.Errorf("get mcp server %d: %w", id, err)
	}
	return m, nil
}

// ListMcpServers returns every MCP server, enabled and disabled alike,
// ordered by id. Disabled servers are invisible to agents (ListMcpServers
// ForWorkspace) but still listed here -- they only appear in ls/mcp.list
// (ADR-0018 resolved decision 7).
func (s *Store) ListMcpServers() ([]McpServer, error) {
	rows, err := s.db.Query(
		`SELECT ` + mcpServerColumns + ` FROM mcp_servers ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	defer rows.Close()

	servers := make([]McpServer, 0)
	for rows.Next() {
		var m McpServer
		if err := rows.Scan(&m.ID, &m.Name, &m.Transport, &m.Command, &m.Args, &m.Env, &m.URL, &m.Headers, &m.Enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mcp server: %w", err)
		}
		servers = append(servers, m)
	}
	return servers, rows.Err()
}

// UpdateMcpServer replaces every field of the MCP server m.ID (a
// full-record replace, not a partial patch -- matching UpdateAgentProfile),
// preserving its original created_at. Updating a nonexistent id is a clear
// not-found error, never a silent no-op. A name colliding with a different
// row's returns ErrMcpServerNameConflict.
func (s *Store) UpdateMcpServer(m McpServer) (McpServer, error) {
	existing, err := s.GetMcpServer(m.ID)
	if err != nil {
		return McpServer{}, fmt.Errorf("update mcp server %d: %w", m.ID, err)
	}
	m.CreatedAt = existing.CreatedAt
	m.UpdatedAt = time.Now().UTC()

	if _, err := s.db.Exec(
		`UPDATE mcp_servers SET name = ?, transport = ?, command = ?, args = ?, env = ?, url = ?, headers = ?, enabled = ?, updated_at = ?
		 WHERE id = ?`,
		m.Name, m.Transport, m.Command, m.Args, m.Env, m.URL, m.Headers, m.Enabled, m.UpdatedAt, m.ID,
	); err != nil {
		if isMcpServerNameConflict(err) {
			return McpServer{}, fmt.Errorf("update mcp server %d: %w", m.ID, ErrMcpServerNameConflict)
		}
		return McpServer{}, fmt.Errorf("update mcp server %d: %w", m.ID, err)
	}
	return m, nil
}

// SetMcpServerEnabled toggles mcp server id's enabled flag without touching
// any other field, stamping a new updated_at. A nonexistent id is a clear
// not-found error (via GetMcpServer).
func (s *Store) SetMcpServerEnabled(id int64, enabled bool) (McpServer, error) {
	if _, err := s.GetMcpServer(id); err != nil {
		return McpServer{}, fmt.Errorf("set mcp server %d enabled: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE mcp_servers SET enabled = ?, updated_at = ? WHERE id = ?`,
		enabled, time.Now().UTC(), id,
	); err != nil {
		return McpServer{}, fmt.Errorf("set mcp server %d enabled: %w", id, err)
	}
	return s.GetMcpServer(id)
}

// DeleteMcpServer permanently removes the MCP server with the given id and
// its workspace_mcp_servers rows (child-before-parent, matching
// DeleteAccount's workspace_accounts cleanup). Deleting a nonexistent id is
// a clear not-found error (via GetMcpServer), never a silent no-op.
func (s *Store) DeleteMcpServer(id int64) error {
	if _, err := s.GetMcpServer(id); err != nil {
		return fmt.Errorf("delete mcp server %d: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM workspace_mcp_servers WHERE mcp_server_id = ?`, id); err != nil {
		return fmt.Errorf("delete mcp server %d: delete workspace restrictions: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM mcp_servers WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete mcp server %d: %w", id, err)
	}
	return nil
}

// AddWorkspaceMcpServer adds serverID to workspaceID's usable set,
// restricting serverID to that workspace (a server with no restriction row
// is available to every workspace -- ADR-0018's workspace_mcp_servers
// semantics, matching workspace_accounts).
func (s *Store) AddWorkspaceMcpServer(workspaceID, serverID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO workspace_mcp_servers (workspace_id, mcp_server_id) VALUES (?, ?)`,
		workspaceID, serverID,
	)
	if err != nil {
		return fmt.Errorf("add workspace %d mcp server %d: %w", workspaceID, serverID, err)
	}
	return nil
}

// RemoveWorkspaceMcpServer drops serverID from workspaceID's usable set.
// Removing a pair that isn't linked is a no-op (idempotent un-restrict),
// matching how workspace_mcp_servers has no other mutation surface yet.
func (s *Store) RemoveWorkspaceMcpServer(workspaceID, serverID int64) error {
	if _, err := s.db.Exec(
		`DELETE FROM workspace_mcp_servers WHERE workspace_id = ? AND mcp_server_id = ?`,
		workspaceID, serverID,
	); err != nil {
		return fmt.Errorf("remove workspace %d mcp server %d: %w", workspaceID, serverID, err)
	}
	return nil
}

// ListMcpServersForWorkspace returns the enabled MCP servers applicable to
// workspaceID, ordered by id: every enabled server with no
// workspace_mcp_servers restriction row (globally available), plus every
// enabled server restricted specifically to this workspace. Disabled
// servers are never returned (ADR-0018 resolved decision 7: invisible to
// agents and capability checks, visible only in ListMcpServers).
func (s *Store) ListMcpServersForWorkspace(workspaceID int64) ([]McpServer, error) {
	rows, err := s.db.Query(
		`SELECT `+mcpServerColumns+`
		 FROM mcp_servers m
		 WHERE m.enabled = 1
		   AND NOT EXISTS (
		       SELECT 1 FROM workspace_mcp_servers r
		       WHERE r.mcp_server_id = m.id AND r.workspace_id != ?
		   )
		 ORDER BY m.id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	servers := make([]McpServer, 0)
	for rows.Next() {
		var m McpServer
		if err := rows.Scan(&m.ID, &m.Name, &m.Transport, &m.Command, &m.Args, &m.Env, &m.URL, &m.Headers, &m.Enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mcp server: %w", err)
		}
		servers = append(servers, m)
	}
	return servers, rows.Err()
}
