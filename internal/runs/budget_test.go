package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func budgetPtr(v float64) *float64 { return &v }

// setBudget sets (or clears, v == nil) the root's budget.
func (e *treeGuardEnv) setBudget(t *testing.T, v *float64) {
	t.Helper()
	if err := e.st.SetTaskBudget(e.root.ID, v); err != nil {
		t.Fatalf("SetTaskBudget() error = %v", err)
	}
}

// spend records one finished user run on taskID reporting cost.
func (e *treeGuardEnv) spend(t *testing.T, taskID int64, runID string, cost float64) {
	t.Helper()
	if _, err := e.st.CreateRun(store.Run{ID: runID, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := e.st.UpsertRunUsage(store.RunUsage{RunID: runID, TaskID: taskID, Provider: "glm", CostUSD: &cost, Source: "test"}); err != nil {
		t.Fatalf("UpsertRunUsage() error = %v", err)
	}
}

// send launches a run on taskID's fresh chat through StartWhenBusy with the
// given source.
func (e *treeGuardEnv) send(t *testing.T, taskID int64, source string) (StartOutcome, error) {
	t.Helper()
	chat, err := e.wm.CreateChat(taskID, "chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	return e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, taskID, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, WhenBusyReject, source, 0, 0)
}

func asBudgetExceeded(err error) (*BudgetExceededError, bool) {
	var be *BudgetExceededError
	if errors.As(err, &be) {
		return be, true
	}
	return nil, false
}

func TestBudgetAdmission_AgentRefusedAtAndOverCap(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	e.setBudget(t, budgetPtr(5))
	e.spend(t, e.root.ID, "r1", 5.02)

	_, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent)
	be, ok := asBudgetExceeded(err)
	if !ok {
		t.Fatalf("agent StartWhenBusy over the cap error = %v, want *BudgetExceededError", err)
	}
	if be.RootTaskID != e.root.ID || be.SpentUSD != 5.02 || be.BudgetUSD != 5 {
		t.Fatalf("error = %+v, want root %d spent 5.02 of 5", be, e.root.ID)
	}
	// The direct Start path refuses too, with the agent origin recorded.
	_, err = e.reg.Start(context.Background(), e.wm, e.runner, e.tasks[0].ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WithOrigin(store.ChatQueueSourceAgent))
	if _, ok := asBudgetExceeded(err); !ok {
		t.Fatalf("agent Start over the cap error = %v, want *BudgetExceededError", err)
	}
}

func TestBudgetAdmission_AgentAllowedUnderCap(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	e.setBudget(t, budgetPtr(5))
	e.spend(t, e.root.ID, "r1", 4.99)

	if _, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent); err != nil {
		t.Fatalf("agent StartWhenBusy under the cap error = %v, want nil", err)
	}
}

func TestBudgetAdmission_ExactlyAtCapBoundary(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	e.setBudget(t, budgetPtr(5))
	e.spend(t, e.root.ID, "r1", 5)

	_, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent)
	if _, ok := asBudgetExceeded(err); !ok {
		t.Fatalf("agent StartWhenBusy exactly at the cap error = %v, want *BudgetExceededError (a reached budget refuses)", err)
	}
}

func TestBudgetAdmission_HumanNeverRefused(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 3, false)
	e.setBudget(t, budgetPtr(1))
	e.spend(t, e.root.ID, "r1", 50)

	for i, source := range []string{store.ChatQueueSourceHuman, store.ChatQueueSourceOrchestrator, store.ChatQueueSourceChildEvent} {
		if _, err := e.send(t, e.tasks[i].ID, source); err != nil {
			t.Fatalf("%s StartWhenBusy over the cap error = %v, want nil (only agent launches are refused)", source, err)
		}
	}
	// A direct human Start (no origin) is not refused either.
	e.startOnNewChat(t, e.root.ID)
}

func TestBudgetAdmission_ChildOfChildUsesRootBudget(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, true)
	grand := e.tasks[1]
	e.setBudget(t, budgetPtr(2))
	// A budget on a non-root row is ignored: only the root's counts.
	if err := e.st.SetTaskBudget(e.tasks[0].ID, budgetPtr(1000)); err != nil {
		t.Fatal(err)
	}
	e.spend(t, e.tasks[0].ID, "r1", 2.5) // spent by the middle task

	_, err := e.send(t, grand.ID, store.ChatQueueSourceAgent)
	be, ok := asBudgetExceeded(err)
	if !ok {
		t.Fatalf("agent send on a grandchild error = %v, want *BudgetExceededError from the root's budget", err)
	}
	if be.RootTaskID != e.root.ID {
		t.Fatalf("RootTaskID = %d, want the root %d", be.RootTaskID, e.root.ID)
	}
}

func TestBudgetAdmission_RaiseAndClearReallow(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 2, false)
	e.setBudget(t, budgetPtr(5))
	e.spend(t, e.root.ID, "r1", 6)

	if _, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent); err == nil {
		t.Fatal("agent send over the cap: want refusal")
	}
	e.setBudget(t, budgetPtr(10))
	if _, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent); err != nil {
		t.Fatalf("agent send after raising the cap error = %v, want nil", err)
	}
	e.spend(t, e.root.ID, "r2", 10) // 16 of 10
	if _, err := e.send(t, e.tasks[1].ID, store.ChatQueueSourceAgent); err == nil {
		t.Fatal("agent send over the raised cap: want refusal")
	}
	e.setBudget(t, nil)
	if _, err := e.send(t, e.tasks[1].ID, store.ChatQueueSourceAgent); err != nil {
		t.Fatalf("agent send after clearing the budget error = %v, want nil", err)
	}
}

func TestBudgetAdmission_QueuedAgentRefusedAtEnqueueAndDelivery(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 1, false)
	chatTask := e.tasks[0].ID
	chat, err := e.wm.CreateChat(chatTask, "chat")
	if err != nil {
		t.Fatal(err)
	}
	// Under the cap: a busy chat enqueues the agent item.
	e.setBudget(t, budgetPtr(5))
	busy, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, chatTask, chat.ID, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, chatTask, chat.ID, taskrunner.ProviderGLM, "later", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, WhenBusyQueue, store.ChatQueueSourceAgent, 0, 0)
	if err != nil || !queued.Queued {
		t.Fatalf("agent enqueue under the cap = %+v, %v, want queued", queued, err)
	}
	// The cap is reached while it waits: a new agent enqueue is refused ...
	e.spend(t, e.root.ID, "r1", 9)
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, chatTask, chat.ID, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, WhenBusyQueue, store.ChatQueueSourceAgent, 0, 0)
	if _, ok := asBudgetExceeded(err); !ok {
		t.Fatalf("agent enqueue over the cap error = %v, want *BudgetExceededError", err)
	}
	// ... and the waiting item is cancelled at delivery without wedging the
	// queue: a human item behind it is delivered.
	human, err := e.st.EnqueueChatPrompt(chat.ID, "human", `{"provider":"glm"}`, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.reg.Stop(busy.RunID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, _ := e.st.GetChatQueueItem(queued.QueueItemID)
		h, _ := e.st.GetChatQueueItem(human.ID)
		if a.Status == store.ChatQueueStatusCancelled && h.Status == store.ChatQueueStatusDelivered {
			if !strings.Contains(a.CancelReason, "reached its budget") {
				t.Fatalf("cancel reason = %q, want the budget sentence", a.CancelReason)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent item %q, human item %q; want cancelled + delivered", a.Status, h.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBudgetExceededError_TypedSentence(t *testing.T) {
	t.Parallel()
	with := (&BudgetExceededError{RootTaskID: 12, SpentUSD: 5.02, BudgetUSD: 5, UnknownRuns: 3}).Error()
	for _, want := range []string{
		"task tree #12 reached its budget",
		"$5.02 of $5.00 reported cost",
		"3 runs reported no cost and are not counted",
		"smind task budget 12 <usd>",
		"budget control in the tree header",
		"do not retry",
	} {
		if !strings.Contains(with, want) {
			t.Errorf("message %q lacks %q", with, want)
		}
	}
	without := (&BudgetExceededError{RootTaskID: 12, SpentUSD: 5, BudgetUSD: 5}).Error()
	if strings.Contains(without, "reported no cost") {
		t.Errorf("message %q mentions unknown runs although there are none", without)
	}
	one := (&BudgetExceededError{RootTaskID: 1, SpentUSD: 1, BudgetUSD: 1, UnknownRuns: 1}).Error()
	if !strings.Contains(one, "1 run reported no cost and is not counted") {
		t.Errorf("message %q does not use the singular", one)
	}
}

func TestBudgetAdmission_QueryCount(t *testing.T) {
	t.Parallel()
	e := newTreeGuardEnv(t, 3, false)
	var walks, costs atomic.Int32
	e.reg.treeRootStoreRead = func() { walks.Add(1) }
	e.reg.budgetCostRead = func() { costs.Add(1) }

	// No budget: an agent send adds no cost query.
	if _, err := e.send(t, e.tasks[0].ID, store.ChatQueueSourceAgent); err != nil {
		t.Fatal(err)
	}
	if got := costs.Load(); got != 0 {
		t.Fatalf("agent send without a budget ran %d cost queries, want 0", got)
	}

	// Budgeted tree: a human send adds nothing (only the existing root walk, no cost query)...
	e.setBudget(t, budgetPtr(100))
	walks.Store(0)
	if _, err := e.send(t, e.tasks[1].ID, store.ChatQueueSourceHuman); err != nil {
		t.Fatal(err)
	}
	if w, c := walks.Load(), costs.Load(); w != 2 || c != 0 {
		t.Fatalf("human send in a budgeted tree: %d root reads, %d cost queries, want 2 (the existing depth-1 walk), 0", w, c)
	}

	// ... and an agent send reuses the single root walk and runs exactly one
	// cost query.
	walks.Store(0)
	if _, err := e.send(t, e.tasks[2].ID, store.ChatQueueSourceAgent); err != nil {
		t.Fatal(err)
	}
	if w, c := walks.Load(), costs.Load(); w != 2 || c != 1 {
		t.Fatalf("agent send in a budgeted tree: %d root reads, %d cost queries, want 2 (the same walk, no extra GetTask), 1", w, c)
	}
}

// budgetNotifyEnv is a notifyEnv whose root (e.task) has a $5 budget already
// reached, a frozen budget clock and a long debounce so the notification
// item stays queued and countable.
func newBudgetNotifyEnv(t *testing.T) (*notifyEnv, time.Time) {
	t.Helper()
	e := newNotifyEnv(t)
	frozen := time.Now().UTC().Truncate(time.Second)
	e.reg.budgetNow = func() time.Time { return frozen }
	e.reg.SetNotifyDebounce(time.Hour)
	if err := e.st.SetTaskBudget(e.task.ID, budgetPtr(5)); err != nil {
		t.Fatal(err)
	}
	cost := 6.0
	if err := e.st.UpsertRunUsage(store.RunUsage{RunID: e.latestRun(t), TaskID: e.task.ID, Provider: "glm", CostUSD: &cost, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	return e, frozen
}

func (e *notifyEnv) latestRun(t *testing.T) string {
	t.Helper()
	r, ok, err := e.st.LatestRunForChat(e.parentChat.ID)
	if err != nil || !ok {
		t.Fatalf("LatestRunForChat() = %v, %v", ok, err)
	}
	return r.ID
}

func (e *notifyEnv) agentSend(t *testing.T) error {
	t.Helper()
	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.child.ID, 0, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceAgent, e.task.ID, e.parentChat.ID)
	return err
}

func TestBudgetNotify_OnePerCrossing_FakeClock(t *testing.T) {
	t.Parallel()
	e, frozen := newBudgetNotifyEnv(t)
	for i := 0; i < 3; i++ {
		if _, ok := asBudgetExceeded(e.agentSend(t)); !ok {
			t.Fatalf("refusal %d: want *BudgetExceededError", i+1)
		}
	}
	items := e.events(t, e.parentChat.ID)
	if len(items) != 1 {
		t.Fatalf("child event items after 3 refusals = %d, want 1: %+v", len(items), items)
	}
	if n := strings.Count(items[0].Prompt, "task tree #"); n != 1 {
		t.Fatalf("notification count = %d, want 1 in %q", n, items[0].Prompt)
	}
	want := "task tree #" + fmt.Sprint(e.task.ID) + " reached its budget ($6.00 of $5.00 reported cost)"
	if !strings.Contains(items[0].Prompt, want) {
		t.Fatalf("prompt %q lacks %q", items[0].Prompt, want)
	}
	if items[0].NotBefore == nil || !items[0].NotBefore.Equal(frozen.Add(time.Hour)) {
		t.Fatalf("not_before = %v, want the frozen clock + debounce %v", items[0].NotBefore, frozen.Add(time.Hour))
	}
}

func TestBudgetNotify_RearmedAfterRaise(t *testing.T) {
	t.Parallel()
	e, _ := newBudgetNotifyEnv(t)
	if err := e.agentSend(t); err == nil {
		t.Fatal("want refusal")
	}
	// Raising the cap re-arms (what task.setBudget does); the next crossing
	// notifies again, coalesced into the same still-queued item.
	if err := e.st.SetTaskBudget(e.task.ID, budgetPtr(6.5)); err != nil {
		t.Fatal(err)
	}
	e.reg.RearmBudgetNotify(e.task.ID)
	cost := 7.0
	if err := e.st.UpsertRunUsage(store.RunUsage{RunID: e.latestRun(t), TaskID: e.task.ID, Provider: "glm", CostUSD: &cost, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := e.agentSend(t); err == nil {
		t.Fatal("want refusal after recrossing")
	}
	items := e.events(t, e.parentChat.ID)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1 coalesced item", len(items))
	}
	if n := strings.Count(items[0].Prompt, "reached its budget"); n != 2 {
		t.Fatalf("notifications = %d, want 2 (one per crossing) in %q", n, items[0].Prompt)
	}
	// A later admission that finds the tree under the cap re-arms by itself.
	if err := e.st.SetTaskBudget(e.task.ID, budgetPtr(100)); err != nil {
		t.Fatal(err)
	}
	if err := e.agentSend(t); err != nil {
		t.Fatalf("agent send under the raised cap error = %v", err)
	}
	e.reg.budgetMu.Lock()
	armed := e.reg.budgetNotified[e.task.ID]
	e.reg.budgetMu.Unlock()
	if armed {
		t.Fatal("budgetNotified still set after an admission found the tree under the cap")
	}
}
