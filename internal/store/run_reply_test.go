package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMigrate_RunReplyColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-reply.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	task := newTestTaskForRuns(t, s)
	for _, col := range []string{"reply_chat_id", "parent_notified_at"} {
		if _, err := s.db.Exec(`ALTER TABLE runs DROP COLUMN ` + col); err != nil {
			t.Fatalf("drop runs.%s: %v", col, err)
		}
	}
	if _, err := s.db.Exec(
		`INSERT INTO runs (id, task_id, provider, prompt, status, started_at) VALUES ('old-run', ?, 'glm', 'hi', 'done', ?)`,
		task.ID, time.Now().UTC(),
	); err != nil {
		t.Fatalf("seed old run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		got, err := s.GetRun("old-run")
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if got.ReplyChatID != 0 || got.ParentNotifiedAt != nil {
			t.Fatalf("Open() #%d reply = %d/%v, want 0/nil", i, got.ReplyChatID, got.ParentNotifiedAt)
		}
		s.Close()
	}
}

// newReplyRun creates a run in a fresh task with the given fields.
func newReplyRun(t *testing.T, s *Store, id, status string, started time.Time, finished *time.Time, replyChat int64) {
	t.Helper()
	task := newTestTaskForRuns(t, s)
	if _, err := s.CreateRun(Run{
		ID: id, TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: status, StartedAt: started, FinishedAt: finished,
		Purpose: "user", ReplyChatID: replyChat,
	}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", id, err)
	}
}

func TestStore_RunReplyChatRoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	now := time.Now().UTC()
	for id, rc := range map[string]int64{"with": chat.ID, "without": 0} {
		if _, err := s.CreateRun(Run{ID: id, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now, ReplyChatID: rc}); err != nil {
			t.Fatalf("CreateRun(%s) error = %v", id, err)
		}
		got, err := s.GetRun(id)
		if err != nil {
			t.Fatalf("GetRun(%s) error = %v", id, err)
		}
		if got.ReplyChatID != rc || got.ParentNotifiedAt != nil {
			t.Fatalf("%s: ReplyChatID/ParentNotifiedAt = %d/%v, want %d/nil", id, got.ReplyChatID, got.ParentNotifiedAt, rc)
		}
	}
}

func TestStore_ClaimRunParentNotifiedOnce(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	now := time.Now().UTC()
	newReplyRun(t, s, "r1", "done", now, &now, 0)
	at := now.Add(time.Second)
	ok, err := s.ClaimRunParentNotified("r1", at)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v, want true, nil", ok, err)
	}
	ok, err = s.ClaimRunParentNotified("r1", at.Add(time.Second))
	if err != nil || ok {
		t.Fatalf("second claim = %v, %v, want false, nil", ok, err)
	}
	got, err := s.GetRun("r1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.ParentNotifiedAt == nil || !got.ParentNotifiedAt.Equal(at) {
		t.Fatalf("ParentNotifiedAt = %v, want %v", got.ParentNotifiedAt, at)
	}
	if ok, err := s.ClaimRunParentNotified("missing", at); err != nil || ok {
		t.Fatalf("claim missing = %v, %v, want false, nil", ok, err)
	}
}

func TestStore_ListUnnotifiedChildRunsFilters(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-time.Hour)
	fin := now
	old := now.Add(-2 * time.Hour)

	mk := func(id, status string, started time.Time, finished *time.Time, reply int64) {
		t.Helper()
		if _, err := s.CreateRun(Run{ID: id, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: status, StartedAt: started, FinishedAt: finished, ReplyChatID: reply}); err != nil {
			t.Fatalf("CreateRun(%s) error = %v", id, err)
		}
	}
	mk("a-done", "done", now.Add(-time.Minute), &fin, chat.ID)
	mk("b-running", "running", now, nil, chat.ID)
	mk("c-notified", "done", now, &fin, chat.ID)
	mk("d-noreply", "done", now, &fin, 0)
	mk("e-old", "done", old, &old, chat.ID)
	mk("f-interrupted", "interrupted", now, nil, chat.ID)
	if ok, err := s.ClaimRunParentNotified("c-notified", now); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}

	got, err := s.ListUnnotifiedChildRuns(since)
	if err != nil {
		t.Fatalf("ListUnnotifiedChildRuns() error = %v", err)
	}
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	want := []string{"a-done", "f-interrupted"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}
