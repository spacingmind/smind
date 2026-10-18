package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSkillVersionConflict reports a PatchSkill whose ExpectVersion doesn't
// match the row's current version -- the optimistic-concurrency guard so a
// concurrent editor's patch can't be silently clobbered.
var ErrSkillVersionConflict = errors.New("learned skill version conflict")

const learnedSkillColumns = `id, workspace_id, name, category, description, body, status, pinned, source,
	created_by_run, evidence, possible_duplicate_of, similarity, version,
	offered_runs, loaded_runs, loads, loads_via_skill, loads_via_read,
	patched, reused_after_patch, measured, last_offered_at, last_loaded_at,
	created_at, updated_at`

func scanLearnedSkill(row interface{ Scan(...any) error }) (LearnedSkill, error) {
	var k LearnedSkill
	err := row.Scan(&k.ID, &k.WorkspaceID, &k.Name, &k.Category, &k.Description, &k.Body, &k.Status, &k.Pinned, &k.Source,
		&k.CreatedByRun, &k.Evidence, &k.PossibleDuplicateOf, &k.Similarity, &k.Version,
		&k.OfferedRuns, &k.LoadedRuns, &k.Loads, &k.LoadsViaSkill, &k.LoadsViaRead,
		&k.Patched, &k.ReusedAfterPatch, &k.Measured, &k.LastOfferedAt, &k.LastLoadedAt,
		&k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return LearnedSkill{}, err
	}
	return k, nil
}

func isLearnedSkillStatus(s string) bool {
	return s == SkillActive || s == SkillDisabled || s == SkillArchived
}

// SetWorkspaceLearningMode sets workspaces.learning_mode ('off' or
// 'propose'); anything else is rejected rather than stored.
func (s *Store) SetWorkspaceLearningMode(workspaceID int64, mode string) error {
	if mode != LearningModeOff && mode != LearningModePropose {
		return fmt.Errorf("set workspace %d learning mode: unknown mode %q", workspaceID, mode)
	}
	res, err := s.db.Exec(`UPDATE workspaces SET learning_mode = ?, updated_at = ? WHERE id = ?`, mode, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("set workspace %d learning mode: %w", workspaceID, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("set workspace %d learning mode: %w", workspaceID, sql.ErrNoRows)
	}
	return nil
}

// GetWorkspaceLearningMode returns workspaces.learning_mode.
func (s *Store) GetWorkspaceLearningMode(workspaceID int64) (string, error) {
	var mode string
	if err := s.db.QueryRow(`SELECT learning_mode FROM workspaces WHERE id = ?`, workspaceID).Scan(&mode); err != nil {
		return "", fmt.Errorf("get workspace %d learning mode: %w", workspaceID, err)
	}
	return mode, nil
}

// ListLearnedSkills returns the workspace's skills ordered by id, filtered
// to the given statuses when any are passed (no statuses = all).
func (s *Store) ListLearnedSkills(workspaceID int64, statuses ...string) ([]LearnedSkill, error) {
	q := `SELECT ` + learnedSkillColumns + ` FROM learned_skills WHERE workspace_id = ?`
	args := []any{workspaceID}
	if len(statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list learned skills for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	skills := make([]LearnedSkill, 0)
	for rows.Next() {
		k, err := scanLearnedSkill(rows)
		if err != nil {
			return nil, fmt.Errorf("scan learned skill: %w", err)
		}
		skills = append(skills, k)
	}
	return skills, rows.Err()
}

// GetLearnedSkill returns the skill with the given id.
func (s *Store) GetLearnedSkill(id int64) (LearnedSkill, error) {
	k, err := scanLearnedSkill(s.db.QueryRow(`SELECT `+learnedSkillColumns+` FROM learned_skills WHERE id = ?`, id))
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("get learned skill %d: %w", id, err)
	}
	return k, nil
}

// LandSkillInput approves a create proposal (or, for human-initiated
// creates, carries ProposalID 0) into an approved learned_skill row, all in
// one transaction: the skill row (version 1), its first version row, and a
// ledger row. Action selects the ledger action: "create" for a new skill,
// "approve" when landing a previously proposed one.
type LandSkillInput struct {
	Skill      LearnedSkill
	Actor      string
	Reason     string
	Action     string
	ProposalID int64
}

// LandSkill inserts an approved learned skill plus its v1 version row and
// ledger entry in one transaction. Any failure (duplicate live name, a
// non-pending proposal, ...) rolls back everything: nothing is written.
func (s *Store) LandSkill(in LandSkillInput) (LearnedSkill, error) {
	if in.Action == "" {
		in.Action = "create"
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill: begin: %w", err)
	}
	defer tx.Rollback()

	if err := approveProposalInTx(tx, in.ProposalID); err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: %w", in.Skill.Name, err)
	}

	res, err := tx.Exec(`INSERT INTO learned_skills
		(workspace_id, name, category, description, body, status, pinned, source, created_by_run,
		 evidence, possible_duplicate_of, similarity, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		in.Skill.WorkspaceID, in.Skill.Name, in.Skill.Category, in.Skill.Description, in.Skill.Body,
		SkillActive, in.Skill.Pinned, in.Skill.Source, in.Skill.CreatedByRun,
		orEmptyJSON(in.Skill.Evidence), in.Skill.PossibleDuplicateOf, in.Skill.Similarity, now, now)
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: insert: %w", in.Skill.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: id: %w", in.Skill.Name, err)
	}
	if _, err := tx.Exec(`INSERT INTO learned_skill_versions (skill_id, version, description, body, actor, created_at)
		VALUES (?, 1, ?, ?, ?, ?)`, id, in.Skill.Description, in.Skill.Body, in.Actor, now); err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: insert version: %w", in.Skill.Name, err)
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: in.Skill.WorkspaceID, SkillID: &id, SkillName: in.Skill.Name,
		Action: in.Action, Reason: in.Reason, Evidence: orEmptyJSON(in.Skill.Evidence),
		RunID: in.Skill.CreatedByRun, Actor: in.Actor, CreatedAt: now,
	}); err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: %w", in.Skill.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return LearnedSkill{}, fmt.Errorf("land skill %q: commit: %w", in.Skill.Name, err)
	}
	return s.GetLearnedSkill(id)
}

func orEmptyJSON(v string) string {
	if v == "" {
		return "[]"
	}
	return v
}

// PatchSkillInput patches one skill (id) to a new description/body as a new
// version. ExpectVersion is the optimistic-concurrency guard; Action is the
// ledger action ("patch", "merge" or "undo"). A nonzero ProposalID marks
// that proposal approved in the same transaction.
type PatchSkillInput struct {
	ID            int64
	ExpectVersion int
	Description   string
	Body          string
	Actor         string
	Action        string
	Reason        string
	ProposalID    int64
}

// PatchSkill applies a patch atomically: version+1, patched+1, a new
// versions row, a ledger row, and (when ProposalID != 0) the proposal
// marked approved. Nothing is written on any error.
func (s *Store) PatchSkill(in PatchSkillInput) (LearnedSkill, error) {
	if in.Action == "" {
		in.Action = "patch"
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: begin: %w", in.ID, err)
	}
	defer tx.Rollback()

	if err := approveProposalInTx(tx, in.ProposalID); err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: %w", in.ID, err)
	}

	var current int
	if err := tx.QueryRow(`SELECT version FROM learned_skills WHERE id = ?`, in.ID).Scan(&current); err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: %w", in.ID, err)
	}
	if current != in.ExpectVersion {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: have version %d: %w", in.ID, current, ErrSkillVersionConflict)
	}

	res, err := tx.Exec(`UPDATE learned_skills
		SET description = ?, body = ?, version = version + 1, patched = patched + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		in.Description, in.Body, now, in.ID, in.ExpectVersion)
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: %w", in.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: have version %d: %w", in.ID, in.ExpectVersion, ErrSkillVersionConflict)
	}
	if _, err := tx.Exec(`INSERT INTO learned_skill_versions (skill_id, version, description, body, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, in.ID, in.ExpectVersion+1, in.Description, in.Body, in.Actor, now); err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: insert version: %w", in.ID, err)
	}
	k, err := scanLearnedSkill(tx.QueryRow(`SELECT `+learnedSkillColumns+` FROM learned_skills WHERE id = ?`, in.ID))
	if err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: %w", in.ID, err)
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: k.WorkspaceID, SkillID: &k.ID, SkillName: k.Name,
		Action: in.Action, Reason: in.Reason, Evidence: "[]", Actor: in.Actor, CreatedAt: now,
	}); err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: %w", in.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return LearnedSkill{}, fmt.Errorf("patch skill %d: commit: %w", in.ID, err)
	}
	return k, nil
}

// approveProposalInTx marks proposalID approved if it is currently pending.
// A zero id is a no-op (human-initiated change, no proposal). A missing or
// non-pending proposal is an error, which rolls back the enclosing
// transaction.
func approveProposalInTx(tx *sql.Tx, proposalID int64) error {
	if proposalID == 0 {
		return nil
	}
	res, err := tx.Exec(`UPDATE learned_skill_proposals
		SET status = 'approved', decided_at = ?
		WHERE id = ? AND status = 'pending'`, time.Now().UTC(), proposalID)
	if err != nil {
		return fmt.Errorf("approve proposal %d: %w", proposalID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("approve proposal %d: %w", proposalID, sql.ErrNoRows)
	}
	return nil
}

// ApplyBundleInput approves a bundle (merge) proposal: the umbrella skill
// is patched to the merged body (ledger action "merge") and every absorbed
// skill is archived with absorbed_into set to the umbrella's name, all in
// one transaction with the proposal approval.
type ApplyBundleInput struct {
	ProposalID    int64
	UmbrellaID    int64
	ExpectVersion int
	Description   string
	Body          string
	Actor         string
	Reason        string
	AbsorbedIDs   []int64
}

// ApplyBundle applies a merge bundle atomically: any failure (e.g. an
// absorbed skill from another workspace) rolls back everything, including
// the umbrella patch and the proposal approval.
func (s *Store) ApplyBundle(in ApplyBundleInput) error {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("apply bundle: begin: %w", err)
	}
	defer tx.Rollback()

	if err := approveProposalInTx(tx, in.ProposalID); err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}

	var current int
	var umbrellaName string
	if err := tx.QueryRow(`SELECT version, name FROM learned_skills WHERE id = ?`, in.UmbrellaID).Scan(&current, &umbrellaName); err != nil {
		return fmt.Errorf("apply bundle: umbrella %d: %w", in.UmbrellaID, err)
	}
	if current != in.ExpectVersion {
		return fmt.Errorf("apply bundle: umbrella %d: have version %d: %w", in.UmbrellaID, current, ErrSkillVersionConflict)
	}
	if _, err := tx.Exec(`UPDATE learned_skills
		SET description = ?, body = ?, version = version + 1, patched = patched + 1, updated_at = ?
		WHERE id = ?`, in.Description, in.Body, now, in.UmbrellaID); err != nil {
		return fmt.Errorf("apply bundle: patch umbrella %d: %w", in.UmbrellaID, err)
	}
	if _, err := tx.Exec(`INSERT INTO learned_skill_versions (skill_id, version, description, body, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, in.UmbrellaID, current+1, in.Description, in.Body, in.Actor, now); err != nil {
		return fmt.Errorf("apply bundle: umbrella version: %w", err)
	}
	var wsID int64
	if err := tx.QueryRow(`SELECT workspace_id FROM learned_skills WHERE id = ?`, in.UmbrellaID).Scan(&wsID); err != nil {
		return fmt.Errorf("apply bundle: umbrella %d: %w", in.UmbrellaID, err)
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: wsID, SkillID: &in.UmbrellaID, SkillName: umbrellaName,
		Action: "merge", Reason: in.Reason, Evidence: "[]", Actor: in.Actor, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("apply bundle: %w", err)
	}

	for _, absorbed := range in.AbsorbedIDs {
		var absorbedWS int64
		var absorbedName string
		q := `SELECT workspace_id, name FROM learned_skills WHERE id = ?`
		if err := tx.QueryRow(q, absorbed).Scan(&absorbedWS, &absorbedName); err != nil {
			return fmt.Errorf("apply bundle: absorbed %d: %w", absorbed, err)
		}
		if absorbedWS != wsID {
			return fmt.Errorf("apply bundle: absorbed %d: workspace %d: %w", absorbed, absorbedWS, errCrossWorkspaceSkill)
		}
		if _, err := tx.Exec(`UPDATE learned_skills SET status = ?, updated_at = ? WHERE id = ?`, SkillArchived, now, absorbed); err != nil {
			return fmt.Errorf("apply bundle: archive absorbed %d: %w", absorbed, err)
		}
		sid := absorbed
		if err := insertLedger(tx, LearnedLedgerEntry{
			WorkspaceID: wsID, SkillID: &sid, SkillName: absorbedName,
			Action: "archive", Reason: in.Reason, AbsorbedInto: umbrellaName, Evidence: "[]", Actor: in.Actor, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("apply bundle: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("apply bundle: commit: %w", err)
	}
	return nil
}

// errCrossWorkspaceSkill reports an ApplyBundle whose absorbed skill lives
// in a different workspace than the umbrella.
var errCrossWorkspaceSkill = errors.New("absorbed skill in different workspace")

// SetSkillStatus transitions a skill's status and appends the matching
// ledger row (archive/restore/disable/enable). Valid transitions:
// active<->disabled, active|disabled->archived, archived->active.
func (s *Store) SetSkillStatus(id int64, status, actor, reason, absorbedInto string) error {
	if !isLearnedSkillStatus(status) {
		return fmt.Errorf("set learned skill %d status: unknown status %q", id, status)
	}
	k, err := s.GetLearnedSkill(id)
	if err != nil {
		return fmt.Errorf("set learned skill %d status: %w", id, err)
	}
	action := map[[2]string]string{
		{SkillActive, SkillArchived}:   "archive",
		{SkillDisabled, SkillArchived}: "archive",
		{SkillArchived, SkillActive}:   "restore",
		{SkillActive, SkillDisabled}:   "disable",
		{SkillDisabled, SkillActive}:   "enable",
	}[[2]string{k.Status, status}]
	if action == "" {
		return fmt.Errorf("set learned skill %d status: invalid transition %s -> %s", id, k.Status, status)
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set learned skill %d status: begin: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE learned_skills SET status = ?, updated_at = ? WHERE id = ?`, status, now, id); err != nil {
		return fmt.Errorf("set learned skill %d status: %w", id, err)
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: k.WorkspaceID, SkillID: &k.ID, SkillName: k.Name,
		Action: action, Reason: reason, AbsorbedInto: absorbedInto, Evidence: "[]", Actor: actor, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("set learned skill %d status: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set learned skill %d status: commit: %w", id, err)
	}
	return nil
}

// SetSkillPinned toggles the skill's pinned flag, appending a "pin"/"unpin"
// ledger row.
func (s *Store) SetSkillPinned(id int64, pinned bool, actor string) error {
	k, err := s.GetLearnedSkill(id)
	if err != nil {
		return fmt.Errorf("set learned skill %d pinned: %w", id, err)
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set learned skill %d pinned: begin: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE learned_skills SET pinned = ?, updated_at = ? WHERE id = ?`, pinned, now, id); err != nil {
		return fmt.Errorf("set learned skill %d pinned: %w", id, err)
	}
	action := "unpin"
	if pinned {
		action = "pin"
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: k.WorkspaceID, SkillID: &k.ID, SkillName: k.Name,
		Action: action, Evidence: "[]", Actor: actor, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("set learned skill %d pinned: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set learned skill %d pinned: commit: %w", id, err)
	}
	return nil
}

// DeleteLearnedSkill hard-deletes the skill and its version rows; the
// ledger row (action "delete", skill_id and skill_name retained) survives,
// since the ledger has no FK to learned_skills by design.
func (s *Store) DeleteLearnedSkill(id int64, actor string) error {
	k, err := s.GetLearnedSkill(id)
	if err != nil {
		return fmt.Errorf("delete learned skill %d: %w", id, err)
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete learned skill %d: begin: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM learned_skill_versions WHERE skill_id = ?`, id); err != nil {
		return fmt.Errorf("delete learned skill %d: versions: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM learned_skills WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete learned skill %d: %w", id, err)
	}
	if err := insertLedger(tx, LearnedLedgerEntry{
		WorkspaceID: k.WorkspaceID, SkillID: &k.ID, SkillName: k.Name,
		Action: "delete", Evidence: "[]", Actor: actor, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("delete learned skill %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete learned skill %d: commit: %w", id, err)
	}
	return nil
}

// ListSkillVersions returns the skill's versions, newest first.
func (s *Store) ListSkillVersions(skillID int64) ([]LearnedSkillVersion, error) {
	rows, err := s.db.Query(`SELECT skill_id, version, description, body, actor, created_at
		FROM learned_skill_versions WHERE skill_id = ? ORDER BY version DESC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("list versions of skill %d: %w", skillID, err)
	}
	defer rows.Close()

	versions := make([]LearnedSkillVersion, 0)
	for rows.Next() {
		var v LearnedSkillVersion
		if err := rows.Scan(&v.SkillID, &v.Version, &v.Description, &v.Body, &v.Actor, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan skill version: %w", err)
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

const ledgerColumns = `id, workspace_id, skill_id, skill_name, action, reason, evidence, absorbed_into, run_id, actor, created_at`

func scanLedger(row interface{ Scan(...any) error }) (LearnedLedgerEntry, error) {
	var e LearnedLedgerEntry
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.SkillID, &e.SkillName, &e.Action, &e.Reason, &e.Evidence, &e.AbsorbedInto, &e.RunID, &e.Actor, &e.CreatedAt)
	return e, err
}

// ListLedger returns ledger entries for a workspace (skillID 0 = the whole
// workspace's), newest first, up to limit (limit <= 0 = no limit).
func (s *Store) ListLedger(workspaceID, skillID int64, limit int) ([]LearnedLedgerEntry, error) {
	q := `SELECT ` + ledgerColumns + ` FROM learned_skill_ledger WHERE workspace_id = ?`
	args := []any{workspaceID}
	if skillID != 0 {
		q += ` AND skill_id = ?`
		args = append(args, skillID)
	}
	q += ` ORDER BY id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list ledger for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	entries := make([]LearnedLedgerEntry, 0)
	for rows.Next() {
		e, err := scanLedger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AppendLedger appends one raw ledger entry outside any transaction.
func (s *Store) AppendLedger(e LearnedLedgerEntry) error {
	if err := insertLedger(s.db, e); err != nil {
		return fmt.Errorf("append ledger: %w", err)
	}
	return nil
}

// execer is satisfied by both *sql.DB and *sql.Tx, so ledger inserts share
// one implementation inside and outside transactions.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertLedger(db execer, e LearnedLedgerEntry) error {
	_, err := db.Exec(`INSERT INTO learned_skill_ledger
		(workspace_id, skill_id, skill_name, action, reason, evidence, absorbed_into, run_id, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.WorkspaceID, e.SkillID, e.SkillName, e.Action, e.Reason, orEmptyJSON(e.Evidence), e.AbsorbedInto, e.RunID, e.Actor, e.CreatedAt)
	return err
}

const proposalColumns = `id, workspace_id, kind, source, name, target_skill_id, job_id, intents, evidence,
	possible_duplicate_of, similarity, status, note, created_at, decided_at`

func scanProposal(row interface{ Scan(...any) error }) (LearnedProposal, error) {
	var p LearnedProposal
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Kind, &p.Source, &p.Name, &p.TargetSkillID, &p.JobID, &p.Intents, &p.Evidence,
		&p.PossibleDuplicateOf, &p.Similarity, &p.Status, &p.Note, &p.CreatedAt, &p.DecidedAt)
	return p, err
}

// CreateProposal inserts a pending create/patch/bundle proposal.
func (s *Store) CreateProposal(p LearnedProposal) (LearnedProposal, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(`INSERT INTO learned_skill_proposals
		(workspace_id, kind, source, name, target_skill_id, job_id, intents, evidence,
		 possible_duplicate_of, similarity, status, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		p.WorkspaceID, p.Kind, p.Source, p.Name, p.TargetSkillID, p.JobID, orEmptyJSON(p.Intents), orEmptyJSON(p.Evidence),
		p.PossibleDuplicateOf, p.Similarity, p.Note, now)
	if err != nil {
		return LearnedProposal{}, fmt.Errorf("create %s proposal %q: %w", p.Kind, p.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearnedProposal{}, fmt.Errorf("create %s proposal %q: id: %w", p.Kind, p.Name, err)
	}
	return s.GetProposal(id)
}

// GetProposal returns the proposal with the given id.
func (s *Store) GetProposal(id int64) (LearnedProposal, error) {
	p, err := scanProposal(s.db.QueryRow(`SELECT `+proposalColumns+` FROM learned_skill_proposals WHERE id = ?`, id))
	if err != nil {
		return LearnedProposal{}, fmt.Errorf("get proposal %d: %w", id, err)
	}
	return p, nil
}

// ListProposals returns the workspace's proposals with the given status
// ("" = all), oldest first.
func (s *Store) ListProposals(workspaceID int64, status string) ([]LearnedProposal, error) {
	q := `SELECT ` + proposalColumns + ` FROM learned_skill_proposals WHERE workspace_id = ?`
	args := []any{workspaceID}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	proposals := make([]LearnedProposal, 0)
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// ListRejectedProposals returns the workspace's rejected proposals, newest
// first, up to limit -- negative examples for the reflector.
func (s *Store) ListRejectedProposals(workspaceID int64, limit int) ([]LearnedProposal, error) {
	rows, err := s.db.Query(`SELECT `+proposalColumns+`
		FROM learned_skill_proposals WHERE workspace_id = ? AND status = 'rejected'
		ORDER BY id DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list rejected proposals for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	proposals := make([]LearnedProposal, 0)
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// DecideProposal settles a pending proposal as approved or rejected with a
// note. Rejecting also appends a ledger row (action "reject", no skill_id,
// the proposal's name and evidence). Only a pending proposal can be
// decided; anything else is an error and writes nothing. Approval's actual
// skill changes happen in LandSkill/PatchSkill/ApplyBundle's transactions;
// this path is for rejecting and for direct approvals without changes.
func (s *Store) DecideProposal(id int64, status, note, actor string) error {
	if status != "approved" && status != "rejected" {
		return fmt.Errorf("decide proposal %d: unknown status %q", id, status)
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("decide proposal %d: begin: %w", id, err)
	}
	defer tx.Rollback()

	var p LearnedProposal
	err = tx.QueryRow(`SELECT `+proposalColumns+` FROM learned_skill_proposals WHERE id = ?`, id).
		Scan(&p.ID, &p.WorkspaceID, &p.Kind, &p.Source, &p.Name, &p.TargetSkillID, &p.JobID, &p.Intents, &p.Evidence,
			&p.PossibleDuplicateOf, &p.Similarity, &p.Status, &p.Note, &p.CreatedAt, &p.DecidedAt)
	if err != nil {
		return fmt.Errorf("decide proposal %d: %w", id, err)
	}
	if p.Status != "pending" {
		return fmt.Errorf("decide proposal %d: status is %s, want pending", id, p.Status)
	}
	if _, err := tx.Exec(`UPDATE learned_skill_proposals SET status = ?, note = ?, decided_at = ? WHERE id = ?`,
		status, note, now, id); err != nil {
		return fmt.Errorf("decide proposal %d: %w", id, err)
	}
	if status == "rejected" {
		if _, err := tx.Exec(`INSERT INTO learned_skill_ledger
			(workspace_id, skill_id, skill_name, action, reason, evidence, absorbed_into, run_id, actor, created_at)
			VALUES (?, NULL, ?, 'reject', ?, ?, '', '', ?, ?)`,
			p.WorkspaceID, p.Name, note, orEmptyJSON(p.Evidence), actor, now); err != nil {
			return fmt.Errorf("decide proposal %d: ledger: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("decide proposal %d: commit: %w", id, err)
	}
	return nil
}

const jobColumns = `id, workspace_id, chat_id, kind, trigger_kind, window_from_event_id, window_to_event_id,
	status, skip_reason, verdicts, run_id, cost_usd, tokens, error, created_at, finished_at`

func scanJob(row interface{ Scan(...any) error }) (LearningJob, error) {
	var j LearningJob
	err := row.Scan(&j.ID, &j.WorkspaceID, &j.ChatID, &j.Kind, &j.TriggerKind, &j.WindowFromEventID, &j.WindowToEventID,
		&j.Status, &j.SkipReason, &j.Verdicts, &j.RunID, &j.CostUSD, &j.Tokens, &j.Error, &j.CreatedAt, &j.FinishedAt)
	return j, err
}

// CreateLearningJob inserts a job row as given (caller sets status).
func (s *Store) CreateLearningJob(j LearningJob) (LearningJob, error) {
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO learning_jobs
		(workspace_id, chat_id, kind, trigger_kind, window_from_event_id, window_to_event_id,
		 status, skip_reason, verdicts, run_id, cost_usd, tokens, error, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.WorkspaceID, j.ChatID, j.Kind, j.TriggerKind, j.WindowFromEventID, j.WindowToEventID,
		j.Status, j.SkipReason, orEmptyJSON(j.Verdicts), j.RunID, j.CostUSD, j.Tokens, j.Error, j.CreatedAt, j.FinishedAt)
	if err != nil {
		return LearningJob{}, fmt.Errorf("create learning job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return LearningJob{}, fmt.Errorf("create learning job: id: %w", err)
	}
	j.ID = id
	return j, nil
}

// UpdateLearningJob replaces every field of job j.ID (full-record replace,
// matching UpdateMcpServer's shape).
func (s *Store) UpdateLearningJob(j LearningJob) error {
	res, err := s.db.Exec(`UPDATE learning_jobs SET
		workspace_id = ?, chat_id = ?, kind = ?, trigger_kind = ?, window_from_event_id = ?, window_to_event_id = ?,
		status = ?, skip_reason = ?, verdicts = ?, run_id = ?, cost_usd = ?, tokens = ?, error = ?, created_at = ?, finished_at = ?
		WHERE id = ?`,
		j.WorkspaceID, j.ChatID, j.Kind, j.TriggerKind, j.WindowFromEventID, j.WindowToEventID,
		j.Status, j.SkipReason, orEmptyJSON(j.Verdicts), j.RunID, j.CostUSD, j.Tokens, j.Error, j.CreatedAt, j.FinishedAt, j.ID)
	if err != nil {
		return fmt.Errorf("update learning job %d: %w", j.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update learning job %d: %w", j.ID, sql.ErrNoRows)
	}
	return nil
}

// GetLearningJob returns the job with the given id.
func (s *Store) GetLearningJob(id int64) (LearningJob, error) {
	j, err := scanJob(s.db.QueryRow(`SELECT `+jobColumns+` FROM learning_jobs WHERE id = ?`, id))
	if err != nil {
		return LearningJob{}, fmt.Errorf("get learning job %d: %w", id, err)
	}
	return j, nil
}

// ListLearningJobs returns the workspace's jobs, newest first, up to limit.
func (s *Store) ListLearningJobs(workspaceID int64, limit int) ([]LearningJob, error) {
	rows, err := s.db.Query(`SELECT `+jobColumns+`
		FROM learning_jobs WHERE workspace_id = ? ORDER BY id DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list learning jobs for workspace %d: %w", workspaceID, err)
	}
	defer rows.Close()

	jobs := make([]LearningJob, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan learning job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// HasActiveLearningJob reports whether the workspace has any queued or
// running job -- the single-flight guard so overlapping reflect/curate runs
// can't pile up.
func (s *Store) HasActiveLearningJob(workspaceID int64) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM learning_jobs WHERE workspace_id = ? AND status IN ('queued','running')`, workspaceID).Scan(&n); err != nil {
		return false, fmt.Errorf("has active learning job for workspace %d: %w", workspaceID, err)
	}
	return n > 0, nil
}

// ForgetWorkspaceLearning hard-deletes every learning row of the workspace
// (skills, versions, ledger, proposals, jobs) and resets learning_mode to
// 'off', in one transaction. DeleteWorkspace calls this before removing the
// workspace row (FK).
func (s *Store) ForgetWorkspaceLearning(workspaceID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("forget learning for workspace %d: begin: %w", workspaceID, err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM learned_skill_versions WHERE skill_id IN (SELECT id FROM learned_skills WHERE workspace_id = ?)`,
		`DELETE FROM learned_skills WHERE workspace_id = ?`,
		`DELETE FROM learned_skill_ledger WHERE workspace_id = ?`,
		`DELETE FROM learned_skill_proposals WHERE workspace_id = ?`,
		`DELETE FROM learning_jobs WHERE workspace_id = ?`,
		`UPDATE workspaces SET learning_mode = 'off' WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, workspaceID); err != nil {
			return fmt.Errorf("forget learning for workspace %d: %w", workspaceID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("forget learning for workspace %d: commit: %w", workspaceID, err)
	}
	return nil
}
