package store

import "fmt"

// TaskLifecycle returns id -> archived (archived_at IS NOT NULL) for every
// row of the tasks table, including kind='system' tasks. It is used by the
// scratch-dir sweep to tell live tasks from closed or unknown ones.
func (s *Store) TaskLifecycle() (map[int64]bool, error) {
	out := make(map[int64]bool)
	q, err := s.db.Query(`SELECT id, archived_at IS NOT NULL FROM tasks`)
	if err != nil {
		return nil, fmt.Errorf("task lifecycle: %w", err)
	}
	defer q.Close()
	for q.Next() {
		var (
			id       int64
			archived bool
		)
		if err := q.Scan(&id, &archived); err != nil {
			return nil, fmt.Errorf("task lifecycle: scan: %w", err)
		}
		out[id] = archived
	}
	if err := q.Err(); err != nil {
		return nil, fmt.Errorf("task lifecycle: %w", err)
	}
	return out, nil
}
