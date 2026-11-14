package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PermissionGrant is one saved "allow always in this project" decision
// (docs/plans/active/saved-grants.md). It is keyed by project and the SHA-256
// of the exact RAW command; CommandDisplay is the redacted text a human saw,
// never what is matched.
type PermissionGrant struct {
	ID              int64      `json:"id"`
	WorkspaceID     int64      `json:"workspaceId"`
	CommandSHA      string     `json:"-"`
	CommandLen      int        `json:"-"`
	CommandDisplay  string     `json:"command"`
	Folder          string     `json:"folder"`
	MCPFingerprint  string     `json:"-"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	CreatedBy       string     `json:"createdBy"`
	SourceRunID     string     `json:"sourceRunId,omitempty"`
	SourceRequestID string     `json:"sourceRequestId,omitempty"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
	RevokedBy       string     `json:"revokedBy,omitempty"`
	UseCount        int        `json:"useCount"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
}

// Grant audit kinds.
const (
	GrantAuditCreated        = "created"
	GrantAuditUsed           = "used"
	GrantAuditRevoked        = "revoked"
	GrantAuditSkippedExpired = "skipped_expired"
	GrantAuditSkippedVoid    = "skipped_void"
)

// GrantAuditRow is one audit line of a grant.
type GrantAuditRow struct {
	ID        int64     `json:"id"`
	GrantID   int64     `json:"grantId"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor"`
	RunID     string    `json:"runId,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
	Note      string    `json:"note,omitempty"`
}

// ErrGrantInactive reports a use of a grant that is revoked or expired (or gone):
// the use is refused and nothing is recorded.
var ErrGrantInactive = errors.New("permission grant is not active")

// ErrGrantNotFound reports a grant id that does not exist.
var ErrGrantNotFound = errors.New("permission grant not found")

const grantColumns = `id, workspace_id, command_sha, command_len, command_display, folder, mcp_fingerprint, created_at, expires_at,
	created_by, source_run_id, source_request_id, revoked_at, revoked_by, use_count, last_used_at`

func scanGrant(r rowScanner) (PermissionGrant, error) {
	var g PermissionGrant
	err := r.Scan(&g.ID, &g.WorkspaceID, &g.CommandSHA, &g.CommandLen, &g.CommandDisplay, &g.Folder, &g.MCPFingerprint, &g.CreatedAt, &g.ExpiresAt,
		&g.CreatedBy, &g.SourceRunID, &g.SourceRequestID, &g.RevokedAt, &g.RevokedBy, &g.UseCount, &g.LastUsedAt)
	return g, err
}

// SaveGrant writes a grant for (workspace, command sha) and its "created" audit
// row in one transaction. Saving again for the same pair (after a revoke or an
// expiry, or a repeat answer) replaces the row's scope fields and clears the
// revocation and the use count; the audit rows of the old life stay.
func (s *Store) SaveGrant(g PermissionGrant) (PermissionGrant, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PermissionGrant{}, fmt.Errorf("save permission grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO permission_grants (workspace_id, command_sha, command_len, command_display, folder, mcp_fingerprint, created_at, expires_at, created_by, source_run_id, source_request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, command_sha) DO UPDATE SET command_len = excluded.command_len, command_display = excluded.command_display, folder = excluded.folder,
			mcp_fingerprint = excluded.mcp_fingerprint, created_at = excluded.created_at, expires_at = excluded.expires_at, created_by = excluded.created_by,
			source_run_id = excluded.source_run_id, source_request_id = excluded.source_request_id, revoked_at = NULL, revoked_by = '', use_count = 0, last_used_at = NULL`,
		g.WorkspaceID, g.CommandSHA, g.CommandLen, g.CommandDisplay, g.Folder, g.MCPFingerprint, g.CreatedAt.UTC(), g.ExpiresAt.UTC(), g.CreatedBy, g.SourceRunID, g.SourceRequestID); err != nil {
		return PermissionGrant{}, fmt.Errorf("save permission grant: %w", err)
	}
	out, err := scanGrant(tx.QueryRow(`SELECT `+grantColumns+` FROM permission_grants WHERE workspace_id = ? AND command_sha = ?`, g.WorkspaceID, g.CommandSHA))
	if err != nil {
		return PermissionGrant{}, fmt.Errorf("save permission grant: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO permission_grant_audit (grant_id, at, kind, actor, run_id, request_id) VALUES (?, ?, ?, ?, ?, ?)`,
		out.ID, g.CreatedAt.UTC(), GrantAuditCreated, g.CreatedBy, g.SourceRunID, g.SourceRequestID); err != nil {
		return PermissionGrant{}, fmt.Errorf("save permission grant: audit: %w", err)
	}
	return out, tx.Commit()
}

// FindGrant returns the grant for (workspace, command sha), whatever its
// status; the caller decides whether it is active. ok is false when none exists.
func (s *Store) FindGrant(workspaceID int64, commandSHA string) (PermissionGrant, bool, error) {
	g, err := scanGrant(s.db.QueryRow(`SELECT `+grantColumns+` FROM permission_grants WHERE workspace_id = ? AND command_sha = ?`, workspaceID, commandSHA))
	if errors.Is(err, sql.ErrNoRows) {
		return PermissionGrant{}, false, nil
	}
	if err != nil {
		return PermissionGrant{}, false, fmt.Errorf("find permission grant: %w", err)
	}
	return g, true, nil
}

// GetGrant returns the grant with the given id.
func (s *Store) GetGrant(id int64) (PermissionGrant, error) {
	g, err := scanGrant(s.db.QueryRow(`SELECT `+grantColumns+` FROM permission_grants WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PermissionGrant{}, ErrGrantNotFound
	}
	if err != nil {
		return PermissionGrant{}, fmt.Errorf("get permission grant: %w", err)
	}
	return g, nil
}

// ListGrants returns every grant (a workspaceID of 0 means all projects),
// newest first, revoked and expired ones included so a human can see them.
func (s *Store) ListGrants(workspaceID int64) ([]PermissionGrant, error) {
	q := `SELECT ` + grantColumns + ` FROM permission_grants`
	var args []any
	if workspaceID != 0 {
		q += ` WHERE workspace_id = ?`
		args = append(args, workspaceID)
	}
	rows, err := s.db.Query(q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list permission grants: %w", err)
	}
	defer rows.Close()
	out := []PermissionGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("list permission grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeGrant marks the grant revoked and audits it. It reports false (and
// writes nothing) when the grant was already revoked, and ErrGrantNotFound
// when there is no such grant.
func (s *Store) RevokeGrant(id int64, actor string, now time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("revoke permission grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE permission_grants SET revoked_at = ?, revoked_by = ? WHERE id = ? AND revoked_at IS NULL`, now.UTC(), actor, id)
	if err != nil {
		return false, fmt.Errorf("revoke permission grant: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM permission_grants WHERE id = ?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return false, ErrGrantNotFound
		}
		return false, nil
	}
	if _, err := tx.Exec(`INSERT INTO permission_grant_audit (grant_id, at, kind, actor) VALUES (?, ?, ?, ?)`, id, now.UTC(), GrantAuditRevoked, actor); err != nil {
		return false, fmt.Errorf("revoke permission grant: audit: %w", err)
	}
	return true, tx.Commit()
}

// RecordGrantUse counts a use of the grant and audits it, in one transaction.
// The update itself requires the grant to be neither revoked nor expired at
// `now`, so a revoke that lands between the caller's check and this call still
// wins: ErrGrantInactive, nothing counted, nothing audited.
func (s *Store) RecordGrantUse(id int64, runID, requestID string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("record permission grant use: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE permission_grants SET use_count = use_count + 1, last_used_at = ? WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`, now.UTC(), id, now.UTC())
	if err != nil {
		return fmt.Errorf("record permission grant use: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGrantInactive
	}
	if _, err := tx.Exec(`INSERT INTO permission_grant_audit (grant_id, at, kind, actor, run_id, request_id) VALUES (?, ?, ?, 'daemon', ?, ?)`, id, now.UTC(), GrantAuditUsed, runID, requestID); err != nil {
		return fmt.Errorf("record permission grant use: audit: %w", err)
	}
	return tx.Commit()
}

// NoteGrantSkip audits a refusal to apply a grant (expired, void), once per
// episode: it writes nothing when the grant's latest audit row is already the
// same kind, so a grant that stays expired does not grow the table on every
// request it fails to cover.
func (s *Store) NoteGrantSkip(row GrantAuditRow) error {
	var last string
	err := s.db.QueryRow(`SELECT kind FROM permission_grant_audit WHERE grant_id = ? ORDER BY id DESC LIMIT 1`, row.GrantID).Scan(&last)
	if err == nil && last == row.Kind {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("note permission grant skip: %w", err)
	}
	return s.AppendGrantAudit(row)
}

// AppendGrantAudit writes one audit row (a skipped use, for instance).
func (s *Store) AppendGrantAudit(row GrantAuditRow) error {
	if _, err := s.db.Exec(`INSERT INTO permission_grant_audit (grant_id, at, kind, actor, run_id, request_id, note) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.GrantID, row.At.UTC(), row.Kind, row.Actor, row.RunID, row.RequestID, row.Note); err != nil {
		return fmt.Errorf("append permission grant audit: %w", err)
	}
	return nil
}

// ListGrantAudit returns a grant's audit rows, oldest first.
func (s *Store) ListGrantAudit(grantID int64) ([]GrantAuditRow, error) {
	rows, err := s.db.Query(`SELECT id, grant_id, at, kind, actor, run_id, request_id, note FROM permission_grant_audit WHERE grant_id = ? ORDER BY id`, grantID)
	if err != nil {
		return nil, fmt.Errorf("list permission grant audit: %w", err)
	}
	defer rows.Close()
	var out []GrantAuditRow
	for rows.Next() {
		var r GrantAuditRow
		if err := rows.Scan(&r.ID, &r.GrantID, &r.At, &r.Kind, &r.Actor, &r.RunID, &r.RequestID, &r.Note); err != nil {
			return nil, fmt.Errorf("list permission grant audit: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
