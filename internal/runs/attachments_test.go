package runs

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func acceptFile(t *testing.T, st *attach.Store, name, content string) attach.Set {
	t.Helper()
	set, err := st.Accept(context.Background(), []attach.Request{{
		Kind: attach.KindFile, Name: name, MimeType: "text/plain",
		Data: base64.StdEncoding.EncodeToString([]byte(content)),
	}}, attach.DefaultLimits(), attach.Resolvers{})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	return set
}

func queueWithSet(t *testing.T, e *queuedTestEnv, st *attach.Store, mode WhenBusy, chatID int64) (StartOutcome, attach.Set) {
	t.Helper()
	set := acceptFile(t, st, "notes.txt", "attachment-body-sentinel")
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "second", taskrunner.PermissionSettings{}, "", mode, store.ChatQueueSourceHuman, 0, 0, WithAttachments(set, st))
	if err != nil || !out.Queued {
		t.Fatalf("StartWhenBusy(%s) = %+v, %v", mode, out, err)
	}
	item, err := e.st.GetChatQueueItem(out.QueueItemID)
	if err != nil || attach.EncodeSet(set) != item.Attachments {
		t.Fatalf("queue item attachments = %q, %v; want the accepted set", item.Attachments, err)
	}
	return out, set
}

// delivered checks the queue item's run carries the set (row, status) and the
// agent received the rendered text after the user's bare prompt.
func assertDeliveredWithSet(t *testing.T, e *queuedTestEnv, itemID int64, set attach.Set) {
	t.Helper()
	item := e.waitForQueuedStatus(t, itemID, store.ChatQueueStatusDelivered)
	waitForStatus(t, e.reg, *item.RunID, StatusDone, 5*time.Second)
	row, err := e.st.GetRun(*item.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Attachments != attach.EncodeSet(set) || row.Prompt != "second" {
		t.Fatalf("delivered run row = prompt %q attachments %q", row.Prompt, row.Attachments)
	}
	_, status, err := e.reg.History(*item.RunID)
	if err != nil || len(status.Attachments) != 1 || status.Attachments[0].File != "" {
		t.Fatalf("status attachments = %+v, %v", status.Attachments, err)
	}
	data, err := os.ReadFile(filepath.Join(*e.task.WorktreePath, "prompt-blocks"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "01-notes.txt") || !strings.Contains(string(data), "second") {
		t.Fatalf("agent never saw the attachment text: %s", data)
	}
}

func TestRunStart_QueueKeepsAttachments(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	st := attach.NewStore(t.TempDir())
	e.reg.SetAttachmentStore(st)
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)
	out, set := queueWithSet(t, e, st, WhenBusyQueue, chatID)
	waitForStatus(t, e.reg, runA, StatusDone, 5*time.Second)
	assertDeliveredWithSet(t, e, out.QueueItemID, set)
}

func TestRunStart_InterruptKeepsAttachments(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "hang")
	st := attach.NewStore(t.TempDir())
	e.reg.SetAttachmentStore(st)
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runA := e.startRun(t, "a")
	chatID := e.chatIDOf(t, runA)
	waitForHistoryLen(t, e.reg, runA, 1, 5*time.Second)
	t.Cleanup(func() { _ = e.reg.Stop(runA) })
	out, set := queueWithSet(t, e, st, WhenBusyInterrupt, chatID)
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, runA, StatusStopped, 5*time.Second)
	assertDeliveredWithSet(t, e, out.QueueItemID, set)
}

func startWithSet(t *testing.T, e *queuedTestEnv, st *attach.Store, set attach.Set) string {
	t.Helper()
	id, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "p", taskrunner.PermissionSettings{}, "", WithAttachments(set, st))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, id, StatusDone, 5*time.Second)
	return id
}

func TestReuse_SameTaskOnly(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	st := attach.NewStore(t.TempDir())
	set := acceptFile(t, st, "a.txt", "reuse-me")
	runID := startWithSet(t, e, st, set)

	it, path, err := e.reg.ReuseAttachment(st, e.task.ID, runID, "1")
	if err != nil || it.Kind != attach.KindFile {
		t.Fatalf("same-task reuse = %+v, %v", it, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "reuse-me" {
		t.Fatalf("reuse path bytes = %q, %v", b, err)
	}
	if _, _, err := e.reg.ReuseAttachment(st, e.task.ID+1, runID, "1"); err == nil {
		t.Fatal("reuse from another task succeeded")
	}
	if _, _, err := e.reg.ReuseAttachment(st, e.task.ID, runID, "2"); err == nil {
		t.Fatal("reuse of a missing item succeeded")
	}
	if _, _, err := e.reg.ReuseAttachment(st, e.task.ID, "no-such-run", "1"); err == nil {
		t.Fatal("reuse from an unknown run succeeded")
	}
}

// A reused item is a copy: deleting the old run's set leaves the new one
// readable, and a run evicted from memory is still a valid source.
func TestReuse_SurvivesOldRunDeletion(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	st := attach.NewStore(t.TempDir())
	old := acceptFile(t, st, "a.txt", "keep-me")
	runID := startWithSet(t, e, st, old)

	// A fresh registry over the same DB has no in-memory run: reuse must
	// read the row.
	reg2 := newTestRegistry(t, e.st)
	res := attach.Resolvers{Reuse: func(rid, id string) (attach.Item, string, error) {
		return reg2.ReuseAttachment(st, e.task.ID, rid, id)
	}}
	fresh, err := st.Accept(context.Background(), []attach.Request{{Kind: attach.KindReuse, RunID: runID, ID: "1"}}, attach.DefaultLimits(), res)
	if err != nil {
		t.Fatalf("Accept(reuse): %v", err)
	}
	if err := st.Remove(old.ID); err != nil {
		t.Fatal(err)
	}
	b, err := st.ReadFile(fresh.ID, fresh.Items[0])
	if err != nil || string(b) != "keep-me" {
		t.Fatalf("reused copy = %q, %v", b, err)
	}
}
