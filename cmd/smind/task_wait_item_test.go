package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// queuedBehindHold sets up a task whose agent holds until a "release" file
// appears in its worktree, starts one run (it busies the chat) and queues a
// second prompt behind it, returning the daemon client, the task id, the
// queue item's reference as `task send --detach` printed it, and a release
// function.
func queuedBehindHold(t *testing.T) (client *wsclient.Client, taskID, ref string, release func()) {
	t.Helper()
	client, taskID = waitEnv(t, "hold")
	first := detachedRun(t, taskID)
	waitForRunStatus(t, client, first, "running")

	code, stdout, stderr := sendCaptured(t, []string{"task", "send", taskID, "glm", "second", "--when-busy", "queue", "--detach"})
	if code != 0 {
		t.Fatalf("queued send = %d; stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "queued as item") {
		t.Fatalf("stderr = %q, want the queued notice", stderr)
	}
	var task struct {
		WorktreePath *string `json:"WorktreePath"`
	}
	id, _ := strconv.ParseInt(taskID, 10, 64)
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": id}, &task); err != nil {
		t.Fatalf("task.get: %v", err)
	}
	release = func() {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "release"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write release: %v", err)
		}
	}
	return client, taskID, strings.TrimSpace(stdout), release
}

func TestTaskSendDetachQueuedPrintsItemRef(t *testing.T) {
	_, _, ref, release := queuedBehindHold(t)
	defer release()
	rest, ok := strings.CutPrefix(ref, "item:")
	if !ok {
		t.Fatalf("stdout = %q, want item:<id> (a queued prompt has no run id yet)", ref)
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		t.Fatalf("stdout = %q, want item:<number>: %v", ref, err)
	}
}

// TestTaskWait_QueueItem: `task wait item:<id>` waits for the queued prompt to
// be delivered, then follows the run it became and reports its outcome.
func TestTaskWait_QueueItem(t *testing.T) {
	_, _, ref, release := queuedBehindHold(t)
	type res struct {
		code           int
		stdout, stderr string
	}
	ch := make(chan res, 1)
	go func() {
		c, o, e := sendCaptured(t, []string{"task", "wait", ref, "--timeout", "30s"})
		ch <- res{c, o, e}
	}()
	time.Sleep(700 * time.Millisecond) // wait is polling a still-queued item
	release()                          // the first run ends, the queued prompt starts and ends
	select {
	case r := <-ch:
		if r.code != 0 || !strings.Contains(r.stdout, "done (end_turn)") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q, want the delivered run's done summary", r.code, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stderr, "waiting for it to start") {
			t.Errorf("stderr = %q, want the waiting notice", r.stderr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("task wait item:<id> did not return after the queue drained")
	}
}

func TestTaskWait_QueueItemCancelled(t *testing.T) {
	_, _, ref, release := queuedBehindHold(t)
	defer release()
	id := strings.TrimPrefix(ref, "item:")
	type res struct {
		code           int
		stdout, stderr string
	}
	ch := make(chan res, 1)
	go func() {
		c, o, e := sendCaptured(t, []string{"task", "wait", ref, "--timeout", "30s"})
		ch <- res{c, o, e}
	}()
	time.Sleep(700 * time.Millisecond)
	if code, _, stderr := sendCaptured(t, []string{"task", "queue", "cancel", id}); code != 0 {
		t.Fatalf("queue cancel = %d; stderr = %q", code, stderr)
	}
	select {
	case r := <-ch:
		if r.code != 3 || !strings.Contains(r.stderr, "was cancelled") {
			t.Fatalf("code = %d, stderr = %q, want exit 3 and the cancellation", r.code, r.stderr)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("task wait did not return after the item was cancelled")
	}
}

func TestTaskWait_QueueItemTimeoutLeavesItQueued(t *testing.T) {
	_, _, ref, release := queuedBehindHold(t)
	defer release()
	code, _, stderr := sendCaptured(t, []string{"task", "wait", ref, "--timeout", "600ms"})
	if code != 5 || !strings.Contains(stderr, "is still queued") {
		t.Fatalf("code = %d, stderr = %q, want exit 5 and 'still queued'", code, stderr)
	}
}

func TestTaskWait_QueueItemUnknownAndMalformed(t *testing.T) {
	waitEnv(t, "")
	code, _, stderr := sendCaptured(t, []string{"task", "wait", "item:99999"})
	if code != 1 || !strings.Contains(stderr, "task wait:") {
		t.Fatalf("unknown item: code = %d, stderr = %q, want exit 1 with the daemon's error", code, stderr)
	}
	for _, bad := range []string{"item:abc", "item:", "item:0", "item:-3"} {
		code, _, stderr := sendCaptured(t, []string{"task", "wait", bad})
		if code != 2 || !strings.Contains(stderr, "invalid queue item") {
			t.Errorf("%s: code = %d, stderr = %q, want exit 2", bad, code, stderr)
		}
	}
}
