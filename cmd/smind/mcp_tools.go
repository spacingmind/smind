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
	mcp.AddTool(srv, &mcp.Tool{Name: "task_new", Description: "Create a new sMind task (agent workspace) in a workspace. Returns the created task with its id, worktree path, and branch. The task's starting point is chosen with exactly ONE optional argument among baseRef (new branch at that ref's commit, e.g. origin/develop; the daemon best-effort fetches the remote branch first and falls back to the stale tracking ref), branch (check out an EXISTING local or origin-tracking branch into the worktree, no new branch; a later archive checkpoints onto that same branch), or prNumber (GitHub-hosted origin only: fetch refs/pull/<n>/head and branch smind/pr-<n>-<slug> from it). Supplying more than one is an error; omitting all three branches off the workspace checkout's current HEAD."}, mcpTaskNew(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_list", Description: "List a workspace's sMind tasks (id, title, status, branch)."}, mcpTaskList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "chat_list", Description: "List a task's chats (conversation threads)."}, mcpChatList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "chat_new", Description: "Create a new chat (conversation thread) under a task."}, mcpChatNew(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_send", Description: "Send a prompt to a task, starting a run. Returns {runId} immediately -- never blocks on the run; follow up with task_wait/task_status/task_logs. Optional provider-native permission settings (permissionMode/autoAccept) may not name an auto-approving configuration (bypass modes, autoAccept) -- an orchestrating agent cannot pick those itself; pass profileId to use a human-authored profile's settings instead. Optional model picks the run's model: any catalog id of the chosen provider (see the provider.list models), omitted for the provider's default; a profile's model applies unless you pass one."}, mcpTaskSend(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_archive", Description: "Archive a finished task. Commits everything uncommitted as a checkpoint commit onto the task's checked-out branch (for a task created from an existing branch that is the user's own branch) and removes the worktree directory; ignored files (.env, node_modules) are NOT preserved. Refuses while the task has a running or queued run -- stop it with task_stop first. A checkpoint failure aborts and keeps the worktree intact. Never deletes the branch (only the user's archive dialog can), so the work stays restorable."}, mcpTaskArchive(client))
	registerMCPRunTools(srv, client)
	registerMCPTreeTools(srv, client)
	registerMCPDiscoveryTools(srv, client)
	registerMCPLiveTools(srv, client)
	registerMCPReviewTools(srv, client)
	registerMCPAccountUsageTool(srv, client)
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
	WorkspaceID  int64  `json:"workspaceId" jsonschema:"id of the workspace the task is created in"`
	SpaceID      *int64 `json:"spaceId,omitempty" jsonschema:"optional space id within the workspace to group the task under"`
	ParentTaskID *int64 `json:"parentTaskId,omitempty" jsonschema:"optional id of an existing task in the same workspace to make this task a child of"`
	Title        string `json:"title" jsonschema:"title for the new task"`
	// BaseRef/Branch/PRNumber are mutually exclusive ref modes; the
	// jsonschema descriptions state the exclusion so the model sees it.
	BaseRef string `json:"baseRef,omitempty" jsonschema:"optional git ref (e.g. origin/develop) to branch the new task branch from at that ref's commit; mutually exclusive with branch and prNumber"`
	Branch  string `json:"branch,omitempty" jsonschema:"optional name of an EXISTING branch (local, or origin/<name>) to check out into the new worktree without creating a new branch; mutually exclusive with baseRef and prNumber"`
	PRNum   *int64 `json:"prNumber,omitempty" jsonschema:"optional GitHub pull request number to build the task from (GitHub-hosted origin only); mutually exclusive with baseRef and branch"`
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
		if in.ParentTaskID != nil {
			params["parentTaskId"] = *in.ParentTaskID
		}
		if in.BaseRef != "" {
			params["baseRef"] = in.BaseRef
		}
		if in.Branch != "" {
			params["branch"] = in.Branch
		}
		if in.PRNum != nil {
			params["prNumber"] = *in.PRNum
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
	WorkspaceID  int64  `json:"workspaceId" jsonschema:"id of the workspace whose tasks to list"`
	ParentTaskID *int64 `json:"parentTaskId,omitempty" jsonschema:"optional: list only the direct children of this task id, instead of every task in the workspace"`
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
		params := map[string]any{"workspaceId": in.WorkspaceID}
		if in.ParentTaskID != nil {
			params["parentTaskId"] = *in.ParentTaskID
		}
		var tasks []map[string]any
		if err := callWS(ctx, client, "task_list", "task.list", params, &tasks); err != nil {
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
