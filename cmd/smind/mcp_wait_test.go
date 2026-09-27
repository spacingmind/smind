package main

import (
	"context"
	"testing"
	"time"
)

// Tests for task_wait (docs/plans/active/mcp-server.md step 4). Runs are
// started via run.start directly (task_send is a later, out-of-scope step
// -- ADR-0019), driven through the fake-agent-backed test daemon
// newMCPSession/startTestRun already set up for the read-only tools.

// TestMCPTools_TaskWait_HappyPath proves the plan's happy-path scenario:
// task_wait on a run that finishes normally returns once it reaches "done",
// with no pendingPermission and timedOut false, and task_logs afterward
// shows the same transcript task_wait's last poll observed.
func TestMCPTools_TaskWait_HappyPath(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm")

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 5}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.RunID != runID || out.Status != "done" {
		t.Fatalf("task_wait = %+v, want runId %s done", out, runID)
	}
	if out.TimedOut {
		t.Fatal("task_wait timedOut = true on a run that finished within the timeout")
	}
	if out.PendingPermission != nil {
		t.Fatalf("task_wait pendingPermission = %+v, want none", out.PendingPermission)
	}

	var logs taskLogsOutput
	if isErr := callMCPTool(t, env.cs, "task_logs", map[string]any{"runId": runID}, &logs); isErr {
		t.Fatal("task_logs returned an error result")
	}
	if logs.Status != "done" || len(logs.Events) == 0 {
		t.Fatalf("task_logs after task_wait = %+v, want a finished transcript", logs)
	}
}

// TestMCPTools_TaskWait_EarlyReturnOnPendingPermission proves task_wait
// returns as soon as a permission request goes pending -- well before the
// run reaches a terminal status -- with pendingPermission populated, and
// that task_permissions on the same runId shows the same request.
func TestMCPTools_TaskWait_EarlyReturnOnPendingPermission(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "permission")

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 10}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if elapsed := time.Since(start); elapsed >= 10*time.Second {
		t.Fatalf("task_wait took %s, want it to return early on the pending permission, not the full timeout", elapsed)
	}
	if out.TimedOut {
		t.Fatal("task_wait timedOut = true, want an early return on the pending permission")
	}
	if out.PendingPermission == nil {
		t.Fatal("task_wait pendingPermission = nil, want the run's pending request")
	}
	if isTerminalRunStatus(out.Status) {
		t.Fatalf("task_wait status = %q, want a non-terminal status (the run is blocked on a decision)", out.Status)
	}

	var perms taskPermissionsOutput
	if isErr := callMCPTool(t, env.cs, "task_permissions", map[string]any{"runId": runID}, &perms); isErr {
		t.Fatal("task_permissions returned an error result")
	}
	if len(perms.Pending) != 1 || perms.Pending[0].RequestID != out.PendingPermission.RequestID {
		t.Fatalf("task_permissions = %+v, want the same request %q task_wait saw", perms.Pending, out.PendingPermission.RequestID)
	}

	// Leave no run blocked on a decision behind us.
	client := dialTestClient(t, env.srvURL)
	_ = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": out.PendingPermission.RequestID, "optionId": firstDenyOption(out.PendingPermission.Options),
	}, nil)
}

// TestMCPTools_TaskWait_Timeout proves task_wait's timeout is not an error:
// on a run that never finishes within the requested window (the fake
// agent's "hang" scenario streams one chunk then blocks forever), task_wait
// returns {timedOut: true} with no error, bounded by the short timeout the
// test itself requests -- never the tool's real 120s default.
func TestMCPTools_TaskWait_Timeout(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 1}, &out); isErr {
		t.Fatal("task_wait returned an error result, want a plain timedOut result")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("task_wait took %s, want it bounded by the 1s timeout requested", elapsed)
	}
	if !out.TimedOut {
		t.Fatalf("task_wait = %+v, want timedOut true on a run that never finishes", out)
	}
	if out.RunID != runID {
		t.Fatalf("task_wait runId = %q, want %q", out.RunID, runID)
	}
	if isTerminalRunStatus(out.Status) {
		t.Fatalf("task_wait status = %q on timeout, want the run still non-terminal", out.Status)
	}

	// Leave no hung run behind us.
	client := dialTestClient(t, env.srvURL)
	_ = client.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil)
}
