package store

import (
	"fmt"
	"strings"
)

// StreamRunEvents calls fn for each of runID's events in recorded order, one
// row in flight at a time (ListRunEvents holds them all). A fn error stops the
// stream and is returned as is.
func (s *Store) StreamRunEvents(runID string, fn func(RunEvent) error) error {
	rows, err := s.db.Query(
		`SELECT id, run_id, seq, event_data, created_at FROM run_events WHERE run_id = ? ORDER BY seq`,
		runID,
	)
	if err != nil {
		return fmt.Errorf("stream run events for %q: %w", runID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var e RunEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return fmt.Errorf("scan run event: %w", err)
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// TailRunEvents returns the last n (n > 0) of runID's events in recorded order.
// It is an index range scan on (run_id, seq), newest first, so it reads n rows
// whatever the run's size.
func (s *Store) TailRunEvents(runID string, n int) ([]RunEvent, error) {
	if n <= 0 {
		return nil, fmt.Errorf("tail run events for %q: n must be positive", runID)
	}
	rows, err := s.db.Query(
		`SELECT id, run_id, seq, event_data, created_at FROM run_events WHERE run_id = ? ORDER BY seq DESC LIMIT ?`,
		runID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("tail run events for %q: %w", runID, err)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

// ToolCallInputEvents returns, in recorded order, runID's events with seq <
// beforeSeq whose tool call id is one of ids and that carry a tool input: what
// the log elider needs to learn a call's heredoc bodies when the event with its
// result is the only one inside a tail window. ids is capped by the caller.
func (s *Store) ToolCallInputEvents(runID string, ids []string, beforeSeq int64) ([]RunEvent, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	args = append(args, runID, beforeSeq)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.Query(
		`SELECT id, run_id, seq, event_data, created_at FROM run_events
		 WHERE run_id = ? AND seq < ? AND json_extract(event_data, '$.toolCallId') IN (`+marks+`)
		   AND json_extract(event_data, '$.toolInput') IS NOT NULL
		 ORDER BY seq`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("tool call input events for %q: %w", runID, err)
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
