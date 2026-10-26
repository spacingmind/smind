package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrPluginExists reports an insert of a plugin id that is already installed.
var ErrPluginExists = errors.New("plugin already installed")

// Plugin is one installed plugin (ADR-0042). Capabilities and Pending hold JSON
// owned by internal/plugins; the store does not interpret them.
type Plugin struct {
	ID           string
	Name         string
	Version      string
	SourceKind   string
	Source       string
	Hash         string
	Enabled      bool
	Capabilities string
	ConsentHash  string
	Pending      string
	LastError    string
	InstalledAt  time.Time
	UpdatedAt    time.Time
}

const pluginColumns = `id, name, version, source_kind, source, hash, enabled, capabilities, consent_hash, pending, last_error, installed_at, updated_at`

func scanPlugin(sc interface{ Scan(...any) error }) (Plugin, error) {
	var p Plugin
	err := sc.Scan(&p.ID, &p.Name, &p.Version, &p.SourceKind, &p.Source, &p.Hash, &p.Enabled, &p.Capabilities, &p.ConsentHash, &p.Pending, &p.LastError, &p.InstalledAt, &p.UpdatedAt)
	return p, err
}

// InsertPlugin records a new plugin, disabled with no consent. A duplicate id
// returns ErrPluginExists.
func (s *Store) InsertPlugin(p Plugin) (Plugin, error) {
	now := time.Now().UTC()
	p.InstalledAt, p.UpdatedAt, p.Enabled, p.ConsentHash, p.Pending, p.LastError = now, now, false, "", "", ""
	_, err := s.db.Exec(`INSERT INTO plugins (`+pluginColumns+`) VALUES (?, ?, ?, ?, ?, ?, 0, ?, '', '', '', ?, ?)`,
		p.ID, p.Name, p.Version, p.SourceKind, p.Source, p.Hash, p.Capabilities, p.InstalledAt, p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Plugin{}, fmt.Errorf("insert plugin %q: %w", p.ID, ErrPluginExists)
		}
		return Plugin{}, fmt.Errorf("insert plugin: %w", err)
	}
	return p, nil
}

// GetPlugin returns the plugin with the id, or sql.ErrNoRows.
func (s *Store) GetPlugin(id string) (Plugin, error) {
	p, err := scanPlugin(s.db.QueryRow(`SELECT `+pluginColumns+` FROM plugins WHERE id = ?`, id))
	if err != nil {
		return Plugin{}, fmt.Errorf("get plugin %q: %w", id, err)
	}
	return p, nil
}

// ListPlugins returns every plugin ordered by id.
func (s *Store) ListPlugins() ([]Plugin, error) {
	rows, err := s.db.Query(`SELECT ` + pluginColumns + ` FROM plugins ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	defer rows.Close()
	var out []Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, fmt.Errorf("scan plugin: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePlugin overwrites the mutable columns of an existing plugin.
func (s *Store) UpdatePlugin(p Plugin) error {
	res, err := s.db.Exec(`UPDATE plugins SET name=?, version=?, source_kind=?, source=?, hash=?, enabled=?, capabilities=?, consent_hash=?, pending=?, last_error=?, updated_at=? WHERE id=?`,
		p.Name, p.Version, p.SourceKind, p.Source, p.Hash, p.Enabled, p.Capabilities, p.ConsentHash, p.Pending, p.LastError, time.Now().UTC(), p.ID)
	if err != nil {
		return fmt.Errorf("update plugin %q: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update plugin %q: %w", p.ID, sql.ErrNoRows)
	}
	return nil
}

// DeletePlugin removes the plugin's row; a missing row is not an error.
func (s *Store) DeletePlugin(id string) error {
	if _, err := s.db.Exec(`DELETE FROM plugins WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete plugin %q: %w", id, err)
	}
	return nil
}

// PluginSetting is one stored plugin setting value: JSON text (a string, a
// number or a boolean). Secrets are stored like provider credentials: a plain
// column of this database.
type PluginSetting struct {
	Key   string
	Value string
}

// ListPluginSettings returns a plugin's stored setting values ordered by key.
func (s *Store) ListPluginSettings(pluginID string) ([]PluginSetting, error) {
	rows, err := s.db.Query(`SELECT key, value FROM plugin_settings WHERE plugin_id = ? ORDER BY key`, pluginID)
	if err != nil {
		return nil, fmt.Errorf("list plugin settings %q: %w", pluginID, err)
	}
	defer rows.Close()
	var out []PluginSetting
	for rows.Next() {
		var ps PluginSetting
		if err := rows.Scan(&ps.Key, &ps.Value); err != nil {
			return nil, fmt.Errorf("scan plugin setting: %w", err)
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// ApplyPluginSettings writes set (key -> JSON value) and deletes the keys in
// clear in one transaction, so a failed write changes nothing.
func (s *Store) ApplyPluginSettings(pluginID string, set map[string]string, clear []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("apply plugin settings %q: %w", pluginID, err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	for k, v := range set {
		if _, err := tx.Exec(`INSERT INTO plugin_settings (plugin_id, key, value, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, pluginID, k, v, now); err != nil {
			return fmt.Errorf("apply plugin settings %q: %w", pluginID, err)
		}
	}
	for _, k := range clear {
		if _, err := tx.Exec(`DELETE FROM plugin_settings WHERE plugin_id = ? AND key = ?`, pluginID, k); err != nil {
			return fmt.Errorf("apply plugin settings %q: %w", pluginID, err)
		}
	}
	return tx.Commit()
}

// DeletePluginSettings removes every stored value of a plugin.
func (s *Store) DeletePluginSettings(pluginID string) error {
	if _, err := s.db.Exec(`DELETE FROM plugin_settings WHERE plugin_id = ?`, pluginID); err != nil {
		return fmt.Errorf("delete plugin settings %q: %w", pluginID, err)
	}
	return nil
}
