package store

import (
	"errors"
	"testing"
	"time"
)

func newFleetNotifyFixture(t *testing.T, s *Store) (Chat, int64) {
	t.Helper()
	ws := childEventWS(t, s)
	parent := childEventTask(t, s, ws, nil)
	chat, err := s.CreateChat(parent.ID, "p")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	watchID, err := s.PutFleetRemoteTaskAndWatch(
		FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", LocalTaskID: parent.ID, Title: "remote work", State: "running"},
		&FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1", LocalTaskID: parent.ID, ChatID: chat.ID, Notify: true},
	)
	if err != nil {
		t.Fatalf("PutFleetRemoteTaskAndWatch() error = %v", err)
	}
	return chat, watchID
}

func childEventItems(t *testing.T, s *Store, chatID int64) []ChatQueueItem {
	t.Helper()
	items, err := s.ListChatQueue(chatID)
	if err != nil {
		t.Fatalf("ListChatQueue() error = %v", err)
	}
	var out []ChatQueueItem
	for _, it := range items {
		if it.Source == ChatQueueSourceChildEvent {
			out = append(out, it)
		}
	}
	return out
}

func TestEnqueueChildEventClosingWatchOnce(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	chat, watchID := newFleetNotifyFixture(t, s)
	now := time.Now().UTC()
	in := ChildEventInput{ChatID: chat.ID, RunIDs: []string{"ghost"}, Block: "B", RunConfig: "{}", Now: now, Debounce: time.Second, MaxHold: time.Minute}

	item, err := s.EnqueueChildEventClosingWatch(in, watchID, "finished")
	if err != nil {
		t.Fatalf("first EnqueueChildEventClosingWatch() error = %v", err)
	}
	if item.Source != ChatQueueSourceChildEvent || item.Prompt != "B" {
		t.Fatalf("item = %+v", item)
	}
	if _, err := s.EnqueueChildEventClosingWatch(in, watchID, "finished"); !errors.Is(err, ErrChildEventAlreadyNotified) {
		t.Fatalf("second EnqueueChildEventClosingWatch() = %v, want ErrChildEventAlreadyNotified", err)
	}
	if items := childEventItems(t, s, chat.ID); len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("child_event items = %+v, want only the first", items)
	}
	w, err := s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != FleetWatchClosed || w.ClosedReason != "finished" {
		t.Fatalf("watch = %+v, want closed/finished", w)
	}
}

func TestEnqueueChildEventClosingWatchQueueFullKeepsWatchOpen(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	chat, watchID := newFleetNotifyFixture(t, s)
	for i := 0; i < chatQueueBound; i++ {
		if _, err := s.EnqueueChatPrompt(chat.ID, "x", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}

	_, err := s.EnqueueChildEventClosingWatch(ChildEventInput{
		ChatID: chat.ID, Block: "B", RunConfig: "{}", Now: time.Now().UTC(),
		Debounce: time.Second, MaxHold: time.Minute,
	}, watchID, "finished")
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("EnqueueChildEventClosingWatch() = %v, want ErrQueueFull", err)
	}
	if items := childEventItems(t, s, chat.ID); len(items) != 0 {
		t.Fatalf("child_event items = %+v, want none", items)
	}
	w, err := s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != FleetWatchOpen {
		t.Fatalf("watch = %+v, want still open: the rollback undoes the close so the poll retries", w)
	}
}

func TestCloseFleetWatchIfOpen(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	_, watchID := newFleetNotifyFixture(t, s)

	closed, err := s.CloseFleetWatchIfOpen(watchID, "done")
	if err != nil || !closed {
		t.Fatalf("first CloseFleetWatchIfOpen() = %t, %v; want true", closed, err)
	}
	closed, err = s.CloseFleetWatchIfOpen(watchID, "other")
	if err != nil || closed {
		t.Fatalf("second CloseFleetWatchIfOpen() = %t, %v; want false", closed, err)
	}
	w, err := s.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != FleetWatchClosed || w.ClosedReason != "done" {
		t.Fatalf("watch = %+v, want closed with the first reason kept", w)
	}
}
