package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// taskWorktree returns the task's worktree path via task.get.
func taskWorktree(t *testing.T, client interface {
	Call(ctx context.Context, method string, params, out any) error
}, taskID int64) string {
	t.Helper()
	var task struct {
		WorktreePath *string `json:"WorktreePath"`
	}
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil || task.WorktreePath == nil {
		t.Fatalf("task.get: %v (path %v)", err, task.WorktreePath)
	}
	return *task.WorktreePath
}

func TestMCPTools_TaskDiff(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)
	wt := taskWorktree(t, client, taskID)

	var empty taskDiffOutput
	if isErr := callMCPTool(t, env.cs, "task_diff", map[string]any{"taskId": taskID}, &empty); isErr {
		t.Fatal("task_diff on a clean worktree returned an error")
	}
	if empty.Diff != "" || empty.Truncated {
		t.Fatalf("clean diff = %+v, want empty", empty)
	}

	if err := os.WriteFile(filepath.Join(wt, "NEW.txt"), []byte("hello diff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out taskDiffOutput
	if isErr := callMCPTool(t, env.cs, "task_diff", map[string]any{"taskId": taskID}, &out); isErr {
		t.Fatal("task_diff returned an error")
	}
	if !strings.Contains(out.Diff, "NEW.txt") || !strings.Contains(out.Diff, "hello diff") || out.Truncated {
		t.Fatalf("diff = %+v, want NEW.txt content, not truncated", out)
	}

	// Over the cap: truncated flag set and the text is cut to the cap.
	big := strings.Repeat("0123456789abcdef\n", 30000) // ~510 KiB
	if err := os.WriteFile(filepath.Join(wt, "BIG.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	var capped taskDiffOutput
	callMCPTool(t, env.cs, "task_diff", map[string]any{"taskId": taskID}, &capped)
	if !capped.Truncated || len(capped.Diff) != mcpDiffMaxBytes {
		t.Fatalf("big diff: truncated=%v len=%d, want truncated at %d", capped.Truncated, len(capped.Diff), mcpDiffMaxBytes)
	}

	if s, cut := truncateUTF8("aé", 2); !cut || s != "a" {
		t.Fatalf("truncateUTF8 split a rune: %q %v", s, cut)
	}
}

func TestMCPTools_QueueListAndCancel(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)
	wt := taskWorktree(t, client, taskID)
	if err := os.WriteFile(filepath.Join(wt, "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("run.start: %v", err)
	}
	waitForFirstRunChunk(t, client, busy.RunID)
	stopTestRuns(t, env, busy.RunID)

	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "later"}, &sent); isErr || !sent.Queued {
		t.Fatalf("task_send = %+v isErr=%v, want queued", sent, isErr)
	}

	var list queueListOutput
	if isErr := callMCPTool(t, env.cs, "queue_list", map[string]any{"chatId": chatID}, &list); isErr {
		t.Fatal("queue_list returned an error")
	}
	if len(list.Items) != 1 || list.Items[0]["Prompt"] != "later" || list.Items[0]["Status"] != "queued" {
		t.Fatalf("queue_list = %+v, want the one queued item", list.Items)
	}

	var cancelled queueCancelOutput
	if isErr := callMCPTool(t, env.cs, "queue_cancel", map[string]any{"chatId": chatID, "itemId": sent.QueueItemID}, &cancelled); isErr || !cancelled.Cancelled {
		t.Fatalf("queue_cancel = %+v isErr=%v", cancelled, isErr)
	}
	callMCPTool(t, env.cs, "queue_list", map[string]any{"chatId": chatID}, &list)
	if len(list.Items) != 1 || list.Items[0]["Status"] != "cancelled" {
		t.Fatalf("after cancel queue_list = %+v, want the item cancelled", list.Items)
	}
}

func TestMCPTools_UsageSummary(t *testing.T) {
	env := newMCPSession(t)
	var out usageSummaryOutput
	if isErr := callMCPTool(t, env.cs, "usage_summary", map[string]any{"groupBy": "provider", "scope": "runs"}, &out); isErr {
		t.Fatal("usage_summary returned an error")
	}
	if out.Rows == nil {
		t.Fatal("rows = nil, want an empty array")
	}
	// Invalid groupBy surfaces the daemon's rejection as a tool error.
	if msg := callMCPToolErr(t, env.cs, "usage_summary", map[string]any{"groupBy": "bogus"}); !strings.Contains(msg, "groupBy") {
		t.Fatalf("error = %q, want the daemon's groupBy rejection", msg)
	}
}

// busyChatEnv returns a session whose task's default chat is busy with a
// hanging run, so further sends queue.
func busyChatEnv(t *testing.T) (env *mcpTestEnv, client interface {
	Call(ctx context.Context, method string, params, out any) error
}, taskID, chatID int64) {
	t.Helper()
	env = newMCPSession(t)
	c := dialTestClient(t, env.srvURL)
	taskID = newTestRepoTask(t, c)
	if err := os.WriteFile(filepath.Join(taskWorktree(t, c, taskID), "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatal(err)
	}
	var chats []struct {
		ID int64 `json:"ID"`
	}
	if err := c.Call(context.Background(), "chat.list", map[string]any{"taskId": taskID}, &chats); err != nil {
		t.Fatalf("chat.list: %v", err)
	}
	chatID = chats[0].ID
	var busy struct {
		RunID string `json:"runId"`
	}
	if err := c.Call(context.Background(), "run.start", map[string]any{"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "busy"}, &busy); err != nil {
		t.Fatalf("run.start: %v", err)
	}
	waitForFirstRunChunk(t, c, busy.RunID)
	stopTestRuns(t, env, busy.RunID)
	return env, c, taskID, chatID
}

func queueItemStatus(t *testing.T, env *mcpTestEnv, chatID, itemID int64) string {
	t.Helper()
	var list queueListOutput
	callMCPTool(t, env.cs, "queue_list", map[string]any{"chatId": chatID}, &list)
	for _, it := range list.Items {
		if int64(it["ID"].(float64)) == itemID {
			return it["Status"].(string)
		}
	}
	return ""
}

func TestMCPTools_QueueCancelRefusesHumanItem(t *testing.T) {
	env, client, taskID, chatID := busyChatEnv(t)

	// run.start without a source is a human send (the web composer's path).
	var queued struct {
		QueueItemID int64 `json:"queueItemId"`
	}
	if err := client.Call(context.Background(), "run.start", map[string]any{
		"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "human instruction", "whenBusy": "queue",
	}, &queued); err != nil || queued.QueueItemID == 0 {
		t.Fatalf("human queue: %+v, %v", queued, err)
	}
	msg := callMCPToolErr(t, env.cs, "queue_cancel", map[string]any{"chatId": chatID, "itemId": queued.QueueItemID})
	if !strings.Contains(msg, "human") {
		t.Fatalf("error = %q, want a refusal naming the human source", msg)
	}
	if st := queueItemStatus(t, env, chatID, queued.QueueItemID); st != "queued" {
		t.Fatalf("human item status = %q, want still queued", st)
	}
}

func TestMCPTools_QueueCancelRefusesWrongChat(t *testing.T) {
	env, _, taskID, chatID := busyChatEnv(t)
	var sent taskSendOutput
	if isErr := callMCPTool(t, env.cs, "task_send", map[string]any{"taskId": taskID, "chatId": chatID, "provider": "glm", "prompt": "mine"}, &sent); isErr || !sent.Queued {
		t.Fatalf("task_send = %+v, want queued", sent)
	}

	msg := callMCPToolErr(t, env.cs, "queue_cancel", map[string]any{"chatId": chatID + 1000, "itemId": sent.QueueItemID})
	if !strings.Contains(msg, "not in chat") {
		t.Fatalf("wrong-chat error = %q", msg)
	}
	if st := queueItemStatus(t, env, chatID, sent.QueueItemID); st != "queued" {
		t.Fatalf("item status = %q, want still queued", st)
	}

	// Once cancelled it is no longer queued, so a second cancel is refused.
	var out queueCancelOutput
	if isErr := callMCPTool(t, env.cs, "queue_cancel", map[string]any{"chatId": chatID, "itemId": sent.QueueItemID}, &out); isErr || !out.Cancelled {
		t.Fatalf("cancel = %+v", out)
	}
	if msg := callMCPToolErr(t, env.cs, "queue_cancel", map[string]any{"chatId": chatID, "itemId": sent.QueueItemID}); !strings.Contains(msg, "not queued") {
		t.Fatalf("second cancel error = %q", msg)
	}
}
