package runs

import (
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func TestClearProviderCooldown_ClearsBlock(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.cool(t, "glm", time.Hour)
	var ce *ProviderCooldownError
	if _, err := e.send(0, WhenBusyQueue, store.ChatQueueSourceAgent, 7, 8); !errors.As(err, &ce) {
		t.Fatalf("send while cooling down: err = %v, want *ProviderCooldownError", err)
	}

	cleared, woken, err := e.reg.ClearProviderCooldown("glm")
	if err != nil || !cleared || woken != 0 {
		t.Fatalf("ClearProviderCooldown() = %v, %d, %v, want cleared with nothing held", cleared, woken, err)
	}

	out, err := e.send(0, WhenBusyQueue, store.ChatQueueSourceAgent, 7, 8)
	if errors.As(err, &ce) {
		t.Fatalf("send after clear: err = %v, still fast-failing on the cooldown", err)
	}
	if err != nil {
		t.Fatalf("send after clear: err = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}

func TestClearProviderCooldown_WakesHeldRetriesOnce(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r1 := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	chat2, err := e.wm.CreateChat(e.task.ID, "second")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	r2 := e.failedRun(t, "failed-2", failedOpts{profileID: pid, chatID: chat2.ID})
	reset := time.Now().Add(time.Hour)
	for _, r := range []*run{r1, r2} {
		e.finishRateLimited(r, reset)
		items := retryItems(t, e.st, r.chatID)
		if len(items) != 1 || items[0].NotBefore == nil {
			t.Fatalf("chat %d retry items = %+v, want one held", r.chatID, items)
		}
	}
	e.cool(t, "glm", time.Hour)

	cleared, woken, err := e.reg.ClearProviderCooldown("glm")
	if err != nil || !cleared || woken != 2 {
		t.Fatalf("ClearProviderCooldown() = %v, %d, %v, want cleared and 2 woken", cleared, woken, err)
	}
	for _, r := range []*run{r1, r2} {
		items := retryItems(t, e.st, r.chatID)
		got := pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
		if got.RunID == nil || *got.RunID == "" || got.NotBefore != nil {
			t.Fatalf("released item %d = %+v, want delivered with no hold", items[0].ID, got)
		}
		waitForStatus(t, e.reg, *got.RunID, StatusDone, 10*time.Second)
	}

	before := map[int64]int{r1.chatID: len(e.reg.List(r1.chatID)), r2.chatID: len(e.reg.List(r2.chatID))}
	cleared, woken, err = e.reg.ClearProviderCooldown("glm")
	if err != nil || cleared || woken != 0 {
		t.Fatalf("second ClearProviderCooldown() = %v, %d, %v, want a no-op", cleared, woken, err)
	}
	for chatID, n := range before {
		if got := len(e.reg.List(chatID)); got != n {
			t.Fatalf("chat %d runs grew from %d to %d after the second clear", chatID, n, got)
		}
	}
}

func TestClearProviderCooldown_OtherProviderUntouched(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	nb := time.Now().Add(time.Hour).UTC()
	item, err := e.st.EnqueueChatPromptAt(chat.ID, "held", `{"provider":"claude","failoverOf":"x"}`, store.ChatQueueSourceOrchestrator, 0, 0, 0, "", &nb)
	if err != nil {
		t.Fatalf("EnqueueChatPromptAt() error = %v", err)
	}
	e.cool(t, "glm", time.Hour)

	if _, _, err := e.reg.ClearProviderCooldown("glm"); err != nil {
		t.Fatalf("ClearProviderCooldown() error = %v", err)
	}
	got, err := e.st.GetChatQueueItem(item.ID)
	if err != nil || got.NotBefore == nil || !got.NotBefore.Equal(nb) {
		t.Fatalf("claude item = %+v, %v, want its hold kept", got, err)
	}
}
