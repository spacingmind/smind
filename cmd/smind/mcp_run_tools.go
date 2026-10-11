package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The three run-reading tools of ADR-0017's tool set. All of them share
// run.logs under the hood, exactly like their CLI counterparts
// (`task logs`/`task permissions`, cmd/smind/task.go), because run.logs is
// the daemon's single non-blocking source of both a run's transcript and
// its permission history -- there is no separate "pending permissions"
// RPC to wrap.

// registerMCPRunTools adds the run-reading tools, task_wait, and task_stop
// to the catalog.
func registerMCPRunTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "task_status", Description: "Non-blocking snapshot of a run: status, the last few transcript entries, and any still-pending permission request. A pending permission means the run is NOT finished: loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again; only done/error/stopped mean the run ended."}, mcpTaskStatus(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_logs", Description: "Read a run's full (or tailed) transcript and current status."}, mcpTaskLogs(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_permissions", Description: "List a run's still-pending permission requests (read-only). Each carries a `detail` when the daemon has one (path, old/new sizes, replacesExisting/largeShrink/outsideWorktree flags, and a diff or preview): surface it to the human so they can see what an approval would change. The human approves or denies them via `smind task approve` or the smind web UI; there is no tool for that here. loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again; only done/error/stopped mean the run ended."}, mcpTaskPermissions(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_wait", Description: "Block until a run finishes (done/error/stopped), a permission request goes pending, or the timeout elapses (default 120s). Check `state`: loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again; only done/error/stopped mean the run ended. A timeout is not a failure -- timedOut: true just means the run is still going; re-issue task_wait with the same runId to keep waiting, or use task_status for a non-blocking check."}, mcpTaskWait(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_stop", Description: "Stop a running run."}, mcpTaskStop(client))
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

// mcpRunEvent is runLogEvent's MCP-facing mirror: same JSON names, but the
// free-form fields (tool input/result, raw payload) are `any` rather than
// json.RawMessage. The go-sdk infers "array or null" for a []byte-typed
// field, so output validation rejected every real object there.
type mcpRunEvent struct {
	Type       string                   `json:"type"`
	Text       string                   `json:"text,omitempty"`
	StopReason string                   `json:"stopReason,omitempty"`
	RequestID  string                   `json:"requestId,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Options    []permissionOptionParams `json:"options,omitempty"`
	Detail     *permissionDetail        `json:"detail,omitempty"`
	OptionID   string                   `json:"optionId,omitempty"`
	Reason     string                   `json:"reason,omitempty"`
	ToolCallID string                   `json:"toolCallId"`
	ToolName   string                   `json:"toolName,omitempty"`
	Title      string                   `json:"title,omitempty"`
	Status     string                   `json:"status,omitempty"`
	Input      any                      `json:"input,omitempty"`
	Result     any                      `json:"result,omitempty"`
	Kind       string                   `json:"kind"`
	Payload    any                      `json:"payload,omitempty"`
}

// rawToAny decodes one raw JSON value, falling back to its text if it is
// not valid JSON; nil (absent) stays nil.
func rawToAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// toMCPRunEvents converts run.logs events to their MCP shape; the result
// is never nil so events serializes as [] rather than null.
func toMCPRunEvents(events []runLogEvent) []mcpRunEvent {
	out := make([]mcpRunEvent, 0, len(events))
	for _, e := range events {
		out = append(out, mcpRunEvent{
			Type: e.Type, Text: e.Text, StopReason: e.StopReason,
			RequestID: e.RequestID, Summary: e.Summary, Options: e.Options, Detail: e.Detail,
			OptionID: e.OptionID, Reason: e.Reason,
			ToolCallID: e.ToolCallID, ToolName: e.ToolName, Title: e.Title, Status: e.Status,
			Input: rawToAny(e.Input), Result: rawToAny(e.Result),
			Kind: e.Kind, Payload: rawToAny(e.Payload),
		})
	}
	return out
}

// taskStatusOutput is task_status's structured output.
type taskStatusOutput struct {
	RunID             string                `json:"runId"`
	Status            string                `json:"status"`
	StopReason        string                `json:"stopReason,omitempty"`
	Err               string                `json:"err,omitempty"`
	Events            []mcpRunEvent         `json:"events"`
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
		events := result.Events
		if len(events) > mcpStatusTail {
			events = events[len(events)-mcpStatusTail:]
		}
		out.Events = toMCPRunEvents(events)
		if pending := pendingPermissionsFrom(result); len(pending) > 0 {
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

// taskLogsOutput is task_logs's structured output: the run's current
// status plus its full (or tailed) transcript. events is empty rather
// than null for an eventless run.
type taskLogsOutput struct {
	RunID      string        `json:"runId"`
	Status     string        `json:"status"`
	StopReason string        `json:"stopReason,omitempty"`
	Err        string        `json:"err,omitempty"`
	Events     []mcpRunEvent `json:"events"`
}

// mcpTaskLogs wraps run.logs: full or tailed transcript plus the run's
// current status, mirroring `smind task logs` (without --follow, which
// has no request/response MCP shape).
func mcpTaskLogs(client *wsclient.Client) mcp.ToolHandlerFor[taskLogsInput, taskLogsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskLogsInput) (*mcp.CallToolResult, taskLogsOutput, error) {
		result, err := mcpRunLogs(ctx, client, "task_logs", in.RunID, in.Tail)
		if err != nil {
			return nil, taskLogsOutput{}, err
		}
		return nil, taskLogsOutput{
			RunID: result.RunID, Status: result.Status,
			StopReason: result.StopReason, Err: result.Err, Events: toMCPRunEvents(result.Events),
		}, nil
	}
}

// pendingPermissionOut is one still-unresolved permission request, the
// MCP shape of the pendingPermission cmdTaskPermissions derives today.
type pendingPermissionOut struct {
	RequestID string                   `json:"requestId"`
	Summary   string                   `json:"summary"`
	Options   []permissionOptionParams `json:"options"`
	Detail    *permissionDetail        `json:"detail,omitempty"`
}

// pendingPermissionsFrom derives the still-pending permission requests
// from a run.logs result -- every permission_request whose requestId never
// shows up on a later permission_resolved entry -- the same read-through
// logic fetchPendingPermissions (cmd/smind/task.go) implements for the
// CLI. Read-only by construction: nothing here can resolve a request.
func pendingPermissionsFrom(result runLogsResult) []pendingPermissionOut {
	// A finished run has nothing left to approve: a request still
	// unresolved in its transcript (e.g. the run was stopped mid-request)
	// is stale and must not make a looping driver wait on a dead run.
	if isTerminalRunStatus(result.Status) {
		return nil
	}
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
				RequestID: e.RequestID, Summary: e.Summary, Options: e.Options, Detail: e.Detail,
			})
		}
	}
	return pending
}

// mcpTaskPermissions wraps the same run.logs read the CLI's
// `task permissions` does: {runId} -> the run's still-pending permission
// requests. Deliberately read-only (ADR-0017 resolved decision 1): the
// orchestrator surfaces these to the human, who approves via
// `smind task approve` or the web UI -- there is no task_approve tool.
// taskPermissionsOutput is task_permissions's structured output: the
// run's still-pending permission requests, empty rather than null.
type taskPermissionsOutput struct {
	RunID   string                 `json:"runId"`
	Pending []pendingPermissionOut `json:"pending"`
}

func mcpTaskPermissions(client *wsclient.Client) mcp.ToolHandlerFor[runIDInput, taskPermissionsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runIDInput) (*mcp.CallToolResult, taskPermissionsOutput, error) {
		result, err := mcpRunLogs(ctx, client, "task_permissions", in.RunID, 0)
		if err != nil {
			return nil, taskPermissionsOutput{}, err
		}
		pending := pendingPermissionsFrom(result)
		if pending == nil {
			pending = []pendingPermissionOut{}
		}
		return nil, taskPermissionsOutput{RunID: in.RunID, Pending: pending}, nil
	}
}

// mcpWaitDefaultTimeout is task_wait's default timeout (ADR-0017 resolved
// decision 4): a timeout is not a failure, so this only bounds how long one
// tool call blocks -- the orchestrator is expected to re-issue task_wait
// with the same runId when it sees timedOut: true.
const mcpWaitDefaultTimeout = 120 * time.Second

// mcpWaitPollMin/Max bound the backoff task_wait polls run.logs with,
// instead of a new wsapi RPC or a busy loop (ADR-0017's "Surfacing
// long-running runs" section): fast enough at first to catch a quick
// permission request without materially overshooting a short timeout, capped
// well below any timeout a caller is likely to use.
const (
	mcpWaitPollMin = 50 * time.Millisecond
	mcpWaitPollMax = 500 * time.Millisecond
)

// isTerminalRunStatus reports whether status is one of run.logs's terminal
// statuses (internal/runs.StatusDone/StatusError/StatusStopped's wire
// values) -- the set task_wait blocks for, mirrored from printRunLogs's own
// switch over the same statuses.
func isTerminalRunStatus(status string) bool {
	switch status {
	case "done", "error", "stopped":
		return true
	default:
		return false
	}
}

// taskWaitInput is task_wait's argument shape.
type taskWaitInput struct {
	RunID          string `json:"runId" jsonschema:"id of the run to wait on (returned by run.start/task.prompt)"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" jsonschema:"how long to wait before returning timedOut: true (default 120s, always caller-overridable); a timeout is not a failure -- re-issue task_wait with the same runId to keep waiting"`
}

// taskWaitOutput is task_wait's structured output: the run's status once it
// stopped waiting, for one of three reasons -- terminal status reached,
// permission pending, or the timeout elapsed (timedOut).
type taskWaitOutput struct {
	RunID             string                `json:"runId"`
	Status            string                `json:"status"`
	StopReason        string                `json:"stopReason,omitempty"`
	Err               string                `json:"err,omitempty"`
	PendingPermission *pendingPermissionOut `json:"pendingPermission,omitempty"`
	TimedOut          bool                  `json:"timedOut"`
	// State is the unambiguous summary of why task_wait returned:
	// waiting_permission | running | done | error | stopped | timed_out.
	// Only done/error/stopped mean the run ended.
	State string `json:"state"`
	// PendingPermissions lists every pending request, in order
	// (PendingPermission is just the first).
	PendingPermissions []pendingPermissionOut `json:"pendingPermissions,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

// buildTaskWaitOutput derives task_wait's output from one run.logs
// snapshot. timedOut marks the deadline having passed with the run still
// going.
func buildTaskWaitOutput(result runLogsResult, timedOut bool) taskWaitOutput {
	out := taskWaitOutput{RunID: result.RunID, Status: result.Status, StopReason: result.StopReason, Err: result.Err}
	if pending := pendingPermissionsFrom(result); len(pending) > 0 {
		out.PendingPermission = &pending[0]
		out.PendingPermissions = pending
		out.State = "waiting_permission"
		out.Message = "Run " + result.RunID + " is waiting for a permission decision: a human must approve or deny this via `smind task approve <runId> [requestId] [optionId]` or the smind web UI. This is NOT completion; surface it to the human, then call task_wait again with the same runId."
		return out
	}
	switch {
	case isTerminalRunStatus(result.Status):
		out.State = result.Status
		if out.State != "done" && out.State != "stopped" {
			out.State = "error"
		}
	case timedOut:
		out.TimedOut = true
		out.State = "timed_out"
	default:
		out.State = "running"
	}
	return out
}

// mcpTaskWait implements ADR-0017's one genuinely new capability: block
// until runId reaches a terminal status, a permission request goes pending,
// or the timeout elapses -- client-side in this process via bounded,
// backed-off run.logs polling, not a new wsapi method (matching the
// reasoning that led to run.attach's own event-stream API rather than a
// run.wait RPC).
func mcpTaskWait(client *wsclient.Client) mcp.ToolHandlerFor[taskWaitInput, taskWaitOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskWaitInput) (*mcp.CallToolResult, taskWaitOutput, error) {
		timeout := mcpWaitDefaultTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
		}
		deadline := time.Now().Add(timeout)
		interval := mcpWaitPollMin

		for {
			result, err := mcpRunLogs(ctx, client, "task_wait", in.RunID, 0)
			if err != nil {
				return nil, taskWaitOutput{}, err
			}
			remaining := time.Until(deadline)
			if out := buildTaskWaitOutput(result, remaining <= 0); out.State != "running" {
				return nil, out, nil
			}
			sleep := interval
			if sleep > remaining {
				sleep = remaining
			}
			select {
			case <-ctx.Done():
				return nil, taskWaitOutput{}, ctx.Err()
			case <-time.After(sleep):
			}
			if interval *= 2; interval > mcpWaitPollMax {
				interval = mcpWaitPollMax
			}
		}
	}
}

// taskStopOutput is task_stop's structured output.
type taskStopOutput struct {
	RunID   string `json:"runId"`
	Stopped bool   `json:"stopped"`
}

// mcpTaskStop wraps run.stop: {runId} -> {runId, stopped: true} once the
// daemon has accepted the stop request.
func mcpTaskStop(client *wsclient.Client) mcp.ToolHandlerFor[runIDInput, taskStopOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runIDInput) (*mcp.CallToolResult, taskStopOutput, error) {
		if err := callWS(ctx, client, "task_stop", "run.stop", map[string]any{"runId": in.RunID}, nil); err != nil {
			return nil, taskStopOutput{}, err
		}
		return nil, taskStopOutput{RunID: in.RunID, Stopped: true}, nil
	}
}
