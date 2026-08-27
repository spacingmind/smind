package store

import "fmt"

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
