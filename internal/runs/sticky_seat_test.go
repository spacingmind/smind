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

// stickySend is an immediate StartWhenBusy on chatID (0 = default chat) that
// waits for the run to finish and returns its persisted ProfileID.
func (e *queuedTestEnv) stickySend(t *testing.T, chatID int64, opts ...StartOption) int64 {
	t.Helper()
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, opts...)
	if err != nil {
		t.Fatalf("StartWhenBusy() error = %v", err)
	}
	if out.Queued {
		t.Fatalf("StartWhenBusy() queued, want immediate start")
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(out.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	return row.ProfileID
}

func (e *queuedTestEnv) defaultChatID(t *testing.T) int64 {
	t.Helper()
	c, err := e.wm.DefaultChat(e.task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	return c.ID
}

func TestStickySeat_FollowUpInheritsLastProfile(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat := e.defaultChatID(t)
	if got := e.stickySend(t, chat, WithProfile(7)); got != 7 {
		t.Fatalf("first run ProfileID = %d, want 7", got)
	}
	if got := e.stickySend(t, chat); got != 7 {
		t.Fatalf("follow-up ProfileID = %d, want 7 (inherited)", got)
	}
}

func TestStickySeat_ExplicitProfileOverrides(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat := e.defaultChatID(t)
	e.stickySend(t, chat, WithProfile(7))
	if got := e.stickySend(t, chat, WithProfile(9)); got != 9 {
		t.Fatalf("ProfileID = %d, want 9", got)
	}
}

func TestStickySeat_ExplicitNoneOverridesAndSticks(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat := e.defaultChatID(t)
	e.stickySend(t, chat, WithProfile(7))
	if got := e.stickySend(t, chat, WithProfile(0)); got != 0 {
		t.Fatalf("explicit none ProfileID = %d, want 0", got)
	}
	if got := e.stickySend(t, chat); got != 0 {
		t.Fatalf("next omitted ProfileID = %d, want 0 (stays plain)", got)
	}
}

func TestStickySeat_QueuedFollowUpKeepsInheritedProfile(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	runA, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "a", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithProfile(7))
	if err != nil {
		t.Fatalf("start a error = %v", err)
	}
	t.Cleanup(func() { _ = e.reg.Stop(runA.RunID) })
	chatID := e.chatIDOf(t, runA.RunID)
	waitForHistoryLen(t, e.reg, runA.RunID, 1, 5*time.Second)

	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "b", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("queue error = %v", err)
	}
	if !out.Queued {
		t.Fatalf("outcome = %+v, want queued", out)
	}
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}
	if err := e.reg.Stop(runA.RunID); err != nil {
		t.Fatalf("Stop(a) error = %v", err)
	}
	item := e.waitForQueuedStatus(t, out.QueueItemID, store.ChatQueueStatusDelivered)
	if item.RunID == nil {
		t.Fatal("delivered item has no run id")
	}
	waitForStatus(t, e.reg, *item.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(*item.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.ProfileID != 7 {
		t.Fatalf("delivered run ProfileID = %d, want 7", row.ProfileID)
	}
}

func TestStickySeat_FirstRunOnAChatHasNoProfile(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	if got := e.stickySend(t, e.defaultChatID(t)); got != 0 {
		t.Fatalf("first run ProfileID = %d, want 0", got)
	}
}

func TestStickySeat_OtherChatsNotAffected(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.stickySend(t, e.defaultChatID(t), WithProfile(7))
	other, err := e.wm.CreateChat(e.task.ID, "other")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if got := e.stickySend(t, other.ID); got != 0 {
		t.Fatalf("other chat ProfileID = %d, want 0", got)
	}
}

func TestStickySeat_SystemRunNeverInherits(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat := e.defaultChatID(t)
	e.stickySend(t, chat, WithProfile(7))
	if got := e.stickySend(t, chat, WithPurpose("reflect")); got != 0 {
		t.Fatalf("system run ProfileID = %d, want 0", got)
	}
}

func TestStickySeat_PlainStartNeverInherits(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	chat := e.defaultChatID(t)
	e.stickySend(t, chat, WithProfile(7))
	runID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, chat, taskrunner.ProviderGLM, "plain", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, e.reg, runID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if row.ProfileID != 0 {
		t.Fatalf("plain Start ProfileID = %d, want 0", row.ProfileID)
	}
}
