package main

import (
	"context"
	"testing"
	"time"
)

// assertIgnoredWaitTimesOut re-issues task_wait with the already-seen request
// id ignored and requires it to run to its (short) timeout instead of
// returning on the still-pending request.
func assertIgnoredWaitTimesOut(t *testing.T, env *mcpTestEnv, args map[string]any) {
	t.Helper()
	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", args, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if !out.TimedOut || out.PendingPermission != nil {
		t.Fatalf("task_wait = %+v, want timedOut with the ignored request skipped", out)
	}
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond {
		t.Fatalf("task_wait returned after %s, want the full ~2s timeout", elapsed)
	}
}

func TestMCPTools_TaskWaitIgnorePermissionIds(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "permission")
	stopTestRuns(t, env, runID)

	var first taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 10}, &first)
	if first.PendingPermission == nil {
		t.Fatalf("first task_wait = %+v, want a pending permission", first)
	}
	reqID := first.PendingPermission.RequestID

	assertIgnoredWaitTimesOut(t, env, map[string]any{
		"runId": runID, "timeoutSeconds": 2, "ignorePermissionIds": []string{reqID},
	})

	// Resolving the request (as the human would) lets the run finish; the
	// ignoring wait must then return on the terminal status.
	client := dialTestClient(t, env.srvURL)
	if err := client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": reqID, "optionId": firstDenyOption(first.PendingPermission.Options),
	}, nil); err != nil {
		t.Fatalf("respondPermission: %v", err)
	}
	var last taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{
		"runId": runID, "timeoutSeconds": 10, "ignorePermissionIds": []string{reqID},
	}, &last)
	if last.TimedOut || !isTerminalRunStatus(last.Status) {
		t.Fatalf("task_wait = %+v, want a terminal status after the request resolved", last)
	}
}

func TestMCPTools_TaskWaitAnyIgnorePermissionIds(t *testing.T) {
	env := newMCPSession(t)
	hung := startTestRun(t, env.srvURL, "glm", "hang")
	blocked := startTestRun(t, env.srvURL, "glm", "permission")
	stopTestRuns(t, env, hung, blocked)

	var first taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runIds": []string{hung, blocked}, "timeoutSeconds": 10}, &first)
	if first.RunID != blocked || first.PendingPermission == nil {
		t.Fatalf("first task_wait = %+v, want %s with a pending permission", first, blocked)
	}

	assertIgnoredWaitTimesOut(t, env, map[string]any{
		"runIds": []string{hung, blocked}, "timeoutSeconds": 2,
		"ignorePermissionIds": []string{first.PendingPermission.RequestID},
	})
}
