package main

import (
	"context"
	"encoding/json"
	"strings"
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
	env := newMCPSession(t)
	cs := env.cs
	client := dialTestClient(t, env.srvURL)

	runID := startTestRun(t, env.srvURL, "glm")
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
	var logs taskLogsOutput
	if isErr := callMCPTool(t, cs, "task_logs", map[string]any{"runId": runID}, &logs); isErr {
		t.Fatal("task_logs returned an error result")
	}
	if len(logs.Events) != len(status.Events) {
		t.Fatalf("task_logs = %d events, task_status = %d, want the same finished transcript", len(logs.Events), len(status.Events))
	}

	// task_permissions: read-only, empty on a finished run.
	var perms taskPermissionsOutput
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
	env := newMCPSession(t)
	cs := env.cs
	client := dialTestClient(t, env.srvURL)

	runID := startTestRun(t, env.srvURL, "glm", "permission")
	waitForPendingPermission(t, client, runID)

	var perms taskPermissionsOutput
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
			if pending := pendingPermissionsFrom(result); len(pending) > 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never raised a pending permission", runID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// structuredToolCallEvent returns the first tool_call event with an input.
func structuredToolCallEvent(t *testing.T, events []mcpRunEvent) mcpRunEvent {
	t.Helper()
	for _, e := range events {
		if e.Type == "tool_call" && e.Input != nil {
			return e
		}
	}
	t.Fatalf("no tool_call event with an input in %+v", events)
	return mcpRunEvent{}
}

// TestMCPTools_TaskStatusWithEvents drives a real run that emits a
// tool_call with an object input (D-G1: the SDK used to infer an array
// schema for json.RawMessage and reject the object at output validation).
func TestMCPTools_TaskStatusWithEvents(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	runID := startTestRun(t, env.srvURL, "glm", "structured")
	waitForRunStatus(t, client, runID, "done")

	var status taskStatusOutput
	if isErr := callMCPTool(t, env.cs, "task_status", map[string]any{"runId": runID}, &status); isErr {
		t.Fatal("task_status returned an error result")
	}
	e := structuredToolCallEvent(t, status.Events)
	in, ok := e.Input.(map[string]any)
	if !ok || in["command"] != "go test ./..." {
		t.Fatalf("tool_call input = %#v, want object with command", e.Input)
	}
}

// TestMCPTools_TaskLogsWithEvents is the task_logs twin of the above.
func TestMCPTools_TaskLogsWithEvents(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	runID := startTestRun(t, env.srvURL, "glm", "structured")
	waitForRunStatus(t, client, runID, "done")

	var logs taskLogsOutput
	if isErr := callMCPTool(t, env.cs, "task_logs", map[string]any{"runId": runID}, &logs); isErr {
		t.Fatal("task_logs returned an error result")
	}
	e := structuredToolCallEvent(t, logs.Events)
	in, ok := e.Input.(map[string]any)
	if !ok || in["command"] != "go test ./..." {
		t.Fatalf("tool_call input = %#v, want object with command", e.Input)
	}
}

// TestMCPTaskPermissions_IncludesDetail: the daemon's detail on a Write over
// an existing file survives the MCP round trip (and output-schema
// validation) on task_permissions.
func TestMCPTaskPermissions_IncludesDetail(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	runID := startTestRun(t, env.srvURL, "glm", "permission-write")
	waitForPendingPermission(t, client, runID)

	var perms taskPermissionsOutput
	if isErr := callMCPTool(t, env.cs, "task_permissions", map[string]any{"runId": runID}, &perms); isErr {
		t.Fatal("task_permissions returned an error result")
	}
	if len(perms.Pending) != 1 {
		t.Fatalf("pending = %+v, want exactly one", perms.Pending)
	}
	req := perms.Pending[0]
	if !strings.Contains(req.Summary, "overwrites existing file") {
		t.Errorf("summary = %q, want overwrites existing file", req.Summary)
	}
	if d := req.Detail; d == nil || !d.ReplacesExisting || d.Diff == "" {
		t.Fatalf("detail = %+v, want replacesExisting and a diff", d)
	}
	_ = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": req.RequestID, "optionId": firstDenyOption(req.Options),
	}, nil)
}

// TestMCPTaskWait_PendingPermissionsIncludeDetail: task_wait returns the
// detail on pendingPermission and pendingPermissions.
func TestMCPTaskWait_PendingPermissionsIncludeDetail(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	runID := startTestRun(t, env.srvURL, "glm", "permission-write")
	waitForPendingPermission(t, client, runID)

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 10}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.State != "waiting_permission" || out.PendingPermission == nil || len(out.PendingPermissions) != 1 {
		t.Fatalf("task_wait = %+v, want waiting_permission with one pending", out)
	}
	for _, p := range []*pendingPermissionOut{out.PendingPermission, &out.PendingPermissions[0]} {
		if p.Detail == nil || !p.Detail.ReplacesExisting || p.Detail.Diff == "" {
			t.Fatalf("pending detail = %+v, want replacesExisting and a diff", p.Detail)
		}
	}
	_ = client.Call(context.Background(), "run.respondPermission", map[string]any{
		"runId": runID, "requestId": out.PendingPermission.RequestID, "optionId": firstDenyOption(out.PendingPermission.Options),
	}, nil)
}

func TestTaskStatus_ProvenanceFields(t *testing.T) {
	keysOf := func(out taskStatusOutput) map[string]any {
		b, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// Decode from the run.logs wire shape, as mcpRunLogs does.
	var known runLogsResult
	wire := `{"runId":"r1","status":"done","baseSha":"aaaa1111","headSha":"bbbb2222","dirty":false,"events":[]}`
	if err := json.Unmarshal([]byte(wire), &known); err != nil {
		t.Fatal(err)
	}
	got := keysOf(buildTaskStatusOutput(known))
	// dirty=false is known and must not be dropped by omitempty.
	if got["baseSha"] != "aaaa1111" || got["headSha"] != "bbbb2222" || got["dirty"] != false {
		t.Fatalf("known task_status = %+v, want baseSha/headSha/dirty=false", got)
	}

	var unknown runLogsResult
	if err := json.Unmarshal([]byte(`{"runId":"r2","status":"running","events":[]}`), &unknown); err != nil {
		t.Fatal(err)
	}
	got = keysOf(buildTaskStatusOutput(unknown))
	for _, key := range []string{"baseSha", "headSha", "dirty"} {
		if _, ok := got[key]; ok {
			t.Errorf("unknown task_status has %q, want it absent: %+v", key, got)
		}
	}
}
