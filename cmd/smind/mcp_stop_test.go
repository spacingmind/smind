package main

import "testing"

// TestMCPTools_TaskStop proves task_stop (docs/plans/active/mcp-server.md
// step 5) wraps run.stop: stopping a still-running run is reflected in a
// subsequent task_status as a terminal "stopped" status. Started via
// run.start directly (task_send is out of scope -- ADR-0019).
func TestMCPTools_TaskStop(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	runID := startTestRun(t, env.srvURL, "glm", "hang")

	var out taskStopOutput
	if isErr := callMCPTool(t, env.cs, "task_stop", map[string]any{"runId": runID}, &out); isErr {
		t.Fatal("task_stop returned an error result")
	}
	if out.RunID != runID || !out.Stopped {
		t.Fatalf("task_stop = %+v, want runId %s stopped", out, runID)
	}

	waitForRunStatus(t, client, runID, "stopped")

	var status taskStatusOutput
	if isErr := callMCPTool(t, env.cs, "task_status", map[string]any{"runId": runID}, &status); isErr {
		t.Fatal("task_status returned an error result")
	}
	if status.Status != "stopped" {
		t.Fatalf("task_status after task_stop = %+v, want status stopped", status)
	}
}
