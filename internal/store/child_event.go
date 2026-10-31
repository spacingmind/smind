package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrChildEventAlreadyNotified is returned by EnqueueChildEvent when every
// run in the event was already claimed as parent-notified (ADR-0041 B3).
var ErrChildEventAlreadyNotified = errors.New("child event: all runs already notified")

// SubtreeBusy reports whether any task in taskID's subtree (itself and all
// descendants via parent_task_id) has a running run or a queued chat-queue
// item. UNION (not UNION ALL) keeps the recursion cycle safe.
func (s *Store) SubtreeBusy(taskID int64) (bool, error) {
	var busy bool
	err := s.db.QueryRow(
		`WITH RECURSIVE sub(id) AS (
		     SELECT ? UNION SELECT t.id FROM tasks t JOIN sub ON t.parent_task_id = sub.id
		 )
		 SELECT EXISTS (SELECT 1 FROM runs r WHERE r.task_id IN (SELECT id FROM sub) AND r.status = 'running')
		     OR EXISTS (SELECT 1 FROM chat_queue q JOIN chats c ON c.id = q.chat_id
		                WHERE c.task_id IN (SELECT id FROM sub) AND q.status = 'queued')`,
		taskID,
	).Scan(&busy)
	if err != nil {
		return false, fmt.Errorf("subtree busy for task %d: %w", taskID, err)
	}
	return busy, nil
}

// ListUnnotifiedRepliesForTask returns taskID's finished runs that carry a
// reply chat and have not been claimed as parent-notified, oldest first.
func (s *Store) ListUnnotifiedRepliesForTask(taskID int64) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT `+runColumns+`
		 FROM runs
		 WHERE task_id = ? AND reply_chat_id IS NOT NULL AND parent_notified_at IS NULL
		   AND status != 'running'
		 ORDER BY started_at, id`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list unnotified replies for task %d: %w", taskID, err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TasksWithUnnotifiedReplies returns the distinct task ids that own an
// unnotified child run newer than since, in first-seen order.
func (s *Store) TasksWithUnnotifiedReplies(since time.Time) ([]int64, error) {
	runs, err := s.ListUnnotifiedChildRuns(since)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool)
	var ids []int64
	for _, r := range runs {
		if !seen[r.TaskID] {
			seen[r.TaskID] = true
			ids = append(ids, r.TaskID)
		}
	}
	return ids, nil
}

// ClaimRunsParentNotified claims each run (parent_notified_at = at where it
// was NULL) and returns how many were newly claimed. Used to drop a
// notification silently.
func (s *Store) ClaimRunsParentNotified(runIDs []string, at time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("claim runs parent notified: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	n, err := claimRunsTx(tx, runIDs, at)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("claim runs parent notified: commit: %w", err)
	}
	return n, nil
}

func claimRunsTx(tx *sql.Tx, runIDs []string, at time.Time) (int, error) {
	claimed := 0
	for _, id := range runIDs {
		res, err := tx.Exec(`UPDATE runs SET parent_notified_at = ? WHERE id = ? AND parent_notified_at IS NULL`, at.UTC(), id)
		if err != nil {
			return 0, fmt.Errorf("claim run %q parent notified: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("claim run %q parent notified: %w", id, err)
		}
		claimed += int(n)
	}
	return claimed, nil
}

// ChildEventInput describes one parent notification: the runs it reports,
// the rendered Block, and the coalescing window.
type ChildEventInput struct {
	ChatID    int64
	RunIDs    []string
	Block     string
	RunConfig string
	Now       time.Time
	// Debounce holds the item that long past Now; MaxHold caps how far
	// appends may push not_before past the item's creation.
	Debounce, MaxHold time.Duration
}

// EnqueueChildEvent atomically claims every run in in.RunIDs (none is fine) and either
// appends in.Block to the chat's queued child_event item (pushing its
// not_before, capped at created_at+MaxHold) or inserts a new one. A crash
// or error never leaves a run claimed without its message. It returns
// ErrChildEventAlreadyNotified when no run was newly claimed and
// ErrQueueFull when a new item would exceed the per-chat bound.
func (s *Store) EnqueueChildEvent(in ChildEventInput) (item ChatQueueItem, appended bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ChatQueueItem{}, false, fmt.Errorf("enqueue child event: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	id, appended, err := enqueueChildEventTx(tx, in)
	if err != nil {
		return ChatQueueItem{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ChatQueueItem{}, false, fmt.Errorf("enqueue child event: commit: %w", err)
	}
	item, err = s.GetChatQueueItem(id)
	return item, appended, err
}

// enqueueChildEventTx is the in-transaction body of EnqueueChildEvent:
// claim in.RunIDs, then append-or-insert the queue item. Callers own the
// transaction, so any error they roll back on leaves nothing behind.
func enqueueChildEventTx(tx *sql.Tx, in ChildEventInput) (id int64, appended bool, err error) {
	now := in.Now.UTC()
	// A message with no RunIDs (the tree budget notice) reports no run, so
	// there is nothing to claim and the claim gate does not apply; its caller
	// owns the once-per-crossing idempotency.
	if len(in.RunIDs) > 0 {
		claimed, err := claimRunsTx(tx, in.RunIDs, now)
		if err != nil {
			return 0, false, err
		}
		if claimed == 0 {
			return 0, false, ErrChildEventAlreadyNotified
		}
	}

	existing, err := scanChatQueueItem(tx.QueryRow(
		`SELECT `+chatQueueColumns+` FROM chat_queue
		 WHERE chat_id = ? AND status = 'queued' AND source = ?
		 ORDER BY id LIMIT 1`, in.ChatID, ChatQueueSourceChildEvent))
	switch {
	case err == nil:
		notBefore := now.Add(in.Debounce)
		if limit := existing.CreatedAt.Add(in.MaxHold); limit.Before(notBefore) {
			notBefore = limit
		}
		if notBefore.Before(now) {
			notBefore = now
		}
		if _, err := tx.Exec(
			`UPDATE chat_queue SET prompt = prompt || ? || ?, not_before = ?, updated_at = ? WHERE id = ?`,
			"\n\n", in.Block, notBefore.UTC(), now, existing.ID,
		); err != nil {
			return 0, false, fmt.Errorf("append child event to queue item %d: %w", existing.ID, err)
		}
		id, appended = existing.ID, true
	case errors.Is(err, sql.ErrNoRows):
		var queued int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id = ? AND status = 'queued'`, in.ChatID).Scan(&queued); err != nil {
			return 0, false, fmt.Errorf("count queued prompts for chat %d: %w", in.ChatID, err)
		}
		if queued >= chatQueueBound {
			return 0, false, fmt.Errorf("enqueue child event for chat %d: %w", in.ChatID, ErrQueueFull)
		}
		res, err := tx.Exec(
			`INSERT INTO chat_queue (chat_id, prompt, run_config, source, from_task_id, from_chat_id, priority, status, run_id, cancel_reason, attachments, not_before, created_at, updated_at)
			 VALUES (?, ?, ?, ?, NULL, NULL, 0, 'queued', NULL, '', '', ?, ?, ?)`,
			in.ChatID, in.Block, in.RunConfig, ChatQueueSourceChildEvent, now.Add(in.Debounce), now, now,
		)
		if err != nil {
			return 0, false, fmt.Errorf("insert child event for chat %d: %w", in.ChatID, err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, false, fmt.Errorf("child event item id: %w", err)
		}
	default:
		return 0, false, fmt.Errorf("find queued child event for chat %d: %w", in.ChatID, err)
	}
	return id, appended, nil
}

// LatestFinishedRun returns taskID's most recently started non-running run.
// ok is false when the task has none.
func (s *Store) LatestFinishedRun(taskID int64) (Run, bool, error) {
	r, err := scanRun(s.db.QueryRow(
		`SELECT `+runColumns+` FROM runs WHERE task_id = ? AND status != 'running'
		 ORDER BY started_at DESC, id DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("latest finished run for task %d: %w", taskID, err)
	}
	return r, true, nil
}

// LatestRunForChat returns chatID's most recently started run (any status).
// ok is false when the chat has none.
func (s *Store) LatestRunForChat(chatID int64) (Run, bool, error) {
	r, err := scanRun(s.db.QueryRow(
		`SELECT `+runColumns+` FROM runs WHERE chat_id = ?
		 ORDER BY started_at DESC, id DESC LIMIT 1`, chatID))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("latest run for chat %d: %w", chatID, err)
	}
	return r, true, nil
}
