package store

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// FleetAuditRetention is how long fleet exchange rows stay readable
// (ADR-0048 F1a); the daemon's sweep deletes only rows older than this.
const FleetAuditRetention = 30 * 24 * time.Hour

// Caps on what one fleet audit row may hold: the summary arrives already
// redacted; the store still bounds it, so a runaway log line cannot bloat
// the database.
const (
	fleetAuditMaxSummary = 512
	fleetAuditMaxShort   = 128
)

// fleetAuditDirections is the closed direction vocabulary of the audit
// log: which way the exchange went, not the link direction.
var fleetAuditDirections = map[string]bool{
	"out": true,
	"in":  true,
}

// FleetAuditRow is one fleet exchange: a request we sent ("out") or
// served ("in"), with its outcome and the trace/req ids that link the
// pair. Append-only; the only delete is SweepFleetAudit.
type FleetAuditRow struct {
	ID        int64
	TS        time.Time
	Direction string
	HostID    string
	Trace     string
	ReqID     string
	Method    string
	TaskID    string
	RunID     string
	Outcome   string
	Code      string
	LatencyMs int64
	Bytes     int64
	Summary   string
}

// capRunes caps s at max runes without cutting one in half, marking the
// cut with an ellipsis (like capUTF8, but the summary is capped by
// characters, not bytes: it is user-facing text).
func capRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	// The marker itself takes a rune of the budget (it is 3 bytes, so len
	// would under-count the prefix by 2).
	runes := []rune(s)
	return string(runes[:max-utf8.RuneCountInString(policyAuditTrunc)]) + policyAuditTrunc
}

// AppendFleetAudit appends one exchange row and returns its id. It is the
// write path of the fleet audit: there is no update function, and the
// only delete is SweepFleetAudit.
func (s *Store) AppendFleetAudit(r FleetAuditRow) (int64, error) {
	if !fleetAuditDirections[r.Direction] {
		return 0, fmt.Errorf("append fleet audit: unknown direction %q", r.Direction)
	}
	if r.Method == "" {
		return 0, fmt.Errorf("append fleet audit: method is required")
	}
	if r.Outcome == "" {
		return 0, fmt.Errorf("append fleet audit: outcome is required")
	}
	r.Summary = capRunes(r.Summary, fleetAuditMaxSummary)
	r.Trace = capUTF8(r.Trace, fleetAuditMaxShort)
	r.ReqID = capUTF8(r.ReqID, fleetAuditMaxShort)
	r.Code = capUTF8(r.Code, fleetAuditMaxShort)
	if r.TS.IsZero() {
		r.TS = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO fleet_audit
		(ts, direction, host_id, trace, req_id, method, task_id, run_id, outcome, code, latency_ms, bytes, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TS.UTC(), r.Direction, r.HostID, r.Trace, r.ReqID, r.Method, r.TaskID, r.RunID,
		r.Outcome, r.Code, r.LatencyMs, r.Bytes, r.Summary)
	if err != nil {
		return 0, fmt.Errorf("append fleet audit: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("append fleet audit: %w", err)
	}
	return id, nil
}

const fleetAuditColumns = `id, ts, direction, host_id, trace, req_id, method, task_id, run_id, outcome, code, latency_ms, bytes, summary`

func scanFleetAudit(sc rowScanner) (FleetAuditRow, error) {
	var r FleetAuditRow
	if err := sc.Scan(&r.ID, &r.TS, &r.Direction, &r.HostID, &r.Trace, &r.ReqID, &r.Method, &r.TaskID,
		&r.RunID, &r.Outcome, &r.Code, &r.LatencyMs, &r.Bytes, &r.Summary); err != nil {
		return FleetAuditRow{}, err
	}
	r.TS = r.TS.UTC()
	return r, nil
}

// ListFleetAudit returns audit rows newest first. An empty hostID lists
// every host; a zero since lists every age. Limit defaults to 100 and is
// capped at 1000.
func (s *Store) ListFleetAudit(hostID string, since time.Time, limit int) ([]FleetAuditRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT ` + fleetAuditColumns + ` FROM fleet_audit`
	var conds []string
	var args []any
	if hostID != "" {
		conds = append(conds, `host_id = ?`)
		args = append(args, hostID)
	}
	if !since.IsZero() {
		conds = append(conds, `ts >= ?`)
		args = append(args, since.UTC())
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list fleet audit: %w", err)
	}
	defer rows.Close()
	var out []FleetAuditRow
	for rows.Next() {
		r, err := scanFleetAudit(rows)
		if err != nil {
			return nil, fmt.Errorf("list fleet audit: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SweepFleetAudit deletes rows stamped strictly before olderThan and
// returns how many went: the retention path of the fleet audit, and the
// only delete this table has.
func (s *Store) SweepFleetAudit(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM fleet_audit WHERE ts < ?`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("sweep fleet audit: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sweep fleet audit: %w", err)
	}
	return n, nil
}
