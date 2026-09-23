package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestServer_RunSetApprovalPolicy_SwitchesLiveRun proves run.setApprovalPolicy
// switches a running run's approvalPolicy from any connection, and that the
// change is visible back via run.list -- see
// docs/plans/active/mid-run-approval-and-retry-effort.md's Item A.
func TestServer_RunSetApprovalPolicy_SwitchesLiveRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	term := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if term.Error != nil {
		t.Fatalf("run.start error = %v", term.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(term.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	other := dialWS(t, srv, "tok")
	sendRequest(t, other, "switch", "run.setApprovalPolicy", map[string]any{
		"runId": started.RunID, "policy": "auto-safe",
	})
	switchResp := readEnvelopeFor(t, other, "switch", 5*time.Second)
	if switchResp.Error != nil {
		t.Fatalf("run.setApprovalPolicy error = %v", switchResp.Error.Message)
	}
	var switchResult runApprovalPolicyResult
	if err := json.Unmarshal(switchResp.Result, &switchResult); err != nil {
		t.Fatalf("decode run.setApprovalPolicy result: %v", err)
	}
	if switchResult.ApprovalPolicy != taskrunner.ApprovalPolicyAutoSafe {
		t.Fatalf("run.setApprovalPolicy result = %q, want %q", switchResult.ApprovalPolicy, taskrunner.ApprovalPolicyAutoSafe)
	}

	sendRequest(t, other, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, other, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("run.list error = %v", listResp.Error.Message)
	}
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ApprovalPolicy != taskrunner.ApprovalPolicyAutoSafe {
		t.Fatalf("run.list = %+v, want ApprovalPolicy %q", summaries, taskrunner.ApprovalPolicyAutoSafe)
	}

	sendRequest(t, other, "stop", "run.stop", map[string]any{"runId": started.RunID})
	if resp := readEnvelopeFor(t, other, "stop", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.stop error = %v", resp.Error.Message)
	}
}

// TestServer_RunSetApprovalPolicy_RejectsFullAccess proves the RPC surfaces
// full-access's rejection as a clear error over the wire, not a silently
// accepted request.
func TestServer_RunSetApprovalPolicy_RejectsFullAccess(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	ws := dialWS(t, srv, "tok")
	sendRequest(t, ws, "1", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	term := readEnvelopeFor(t, ws, "1", 5*time.Second)
	var started runStartResult
	if err := json.Unmarshal(term.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	sendRequest(t, ws, "switch", "run.setApprovalPolicy", map[string]any{
		"runId": started.RunID, "policy": "full-access",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("run.setApprovalPolicy(full-access) error = nil, want a clear rejection")
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	if resp := readEnvelopeFor(t, ws, "stop", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.stop error = %v", resp.Error.Message)
	}
}

// TestServer_RunSetApprovalPolicy_RejectsFinishedRun proves the RPC reports
// a clear error against an already-finished run rather than a silent no-op.
func TestServer_RunSetApprovalPolicy_RejectsFinishedRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	ws := dialWS(t, srv, "tok")
	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, ws, "1", time.Until(deadline))
		if env.Event == "" {
			break
		}
	}

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d runs, want 1: %+v", len(summaries), summaries)
	}
	runID := summaries[0].ID

	sendRequest(t, ws, "switch", "run.setApprovalPolicy", map[string]any{
		"runId": runID, "policy": "auto-safe",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("run.setApprovalPolicy() on a finished run error = nil, want a clear rejection")
	}
}

// TestServer_RunSetApprovalPolicy_UnknownRun proves the RPC reports a clear
// error for a run id the daemon has never heard of.
func TestServer_RunSetApprovalPolicy_UnknownRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	ws := dialWS(t, srv, "tok")
	sendRequest(t, ws, "switch", "run.setApprovalPolicy", map[string]any{
		"runId": "does-not-exist", "policy": "auto-safe",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("run.setApprovalPolicy() on an unknown run error = nil, want a clear rejection")
	}
}
