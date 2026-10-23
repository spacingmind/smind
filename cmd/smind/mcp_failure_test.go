package main

import (
	"testing"
	"time"
)

// checkRateLimited asserts the typed failure fields of a rate-limited run.
func checkRateLimited(t *testing.T, tool, kind, window, resetAt string) {
	t.Helper()
	if kind != "rate_limited" || window != "5h" || resetAt == "" {
		t.Fatalf("%s failure = %q/%q/%q, want rate_limited/5h/resetAt set", tool, kind, window, resetAt)
	}
	ts, err := time.Parse(time.RFC3339, resetAt)
	if err != nil || ts.Location() != time.UTC {
		t.Fatalf("%s resetAt = %q (%v), want RFC 3339 UTC", tool, resetAt, err)
	}
}

func TestMCPTaskStatus_FailureFields(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "quota-error")
	var wait taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 30}, &wait)

	var status taskStatusOutput
	if isErr := callMCPTool(t, env.cs, "task_status", map[string]any{"runId": runID}, &status); isErr {
		t.Fatal("task_status returned an error result")
	}
	if status.Status != "error" {
		t.Fatalf("status = %q, want error", status.Status)
	}
	checkRateLimited(t, "task_status", status.FailureKind, status.Window, status.ResetAt)

	var logs taskLogsOutput
	callMCPTool(t, env.cs, "task_logs", map[string]any{"runId": runID}, &logs)
	checkRateLimited(t, "task_logs", logs.FailureKind, logs.Window, logs.ResetAt)

	// An ordinary finish carries none.
	okID := startTestRun(t, env.srvURL, "glm", "stop end_turn")
	var okWait taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": okID, "timeoutSeconds": 30}, &okWait)
	var okStatus taskStatusOutput
	callMCPTool(t, env.cs, "task_status", map[string]any{"runId": okID}, &okStatus)
	if okStatus.FailureKind != "" || okStatus.Window != "" || okStatus.ResetAt != "" {
		t.Fatalf("done run carries failure fields: %+v", okStatus)
	}
}

func TestMCPTaskWait_RateLimitedResult(t *testing.T) {
	env := newMCPSession(t)
	runID := startTestRun(t, env.srvURL, "glm", "quota-error")
	var wait taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": runID, "timeoutSeconds": 30}, &wait); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if wait.State != "error" {
		t.Fatalf("state = %q, want error", wait.State)
	}
	checkRateLimited(t, "task_wait", wait.FailureKind, wait.Window, wait.ResetAt)
}

func TestMCPRunList_FailureFields(t *testing.T) {
	env := newMCPSession(t)
	limitedID := startTestRun(t, env.srvURL, "glm", "quota-error")
	var wait taskWaitOutput
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": limitedID, "timeoutSeconds": 30}, &wait)
	okID := startTestRun(t, env.srvURL, "glm", "stop end_turn")
	callMCPTool(t, env.cs, "task_wait", map[string]any{"runId": okID, "timeoutSeconds": 30}, &wait)

	var list runListOutput
	callMCPTool(t, env.cs, "run_list", map[string]any{}, &list)
	seen := 0
	for _, r := range list.Runs {
		switch r["ID"] {
		case limitedID:
			seen++
			kind, _ := r["failureKind"].(string)
			window, _ := r["window"].(string)
			resetAt, _ := r["resetAt"].(string)
			checkRateLimited(t, "run_list", kind, window, resetAt)
		case okID:
			seen++
			for _, k := range []string{"failureKind", "window", "resetAt"} {
				if _, ok := r[k]; ok {
					t.Fatalf("done run row carries %q: %v", k, r)
				}
			}
		}
	}
	if seen != 2 {
		t.Fatalf("run_list rows matched = %d, want 2: %v", seen, list.Runs)
	}
}
