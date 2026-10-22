package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// forgeBudgetWindow is the rolling window of the per-host call counter
// (ADR-0039 section 4).
const forgeBudgetWindow = time.Hour

// AddForgeCalls adds n forge calls to host's counter at now and returns the
// calls counted in the current window. The window opens at the first call
// after the previous one ended: when now - window_start >= 1h the row
// restarts at n.
func (s *Store) AddForgeCalls(host string, n int, now time.Time) (int, error) {
	now = now.UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("add forge calls for %s: begin: %w", host, err)
	}
	var start time.Time
	var calls int
	err = tx.QueryRow(`SELECT window_start, calls FROM forge_budget WHERE host = ?`, host).Scan(&start, &calls)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		start, calls = now, 0
	case err != nil:
		_ = tx.Rollback()
		return 0, fmt.Errorf("add forge calls for %s: %w", host, err)
	case now.Sub(start) >= forgeBudgetWindow:
		start, calls = now, 0
	}
	calls += n
	if _, err := tx.Exec(
		`INSERT INTO forge_budget (host, window_start, calls) VALUES (?, ?, ?)
		 ON CONFLICT(host) DO UPDATE SET window_start = excluded.window_start, calls = excluded.calls`,
		host, start, calls,
	); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("add forge calls for %s: %w", host, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("add forge calls for %s: commit: %w", host, err)
	}
	return calls, nil
}

// GetForgeBudget returns the calls counted for host in the window containing
// now and when that window opened; (0, zero time) when there is no live
// window.
func (s *Store) GetForgeBudget(host string, now time.Time) (calls int, windowStart time.Time, err error) {
	err = s.db.QueryRow(`SELECT window_start, calls FROM forge_budget WHERE host = ?`, host).Scan(&windowStart, &calls)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, nil
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("get forge budget for %s: %w", host, err)
	}
	if now.UTC().Sub(windowStart) >= forgeBudgetWindow {
		return 0, time.Time{}, nil
	}
	return calls, windowStart, nil
}
