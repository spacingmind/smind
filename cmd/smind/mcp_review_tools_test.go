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
	if isErr := callMCPTool(t, env.cs, "queue_cancel", map[string]any{"itemId": sent.QueueItemID}, &cancelled); isErr || !cancelled.Cancelled {
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
