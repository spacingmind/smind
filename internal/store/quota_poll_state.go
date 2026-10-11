package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// QuotaPollState is the per-account call-budget ledger row for the usage
// poller (ADR-0028 §3 "Call budget"). One row per account; absent row means
// the account has never been polled.
type QuotaPollState struct {
	AccountID           int64
	LastAttemptAt       time.Time // zero = never (NULL)
	NextAllowedAt       time.Time // zero = no backoff (NULL)
	ConsecutiveFailures int
	// ForcedAt holds unix-millis timestamps of attempts made by an explicit
	// Refresh (the per-hour forced budget), oldest first. Save keeps only the
	// newest 6.
	ForcedAt   []int64
	RejectedFP string
	LastError  string
}

// GetQuotaPollState returns the account's budget row, reporting false when
// no row exists (never polled).
func (s *Store) GetQuotaPollState(accountID int64) (QuotaPollState, bool, error) {
	var q QuotaPollState
	var lastAttempt, nextAllowed sql.NullTime
	var forcedJSON string
	err := s.db.QueryRow(
		`SELECT account_id, last_attempt_at, next_allowed_at, consecutive_failures, forced_at, rejected_fp, last_error
		 FROM quota_poll_state WHERE account_id = ?`, accountID,
	).Scan(&q.AccountID, &lastAttempt, &nextAllowed, &q.ConsecutiveFailures, &forcedJSON, &q.RejectedFP, &q.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return QuotaPollState{}, false, nil
	}
	if err != nil {
		return QuotaPollState{}, false, fmt.Errorf("get quota poll state for account %d: %w", accountID, err)
	}
	if lastAttempt.Valid {
		q.LastAttemptAt = lastAttempt.Time
	}
	if nextAllowed.Valid {
		q.NextAllowedAt = nextAllowed.Time
	}
	if err := json.Unmarshal([]byte(forcedJSON), &q.ForcedAt); err != nil {
		return QuotaPollState{}, false, fmt.Errorf("get quota poll state for account %d: decode forced_at: %w", accountID, err)
	}
	return q, true, nil
}

// SaveQuotaPollState upserts the account's budget row. The account must
// exist (foreign key). Times are stored as UTC; zero times become NULL.
// ForcedAt is truncated to the newest 6 entries.
func (s *Store) SaveQuotaPollState(q QuotaPollState) error {
	if len(q.ForcedAt) > 6 {
		q.ForcedAt = q.ForcedAt[len(q.ForcedAt)-6:]
	}
	if q.ForcedAt == nil {
		q.ForcedAt = []int64{}
	}
	forcedJSON, err := json.Marshal(q.ForcedAt)
	if err != nil {
		return fmt.Errorf("save quota poll state for account %d: encode forced_at: %w", q.AccountID, err)
	}
	var lastAttempt, nextAllowed sql.NullTime
	if !q.LastAttemptAt.IsZero() {
		lastAttempt = sql.NullTime{Time: q.LastAttemptAt.UTC(), Valid: true}
	}
	if !q.NextAllowedAt.IsZero() {
		nextAllowed = sql.NullTime{Time: q.NextAllowedAt.UTC(), Valid: true}
	}
	if _, err := s.db.Exec(
		`INSERT INTO quota_poll_state (account_id, last_attempt_at, next_allowed_at, consecutive_failures, forced_at, rejected_fp, last_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET
		   last_attempt_at = excluded.last_attempt_at,
		   next_allowed_at = excluded.next_allowed_at,
		   consecutive_failures = excluded.consecutive_failures,
		   forced_at = excluded.forced_at,
		   rejected_fp = excluded.rejected_fp,
		   last_error = excluded.last_error`,
		q.AccountID, lastAttempt, nextAllowed, q.ConsecutiveFailures, string(forcedJSON), q.RejectedFP, q.LastError,
	); err != nil {
		return fmt.Errorf("save quota poll state for account %d: %w", q.AccountID, err)
	}
	return nil
}
