package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// OriginRun is one live run a remote host started here: just what the
// wall-limit sweeper and the live-agent budget (ADR-0048 F3a) need to
// decide, and nothing else.
type OriginRun struct {
	ID         string
	TaskID     int64
	OriginHost string
	StartedAt  time.Time
}

// ListLiveOriginRuns returns originHost's status='running' runs, or the
// live runs of every remote origin when originHost is empty. Local runs
// (empty origin_host) are never listed: their wall clock is not the
// fleet's to enforce.
func (s *Store) ListLiveOriginRuns(originHost string) ([]OriginRun, error) {
	q := `SELECT id, task_id, origin_host, started_at FROM runs WHERE status = 'running'`
	var args []any
	if originHost == "" {
		q += ` AND origin_host <> ''`
	} else {
		q += ` AND origin_host = ?`
		args = append(args, originHost)
	}
	q += ` ORDER BY started_at, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list live origin runs %s: %w", originHost, err)
	}
	defer rows.Close()
	var out []OriginRun
	for rows.Next() {
		var r OriginRun
		if err := rows.Scan(&r.ID, &r.TaskID, &r.OriginHost, &r.StartedAt); err != nil {
			return nil, fmt.Errorf("list live origin runs %s: %w", originHost, err)
		}
		r.StartedAt = r.StartedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// OriginCostSince sums the cost originHost's runs started at or after
// since reported. A NULL cost adds nothing -- unreported is never
// estimated -- and an origin with no rows (or none reported) sums to 0.
// Not filtered by trace: the per-origin budget (ADR-0048 D3) counts a
// host's spend across every trace id.
func (s *Store) OriginCostSince(originHost string, since time.Time) (float64, error) {
	var sum float64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(u.cost_usd), 0)
		 FROM runs r JOIN run_usage u ON u.run_id = r.id
		 WHERE r.origin_host = ? AND r.started_at >= ?`,
		originHost, since.UTC(),
	).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("origin %s cost since %s: %w", originHost, since.UTC(), err)
	}
	return sum, nil
}

// GetFleetIdem is the read-only half of PutFleetIdem: it reports the
// stored response for (hostID, key) without writing anything, so the
// write path can replay an idempotent request before re-executing it.
func (s *Store) GetFleetIdem(hostID, key string) (FleetIdem, bool, error) {
	var i FleetIdem
	err := s.db.QueryRow(`SELECT host_id, idem_key, method, task_id, run_id, response, created_at
		FROM fleet_idem WHERE host_id = ? AND idem_key = ?`, hostID, key,
	).Scan(&i.HostID, &i.Key, &i.Method, &i.TaskID, &i.RunID, &i.Response, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FleetIdem{}, false, nil
	}
	if err != nil {
		return FleetIdem{}, false, fmt.Errorf("get fleet idem %s/%s: %w", hostID, key, err)
	}
	i.CreatedAt = i.CreatedAt.UTC()
	return i, true, nil
}

// MarkRunWallLimit stamps stop_reason='wall_limit' on a run that is no
// longer running. The wall sweeper (ADR-0048 S17) stops the run first and
// records why after, so a still-running (or missing) run is never
// stamped: it is an error carrying ErrFleetNotFound.
func (s *Store) MarkRunWallLimit(runID string) error {
	res, err := s.db.Exec(`UPDATE runs SET stop_reason = 'wall_limit' WHERE id = ? AND status <> 'running'`, runID)
	if err != nil {
		return fmt.Errorf("mark run %s wall limit: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark run %s wall limit: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("mark run %s wall limit: %w", runID, ErrFleetNotFound)
	}
	return nil
}
