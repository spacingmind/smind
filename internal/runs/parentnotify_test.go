package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// notifyEnv is a replyEnv whose parent chat already has one finished run (the
// notifier copies its start settings) and whose debounce is 0 (deliver at
// once) unless a test sets one.
type notifyEnv struct {
	*replyEnv
}

func newNotifyEnv(t *testing.T) *notifyEnv {
	t.Helper()
	e := &notifyEnv{newReplyEnv(t, "")}
	e.reg.SetNotifyDebounce(0)
	e.waitDone(t, e.startRun(t, "init"))
	return e
}

func (e *notifyEnv) waitDone(t *testing.T, runID string) {
	t.Helper()
	waitForStatus(t, e.reg, runID, StatusDone, 10*time.Second)
}

func waitUntil(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// events returns chatID's child_event queue items (all statuses), oldest first.
func (e *notifyEnv) events(t *testing.T, chatID int64) []store.ChatQueueItem {
	t.Helper()
	items, err := e.st.ListChatQueue(chatID)
	if err != nil {
		t.Fatalf("ListChatQueue() error = %v", err)
	}
	var out []store.ChatQueueItem
	for _, it := range items {
		if it.Source == store.ChatQueueSourceChildEvent {
			out = append(out, it)
		}
	}
	return out
}

func (e *notifyEnv) chatOf(t *testing.T, taskID int64) int64 {
	t.Helper()
	c, err := e.wm.DefaultChat(taskID)
	if err != nil {
		t.Fatalf("DefaultChat(%d) error = %v", taskID, err)
	}
	return c.ID
}

func (e *notifyEnv) notified(t *testing.T, runID string) bool {
	t.Helper()
	r, err := e.st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", runID, err)
	}
	return r.ParentNotifiedAt != nil
}

// childRun starts a run on taskID as an agent send from the parent task, and
// returns once the run reached want. The notifier evaluation that follows
// finish is serialised by notifyMu, so the explicit evaluate here waits for
// the in-flight one and makes negative assertions safe.
func (e *notifyEnv) childRun(t *testing.T, taskID int64, want Status, opts ...StartOption) string {
	t.Helper()
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, taskID, 0, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID, opts...)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, want, 10*time.Second)
	e.reg.evaluateChildNotifications(taskID)
	return out.RunID
}

func (e *notifyEnv) newChild(t *testing.T, parent int64, title string) store.Task {
	t.Helper()
	task, err := e.wm.CreateTask(e.task.WorkspaceID, nil, title, workspace.WithParentTask(parent))
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", title, err)
	}
	return task
}

func writeScenario(t *testing.T, task store.Task, scenario string) {
	t.Helper()
	p := filepath.Join(*task.WorktreePath, "scenario")
	if scenario == "" {
		_ = os.Remove(p)
		return
	}
	if err := os.WriteFile(p, []byte(scenario), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
}

// release lets a "hold" scenario run end and resets the scenario so later
// prompts on the task reply at once.
func release(t *testing.T, task store.Task) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "release"), nil, 0o644); err != nil {
		t.Fatalf("write release: %v", err)
	}
	writeScenario(t, task, "")
}

// row inserts a finished run row directly (no process, no history).
func (e *notifyEnv) row(t *testing.T, id string, taskID int64, status, stopReason, purpose string, reply int64, finished time.Time) {
	t.Helper()
	fin := finished
	if _, err := e.st.CreateRun(store.Run{
		ID: id, TaskID: taskID, ChatID: e.chatOf(t, taskID), Provider: "glm", Prompt: "p", Status: status,
		StartedAt: finished.Add(-time.Second), FinishedAt: &fin, StopReason: stopReason, Purpose: purpose, ReplyChatID: reply,
	}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", id, err)
	}
}

func TestParentNotify_DoneAndErrorEnqueueOneItem(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)

	e.childRun(t, e.child.ID, StatusDone)
	waitUntil(t, "first item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	first := e.events(t, e.parentChat.ID)[0]
	if !strings.Contains(first.Prompt, "task #") || !strings.Contains(first.Prompt, ": done") || !strings.Contains(first.Prompt, `<child-response task="`) {
		t.Fatalf("done prompt = %q", first.Prompt)
	}
	if first.FromTaskID != nil || first.Priority != 0 {
		t.Fatalf("item = %+v, want daemon-authored priority 0", first)
	}

	writeScenario(t, e.child, "plain-error")
	e.childRun(t, e.child.ID, StatusError)
	waitUntil(t, "second item", func() bool { return len(e.events(t, e.parentChat.ID)) == 2 })
	if got := e.events(t, e.parentChat.ID)[1].Prompt; !strings.Contains(got, ": error") {
		t.Fatalf("error prompt = %q", got)
	}
}

func TestParentNotify_DaemonRestartStopNotifies(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "dr1", e.child.ID, "stopped", StopReasonDaemonRestart, "user", e.parentChat.ID, time.Now())
	e.reg.evaluateChildNotifications(e.child.ID)
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	if got := e.events(t, e.parentChat.ID)[0].Prompt; !strings.Contains(got, "stopped (needs-continue) (daemon restart)") {
		t.Fatalf("prompt = %q", got)
	}
	if !e.notified(t, "dr1") {
		t.Fatal("run not claimed")
	}
}

func TestParentNotify_PlainStopDoesNotNotify(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "ps1", e.child.ID, "stopped", "", "user", e.parentChat.ID, time.Now())
	e.reg.evaluateChildNotifications(e.child.ID)
	e.row(t, "ps2", e.sibling.ID, "interrupted", StopReasonDaemonRestartFresh, "user", e.parentChat.ID, time.Now())
	e.reg.evaluateChildNotifications(e.sibling.ID)
	for _, id := range []string{"ps1", "ps2"} {
		if !e.notified(t, id) {
			t.Fatalf("%s not claimed", id)
		}
	}
	if got := e.events(t, e.parentChat.ID); len(got) != 0 {
		t.Fatalf("items = %+v, want none", got)
	}
}

func TestParentNotify_SystemPurposeRunDoesNotNotify(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "sys1", e.child.ID, "done", "", store.RunPurposeProbe, e.parentChat.ID, time.Now())
	e.reg.evaluateChildNotifications(e.child.ID)
	if !e.notified(t, "sys1") {
		t.Fatal("system run not claimed")
	}
	if got := e.events(t, e.parentChat.ID); len(got) != 0 {
		t.Fatalf("items = %+v, want none", got)
	}
}

func TestParentNotify_RootTaskNothing(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	runID := e.startRun(t, "again")
	e.waitDone(t, runID)
	e.reg.evaluateChildNotifications(e.task.ID)
	row, err := e.st.GetRun(runID)
	if err != nil || row.ReplyChatID != 0 {
		t.Fatalf("root run ReplyChatID = %d, %v, want 0", row.ReplyChatID, err)
	}
	for _, c := range []int64{e.parentChat.ID, e.chatOf(t, e.child.ID)} {
		if got := e.events(t, c); len(got) != 0 {
			t.Fatalf("chat %d items = %+v, want none", c, got)
		}
	}
}

func TestParentNotify_OptOutAndKillSwitch(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)

	id := e.childRun(t, e.child.ID, StatusDone, WithNotifyOnFinish(false))
	row, err := e.st.GetRun(id)
	if err != nil || row.ReplyChatID != 0 {
		t.Fatalf("opt-out ReplyChatID = %d, %v, want 0", row.ReplyChatID, err)
	}
	if got := e.events(t, e.parentChat.ID); len(got) != 0 {
		t.Fatalf("opt-out items = %+v, want none", got)
	}

	e.reg.SetNotifyParent(false)
	id = e.childRun(t, e.sibling.ID, StatusDone)
	if !e.notified(t, id) {
		t.Fatal("kill switch: run not claimed")
	}
	if got := e.events(t, e.parentChat.ID); len(got) != 0 {
		t.Fatalf("kill switch items = %+v, want none", got)
	}
}

func TestParentNotify_ArchivedParentChatNothing(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	extra, err := e.wm.CreateChat(e.task.ID, "extra")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := e.wm.ArchiveChat(extra.ID); err != nil {
		t.Fatalf("ArchiveChat() error = %v", err)
	}
	e.row(t, "ar1", e.child.ID, "done", "", "user", extra.ID, time.Now())
	e.reg.evaluateChildNotifications(e.child.ID)
	if !e.notified(t, "ar1") {
		t.Fatal("run not claimed")
	}
	if got := e.events(t, extra.ID); len(got) != 0 {
		t.Fatalf("items = %+v, want none", got)
	}
}

func TestParentNotify_QuiescenceBlocksWhileDescendantRunning(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	grand := e.newChild(t, e.child.ID, "Grand")
	now := time.Now()
	e.row(t, "q1", e.child.ID, "done", "", "user", e.parentChat.ID, now)
	if _, err := e.st.CreateRun(store.Run{ID: "g-run", TaskID: grand.ID, ChatID: e.chatOf(t, grand.ID), Provider: "glm", Prompt: "p", Status: "running", StartedAt: now, Purpose: "user"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	e.reg.evaluateChildNotifications(e.child.ID)
	if e.notified(t, "q1") || len(e.events(t, e.parentChat.ID)) != 0 {
		t.Fatal("notified while a descendant run is running")
	}
	finished := time.Now()
	if _, err := e.st.UpdateRunStatus("g-run", "done", &finished, "", ""); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}
	// The grandchild's own finish would walk up; do the same here.
	e.reg.evaluateChildNotifications(grand.ID)
	waitUntil(t, "item after quiet", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	if !e.notified(t, "q1") {
		t.Fatal("run not claimed once quiet")
	}
}

func TestParentNotify_QuiescenceBlocksWhileQueuedItem(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "qi1", e.child.ID, "done", "", "user", e.parentChat.ID, time.Now())
	hold := time.Now().Add(time.Hour)
	item, err := e.st.EnqueueChatPromptAt(e.chatOf(t, e.child.ID), "follow-up", "{}", store.ChatQueueSourceHuman, 0, 0, 0, "", &hold)
	if err != nil {
		t.Fatalf("EnqueueChatPromptAt() error = %v", err)
	}
	e.reg.evaluateChildNotifications(e.child.ID)
	if e.notified(t, "qi1") || len(e.events(t, e.parentChat.ID)) != 0 {
		t.Fatal("notified while the child has a queued follow-up")
	}
	if err := e.st.MarkCancelled(item.ID, store.ChatQueueCancelUser, "x"); err != nil {
		t.Fatalf("MarkCancelled() error = %v", err)
	}
	e.reg.evaluateChildNotifications(e.child.ID)
	waitUntil(t, "item after cancel", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
}

func TestParentNotify_SupervisorHearsOnceAfterLeadSettles(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t) // e.task = Supervisor, its chat has a run
	lead := e.child
	leadChat := e.chatOf(t, lead.ID)
	// The Lead's chat needs a prior run to copy settings from.
	if id, err := e.reg.Start(context.Background(), e.wm, e.runner, lead.ID, 0, taskrunner.ProviderGLM, "init", taskrunner.PermissionSettings{}, ""); err != nil {
		t.Fatalf("Start(lead init) error = %v", err)
	} else {
		e.waitDone(t, id)
	}
	peers := []store.Task{e.newChild(t, lead.ID, "Peer A"), e.newChild(t, lead.ID, "Peer B")}

	// The Lead is working (L1, a reply-to-Supervisor run) while the Peers finish.
	writeScenario(t, lead, "hold")
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, lead.ID, 0, taskrunner.ProviderGLM, "lead work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
	if err != nil || out.Queued {
		t.Fatalf("StartWhenBusy(lead) = %+v, %v", out, err)
	}
	for _, p := range peers {
		o, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, p.ID, 0, taskrunner.ProviderGLM, "peer work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, lead.ID, leadChat)
		if err != nil || o.Queued {
			t.Fatalf("StartWhenBusy(peer) = %+v, %v", o, err)
		}
		waitForStatus(t, e.reg, o.RunID, StatusDone, 10*time.Second)
		e.reg.evaluateChildNotifications(p.ID)
	}
	waitUntil(t, "peer events on the Lead chat", func() bool {
		var n int
		for _, it := range e.events(t, leadChat) {
			n += strings.Count(it.Prompt, "<child-response")
		}
		return n == 2
	})
	if got := e.events(t, e.parentChat.ID); len(got) != 0 {
		t.Fatalf("Supervisor heard early: %+v", got)
	}

	release(t, lead) // L1 ends -> the Lead is woken by the delivered item -> settles.
	waitForStatus(t, e.reg, out.RunID, StatusDone, 10*time.Second)
	waitUntil(t, "Supervisor event", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	e.reg.evaluateChildNotifications(lead.ID)
	got := e.events(t, e.parentChat.ID)
	// The subject is the Lead's most recent finished run (its settling turn,
	// the delivered child event), not L1: the Supervisor hears the Lead's
	// final word once.
	if len(got) != 1 || !strings.Contains(got[0].Prompt, fmt.Sprintf("task #%d ", lead.ID)) || strings.Contains(got[0].Prompt, "Peer") {
		t.Fatalf("Supervisor items = %+v, want exactly one about the Lead task", got)
	}
	if !e.notified(t, out.RunID) {
		t.Fatal("L1 not claimed")
	}
}

func TestParentNotify_CoalescesThreeChildrenIntoOneItem(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	kids := []store.Task{e.child, e.sibling, e.newChild(t, e.task.ID, "Third")}
	for _, k := range kids {
		writeScenario(t, k, "hold")
	}
	var runs []string
	for _, k := range kids {
		out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, k.ID, 0, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
		if err != nil || out.Queued {
			t.Fatalf("StartWhenBusy() = %+v, %v", out, err)
		}
		runs = append(runs, out.RunID)
	}
	for _, id := range runs {
		waitUntil(t, "run history", func() bool { h, _, _ := e.reg.History(id); return len(h) > 0 })
	}
	e.reg.SetNotifyDebounce(300 * time.Millisecond)
	for _, k := range kids {
		release(t, k)
	}
	for _, id := range runs {
		waitForStatus(t, e.reg, id, StatusDone, 10*time.Second)
	}
	for _, k := range kids {
		e.reg.evaluateChildNotifications(k.ID)
	}
	items := e.events(t, e.parentChat.ID)
	if len(items) != 1 || items[0].Status != store.ChatQueueStatusQueued {
		t.Fatalf("items = %+v, want one still-queued item", items)
	}
	if n := strings.Count(items[0].Prompt, "<child-response"); n != 3 {
		t.Fatalf("response blocks = %d, want 3 in %q", n, items[0].Prompt)
	}
	if items[0].NotBefore == nil {
		t.Fatal("coalesced item has no hold")
	}
	notBefore := *items[0].NotBefore

	delivered := waitDelivered(t, e, items[0].ID)
	run, err := e.st.GetRun(*delivered.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.StartedAt.Before(notBefore) {
		t.Fatalf("delivered at %v, before not_before %v", run.StartedAt, notBefore)
	}
	if got := e.events(t, e.parentChat.ID); len(got) != 1 {
		t.Fatalf("items after delivery = %d, want 1", len(got))
	}
}

func waitDelivered(t *testing.T, e *notifyEnv, itemID int64) store.ChatQueueItem {
	t.Helper()
	var it store.ChatQueueItem
	waitUntil(t, "item delivered", func() bool {
		var err error
		it, err = e.st.GetChatQueueItem(itemID)
		return err == nil && it.Status == store.ChatQueueStatusDelivered && it.RunID != nil
	})
	return it
}

func TestParentNotify_LateChildMakesNewItem(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.childRun(t, e.child.ID, StatusDone)
	waitUntil(t, "first item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	first := waitDelivered(t, e, e.events(t, e.parentChat.ID)[0].ID)
	waitForStatus(t, e.reg, *first.RunID, StatusDone, 10*time.Second)

	e.childRun(t, e.sibling.ID, StatusDone)
	waitUntil(t, "second item", func() bool { return len(e.events(t, e.parentChat.ID)) == 2 })
	items := e.events(t, e.parentChat.ID)
	if items[0].ID == items[1].ID || strings.Contains(items[1].Prompt, fmt.Sprintf("task #%d ", e.child.ID)) {
		t.Fatalf("late child merged into the delivered item: %+v", items)
	}
}

func TestParentNotify_DeliveredAtTurnBoundaryNeverInterrupts(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	writeScenario(t, e.task, "hold")
	parentRun := e.startRun(t, "parent busy")
	waitUntil(t, "parent hold", func() bool { h, _, _ := e.reg.History(parentRun); return len(h) > 0 })

	e.childRun(t, e.child.ID, StatusDone)
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	itemID := e.events(t, e.parentChat.ID)[0].ID
	time.Sleep(50 * time.Millisecond)
	it, _ := e.st.GetChatQueueItem(itemID)
	_, st, _ := e.reg.History(parentRun)
	if it.Status != store.ChatQueueStatusQueued || st.Status != StatusRunning {
		t.Fatalf("item %q, parent run %q: want queued while the parent run is running", it.Status, st.Status)
	}

	release(t, e.task)
	waitForStatus(t, e.reg, parentRun, StatusDone, 10*time.Second) // done, not stopped
	waitDelivered(t, e, itemID)
}

func TestParentNotify_ChildEventRunHasNoReplyChat(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.childRun(t, e.child.ID, StatusDone)
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	it := waitDelivered(t, e, e.events(t, e.parentChat.ID)[0].ID)
	waitForStatus(t, e.reg, *it.RunID, StatusDone, 10*time.Second)
	run, err := e.st.GetRun(*it.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Origin != store.ChatQueueSourceChildEvent || run.ReplyChatID != 0 {
		t.Fatalf("delivered run origin/reply = %q/%d, want child_event/0", run.Origin, run.ReplyChatID)
	}
	e.reg.evaluateChildNotifications(e.task.ID)
	if n := len(e.events(t, e.parentChat.ID)); n != 1 {
		t.Fatalf("items = %d after the child-event run finished, want 1 (no loop)", n)
	}
}

func TestParentNotify_FinishTwiceOneItem(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.childRun(t, e.child.ID, StatusDone)
	e.reg.evaluateChildNotifications(e.child.ID)
	e.reg.evaluateChildNotifications(e.child.ID)
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) >= 1 })
	if n := len(e.events(t, e.parentChat.ID)); n != 1 {
		t.Fatalf("items = %d, want 1", n)
	}
}

func TestParentNotify_StartupScanNotifiesInterruptedRunOnce(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "int1", e.child.ID, "interrupted", StopReasonDaemonRestart, "user", e.parentChat.ID, time.Now())
	e.reg.NotifyPendingChildren()
	e.reg.NotifyPendingChildren()
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) >= 1 })
	if n := len(e.events(t, e.parentChat.ID)); n != 1 {
		t.Fatalf("items = %d, want 1", n)
	}
	if !e.notified(t, "int1") {
		t.Fatal("run not claimed")
	}
}

func TestParentNotify_StartupScanIgnoresRunsOlderThan24h(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.row(t, "old1", e.child.ID, "interrupted", StopReasonDaemonRestart, "user", e.parentChat.ID, time.Now().Add(-25*time.Hour))
	e.reg.NotifyPendingChildren()
	if e.notified(t, "old1") || len(e.events(t, e.parentChat.ID)) != 0 {
		t.Fatal("startup scan picked up a run older than 24h")
	}
}

func TestParentNotify_QueueFullLeavesRunUnclaimed(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	hold := time.Now().Add(time.Hour)
	for i := 0; i < 20; i++ {
		if _, err := e.st.EnqueueChatPromptAt(e.parentChat.ID, "x", "{}", store.ChatQueueSourceHuman, 0, 0, 0, "", &hold); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	e.row(t, "full1", e.child.ID, "done", "", "user", e.parentChat.ID, time.Now())
	e.reg.evaluateChildNotifications(e.child.ID)
	if e.notified(t, "full1") || len(e.events(t, e.parentChat.ID)) != 0 {
		t.Fatal("queue-full notification claimed the run or enqueued")
	}
}

func TestParentNotify_ConcurrentFinishesAdmitExactlyOnePerRun(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	kids := []store.Task{e.child, e.sibling, e.newChild(t, e.task.ID, "Third")}
	ids := []string{"cc1", "cc2", "cc3"}
	for i, k := range kids {
		e.row(t, ids[i], k.ID, "done", "", "user", e.parentChat.ID, time.Now())
	}
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, k := range kids {
				e.reg.evaluateChildNotifications(k.ID)
			}
		}()
	}
	wg.Wait()
	var all string
	waitUntil(t, "all runs announced", func() bool {
		all = ""
		for _, it := range e.events(t, e.parentChat.ID) {
			all += it.Prompt + "\n"
		}
		return strings.Count(all, "run cc") == 3
	})
	for _, id := range ids {
		if n := strings.Count(all, "run "+id+":"); n != 1 {
			t.Fatalf("run %s announced %d times, want 1", id, n)
		}
		if !e.notified(t, id) {
			t.Fatalf("run %s not claimed", id)
		}
	}
}

// TestParentNotify_WokenRunKeepsTheParentsProfile: the daemon-authored item
// copies the parent chat's last run settings including its profile, so the
// woken Lead run still gets its seat's instructions and orchestration MCP
// entry (the first in-smind dogfood run lost both on the wake-up).
func TestParentNotify_WokenRunKeepsTheParentsProfile(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	if _, err := e.st.CreateRun(store.Run{
		ID: "parent-seat-run", TaskID: e.task.ID, ChatID: e.parentChat.ID, Provider: "glm", Prompt: "lead", Status: "done",
		StartedAt: time.Now().Add(time.Hour), ProfileID: 42,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	e.childRun(t, e.child.ID, StatusDone)
	waitUntil(t, "item", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	var cfg chatQueueRunConfig
	if err := json.Unmarshal([]byte(e.events(t, e.parentChat.ID)[0].RunConfig), &cfg); err != nil {
		t.Fatalf("decode run config: %v", err)
	}
	if cfg.ProfileID != 42 {
		t.Fatalf("run config profile = %d, want 42 (the parent's seat)", cfg.ProfileID)
	}
}
