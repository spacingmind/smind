package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newQueueChat(t *testing.T, s *Store) Chat {
	t.Helper()
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "q")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	return chat
}

func enqueueAt(t *testing.T, s *Store, chatID int64, prompt string, nb *time.Time) ChatQueueItem {
	t.Helper()
	item, err := s.EnqueueChatPromptAt(chatID, prompt, "{}", ChatQueueSourceHuman, 0, 0, 0, "", nb)
	if err != nil {
		t.Fatalf("EnqueueChatPromptAt(%s) error = %v", prompt, err)
	}
	return item
}

func TestMigrate_ChatQueueNotBefore(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-queue.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	chat := newQueueChat(t, s)
	if _, err := s.db.Exec(`ALTER TABLE chat_queue DROP COLUMN not_before`); err != nil {
		t.Fatalf("drop chat_queue.not_before: %v", err)
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT INTO chat_queue (chat_id, prompt, run_config, source, priority, status, created_at, updated_at) VALUES (?, 'old', '{}', 'human', 0, 'queued', ?, ?)`,
		chat.ID, now, now,
	); err != nil {
		t.Fatalf("seed old item: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		items, err := s.ListChatQueue(chat.ID)
		if err != nil || len(items) != 1 {
			t.Fatalf("ListChatQueue() = %v, %v, want 1 item", items, err)
		}
		if items[0].NotBefore != nil {
			t.Fatalf("Open() #%d NotBefore = %v, want nil", i, items[0].NotBefore)
		}
		if _, ok, err := s.NextQueued(chat.ID); err != nil || !ok {
			t.Fatalf("NextQueued() = %v, %v, want the old item deliverable", ok, err)
		}
		s.Close()
	}
}

func TestStore_NextQueuedHonoursNotBefore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	chat := newQueueChat(t, s)
	now := time.Now().UTC().Truncate(time.Millisecond)
	hold := now.Add(time.Hour)
	a := enqueueAt(t, s, chat.ID, "held", &hold)
	b := enqueueAt(t, s, chat.ID, "free", nil)

	got, ok, err := s.NextQueuedAt(chat.ID, now)
	if err != nil || !ok || got.ID != b.ID {
		t.Fatalf("NextQueuedAt(now) = %+v, %v, %v, want item %d (held one skipped)", got, ok, err, b.ID)
	}
	if err := s.MarkDelivered(b.ID, "r"); err != nil {
		t.Fatalf("MarkDelivered() error = %v", err)
	}
	if _, ok, err := s.NextQueuedAt(chat.ID, now); err != nil || ok {
		t.Fatalf("NextQueuedAt(now) with only a held item = %v, %v, want none", ok, err)
	}
	// After the hold passes the held item is returned (and first, FIFO).
	c := enqueueAt(t, s, chat.ID, "later", nil)
	got, ok, err = s.NextQueuedAt(chat.ID, hold.Add(time.Second))
	if err != nil || !ok || got.ID != a.ID {
		t.Fatalf("NextQueuedAt(after hold) = %+v, %v, %v, want held item %d before %d", got, ok, err, a.ID, c.ID)
	}
	if got.NotBefore == nil || !got.NotBefore.Equal(hold) {
		t.Fatalf("NotBefore = %v, want %v", got.NotBefore, hold)
	}
}

func TestStore_EarliestHeldNotBefore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	chat := newQueueChat(t, s)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, ok, err := s.EarliestHeldNotBefore(now); err != nil || ok {
		t.Fatalf("empty: %v, %v, want none", ok, err)
	}
	past, soon, later := now.Add(-time.Minute), now.Add(time.Minute), now.Add(time.Hour)
	enqueueAt(t, s, chat.ID, "past", &past)
	enqueueAt(t, s, chat.ID, "later", &later)
	soonItem := enqueueAt(t, s, chat.ID, "soon", &soon)
	enqueueAt(t, s, chat.ID, "none", nil)

	got, ok, err := s.EarliestHeldNotBefore(now)
	if err != nil || !ok || !got.Equal(soon) {
		t.Fatalf("EarliestHeldNotBefore = %v, %v, %v, want %v", got, ok, err, soon)
	}
	// A delivered item no longer counts.
	if err := s.MarkDelivered(soonItem.ID, "r"); err != nil {
		t.Fatalf("MarkDelivered() error = %v", err)
	}
	got, ok, err = s.EarliestHeldNotBefore(now)
	if err != nil || !ok || !got.Equal(later) {
		t.Fatalf("after delivery = %v, %v, %v, want %v", got, ok, err, later)
	}
}

func TestStore_SetQueuedNotBeforeOnlyWhileQueued(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	chat := newQueueChat(t, s)
	item := enqueueAt(t, s, chat.ID, "x", nil)
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	if err := s.SetQueuedNotBefore(item.ID, &at); err != nil {
		t.Fatalf("SetQueuedNotBefore() error = %v", err)
	}
	got, err := s.GetChatQueueItem(item.ID)
	if err != nil || got.NotBefore == nil || !got.NotBefore.Equal(at) {
		t.Fatalf("after set: %+v, %v, want NotBefore %v", got, err, at)
	}
	if err := s.SetQueuedNotBefore(item.ID, nil); err != nil {
		t.Fatalf("clear error = %v", err)
	}
	if got, _ := s.GetChatQueueItem(item.ID); got.NotBefore != nil {
		t.Fatalf("after clear NotBefore = %v, want nil", got.NotBefore)
	}
	if err := s.MarkDelivered(item.ID, "r"); err != nil {
		t.Fatalf("MarkDelivered() error = %v", err)
	}
	if err := s.SetQueuedNotBefore(item.ID, &at); err == nil {
		t.Fatal("SetQueuedNotBefore on a delivered item = nil, want an error")
	}
}
