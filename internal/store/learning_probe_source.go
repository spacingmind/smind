package store

import (
	"fmt"
	"time"
)

// ChatHasEarlierRun reports whether the chat has a user run other than
// runID that started before startedAt (ties broken by id): the learning
// probe replays only a prompt that began a conversation.
func (s *Store) ChatHasEarlierRun(chatID int64, runID string, startedAt time.Time) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM runs WHERE chat_id = ? AND purpose = 'user' AND id != ?
		   AND (started_at < ? OR (started_at = ? AND id < ?))`,
		chatID, runID, startedAt.UTC(), startedAt.UTC(), runID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("chat %d earlier runs: %w", chatID, err)
	}
	return n > 0, nil
}
