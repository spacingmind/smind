package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// agentProfileColumns is the agent_profiles SELECT column list;
// scanAgentProfile's Scan argument order must stay in lockstep with it.
const agentProfileColumns = "id, name, provider, permission_mode, auto_accept, thinking_level, model, notes, on_rate_limit, fallback_profile_id, orchestration_tools, instructions, skills, child_policy, child_profile_ids, created_at, updated_at"

// scanAgentProfile scans one row selected with agentProfileColumns.
func scanAgentProfile(row rowScanner) (AgentProfile, error) {
	var p AgentProfile
	var model sql.NullString
	var fallback, orch sql.NullInt64
	var skills, childIDs string
	if err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.PermissionMode, &p.AutoAccept, &p.ThinkingLevel, &model, &p.Notes, &p.OnRateLimit, &fallback, &orch, &p.Instructions, &skills, &p.ChildPolicy, &childIDs, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return AgentProfile{}, err
	}
	if orch.Valid {
		v := orch.Int64 != 0
		p.OrchestrationTools = &v
	}
	if skills != "" {
		if err := json.Unmarshal([]byte(skills), &p.Skills); err != nil {
			return AgentProfile{}, fmt.Errorf("decode profile %d skills: %w", p.ID, err)
		}
	}
	if childIDs != "" {
		if err := json.Unmarshal([]byte(childIDs), &p.ChildProfileIDs); err != nil {
			return AgentProfile{}, fmt.Errorf("decode profile %d child_profile_ids: %w", p.ID, err)
		}
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

// nullOrchBool maps a nil pointer onto SQL NULL and a bool onto 0/1.
func nullOrchBool(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}

// jsonListText encodes a list as JSON array text; an empty list is ”.
func jsonListText[T any](v []T) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
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

	skills, err := jsonListText(p.Skills)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("encode skills: %w", err)
	}
	childIDs, err := jsonListText(p.ChildProfileIDs)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("encode child profile ids: %w", err)
	}
	res, err := s.db.Exec(
		`INSERT INTO agent_profiles (name, provider, permission_mode, auto_accept, thinking_level, model, notes, on_rate_limit, fallback_profile_id, orchestration_tools, instructions, skills, child_policy, child_profile_ids, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Provider, p.PermissionMode, p.AutoAccept, p.ThinkingLevel, nullString(p.Model), p.Notes, p.OnRateLimit, nullInt64Ptr(p.FallbackProfileID),
		nullOrchBool(p.OrchestrationTools), p.Instructions, skills, p.ChildPolicy, childIDs, p.CreatedAt, p.UpdatedAt,
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
	if IsOwnedID(id) {
		return s.getOwnedProfile(id)
	}
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

	skills, err := jsonListText(p.Skills)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("encode skills: %w", err)
	}
	childIDs, err := jsonListText(p.ChildProfileIDs)
	if err != nil {
		return AgentProfile{}, fmt.Errorf("encode child profile ids: %w", err)
	}
	if _, err := s.db.Exec(
		`UPDATE agent_profiles SET name = ?, provider = ?, permission_mode = ?, auto_accept = ?, thinking_level = ?, model = ?, notes = ?, on_rate_limit = ?, fallback_profile_id = ?,
		        orchestration_tools = ?, instructions = ?, skills = ?, child_policy = ?, child_profile_ids = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Provider, p.PermissionMode, p.AutoAccept, p.ThinkingLevel, nullString(p.Model), p.Notes, p.OnRateLimit, nullInt64Ptr(p.FallbackProfileID),
		nullOrchBool(p.OrchestrationTools), p.Instructions, skills, p.ChildPolicy, childIDs, p.UpdatedAt, p.ID,
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
	if err := removeChildReference(tx, id); err != nil {
		return fmt.Errorf("delete agent profile %d: clear child references: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete agent profile %d: %w", id, err)
	}
	return nil
}

// removeChildReference drops id from every profile's child_profile_ids and
// downgrades a "profiles" policy left with an empty list to "none".
func removeChildReference(tx *sql.Tx, id int64) error {
	rows, err := tx.Query(`SELECT id, child_profile_ids FROM agent_profiles WHERE child_profile_ids != '' AND id != ?`, id)
	if err != nil {
		return err
	}
	type change struct {
		id   int64
		text string
	}
	var changes []change
	for rows.Next() {
		var pid int64
		var text string
		if err := rows.Scan(&pid, &text); err != nil {
			rows.Close()
			return err
		}
		var ids []int64
		if err := json.Unmarshal([]byte(text), &ids); err != nil {
			rows.Close()
			return fmt.Errorf("decode profile %d child_profile_ids: %w", pid, err)
		}
		kept := ids[:0:0]
		for _, c := range ids {
			if c != id {
				kept = append(kept, c)
			}
		}
		if len(kept) == len(ids) {
			continue
		}
		newText, err := jsonListText(kept)
		if err != nil {
			rows.Close()
			return err
		}
		changes = append(changes, change{pid, newText})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, c := range changes {
		if _, err := tx.Exec(
			`UPDATE agent_profiles SET child_profile_ids = ?,
			        child_policy = CASE WHEN ? = '' AND child_policy = 'profiles' THEN 'none' ELSE child_policy END
			 WHERE id = ?`, c.text, c.text, c.id,
		); err != nil {
			return err
		}
	}
	return nil
}
