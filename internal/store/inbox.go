package store

import (
	"fmt"
	"strings"
	"time"
)

// InboxState is the Human's recorded action on one derived inbox item.
type InboxState struct {
	Key          string
	State        string // seen | snoozed | resolved
	SnoozedUntil *time.Time
	Actor        string
	UpdatedAt    time.Time
}

// ListInboxStates returns every stored state.
func (s *Store) ListInboxStates() ([]InboxState, error) {
	rows, err := s.db.Query(`SELECT key, state, snoozed_until, actor, updated_at FROM inbox_state`)
	if err != nil {
		return nil, fmt.Errorf("list inbox state: %w", err)
	}
	defer rows.Close()
	var out []InboxState
	for rows.Next() {
		var st InboxState
		if err := rows.Scan(&st.Key, &st.State, &st.SnoozedUntil, &st.Actor, &st.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list inbox state: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetInboxState writes (replaces) the state of each key in one transaction.
func (s *Store) SetInboxState(keys []string, state string, until *time.Time, actor string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set inbox state: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, k := range keys {
		if _, err := tx.Exec(`INSERT INTO inbox_state (key, state, snoozed_until, actor, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET state = excluded.state, snoozed_until = excluded.snoozed_until, actor = excluded.actor, updated_at = excluded.updated_at`,
			k, state, until, actor, now.UTC()); err != nil {
			return fmt.Errorf("set inbox state: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteInboxStatesExcept removes every state whose key is not in keep (the
// keys that still derive): a row is garbage-collected once its item is gone.
func (s *Store) DeleteInboxStatesExcept(keep []string) error {
	if len(keep) == 0 {
		if _, err := s.db.Exec(`DELETE FROM inbox_state`); err != nil {
			return fmt.Errorf("gc inbox state: %w", err)
		}
		return nil
	}
	existing, err := s.ListInboxStates()
	if err != nil {
		return err
	}
	keepSet := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepSet[k] = true
	}
	var stale []string
	for _, st := range existing {
		if !keepSet[st.Key] {
			stale = append(stale, st.Key)
		}
	}
	for len(stale) > 0 {
		n := len(stale)
		if n > 200 {
			n = 200
		}
		args := make([]any, n)
		for i, k := range stale[:n] {
			args[i] = k
		}
		if _, err := s.db.Exec(`DELETE FROM inbox_state WHERE key IN (`+strings.TrimSuffix(strings.Repeat("?,", n), ",")+`)`, args...); err != nil {
			return fmt.Errorf("gc inbox state: %w", err)
		}
		stale = stale[n:]
	}
	return nil
}

// ListLatestValidations returns the newest validation of every live task that
// has one (the inbox's verdict source).
func (s *Store) ListLatestValidations() ([]Validation, error) {
	rows, err := s.db.Query(`SELECT ` + prefixCols("v.", validationCols) + ` FROM validations v
		JOIN tasks t ON t.id = v.task_id
		WHERE t.archived_at IS NULL AND v.id = (SELECT MAX(id) FROM validations WHERE task_id = v.task_id)
		ORDER BY v.id`)
	if err != nil {
		return nil, fmt.Errorf("list latest validations: %w", err)
	}
	defer rows.Close()
	var out []Validation
	for rows.Next() {
		v, err := scanValidation(rows)
		if err != nil {
			return nil, fmt.Errorf("list latest validations: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
