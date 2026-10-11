package main

import (
	"strings"
	"testing"
	"time"
)

// startPermissionRun starts a fake-agent run blocked on a permission request
// and returns its runId and the pending request id.
func startPermissionRun(t *testing.T) (runID, requestID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	runID = startTestRun(t, srvURL, "glm", "permission")
	client := dialTestClient(t, srvURL)
	deadline := time.Now().Add(10 * time.Second)
	for {
		result, err := mcpRunLogs(t.Context(), client, "test", runID, 0)
		if err == nil {
			if p := pendingPermissionsFrom(result); len(p) > 0 {
				return runID, p[0].RequestID
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending permission appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func approveChose(t *testing.T, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var logs runLogsResult
		client, err := dialDaemon(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_ = client.Call(t.Context(), "run.logs", map[string]any{"runId": runID}, &logs)
		client.Close()
		for _, e := range logs.Events {
			if e.Type == "permission_resolved" && e.OptionID == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s never resolved its permission with option %q", runID, want)
}

func TestTaskApprove_ExplicitRejectOption(t *testing.T) {
	runID, reqID := startPermissionRun(t)
	var code int
	out := captureStdout(t, func() { code = run([]string{"task", "approve", runID, reqID, "deny-1"}) })
	if code != 0 {
		t.Fatalf("task approve = %d, want 0 (stdout %q)", code, out)
	}
	if !strings.Contains(out, "-> deny-1") {
		t.Fatalf("stdout = %q, want the chosen reject option", out)
	}
	approveChose(t, runID, "deny-1")
}

func TestTaskApprove_UnknownOptionListsValid(t *testing.T) {
	runID, reqID := startPermissionRun(t)
	var code int
	errOut := captureStderr(t, func() { code = run([]string{"task", "approve", runID, reqID, "nope"}) })
	if code != 1 {
		t.Fatalf("task approve = %d, want 1", code)
	}
	if !strings.Contains(errOut, "allow-1") || !strings.Contains(errOut, "deny-1") {
		t.Fatalf("stderr = %q, want valid option ids listed", errOut)
	}
	// Unblock the run.
	_ = captureStdout(t, func() { run([]string{"task", "approve", runID}) })
}

func TestTaskApprove_ThreeArgsAccepted(t *testing.T) {
	runID, reqID := startPermissionRun(t)
	var code int
	errOut := captureStderr(t, func() {
		_ = captureStdout(t, func() { code = run([]string{"task", "approve", runID, reqID, "allow-1"}) })
	})
	if code != 0 {
		t.Fatalf("task approve with 3 args = %d, want 0 (stderr %q)", code, errOut)
	}
	approveChose(t, runID, "allow-1")
}

func TestTaskApprove_HelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		var code int
		out := captureStdout(t, func() { code = cmdTaskApprove([]string{flag}) })
		if code != 0 {
			t.Fatalf("task approve %s = %d, want 0", flag, code)
		}
		if !strings.Contains(out, "usage: smind task approve") || !strings.Contains(out, "reject") {
			t.Fatalf("stdout = %q, want the usage text", out)
		}
	}
}
