package runs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestRegistry_StartWhenBusyRecordsOrigin(t *testing.T) {
	t.Parallel()
	for _, source := range []string{store.ChatQueueSourceHuman, store.ChatQueueSourceOrchestrator} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			e := newQueuedTestEnv(t, "")
			out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{Mode: ""}, "", WhenBusyReject, source, 0, 0)
			if err != nil || out.Queued {
				t.Fatalf("StartWhenBusy = %+v, %v", out, err)
			}
			st := waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
			if st.Origin != source {
				t.Fatalf("RunStatus.Origin = %q, want %q", st.Origin, source)
			}
			row, err := e.st.GetRun(out.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if row.Origin != source || row.Interrupt {
				t.Fatalf("row origin/interrupt = %q/%v, want %q/false", row.Origin, row.Interrupt, source)
			}
		})
	}
}

func TestRegistry_QueuedDeliveryRecordsOriginAndInterrupt(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)
	waitForHistoryLen(t, e.reg, runA, 1, 5*time.Second)
	t.Cleanup(func() { _ = e.reg.Stop(runA) })

	q1, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "q1", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceAgent, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	i1, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "i1", taskrunner.PermissionSettings{}, "", WhenBusyInterrupt, store.ChatQueueSourceOrchestrator, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	itemI1 := e.waitForQueuedStatus(t, i1.QueueItemID, store.ChatQueueStatusDelivered)
	itemQ1 := e.waitForQueuedStatus(t, q1.QueueItemID, store.ChatQueueStatusDelivered)
	waitForStatus(t, e.reg, *itemI1.RunID, StatusDone, 5*time.Second)
	waitForStatus(t, e.reg, *itemQ1.RunID, StatusDone, 5*time.Second)

	for _, c := range []struct {
		runID     string
		origin    string
		interrupt bool
	}{
		{*itemI1.RunID, store.ChatQueueSourceOrchestrator, true},
		{*itemQ1.RunID, store.ChatQueueSourceAgent, false},
	} {
		row, err := e.st.GetRun(c.runID)
		if err != nil {
			t.Fatal(err)
		}
		if row.Origin != c.origin || row.Interrupt != c.interrupt {
			t.Fatalf("run %s origin/interrupt = %q/%v, want %q/%v", c.runID, row.Origin, row.Interrupt, c.origin, c.interrupt)
		}
	}
}

func TestRegistry_RehydrateKeepsOrigin(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceOrchestrator, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)

	reg2 := newTestRegistry(t, e.st)
	_, status, err := reg2.History(out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Origin != store.ChatQueueSourceOrchestrator {
		t.Fatalf("rehydrated Origin = %q, want orchestrator", status.Origin)
	}
}
