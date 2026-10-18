package main

import (
	"strings"
	"testing"
)

// A run cut off at a turn cap ends "done" (no new status), but every MCP read of
// it says needs-continue so a driver does not take the work for finished
// (orchestration-parity D13).
func TestMCPTools_DispositionNeedsContinue(t *testing.T) {
	env := newMCPSession(t)
	cappedID := startTestRun(t, env.srvURL, "glm", "stop max_turn_requests")
	normalID := startTestRun(t, env.srvURL, "glm", "stop end_turn")

	var wait taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": cappedID, "timeoutSeconds": 30}, &wait); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if wait.Status != "done" || wait.State != "done" || wait.StopReason != "max_turn_requests" || wait.Disposition != "needs-continue" {
		t.Fatalf("task_wait = status %q state %q stop %q disposition %q, want done/done/max_turn_requests/needs-continue", wait.Status, wait.State, wait.StopReason, wait.Disposition)
	}
	if !strings.Contains(wait.Message, "NOT finished") || !strings.Contains(wait.Message, "continue") {
		t.Fatalf("task_wait message = %q, want it to say the work is not finished and how to continue", wait.Message)
	}

	var status taskStatusOutput
	callMCPTool(t, env.cs, "task_status", map[string]any{"runId": cappedID}, &status)
	if status.Status != "done" || status.Disposition != "needs-continue" {
		t.Fatalf("task_status = %q / %q", status.Status, status.Disposition)
	}
	var logs taskLogsOutput
	callMCPTool(t, env.cs, "task_logs", map[string]any{"runId": cappedID}, &logs)
	if logs.Disposition != "needs-continue" {
		t.Fatalf("task_logs disposition = %q", logs.Disposition)
	}

	// run_list passes run.list's own fields through.
	var list runListOutput
	callMCPTool(t, env.cs, "run_list", map[string]any{}, &list)
	seen := map[string]any{}
	for _, r := range list.Runs {
		seen[r["ID"].(string)] = r["Disposition"]
	}
	if seen[cappedID] != "needs-continue" || seen[normalID] != "" {
		t.Fatalf("run_list dispositions = %v, want capped needs-continue and normal empty", seen)
	}

	// A normal finish carries none.
	var normalWait taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": normalID, "timeoutSeconds": 30}, &normalWait)
	if normalWait.Disposition != "" || normalWait.Message != "" {
		t.Fatalf("normal finish: disposition %q message %q, want none", normalWait.Disposition, normalWait.Message)
	}
}
