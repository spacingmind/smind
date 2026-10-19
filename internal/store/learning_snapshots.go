package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// defaultSnapshotsKept is how many library snapshots a workspace keeps
// (ADR-0026 section 6).
const defaultSnapshotsKept = 5

// ErrSnapshotWorkspaceMismatch: a snapshot names a skill id that now belongs
// to another workspace; restoring would touch foreign rows.
var ErrSnapshotWorkspaceMismatch = errors.New("snapshot skill belongs to another workspace")

// LearningSnapshot is one JSON export of a workspace's learned skills (every
// status) and their versions, written before a curator run. Payload is empty
// in ListLearningSnapshots results.
type LearningSnapshot struct {
	ID          int64
	WorkspaceID int64
	CreatedAt   time.Time
	Reason      string
	Payload     string
}

// RestoreResult reports what RestoreLearningSnapshot did, by skill name.
type RestoreResult struct {
	// Restored are skills whose text, category, status or pin changed back.
	Restored []string
	// Untouched are skills that exist now but not in the snapshot (created
	// after it); they are never changed.
	Untouched []string
}

type snapshotPayload struct {
	Skills   []LearnedSkill        `json:"skills"`
	Versions []LearnedSkillVersion `json:"versions"`
}

// CreateLearningSnapshot serializes the workspace's learned_skills rows
// (every status) and their versions into one snapshot row and deletes the
// workspace's older snapshots beyond keep (keep <= 0 means 5), in one
// transaction.
func (s *Store) CreateLearningSnapshot(workspaceID int64, reason string, keep int) (LearningSnapshot, error) {
	if keep <= 0 {
		keep = defaultSnapshotsKept
	}
	tx, err := s.db.Begin()
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: begin: %w", workspaceID, err)
	}
	defer tx.Rollback()

	payload := snapshotPayload{Skills: []LearnedSkill{}, Versions: []LearnedSkillVersion{}}
	rows, err := tx.Query(`SELECT `+learnedSkillColumns+` FROM learned_skills WHERE workspace_id = ? ORDER BY id`, workspaceID)
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: skills: %w", workspaceID, err)
	}
	for rows.Next() {
		k, err := scanLearnedSkill(rows)
		if err != nil {
			rows.Close()
			return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: scan skill: %w", workspaceID, err)
		}
		payload.Skills = append(payload.Skills, k)
	}
	if err := rows.Close(); err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: %w", workspaceID, err)
	}
	vrows, err := tx.Query(`SELECT v.skill_id, v.version, v.description, v.body, v.actor, v.created_at
		FROM learned_skill_versions v JOIN learned_skills k ON k.id = v.skill_id
		WHERE k.workspace_id = ? ORDER BY v.skill_id, v.version`, workspaceID)
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: versions: %w", workspaceID, err)
	}
	for vrows.Next() {
		var v LearnedSkillVersion
		if err := vrows.Scan(&v.SkillID, &v.Version, &v.Description, &v.Body, &v.Actor, &v.CreatedAt); err != nil {
			vrows.Close()
			return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: scan version: %w", workspaceID, err)
		}
		payload.Versions = append(payload.Versions, v)
	}
	if err := vrows.Close(); err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: %w", workspaceID, err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: encode: %w", workspaceID, err)
	}

	now := time.Now().UTC()
	res, err := tx.Exec(`INSERT INTO learning_snapshots (workspace_id, created_at, reason, payload) VALUES (?, ?, ?, ?)`,
		workspaceID, now, reason, string(raw))
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: insert: %w", workspaceID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: id: %w", workspaceID, err)
	}
	if _, err := tx.Exec(`DELETE FROM learning_snapshots WHERE workspace_id = ? AND id NOT IN
		(SELECT id FROM learning_snapshots WHERE workspace_id = ? ORDER BY id DESC LIMIT ?)`,
		workspaceID, workspaceID, keep); err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: rotate: %w", workspaceID, err)
	}
	if err := tx.Commit(); err != nil {
		return LearningSnapshot{}, fmt.Errorf("snapshot workspace %d: commit: %w", workspaceID, err)
	}
	return LearningSnapshot{ID: id, WorkspaceID: workspaceID, CreatedAt: now, Reason: reason, Payload: string(raw)}, nil
}

// ListLearningSnapshots returns the workspace's snapshots newest first,
// without payloads.
func (s *Store) ListLearningSnapshots(workspaceID int64) ([]LearningSnapshot, error) {
	rows, err := s.db.Query(`SELECT id, workspace_id, created_at, reason FROM learning_snapshots
		WHERE workspace_id = ? ORDER BY id DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list snapshots for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()
	out := make([]LearningSnapshot, 0)
	for rows.Next() {
		var sn LearningSnapshot
		if err := rows.Scan(&sn.ID, &sn.WorkspaceID, &sn.CreatedAt, &sn.Reason); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// GetLearningSnapshot returns one snapshot including its payload.
func (s *Store) GetLearningSnapshot(id int64) (LearningSnapshot, error) {
	var sn LearningSnapshot
	err := s.db.QueryRow(`SELECT id, workspace_id, created_at, reason, payload FROM learning_snapshots WHERE id = ?`, id).
		Scan(&sn.ID, &sn.WorkspaceID, &sn.CreatedAt, &sn.Reason, &sn.Payload)
	if err != nil {
		return LearningSnapshot{}, fmt.Errorf("get snapshot %d: %w", id, err)
	}
	return sn, nil
}

// RestoreLearningSnapshot sets every skill that exists now and in the
// snapshot back to the snapshot's description, body, category, status and
// pinned flag. A text change is a new version (the next number, so history
// is kept) and every changed skill gets one ledger row with action
// "restore-snapshot". Skills created after the snapshot are left alone
// (Untouched); skills hard-deleted since are NOT resurrected. The whole
// restore is one transaction: any failure (e.g. a restored status colliding
// with a live skill of the same name, or a snapshot skill id that now
// belongs to another workspace) changes nothing.
func (s *Store) RestoreLearningSnapshot(id int64, actor string) (RestoreResult, error) {
	sn, err := s.GetLearningSnapshot(id)
	if err != nil {
		return RestoreResult{}, err
	}
	var payload snapshotPayload
	if err := json.Unmarshal([]byte(sn.Payload), &payload); err != nil {
		return RestoreResult{}, fmt.Errorf("restore snapshot %d: decode: %w", id, err)
	}
	sort.Slice(payload.Skills, func(i, j int) bool { return payload.Skills[i].ID < payload.Skills[j].ID })

	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore snapshot %d: begin: %w", id, err)
	}
	defer tx.Rollback()

	res := RestoreResult{Restored: []string{}, Untouched: []string{}}
	inSnapshot := make(map[int64]bool, len(payload.Skills))
	for _, snap := range payload.Skills {
		inSnapshot[snap.ID] = true
		cur, err := scanLearnedSkill(tx.QueryRow(`SELECT `+learnedSkillColumns+` FROM learned_skills WHERE id = ?`, snap.ID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // hard-deleted since: not resurrected
			}
			return RestoreResult{}, fmt.Errorf("restore snapshot %d: skill %d: %w", id, snap.ID, err)
		}
		if cur.WorkspaceID != sn.WorkspaceID {
			return RestoreResult{}, fmt.Errorf("restore snapshot %d: skill %d: %w", id, snap.ID, ErrSnapshotWorkspaceMismatch)
		}
		textChanged := cur.Description != snap.Description || cur.Body != snap.Body
		if !textChanged && cur.Category == snap.Category && cur.Status == snap.Status && cur.Pinned == snap.Pinned {
			continue
		}
		version := cur.Version
		if textChanged {
			version++
			if _, err := tx.Exec(`INSERT INTO learned_skill_versions (skill_id, version, description, body, actor, created_at)
				VALUES (?, ?, ?, ?, ?, ?)`, cur.ID, version, snap.Description, snap.Body, actor, now); err != nil {
				return RestoreResult{}, fmt.Errorf("restore snapshot %d: skill %d: version: %w", id, cur.ID, err)
			}
		}
		if _, err := tx.Exec(`UPDATE learned_skills SET description = ?, body = ?, category = ?, status = ?, pinned = ?,
			version = ?, updated_at = ? WHERE id = ?`,
			snap.Description, snap.Body, snap.Category, snap.Status, snap.Pinned, version, now, cur.ID); err != nil {
			return RestoreResult{}, fmt.Errorf("restore snapshot %d: skill %d: %w", id, cur.ID, err)
		}
		if err := insertLedger(tx, LearnedLedgerEntry{
			WorkspaceID: cur.WorkspaceID, SkillID: &cur.ID, SkillName: cur.Name, Action: "restore-snapshot",
			Reason: fmt.Sprintf("snapshot %d: %s", id, restoredParts(cur, snap, textChanged)), Evidence: "[]", Actor: actor, CreatedAt: now,
		}); err != nil {
			return RestoreResult{}, fmt.Errorf("restore snapshot %d: %w", id, err)
		}
		res.Restored = append(res.Restored, cur.Name)
	}

	rows, err := tx.Query(`SELECT id, name FROM learned_skills WHERE workspace_id = ? ORDER BY id`, sn.WorkspaceID)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore snapshot %d: list: %w", id, err)
	}
	for rows.Next() {
		var sid int64
		var name string
		if err := rows.Scan(&sid, &name); err != nil {
			rows.Close()
			return RestoreResult{}, fmt.Errorf("restore snapshot %d: scan: %w", id, err)
		}
		if !inSnapshot[sid] {
			res.Untouched = append(res.Untouched, name)
		}
	}
	if err := rows.Close(); err != nil {
		return RestoreResult{}, fmt.Errorf("restore snapshot %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return RestoreResult{}, fmt.Errorf("restore snapshot %d: commit: %w", id, err)
	}
	return res, nil
}

func restoredParts(cur, snap LearnedSkill, textChanged bool) string {
	var parts []string
	if textChanged {
		parts = append(parts, "text")
	}
	if cur.Category != snap.Category {
		parts = append(parts, "category")
	}
	if cur.Status != snap.Status {
		parts = append(parts, "status "+cur.Status+" -> "+snap.Status)
	}
	if cur.Pinned != snap.Pinned {
		parts = append(parts, "pinned")
	}
	return strings.Join(parts, ", ")
}
