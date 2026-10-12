package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// CreateQuotaSnapshot inserts a new quota snapshot.
func (s *Store) CreateQuotaSnapshot(q QuotaSnapshot) (QuotaSnapshot, error) {
	res, err := s.db.Exec(
		`INSERT INTO quota_snapshots (account_id, usage_data, polled_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		q.AccountID, q.UsageData, q.PolledAt, q.ExpiresAt,
	)
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("insert quota snapshot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("quota snapshot id: %w", err)
	}
	q.ID = id
	return q, nil
}

// GetQuotaSnapshot returns the quota snapshot with the given id.
func (s *Store) GetQuotaSnapshot(id int64) (QuotaSnapshot, error) {
	var q QuotaSnapshot
	err := s.db.QueryRow(
		`SELECT id, account_id, usage_data, polled_at, expires_at
		 FROM quota_snapshots WHERE id = ?`, id,
	).Scan(&q.ID, &q.AccountID, &q.UsageData, &q.PolledAt, &q.ExpiresAt)
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("get quota snapshot %d: %w", id, err)
	}
	return q, nil
}

// GetLatestQuotaSnapshot returns the most recent quota snapshot for the
// account, by polled_at with id as tiebreaker. It reports false when the
// account has no snapshot.
func (s *Store) GetLatestQuotaSnapshot(accountID int64) (QuotaSnapshot, bool, error) {
	var q QuotaSnapshot
	err := s.db.QueryRow(
		`SELECT id, account_id, usage_data, polled_at, expires_at
		 FROM quota_snapshots WHERE account_id = ?
		 ORDER BY polled_at DESC, id DESC LIMIT 1`, accountID,
	).Scan(&q.ID, &q.AccountID, &q.UsageData, &q.PolledAt, &q.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return QuotaSnapshot{}, false, nil
	}
	if err != nil {
		return QuotaSnapshot{}, false, fmt.Errorf("get latest quota snapshot for account %d: %w", accountID, err)
	}
	return q, true, nil
}

// ListQuotaSnapshots returns all quota snapshots, ordered by id.
func (s *Store) ListQuotaSnapshots() ([]QuotaSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, account_id, usage_data, polled_at, expires_at
		 FROM quota_snapshots ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list quota snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []QuotaSnapshot
	for rows.Next() {
		var q QuotaSnapshot
		if err := rows.Scan(&q.ID, &q.AccountID, &q.UsageData, &q.PolledAt, &q.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan quota snapshot: %w", err)
		}
		snapshots = append(snapshots, q)
	}
	return snapshots, rows.Err()
}

// ReplaceQuotaSnapshot inserts q and deletes every other quota snapshot for
// q.AccountID in one transaction, keeping only the latest row per account
// (ADR-0028). Snapshots of other accounts are untouched.
func (s *Store) ReplaceQuotaSnapshot(q QuotaSnapshot) (QuotaSnapshot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("replace quota snapshot: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO quota_snapshots (account_id, usage_data, polled_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		q.AccountID, q.UsageData, q.PolledAt, q.ExpiresAt,
	)
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("replace quota snapshot: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return QuotaSnapshot{}, fmt.Errorf("replace quota snapshot: id: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM quota_snapshots WHERE account_id = ? AND id != ?`, q.AccountID, id); err != nil {
		return QuotaSnapshot{}, fmt.Errorf("replace quota snapshot: prune: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return QuotaSnapshot{}, fmt.Errorf("replace quota snapshot: commit: %w", err)
	}
	q.ID = id
	return q, nil
}
