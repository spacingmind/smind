package main

import (
	"testing"
)

// taskForTest is the created-task handle the task tools return: store.Task
// has no json tags, so its Go field names are the wire keys.
type taskForTest struct {
	ID           int64
	WorkspaceID  int64
	Title        string
	Status       string
	WorktreePath *string
	Branch       *string
}

// TestMCPTools_TaskNewListRoundTrip proves task_new/task_list round-trip
// through a real daemon: create two tasks in one workspace, list it, and
// see exactly those two (the plan's per-tool happy path).
func TestMCPTools_TaskNewListRoundTrip(t *testing.T) {
	cs := newMCPSession(t)
	client := dialTestClient(t, daemonURLForTest(t))
	wsID := createTestWorkspace(t, client)

	var first taskForTest
	if isErr := callMCPTool(t, cs, "task_new", map[string]any{
		"workspaceId": wsID, "title": "First",
	}, &first); isErr {
		t.Fatal("task_new returned an error result")
	}
	if first.ID == 0 || first.Title != "First" || first.Status == "" || first.WorktreePath == nil || first.Branch == nil {
		t.Fatalf("task_new = %+v, want a fully-formed created task", first)
	}

	var second taskForTest
	callMCPTool(t, cs, "task_new", map[string]any{"workspaceId": wsID, "title": "Second"}, &second)

	var listed []taskForTest
	if isErr := callMCPTool(t, cs, "task_list", map[string]any{"workspaceId": wsID}, &listed); isErr {
		t.Fatal("task_list returned an error result")
	}
	if len(listed) != 2 {
		t.Fatalf("task_list = %d tasks, want 2", len(listed))
	}
	titles := map[string]bool{}
	for _, task := range listed {
		titles[task.Title] = true
	}
	if !titles["First"] || !titles["Second"] {
		t.Fatalf("task_list titles = %v, want First and Second", titles)
	}
}

// chatForMCPTest is the created-chat handle chat_new returns / chat_list
// lists.
type chatForMCPTest struct {
	ID         int64
	TaskID     int64
	Title      string
	Provider   *string
	ArchivedAt *string
}

// TestMCPTools_ChatNewList proves chat_new/chat_list: a task starts with
// its default chat, chat_new adds a second (optionally untitled), and
// chat_list returns both.
func TestMCPTools_ChatNewList(t *testing.T) {
	cs := newMCPSession(t)
	client := dialTestClient(t, daemonURLForTest(t))
	taskID := newTestRepoTask(t, client)

	var before []chatForMCPTest
	if isErr := callMCPTool(t, cs, "chat_list", map[string]any{"taskId": taskID}, &before); isErr {
		t.Fatal("chat_list returned an error result")
	}
	if len(before) != 1 || before[0].Title != "Chat" {
		t.Fatalf("chat_list on a fresh task = %+v, want the one default chat", before)
	}

	var created chatForMCPTest
	if isErr := callMCPTool(t, cs, "chat_new", map[string]any{"taskId": taskID, "title": "Side thread"}, &created); isErr {
		t.Fatal("chat_new returned an error result")
	}
	if created.ID == 0 || created.Title != "Side thread" || created.TaskID != taskID {
		t.Fatalf("chat_new = %+v, want the created chat", created)
	}

	var after []chatForMCPTest
	callMCPTool(t, cs, "chat_list", map[string]any{"taskId": taskID}, &after)
	if len(after) != 2 {
		t.Fatalf("chat_list after chat_new = %d chats, want 2", len(after))
	}
}
