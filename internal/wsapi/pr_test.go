package wsapi

import (
	"testing"
	"time"
)

// TestServer_TaskCreatePR_Wired proves task.createPr is actually wired up to
// workspace.Manager.CreatePR and that a real failure (here: the test repo
// has no "origin" remote configured, so the fetch step of CreatePR fails
// for real, no gh involved at all) surfaces as a descriptive JSON-RPC error
// over the wire rather than being swallowed -- internal/workspace's own
// pr_test.go covers the full happy-path/diverged-base/gh-failure matrix
// against a mocked gh, since gh itself needs a real remote+auth this
// package's tests don't have.
func TestServer_TaskCreatePR_Wired(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.createPr", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("task.createPr on a remote-less repo: error = nil, want a descriptive failure")
	}
	if resp.Error.Message == "" {
		t.Fatalf("task.createPr error message is empty, want a non-empty descriptive message")
	}

	// A bogus taskId is its own distinct, descriptive failure (not found),
	// proving handleTaskCreatePR doesn't just forward one generic error for
	// every failure mode.
	sendRequest(t, ws, "2", "task.createPr", map[string]any{"taskId": task.ID + 999})
	resp = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("task.createPr(bogus taskId): error = nil, want a not-found failure")
	}

	sendRequest(t, ws, "3", "task.createPr", map[string]any{"taskId": "not-a-number"})
	resp = readEnvelopeFor(t, ws, "3", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("task.createPr(invalid params): error = nil, want an invalid-params failure")
	}
}
