package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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
	mcp.AddTool(srv, &mcp.Tool{Name: "task_status", Description: "Non-blocking snapshot of a run: status, the last few transcript entries, and any still-pending permission request."}, mcpTaskStatus(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_logs", Description: "Read a run's full (or tailed) transcript and current status."}, mcpTaskLogs(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_permissions", Description: "List a run's still-pending permission requests (read-only). The human approves or denies them via `smind task approve` or the smind web UI; there is no tool for that here."}, mcpTaskPermissions(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_wait", Description: "Block until a run finishes (done/error/stopped), a permission request goes pending, or the timeout elapses (default 120s). Pass runIds (2-16 distinct runs, instead of runId) to wait for the first of several: it returns as soon as any one is done/error/stopped or has a pending permission, and runId in the result names which. A timeout is not a failure -- timedOut: true just means the run is still going; re-issue task_wait with the same runId to keep waiting, or use task_status for a non-blocking check."}, mcpTaskWait(client))
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

// mcpRunEvent is runLogEvent flattened for MCP output. The tool-call
// input/result and raw payload are arbitrary JSON, so they are declared
// `any`: the SDK infers an array schema for json.RawMessage ([]byte) and
// would reject the object-shaped values at output validation.
type mcpRunEvent struct {
	Type       string                   `json:"type"`
	Text       string                   `json:"text,omitempty"`
	StopReason string                   `json:"stopReason,omitempty"`
	RequestID  string                   `json:"requestId,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Options    []permissionOptionParams `json:"options,omitempty"`
	OptionID   string                   `json:"optionId,omitempty"`
	Reason     string                   `json:"reason,omitempty"`
	ToolCallID string                   `json:"toolCallId,omitempty"`
	ToolName   string                   `json:"toolName,omitempty"`
	Title      string                   `json:"title,omitempty"`
	Status     string                   `json:"status,omitempty"`
	Input      any                      `json:"input,omitempty"`
	Result     any                      `json:"result,omitempty"`
	Kind       string                   `json:"kind,omitempty"`
	Payload    any                      `json:"payload,omitempty"`
}

// rawJSONAny decodes raw into a generic value (nil when empty or invalid).
func rawJSONAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// toMCPRunEvents converts run.logs events to their MCP shape; never nil.
func toMCPRunEvents(events []runLogEvent) []mcpRunEvent {
	out := make([]mcpRunEvent, 0, len(events))
	for _, e := range events {
		out = append(out, mcpRunEvent{
			Type: e.Type, Text: e.Text, StopReason: e.StopReason, RequestID: e.RequestID,
			Summary: e.Summary, Options: e.Options, OptionID: e.OptionID, Reason: e.Reason,
			ToolCallID: e.ToolCallID, ToolName: e.ToolName, Title: e.Title, Status: e.Status,
			Input: rawJSONAny(e.Input), Result: rawJSONAny(e.Result),
			Kind: e.Kind, Payload: rawJSONAny(e.Payload),
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
		if result.Events == nil {
			result.Events = []runLogEvent{}
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
}

// pendingPermissionsFrom derives the still-pending permission requests
// from a run.logs result -- every permission_request whose requestId never
// shows up on a later permission_resolved entry -- the same read-through
// logic fetchPendingPermissions (cmd/smind/task.go) implements for the
// CLI. Read-only by construction: nothing here can resolve a request.
func pendingPermissionsFrom(result runLogsResult) []pendingPermissionOut {
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

// mcpWaitMaxRunIDs bounds task_wait's runIds (wait-any) list.
const mcpWaitMaxRunIDs = 16

// taskWaitInput is task_wait's argument shape: either runId (wait on one
// run) or runIds (wait for the first of 2-16 runs), never both.
type taskWaitInput struct {
	RunID          string   `json:"runId,omitempty" jsonschema:"id of the run to wait on (returned by run.start/task.prompt); use this or runIds"`
	RunIDs         []string `json:"runIds,omitempty" jsonschema:"wait-any: 2-16 distinct run ids; returns as soon as any one of them is terminal or has a pending permission request, naming it in runId. Use this or runId, not both"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty" jsonschema:"how long to wait before returning timedOut: true (default 120s, always caller-overridable); a timeout is not a failure -- re-issue task_wait with the same runId/runIds to keep waiting"`
}

// taskWaitOutput is task_wait's structured output: the run's status once it
// stopped waiting, for one of three reasons -- terminal status reached,
// permission pending, or the timeout elapsed (timedOut). For wait-any,
// runId names the run that triggered the return (empty on timeout).
type taskWaitOutput struct {
	RunID             string                `json:"runId"`
	Status            string                `json:"status"`
	StopReason        string                `json:"stopReason,omitempty"`
	Err               string                `json:"err,omitempty"`
	PendingPermission *pendingPermissionOut `json:"pendingPermission,omitempty"`
	TimedOut          bool                  `json:"timedOut"`
}

// mcpTaskWait implements ADR-0017's one genuinely new capability: block
// until runId reaches a terminal status, a permission request goes pending,
// or the timeout elapses -- client-side in this process via bounded,
// backed-off run.logs polling, not a new wsapi method (matching the
// reasoning that led to run.attach's own event-stream API rather than a
// run.wait RPC). With runIds it waits for the first of several runs.
func mcpTaskWait(client *wsclient.Client) mcp.ToolHandlerFor[taskWaitInput, taskWaitOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskWaitInput) (*mcp.CallToolResult, taskWaitOutput, error) {
		timeout := mcpWaitDefaultTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
		}
		deadline := time.Now().Add(timeout)

		if len(in.RunIDs) == 0 {
			out, err := mcpWaitOneRun(ctx, client, in.RunID, deadline)
			return nil, out, err
		}
		if in.RunID != "" {
			return nil, taskWaitOutput{}, fmt.Errorf("task_wait: pass runId or runIds, not both")
		}
		if err := validateWaitRunIDs(in.RunIDs); err != nil {
			return nil, taskWaitOutput{}, err
		}
		out, err := mcpWaitAnyRun(ctx, client, in.RunIDs, deadline)
		return nil, out, err
	}
}

// validateWaitRunIDs enforces wait-any's 2-16 distinct ids.
func validateWaitRunIDs(ids []string) error {
	if len(ids) < 2 || len(ids) > mcpWaitMaxRunIDs {
		return fmt.Errorf("task_wait: runIds must list 2-%d runs, got %d (use runId for a single run)", mcpWaitMaxRunIDs, len(ids))
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("task_wait: runIds must not contain an empty id")
		}
		if seen[id] {
			return fmt.Errorf("task_wait: runIds contains duplicate %q", id)
		}
		seen[id] = true
	}
	return nil
}

// mcpWaitAnyRun waits on every run concurrently (one goroutine each, over
// the shared per-run wait logic) and returns the first that is terminal or
// has a pending permission, cancelling and joining the rest before
// returning so no goroutine outlives the call. If every run times out the
// result is {timedOut: true} with no runId.
func mcpWaitAnyRun(ctx context.Context, client *wsclient.Client, runIDs []string, deadline time.Time) (taskWaitOutput, error) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	type result struct {
		out taskWaitOutput
		err error
	}
	results := make(chan result, len(runIDs))
	for _, id := range runIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := mcpWaitOneRun(ctx, client, id, deadline)
			results <- result{out, err}
		}()
	}
	for range runIDs {
		r := <-results
		if r.err != nil {
			return taskWaitOutput{}, r.err
		}
		if !r.out.TimedOut {
			return r.out, nil
		}
	}
	return taskWaitOutput{TimedOut: true}, nil
}

// mcpWaitOneRun is the per-run wait: poll run.logs with backoff until the
// run is terminal, has a pending permission request, or deadline passes
// (TimedOut).
func mcpWaitOneRun(ctx context.Context, client *wsclient.Client, runID string, deadline time.Time) (taskWaitOutput, error) {
	interval := mcpWaitPollMin
	for {
		result, err := mcpRunLogs(ctx, client, "task_wait", runID, 0)
		if err != nil {
			return taskWaitOutput{}, err
		}
		out := taskWaitOutput{RunID: result.RunID, Status: result.Status, StopReason: result.StopReason, Err: result.Err}
		if pending := pendingPermissionsFrom(result); len(pending) > 0 {
			out.PendingPermission = &pending[0]
			return out, nil
		}
		if isTerminalRunStatus(result.Status) {
			return out, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			out.TimedOut = true
			return out, nil
		}
		sleep := interval
		if sleep > remaining {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return taskWaitOutput{}, ctx.Err()
		case <-time.After(sleep):
		}
		if interval *= 2; interval > mcpWaitPollMax {
			interval = mcpWaitPollMax
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
