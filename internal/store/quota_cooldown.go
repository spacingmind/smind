package store

import (
	"fmt"
	"time"
)

// QuotaCooldown is a passive rejection cooldown for an account (ADR-0028 §7).
// ScopeModel "" is credential-wide; otherwise it scopes the cooldown to one
// model.
type QuotaCooldown struct {
	AccountID  int64
	ScopeModel string
	Until      time.Time
	Reason     string
	Level      int
	ObservedAt time.Time
}

// SaveQuotaCooldown upserts the cooldown row for (AccountID, ScopeModel).
// Times are stored in UTC.
func (s *Store) SaveQuotaCooldown(c QuotaCooldown) error {
	_, err := s.db.Exec(
		`INSERT INTO quota_cooldown (account_id, scope_model, until, reason, level, observed_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id, scope_model) DO UPDATE SET
		   until = excluded.until, reason = excluded.reason,
		   level = excluded.level, observed_at = excluded.observed_at`,
		c.AccountID, c.ScopeModel, c.Until.UTC(), c.Reason, c.Level, c.ObservedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save quota cooldown for account %d: %w", c.AccountID, err)
	}
	return nil
}

// ListQuotaCooldowns returns every cooldown row of the account, expired ones
// included, ordered by scope_model.
func (s *Store) ListQuotaCooldowns(accountID int64) ([]QuotaCooldown, error) {
	rows, err := s.db.Query(
		`SELECT account_id, scope_model, until, reason, level, observed_at
		 FROM quota_cooldown WHERE account_id = ? ORDER BY scope_model`, accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list quota cooldowns for account %d: %w", accountID, err)
	}
	defer rows.Close()

	var out []QuotaCooldown
	for rows.Next() {
		var c QuotaCooldown
		if err := rows.Scan(&c.AccountID, &c.ScopeModel, &c.Until, &c.Reason, &c.Level, &c.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan quota cooldown: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteQuotaCooldowns removes every cooldown row of the account.
func (s *Store) DeleteQuotaCooldowns(accountID int64) error {
	if _, err := s.db.Exec(`DELETE FROM quota_cooldown WHERE account_id = ?`, accountID); err != nil {
		return fmt.Errorf("delete quota cooldowns for account %d: %w", accountID, err)
	}
	return nil
}

// DeleteQuotaCooldown removes the cooldown row for one scope of the account.
func (s *Store) DeleteQuotaCooldown(accountID int64, scopeModel string) error {
	if _, err := s.db.Exec(`DELETE FROM quota_cooldown WHERE account_id = ? AND scope_model = ?`, accountID, scopeModel); err != nil {
		return fmt.Errorf("delete quota cooldown for account %d: %w", accountID, err)
	}
	return nil
}
