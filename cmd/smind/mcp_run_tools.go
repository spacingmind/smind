package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/runs/failure"
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
	mcp.AddTool(srv, &mcp.Tool{Name: "task_status", Description: "Non-blocking snapshot of a run: status, the last few transcript entries, and any still-pending permission request. A pending permission means the run is NOT finished: loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again (pass ignorePermissionIds to skip requests you already surfaced); only done/error/stopped mean the run ended. A done run whose `disposition` is \"needs-continue\" hit a turn/token/budget cap and is NOT finished: send \"continue\". Includes the run's git provenance (baseSha, headSha, dirty) when known. A failed run with failureKind rate_limited hit a provider quota: resetAt says when it resets; retry on another provider or after resetAt, do not retry immediately."}, mcpTaskStatus(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_logs", Description: "Read a run's full (or tailed) transcript and current status."}, mcpTaskLogs(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_permissions", Description: "List a run's still-pending permission requests (read-only). The human approves or denies them via `smind task approve` or the sMind web UI; there is no tool for that here. Each request carries a `detail` for writes (path, size change, replacesExisting, largeShrink, diff or preview): surface it to the human as is. loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again (pass ignorePermissionIds to skip requests you already surfaced); only done/error/stopped mean the run ended."}, mcpTaskPermissions(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_wait", Description: "With host, one bounded long poll (max 25s) on a run of a peer host: re-issue it with the returned nextCursor until done; waiting_permission there means a HUMAN on that host approves, you cannot. Otherwise: block until a run finishes (done/error/stopped), a permission request goes pending, or the timeout elapses (default 120s). Check `state`: loop: task_wait; if state is waiting_permission, a HUMAN approves (you cannot); then task_wait again (pass ignorePermissionIds to skip requests you already surfaced); only done/error/stopped mean the run ended; a done run with `disposition` \"needs-continue\" hit a turn/token/budget cap and is NOT finished (send \"continue\"). A human approves or denies a pending permission out of band (sMind web UI or `smind task approve`); to keep waiting after surfacing it, re-issue task_wait with ignorePermissionIds listing the request ids you have already surfaced -- the wait then ends only on a NEW pending request, a terminal status, or the timeout. Pass runIds (2-16 distinct runs, instead of runId) to wait for the first of several: it returns as soon as any one is done/error/stopped or has a pending permission, and runId in the result names which. A timeout is not a failure -- timedOut: true just means the run is still going; re-issue task_wait with the same runId to keep waiting, or use task_status for a non-blocking check. A failed run with failureKind rate_limited hit a provider quota: resetAt says when it resets; retry on another provider or after resetAt, do not retry immediately."}, mcpTaskWait(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "task_stop", Description: "Stop a running run. With host (and the remote taskId) it stops a run on a peer host."}, mcpTaskStop(client))
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
	Detail     *permissionDetail        `json:"detail,omitempty"`
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
			Kind: e.Kind, Payload: rawJSONAny(e.Payload), Detail: e.Detail,
		})
	}
	return out
}

// taskStatusOutput is task_status's structured output.
type taskStatusOutput struct {
	RunID             string                `json:"runId"`
	Status            string                `json:"status"`
	StopReason        string                `json:"stopReason,omitempty"`
	StopNote          string                `json:"stopNote,omitempty"`
	Disposition       string                `json:"disposition,omitempty" jsonschema:"\"needs-continue\" when the run ended done at a turn, token or budget cap: it is NOT finished (send a follow-up such as \"continue\"); absent otherwise. Not a status: status stays done"`
	Err               string                `json:"err,omitempty"`
	FailureKind       string                `json:"failureKind,omitempty" jsonschema:"typed terminal reason of an error run: rate_limited means the run hit a provider quota (retry on another provider or after resetAt, not immediately); absent otherwise"`
	Window            string                `json:"window,omitempty" jsonschema:"for rate_limited: the quota window that was hit, e.g. 5h or weekly; absent when unknown"`
	ResetAt           string                `json:"resetAt,omitempty" jsonschema:"for rate_limited: when the quota resets, RFC 3339 UTC; absent when unknown"`
	ResetInferred     bool                  `json:"resetInferred,omitempty" jsonschema:"true when resetAt was read in an assumed zone (approximate; show with ≈); absent for a stated reset"`
	Action            *failure.Action       `json:"action,omitempty" jsonschema:"the concrete next step for a failed run: kind (sign_in, wait_until, switch_profile, pick_model, auto_retry, retry_now, continue), target and a one-line label; route on failureKind plus action.kind"`
	BaseSHA           string                `json:"baseSha,omitempty" jsonschema:"full commit SHA of the run's worktree when it started; omitted when unknown"`
	HeadSHA           string                `json:"headSha,omitempty" jsonschema:"full commit SHA of the run's worktree at its latest capture; omitted when unknown"`
	Dirty             *bool                 `json:"dirty,omitempty" jsonschema:"whether the run's worktree had uncommitted changes at its latest capture; omitted when unknown"`
	FallbackProfileID int64                 `json:"fallbackProfileId,omitempty" jsonschema:"for a rate_limited run: the profile its author named as the fallback; start the work again with task_send profileId set to it on a NEW chat (chat_new), because a chat is bound to its provider"`
	Events            []mcpRunEvent         `json:"events"`
	PendingPermission *pendingPermissionOut `json:"pendingPermission,omitempty"`
	Host              string                `json:"host,omitempty" jsonschema:"set for a host read: the peer host's alias"`
	GlobalID          string                `json:"globalId,omitempty" jsonschema:"set for a host read when the task is known: <alias>:<taskId>"`
	CostUSD           *float64              `json:"costUsd,omitempty" jsonschema:"set for a host read: the run's cost in USD when the peer reports one"`
	StatusUnknown     bool                  `json:"statusUnknown,omitempty" jsonschema:"set for a host read that could not observe the run: the run is NOT finished, its status is unknown"`
	Note              string                `json:"note,omitempty" jsonschema:"set for a host read with status_unknown: what to do next"`
}

// taskStatusInput is task_status's argument shape: runId, plus the optional
// host-read fields.
type taskStatusInput struct {
	RunID  string `json:"runId" jsonschema:"id of the run (returned by task_send)"`
	Host   any    `json:"host,omitempty" jsonschema:"optional connected host: an alias or id string (see list_hosts), OR a label selector {'labels':{'os':'windows'},'any':false}: it must match exactly one enabled, connected host (zero or several matches are an error naming the candidates) unless any is true, which picks the host with the fewest live remote agents (ties by alias). There is no automatic retry on another host. Read that host instead of this daemon. Direct reads only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon"`
	TaskID int64  `json:"taskId,omitempty" jsonschema:"only with host: the peer's task id (the run id may be omitted to read the task's latest run)"`
}

// mcpTaskStatus wraps run.logs as a non-blocking snapshot: status, the
// last few transcript entries, and any still-pending permission request.
// For an orchestrator that wants to poll cheaply rather than block (the
// blocking shape, task_wait, is a later step of the plan).
func mcpTaskStatus(client *wsclient.Client) mcp.ToolHandlerFor[taskStatusInput, taskStatusOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskStatusInput) (*mcp.CallToolResult, taskStatusOutput, error) {
		if mcpHostSet(in.Host) {
			out, err := remoteTaskStatus(ctx, client, in)
			return nil, out, err
		}
		result, err := mcpRunLogs(ctx, client, "task_status", in.RunID, 0)
		if err != nil {
			return nil, taskStatusOutput{}, err
		}
		return nil, buildTaskStatusOutput(result), nil
	}
}

// buildTaskStatusOutput derives task_status's output from one run.logs
// snapshot: status, the stop note, the trailing events and the first
// still-pending permission.
func buildTaskStatusOutput(result runLogsResult) taskStatusOutput {
	out := taskStatusOutput{
		RunID:         result.RunID,
		Status:        result.Status,
		StopReason:    result.StopReason,
		StopNote:      stopNote(result.StopReason),
		Disposition:   result.Disposition,
		Err:           result.Err,
		FailureKind:   result.FailureKind,
		Window:        result.Window,
		ResetAt:       result.ResetAt,
		ResetInferred: result.ResetInferred, Action: result.Action,
		BaseSHA: result.BaseSHA,
		HeadSHA: result.HeadSHA,
		Dirty:   result.Dirty,

		FallbackProfileID: result.FallbackProfileID,
	}
	events := result.Events
	if len(events) > mcpStatusTail {
		events = events[len(events)-mcpStatusTail:]
	}
	out.Events = toMCPRunEvents(events)
	if pending := pendingPermissionsFrom(result); len(pending) > 0 {
		out.PendingPermission = &pending[0]
	}
	return out
}

// taskLogsInput is task_logs's argument shape (wrapping run.logs).
type taskLogsInput struct {
	RunID string `json:"runId" jsonschema:"id of the run whose transcript to read"`
	Tail  int    `json:"tail,omitempty" jsonschema:"return only the last N entries (default: all)"`
	Host  any    `json:"host,omitempty" jsonschema:"optional connected host: an alias or id string (see list_hosts), OR a label selector {'labels':{'os':'windows'},'any':false}: it must match exactly one enabled, connected host (zero or several matches are an error naming the candidates) unless any is true, which picks the host with the fewest live remote agents (ties by alias). There is no automatic retry on another host. Read that host instead of this daemon. Direct reads only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon"`
	// TaskID and Cursor apply to host reads only.
	TaskID int64 `json:"taskId,omitempty" jsonschema:"REQUIRED with host: the peer's task id (the peer lists activity per task)"`
	Cursor int64 `json:"cursor,omitempty" jsonschema:"only with host: continue the peer's activity from this cursor"`
}

// taskLogsOutput is task_logs's structured output: the run's current
// status plus its full (or tailed) transcript. events is empty rather
// than null for an eventless run.
type taskLogsOutput struct {
	RunID      string `json:"runId"`
	Status     string `json:"status"`
	StopReason string `json:"stopReason,omitempty"`
	StopNote   string `json:"stopNote,omitempty"`
	// Disposition is "needs-continue" for a run cut off at a cap; see taskStatusOutput.
	Disposition   string          `json:"disposition,omitempty"`
	Err           string          `json:"err,omitempty"`
	FailureKind   string          `json:"failureKind,omitempty" jsonschema:"typed terminal reason of an error run: rate_limited means the run hit a provider quota (retry on another provider or after resetAt, not immediately); absent otherwise"`
	Window        string          `json:"window,omitempty" jsonschema:"for rate_limited: the quota window that was hit, e.g. 5h or weekly; absent when unknown"`
	ResetAt       string          `json:"resetAt,omitempty" jsonschema:"for rate_limited: when the quota resets, RFC 3339 UTC; absent when unknown"`
	ResetInferred bool            `json:"resetInferred,omitempty" jsonschema:"true when resetAt was read in an assumed zone (approximate; show with ≈); absent for a stated reset"`
	Action        *failure.Action `json:"action,omitempty" jsonschema:"the concrete next step for a failed run: kind (sign_in, wait_until, switch_profile, pick_model, auto_retry, retry_now, continue), target and a one-line label; route on failureKind plus action.kind"`
	Events        []mcpRunEvent   `json:"events"`
	// Host, NextCursor and HasMore are set for a host read only.
	Host       string `json:"host,omitempty"`
	NextCursor int64  `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore,omitempty"`
}

// mcpTaskLogs wraps run.logs: full or tailed transcript plus the run's
// current status, mirroring `smind task logs` (without --follow, which
// has no request/response MCP shape).
func mcpTaskLogs(client *wsclient.Client) mcp.ToolHandlerFor[taskLogsInput, taskLogsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskLogsInput) (*mcp.CallToolResult, taskLogsOutput, error) {
		if mcpHostSet(in.Host) {
			out, err := remoteTaskLogs(ctx, client, in)
			return nil, out, err
		}
		result, err := mcpRunLogs(ctx, client, "task_logs", in.RunID, in.Tail)
		if err != nil {
			return nil, taskLogsOutput{}, err
		}
		if result.Events == nil {
			result.Events = []runLogEvent{}
		}
		return nil, taskLogsOutput{
			RunID: result.RunID, Status: result.Status,
			StopReason: result.StopReason, StopNote: stopNote(result.StopReason), Disposition: result.Disposition, Err: result.Err,
			FailureKind: result.FailureKind, Window: result.Window, ResetAt: result.ResetAt, ResetInferred: result.ResetInferred, Action: result.Action, Events: toMCPRunEvents(result.Events),
		}, nil
	}
}

// pendingPermissionOut is one still-unresolved permission request, the
// MCP shape of the pendingPermission cmdTaskPermissions derives today.
type pendingPermissionOut struct {
	RequestID string                   `json:"requestId"`
	Summary   string                   `json:"summary"`
	Options   []permissionOptionParams `json:"options"`
	// Detail says what a write/edit/execute will do (ACP only), omitted when
	// the provider supplied none.
	Detail *permissionDetail `json:"detail,omitempty"`
}

// pendingPermissionsFrom derives the still-pending permission requests
// from a run.logs result -- every permission_request whose requestId never
// shows up on a later permission_resolved entry -- the same read-through
// logic fetchPendingPermissions (cmd/smind/task.go) implements for the
// CLI. Read-only by construction: nothing here can resolve a request.
func pendingPermissionsFrom(result runLogsResult) []pendingPermissionOut {
	// A finished run has nothing left to approve: a request left unresolved
	// by a stop or crash must not make a driver wait on a dead run.
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

// mcpWaitMaxRunIDs bounds task_wait's runIds (wait-any) list.
const mcpWaitMaxRunIDs = 16

// taskWaitInput is task_wait's argument shape: either runId (wait on one
// run), runIds (wait for the first of 2-16 runs), or childrenOf (wait for
// the first of a task's direct live children's running runs) -- never more
// than one of the three.
type taskWaitInput struct {
	RunID               string   `json:"runId,omitempty" jsonschema:"id of the run to wait on (returned by run.start/task.prompt); use this or runIds"`
	ChildrenOf          int64    `json:"childrenOf,omitempty" jsonschema:"taskId whose direct live children's running runs to wait for (resolved via the task tree); use this instead of runId/runIds"`
	RunIDs              []string `json:"runIds,omitempty" jsonschema:"wait-any: 2-16 distinct run ids; returns as soon as any one of them is terminal or has a pending permission request, naming it in runId. Use this or runId, not both"`
	IgnorePermissionIDs []string `json:"ignorePermissionIds,omitempty" jsonschema:"permission request ids you have already surfaced to the human; a pending request with one of these ids does not end the wait, so it continues until a NEW pending request, a terminal status, or the timeout"`
	TimeoutSeconds      int      `json:"timeoutSeconds,omitempty" jsonschema:"how long to wait before returning timedOut: true (default 120s, always caller-overridable; with host default and cap 25s); a timeout is not a failure -- re-issue task_wait with the same runId/runIds to keep waiting"`
	Host                any      `json:"host,omitempty" jsonschema:"optional connected host: an alias or id string (see list_hosts), OR a label selector {'labels':{'os':'windows'},'any':false}: it must match exactly one enabled, connected host (zero or several matches are an error naming the candidates) unless any is true, which picks the host with the fewest live remote agents (ties by alias). There is no automatic retry on another host. Act on that host instead of this daemon. Direct only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon. With host, runId is required, timeoutSeconds defaults to and is capped at 25, and runIds, childrenOf and ignorePermissionIds are not supported"`
	Cursor              int64    `json:"cursor,omitempty" jsonschema:"only with host: continue the remote run's events from this cursor (the nextCursor of the previous wait)"`
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
	FailureKind       string                `json:"failureKind,omitempty" jsonschema:"typed terminal reason of an error run: rate_limited means the run hit a provider quota (retry on another provider or after resetAt, not immediately); absent otherwise"`
	Window            string                `json:"window,omitempty" jsonschema:"for rate_limited: the quota window that was hit, e.g. 5h or weekly; absent when unknown"`
	ResetAt           string                `json:"resetAt,omitempty" jsonschema:"for rate_limited: when the quota resets, RFC 3339 UTC; absent when unknown"`
	ResetInferred     bool                  `json:"resetInferred,omitempty" jsonschema:"true when resetAt was read in an assumed zone (approximate; show with ≈); absent for a stated reset"`
	Action            *failure.Action       `json:"action,omitempty" jsonschema:"the concrete next step for a failed run: kind (sign_in, wait_until, switch_profile, pick_model, auto_retry, retry_now, continue), target and a one-line label; route on failureKind plus action.kind"`
	PendingPermission *pendingPermissionOut `json:"pendingPermission,omitempty"`
	TimedOut          bool                  `json:"timedOut"`
	StopNote          string                `json:"stopNote,omitempty"`
	// Disposition is "needs-continue" when the run ended done at a turn,
	// token or budget cap: state is still "done" (no new status), but the
	// work is not finished. It is also set on an error run that is resumable
	// (cli_exited, or rate_limited past its reset, with a stored session).
	// Absent otherwise.
	Disposition string `json:"disposition,omitempty"`
	// State is the unambiguous summary of why task_wait returned:
	// waiting_permission | running | done | error | stopped | timed_out.
	// Only done/error/stopped mean the run ended.
	State string `json:"state"`
	// PendingPermissions lists every pending request that ended the wait
	// (not ignored via ignorePermissionIds), in order; PendingPermission is
	// just the first.
	PendingPermissions []pendingPermissionOut `json:"pendingPermissions,omitempty"`
	Message            string                 `json:"message,omitempty"`
	FallbackProfileID  int64                  `json:"fallbackProfileId,omitempty" jsonschema:"for a rate_limited run: the profile its author named as the fallback; start the work again with task_send profileId set to it on a NEW chat (chat_new), because a chat is bound to its provider"`
	// Set for a host wait only; Events/NextCursor let the next wait resume.
	Host              string        `json:"host,omitempty"`
	NextCursor        int64         `json:"nextCursor,omitempty"`
	Done              bool          `json:"done,omitempty"`
	WaitingPermission bool          `json:"waitingPermission,omitempty"`
	Events            []mcpRunEvent `json:"events,omitempty"`
}

// buildTaskWaitOutput derives task_wait's output from one run.logs snapshot.
// ignore holds request ids the caller already surfaced (they do not count as
// waiting); timedOut marks the deadline having passed with the run going.
func buildTaskWaitOutput(result runLogsResult, ignore map[string]bool, timedOut bool) taskWaitOutput {
	out := taskWaitOutput{RunID: result.RunID, Status: result.Status, StopReason: result.StopReason, StopNote: stopNote(result.StopReason), Disposition: result.Disposition, Err: result.Err,
		FailureKind: result.FailureKind, Window: result.Window, ResetAt: result.ResetAt, ResetInferred: result.ResetInferred, Action: result.Action, FallbackProfileID: result.FallbackProfileID}
	var fresh []pendingPermissionOut
	for _, p := range pendingPermissionsFrom(result) {
		if !ignore[p.RequestID] {
			fresh = append(fresh, p)
		}
	}
	if len(fresh) > 0 {
		out.PendingPermission = &fresh[0]
		out.PendingPermissions = fresh
		out.State = "waiting_permission"
		out.Message = "Run " + result.RunID + " is waiting for a permission decision: a human must approve or deny this via `smind task approve` / `smind task deny` or the sMind web UI. This is NOT completion; surface it to the human, then call task_wait again with the same runId (ignorePermissionIds listing the request ids you surfaced)."
		return out
	}
	switch {
	case isTerminalRunStatus(result.Status):
		out.State = result.Status
		if out.State != "done" && out.State != "stopped" {
			out.State = "error"
		}
		if result.Disposition == "needs-continue" && result.Status == "error" {
			out.Message = "Run " + result.RunID + " failed in a resumable way (" + result.FailureKind + "): the agent's session is stored. Send a follow-up prompt such as \"Continue from where you left off\" with task_send on the same task (and the run's chatId from run_list if it is not the default chat) to resume it."
		} else if result.Disposition == "needs-continue" {
			out.Message = "Run " + result.RunID + " ended at a cap (" + result.StopReason + "): its status is done but the work is NOT finished. Send a follow-up prompt such as \"continue\" with task_send on the same task (and the run's chatId from run_list if it is not the default chat) to resume it; do not treat this as complete."
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
// run.wait RPC). With runIds it waits for the first of several runs.
func mcpTaskWait(client *wsclient.Client) mcp.ToolHandlerFor[taskWaitInput, taskWaitOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskWaitInput) (*mcp.CallToolResult, taskWaitOutput, error) {
		if mcpHostSet(in.Host) {
			out, err := remoteTaskWait(ctx, client, in)
			return nil, out, err
		}
		if in.ChildrenOf != 0 {
			if in.RunID != "" || len(in.RunIDs) > 0 {
				return nil, taskWaitOutput{}, fmt.Errorf("task_wait: childrenOf is exclusive: do not pass runId or runIds with it")
			}
			out, err := taskWaitChildrenOf(ctx, client, in)
			return nil, out, err
		}
		timeout := mcpWaitDefaultTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
		}
		deadline := time.Now().Add(timeout)
		ignore := make(map[string]bool, len(in.IgnorePermissionIDs))
		for _, id := range in.IgnorePermissionIDs {
			ignore[id] = true
		}

		if len(in.RunIDs) == 0 {
			out, err := mcpWaitOneRun(ctx, client, in.RunID, deadline, ignore)
			return nil, out, err
		}
		if in.RunID != "" {
			return nil, taskWaitOutput{}, fmt.Errorf("task_wait: pass runId or runIds, not both")
		}
		if err := validateWaitRunIDs(in.RunIDs); err != nil {
			return nil, taskWaitOutput{}, err
		}
		out, err := mcpWaitAnyRun(ctx, client, in.RunIDs, deadline, ignore)
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
func mcpWaitAnyRun(ctx context.Context, client *wsclient.Client, runIDs []string, deadline time.Time, ignore map[string]bool) (taskWaitOutput, error) {
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
			out, err := mcpWaitOneRun(ctx, client, id, deadline, ignore)
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
	return taskWaitOutput{TimedOut: true, State: "timed_out"}, nil
}

// mcpWaitOneRun is the per-run wait: poll run.logs with backoff until the
// run is terminal, has a pending permission request, or deadline passes
// (TimedOut).
func mcpWaitOneRun(ctx context.Context, client *wsclient.Client, runID string, deadline time.Time, ignore map[string]bool) (taskWaitOutput, error) {
	interval := mcpWaitPollMin
	for {
		result, err := mcpRunLogs(ctx, client, "task_wait", runID, 0)
		if err != nil {
			return taskWaitOutput{}, err
		}
		remaining := time.Until(deadline)
		if out := buildTaskWaitOutput(result, ignore, remaining <= 0); out.State != "running" {
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

// taskStopInput is task_stop's argument shape: runId, plus the optional host
// fields. runId is checked in the handler because host mode may omit it.
type taskStopInput struct {
	RunID  string `json:"runId,omitempty" jsonschema:"id of the run (returned by task_send); required without host"`
	Host   any    `json:"host,omitempty" jsonschema:"optional connected host: an alias or id string (see list_hosts), OR a label selector {'labels':{'os':'windows'},'any':false}: it must match exactly one enabled, connected host (zero or several matches are an error naming the candidates) unless any is true, which picks the host with the fewest live remote agents (ties by alias). There is no automatic retry on another host. Act on that host instead of this daemon. Direct only (hop limit 1): a task created by a peer cannot use it. Omit for this daemon"`
	TaskID int64  `json:"taskId,omitempty" jsonschema:"only with host: the peer's task id (required with host; runId may be omitted to stop the task's running run)"`
}

// taskStopOutput is task_stop's structured output.
type taskStopOutput struct {
	RunID   string `json:"runId"`
	Stopped bool   `json:"stopped"`
	// Set for a host stop only.
	Host     string `json:"host,omitempty"`
	GlobalID string `json:"globalId,omitempty"`
}

// mcpTaskStop wraps run.stop: {runId} -> {runId, stopped: true} once the
// daemon has accepted the stop request.
func mcpTaskStop(client *wsclient.Client) mcp.ToolHandlerFor[taskStopInput, taskStopOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskStopInput) (*mcp.CallToolResult, taskStopOutput, error) {
		if mcpHostSet(in.Host) {
			out, err := remoteTaskStop(ctx, client, in)
			return nil, out, err
		}
		if in.RunID == "" {
			return nil, taskStopOutput{}, fmt.Errorf("task_stop: runId is required")
		}
		if err := callWS(ctx, client, "task_stop", "run.stop", map[string]any{"runId": in.RunID}, nil); err != nil {
			return nil, taskStopOutput{}, err
		}
		return nil, taskStopOutput{RunID: in.RunID, Stopped: true}, nil
	}
}
