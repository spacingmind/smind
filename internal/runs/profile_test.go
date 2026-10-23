package runs

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func newTestProfileID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	p, err := st.CreateAgentProfile(store.AgentProfile{Name: "p", Provider: "glm"})
	if err != nil {
		t.Fatalf("CreateAgentProfile() error = %v", err)
	}
	return p.ID
}

func TestRunProfile_RecordedOnImmediateStart(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := newTestProfileID(t, e.st)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithProfile(pid))
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.ProfileID != pid {
		t.Fatalf("run.ProfileID = %d, want %d", row.ProfileID, pid)
	}

	plain, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithProfile(0)) // explicit none: an omitted profile would inherit (sticky seats)
	if err != nil {
		t.Fatalf("StartWhenBusy(plain) error = %v", err)
	}
	waitForStatus(t, e.reg, plain.RunID, StatusDone, 5*time.Second)
	if row, _ := e.st.GetRun(plain.RunID); row.ProfileID != 0 {
		t.Fatalf("plain run.ProfileID = %d, want 0", row.ProfileID)
	}
}

func TestRunProfile_QueuedItemKeepsIt(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := newTestProfileID(t, e.st)
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0, WithProfile(pid))
	if err != nil {
		t.Fatalf("StartWhenBusy(queue) error = %v", err)
	}
	if !out.Queued {
		t.Fatalf("StartWhenBusy(queue) = %+v, want queued", out)
	}
	waitForStatus(t, e.reg, runA, StatusDone, 5*time.Second)
	item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	if item.RunID == nil {
		t.Fatalf("delivered item has no run id")
	}
	waitForStatus(t, e.reg, *item.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(*item.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.ProfileID != pid {
		t.Fatalf("delivered run.ProfileID = %d, want %d", row.ProfileID, pid)
	}
}

func TestRunProfile_RehydratedKeepsIt(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := newTestProfileID(t, e.st)
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithProfile(pid))
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)

	row, err := e.st.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	r, err := rehydrateRun(e.st, row)
	if err != nil {
		t.Fatalf("rehydrateRun() error = %v", err)
	}
	if r.profileID != pid {
		t.Fatalf("rehydrated profileID = %d, want %d", r.profileID, pid)
	}
}
