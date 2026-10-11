package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// stopTestRuns stops runs at test end so no hung run outlives its daemon.
func stopTestRuns(t *testing.T, env *mcpTestEnv, runIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range runIDs {
			var out taskStopOutput
			callMCPTool(t, env.cs, "task_stop", map[string]any{"runId": id}, &out)
		}
	})
}

// TestMCPTools_TaskWaitAnyOf proves wait-any returns as soon as one run is
// terminal while another is still running, naming the finished run.
func TestMCPTools_TaskWaitAnyOf(t *testing.T) {
	env := newMCPSession(t)
	hung := startTestRun(t, env.srvURL, "glm", "hang")
	stopTestRuns(t, env, hung)
	finishing := startTestRun(t, env.srvURL, "glm")

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runIds": []string{hung, finishing}, "timeoutSeconds": 20}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if elapsed := time.Since(start); elapsed >= 15*time.Second {
		t.Fatalf("task_wait took %s, want an early return once one run finished", elapsed)
	}
	if out.TimedOut || out.RunID != finishing || out.Status != "done" {
		t.Fatalf("task_wait = %+v, want runId %s done", out, finishing)
	}
}

// TestMCPTools_TaskWaitAnyOfTimeout proves wait-any reports timedOut (not
// an error) when every listed run is still going.
func TestMCPTools_TaskWaitAnyOfTimeout(t *testing.T) {
	env := newMCPSession(t)
	a := startTestRun(t, env.srvURL, "glm", "hang")
	b := startTestRun(t, env.srvURL, "glm", "hang")
	stopTestRuns(t, env, a, b)

	start := time.Now()
	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runIds": []string{a, b}, "timeoutSeconds": 1}, &out); isErr {
		t.Fatal("task_wait returned an error result on timeout")
	}
	if !out.TimedOut {
		t.Fatalf("task_wait = %+v, want timedOut true", out)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("task_wait took %s, want about the 1s timeout", elapsed)
	}
}

// TestMCPTools_TaskWaitAnyOfPermissionPending proves a pending permission
// request on one run triggers the return, naming that run.
func TestMCPTools_TaskWaitAnyOfPermissionPending(t *testing.T) {
	env := newMCPSession(t)
	hung := startTestRun(t, env.srvURL, "glm", "hang")
	blocked := startTestRun(t, env.srvURL, "glm", "permission")
	stopTestRuns(t, env, hung, blocked)

	var out taskWaitOutput
	if isErr := callMCPTool(t, env.cs, "task_wait", map[string]any{"runIds": []string{hung, blocked}, "timeoutSeconds": 10}, &out); isErr {
		t.Fatal("task_wait returned an error result")
	}
	if out.TimedOut || out.RunID != blocked || out.PendingPermission == nil {
		t.Fatalf("task_wait = %+v, want runId %s with a pending permission", out, blocked)
	}
	if isTerminalRunStatus(out.Status) {
		t.Fatalf("status = %q, want non-terminal", out.Status)
	}
}

// TestMCPTools_TaskWaitRejectsBothAndTooMany proves the argument rules:
// runId+runIds, >16 ids, duplicates and a single-element list are tool
// errors, before any run is polled.
func TestMCPTools_TaskWaitRejectsBothAndTooMany(t *testing.T) {
	env := newMCPSession(t)
	many := make([]string, 17)
	for i := range many {
		many[i] = fmt.Sprintf("run-%d", i)
	}
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"both":      {map[string]any{"runId": "a", "runIds": []string{"b", "c"}}, "not both"},
		"too many":  {map[string]any{"runIds": many}, "2-16"},
		"duplicate": {map[string]any{"runIds": []string{"a", "b", "a"}}, "duplicate"},
		"single":    {map[string]any{"runIds": []string{"a"}}, "2-16"},
	}
	for name, tc := range cases {
		msg := callMCPToolErr(t, env.cs, "task_wait", tc.args)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: error = %q, want containing %q", name, msg, tc.want)
		}
	}
}
