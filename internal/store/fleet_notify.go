package store

import (
	"fmt"
	"time"
)

// EnqueueChildEventClosingWatch delivers a remote run's finish to the
// parent chat that watched it, exactly once. A remote run has no local run
// row to claim, so the claim gate is the watch row going open -> closed --
// flipped in the same transaction as the enqueue. Any later failure rolls
// everything back, leaving the watch open for the next poll to retry; a
// watch that was already closed returns ErrChildEventAlreadyNotified with
// nothing enqueued.
func (s *Store) EnqueueChildEventClosingWatch(in ChildEventInput, watchID int64, reason string) (ChatQueueItem, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("enqueue child event closing watch %d: %w", watchID, err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(
		`UPDATE fleet_watches SET state = ?, closed_reason = ?, updated_at = ? WHERE id = ? AND state = ?`,
		FleetWatchClosed, reason, time.Now().UTC(), watchID, FleetWatchOpen,
	)
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("close fleet watch %d for notify: %w", watchID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ChatQueueItem{}, fmt.Errorf("close fleet watch %d for notify: %w", watchID, err)
	}
	if n == 0 {
		return ChatQueueItem{}, ErrChildEventAlreadyNotified
	}

	in.RunIDs = nil // the watch close above is the once-gate; there is no run to claim
	id, _, err := enqueueChildEventTx(tx, in)
	if err != nil {
		return ChatQueueItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChatQueueItem{}, fmt.Errorf("enqueue child event closing watch %d: commit: %w", watchID, err)
	}
	return s.GetChatQueueItem(id)
}

// CloseFleetWatchIfOpen closes a watch only while it is still open and
// reports whether this call was the one that closed it. Like
// CloseFleetWatch it keeps the first reason.
func (s *Store) CloseFleetWatchIfOpen(id int64, reason string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE fleet_watches SET state = ?, closed_reason = CASE WHEN closed_reason = '' THEN ? ELSE closed_reason END, updated_at = ? WHERE id = ? AND state = ?`,
		FleetWatchClosed, reason, time.Now().UTC(), id, FleetWatchOpen,
	)
	if err != nil {
		return false, fmt.Errorf("close fleet watch %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("close fleet watch %d: %w", id, err)
	}
	return n == 1, nil
}
