package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestSyncLatchRefusesRunStart(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	start := func() error {
		_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
		return err
	}

	release, err := e.reg.TryBeginSync(e.task.ID)
	if err != nil {
		t.Fatalf("TryBeginSync() error = %v", err)
	}
	if err := start(); !errors.Is(err, ErrTaskSyncing) {
		t.Fatalf("Start while syncing: err = %v, want ErrTaskSyncing", err)
	}
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if !errors.Is(err, ErrTaskSyncing) {
		t.Fatalf("StartWhenBusy while syncing: err = %v, want ErrTaskSyncing", err)
	}
	if _, err := e.reg.TryBeginSync(e.task.ID); !errors.Is(err, ErrTaskSyncing) {
		t.Fatalf("second TryBeginSync: err = %v, want ErrTaskSyncing", err)
	}
	if _, err := e.reg.TryBeginArchive(e.task.ID); err == nil {
		t.Fatal("TryBeginArchive while syncing: error = nil, want refusal")
	}

	release()
	release() // idempotent
	if err := start(); err != nil {
		t.Fatalf("Start after release: %v", err)
	}
}

func TestSyncLatchRefusesActiveRunAndQueue(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, runID, StatusRunning, 5*time.Second)
	if _, err := e.reg.TryBeginSync(e.task.ID); !errors.Is(err, ErrSyncRunActive) {
		t.Fatalf("TryBeginSync with a run: err = %v, want ErrSyncRunActive", err)
	}
	// The refused attempt unlatched; the run-tolerant variant takes it.
	release, err := e.reg.TryBeginSyncAllowRun(e.task.ID)
	if err != nil {
		t.Fatalf("TryBeginSyncAllowRun with a run: %v", err)
	}
	release()
	if err := e.reg.Stop(runID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, runID, StatusStopped, 5*time.Second)
}

// A prompt whose delivery is refused by the latch stays queued (never
// cancelled) and is delivered when the latch is released.
func TestQueuedPromptSurvivesRefusedDelivery(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	item, chatID := e.enqueueHeld(t, "queued during a sync", nil)

	release, err := e.reg.TryBeginSyncAllowRun(e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.reg.DeliverQueued()
	got, err := e.st.GetChatQueueItem(item.ID)
	if err != nil || got.Status != store.ChatQueueStatusQueued {
		t.Fatalf("item while latched = %+v, %v; want still queued", got, err)
	}
	if n := len(e.reg.List(chatID)); n != 0 {
		t.Fatalf("runs while latched = %d, want 0", n)
	}
	release()

	delivered := pollItem(t, e.st, item.ID, store.ChatQueueStatusDelivered, 5*time.Second)
	waitForStatus(t, e.reg, *delivered.RunID, StatusDone, 5*time.Second)
}

func TestSyncLatchRefusesQueuedPrompt(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	hold := time.Now().Add(time.Hour)
	e.enqueueHeld(t, "waiting", &hold)
	if _, err := e.reg.TryBeginSync(e.task.ID); !errors.Is(err, ErrSyncRunActive) {
		t.Fatalf("TryBeginSync with a queued prompt: err = %v, want ErrSyncRunActive", err)
	}
}
