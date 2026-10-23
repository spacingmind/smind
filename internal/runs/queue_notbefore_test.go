package runs

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const heldRunConfig = `{"provider":"glm"}`

// enqueueHeld puts an item on e's default chat straight into the store, held
// until notBefore (nil = deliverable now), and returns it with the chat id.
func (e *queuedTestEnv) enqueueHeld(t *testing.T, prompt string, notBefore *time.Time) (store.ChatQueueItem, int64) {
	t.Helper()
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	item, err := e.st.EnqueueChatPromptAt(chat.ID, prompt, heldRunConfig, store.ChatQueueSourceHuman, 0, 0, 0, "", notBefore)
	if err != nil {
		t.Fatalf("EnqueueChatPromptAt(%s) error = %v", prompt, err)
	}
	return item, chat.ID
}

// pollItem waits (polling, no fixed sleeps) until item id has status want.
func pollItem(t *testing.T, st *store.Store, id int64, want string, timeout time.Duration) store.ChatQueueItem {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		item, err := st.GetChatQueueItem(id)
		if err != nil {
			t.Fatalf("GetChatQueueItem(%d) error = %v", id, err)
		}
		if item.Status == want {
			return item
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue item %d still %q after %v, want %q", id, item.Status, timeout, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestQueue_NotBeforeHoldsThenTimerDelivers(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(300 * time.Millisecond)
	item, chatID := e.enqueueHeld(t, "held", &hold)

	e.reg.DeliverQueued()
	if got, _ := e.st.GetChatQueueItem(item.ID); got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("held item status = %q right after DeliverQueued, want queued", got.Status)
	}
	if n := len(e.reg.List(chatID)); n != 0 {
		t.Fatalf("runs before hold passes = %d, want 0", n)
	}

	// No further call: only the wake timer can deliver it.
	got := pollItem(t, e.st, item.ID, store.ChatQueueStatusDelivered, 10*time.Second)
	if got.RunID == nil {
		t.Fatal("delivered item has no run id")
	}
	waitForStatus(t, e.reg, *got.RunID, StatusDone, 5*time.Second)
	if n := len(e.reg.List(chatID)); n != 1 {
		t.Fatalf("runs after delivery = %d, want 1", n)
	}
}

func TestQueue_NotBeforePastDeliversImmediately(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	past := time.Now().Add(-time.Minute)
	item, _ := e.enqueueHeld(t, "past", &past)

	e.reg.DeliverQueued()
	got := pollItem(t, e.st, item.ID, store.ChatQueueStatusDelivered, 5*time.Second)
	waitForStatus(t, e.reg, *got.RunID, StatusDone, 5*time.Second)
}

func TestQueue_HeldItemDoesNotBlockLaterFIFOItem(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(time.Hour)
	a, _ := e.enqueueHeld(t, "A held", &hold)
	b, _ := e.enqueueHeld(t, "B free", nil)

	e.reg.DeliverQueued()
	gotB := pollItem(t, e.st, b.ID, store.ChatQueueStatusDelivered, 5*time.Second)
	waitForStatus(t, e.reg, *gotB.RunID, StatusDone, 5*time.Second)
	if got, _ := e.st.GetChatQueueItem(a.ID); got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("held item A status = %q, want still queued", got.Status)
	}
}

func TestQueue_NotBeforeSurvivesRestart(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(300 * time.Millisecond)
	item, _ := e.enqueueHeld(t, "held across restart", &hold)

	// A second registry on the same store stands in for the restarted
	// daemon: wire the starter, then run the startup delivery pass.
	reg2 := newTestRegistry(t, e.st)
	reg2.SetStarter(e.wm, e.runner)
	reg2.DeliverQueued()
	if got, _ := e.st.GetChatQueueItem(item.ID); got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("held item status = %q at startup, want queued", got.Status)
	}
	got := pollItem(t, e.st, item.ID, store.ChatQueueStatusDelivered, 10*time.Second)
	waitForStatus(t, reg2, *got.RunID, StatusDone, 5*time.Second)
}

func TestQueue_CloseAllStopsWakeTimer(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(150 * time.Millisecond)
	item, chatID := e.enqueueHeld(t, "never", &hold)

	e.reg.DeliverQueued() // arms the timer
	e.reg.CloseAll()

	// Watch past the hold (plus the timer slack): nothing may start.
	deadline := hold.Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := e.st.GetChatQueueItem(item.ID); got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("item status after CloseAll = %q, want queued", got.Status)
	}
	if n := len(e.reg.List(chatID)); n != 0 {
		t.Fatalf("runs after CloseAll = %d, want 0", n)
	}
}

// startHeld sends a human prompt through StartWhenBusy held until notBefore.
func (e *queuedTestEnv) startHeld(t *testing.T, prompt string, notBefore time.Time) StartOutcome {
	t.Helper()
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, prompt, taskrunner.PermissionSettings{}, "", "", store.ChatQueueSourceHuman, 0, 0, WithNotBefore(notBefore))
	if err != nil {
		t.Fatalf("StartWhenBusy(%q, notBefore) error = %v", prompt, err)
	}
	return out
}

func TestStartWhenBusy_NotBefore_QueuesOnIdleChat(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(time.Hour)
	out := e.startHeld(t, "later", hold)
	if !out.Queued || out.QueueItemID == 0 || out.RunID != "" {
		t.Fatalf("outcome = %+v, want queued item and no run", out)
	}
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if n := len(e.reg.List(chat.ID)); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
	item, err := e.st.GetChatQueueItem(out.QueueItemID)
	if err != nil {
		t.Fatalf("GetChatQueueItem() error = %v", err)
	}
	if item.Status != store.ChatQueueStatusQueued || item.Prompt != "later" || item.NotBefore == nil || !item.NotBefore.Equal(hold) {
		t.Fatalf("item = %+v, want queued %q held until %v", item, "later", hold)
	}
}

func TestStartWhenBusy_NotBefore_DeliversWhenTimePasses(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	out := e.startHeld(t, "soon", time.Now().Add(300*time.Millisecond))
	if got, _ := e.st.GetChatQueueItem(out.QueueItemID); got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("item status = %q right after enqueue, want queued", got.Status)
	}
	// Only the wake timer StartWhenBusy armed can deliver it.
	got := pollItem(t, e.st, out.QueueItemID, store.ChatQueueStatusDelivered, 10*time.Second)
	waitForStatus(t, e.reg, *got.RunID, StatusDone, 5*time.Second)
}
