package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskWait_PendingPermissionReportsWaitingPermission(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "permission")

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 10}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.State != "waiting_permission" {
		t.Fatalf("state = %q, want waiting_permission", out.State)
	}
	if out.PendingPermission == nil || len(out.PendingPermissions) != 1 {
		t.Fatalf("pending = %+v / %+v, want exactly the one request", out.PendingPermission, out.PendingPermissions)
	}
	if out.TimedOut {
		t.Fatal("timedOut = true, want false")
	}
	for _, want := range []string{"human", "smind task approve", "NOT completion", "task_wait again"} {
		if !strings.Contains(out.Message, want) {
			t.Errorf("message %q missing %q", out.Message, want)
		}
	}

	client := dialTestClient(t, env.srvURL)
	_ = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": out.PendingPermission.RequestID, "optionId": firstDenyOption(out.PendingPermission.Options),
	}, nil)
}

func TestTaskWait_DoneReportsDoneState(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm")

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 5}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.State != "done" || out.Status != "done" || out.TimedOut || out.Message != "" {
		t.Fatalf("task_wait = %+v, want state done, no message, not timed out", out)
	}
}

func TestTaskWait_TimeoutReportsTimedOut(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 1}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.State != "timed_out" || !out.TimedOut {
		t.Fatalf("task_wait = %+v, want state timed_out with timedOut true", out)
	}

	client := dialTestClient(t, env.srvURL)
	_ = client.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil)
}

func TestTaskWait_ListsEveryPendingPermission(t *testing.T) {
	opts := []permissionOptionParams{{ID: "a", Kind: "allow_once"}, {ID: "d", Kind: "reject_once"}}
	result := runLogsResult{RunID: "r1", Status: "running", Events: []runLogEvent{
		{Type: "permission_request", RequestID: "req-1", Summary: "one", Options: opts},
		{Type: "permission_request", RequestID: "req-2", Summary: "two", Options: opts},
		{Type: "permission_request", RequestID: "req-3", Summary: "three", Options: opts},
		{Type: "permission_resolved", RequestID: "req-2", OptionID: "a"},
	}}
	out := buildTaskWaitOutput(result, nil, false)
	if out.State != "waiting_permission" {
		t.Fatalf("state = %q, want waiting_permission", out.State)
	}
	if len(out.PendingPermissions) != 2 || out.PendingPermissions[0].RequestID != "req-1" || out.PendingPermissions[1].RequestID != "req-3" {
		t.Fatalf("pendingPermissions = %+v, want req-1, req-3 in order", out.PendingPermissions)
	}
	if out.PendingPermission == nil || out.PendingPermission.RequestID != "req-1" {
		t.Fatalf("pendingPermission = %+v, want the first (req-1)", out.PendingPermission)
	}
}

func TestMCPToolDescriptions_TaskWaitLoopWording(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	seen := 0
	for _, tool := range res.Tools {
		switch tool.Name {
		case "task_wait", "task_status", "task_permissions":
			seen++
			for _, want := range []string{"waiting_permission", "task_wait again", "HUMAN approves"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("%s description missing %q: %s", tool.Name, want, tool.Description)
				}
			}
		}
	}
	if seen != 3 {
		t.Fatalf("saw %d of the 3 tools", seen)
	}
}

func staleRequestRun(status string) runLogsResult {
	return runLogsResult{RunID: "r1", Status: status, Events: []runLogEvent{
		{Type: "permission_request", RequestID: "req-1", Summary: "one", Options: []permissionOptionParams{{ID: "a", Kind: "allow_once"}}},
	}}
}

func TestTaskWait_TerminalRunWithStalePermissionIsNotWaiting(t *testing.T) {
	for _, status := range []string{"done", "error", "stopped"} {
		out := buildTaskWaitOutput(staleRequestRun(status), nil, false)
		if out.State != status {
			t.Errorf("status %s: state = %q, want %q", status, out.State, status)
		}
		if len(out.PendingPermissions) != 0 || out.PendingPermission != nil || out.Message != "" {
			t.Errorf("status %s: pending = %+v / %+v msg %q, want none", status, out.PendingPermission, out.PendingPermissions, out.Message)
		}
	}

	// End to end: stop a real run while its request is pending.
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "permission")
	var waiting taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 10}, &waiting)
	if waiting.State != "waiting_permission" {
		t.Fatalf("state = %q, want waiting_permission before the stop", waiting.State)
	}
	callMCPTool(t, env.cs, "task_stop", map[string]any{"runId": runID}, nil)
	client := dialTestClient(t, env.srvURL)
	waitForRunStatus(t, client, runID, "stopped")
	var out taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 5}, &out)
	if out.State != "stopped" || len(out.PendingPermissions) != 0 {
		t.Fatalf("task_wait after stop = %+v, want state stopped and no pending", out)
	}
}

func TestMCPTaskPermissions_FinishedRunWithStaleRequestIsEmpty(t *testing.T) {
	for _, status := range []string{"done", "error", "stopped"} {
		if got := pendingPermissionsFrom(staleRequestRun(status)); len(got) != 0 {
			t.Errorf("status %s: pending = %+v, want none", status, got)
		}
	}
	if got := pendingPermissionsFrom(staleRequestRun("running")); len(got) != 1 {
		t.Errorf("running: pending = %+v, want 1", got)
	}
}

// A pending request the caller already surfaced (ignorePermissionIds) does
// not make task_wait report waiting_permission: the run is still running.
func TestTaskWait_IgnoredPermissionDoesNotReportWaiting(t *testing.T) {
	result := runLogsResult{RunID: "r", Status: "running", Events: []runLogEvent{
		{Type: "permission_request", RequestID: "q1", Summary: "Write file x"},
	}}
	if out := buildTaskWaitOutput(result, map[string]bool{"q1": true}, false); out.State != "running" || out.PendingPermission != nil {
		t.Fatalf("ignored request: %+v, want running with no pending", out)
	}
	fresh := buildTaskWaitOutput(result, map[string]bool{"other": true}, false)
	if fresh.State != "waiting_permission" || len(fresh.PendingPermissions) != 1 {
		t.Fatalf("fresh request: %+v, want waiting_permission", fresh)
	}
}

// runLogsWithFallback is a run.logs payload for a rate-limited run whose
// profile names profile 7 as the fallback.
const runLogsWithFallback = `{"runId":"r1","status":"error","err":"429 usage limit","fallbackProfileId":7,"events":[]}`

func TestMCPTaskStatus_FallbackProfileID(t *testing.T) {
	var result runLogsResult
	if err := json.Unmarshal([]byte(runLogsWithFallback), &result); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(buildTaskStatusOutput(result))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["fallbackProfileId"] != float64(7) {
		t.Fatalf("task_status = %s, want fallbackProfileId 7", b)
	}
	wait := buildTaskWaitOutput(result, nil, false)
	if wait.FallbackProfileID != 7 || wait.State != "error" {
		t.Fatalf("task_wait = %+v", wait)
	}
	// Absent when the run has no suggestion.
	result.FallbackProfileID = 0
	b, _ = json.Marshal(buildTaskStatusOutput(result))
	if strings.Contains(string(b), "fallbackProfileId") {
		t.Fatalf("task_status = %s, want no fallbackProfileId", b)
	}
}

func TestTaskWait_PrintsFallbackHint(t *testing.T) {
	row := runSummaryRow{ID: "r1", Status: "error", StartedAt: time.Now().Add(-time.Minute), Err: "429"}
	got := renderWaitReport(row, "", time.Now(), 7)
	if !strings.Contains(got, "fallback: profile 7 (use: smind task chat new <task>, then task send --chat ...)") {
		t.Fatalf("report = %q, want the fallback hint", got)
	}
	if strings.Contains(renderWaitReport(row, "", time.Now(), 0), "fallback:") {
		t.Fatalf("hint printed without a fallback profile")
	}
	if waitExitCode(row) != 1 {
		t.Fatalf("waitExitCode changed")
	}
}
