package main

import (
	"context"
	"strings"
	"testing"
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
	out := buildTaskWaitOutput(result, false)
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
