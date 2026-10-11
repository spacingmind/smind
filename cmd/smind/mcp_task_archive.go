package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// taskArchiveInput is task_archive's argument shape (wrapping
// task.archive).
type taskArchiveInput struct {
	TaskID int64 `json:"taskId" jsonschema:"id of the task to archive"`
}

// taskArchiveOutput is task_archive's structured output: the archived
// task (daemon store.Task wire shape) under "task".
type taskArchiveOutput struct {
	Task map[string]any `json:"task"`
}

// mcpTaskArchive wraps task.archive: {taskId} -> the archived task. The
// daemon refuses while the task has a running or queued run (stop it
// first). Archiving commits everything uncommitted as a checkpoint commit
// onto the task's checked-out branch -- for a task created from an
// existing branch, that is the user's own branch -- and then removes the
// worktree directory; ignored files (.env, node_modules) are NOT
// preserved. A checkpoint failure aborts and leaves the worktree intact.
func mcpTaskArchive(client *wsclient.Client) mcp.ToolHandlerFor[taskArchiveInput, taskArchiveOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskArchiveInput) (*mcp.CallToolResult, taskArchiveOutput, error) {
		var task map[string]any
		if err := callWS(ctx, client, "task_archive", "task.archive", map[string]any{"id": in.TaskID}, &task); err != nil {
			return nil, taskArchiveOutput{}, err
		}
		return nil, taskArchiveOutput{Task: task}, nil
	}
}
