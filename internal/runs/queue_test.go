package runs

import (
	"context"
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

// queuedTestEnv is one chat's queue test setup: a registry with the
// starter wired (so delivery can start runs itself), a fake-agent runner,
// and the task/chat to prompt.
type queuedTestEnv struct {
	wm     *workspace.Manager
	st     *store.Store
	task   store.Task
	chatID int64
	runner *taskrunner.Runner
	reg    *Registry
}

func newQueuedTestEnv(t *testing.T, scenario string) *queuedTestEnv {
	t.Helper()
	return newQueuedTestEnvArgs(t, scenario, nil)
}

// newQueuedTestEnvArgs is newQueuedTestEnv with extra fake-agent argv
// (e.g. "resume" so the agent advertises session resume, for O1 checks).
func newQueuedTestEnvArgs(t *testing.T, scenario string, agentArgs []string) *queuedTestEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	// Like serve.go's runner, the session store is wired so a chat's agent
	// session persists -- the queue's interrupt/restart delivery resumes it
	// (O1).
	runner := taskrunner.New(wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, append([]string{fakeACPAgentPath}, agentArgs...)))
	if agentArgs == nil {
		runner = taskrunner.New(wm,
			taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(st)),
			taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	}
	reg := newTestRegistry(t, st)
	reg.SetStarter(wm, runner)
	return &queuedTestEnv{wm: wm, st: st, task: task, chatID: 0, runner: runner, reg: reg}
}

// startRun is a plain Start on the env's task/default chat.
func (e *queuedTestEnv) startRun(t *testing.T, prompt string) string {
	t.Helper()
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, prompt, taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start(%q) error = %v", prompt, err)
	}
	return runID
}

// waitForQueuedStatus polls the store until item id reaches status want.
func (e *queuedTestEnv) waitForQueuedStatus(t *testing.T, itemID int64, want string) store.ChatQueueItem {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		item, err := e.st.GetChatQueueItem(itemID)
		if err != nil {
			t.Fatalf("GetChatQueueItem(%d) error = %v", itemID, err)
		}
		if item.Status == want {
			return item
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue item %d still %q, want %q", itemID, item.Status, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// chatIDOf resolves a run's concrete chat id (Start resolves 0 to the
// default chat).
func (e *queuedTestEnv) chatIDOf(t *testing.T, runID string) int64 {
	t.Helper()
	_, status, err := e.reg.History(runID)
	if err != nil {
		t.Fatalf("History(%q) error = %v", runID, err)
	}
	return status.ChatID
}

// TestRunStart_WhenBusyReject_Unchanged pins AC8/AC2's reject half: a busy
// chat with no whenBusy gives the exact legacy error (same text as
// ChatBusyError), and an idle chat with each of the three modes starts at
// once.
func TestRunStart_WhenBusyReject_Unchanged(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runID)
	waitForHistoryLen(t, e.reg, runID, 1, 5*time.Second)
	t.Cleanup(func() { _ = e.reg.Stop(runID) })

	// Busy + no whenBusy: byte-identical legacy error.
	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", "", store.ChatQueueSourceHuman, 0, 0)
	var busy *ChatBusyError
	if err == nil || !strings.Contains(err.Error(), "already has a running run") {
		t.Fatalf("StartWhenBusy() error = %v, want the legacy busy error", err)
	}
	if !asErr(err, &busy) || busy.RunID != runID {
		t.Fatalf("StartWhenBusy() error = %T(%v), want *ChatBusyError for run %s", err, err, runID)
	}

	// Busy + explicit reject: same error text.
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
	if err == nil || err.Error() != (&ChatBusyError{ChatID: chatID, RunID: runID}).Error() {
		t.Fatalf("reject error = %v, want byte-identical legacy text", err)
	}

	// Idle: every mode starts immediately and returns {runId}.
	_ = e.reg.Stop(runID)
	waitForStatus(t, e.reg, runID, StatusStopped, 5*time.Second)
	other := newQueuedTestEnv(t, "")
	for _, mode := range []WhenBusy{"", WhenBusyReject, WhenBusyQueue, WhenBusyInterrupt} {
		out, err := other.reg.StartWhenBusy(context.Background(), other.wm, other.runner, other.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", mode, store.ChatQueueSourceHuman, 0, 0)
		if err != nil {
			t.Fatalf("StartWhenBusy(%q) on an idle chat error = %v", mode, err)
		}
		if out.Queued || out.RunID == "" {
			t.Fatalf("StartWhenBusy(%q) on an idle chat = %+v, want an immediate {runId}", mode, out)
		}
		waitForStatus(t, other.reg, out.RunID, StatusDone, 5*time.Second)
	}
}

func asErr(err error, target **ChatBusyError) bool {
	if e, ok := err.(*ChatBusyError); ok {
		*target = e
		return true
	}
	return false
}

// TestRunStart_WhenBusyQueue_DeliversOnFinish is AC2/AC3's core scenario:
// run A running, queue B, get {queued, queueItemId}; A finishes, B starts
// automatically on the same chat; the item is delivered with B's run id.
func TestRunStart_WhenBusyQueue_DeliversOnFinish(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("StartWhenBusy(queue) error = %v", err)
	}
	if !out.Queued || out.QueueItemID == 0 || out.RunID != "" {
		t.Fatalf("StartWhenBusy(queue) = %+v, want {queued, queueItemId}", out)
	}

	waitForStatus(t, e.reg, runA, StatusDone, 5*time.Second)

	// B starts automatically on the same chat: a second run appears for
	// chatID after A's terminal state.
	deadline := time.Now().Add(5 * time.Second)
	var runB string
	for {
		runs := e.reg.List(chatID)
		if len(runs) >= 2 {
			runB = runs[0].ID // List is most-recent-first.
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no second run started for chat %d after A finished: %+v", chatID, runs)
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForStatus(t, e.reg, runB, StatusDone, 5*time.Second)

	item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	if item.RunID == nil || *item.RunID != runB {
		t.Fatalf("delivered item runId = %v, want B's run id %s", item.RunID, runB)
	}
}

// TestRunStart_WhenBusyInterrupt_StopsAndDeliversFirst pins AC2's
// interrupt half: A running, q1 queued, then interrupt i1. A is stopped,
// i1 is delivered before q1, and i1 resumes A's session (O1).
func TestRunStart_WhenBusyInterrupt_StopsAndDeliversFirst(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnvArgs(t, "hang", []string{"resume"})
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)
	waitForHistoryLen(t, e.reg, runA, 1, 5*time.Second) // A is genuinely under way (session established)
	t.Cleanup(func() { _ = e.reg.Stop(runA) })

	q1, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "q1", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("queue q1 error = %v", err)
	}
	i1, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "i1", taskrunner.PermissionSettings{}, "", WhenBusyInterrupt, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("interrupt i1 error = %v", err)
	}
	if !i1.Queued || !q1.Queued {
		t.Fatalf("outcomes = %+v, %+v, want both queued", q1, i1)
	}

	// The hang scenario kept A occupiable; the delivered items should run
	// to completion instead (the scenario file is read at prompt time, so
	// clearing it now only affects runs that haven't started yet).
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}

	// A is stopped by the interrupt.
	waitForStatus(t, e.reg, runA, StatusStopped, 5*time.Second)

	// i1 delivers first (priority 1), then q1.
	itemI1 := e.waitForQueuedStatus(t, i1.QueueItemID, store.ChatQueueStatusDelivered)
	itemQ1 := e.waitForQueuedStatus(t, q1.QueueItemID, store.ChatQueueStatusDelivered)
	runs := e.reg.List(chatID)
	if len(runs) < 3 {
		t.Fatalf("runs for chat = %d, want 3 (A, i1, q1)", len(runs))
	}
	// List is most-recent-first: runs[2]=A, runs[1]=first delivered, runs[0]=second.
	if runs[1].ID != *itemI1.RunID {
		t.Fatalf("first delivered run = %s, want i1's run %s", runs[1].ID, *itemI1.RunID)
	}
	if runs[0].ID != *itemQ1.RunID {
		t.Fatalf("second delivered run = %s, want q1's run %s", runs[0].ID, *itemQ1.RunID)
	}
	waitForStatus(t, e.reg, *itemI1.RunID, StatusDone, 5*time.Second)
	waitForStatus(t, e.reg, *itemQ1.RunID, StatusDone, 5*time.Second)

	// i1 resumes A's session (O1): both runs share the chat's persisted
	// session handle, so the delivered prompt runs on the resumed session.
	chat, err := e.wm.GetChat(chatID)
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if chat.AgentSession == nil {
		t.Fatal("chat has no persisted agent session, want one from A (O1 session resume)")
	}
}

// TestChatQueue_ValidationAtEnqueue pins ADR-0021 §6 / ADR-0019 decision 6
// at enqueue time: an orchestrator source may not queue an auto-approving
// permission configuration, and an unknown mode is rejected -- both before
// anything is stored.
func TestChatQueue_ValidationAtEnqueue(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "hi")
	chatID := e.chatIDOf(t, runID)
	t.Cleanup(func() { _ = e.reg.Stop(runID) })

	// Orchestrator queueing an auto-approving mode (glm's
	// bypass_permissions reads as bypass-like even undiscovered).
	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{Mode: "bypass_permissions"}, "", WhenBusyQueue, store.ChatQueueSourceOrchestrator, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "auto-approving") {
		t.Fatalf("orchestrator bypass error = %v, want the ADR-0019 decision-6 rejection", err)
	}
	// Orchestrator autoAccept likewise.
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{AutoAccept: true}, "", WhenBusyQueue, store.ChatQueueSourceOrchestrator, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "auto-approving") {
		t.Fatalf("orchestrator autoAccept error = %v, want the ADR-0019 decision-6 rejection", err)
	}
	// The same configuration is fine from a human source.
	if _, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "human bypass", taskrunner.PermissionSettings{Mode: "bypass_permissions"}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0); err != nil {
		t.Fatalf("human bypass error = %v, want accepted", err)
	}
	// An unknown mode is rejected regardless of source. GLM (ACP,
	// undiscovered) accepts any id by design, so this uses claude-native,
	// whose static catalog rejects "not-a-mode" -- on its own chat, since
	// the test chat is already bound to glm.
	claudeChat, err := e.wm.CreateChat(e.task.ID, "claude")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := e.wm.BindChatProvider(claudeChat.ID, string(taskrunner.ProviderClaudeNative)); err != nil {
		t.Fatalf("BindChatProvider(claude) error = %v", err)
	}
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, claudeChat.ID, taskrunner.ProviderClaudeNative, "x", taskrunner.PermissionSettings{Mode: "not-a-mode"}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "invalid permission mode") {
		t.Fatalf("unknown mode error = %v, want invalid permission mode", err)
	}

	// Nothing was stored by the rejected calls.
	items, err := e.st.ListChatQueue(chatID)
	if err != nil {
		t.Fatalf("ListChatQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Prompt != "human bypass" {
		t.Fatalf("queue = %+v, want only the accepted human item", items)
	}
}

// TestChatQueue_BadItemCancelledNotLooped pins ADR-0021 §3's failure rule:
// an item whose provider no longer matches the chat is cancelled with a
// reason, the next item delivers, and the delivery-attempt counter proves
// delivery never tight-loops on the bad item.
func TestChatQueue_BadItemCancelledNotLooped(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	// Bind the chat to glm, then queue a codex-native item (mismatch) plus
	// a good glm item behind it.
	if _, err := e.wm.BindChatProvider(chat.ID, string(taskrunner.ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}
	bad, err := e.st.EnqueueChatPrompt(chat.ID, "bad", `{"provider":"codex-native"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatalf("enqueue bad error = %v", err)
	}
	good, err := e.st.EnqueueChatPrompt(chat.ID, "good", `{"provider":"glm"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatalf("enqueue good error = %v", err)
	}

	e.reg.DeliverQueued()

	badItem := e.waitForQueuedStatus(t, bad.ID, store.ChatQueueStatusCancelled)
	if badItem.CancelReason == "" {
		t.Fatal("cancelled item has no reason")
	}
	goodItem := e.waitForQueuedStatus(t, good.ID, store.ChatQueueStatusDelivered)
	if goodItem.RunID == nil {
		t.Fatal("good item not delivered with a run id")
	}

	// No tight loop: the bad item was attempted at most a couple of times.
	e.reg.mu.Lock()
	attempts := e.reg.deliveryAttempts[bad.ID]
	e.reg.mu.Unlock()
	if attempts > 2 {
		t.Fatalf("bad item delivery attempts = %d, want <= 2 (no tight loop)", attempts)
	}
}

// TestChatQueue_AgentProvenanceHeader pins ADR-0021 §4: an agent-source
// item's delivered prompt begins with "[message from task #T, chat #C]"
// then a newline; human items are delivered verbatim.
func TestChatQueue_AgentProvenanceHeader(t *testing.T) {
	t.Parallel()

	// Unit-level: deliverPrompt's formatting.
	fromTask, fromChat := int64(7), int64(9)
	agent := store.ChatQueueItem{Prompt: "escalating", Source: store.ChatQueueSourceAgent, FromTaskID: &fromTask, FromChatID: &fromChat}
	if got := deliverPrompt(agent); got != "[message from task #7, chat #9]\nescalating" {
		t.Fatalf("agent deliverPrompt = %q", got)
	}
	human := store.ChatQueueItem{Prompt: "verbatim", Source: store.ChatQueueSourceHuman}
	if got := deliverPrompt(human); got != "verbatim" {
		t.Fatalf("human deliverPrompt = %q, want verbatim", got)
	}
	orchestrator := store.ChatQueueItem{Prompt: "verbatim", Source: store.ChatQueueSourceOrchestrator}
	if got := deliverPrompt(orchestrator); got != "verbatim" {
		t.Fatalf("orchestrator deliverPrompt = %q, want verbatim", got)
	}

	// End to end: the delivered run's persisted prompt carries the header.
	e := newQueuedTestEnv(t, "")
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := e.wm.BindChatProvider(chat.ID, string(taskrunner.ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}
	item, err := e.st.EnqueueChatPrompt(chat.ID, "peer escalation", `{"provider":"glm"}`, store.ChatQueueSourceAgent, 0, 7, 9)
	if err != nil {
		t.Fatalf("enqueue error = %v", err)
	}
	e.reg.DeliverQueued()
	delivered := e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusDelivered)
	row, err := e.st.GetRun(*delivered.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if !strings.HasPrefix(row.Prompt, "[message from task #7, chat #9]\n") {
		t.Fatalf("delivered prompt = %q, want the provenance header first", row.Prompt)
	}
	if !strings.HasSuffix(row.Prompt, "peer escalation") {
		t.Fatalf("delivered prompt = %q, want the original prompt after the header", row.Prompt)
	}
}

// TestChatQueue_RestartKeepsDelivering pins ADR-0021 §5 as amended: after
// runs.New reconciles a stale running row to interrupted and the starter
// is wired, the queued item is delivered -- the interrupted run is not
// retried.
func TestChatQueue_RestartKeepsDelivering(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runID)
	waitForHistoryLen(t, e.reg, runID, 1, 5*time.Second)
	t.Cleanup(func() { _ = e.reg.Stop(runID) })

	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "after restart", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("queue error = %v", err)
	}

	// Simulate a restart: a fresh Registry (New reconciles the running row
	// to interrupted) with the starter wired after construction, then
	// DeliverQueued once the dependencies exist.
	reg2, err := New(e.st)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reg2.SetStarter(e.wm, e.runner)
	// The hang scenario that kept run A "running" would also hang the
	// delivered run -- clear it so delivery can complete.
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}
	if _, status, _ := reg2.History(runID); status.Status != StatusInterrupted {
		t.Fatalf("stale run status = %q, want interrupted", status.Status)
	}
	reg2.DeliverQueued()

	items, err := e.st.ListChatQueue(chatID)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListChatQueue() = %+v, %v, want one item", items, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		items, _ := e.st.ListChatQueue(chatID)
		if items[0].Status == store.ChatQueueStatusDelivered && items[0].RunID != nil {
			waitForStatus(t, reg2, *items[0].RunID, StatusDone, 5*time.Second)
			// The interrupted run was not retried: no second glm run row
			// for this chat beyond the delivered one and the original.
			var runRows int
			rows, err := e.st.ListRecentRuns(10)
			if err != nil {
				t.Fatalf("ListRecentRuns() error = %v", err)
			}
			for _, r := range rows {
				if r.ChatID == chatID {
					runRows++
				}
			}
			if runRows != 2 {
				t.Fatalf("runs for chat after restart = %d, want 2 (original interrupted + delivered)", runRows)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued item not delivered after restart: %+v", items[0])
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// countingNotifier records every queue snapshot notification, for the
// event assertions that live in the wsapi layer's own tests.
type countingNotifier struct {
	mu       sync.Mutex
	chatIDs  []int64
	snapshot map[int64][]store.ChatQueueItem
}

func (c *countingNotifier) NotifyRunStatus(s RunStatus) {}
func (c *countingNotifier) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
}
func (c *countingNotifier) NotifyChatQueueUpdated(chatID int64, items []store.ChatQueueItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatIDs = append(c.chatIDs, chatID)
	c.snapshot = make(map[int64][]store.ChatQueueItem)
	c.snapshot[chatID] = items
}

// TestChatQueue_NotifierGetsSnapshot is the runs-layer half of the
// chat.queueUpdated event: enqueue, deliver, and auto-cancel each fire the
// notifier with the chat's full queue snapshot.
func TestChatQueue_NotifierGetsSnapshot(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := e.wm.BindChatProvider(chat.ID, string(taskrunner.ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}
	n := &countingNotifier{}
	e.reg.SetNotifier(n)

	item, err := e.st.EnqueueChatPrompt(chat.ID, "p", `{"provider":"glm"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatalf("enqueue error = %v", err)
	}
	e.reg.notifyQueueUpdated(chat.ID)

	bad, _ := e.st.EnqueueChatPrompt(chat.ID, "bad", `{"provider":"codex-native"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	_ = bad
	e.reg.deliverOne(chat.ID)
	e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusDelivered)

	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.chatIDs) < 2 {
		t.Fatalf("notifications = %d, want at least one per state change", len(n.chatIDs))
	}
	last := n.snapshot[chat.ID]
	if len(last) != 2 || last[0].Status != store.ChatQueueStatusDelivered {
		t.Fatalf("final snapshot = %+v, want the delivered item and the cancelled one", last)
	}
}

// TestChatQueue_ConcurrentDeliveryNoDuplicate pins deliverMu's contract:
// several concurrent deliverOne calls for the same chat with one queued
// item start exactly one run; the item is delivered once, never twice.
func TestChatQueue_ConcurrentDeliveryNoDuplicate(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := e.wm.BindChatProvider(chat.ID, string(taskrunner.ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}
	item, err := e.st.EnqueueChatPrompt(chat.ID, "only one", `{"provider":"glm"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatalf("enqueue error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.reg.deliverOne(chat.ID)
		}()
	}
	wg.Wait()

	delivered := e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusDelivered)
	waitForStatus(t, e.reg, *delivered.RunID, StatusDone, 5*time.Second)

	// Exactly one run started for this chat.
	runs := e.reg.List(chat.ID)
	if len(runs) != 1 {
		t.Fatalf("runs for chat after concurrent delivery = %d, want exactly 1: %+v", len(runs), runs)
	}
	if runs[0].ID != *delivered.RunID {
		t.Fatalf("the one run = %s, want the delivered item's run %s", runs[0].ID, *delivered.RunID)
	}
}

// TestChatQueue_NoDeliveryDuringCloseAll pins the shutdown rule: CloseAll
// sets the closing latch before stopping runs, so no run's finish delivers
// the chat's next queued item (a run started there would miss CloseAll's
// own stop pass and its item would be marked delivered, losing it for the
// restart's redelivery, ADR-0021 §5). A fresh Registry over the same store
// delivers the item instead.
func TestChatQueue_NoDeliveryDuringCloseAll(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runID)
	waitForHistoryLen(t, e.reg, runID, 1, 5*time.Second)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "next", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("queue error = %v", err)
	}

	e.reg.CloseAll()

	// No new run started and the item stayed queued.
	runs := e.reg.List(chatID)
	if len(runs) != 1 {
		t.Fatalf("runs for chat after CloseAll = %d, want only the original 1: %+v", len(runs), runs)
	}
	item, err := e.st.GetChatQueueItem(out.QueueItemID)
	if err != nil {
		t.Fatalf("GetChatQueueItem() error = %v", err)
	}
	if item.Status != store.ChatQueueStatusQueued {
		t.Fatalf("item status after CloseAll = %q, want still queued", item.Status)
	}

	// A restart delivers it (AC5 loop): clear the hang scenario first so
	// the delivered run can finish.
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}
	reg2 := newTestRegistry(t, e.st)
	reg2.SetStarter(e.wm, e.runner)
	reg2.DeliverQueued()
	delivered := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	waitForStatus(t, reg2, *delivered.RunID, StatusDone, 5*time.Second)
}
