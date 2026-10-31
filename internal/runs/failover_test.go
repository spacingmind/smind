package runs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type failedOpts struct {
	taskID      int64
	chatID      int64
	profileID   int64
	replyChatID int64
	failoverOf  string
	purpose     string
	prompt      string
	model       string
}

// profileWith creates a glm profile with the given policy.
func profileWith(t *testing.T, st *store.Store, policy string) int64 {
	t.Helper()
	p, err := st.CreateAgentProfile(store.AgentProfile{Name: "p-" + policy, Provider: "glm", OnRateLimit: policy})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	return p.ID
}

// failedRun writes a running row for a hand-built run and registers it; the
// caller applies the policy directly or finishes it.
func (e *queuedTestEnv) failedRun(t *testing.T, id string, o failedOpts) *run {
	t.Helper()
	if o.taskID == 0 {
		o.taskID = e.task.ID
	}
	if o.chatID == 0 {
		c, err := e.wm.DefaultChat(o.taskID)
		if err != nil {
			t.Fatalf("DefaultChat() error = %v", err)
		}
		o.chatID = c.ID
	}
	if o.prompt == "" {
		o.prompt = "do the work"
	}
	if _, err := e.st.CreateRun(store.Run{
		ID: id, TaskID: o.taskID, ChatID: o.chatID, Provider: "glm", Prompt: o.prompt, Status: "running",
		StartedAt: time.Now().UTC(), ProfileID: o.profileID, ReplyChatID: o.replyChatID, FailoverOf: o.failoverOf, Purpose: o.purpose, Model: o.model,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	r := registerTestRun(e.reg, id)
	r.taskID, r.chatID, r.provider, r.prompt = o.taskID, o.chatID, taskrunner.ProviderGLM, o.prompt
	r.profileID, r.replyChatID, r.failoverOf, r.purpose, r.model = o.profileID, o.replyChatID, o.failoverOf, o.purpose, o.model
	// A hand-built run that never finishes would make CloseAll wait forever.
	t.Cleanup(func() {
		r.mu.Lock()
		if r.status == StatusRunning {
			r.status = StatusError
		}
		r.mu.Unlock()
	})
	return r
}

// finishRateLimited finishes r as a rate-limited error with a Claude-style
// rejected signal resetting at reset.
func (e *queuedTestEnv) finishRateLimited(r *run, reset time.Time) {
	e.reg.applyRateLimit(r, &taskrunner.RateLimitSignal{Status: "rejected", ResetsAt: reset, Type: "five_hour"})
	e.reg.finish(r, errors.New("claude code prompt: exit status 1"))
}

func retryItems(t *testing.T, st *store.Store, chatID int64) []store.ChatQueueItem {
	t.Helper()
	items, err := st.ListChatQueue(chatID)
	if err != nil {
		t.Fatalf("ListChatQueue() error = %v", err)
	}
	var out []store.ChatQueueItem
	for _, it := range items {
		if it.Source == store.ChatQueueSourceOrchestrator {
			out = append(out, it)
		}
	}
	return out
}

func futureFail(d time.Duration) failure.Result {
	at := time.Now().Add(d).UTC()
	return failure.Result{Kind: failure.KindPlanWindowExhausted, Window: "5h", ResetAt: &at}
}

func TestPolicy_WaitEnqueuesSameChatAtResetPlus30s(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid, replyChatID: 77, model: "haiku", prompt: "[message from task #3, chat #4]\nbuild it"})
	r.perm = taskrunner.PermissionSettings{Mode: "plan"}
	fail := futureFail(time.Hour)

	e.reg.applyRateLimitPolicy(r, fail)

	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	it := items[0]
	if it.NotBefore == nil || !it.NotBefore.Equal(fail.ResetAt.Add(30*time.Second)) {
		t.Fatalf("not_before = %v, want reset+30s = %v", it.NotBefore, fail.ResetAt.Add(30*time.Second))
	}
	if it.Priority != 0 || it.FromTaskID != nil || it.FromChatID != nil || it.Attachments != "" {
		t.Fatalf("item = %+v, want priority 0, no from ids, no attachments", it)
	}
	if !strings.HasPrefix(it.Prompt, retryPreface) || strings.Contains(it.Prompt, "[message from") || !strings.HasSuffix(it.Prompt, "build it") {
		t.Fatalf("prompt = %q", it.Prompt)
	}
	var cfg chatQueueRunConfig
	if err := json.Unmarshal([]byte(it.RunConfig), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "glm" || cfg.Model != "haiku" || cfg.ReplyChatID != 77 || cfg.ProfileID != pid || cfg.FailoverOf != "failed-1" || cfg.PermissionMode != "plan" {
		t.Fatalf("run config = %+v", cfg)
	}
}

func TestPolicy_WaitNeedsKnownFutureReset(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	past := time.Now().Add(-time.Minute)
	for _, fail := range []failure.Result{
		{Kind: "rate_limited"},
		{Kind: "rate_limited", ResetAt: &past},
	} {
		e.reg.applyRateLimitPolicy(r, fail)
	}
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0", n)
	}
	// Not rate limited at all: nothing either.
	e.reg.applyRateLimitPolicy(r, failure.Result{})
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items for unclassified failure = %d, want 0", n)
	}
}

func TestPolicy_NoneAndEmptyDoNothing(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	for i, policy := range []string{"none", "fallback"} {
		pid := profileWith(t, e.st, policy)
		r := e.failedRun(t, "failed-"+policy, failedOpts{profileID: pid})
		e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
		if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
			t.Fatalf("case %d (%s): retry items = %d, want 0", i, policy, n)
		}
	}
}

func TestPolicy_NoProfileNoAction(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	r := e.failedRun(t, "no-profile", failedOpts{})
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))

	// A profile that was deleted after the run started.
	pid := profileWith(t, e.st, "wait")
	r2 := e.failedRun(t, "gone-profile", failedOpts{profileID: pid})
	if err := e.st.DeleteAgentProfile(pid); err != nil {
		t.Fatal(err)
	}
	e.reg.applyRateLimitPolicy(r2, futureFail(time.Hour))

	// A non-user run never acts either.
	pid2 := profileWith(t, e.st, "wait")
	r3 := e.failedRun(t, "system-run", failedOpts{profileID: pid2, purpose: "reflect"})
	e.reg.applyRateLimitPolicy(r3, futureFail(time.Hour))

	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0", n)
	}
}

func TestPolicy_OneActionPerChain(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "retry-run", failedOpts{profileID: pid, failoverOf: "first-run"})
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0 for a run that is itself a retry", n)
	}
}

func TestPolicy_WaitDeliveredRunKeepsFailoverOfAndSameChat(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.retryGrace = 0
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid, replyChatID: 0})
	e.finishRateLimited(r, time.Now().Add(300*time.Millisecond))

	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	got := pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
	waitForStatus(t, e.reg, *got.RunID, StatusDone, 10*time.Second)
	row, err := e.st.GetRun(*got.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ChatID != r.chatID || row.FailoverOf != "failed-1" || row.ProfileID != pid {
		t.Fatalf("retry run = chat %d failoverOf %q profile %d, want chat %d / failed-1 / %d", row.ChatID, row.FailoverOf, row.ProfileID, r.chatID, pid)
	}
}

func TestPolicy_UserCancelWins(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.retryGrace = 0
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	reset := time.Now().Add(200 * time.Millisecond)
	e.finishRateLimited(r, reset)

	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	e.reg.cancelQueuedItem(items[0].ID, store.ChatQueueCancelUser, "cancelled by user")
	for time.Now().Before(reset.Add(100 * time.Millisecond)) {
		time.Sleep(20 * time.Millisecond)
	}
	e.reg.DeliverQueued()
	if got, _ := e.st.GetChatQueueItem(items[0].ID); got.Status != store.ChatQueueStatusCancelled {
		t.Fatalf("item status = %q, want cancelled", got.Status)
	}
	if n := len(e.reg.List(r.chatID)); n != 1 { // only the failed run itself
		t.Fatalf("runs on chat = %d, want just the failed one", n)
	}
}

func TestPolicy_SurvivesRestart(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.retryGrace = 0
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	e.finishRateLimited(r, time.Now().Add(400*time.Millisecond))
	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	e.reg.CloseAll() // the "daemon" goes away before the reset

	reg2 := newTestRegistry(t, e.st)
	reg2.SetStarter(e.wm, e.runner)
	reg2.DeliverQueued()
	got := pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
	waitForStatus(t, reg2, *got.RunID, StatusDone, 10*time.Second)
}

func TestPolicy_ParentHearsRetryNotTheFailure(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.reg.retryGrace = 0
	pid := profileWith(t, e.st, "wait")
	childChat := e.chatOf(t, e.child.ID)
	r := e.failedRun(t, "child-failed", failedOpts{taskID: e.child.ID, chatID: childChat, profileID: pid, replyChatID: e.parentChat.ID})
	e.finishRateLimited(r, time.Now().Add(600*time.Millisecond))

	e.reg.evaluateChildNotifications(e.child.ID)
	if n := len(e.events(t, e.parentChat.ID)); n != 0 {
		t.Fatalf("parent has %d child_event items after the failure, want 0", n)
	}
	items := retryItems(t, e.st, childChat)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	got := pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
	e.waitDone(t, *got.RunID)
	waitUntil(t, "one child_event after the retry", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	e.reg.evaluateChildNotifications(e.child.ID)
	if n := len(e.events(t, e.parentChat.ID)); n != 1 {
		t.Fatalf("parent child_event items = %d, want exactly 1", n)
	}
}

func TestPolicy_QueueFullGivesUp(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	far := time.Now().Add(time.Hour)
	for i := 0; i < 20; i++ {
		if _, err := e.st.EnqueueChatPromptAt(r.chatID, "filler", heldRunConfig, store.ChatQueueSourceHuman, 0, 0, 0, "", &far); err != nil {
			t.Fatalf("filler %d: %v", i, err)
		}
	}
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0 when the queue is full", n)
	}
}
