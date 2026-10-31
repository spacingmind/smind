package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PolicyAuditRetention is how long permission decisions stay readable
// (ADR-0047 §2.3, D15); the daemon's sweep deletes only rows older than
// this.
const PolicyAuditRetention = 90 * 24 * time.Hour

// Caps on what one audit row may hold (ADR-0047 §2.3): the target and
// reasons arrive already redacted; the store still bounds them, so a
// runaway log line cannot bloat the database.
const (
	policyAuditMaxTarget  = 512
	policyAuditMaxReasons = 1024
	policyAuditMaxPaths   = 8
	policyAuditTrunc      = "…"
)

// policyAuditDecisions is the closed decision vocabulary: the engine's three
// verdicts plus the three outcomes an escalated ask can resolve to.
var policyAuditDecisions = map[string]bool{
	"allow":            true,
	"deny":             true,
	"ask":              true,
	"ask_human_allow":  true,
	"ask_human_deny":   true,
	"ask_timeout_deny": true,
}

// PolicyAuditRow is one policy-engine evaluation, or the later human/timeout
// outcome of one, linked back by RequestID.
type PolicyAuditRow struct {
	ID          int64
	CreatedAt   time.Time
	RunID       string
	TaskID      int64
	RequestID   string
	Seat        string
	WorkspaceID int64
	Tool        string
	Target      string
	Paths       []string
	Decision    string
	RuleID      string
	PolicySha   string
	Reasons     string
	LatencyMs   int64
}

// AppendPolicyAudit appends one evaluation row and returns its id. It is the
// write path of the audit (ADR-0047 §2.3): there is no update function, and
// the only delete is SweepPolicyAudit.
func (s *Store) AppendPolicyAudit(r PolicyAuditRow) (int64, error) {
	if !policyAuditDecisions[r.Decision] {
		return 0, fmt.Errorf("append policy audit: unknown decision %q", r.Decision)
	}
	if len(r.Target) > policyAuditMaxTarget {
		r.Target = capUTF8(r.Target, policyAuditMaxTarget)
	}
	if len(r.Reasons) > policyAuditMaxReasons {
		r.Reasons = capUTF8(r.Reasons, policyAuditMaxReasons)
	}
	if len(r.Paths) > policyAuditMaxPaths {
		r.Paths = r.Paths[:policyAuditMaxPaths]
	}
	paths := "[]"
	if len(r.Paths) > 0 {
		b, err := json.Marshal(r.Paths)
		if err != nil {
			return 0, fmt.Errorf("append policy audit: %w", err)
		}
		paths = string(b)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO permission_policy_audit
		(created_at, run_id, task_id, request_id, seat, workspace_id, tool, target, paths, decision, rule_id, policy_sha, reasons, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.CreatedAt.UTC(), r.RunID, r.TaskID, r.RequestID, r.Seat, r.WorkspaceID, r.Tool, r.Target, paths,
		r.Decision, r.RuleID, r.PolicySha, r.Reasons, r.LatencyMs)
	if err != nil {
		return 0, fmt.Errorf("append policy audit: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("append policy audit: %w", err)
	}
	return id, nil
}

// capUTF8 caps s at max bytes without cutting a rune in half: a byte-slice
// cut can land mid-character in redacted audit text, leaving an invalid
// UTF-8 string in the column. Over the cap it keeps the longest prefix that
// is at most max-len("…") bytes and ends on a rune boundary, plus the
// ellipsis marker.
func capUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max-len(policyAuditTrunc)]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + policyAuditTrunc
}

// PolicyAuditFilter narrows a policy-audit listing; zero fields list every
// row.
type PolicyAuditFilter struct {
	TaskID   int64
	Decision string
	Limit    int
	BeforeID int64
}

const policyAuditColumns = `id, created_at, run_id, task_id, request_id, seat, workspace_id, tool, target, paths, decision, rule_id, policy_sha, reasons, latency_ms`

func scanPolicyAudit(sc rowScanner) (PolicyAuditRow, error) {
	var r PolicyAuditRow
	var paths string
	if err := sc.Scan(&r.ID, &r.CreatedAt, &r.RunID, &r.TaskID, &r.RequestID, &r.Seat, &r.WorkspaceID, &r.Tool, &r.Target,
		&paths, &r.Decision, &r.RuleID, &r.PolicySha, &r.Reasons, &r.LatencyMs); err != nil {
		return PolicyAuditRow{}, err
	}
	if paths != "" && paths != "[]" {
		if err := json.Unmarshal([]byte(paths), &r.Paths); err != nil {
			return PolicyAuditRow{}, fmt.Errorf("scan policy audit %d: %w", r.ID, err)
		}
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// ListPolicyAudit returns audit rows newest first. Limit defaults to 50 and
// is capped at 500; BeforeID > 0 pages past that id (rows with a smaller
// one).
func (s *Store) ListPolicyAudit(f PolicyAuditFilter) ([]PolicyAuditRow, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT ` + policyAuditColumns + ` FROM permission_policy_audit`
	var conds []string
	var args []any
	if f.TaskID != 0 {
		conds = append(conds, `task_id = ?`)
		args = append(args, f.TaskID)
	}
	if f.Decision != "" {
		conds = append(conds, `decision = ?`)
		args = append(args, f.Decision)
	}
	if f.BeforeID > 0 {
		conds = append(conds, `id < ?`)
		args = append(args, f.BeforeID)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list policy audit: %w", err)
	}
	defer rows.Close()
	var out []PolicyAuditRow
	for rows.Next() {
		r, err := scanPolicyAudit(rows)
		if err != nil {
			return nil, fmt.Errorf("list policy audit: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SweepPolicyAudit deletes rows created strictly before olderThan and
// returns how many went: the retention path of ADR-0047 §2.3, and the only
// delete this table has.
func (s *Store) SweepPolicyAudit(olderThan time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM permission_policy_audit WHERE created_at < ?`, olderThan.UTC())
	if err != nil {
		return 0, fmt.Errorf("sweep policy audit: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sweep policy audit: %w", err)
	}
	return n, nil
}
