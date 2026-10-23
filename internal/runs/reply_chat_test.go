package runs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// replyEnv: e.task is the PARENT; child is a task with that parent. Chats:
// parentChat belongs to the parent, sibling is another child-less task.
type replyEnv struct {
	*queuedTestEnv
	child      store.Task
	parentChat store.Chat
	sibling    store.Task
	siblingCh  store.Chat
}

func newReplyEnv(t *testing.T, childScenario string) *replyEnv {
	t.Helper()
	e := newQueuedTestEnv(t, "")
	child, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "Child", workspace.WithParentTask(e.task.ID))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	sibling, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "Sibling", workspace.WithParentTask(e.task.ID))
	if err != nil {
		t.Fatalf("CreateTask(sibling) error = %v", err)
	}
	if childScenario != "" {
		if err := os.WriteFile(filepath.Join(*child.WorktreePath, "scenario"), []byte(childScenario), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
	}
	pc, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat(parent) error = %v", err)
	}
	sc, err := e.wm.DefaultChat(sibling.ID)
	if err != nil {
		t.Fatalf("DefaultChat(sibling) error = %v", err)
	}
	return &replyEnv{queuedTestEnv: e, child: child, parentChat: pc, sibling: sibling, siblingCh: sc}
}

// send does an immediate StartWhenBusy on taskID's default chat and returns
// the persisted row's ReplyChatID once the run is done.
func (e *replyEnv) send(t *testing.T, taskID int64, source string, fromTask, fromChat int64, opts ...StartOption) int64 {
	t.Helper()
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, taskID, 0, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, source, fromTask, fromChat, opts...)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	if out.Queued {
		t.Fatalf("StartWhenBusy() queued, want immediate start")
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	return row.ReplyChatID
}

func TestReplyChat_SetOnlyForAgentSendFromParent(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "")
	agent, human, orch := store.ChatQueueSourceAgent, store.ChatQueueSourceHuman, store.ChatQueueSourceOrchestrator

	if got := e.send(t, e.child.ID, agent, e.task.ID, e.parentChat.ID); got != e.parentChat.ID {
		t.Fatalf("agent send from parent: ReplyChatID = %d, want %d", got, e.parentChat.ID)
	}
	if got := e.send(t, e.child.ID, human, 0, 0); got != 0 {
		t.Fatalf("human send: ReplyChatID = %d, want 0", got)
	}
	if got := e.send(t, e.child.ID, orch, 0, 0); got != 0 {
		t.Fatalf("orchestrator send: ReplyChatID = %d, want 0", got)
	}
	if got := e.send(t, e.child.ID, agent, e.sibling.ID, e.siblingCh.ID); got != 0 {
		t.Fatalf("agent send from sibling: ReplyChatID = %d, want 0", got)
	}
}

func TestReplyChat_MustBelongToParent(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "")
	agent := store.ChatQueueSourceAgent

	// fromChatID is a chat of another task.
	if got := e.send(t, e.child.ID, agent, e.task.ID, e.siblingCh.ID); got != 0 {
		t.Fatalf("foreign chat: ReplyChatID = %d, want 0", got)
	}
	// An archived chat of the parent.
	extra, err := e.wm.CreateChat(e.task.ID, "extra")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := e.wm.ArchiveChat(extra.ID); err != nil {
		t.Fatalf("ArchiveChat() error = %v", err)
	}
	if got := e.send(t, e.child.ID, agent, e.task.ID, extra.ID); got != 0 {
		t.Fatalf("archived chat: ReplyChatID = %d, want 0", got)
	}
}

func TestReplyChat_RootTaskNever(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "")
	// e.task is a root task (no parent); an agent send from the sibling
	// must not produce a reply chat.
	if got := e.send(t, e.task.ID, store.ChatQueueSourceAgent, e.sibling.ID, e.siblingCh.ID); got != 0 {
		t.Fatalf("root task: ReplyChatID = %d, want 0", got)
	}
}

func TestReplyChat_OptOut(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "")
	if got := e.send(t, e.child.ID, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID, WithNotifyOnFinish(false)); got != 0 {
		t.Fatalf("notify=false: ReplyChatID = %d, want 0", got)
	}
	if got := e.send(t, e.child.ID, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID, WithNotifyOnFinish(true)); got != e.parentChat.ID {
		t.Fatalf("notify=true: ReplyChatID = %d, want %d", got, e.parentChat.ID)
	}
}

func TestReplyChat_QueuedItemKeepsItAcrossDelivery(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "hang")
	runA, err := e.reg.Start(context.Background(), e.wm, e.runner, e.child.ID, 0, taskrunner.ProviderGLM, "a", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start(a) error = %v", err)
	}
	t.Cleanup(func() { _ = e.reg.Stop(runA) })
	chatID := e.chatIDOf(t, runA)
	waitForHistoryLen(t, e.reg, runA, 1, 5*time.Second)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.child.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
	if err != nil {
		t.Fatalf("StartWhenBusy(queue) error = %v", err)
	}
	if !out.Queued {
		t.Fatalf("outcome = %+v, want queued", out)
	}
	// Let the delivered run finish normally, then release A.
	if err := os.WriteFile(filepath.Join(*e.child.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}
	if err := e.reg.Stop(runA); err != nil {
		t.Fatalf("Stop(a) error = %v", err)
	}
	item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	if item.RunID == nil {
		t.Fatal("delivered item has no run id")
	}
	waitForStatus(t, e.reg, *item.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(*item.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.ReplyChatID != e.parentChat.ID {
		t.Fatalf("delivered run ReplyChatID = %d, want %d", row.ReplyChatID, e.parentChat.ID)
	}
	if rowA, err := e.st.GetRun(runA); err != nil || rowA.ReplyChatID != 0 {
		t.Fatalf("run A ReplyChatID = %d (err %v), want 0", rowA.ReplyChatID, err)
	}
}

func TestReplyChat_RehydratedRunKeepsIt(t *testing.T) {
	t.Parallel()
	e := newReplyEnv(t, "")
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.child.ID, 0, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)

	reg2 := newTestRegistry(t, e.st)
	reg2.SetStarter(e.wm, e.runner)
	r, gerr := reg2.get(out.RunID)
	if gerr != nil {
		t.Fatalf("rehydrated registry has no run %s: %v", out.RunID, gerr)
	}
	if r.replyChatID != e.parentChat.ID {
		t.Fatalf("rehydrated replyChatID = %d, want %d", r.replyChatID, e.parentChat.ID)
	}
	if len(reg2.List(e.chatIDOf(t, out.RunID))) == 0 {
		t.Fatal("rehydrated List() is empty")
	}
	row, err := e.st.GetRun(out.RunID)
	if err != nil || row.ReplyChatID != e.parentChat.ID {
		t.Fatalf("store row ReplyChatID = %d (err %v), want %d", row.ReplyChatID, err, e.parentChat.ID)
	}
}
