package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTaskDeny_RejectsPending(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	runID := startTestRun(t, env.srvURL, "glm", "permission")
	waitForPendingPermission(t, client, runID)

	code, out := runCaptured(t, []string{"task", "deny", runID})
	if code != 0 {
		t.Fatalf("task deny = %d, out %q", code, out)
	}

	// The permission_resolved event is appended by the run's own goroutine
	// after the daemon delivers the decision, so run.logs can lag the CLI's
	// exit: wait for it instead of asserting on the first read.
	var logs runLogsResult
	deadline := time.Now().Add(10 * time.Second)
	for {
		logs = runLogsResult{}
		if err := client.Call(context.Background(), "run.logs", map[string]any{"runId": runID}, &logs); err != nil {
			t.Fatalf("run.logs: %v", err)
		}
		if len(pendingPermissionsFrom(logs)) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a permission is still pending 10s after deny: %+v", logs.Events)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var req, resolved *runLogEvent
	for i := range logs.Events {
		switch logs.Events[i].Type {
		case "permission_request":
			req = &logs.Events[i]
		case "permission_resolved":
			resolved = &logs.Events[i]
		}
	}
	if req == nil || resolved == nil {
		t.Fatalf("missing permission request/resolution in %+v", logs.Events)
	}
	for _, o := range req.Options {
		if o.ID == resolved.OptionID && !strings.HasPrefix(o.Kind, "reject") {
			t.Fatalf("deny chose option %+v, want a reject option", o)
		}
	}
	if !strings.Contains(out, resolved.OptionID) {
		t.Fatalf("stdout %q does not name option %s", out, resolved.OptionID)
	}

	// Nothing left to deny.
	if code, _ := runCaptured(t, []string{"task", "deny", runID}); code != 1 {
		t.Fatalf("second task deny = %d, want 1", code)
	}
}

func TestTaskApprove_HelpFlag(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		for _, sub := range []string{"approve", "deny"} {
			code, out := runCaptured(t, []string{"task", sub, flag})
			if code != 0 || !strings.Contains(out, "usage: smind task "+sub) {
				t.Fatalf("task %s %s = %d, out %q, want usage and exit 0", sub, flag, code, out)
			}
		}
	}
}
