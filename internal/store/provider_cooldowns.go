package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MaxProviderCooldown caps how far ahead a provider cooldown can reach: a
// misparsed or absurd reset time can never lock a provider for longer.
const MaxProviderCooldown = 6 * time.Hour

// SetProviderCooldown records "provider is rate limited until resetAt"
// (ADR-0041 F1). The effective reset is capped at now+MaxProviderCooldown,
// and an existing row keeps the LATER of its reset and the new one, so a
// stale or racing write never shortens a cooldown.
func (s *Store) SetProviderCooldown(provider string, resetAt time.Time, runID string, now time.Time) error {
	if limit := now.Add(MaxProviderCooldown); resetAt.After(limit) {
		resetAt = limit
	}
	_, err := s.db.Exec(
		`INSERT INTO provider_cooldowns (provider, reset_at, run_id, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET
		   run_id = CASE WHEN excluded.reset_at > reset_at THEN excluded.run_id ELSE run_id END,
		   reset_at = CASE WHEN excluded.reset_at > reset_at THEN excluded.reset_at ELSE reset_at END`,
		provider, resetAt.UTC(), runID, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("set provider cooldown for %s: %w", provider, err)
	}
	return nil
}

// GetProviderCooldown returns the reset time of provider's cooldown, only
// while it is still in the future at now.
func (s *Store) GetProviderCooldown(provider string, now time.Time) (time.Time, bool, error) {
	var resetAt time.Time
	err := s.db.QueryRow(
		`SELECT reset_at FROM provider_cooldowns WHERE provider = ? AND reset_at > ?`,
		provider, now.UTC(),
	).Scan(&resetAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("get provider cooldown for %s: %w", provider, err)
	}
	return resetAt.UTC(), true, nil
}

// ListProviderCooldowns returns every active cooldown (reset_at > now) keyed
// by provider.
func (s *Store) ListProviderCooldowns(now time.Time) (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT provider, reset_at FROM provider_cooldowns WHERE reset_at > ?`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("list provider cooldowns: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var p string
		var t time.Time
		if err := rows.Scan(&p, &t); err != nil {
			return nil, fmt.Errorf("scan provider cooldown: %w", err)
		}
		out[p] = t.UTC()
	}
	return out, rows.Err()
}

// ClearProviderCooldown deletes provider's cooldown row -- the human's "I
// reset my quota, retry now" action. It reports whether a row was removed,
// so a repeat call is a harmless no-op.
func (s *Store) ClearProviderCooldown(provider string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM provider_cooldowns WHERE provider = ?`, provider)
	if err != nil {
		return false, fmt.Errorf("clear provider cooldown for %s: %w", provider, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("clear provider cooldown for %s: %w", provider, err)
	}
	return n > 0, nil
}

// PurgeExpiredProviderCooldowns deletes rows whose reset has passed.
func (s *Store) PurgeExpiredProviderCooldowns(now time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM provider_cooldowns WHERE reset_at <= ?`, now.UTC()); err != nil {
		return fmt.Errorf("purge provider cooldowns: %w", err)
	}
	return nil
}
