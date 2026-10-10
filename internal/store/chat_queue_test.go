package store

import (
	"errors"
	"testing"
)

// TestChatQueue_EnqueueBoundAndOrder pins ADR-0021 §1/§2/§7: the 20-item
// per-chat bound (ErrQueueFull on the 21st), FIFO order within a priority,
// and an interrupt item (priority=1) jumping ahead of earlier queue items
// in NextQueued's ordering.
func TestChatQueue_EnqueueBoundAndOrder(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	// 20 enqueues ok; the 21st gives ErrQueueFull.
	for i := 0; i < 20; i++ {
		if _, err := s.EnqueueChatPrompt(chat.ID, "p", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
			t.Fatalf("EnqueueChatPrompt #%d error = %v", i+1, err)
		}
	}
	if _, err := s.EnqueueChatPrompt(chat.ID, "one too many", "{}", ChatQueueSourceHuman, 0, 0, 0); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("EnqueueChatPrompt #21 error = %v, want ErrQueueFull", err)
	}

	// FIFO: the first queued item comes out first.
	first, ok, err := s.NextQueued(chat.ID)
	if err != nil || !ok {
		t.Fatalf("NextQueued() = %v, %v, want the first item", first, err)
	}
	if first.Prompt != "p" || first.ID != 1 {
		t.Fatalf("NextQueued() = item %d (%q), want the oldest (id 1)", first.ID, first.Prompt)
	}

	// Delivered items free a slot for a new enqueue.
	if err := s.MarkDelivered(first.ID, "run-1"); err != nil {
		t.Fatalf("MarkDelivered() error = %v", err)
	}
	if _, err := s.EnqueueChatPrompt(chat.ID, "freed", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt after a delivery error = %v, err", err)
	}

	// The bound is per chat: another chat enqueues freely.
	other, err := s.CreateChat(task.ID, "other")
	if err != nil {
		t.Fatalf("CreateChat(other) error = %v", err)
	}
	if _, err := s.EnqueueChatPrompt(other.ID, "other chat", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt on a second chat error = %v", err)
	}

	// Free one more slot so the interrupt enqueue fits.
	second, ok, err := s.NextQueued(chat.ID)
	if err != nil || !ok {
		t.Fatalf("NextQueued() for the second item error = %v", err)
	}
	if err := s.MarkDelivered(second.ID, "run-2"); err != nil {
		t.Fatalf("MarkDelivered(second) error = %v", err)
	}

	// An interrupt item (priority=1) goes ahead of earlier queue items.
	if _, err := s.EnqueueChatPrompt(chat.ID, "interrupt me", "{}", ChatQueueSourceHuman, 1, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt(interrupt) error = %v", err)
	}
	next, ok, err := s.NextQueued(chat.ID)
	if err != nil || !ok {
		t.Fatalf("NextQueued() after interrupt error = %v", err)
	}
	if next.Priority != 1 || next.Prompt != "interrupt me" {
		t.Fatalf("NextQueued() = %+v, want the interrupt item first", next)
	}

	// A chat with nothing queued reports ok=false, not an error.
	if _, ok, err := s.NextQueued(other.ID + 999999); err != nil || ok {
		t.Fatalf("NextQueued(unknown chat) = %v, %v, want false, nil", ok, err)
	}
}
