package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Chat queue item statuses and sources (ADR-0021).
const (
	ChatQueueStatusQueued    = "queued"
	ChatQueueStatusDelivered = "delivered"
	ChatQueueStatusCancelled = "cancelled"

	ChatQueueSourceHuman        = "human"
	ChatQueueSourceOrchestrator = "orchestrator"
	ChatQueueSourceAgent        = "agent"
)

// chatQueueBound is the per-chat bound on queued items (ADR-0021 §7):
// at most this many status='queued' rows per chat; the next EnqueueChatPrompt
// fails with ErrQueueFull.
const chatQueueBound = 20

// ErrQueueFull is returned by EnqueueChatPrompt when the chat already holds
// the maximum number of queued items (ADR-0021 §7).
var ErrQueueFull = errors.New("queue full: this chat already has 20 queued prompts")

// ChatQueueItem is one persisted prompt waiting for its chat to be free
// (ADR-0021 §1). RunConfig is the JSON-encoded start parameters the item
// will be delivered with (provider, permissionMode, thinkingLevel). RunID
// and CancelReason are set when the item leaves 'queued'.
type ChatQueueItem struct {
	ID           int64
	ChatID       int64
	Prompt       string
	RunConfig    string
	Source       string
	FromTaskID   *int64
	FromChatID   *int64
	Priority     int
	Status       string
	RunID        *string
	CancelReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EnqueueChatPrompt appends an item to chatID's queue. It rejects (with
// ErrQueueFull) once the chat already holds chatQueueBound queued items.
// priority is 0 for a normal FIFO item and 1 for an interrupt item
// (delivered ahead of earlier queue items, ADR-0021 §2).
func (s *Store) EnqueueChatPrompt(chatID int64, prompt, runConfig, source string, priority int) (ChatQueueItem, error) {
	var queued int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id = ? AND status = 'queued'`, chatID).Scan(&queued); err != nil {
		return ChatQueueItem{}, fmt.Errorf("count queued prompts for chat %d: %w", chatID, err)
	}
	if queued >= chatQueueBound {
		return ChatQueueItem{}, fmt.Errorf("enqueue for chat %d: %w", chatID, ErrQueueFull)
	}

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO chat_queue (chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, NULL, NULL, ?, 'queued', NULL, '', ?, ?)`,
		chatID, prompt, runConfig, source, priority, now, now,
	)
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("insert chat queue item for chat %d: %w", chatID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("chat queue item id: %w", err)
	}
	return s.GetChatQueueItem(id)
}

// SetChatQueueItemProvenance records an agent sender (fromTaskId/fromChatId)
// on a queued item, for the [message from task #T, chat #C] delivery header
// (ADR-0021 §4).
func (s *Store) SetChatQueueItemProvenance(id, fromTaskID, fromChatID int64) error {
	if _, err := s.db.Exec(
		`UPDATE chat_queue SET from_task_id = ?, from_chat_id = ?, updated_at = ? WHERE id = ?`,
		fromTaskID, fromChatID, time.Now().UTC(), id,
	); err != nil {
		return fmt.Errorf("set chat queue item %d provenance: %w", id, err)
	}
	return nil
}

// GetChatQueueItem returns the queue item with the given id.
func (s *Store) GetChatQueueItem(id int64) (ChatQueueItem, error) {
	row := s.db.QueryRow(`SELECT id, chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, created_at, updated_at FROM chat_queue WHERE id = ?`, id)
	item, err := scanChatQueueItem(row)
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("get chat queue item %d: %w", id, err)
	}
	return item, nil
}

// NextQueued returns chatID's oldest deliverable queued item, ordered by
// priority DESC (interrupt items first), then id ASC (FIFO within a
// priority). ok is false when the chat has nothing queued.
func (s *Store) NextQueued(chatID int64) (ChatQueueItem, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, created_at, updated_at
		 FROM chat_queue WHERE chat_id = ? AND status = 'queued'
		 ORDER BY priority DESC, id LIMIT 1`, chatID,
	)
	item, err := scanChatQueueItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatQueueItem{}, false, nil
	}
	if err != nil {
		return ChatQueueItem{}, false, fmt.Errorf("next queued for chat %d: %w", chatID, err)
	}
	return item, true, nil
}

// MarkDelivered stamps item id as delivered, recording the run id its
// delivery started. Only a queued item can be marked delivered; anything
// else is a store bug and an error.
func (s *Store) MarkDelivered(id int64, runID string) error {
	res, err := s.db.Exec(
		`UPDATE chat_queue SET status = 'delivered', run_id = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		runID, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("mark chat queue item %d delivered: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("mark chat queue item %d delivered: not queued", id)
	}
	return nil
}

// MarkCancelled stamps item id as cancelled with a reason. Only a queued
// item can be cancelled; anything else is an error.
func (s *Store) MarkCancelled(id int64, reason string) error {
	res, err := s.db.Exec(
		`UPDATE chat_queue SET status = 'cancelled', cancel_reason = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		reason, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("cancel chat queue item %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("cancel chat queue item %d: not queued", id)
	}
	return nil
}

// ListChatQueue returns every queue item for chatID, all statuses, oldest
// (newest last) first.
func (s *Store) ListChatQueue(chatID int64) ([]ChatQueueItem, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, created_at, updated_at
		 FROM chat_queue WHERE chat_id = ? ORDER BY id`, chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat queue for chat %d: %w", chatID, err)
	}
	defer rows.Close()

	items := make([]ChatQueueItem, 0)
	for rows.Next() {
		item, err := scanChatQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan chat queue item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ChatsWithQueued returns the distinct chat ids that still have at least
// one queued item -- used after a daemon restart to resume delivery
// (ADR-0021 §5 as amended).
func (s *Store) ChatsWithQueued() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT chat_id FROM chat_queue WHERE status = 'queued' ORDER BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("list chats with queued prompts: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan chat id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanChatQueueItem(row rowScanner) (ChatQueueItem, error) {
	var item ChatQueueItem
	var fromTaskID, fromChatID sql.NullInt64
	var runID sql.NullString
	if err := row.Scan(
		&item.ID, &item.ChatID, &item.Prompt, &item.RunConfig, &item.Source,
		&fromTaskID, &fromChatID, &item.Priority, &item.Status,
		&runID, &item.CancelReason, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return ChatQueueItem{}, err
	}
	if fromTaskID.Valid {
		v := fromTaskID.Int64
		item.FromTaskID = &v
	}
	if fromChatID.Valid {
		v := fromChatID.Int64
		item.FromChatID = &v
	}
	if runID.Valid {
		v := runID.String
		item.RunID = &v
	}
	return item, nil
}
