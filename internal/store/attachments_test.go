package store

import (
	"path/filepath"
	"sort"
	"testing"
	"time"
)

const (
	setA = `{"set":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","items":[{"id":"1","kind":"file"}]}`
	setB = `{"set":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","items":[{"id":"1","kind":"file"}]}`
	setC = `{"set":"cccccccccccccccccccccccccccccccc","items":[{"id":"1","kind":"file"}]}`
)

func TestRunAttachments_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	if _, err := s.CreateRun(Run{ID: "ra", TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: time.Now().UTC(), Attachments: setA}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "rn", TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun("ra")
	if err != nil || got.Attachments != setA {
		t.Fatalf("GetRun attachments = %q, %v", got.Attachments, err)
	}
	if none, _ := s.GetRun("rn"); none.Attachments != "" {
		t.Fatalf("run without attachments = %q", none.Attachments)
	}
	recent, err := s.ListRecentRuns(10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recent {
		if r.ID == "ra" && r.Attachments == setA {
			found = true
		}
	}
	if !found {
		t.Fatal("ListRecentRuns lost attachments")
	}
}

func TestChatQueueAttachments_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.EnqueueChatPromptAttach(chat.ID, "p", "{}", ChatQueueSourceHuman, 0, 0, 0, setA)
	if err != nil || item.Attachments != setA {
		t.Fatalf("enqueue = %+v, %v", item, err)
	}
	plain, err := s.EnqueueChatPrompt(chat.ID, "q", "{}", ChatQueueSourceHuman, 0, 0, 0)
	if err != nil || plain.Attachments != "" {
		t.Fatalf("plain enqueue = %+v, %v", plain, err)
	}
	next, ok, err := s.NextQueued(chat.ID)
	if err != nil || !ok || next.Attachments != setA {
		t.Fatalf("NextQueued = %+v, %v, %v", next, ok, err)
	}
	list, err := s.ListChatQueue(chat.ID)
	if err != nil || len(list) != 2 || list[0].Attachments != setA || list[1].Attachments != "" {
		t.Fatalf("ListChatQueue = %+v, %v", list, err)
	}
}

func TestMigrateAttachmentColumns_Idempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "att.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		for _, tbl := range []string{"runs", "chat_queue"} {
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'attachments'`, tbl).Scan(&n); err != nil || n != 1 {
				t.Fatalf("%s attachments column count = %d, %v", tbl, n, err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDeleteTask_ReturnsAttachmentSets: AttachmentSetsOfTask (read before the
// delete) names every set of the task's runs and queued prompts and nothing
// of other tasks; ReferencedAttachmentSets skips non-queued queue items.
func TestDeleteTask_ReturnsAttachmentSets(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	other := newTestTaskForRuns(t, s)
	chat, err := s.CreateChat(task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "r1", TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "ok", StartedAt: time.Now().UTC(), Attachments: setA}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRun(Run{ID: "r2", TaskID: other.ID, Provider: "glm", Prompt: "p", Status: "ok", StartedAt: time.Now().UTC(), Attachments: setC}); err != nil {
		t.Fatal(err)
	}
	q, err := s.EnqueueChatPromptAttach(chat.ID, "p", "{}", ChatQueueSourceHuman, 0, 0, 0, setB)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AttachmentSetsOfTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got[1] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("AttachmentSetsOfTask = %v", got)
	}
	ref, err := s.ReferencedAttachmentSets()
	if err != nil || len(ref) != 3 {
		t.Fatalf("ReferencedAttachmentSets = %v, %v", ref, err)
	}
	if err := s.MarkCancelled(q.ID, ChatQueueCancelUser, "x"); err != nil {
		t.Fatal(err)
	}
	ref, _ = s.ReferencedAttachmentSets()
	if ref["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] || !ref["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] || !ref["cccccccccccccccccccccccccccccccc"] {
		t.Fatalf("after cancel ReferencedAttachmentSets = %v", ref)
	}
}
