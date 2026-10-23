package store

import (
	"errors"
	"testing"
	"time"
)

func childEventTask(t *testing.T, s *Store, wsID int64, parent *int64) Task {
	t.Helper()
	task, err := s.CreateTask(Task{WorkspaceID: wsID, Title: "t", Status: "created", ParentTaskID: parent})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}

func childEventWS(t *testing.T, s *Store) int64 {
	t.Helper()
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return ws.ID
}

func TestSubtreeBusy(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := childEventWS(t, s)
	root := childEventTask(t, s, ws, nil)
	child := childEventTask(t, s, ws, &root.ID)
	grand := childEventTask(t, s, ws, &child.ID)
	sibling := childEventTask(t, s, ws, &root.ID)

	busy := func(id int64) bool {
		t.Helper()
		b, err := s.SubtreeBusy(id)
		if err != nil {
			t.Fatalf("SubtreeBusy(%d) error = %v", id, err)
		}
		return b
	}
	if busy(root.ID) {
		t.Fatal("idle tree reported busy")
	}

	now := time.Now().UTC()
	if _, err := s.CreateRun(Run{ID: "g1", TaskID: grand.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if !busy(root.ID) || !busy(child.ID) || !busy(grand.ID) {
		t.Fatal("running grandchild run not seen by ancestors")
	}
	if busy(sibling.ID) {
		t.Fatal("sibling subtree must ignore the grandchild's run")
	}
	if _, err := s.UpdateRunStatus("g1", "done", &now, "", ""); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	if busy(root.ID) {
		t.Fatal("finished run still busy")
	}

	chat, err := s.CreateChat(child.ID, "c")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := s.EnqueueChatPrompt(chat.ID, "x", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
		t.Fatalf("EnqueueChatPrompt() error = %v", err)
	}
	if !busy(root.ID) || !busy(child.ID) {
		t.Fatal("queued item in child chat not seen")
	}
	if busy(grand.ID) || busy(sibling.ID) {
		t.Fatal("queued item leaked outside its subtree")
	}
}

type childEventFixture struct {
	s    *Store
	chat Chat
	now  time.Time
}

func newChildEventFixture(t *testing.T, runs ...string) childEventFixture {
	t.Helper()
	s := newTestStore(t)
	ws := childEventWS(t, s)
	parent := childEventTask(t, s, ws, nil)
	child := childEventTask(t, s, ws, &parent.ID)
	chat, err := s.CreateChat(parent.ID, "p")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range runs {
		fin := now
		if _, err := s.CreateRun(Run{ID: id, TaskID: child.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: now, FinishedAt: &fin, ReplyChatID: chat.ID}); err != nil {
			t.Fatalf("CreateRun(%s) error = %v", id, err)
		}
	}
	return childEventFixture{s: s, chat: chat, now: now}
}

func (f childEventFixture) notified(t *testing.T, id string) bool {
	t.Helper()
	r, err := f.s.GetRun(id)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", id, err)
	}
	return r.ParentNotifiedAt != nil
}

func TestEnqueueChildEvent_NewItemClaimsRun(t *testing.T) {
	t.Parallel()
	f := newChildEventFixture(t, "r1")
	item, appended, err := f.s.EnqueueChildEvent(ChildEventInput{
		ChatID: f.chat.ID, RunIDs: []string{"r1"}, Block: "B1", RunConfig: "{}",
		Now: f.now, Debounce: 5 * time.Second, MaxHold: time.Minute,
	})
	if err != nil || appended {
		t.Fatalf("EnqueueChildEvent() = %v, appended=%v", err, appended)
	}
	if item.Source != ChatQueueSourceChildEvent || item.Prompt != "B1" || item.Priority != 0 || item.FromTaskID != nil || item.FromChatID != nil {
		t.Fatalf("item = %+v", item)
	}
	if item.NotBefore == nil || !item.NotBefore.Equal(f.now.Add(5*time.Second)) {
		t.Fatalf("NotBefore = %v, want now+5s", item.NotBefore)
	}
	if !f.notified(t, "r1") {
		t.Fatal("run not claimed")
	}
}

func TestEnqueueChildEvent_AppendsToQueuedItemAndPushesNotBefore(t *testing.T) {
	t.Parallel()
	f := newChildEventFixture(t, "r1", "r2", "r3")
	in := ChildEventInput{ChatID: f.chat.ID, RunConfig: "{}", Debounce: 10 * time.Second, MaxHold: 15 * time.Second}

	in.RunIDs, in.Block, in.Now = []string{"r1"}, "B1", f.now
	first, _, err := f.s.EnqueueChildEvent(in)
	if err != nil {
		t.Fatalf("first error = %v", err)
	}
	in.RunIDs, in.Block, in.Now = []string{"r2"}, "B2", f.now.Add(8*time.Second)
	second, appended, err := f.s.EnqueueChildEvent(in)
	if err != nil || !appended {
		t.Fatalf("second = %v, appended=%v", err, appended)
	}
	if second.ID != first.ID || second.Prompt != "B1\n\nB2" {
		t.Fatalf("second = %+v", second)
	}
	// now+8s+10s = 18s, capped at created_at+15s.
	if want := first.CreatedAt.Add(15 * time.Second); !second.NotBefore.Equal(want) {
		t.Fatalf("NotBefore = %v, want cap %v", second.NotBefore, want)
	}
	if !second.NotBefore.After(*first.NotBefore) {
		t.Fatalf("NotBefore did not move later: %v -> %v", first.NotBefore, second.NotBefore)
	}
	// Past the cap entirely: never earlier than now.
	in.RunIDs, in.Block, in.Now = []string{"r3"}, "B3", first.CreatedAt.Add(time.Minute)
	third, _, err := f.s.EnqueueChildEvent(in)
	if err != nil {
		t.Fatalf("third error = %v", err)
	}
	if !third.NotBefore.Equal(in.Now.UTC()) {
		t.Fatalf("third NotBefore = %v, want now %v", third.NotBefore, in.Now)
	}
	if items, _ := f.s.ListChatQueue(f.chat.ID); len(items) != 1 {
		t.Fatalf("queue len = %d, want 1", len(items))
	}
}

func TestEnqueueChildEvent_AlreadyNotifiedIsNoop(t *testing.T) {
	t.Parallel()
	f := newChildEventFixture(t, "r1")
	if ok, err := f.s.ClaimRunParentNotified("r1", f.now); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	_, _, err := f.s.EnqueueChildEvent(ChildEventInput{ChatID: f.chat.ID, RunIDs: []string{"r1"}, Block: "B", RunConfig: "{}", Now: f.now, Debounce: time.Second, MaxHold: time.Minute})
	if !errors.Is(err, ErrChildEventAlreadyNotified) {
		t.Fatalf("error = %v, want ErrChildEventAlreadyNotified", err)
	}
	if items, _ := f.s.ListChatQueue(f.chat.ID); len(items) != 0 {
		t.Fatalf("queue len = %d, want 0", len(items))
	}
}

func TestEnqueueChildEvent_QueueFullClaimsNothing(t *testing.T) {
	t.Parallel()
	f := newChildEventFixture(t, "r1")
	for i := 0; i < chatQueueBound; i++ {
		if _, err := f.s.EnqueueChatPrompt(f.chat.ID, "x", "{}", ChatQueueSourceHuman, 0, 0, 0); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	_, _, err := f.s.EnqueueChildEvent(ChildEventInput{ChatID: f.chat.ID, RunIDs: []string{"r1"}, Block: "B", RunConfig: "{}", Now: f.now, Debounce: time.Second, MaxHold: time.Minute})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("error = %v, want ErrQueueFull", err)
	}
	if f.notified(t, "r1") {
		t.Fatal("run claimed despite ErrQueueFull")
	}
}

func TestEnqueueChildEvent_AtomicOnFailure(t *testing.T) {
	t.Parallel()
	f := newChildEventFixture(t, "r1")
	_, _, err := f.s.EnqueueChildEvent(ChildEventInput{ChatID: 999999, RunIDs: []string{"r1"}, Block: "B", RunConfig: "{}", Now: f.now, Debounce: time.Second, MaxHold: time.Minute})
	if err == nil {
		t.Fatal("EnqueueChildEvent() with bad chat id = nil error, want FK failure")
	}
	if f.notified(t, "r1") {
		t.Fatal("run stayed claimed after failed enqueue")
	}
}
