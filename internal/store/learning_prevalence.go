package store

import (
	"fmt"
	"time"
)

// WorkspaceEvent is one run event of a workspace's user runs, with the chat
// its run belongs to (0 when the run has none).
type WorkspaceEvent struct {
	RunID     string
	ChatID    int64
	ID, Seq   int64
	EventData string
	CreatedAt time.Time
}

// ListWorkspaceUserEvents returns the newest events (by id, descending) of
// the workspace's user runs on non-system tasks created at or after since.
// At most maxEvents rows are returned; truncated reports that more matched.
// It is read-only (ADR-0037 stage 0 prevalence scan).
func (s *Store) ListWorkspaceUserEvents(workspaceID int64, since time.Time, maxEvents int) (rows []WorkspaceEvent, truncated bool, err error) {
	if maxEvents < 0 {
		maxEvents = 0
	}
	r, err := s.db.Query(
		`SELECT e.run_id, COALESCE(r.chat_id, 0), e.id, e.seq, e.event_data, e.created_at
		 FROM run_events e JOIN runs r ON r.id = e.run_id JOIN tasks t ON t.id = r.task_id
		 WHERE t.workspace_id = ? AND r.purpose = 'user' AND t.kind != 'system' AND e.created_at >= ?
		 ORDER BY e.id DESC LIMIT ?`, workspaceID, since.UTC(), maxEvents+1)
	if err != nil {
		return nil, false, fmt.Errorf("list user events of workspace %d: %w", workspaceID, err)
	}
	defer r.Close()
	for r.Next() {
		var e WorkspaceEvent
		if err := r.Scan(&e.RunID, &e.ChatID, &e.ID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("scan user event of workspace %d: %w", workspaceID, err)
		}
		rows = append(rows, e)
	}
	if err := r.Err(); err != nil {
		return nil, false, err
	}
	if len(rows) > maxEvents {
		rows, truncated = rows[:maxEvents], true
	}
	return rows, truncated, nil
}
