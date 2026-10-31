package runs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func (e *queuedTestEnv) cool(t *testing.T, provider string, d time.Duration) time.Time {
	t.Helper()
	until := time.Now().Add(d).UTC()
	if err := e.st.SetProviderCooldown(provider, until, "earlier", time.Now()); err != nil {
		t.Fatal(err)
	}
	return until
}

func (e *queuedTestEnv) send(chatID int64, mode WhenBusy, source string, fromTask, fromChat int64) (StartOutcome, error) {
	return e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "go", taskrunner.PermissionSettings{}, "", mode, source, fromTask, fromChat)
}

func TestCooldown_SetFromClassifiedRateLimit(t *testing.T) {
	t.Parallel()
	// A reset within the 6h cap, so the stored value equals the classified one.
	x := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
	_, st, status := finishDirect(t, "cd-set", &taskrunner.RateLimitSignal{Status: "rejected", ResetsAt: x, Type: "five_hour"}, errors.New("429 Usage limit reached for 5 hour"))
	if status.FailureResetAt == nil {
		t.Fatal("no FailureResetAt")
	}
	got, ok, err := st.GetProviderCooldown("glm", time.Now())
	if err != nil || !ok {
		t.Fatalf("cooldown ok=%v err=%v, want set", ok, err)
	}
	if !got.Equal(*status.FailureResetAt) {
		t.Fatalf("cooldown reset = %v, want FailureResetAt %v", got, *status.FailureResetAt)
	}
}

func TestCooldown_UnknownResetSetsNone(t *testing.T) {
	t.Parallel()
	// A rate limit whose reset cannot be parsed keeps the kind, no reset.
	_, st, status := finishDirect(t, "cd-unknown", nil, errors.New("429 Usage limit reached for 5 hour. Please try later"))
	if status.FailureKind != "rate_limited" || status.FailureResetAt != nil {
		t.Fatalf("status = %+v, want rate_limited with unknown reset", status)
	}
	if _, ok, _ := st.GetProviderCooldown("glm", time.Now()); ok {
		t.Fatal("cooldown set for an unknown reset")
	}
}

func TestCooldown_FastFailForAgentSend(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runID)
	t.Cleanup(func() { _ = e.reg.Stop(runID) })
	until := e.cool(t, "glm", time.Hour)

	for _, tc := range []struct {
		source     string
		from, chat int64
	}{{store.ChatQueueSourceAgent, 7, 8}, {store.ChatQueueSourceOrchestrator, 0, 0}} {
		for _, mode := range []WhenBusy{WhenBusyReject, WhenBusyQueue} {
			_, err := e.send(chatID, mode, tc.source, tc.from, tc.chat)
			var ce *ProviderCooldownError
			if !errors.As(err, &ce) || ce.Provider != "glm" || !ce.Until.Equal(until) {
				t.Fatalf("%s/%s: err = %v, want *ProviderCooldownError", tc.source, mode, err)
			}
		}
	}
	if runs, _ := e.st.ListRecentRuns(100); len(runs) != 1 {
		t.Fatalf("runs = %d, want only the original", len(runs))
	}
	if q, _ := e.st.ListChatQueue(chatID); len(q) != 0 {
		t.Fatalf("queue = %+v, want empty", q)
	}
}

func TestCooldown_HumanSendWarnsAndRuns(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "glm", time.Hour)
	out, err := e.send(0, "", store.ChatQueueSourceHuman, 0, 0)
	if err != nil || out.RunID == "" {
		t.Fatalf("human send = %+v err=%v, want a started run", out, err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
	hist, _, err := e.reg.History(out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range hist {
		if ev.Type == taskrunner.EventTypeSessionNote && strings.Contains(ev.Text, "provider glm was rate-limited until") && strings.Contains(ev.Text, "may fail the same way") {
			return
		}
	}
	t.Fatalf("history has no cooldown session note: %+v", hist)
}

func TestCooldown_ProfileSendWithFromIdsIsBlocked(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "glm", time.Hour)
	_, err := e.send(0, "", store.ChatQueueSourceHuman, 7, 8)
	var ce *ProviderCooldownError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ProviderCooldownError", err)
	}
}

func TestCooldown_ExpiresAtReset(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "glm", 150*time.Millisecond)
	if _, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8); err == nil {
		t.Fatal("send before the reset was accepted")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
		if err == nil {
			waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
			return
		}
		var ce *ProviderCooldownError
		if !errors.As(err, &ce) || time.Now().After(deadline) {
			t.Fatalf("send after the reset: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCooldown_OtherProviderUnaffected(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "claude", time.Hour)
	out, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	if err != nil {
		t.Fatalf("glm send while only claude cools down: %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}

func TestCooldown_DeliveryOfQueuedItemNeverBlocked(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)
	t.Cleanup(func() { _ = e.reg.Stop(runA) })
	out, err := e.send(chatID, WhenBusyQueue, store.ChatQueueSourceAgent, 7, 8)
	if err != nil || !out.Queued {
		t.Fatalf("queue = %+v err=%v", out, err)
	}
	e.cool(t, "glm", time.Hour) // set after the item was accepted
	if err := e.reg.Stop(runA); err != nil {
		t.Fatal(err)
	}
	item := e.waitForQueuedStatus(t, out.QueueItemID, "delivered")
	if item.RunID == nil || *item.RunID == "" {
		t.Fatalf("delivered item has no run: %+v", item)
	}
	_ = e.reg.Stop(*item.RunID)
}

func TestCooldown_SurvivesRestart(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "glm", time.Hour)
	reg2, err := New(e.st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg2.CloseAll() })
	reg2.SetStarter(e.wm, e.runner)
	_, err = reg2.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "go", taskrunner.PermissionSettings{}, "", "", store.ChatQueueSourceAgent, 7, 8)
	var ce *ProviderCooldownError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ProviderCooldownError after a restart", err)
	}
	if got := reg2.ProviderCooldowns(); len(got) != 1 {
		t.Fatalf("ProviderCooldowns = %v", got)
	}
}

func TestProviderCooldownErrorText(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 15, 32, 0, 0, time.Local)
	until := time.Date(2026, 10, 23, 17, 35, 0, 0, time.Local)
	want := "runs: start: provider glm is rate-limited until 17:35 (in 2h 3m): start the work on another provider (provider_list) or retry after that time"
	if got := formatProviderCooldownError("glm", until.UTC(), now); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := formatProviderCooldownError("glm", now.Add(14*time.Minute), now); !strings.Contains(got, "(in 14m)") {
		t.Fatalf("minutes form: %q", got)
	}
}

func TestCooldown_ModelScopedWindowSetsNone(t *testing.T) {
	t.Parallel()
	x := time.Unix(time.Now().Add(time.Hour).Unix(), 0)
	for _, tc := range []struct {
		typ  string
		want bool
	}{{"seven_day_opus", false}, {"seven_day_sonnet", false}, {"five_hour", true}, {"seven_day", true}} {
		_, st, status := finishDirect(t, "cd-win-"+tc.typ, &taskrunner.RateLimitSignal{Status: "rejected", ResetsAt: x, Type: tc.typ}, errors.New("claude code prompt: exit status 1"))
		if status.FailureKind != "rate_limited" || status.FailureResetAt == nil {
			t.Fatalf("%s: status = %+v, want a classified rate limit with a reset", tc.typ, status)
		}
		if _, ok, _ := st.GetProviderCooldown("glm", time.Now()); ok != tc.want {
			t.Fatalf("%s: cooldown set = %v, want %v", tc.typ, ok, tc.want)
		}
	}
}

// TestRateLimit_InstanceCooldownLeavesBaseRunning: a claude-native instance
// profiled as vendor zai classifies a GLM-worded 429 by its vendor profile,
// so the cooldown lands on the instance id only -- the base login keeps
// running. Mutates providers.Default(), so no t.Parallel.
func TestRateLimit_InstanceCooldownLeavesBaseRunning(t *testing.T) {
	if err := providers.Default().Register(providers.Spec{ID: "zai-t", Extends: providers.IDClaudeNative, Vendor: providers.VendorZAI}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providers.Default().Unregister("zai-t") })

	// GLM-worded 429 with a dated reset ~2h out, printed on the UTC+8 clock.
	text := "429 Usage limit reached for 5 hour. Your limit will reset at " +
		time.Now().Add(2*time.Hour).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")

	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	now := time.Now().UTC()
	if _, err := st.CreateRun(store.Run{ID: "cd-inst", TaskID: task.ID, Provider: "zai-t", Prompt: "p", Status: "running", StartedAt: now}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	r := registerTestRun(reg, "cd-inst")
	r.taskID, r.provider, r.prompt = task.ID, taskrunner.Provider("zai-t"), "p"
	reg.finish(r, errors.New(text))

	if _, status, _ := reg.History("cd-inst"); status.FailureKind != "rate_limited" || status.FailureWindow != "5h" || status.FailureResetAt == nil {
		t.Fatalf("status = %+v, want a classified GLM 5h rate limit with a reset", status)
	}
	cool := reg.ProviderCooldowns()
	if _, ok := cool["zai-t"]; !ok {
		t.Fatalf("ProviderCooldowns = %v, want an entry for zai-t", cool)
	}
	if _, ok := cool[providers.IDClaudeNative]; ok {
		t.Fatalf("ProviderCooldowns = %v, want no entry for %s", cool, providers.IDClaudeNative)
	}
}
