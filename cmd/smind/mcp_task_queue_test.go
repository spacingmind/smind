package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// TestMCPTools_TaskSendDefaultsToQueue pins ADR-0021 §9: task_send to a
// busy chat returns {queued: true, queueItemId} (the MCP default is
// queue, unlike the wsapi default reject), the queued prompt starts
// automatically once the first run finishes, and fromTaskId/fromChatId
// stamp the item as agent-sourced provenance.
func TestMCPTools_TaskSendDefaultsToQueue(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	// Make the task's runs hang (the fake agent reads a "scenario" file
	// from the worktree), so the chat stays busy until we stop the run.
	var task struct {
		WorktreePath *string `json:"WorktreePath"`
	}
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
		t.Fatalf("task.get: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	// Learn the task's default chat and make it busy with a hang run.
	var chats []struct {
		ID int64 `json:"ID"`
	}
	if err := client.Call(context.Background(), "chat.list", map[string]any{"taskId": taskID}, &chats); err != nil {
		t.Fatalf("chat.list: %v", err)
	}
	chatID := chats[0].ID

	var busy struct {
		RunID string `json:"runId"`
	}
	if err := client.Call(context.Background(), "run.start", map[string]any{
		"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "busy",
	}, &busy); err != nil {
		t.Fatalf("run.start(busy): %v", err)
	}
	waitForFirstRunChunk(t, client, busy.RunID)

	// No whenBusy passed: defaults to queue.
	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "queued follow-up",
	}, &sent); isErr {
		t.Fatal("task_send to a busy chat was rejected; want it queued by default")
	}
	if !sent.Queued || sent.QueueItemID == 0 {
		t.Fatalf("task_send output = %+v, want {queued: true, queueItemId}", sent)
	}

	// With fromTaskId/fromChatId the item carries agent provenance.
	var sent2 taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{
		"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "peer escalation",
		"fromTaskId": 7, "fromChatId": 9,
	}, &sent2); isErr {
		t.Fatal("agent-sourced task_send was rejected")
	}
	if !sent2.Queued {
		t.Fatalf("agent-sourced task_send = %+v, want queued", sent2)
	}
	var queue struct {
		Items []struct {
			ID     int64  `json:"ID"`
			Source string `json:"Source"`
		} `json:"items"`
	}
	if err := client.Call(context.Background(), "chat.queueList", map[string]any{"chatId": chatID}, &queue); err != nil {
		t.Fatalf("chat.queueList: %v", err)
	}
	items := queue.Items
	if len(items) != 2 || items[0].Source != "orchestrator" || items[1].Source != "agent" {
		t.Fatalf("queue = %+v, want an orchestrator item then an agent item", items)
	}

	// Free the chat: the first queued item is delivered automatically.
	// The scenario is cleared first so the delivered run can finish (the
	// fake agent re-reads the file per prompt).
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(""), 0o644); err != nil {
		t.Fatalf("clear scenario: %v", err)
	}
	if err := client.Call(context.Background(), "run.stop", map[string]any{"runId": busy.RunID}, nil); err != nil {
		t.Fatalf("run.stop: %v", err)
	}
	if deliveredRun := waitForDelivered(t, client, chatID, sent.QueueItemID); deliveredRun == "" {
		t.Fatal("queued item never delivered after the run finished")
	} else {
		waitForRunStatus(t, client, deliveredRun, "done")
	}
}

// waitForFirstRunChunk blocks until runID has at least one run.logs event.
func waitForFirstRunChunk(t *testing.T, client *wsclient.Client, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var logs struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		}
		if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": runID}, &logs); err == nil && len(logs.Events) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("busy run never produced its first chunk")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForDelivered polls chat.queueList until itemID reaches status
// delivered, returning the delivered item's run id (0 on timeout).
func waitForDelivered(t *testing.T, client *wsclient.Client, chatID, itemID int64) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var result struct {
			Items []struct {
				ID     int64   `json:"ID"`
				Status string  `json:"Status"`
				RunID  *string `json:"RunID"`
			} `json:"items"`
		}
		if err := client.Call(context.Background(), "chat.queueList", map[string]any{"chatId": chatID}, &result); err == nil {
			for _, item := range result.Items {
				if item.ID == itemID && item.Status == "delivered" && item.RunID != nil {
					return *item.RunID
				}
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(20 * time.Millisecond)
	}
}
