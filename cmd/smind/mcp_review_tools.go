package main

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The review/queue/usage tools an orchestrator needs to inspect a task's
// work and manage its own pending prompts. None of them can resolve a
// permission request or widen a run's permissions (ADR-0017 #1,
// ADR-0019 #6): task_diff and usage_summary are read-only, and queue_cancel
// only removes a prompt that has not been delivered yet.

// mcpDiffMaxBytes caps task_diff's output so a huge diff cannot blow up the
// orchestrator's context.
const mcpDiffMaxBytes = 256 * 1024

// registerMCPReviewTools adds task_diff, queue_list, queue_cancel and
// usage_summary to the catalog.
func registerMCPReviewTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_diff", Description: "Read-only unified diff of a task's worktree against the commit its branch was created from (committed and uncommitted changes). Output is capped at 256 KiB; truncated: true means the diff was cut."}, mcpTaskDiff(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "queue_list", Description: "List a chat's queued prompts (all statuses, oldest first)."}, mcpQueueList(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "queue_cancel", Description: "Cancel a queued prompt that has not been delivered yet. It only removes the pending prompt; it cannot resolve permissions or change any run's settings."}, mcpQueueCancel(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "usage_summary", Description: "Read-only token/cost usage aggregated by groupBy. scope: proxy (groupBy account|model|day), runs (groupBy workspace|task|chat|run|provider|model) or all (default). since is inclusive, until exclusive."}, mcpUsageSummary(client))
}

type taskDiffInput struct {
	TaskID int64 `json:"taskId" jsonschema:"id of the task whose worktree diff to read"`
}

type taskDiffOutput struct {
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.Clone(s[:cut]), true
}

func mcpTaskDiff(client *wsclient.Client) mcp.ToolHandlerFor[taskDiffInput, taskDiffOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskDiffInput) (*mcp.CallToolResult, taskDiffOutput, error) {
		var res struct {
			Diff string `json:"diff"`
		}
		if err := callWS(ctx, client, "task_diff", "task.diff", map[string]any{"taskId": in.TaskID}, &res); err != nil {
			return nil, taskDiffOutput{}, err
		}
		diff, truncated := truncateUTF8(res.Diff, mcpDiffMaxBytes)
		return nil, taskDiffOutput{Diff: diff, Truncated: truncated}, nil
	}
}

type queueListInput struct {
	ChatID int64 `json:"chatId" jsonschema:"id of the chat whose queue to list"`
}

// queueListOutput carries the daemon's chat.queueList items as-is (bare Go
// field names: ID, Prompt, Status, Source, RunID, CancelKind, ...).
type queueListOutput struct {
	Items []map[string]any `json:"items"`
}

func mcpQueueList(client *wsclient.Client) mcp.ToolHandlerFor[queueListInput, queueListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in queueListInput) (*mcp.CallToolResult, queueListOutput, error) {
		var res queueListOutput
		if err := callWS(ctx, client, "queue_list", "chat.queueList", map[string]any{"chatId": in.ChatID}, &res); err != nil {
			return nil, queueListOutput{}, err
		}
		if res.Items == nil {
			res.Items = []map[string]any{}
		}
		return nil, res, nil
	}
}

type queueCancelInput struct {
	ItemID int64 `json:"itemId" jsonschema:"id of the queued item to cancel (from queue_list or task_send's queueItemId)"`
}

type queueCancelOutput struct {
	ItemID    int64 `json:"itemId"`
	Cancelled bool  `json:"cancelled"`
}

func mcpQueueCancel(client *wsclient.Client) mcp.ToolHandlerFor[queueCancelInput, queueCancelOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in queueCancelInput) (*mcp.CallToolResult, queueCancelOutput, error) {
		if err := callWS(ctx, client, "queue_cancel", "chat.queueCancel", map[string]any{"itemId": in.ItemID}, nil); err != nil {
			return nil, queueCancelOutput{}, err
		}
		return nil, queueCancelOutput{ItemID: in.ItemID, Cancelled: true}, nil
	}
}

type usageSummaryInput struct {
	Scope   string `json:"scope,omitempty" jsonschema:"proxy, runs, or all (default all)"`
	GroupBy string `json:"groupBy" jsonschema:"proxy: account|model|day; runs: workspace|task|chat|run|provider|model"`
	Since   string `json:"since,omitempty" jsonschema:"inclusive lower bound (RFC 3339 or YYYY-MM-DD)"`
	Until   string `json:"until,omitempty" jsonschema:"exclusive upper bound (RFC 3339 or YYYY-MM-DD)"`
}

type usageSummaryOutput struct {
	Rows []map[string]any `json:"rows"`
}

func mcpUsageSummary(client *wsclient.Client) mcp.ToolHandlerFor[usageSummaryInput, usageSummaryOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in usageSummaryInput) (*mcp.CallToolResult, usageSummaryOutput, error) {
		params := map[string]any{"groupBy": in.GroupBy}
		if in.Scope != "" {
			params["scope"] = in.Scope
		}
		if in.Since != "" {
			params["since"] = in.Since
		}
		if in.Until != "" {
			params["until"] = in.Until
		}
		var rows []map[string]any
		if err := callWS(ctx, client, "usage_summary", "usage.summary", params, &rows); err != nil {
			return nil, usageSummaryOutput{}, err
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		return nil, usageSummaryOutput{Rows: rows}, nil
	}
}
