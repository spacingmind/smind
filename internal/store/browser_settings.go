package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// BrowserSettings is one workspace's agent-browser decision (ADR-0061): the
// on/off switch and the exact hosts the browser may reach. No row means OFF
// with an empty allowlist.
type BrowserSettings struct {
	WorkspaceID int64     `json:"workspaceId"`
	Enabled     bool      `json:"enabled"`
	AllowHosts  []string  `json:"allowHosts"`  // canonical hosts, never nil
	ProfileMode string    `json:"profileMode"` // "ephemeral"
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

// GetBrowserSettings returns the workspace's browser settings. A workspace
// with no row is the zero value -- OFF, empty allowlist, "ephemeral" -- never
// sql.ErrNoRows.
func (s *Store) GetBrowserSettings(workspaceID int64) (BrowserSettings, error) {
	var (
		bs        BrowserSettings
		allowJSON string
	)
	err := s.db.QueryRow(`SELECT workspace_id, enabled, allow_hosts, profile_mode, updated_at, updated_by FROM browser_settings WHERE workspace_id = ?`, workspaceID).
		Scan(&bs.WorkspaceID, &bs.Enabled, &allowJSON, &bs.ProfileMode, &bs.UpdatedAt, &bs.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return BrowserSettings{WorkspaceID: workspaceID, AllowHosts: []string{}, ProfileMode: "ephemeral"}, nil
	}
	if err != nil {
		return BrowserSettings{}, fmt.Errorf("get browser settings for workspace %d: %w", workspaceID, err)
	}
	if err := json.Unmarshal([]byte(allowJSON), &bs.AllowHosts); err != nil {
		return BrowserSettings{}, fmt.Errorf("get browser settings for workspace %d: allow_hosts is not valid JSON: %w", workspaceID, err)
	}
	if bs.AllowHosts == nil {
		bs.AllowHosts = []string{}
	}
	return bs, nil
}

// SaveBrowserSettings upserts the workspace's row. A nil allowlist is stored
// as "[]" (never "null"), an empty profile mode becomes "ephemeral", and a
// zero UpdatedAt is stamped with the current time. A workspace id that does
// not exist is refused by the workspaces foreign key.
func (s *Store) SaveBrowserSettings(bs BrowserSettings) error {
	if bs.AllowHosts == nil {
		bs.AllowHosts = []string{}
	}
	allowJSON, err := json.Marshal(bs.AllowHosts)
	if err != nil {
		return fmt.Errorf("save browser settings for workspace %d: %w", bs.WorkspaceID, err)
	}
	if bs.ProfileMode == "" {
		bs.ProfileMode = "ephemeral"
	}
	updatedAt := bs.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if _, err := s.db.Exec(`INSERT INTO browser_settings (workspace_id, enabled, allow_hosts, profile_mode, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET enabled = excluded.enabled, allow_hosts = excluded.allow_hosts,
			profile_mode = excluded.profile_mode, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		bs.WorkspaceID, bs.Enabled, string(allowJSON), bs.ProfileMode, updatedAt.UTC(), bs.UpdatedBy); err != nil {
		return fmt.Errorf("save browser settings for workspace %d: %w", bs.WorkspaceID, err)
	}
	return nil
}

// DeleteBrowserSettings removes the workspace's row (back to OFF, empty
// allowlist). Deleting when there is no row is a quiet no-op.
func (s *Store) DeleteBrowserSettings(workspaceID int64) error {
	if _, err := s.db.Exec(`DELETE FROM browser_settings WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("delete browser settings for workspace %d: %w", workspaceID, err)
	}
	return nil
}
