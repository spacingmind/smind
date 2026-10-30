package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// ChatQueueSourceChildEvent is a daemon-authored message about a child
	// run finishing (ADR-0041 B3). Never accepted on the wire: only the
	// daemon enqueues it.
	ChatQueueSourceChildEvent = "child_event"

	// ChatQueueSourcePluginPrefix starts the source of a message a plugin
	// queued with task.send (ADR-0042 PB.3): "plugin:<id>".
	ChatQueueSourcePluginPrefix = "plugin:"

	// ChatQueueCancelUser/ChatQueueCancelAuto are the cancel_kind values
	// stamped when a queued item is cancelled: 'user' by the
	// chat.queueCancel RPC, 'auto' by a failed delivery. '' means not
	// cancelled; CancelReason carries the display text either way.
	ChatQueueCancelUser = "user"
	ChatQueueCancelAuto = "auto"
)

// PluginSource is the queue source of a message plugin id sent.
func PluginSource(id string) string { return ChatQueueSourcePluginPrefix + id }

// PluginSourceID reports whether source is a plugin's and which plugin.
func PluginSourceID(source string) (string, bool) {
	id, ok := strings.CutPrefix(source, ChatQueueSourcePluginPrefix)
	return id, ok && id != ""
}

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
// and CancelReason are set when the item leaves 'queued'; CancelKind
// (ChatQueueCancelUser/ChatQueueCancelAuto) is set on cancel -- the
// structured signal clients branch on, with CancelReason as display text.
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
	CancelKind   string
	// Attachments is attach.EncodeSet of the prompt's accepted attachments
	// (ADR-0038); "" = none.
	Attachments string
	// NotBefore holds the item until that instant (ADR-0041); nil =
	// deliverable now.
	NotBefore *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EnqueueChatPrompt appends an item to chatID's queue in one statement
// (prompt, provenance, and status all land atomically). It rejects (with
// ErrQueueFull) once the chat already holds chatQueueBound queued items.
// priority is 0 for a normal FIFO item and 1 for an interrupt item
// (delivered ahead of earlier queue items, ADR-0021 §2). fromTaskID and
// fromChatID (the agent sender, ADR-0021 §4) are stored as NULL when 0 --
// only meaningful for source='agent'.
func (s *Store) EnqueueChatPrompt(chatID int64, prompt, runConfig, source string, priority int, fromTaskID, fromChatID int64) (ChatQueueItem, error) {
	return s.EnqueueChatPromptAttach(chatID, prompt, runConfig, source, priority, fromTaskID, fromChatID, "")
}

// EnqueueChatPromptAttach is EnqueueChatPrompt with the prompt's encoded
// attachment set (attach.EncodeSet; "" = none) stored on the row.
func (s *Store) EnqueueChatPromptAttach(chatID int64, prompt, runConfig, source string, priority int, fromTaskID, fromChatID int64, attachments string) (ChatQueueItem, error) {
	return s.EnqueueChatPromptAt(chatID, prompt, runConfig, source, priority, fromTaskID, fromChatID, attachments, nil)
}

// EnqueueChatPromptAt is EnqueueChatPromptAttach with an optional hold: a
// non-nil notBefore keeps the item undeliverable until that instant
// (ADR-0041). It still counts toward the per-chat bound.
func (s *Store) EnqueueChatPromptAt(chatID int64, prompt, runConfig, source string, priority int, fromTaskID, fromChatID int64, attachments string, notBefore *time.Time) (ChatQueueItem, error) {
	var queued int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id = ? AND status = 'queued'`, chatID).Scan(&queued); err != nil {
		return ChatQueueItem{}, fmt.Errorf("count queued prompts for chat %d: %w", chatID, err)
	}
	if queued >= chatQueueBound {
		return ChatQueueItem{}, fmt.Errorf("enqueue for chat %d: %w", chatID, ErrQueueFull)
	}

	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO chat_queue (chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, attachments, not_before, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', NULL, '', ?, ?, ?, ?)`,
		chatID, prompt, runConfig, source, nullID(fromTaskID), nullID(fromChatID), priority, attachments, timePtrToNull(utcPtr(notBefore)), now, now,
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

// nullID maps 0 ("no sender") onto SQL NULL for the from_*_id columns.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// GetChatQueueItem returns the queue item with the given id.
func (s *Store) GetChatQueueItem(id int64) (ChatQueueItem, error) {
	row := s.db.QueryRow(`SELECT `+chatQueueColumns+` FROM chat_queue WHERE id = ?`, id)
	item, err := scanChatQueueItem(row)
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("get chat queue item %d: %w", id, err)
	}
	return item, nil
}

// NextQueued returns chatID's oldest deliverable (not held) queued item, ordered by
// priority DESC (interrupt items first), then id ASC (FIFO within a
// priority). ok is false when the chat has nothing queued.
func (s *Store) NextQueued(chatID int64) (ChatQueueItem, bool, error) {
	return s.NextQueuedAt(chatID, time.Now())
}

// NextQueuedAt is NextQueued at an explicit instant: an item whose
// not_before is after now is held and skipped (ADR-0041), so a later FIFO
// item without a hold is still returned.
func (s *Store) NextQueuedAt(chatID int64, now time.Time) (ChatQueueItem, bool, error) {
	row := s.db.QueryRow(
		`SELECT `+chatQueueColumns+`
		 FROM chat_queue WHERE chat_id = ? AND status = 'queued'
		   AND (not_before IS NULL OR not_before <= ?)
		 ORDER BY priority DESC, id LIMIT 1`, chatID, now.UTC(),
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

// SetQueuedNotBefore sets (or, with nil, clears) a queued item's hold. Only a
// status='queued' item can be changed; anything else is an error.
func (s *Store) SetQueuedNotBefore(id int64, t *time.Time) error {
	res, err := s.db.Exec(
		`UPDATE chat_queue SET not_before = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		timePtrToNull(utcPtr(t)), time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set chat queue item %d not_before: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set chat queue item %d not_before: not queued", id)
	}
	return nil
}

// EarliestHeldNotBefore returns the smallest not_before strictly after now
// among queued items; ok is false when no item is held into the future.
func (s *Store) EarliestHeldNotBefore(now time.Time) (time.Time, bool, error) {
	var raw sql.NullTime
	err := s.db.QueryRow(
		`SELECT not_before FROM chat_queue WHERE status = 'queued' AND not_before > ? ORDER BY not_before LIMIT 1`, now.UTC(),
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("earliest held not_before: %w", err)
	}
	if !raw.Valid {
		return time.Time{}, false, nil
	}
	return raw.Time, true, nil
}

// utcPtr returns t normalised to UTC (nil stays nil), so every stored
// not_before compares consistently.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// ReleaseHeldRetries drops the hold of every queued orchestrator rate-limit
// retry of provider (run_config provider matches, failoverOf set), making
// it deliverable now. It returns the chat id of each released item -- one
// entry per item, so a chat may repeat -- for the caller to wake.
func (s *Store) ReleaseHeldRetries(provider string, now time.Time) ([]int64, error) {
	rows, err := s.db.Query(
		`UPDATE chat_queue SET not_before = NULL, updated_at = ?
		 WHERE status = 'queued' AND not_before > ? AND source = ?
		   AND json_extract(run_config,'$.provider') = ?
		   AND COALESCE(json_extract(run_config,'$.failoverOf'),'') <> ''
		 RETURNING chat_id`,
		now.UTC(), now.UTC(), ChatQueueSourceOrchestrator, provider,
	)
	if err != nil {
		return nil, fmt.Errorf("release held retries for %s: %w", provider, err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan released retry chat id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

// MarkCancelled stamps item id as cancelled with a kind
// (ChatQueueCancelUser/ChatQueueCancelAuto) and a display reason. Only a
// queued item can be cancelled; anything else is an error.
func (s *Store) MarkCancelled(id int64, kind, reason string) error {
	res, err := s.db.Exec(
		`UPDATE chat_queue SET status = 'cancelled', cancel_reason = ?, cancel_kind = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		reason, kind, time.Now().UTC(), id,
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
		`SELECT `+chatQueueColumns+`
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

// chatQueueColumns is the chat_queue SELECT column list; scanChatQueueItem's
// Scan order must stay in lockstep with it.
const chatQueueColumns = "id, chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, cancel_kind, attachments, not_before, created_at, updated_at"

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
	var notBefore sql.NullTime
	if err := row.Scan(
		&item.ID, &item.ChatID, &item.Prompt, &item.RunConfig, &item.Source,
		&fromTaskID, &fromChatID, &item.Priority, &item.Status,
		&runID, &item.CancelReason, &item.CancelKind, &item.Attachments, &notBefore, &item.CreatedAt, &item.UpdatedAt,
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
	item.NotBefore = nullToTimePtr(notBefore)
	return item, nil
}

// attachmentSetID extracts the set id from an attach.EncodeSet value without
// importing internal/attach; "" when raw is empty or malformed.
func attachmentSetID(raw string) string {
	if raw == "" {
		return ""
	}
	var v struct {
		Set string `json:"set"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	return v.Set
}

func (s *Store) attachmentSets(query string, args ...any) ([]string, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list attachment sets: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan attachment set: %w", err)
		}
		if id := attachmentSetID(raw); id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// AttachmentSetsOfTask returns the attachment set ids held by taskID's runs
// and by queue items of its chats (ADR-0038). Call it before DeleteTask.
func (s *Store) AttachmentSetsOfTask(taskID int64) ([]string, error) {
	runs, err := s.attachmentSets(`SELECT attachments FROM runs WHERE task_id = ? AND attachments != ''`, taskID)
	if err != nil {
		return nil, err
	}
	queued, err := s.attachmentSets(
		`SELECT attachments FROM chat_queue WHERE attachments != '' AND chat_id IN (SELECT id FROM chats WHERE task_id = ?)`, taskID)
	if err != nil {
		return nil, err
	}
	return append(runs, queued...), nil
}

// ReferencedAttachmentSets returns every set id still referenced by a run or
// a queued prompt -- the keep-list for attach.Store.Sweep.
func (s *Store) ReferencedAttachmentSets() (map[string]bool, error) {
	runs, err := s.attachmentSets(`SELECT attachments FROM runs WHERE attachments != ''`)
	if err != nil {
		return nil, err
	}
	queued, err := s.attachmentSets(`SELECT attachments FROM chat_queue WHERE attachments != '' AND status = 'queued'`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(runs)+len(queued))
	for _, id := range append(runs, queued...) {
		out[id] = true
	}
	return out, nil
}
