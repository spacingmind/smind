package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// registerMCPTools wires ADR-0017's read-only/simple tool set onto srv.
// Each tool is one wsapi round trip over the same client the CLI uses;
// the wire-shape structs below follow cmd/smind's existing convention of
// mirroring internal/wsapi's unexported result types field for field (see
// task.go's doc comment) -- the JSON wire contract is the interface, not
// a shared Go struct.
//
// No task_approve/task_deny here, by design (ADR-0017 resolved decision
// 1): an orchestrating agent must never resolve a sub-agent's permission
// request. task_send/task_wait/task_stop (steps 3-5 of the plan) land in
// later tasks.
func registerMCPTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_new", Description: "Create a new smind task (agent workspace) in a workspace. Returns the created task with its id, worktree path, and branch."}, mcpTaskNew(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_list", Description: "List a workspace's smind tasks (id, title, status, branch)."}, mcpTaskList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "chat_list", Description: "List a task's chats (conversation threads)."}, mcpChatList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "chat_new", Description: "Create a new chat (conversation thread) under a task."}, mcpChatNew(client))
	registerMCPRunTools(srv, client)
}

// callWS is the one wsapi invocation every tool handler funnels through:
// it carries the tool's own error text back so an MCP caller sees
// "task_list: <daemon rejection>" rather than an unlabeled string.
func callWS(ctx context.Context, client *wsclient.Client, tool, method string, params, out any) error {
	if err := client.Call(ctx, method, params, out); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}

// taskNewInput is task_new's argument shape (wrapping task.create).
type taskNewInput struct {
	WorkspaceID int64  `json:"workspaceId" jsonschema:"id of the workspace the task is created in"`
	SpaceID     *int64 `json:"spaceId,omitempty" jsonschema:"optional space id within the workspace to group the task under"`
	Title       string `json:"title" jsonschema:"title for the new task"`
}

// Output-shape note: the outputs below deliberately wrap daemon results
// in objects ({"task": {...}}, {"tasks": [...]}) rather than passing a
// bare array/object through as structuredContent -- MCP's
// structuredContent is specified as a JSON object, and a bare array
// risks rejection by clients that enforce that. The daemon's own shapes
// (store.Task, store.Chat: bare Go field names) ride along as
// map[string]any -- the wire contract with the daemon is dynamic, and
// json.RawMessage would make the SDK's output-schema validation reject
// an object where it inferred "array or null".

// taskNewOutput is task_new's structured output: the created task. The
// daemon's own store.Task wire shape (bare Go field names) passes through
// as-is under "task".
type taskNewOutput struct {
	Task map[string]any `json:"task"`
}

// mcpTaskNew wraps task.create: {workspaceId, title, spaceId?} -> the
// created task.
func mcpTaskNew(client *wsclient.Client) mcp.ToolHandlerFor[taskNewInput, taskNewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskNewInput) (*mcp.CallToolResult, taskNewOutput, error) {
		params := map[string]any{"workspaceId": in.WorkspaceID, "title": in.Title}
		if in.SpaceID != nil {
			params["spaceId"] = *in.SpaceID
		}
		var task map[string]any
		if err := callWS(ctx, client, "task_new", "task.create", params, &task); err != nil {
			return nil, taskNewOutput{}, err
		}
		return nil, taskNewOutput{Task: task}, nil
	}
}

// taskListInput is task_list's argument shape (wrapping task.list).
type taskListInput struct {
	WorkspaceID int64 `json:"workspaceId" jsonschema:"id of the workspace whose tasks to list"`
}

// taskListOutput is task_list's structured output: the workspace's tasks
// under "tasks" (an object, not a bare array -- see the note above
// taskNewOutput), empty rather than null when there are none.
type taskListOutput struct {
	Tasks []map[string]any `json:"tasks"`
}

// mcpTaskList wraps task.list: {workspaceId} -> the workspace's tasks.
func mcpTaskList(client *wsclient.Client) mcp.ToolHandlerFor[taskListInput, taskListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskListInput) (*mcp.CallToolResult, taskListOutput, error) {
		var tasks []map[string]any
		if err := callWS(ctx, client, "task_list", "task.list", map[string]any{"workspaceId": in.WorkspaceID}, &tasks); err != nil {
			return nil, taskListOutput{}, err
		}
		if tasks == nil {
			tasks = []map[string]any{}
		}
		return nil, taskListOutput{Tasks: tasks}, nil
	}
}

// chatListInput is chat_list's argument shape (wrapping chat.list).
type chatListInput struct {
	TaskID          int64 `json:"taskId" jsonschema:"id of the task whose chats to list"`
	IncludeArchived bool  `json:"includeArchived,omitempty" jsonschema:"include archived chats (default: active only)"`
}

// mcpChatList wraps chat.list (ADR-0016): {taskId, includeArchived?} ->
// the task's chats, so an orchestrator can address a specific
// conversation rather than always the task's default chat.
// chatListOutput is chat_list's structured output: the task's chats
// under "chats", empty rather than null when there are none.
type chatListOutput struct {
	Chats []map[string]any `json:"chats"`
}

func mcpChatList(client *wsclient.Client) mcp.ToolHandlerFor[chatListInput, chatListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in chatListInput) (*mcp.CallToolResult, chatListOutput, error) {
		var chats []map[string]any
		if err := callWS(ctx, client, "chat_list", "chat.list", map[string]any{
			"taskId": in.TaskID, "includeArchived": in.IncludeArchived,
		}, &chats); err != nil {
			return nil, chatListOutput{}, err
		}
		if chats == nil {
			chats = []map[string]any{}
		}
		return nil, chatListOutput{Chats: chats}, nil
	}
}

// chatNewInput is chat_new's argument shape (wrapping chat.create).
type chatNewInput struct {
	TaskID int64  `json:"taskId" jsonschema:"id of the task to create the chat under"`
	Title  string `json:"title,omitempty" jsonschema:"optional title for the new chat"`
}

// mcpChatNew wraps chat.create: {taskId, title?} -> the created chat. An
// omitted title is stored as a genuinely untitled chat, never defaulted
// (see handleChatCreate).
// chatNewOutput is chat_new's structured output: the created chat.
type chatNewOutput struct {
	Chat map[string]any `json:"chat"`
}

func mcpChatNew(client *wsclient.Client) mcp.ToolHandlerFor[chatNewInput, chatNewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in chatNewInput) (*mcp.CallToolResult, chatNewOutput, error) {
		var chat map[string]any
		if err := callWS(ctx, client, "chat_new", "chat.create", map[string]any{
			"taskId": in.TaskID, "title": in.Title,
		}, &chat); err != nil {
			return nil, chatNewOutput{}, err
		}
		return nil, chatNewOutput{Chat: chat}, nil
	}
}
