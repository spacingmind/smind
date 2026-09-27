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
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	wsID := createTestWorkspace(t, client)

	// task_new wraps the created task under "task".
	var created struct {
		Task taskForTest `json:"task"`
	}
	if isErr := callMCPTool(t, env.cs, "task_new", map[string]any{
		"workspaceId": wsID, "title": "First",
	}, &created); isErr {
		t.Fatal("task_new returned an error result")
	}
	if created.Task.ID == 0 || created.Task.Title != "First" || created.Task.Status == "" || created.Task.WorktreePath == nil || created.Task.Branch == nil {
		t.Fatalf("task_new = %+v, want a fully-formed created task", created.Task)
	}

	var second struct {
		Task taskForTest `json:"task"`
	}
	callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": wsID, "title": "Second"}, &second)

	// task_list wraps the list under "tasks" and returns [] not null for
	// an empty workspace.
	var listed struct {
		Tasks []taskForTest `json:"tasks"`
	}
	if isErr := callMCPTool(t, env.cs, "task_list", map[string]any{"workspaceId": wsID}, &listed); isErr {
		t.Fatal("task_list returned an error result")
	}
	if len(listed.Tasks) != 2 {
		t.Fatalf("task_list = %d tasks, want 2 (both created via task_new)", len(listed.Tasks))
	}
	titles := map[string]bool{}
	for _, task := range listed.Tasks {
		titles[task.Title] = true
	}
	if !titles["First"] || !titles["Second"] {
		t.Fatalf("task_list titles = %v, want First and Second", titles)
	}

	var empty struct {
		Tasks []taskForTest `json:"tasks"`
	}
	if isErr := callMCPTool(t, env.cs, "task_list", map[string]any{"workspaceId": 999999}, &empty); isErr {
		t.Fatal("task_list on an unknown workspace returned an error result")
	}
	if empty.Tasks == nil || len(empty.Tasks) != 0 {
		t.Fatalf("task_list on an unknown workspace = %#v, want tasks: [] not null", empty.Tasks)
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
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	taskID := newTestRepoTask(t, client)

	var before struct {
		Chats []chatForMCPTest `json:"chats"`
	}
	if isErr := callMCPTool(t, env.cs, "chat_list", map[string]any{"taskId": taskID}, &before); isErr {
		t.Fatal("chat_list returned an error result")
	}
	if len(before.Chats) != 1 || before.Chats[0].Title != "Chat" {
		t.Fatalf("chat_list on a fresh task = %+v, want the one default chat", before.Chats)
	}

	var created struct {
		Chat chatForMCPTest `json:"chat"`
	}
	if isErr := callMCPTool(t, env.cs, "chat_new", map[string]any{"taskId": taskID, "title": "Side thread"}, &created); isErr {
		t.Fatal("chat_new returned an error result")
	}
	if created.Chat.ID == 0 || created.Chat.Title != "Side thread" || created.Chat.TaskID != taskID {
		t.Fatalf("chat_new = %+v, want the created chat", created.Chat)
	}

	var after struct {
		Chats []chatForMCPTest `json:"chats"`
	}
	callMCPTool(t, env.cs, "chat_list", map[string]any{"taskId": taskID}, &after)
	if len(after.Chats) != 2 {
		t.Fatalf("chat_list after chat_new = %d chats, want 2", len(after.Chats))
	}
}
