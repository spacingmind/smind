package main

import (
	"context"
	"encoding/json"
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

// mcpTaskNew wraps task.create: {workspaceId, title, spaceId?} -> the
// created task.
func mcpTaskNew(client *wsclient.Client) mcp.ToolHandlerFor[taskNewInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskNewInput) (*mcp.CallToolResult, any, error) {
		params := map[string]any{"workspaceId": in.WorkspaceID, "title": in.Title}
		if in.SpaceID != nil {
			params["spaceId"] = *in.SpaceID
		}
		var task json.RawMessage
		if err := callWS(ctx, client, "task_new", "task.create", params, &task); err != nil {
			return nil, nil, err
		}
		return nil, task, nil
	}
}

// taskListInput is task_list's argument shape (wrapping task.list).
type taskListInput struct {
	WorkspaceID int64 `json:"workspaceId" jsonschema:"id of the workspace whose tasks to list"`
}

// mcpTaskList wraps task.list: {workspaceId} -> the workspace's tasks.
func mcpTaskList(client *wsclient.Client) mcp.ToolHandlerFor[taskListInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskListInput) (*mcp.CallToolResult, any, error) {
		var tasks json.RawMessage
		if err := callWS(ctx, client, "task_list", "task.list", map[string]any{"workspaceId": in.WorkspaceID}, &tasks); err != nil {
			return nil, nil, err
		}
		return nil, tasks, nil
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
func mcpChatList(client *wsclient.Client) mcp.ToolHandlerFor[chatListInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in chatListInput) (*mcp.CallToolResult, any, error) {
		var chats json.RawMessage
		if err := callWS(ctx, client, "chat_list", "chat.list", map[string]any{
			"taskId": in.TaskID, "includeArchived": in.IncludeArchived,
		}, &chats); err != nil {
			return nil, nil, err
		}
		return nil, chats, nil
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
func mcpChatNew(client *wsclient.Client) mcp.ToolHandlerFor[chatNewInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in chatNewInput) (*mcp.CallToolResult, any, error) {
		var chat json.RawMessage
		if err := callWS(ctx, client, "chat_new", "chat.create", map[string]any{
			"taskId": in.TaskID, "title": in.Title,
		}, &chat); err != nil {
			return nil, nil, err
		}
		return nil, chat, nil
	}
}
