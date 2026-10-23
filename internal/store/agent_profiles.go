package store

import (
	"database/sql"
	"fmt"
	"time"
)

// agentProfileColumns is the agent_profiles SELECT column list;
// scanAgentProfile's Scan argument order must stay in lockstep with it.
const agentProfileColumns = "id, name, provider, permission_mode, auto_accept, thinking_level, model, notes, on_rate_limit, fallback_profile_id, created_at, updated_at"

// scanAgentProfile scans one row selected with agentProfileColumns.
func scanAgentProfile(row rowScanner) (AgentProfile, error) {
	var p AgentProfile
	var model sql.NullString
	var fallback sql.NullInt64
	if err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.PermissionMode, &p.AutoAccept, &p.ThinkingLevel, &model, &p.Notes, &p.OnRateLimit, &fallback, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return AgentProfile{}, err
	}
	p.Model = model.String
	if fallback.Valid {
		v := fallback.Int64
		p.FallbackProfileID = &v
	}
	return p, nil
}

// nullInt64Ptr maps a nil pointer onto SQL NULL.
func nullInt64Ptr(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// CreateAgentProfile inserts a new agent profile, stamping
// created_at/updated_at. p.ID is ignored on input and set to the assigned
// row id on return.
func (s *Store) CreateAgentProfile(p AgentProfile) (AgentProfile, error) {
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	if p.OnRateLimit == "" {
		p.OnRateLimit = "none"
	}

	res, err := s.db.Exec(
		`INSERT INTO agent_profiles (name, provider, permission_mode, auto_accept, thinking_level, model, notes, on_rate_limit, fallback_profile_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Provider, p.PermissionMode, p.AutoAccept, p.ThinkingLevel, nullString(p.Model), p.Notes, p.OnRateLimit, nullInt64Ptr(p.FallbackProfileID), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("insert agent profile: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentProfile{}, fmt.Errorf("agent profile id: %w", err)
	}
	p.ID = id
	return p, nil
}

// GetAgentProfile returns the agent profile with the given id.
func (s *Store) GetAgentProfile(id int64) (AgentProfile, error) {
	p, err := scanAgentProfile(s.db.QueryRow(
		`SELECT `+agentProfileColumns+` FROM agent_profiles WHERE id = ?`, id,
	))
	if err != nil {
		return AgentProfile{}, fmt.Errorf("get agent profile %d: %w", id, err)
	}
	return p, nil
}

// ListAgentProfiles returns every agent profile, ordered by id.
func (s *Store) ListAgentProfiles() ([]AgentProfile, error) {
	rows, err := s.db.Query(`SELECT ` + agentProfileColumns + ` FROM agent_profiles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]AgentProfile, 0)
	for rows.Next() {
		p, err := scanAgentProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// UpdateAgentProfile replaces every field of the agent profile p.ID (a
// full-record replace, not a partial patch -- see ADR-0014's wsapi surface
// section), preserving its original created_at. Updating a nonexistent id
// is a clear not-found error, never a silent no-op.
func (s *Store) UpdateAgentProfile(p AgentProfile) (AgentProfile, error) {
	existing, err := s.GetAgentProfile(p.ID)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("update agent profile %d: %w", p.ID, err)
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	if p.OnRateLimit == "" {
		p.OnRateLimit = "none"
	}

	if _, err := s.db.Exec(
		`UPDATE agent_profiles SET name = ?, provider = ?, permission_mode = ?, auto_accept = ?, thinking_level = ?, model = ?, notes = ?, on_rate_limit = ?, fallback_profile_id = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Provider, p.PermissionMode, p.AutoAccept, p.ThinkingLevel, nullString(p.Model), p.Notes, p.OnRateLimit, nullInt64Ptr(p.FallbackProfileID), p.UpdatedAt, p.ID,
	); err != nil {
		return AgentProfile{}, fmt.Errorf("update agent profile %d: %w", p.ID, err)
	}
	return p, nil
}

// DeleteAgentProfile permanently removes the agent profile with the given
// id. Deleting a nonexistent id is a clear not-found error (via
// GetAgentProfile), never a silent no-op -- matching DeleteSpace/
// DeleteWorkspace's convention. In the same transaction, every profile that
// named it as its fallback loses the reference (fallback_profile_id = NULL)
// and a 'fallback' policy is downgraded to 'none'; runs.profile_id is left
// untouched (history).
func (s *Store) DeleteAgentProfile(id int64) error {
	if _, err := s.GetAgentProfile(id); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.Exec(
		`UPDATE agent_profiles SET fallback_profile_id = NULL,
		        on_rate_limit = CASE WHEN on_rate_limit = 'fallback' THEN 'none' ELSE on_rate_limit END
		 WHERE fallback_profile_id = ?`, id,
	); err != nil {
		return fmt.Errorf("delete agent profile %d: clear fallback references: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	return nil
}
