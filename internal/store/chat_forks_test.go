package store

import (
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatForks_SchemaAndAccessors(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	parent, _ := s.CreateChat(task.ID, "parent")
	child, _ := s.CreateChat(task.ID, "child")

	if _, err := s.GetChatFork(child.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatFork(unforked) error = %v, want sql.ErrNoRows", err)
	}
	if err := s.InsertChatFork(ChatFork{ChatID: child.ID, ParentChatID: parent.ID, ForkedAtRunID: "run-1"}); err != nil {
		t.Fatalf("InsertChatFork() error = %v", err)
	}
	got, err := s.GetChatFork(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentChatID != parent.ID || got.ForkedAtRunID != "run-1" || got.Kind != ChatForkKindFork || got.Mode != ChatForkModeNative || got.SeedUsedAt != nil {
		t.Fatalf("GetChatFork() = %+v", got)
	}
	if err := s.InsertChatFork(ChatFork{ChatID: child.ID, ParentChatID: parent.ID, ForkedAtRunID: "run-2"}); err == nil {
		t.Fatal("a second InsertChatFork for the same chat succeeded; a fork record is write-once")
	}
	byTask, err := s.ListChatForksByTask(task.ID)
	if err != nil || len(byTask) != 1 || byTask[child.ID].ParentChatID != parent.ID {
		t.Fatalf("ListChatForksByTask() = %+v, %v", byTask, err)
	}
}

// Forking adds a side table: the chat rows and every query over them read
// exactly what they did before.
func TestChatForks_ChatsQueriesUnchanged(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	parent, _ := s.CreateChat(task.ID, "parent")
	child, _ := s.CreateChat(task.ID, "child")
	before, _ := s.GetChat(child.ID)
	if err := s.InsertChatFork(ChatFork{ChatID: child.ID, ParentChatID: parent.ID, ForkedAtRunID: "r", Mode: ChatForkModeSeeded, Seed: "x"}); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetChat(child.ID)
	if err != nil || after != before {
		t.Fatalf("GetChat after a fork = %+v, %v; want %+v", after, err, before)
	}
	list, err := s.ListChatsByTask(task.ID, true)
	if err != nil || len(list) != 2 || list[1] != before {
		t.Fatalf("ListChatsByTask after a fork = %+v, %v", list, err)
	}
}

func TestChatForks_ClaimSeedOnce(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	parent, _ := s.CreateChat(task.ID, "parent")
	child, _ := s.CreateChat(task.ID, "child")
	native, _ := s.CreateChat(task.ID, "native")
	if err := s.InsertChatFork(ChatFork{ChatID: child.ID, ParentChatID: parent.ID, ForkedAtRunID: "r", Mode: ChatForkModeSeeded, Seed: "earlier turns"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChatFork(ChatFork{ChatID: native.ID, ParentChatID: parent.ID, ForkedAtRunID: "r"}); err != nil {
		t.Fatal(err)
	}
	var got atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if seed, ok, err := s.ClaimChatForkSeed(child.ID, time.Now()); err == nil && ok && seed == "earlier turns" {
				got.Add(1)
			}
		}()
	}
	wg.Wait()
	if got.Load() != 1 {
		t.Fatalf("%d claimers got the seed, want exactly 1", got.Load())
	}
	if _, ok, _ := s.ClaimChatForkSeed(native.ID, time.Now()); ok {
		t.Fatal("a native fork has no seed to claim")
	}
	if _, ok, _ := s.ClaimChatForkSeed(parent.ID, time.Now()); ok {
		t.Fatal("an unforked chat has no seed to claim")
	}
}

func TestClaudeTotalsLog_BaselineAtAnchor(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, _ := s.CreateChat(task.ID, "c")
	t0 := time.Now().UTC().Truncate(time.Second)
	if js, err := s.ChatClaudeTotalsAt(c.ID, t0); err != nil || js != "" {
		t.Fatalf("ChatClaudeTotalsAt(empty log) = %q, %v; want none", js, err)
	}
	for i, js := range []string{`{"costUsd":1}`, `{"costUsd":2}`, `{"costUsd":3}`} {
		if err := s.AppendChatClaudeTotals(c.ID, t0.Add(time.Duration(i)*time.Minute), js); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		at   time.Duration
		want string
	}{{-time.Second, ""}, {0, `{"costUsd":1}`}, {90 * time.Second, `{"costUsd":2}`}, {time.Hour, `{"costUsd":3}`}} {
		if got, err := s.ChatClaudeTotalsAt(c.ID, t0.Add(tc.at)); err != nil || got != tc.want {
			t.Errorf("ChatClaudeTotalsAt(+%v) = %q, %v; want %q", tc.at, got, err, tc.want)
		}
	}
}

func TestChatForks_DeleteTaskRemovesRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	parent, _ := s.CreateChat(task.ID, "parent")
	child, _ := s.CreateChat(task.ID, "child")
	if err := s.InsertChatFork(ChatFork{ChatID: child.ID, ParentChatID: parent.ID, ForkedAtRunID: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendChatClaudeTotals(child.ID, time.Now(), `{}`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	for _, q := range []string{`SELECT COUNT(*) FROM chat_forks`, `SELECT COUNT(*) FROM chat_claude_totals`} {
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s = %d, %v; want 0 after the task is deleted", q, n, err)
		}
	}
}
