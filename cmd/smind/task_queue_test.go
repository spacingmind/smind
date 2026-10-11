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

// TestTaskSend_WhenBusyFlag pins the CLI half of ADR-0021 §9:
// `task send --when-busy=queue` on a busy chat passes the flag through to
// run.start, prints the {queued, queueItemId} outcome instead of trying to
// stream a nonexistent run, and the default (no flag) is reject -- the
// busy error surfaces verbatim. `task queue ls` lists the item and
// `task queue cancel` cancels it.
func TestTaskSend_WhenBusyFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	id := strconv.FormatInt(taskID, 10)

	// Make runs hang so the chat stays busy.
	var task struct {
		WorktreePath *string `json:"WorktreePath"`
	}
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
		t.Fatalf("task.get: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	// Busy the chat.
	var busy struct {
		RunID string `json:"runId"`
	}
	if err := client.Call(context.Background(), "run.start", map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "busy",
	}, &busy); err != nil {
		t.Fatalf("run.start(busy): %v", err)
	}
	waitForRunStatus(t, client, busy.RunID, "running")

	// Default (no flag): reject -- the legacy busy error reaches stderr.
	code := -1
	stderr := captureStderr(t, func() {
		captureStdout(t, func() { code = run([]string{"task", "send", id, "glm", "hi"}) })
	})
	if code != 1 {
		t.Fatalf("run(task send, busy, no flag) = %d, want 1; stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "already has a running run") {
		t.Fatalf("stderr = %q, want the legacy busy error", stderr)
	}

	// --when-busy=queue: passes through, prints the queued outcome.
	code = -1
	stderr = captureStderr(t, func() {
		captureStdout(t, func() { code = run([]string{"task", "send", id, "glm", "hi", "--when-busy=queue"}) })
	})
	if code != 0 {
		t.Fatalf("run(task send --when-busy=queue) = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "queued as item") {
		t.Fatalf("stderr = %q, want the queued-item outcome", stderr)
	}

	// An invalid value exits 2 without dialing.
	if code, _ := runCaptured(t, []string{"task", "send", id, "glm", "hi", "--when-busy=now"}); code != 2 {
		t.Fatalf("run(task send --when-busy=now) = %d, want 2", code)
	}

	// task queue ls shows the queued item.
	var queue struct {
		Items []struct {
			ID     int64  `json:"ID"`
			Status string `json:"Status"`
		} `json:"items"`
	}
	if err := client.Call(context.Background(), "chat.queueList", map[string]any{"chatId": chatIDOfRun(t, client, busy.RunID)}, &queue); err != nil {
		t.Fatalf("chat.queueList: %v", err)
	}
	if len(queue.Items) != 1 || queue.Items[0].Status != "queued" {
		t.Fatalf("queue = %+v, want one queued item", queue.Items)
	}
	code, lsOut := runCaptured(t, []string{"task", "queue", "ls", "--chat", strconv.FormatInt(chatIDOfRun(t, client, busy.RunID), 10)})
	if code != 0 || !strings.Contains(collapseSpaces(lsOut), "queued") {
		t.Fatalf("run(task queue ls) = %d out %q, want the queued item", code, lsOut)
	}

	// task queue cancel cancels it.
	code, _ = runCaptured(t, []string{"task", "queue", "cancel", strconv.FormatInt(queue.Items[0].ID, 10)})
	if code != 0 {
		t.Fatalf("run(task queue cancel) = %d, want 0", code)
	}
	if err := client.Call(context.Background(), "chat.queueList", map[string]any{"chatId": chatIDOfRun(t, client, busy.RunID)}, &queue); err != nil {
		t.Fatalf("chat.queueList(after cancel): %v", err)
	}
	if len(queue.Items) != 1 || queue.Items[0].Status != "cancelled" {
		t.Fatalf("queue after cancel = %+v, want the item cancelled", queue.Items)
	}

	// Clean up the hang run.
	if err := client.Call(context.Background(), "run.stop", map[string]any{"runId": busy.RunID}, nil); err != nil {
		t.Fatalf("run.stop: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}

// chatIDOfRun resolves a run's chat id via run.list.
func chatIDOfRun(t *testing.T, client *wsclient.Client, runID string) int64 {
	t.Helper()
	var runs []struct {
		ID     string `json:"ID"`
		ChatID int64  `json:"ChatID"`
	}
	if err := client.Call(context.Background(), "run.list", map[string]any{}, &runs); err != nil {
		t.Fatalf("run.list: %v", err)
	}
	for _, r := range runs {
		if r.ID == runID {
			return r.ChatID
		}
	}
	t.Fatalf("run %s not in run.list", runID)
	return 0
}
