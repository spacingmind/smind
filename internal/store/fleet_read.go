package store

import "fmt"

// ListRunEventsAfterSeq returns runID's events with seq > afterSeq in seq
// order, at most limit (<= 0 means 50; capped at 1000).
func (s *Store) ListRunEventsAfterSeq(runID string, afterSeq int64, limit int) ([]RunEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(
		`SELECT id, run_id, seq, event_data, created_at FROM run_events WHERE run_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		runID, afterSeq, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list run events for %q after %d: %w", runID, afterSeq, err)
	}
	defer rows.Close()

	var events []RunEvent
	for rows.Next() {
		var e RunEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan run event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
