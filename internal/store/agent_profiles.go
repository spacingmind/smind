package store

import (
	"fmt"
	"time"
)

// CreateAgentProfile inserts a new agent profile, stamping
// created_at/updated_at. p.ID is ignored on input and set to the assigned
// row id on return.
func (s *Store) CreateAgentProfile(p AgentProfile) (AgentProfile, error) {
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO agent_profiles (name, provider, approval_policy, thinking_level, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Provider, p.ApprovalPolicy, p.ThinkingLevel, p.Notes, p.CreatedAt, p.UpdatedAt,
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
	var p AgentProfile
	err := s.db.QueryRow(
		`SELECT id, name, provider, approval_policy, thinking_level, notes, created_at, updated_at
		 FROM agent_profiles WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Provider, &p.ApprovalPolicy, &p.ThinkingLevel, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("get agent profile %d: %w", id, err)
	}
	return p, nil
}

// ListAgentProfiles returns every agent profile, ordered by id.
func (s *Store) ListAgentProfiles() ([]AgentProfile, error) {
	rows, err := s.db.Query(
		`SELECT id, name, provider, approval_policy, thinking_level, notes, created_at, updated_at
		 FROM agent_profiles ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]AgentProfile, 0)
	for rows.Next() {
		var p AgentProfile
		if err := rows.Scan(&p.ID, &p.Name, &p.Provider, &p.ApprovalPolicy, &p.ThinkingLevel, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err != nil {
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

	if _, err := s.db.Exec(
		`UPDATE agent_profiles SET name = ?, provider = ?, approval_policy = ?, thinking_level = ?, notes = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Provider, p.ApprovalPolicy, p.ThinkingLevel, p.Notes, p.UpdatedAt, p.ID,
	); err != nil {
		return AgentProfile{}, fmt.Errorf("update agent profile %d: %w", p.ID, err)
	}
	return p, nil
}

// DeleteAgentProfile permanently removes the agent profile with the given
// id. Deleting a nonexistent id is a clear not-found error (via
// GetAgentProfile), never a silent no-op -- matching DeleteSpace/
// DeleteWorkspace's convention. Nothing else references an agent_profiles
// row, so there is no cascade.
func (s *Store) DeleteAgentProfile(id int64) error {
	if _, err := s.GetAgentProfile(id); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	return nil
}
