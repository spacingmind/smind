package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CreateRequestLog inserts one proxied-request trace row. Called from
// internal/server's bounded async writer (never synchronously from the
// request path -- see docs/plans/active/orchestration-and-metering.md
// "M1"), so a caller here has already decided the write is worth
// attempting; a failure is reported back to the caller to count and log,
// not retried.
func (s *Store) CreateRequestLog(r RequestLog) (RequestLog, error) {
	res, err := s.db.Exec(
		`INSERT INTO request_log (
			started_at, provider, account_id, session_key, model, stream,
			status, upstream_status, outcome, error, ttfb_ms, duration_ms,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.StartedAt, r.Provider, r.AccountID, r.SessionKey, r.Model, nullableBool(r.Stream),
		r.Status, r.UpstreamStatus, r.Outcome, r.Error, r.TTFBMs, r.DurationMs,
		r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
	)
	if err != nil {
		return RequestLog{}, fmt.Errorf("insert request log: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RequestLog{}, fmt.Errorf("request log id: %w", err)
	}
	r.ID = id
	return r, nil
}

// RequestLogFilter narrows ListRequestLogs; a zero value lists everything.
// Since is inclusive, Until is exclusive -- Since <= started_at < Until --
// matching usage.summary's own Since/Until semantics (M1 test scenarios).
type RequestLogFilter struct {
	Since     *time.Time
	Until     *time.Time
	AccountID *int64
	Limit     int
}

// ListRequestLogs returns rows matching filter, most recent first.
func (s *Store) ListRequestLogs(filter RequestLogFilter) ([]RequestLog, error) {
	where, args := filter.whereClause()
	query := `SELECT id, started_at, provider, account_id, session_key, model, stream,
		status, upstream_status, outcome, error, ttfb_ms, duration_ms,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens
		FROM request_log` + where + ` ORDER BY started_at DESC, id DESC`
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list request log: %w", err)
	}
	defer rows.Close()

	var out []RequestLog
	for rows.Next() {
		r, err := scanRequestLog(rows)
		if err != nil {
			return nil, fmt.Errorf("scan request log: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// requestLogGroupColumns maps usage.summary's groupBy values to the SQL
// expression that produces the group key. "day" truncates started_at (an
// RFC3339 TIMESTAMP column) to its date portion in UTC.
var requestLogGroupColumns = map[string]string{
	"account": "COALESCE(CAST(account_id AS TEXT), '')",
	"model":   "COALESCE(model, '')",
	"day":     "substr(started_at, 1, 10)",
}

// SummarizeRequestLogs aggregates request_log rows matching since/until
// (Since inclusive, Until exclusive; either may be nil for an open bound)
// into one UsageSummary per distinct value of groupBy ("account", "model",
// or "day"). Token sums only ever include non-NULL rows (SQL SUM's normal
// behavior), matching RequestLog's own "nil means unknown" convention.
func (s *Store) SummarizeRequestLogs(since, until *time.Time, groupBy string) ([]UsageSummary, error) {
	col, ok := requestLogGroupColumns[groupBy]
	if !ok {
		return nil, fmt.Errorf("summarize request log: invalid groupBy %q", groupBy)
	}

	filter := RequestLogFilter{Since: since, Until: until}
	where, args := filter.whereClause()
	query := fmt.Sprintf(`SELECT %s AS grp, COUNT(*),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(reasoning_tokens), 0)
		FROM request_log%s GROUP BY grp ORDER BY grp`, col, where)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize request log: %w", err)
	}
	defer rows.Close()

	var out []UsageSummary
	for rows.Next() {
		var u UsageSummary
		if err := rows.Scan(&u.Key, &u.Count, &u.InputTokens, &u.OutputTokens,
			&u.CacheReadTokens, &u.CacheWriteTokens, &u.ReasoningTokens); err != nil {
			return nil, fmt.Errorf("scan usage summary: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// whereClause builds the shared WHERE clause ListRequestLogs and
// SummarizeRequestLogs both filter by -- AccountID is only honored by
// ListRequestLogs (SummarizeRequestLogs groups by it instead when
// groupBy="account"), but building it in one place keeps the Since/Until
// semantics identical between the two call sites.
func (f RequestLogFilter) whereClause() (string, []any) {
	var conds []string
	var args []any
	if f.Since != nil {
		conds = append(conds, "started_at >= ?")
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		conds = append(conds, "started_at < ?")
		args = append(args, *f.Until)
	}
	if f.AccountID != nil {
		conds = append(conds, "account_id = ?")
		args = append(args, *f.AccountID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func scanRequestLog(rows *sql.Rows) (RequestLog, error) {
	var r RequestLog
	var stream sql.NullBool
	if err := rows.Scan(&r.ID, &r.StartedAt, &r.Provider, &r.AccountID, &r.SessionKey, &r.Model, &stream,
		&r.Status, &r.UpstreamStatus, &r.Outcome, &r.Error, &r.TTFBMs, &r.DurationMs,
		&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.ReasoningTokens); err != nil {
		return RequestLog{}, err
	}
	if stream.Valid {
		r.Stream = &stream.Bool
	}
	return r, nil
}

// formatAccountKey renders an account id exactly the way
// requestLogGroupColumns' "account" grouping expression renders it in SQL
// (CAST(account_id AS TEXT)), so a caller grouping by account can match a
// UsageSummary.Key back to the account id it came from.
func formatAccountKey(id int64) string {
	return strconv.FormatInt(id, 10)
}

// nullableBool adapts a *bool to a driver value SQLite's INTEGER stream
// column accepts: nil stays NULL, otherwise 0/1 -- database/sql has no
// built-in Valuer for *bool the way it does for *int64/*string.
func nullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
