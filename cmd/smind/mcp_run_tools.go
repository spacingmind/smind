package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The three run-reading tools of ADR-0017's tool set. All of them share
// run.logs under the hood, exactly like their CLI counterparts
// (`task logs`/`task permissions`, cmd/smind/task.go), because run.logs is
// the daemon's single non-blocking source of both a run's transcript and
// its permission history -- there is no separate "pending permissions"
// RPC to wrap.

// registerMCPRunTools adds the run-reading tools to the catalog.
func registerMCPRunTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_status", Description: "Non-blocking snapshot of a run: status, the last few transcript entries, and any still-pending permission request."}, mcpTaskStatus(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_logs", Description: "Read a run's full (or tailed) transcript and current status."}, mcpTaskLogs(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_permissions", Description: "List a run's still-pending permission requests (read-only). The human approves or denies them via `smind task approve` or the smind web UI; there is no tool for that here."}, mcpTaskPermissions(client))
}

// runIDInput is the shared argument shape of the run-reading tools.
type runIDInput struct {
	RunID string `json:"runId" jsonschema:"id of the run (returned by task_send)"`
}

// mcpRunLogs performs one run.logs call with the given tail (0 = full
// history), decoding into the CLI's own runLogsResult mirror -- shared by
// all three tools rather than re-decoded per tool.
func mcpRunLogs(ctx context.Context, client *wsclient.Client, tool string, runID string, tail int) (runLogsResult, error) {
	var result runLogsResult
	err := callWS(ctx, client, tool, "run.logs", map[string]any{"runId": runID, "tail": tail}, &result)
	return result, err
}

// mcpStatusTail is how many trailing transcript entries task_status
// returns -- a snapshot, not the full history (that's task_logs).
const mcpStatusTail = 20

// taskStatusOutput is task_status's structured output.
type taskStatusOutput struct {
	RunID             string                `json:"runId"`
	Status            string                `json:"status"`
	StopReason        string                `json:"stopReason,omitempty"`
	Err               string                `json:"err,omitempty"`
	Events            []runLogEvent         `json:"events"`
	PendingPermission *pendingPermissionOut `json:"pendingPermission,omitempty"`
}

// mcpTaskStatus wraps run.logs as a non-blocking snapshot: status, the
// last few transcript entries, and any still-pending permission request.
// For an orchestrator that wants to poll cheaply rather than block (the
// blocking shape, task_wait, is a later step of the plan).
func mcpTaskStatus(client *wsclient.Client) mcp.ToolHandlerFor[runIDInput, taskStatusOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runIDInput) (*mcp.CallToolResult, taskStatusOutput, error) {
		result, err := mcpRunLogs(ctx, client, "task_status", in.RunID, 0)
		if err != nil {
			return nil, taskStatusOutput{}, err
		}
		out := taskStatusOutput{
			RunID:      result.RunID,
			Status:     result.Status,
			StopReason: result.StopReason,
			Err:        result.Err,
		}
		if len(result.Events) > mcpStatusTail {
			out.Events = result.Events[len(result.Events)-mcpStatusTail:]
		} else {
			out.Events = result.Events
		}
		pending, _ := pendingPermissionsFrom(result)
		if len(pending) > 0 {
			out.PendingPermission = &pending[0]
		}
		return nil, out, nil
	}
}

// taskLogsInput is task_logs's argument shape (wrapping run.logs).
type taskLogsInput struct {
	RunID string `json:"runId" jsonschema:"id of the run whose transcript to read"`
	Tail  int    `json:"tail,omitempty" jsonschema:"return only the last N entries (default: all)"`
}

// mcpTaskLogs wraps run.logs: full or tailed transcript plus the run's
// current status, mirroring `smind task logs` (without --follow, which
// has no request/response MCP shape).
func mcpTaskLogs(client *wsclient.Client) mcp.ToolHandlerFor[taskLogsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskLogsInput) (*mcp.CallToolResult, any, error) {
		result, err := mcpRunLogs(ctx, client, "task_logs", in.RunID, in.Tail)
		if err != nil {
			return nil, nil, err
		}
		return nil, result, nil
	}
}

// pendingPermissionOut is one still-unresolved permission request, the
// MCP shape of the pendingPermission cmdTaskPermissions derives today.
type pendingPermissionOut struct {
	RequestID string                   `json:"requestId"`
	Summary   string                   `json:"summary"`
	Options   []permissionOptionParams `json:"options"`
}

// pendingPermissionsFrom derives the still-pending permission requests
// from a run.logs result -- every permission_request whose requestId never
// shows up on a later permission_resolved entry -- the same read-through
// logic fetchPendingPermissions (cmd/smind/task.go) implements for the
// CLI. Read-only by construction: nothing here can resolve a request.
func pendingPermissionsFrom(result runLogsResult) ([]pendingPermissionOut, error) {
	resolved := map[string]bool{}
	for _, e := range result.Events {
		if e.Type == "permission_resolved" {
			resolved[e.RequestID] = true
		}
	}
	var pending []pendingPermissionOut
	for _, e := range result.Events {
		if e.Type == "permission_request" && !resolved[e.RequestID] {
			pending = append(pending, pendingPermissionOut{
				RequestID: e.RequestID, Summary: e.Summary, Options: e.Options,
			})
		}
	}
	return pending, nil
}

// mcpTaskPermissions wraps the same run.logs read the CLI's
// `task permissions` does: {runId} -> the run's still-pending permission
// requests. Deliberately read-only (ADR-0017 resolved decision 1): the
// orchestrator surfaces these to the human, who approves via
// `smind task approve` or the web UI -- there is no task_approve tool.
func mcpTaskPermissions(client *wsclient.Client) mcp.ToolHandlerFor[runIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runIDInput) (*mcp.CallToolResult, any, error) {
		result, err := mcpRunLogs(ctx, client, "task_permissions", in.RunID, 0)
		if err != nil {
			return nil, nil, err
		}
		pending, err := pendingPermissionsFrom(result)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"runId": in.RunID, "pending": pending}, nil
	}
}
