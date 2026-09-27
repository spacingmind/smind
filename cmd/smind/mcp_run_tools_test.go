package main

import (
	"context"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// TestMCPTools_RunReadToolsAgainstFinishedRun proves task_status,
// task_logs, and task_permissions round-trip against a real (fake-agent)
// run that completed normally -- the plan's per-tool happy path. The run
// is started via the daemon's own run.start RPC (task_send is a later
// step) and polled to completion through a direct wsclient, so the MCP
// assertions below read a genuinely finished run.
func TestMCPTools_RunReadToolsAgainstFinishedRun(t *testing.T) {
	cs := newMCPSession(t)
	client := dialTestClient(t, daemonURLForTest(t))

	runID := startTestRun(t, daemonURLForTest(t), "glm")
	waitForRunStatus(t, client, runID, "done")

	// task_status: a snapshot with terminal status and no pending
	// permission.
	var status taskStatusOutput
	if isErr := callMCPTool(t, cs, "task_status", map[string]any{"runId": runID}, &status); isErr {
		t.Fatal("task_status returned an error result")
	}
	if status.RunID != runID || status.Status != "done" {
		t.Fatalf("task_status = %+v, want runId %s done", status, runID)
	}
	if status.PendingPermission != nil {
		t.Fatalf("task_status pendingPermission = %+v, want none", status.PendingPermission)
	}
	if len(status.Events) == 0 {
		t.Fatal("task_status events = [], want the transcript snapshot")
	}

	// task_logs: the same transcript in full.
	var logs struct {
		RunID  string        `json:"runId"`
		Status string        `json:"status"`
		Events []runLogEvent `json:"events"`
	}
	if isErr := callMCPTool(t, cs, "task_logs", map[string]any{"runId": runID}, &logs); isErr {
		t.Fatal("task_logs returned an error result")
	}
	if len(logs.Events) != len(status.Events) {
		t.Fatalf("task_logs = %d events, task_status = %d, want the same finished transcript", len(logs.Events), len(status.Events))
	}

	// task_permissions: read-only, empty on a finished run.
	var perms struct {
		Pending []pendingPermissionOut `json:"pending"`
	}
	if isErr := callMCPTool(t, cs, "task_permissions", map[string]any{"runId": runID}, &perms); isErr {
		t.Fatal("task_permissions returned an error result")
	}
	if len(perms.Pending) != 0 {
		t.Fatalf("task_permissions pending = %+v, want none", perms.Pending)
	}
}

// TestMCPTools_TaskPermissionsShowsPendingPermission proves the
// permission read path: a fake-agent "permission" run (approval policy
// manual) raises a real pending permission request; task_permissions
// lists it and task_status surfaces it as pendingPermission.
func TestMCPTools_TaskPermissionsShowsPendingPermission(t *testing.T) {
	cs := newMCPSession(t)
	client := dialTestClient(t, daemonURLForTest(t))

	runID := startTestRun(t, daemonURLForTest(t), "glm", "permission")
	waitForPendingPermission(t, client, runID)

	var perms struct {
		Pending []pendingPermissionOut `json:"pending"`
	}
	if isErr := callMCPTool(t, cs, "task_permissions", map[string]any{"runId": runID}, &perms); isErr {
		t.Fatal("task_permissions returned an error result")
	}
	if len(perms.Pending) != 1 {
		t.Fatalf("task_permissions pending = %+v, want exactly one", perms.Pending)
	}
	req := perms.Pending[0]
	if req.RequestID == "" || req.Summary == "" || len(req.Options) == 0 {
		t.Fatalf("pending permission = %+v, want id/summary/options", req)
	}

	var status taskStatusOutput
	callMCPTool(t, cs, "task_status", map[string]any{"runId": runID}, &status)
	if status.PendingPermission == nil || status.PendingPermission.RequestID != req.RequestID {
		t.Fatalf("task_status pendingPermission = %+v, want request %s", status.PendingPermission, req.RequestID)
	}

	// Leave no run blocked on a decision behind us.
	_ = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": req.RequestID, "optionId": firstDenyOption(req.Options),
	}, nil)
}

// firstDenyOption picks the first non-allow option (a deny), for cleaning
// up a pending permission at the end of a test.
func firstDenyOption(opts []permissionOptionParams) string {
	for _, o := range opts {
		if o.Kind != "allow_once" && o.Kind != "allow_always" {
			return o.ID
		}
	}
	return opts[0].ID
}

// waitForRunStatus polls run.logs (bounded, with backoff) until the run
// reaches want, failing the test on timeout instead of hanging.
func waitForRunStatus(t *testing.T, client *wsclient.Client, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var logs struct {
			Status string `json:"status"`
		}
		if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": runID}, &logs); err == nil && logs.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never reached status %q", runID, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForPendingPermission polls run.logs until a permission_request
// without a matching permission_resolved appears.
func waitForPendingPermission(t *testing.T, client *wsclient.Client, runID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var result runLogsResult
		if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": runID}, &result); err == nil {
			if pending, _ := pendingPermissionsFrom(result); len(pending) > 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never raised a pending permission", runID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
