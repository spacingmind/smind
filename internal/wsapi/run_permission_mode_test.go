package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func startRun(t *testing.T, ws *websocket.Conn, params map[string]any) runStartResult {
	t.Helper()
	sendRequest(t, ws, "start", "run.start", params)
	term := readEnvelopeFor(t, ws, "start", 5*time.Second)
	if term.Error != nil {
		t.Fatalf("run.start error = %v", term.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(term.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}
	return started
}

// TestServer_RunSetPermissionMode_TogglesAutoAcceptOnLiveRun proves
// run.setPermissionMode flips a live ACP run's autoAccept from any
// connection and the change is visible via run.list (S12 over the wire).
func TestServer_RunSetPermissionMode_TogglesAutoAcceptOnLiveRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	started := startRun(t, dialWS(t, srv, "tok"), map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})

	other := dialWS(t, srv, "tok")
	sendRequest(t, other, "switch", "run.setPermissionMode", map[string]any{"runId": started.RunID, "autoAccept": true})
	resp := readEnvelopeFor(t, other, "switch", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.setPermissionMode error = %v", resp.Error.Message)
	}
	var result runPermissionModeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.AutoAccept || result.PermissionMode != taskrunner.ACPModeDefault {
		t.Fatalf("result = %+v, want default mode with autoAccept", result)
	}

	sendRequest(t, other, "list", "run.list", nil)
	var summaries []runs.RunSummary
	mustDecode(t, readEnvelopeFor(t, other, "list", 5*time.Second).Result, &summaries)
	if len(summaries) != 1 || !summaries[0].AutoAccept {
		t.Fatalf("run.list = %+v, want AutoAccept", summaries)
	}

	sendRequest(t, other, "stop", "run.stop", map[string]any{"runId": started.RunID})
	if resp := readEnvelopeFor(t, other, "stop", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.stop error = %v", resp.Error.Message)
	}
}

// TestServer_RunSetPermissionMode_Errors covers the RPC's rejections: no
// field given, an unknown run, and an invalid mode for the provider.
func TestServer_RunSetPermissionMode_Errors(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})

	for name, params := range map[string]map[string]any{
		"neither field": {"runId": started.RunID},
		"unknown run":   {"runId": "does-not-exist", "modeId": "plan"},
		// The fake agent advertises no modes, so only "default" applies.
		"invalid mode": {"runId": started.RunID, "modeId": "auto-safe"},
	} {
		sendRequest(t, ws, name, "run.setPermissionMode", params)
		if resp := readEnvelopeFor(t, ws, name, 5*time.Second); resp.Error == nil {
			t.Errorf("run.setPermissionMode(%s) error = nil, want a clear rejection", name)
		}
	}
	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestServer_RunStart_PermissionSettings is S10 over the wire: run.start
// takes permissionMode/autoAccept, rejects an unknown mode and autoAccept
// on a non-ACP provider, and hard-fails the removed approvalPolicy field
// with an error naming permissionMode (ADR-0019 resolved decision 7).
func TestServer_RunStart_PermissionSettings(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	for name, tc := range map[string]struct {
		params  map[string]any
		wantErr string
	}{
		"glm autoAccept":      {map[string]any{"provider": "glm", "autoAccept": true}, ""},
		"unknown claude mode": {map[string]any{"provider": "claude-native", "permissionMode": "yolo"}, "valid:"},
		"autoAccept claude":   {map[string]any{"provider": "claude-native", "autoAccept": true}, "autoAccept is not supported"},
		"legacy field":        {map[string]any{"provider": "glm", "approvalPolicy": "auto-safe"}, "permissionMode"},
		"legacy manual":       {map[string]any{"provider": "glm", "approvalPolicy": "manual"}, "permissionMode"},
	} {
		task := newTestTask(t, wm, "")
		tc.params["taskId"] = task.ID
		tc.params["prompt"] = "hi"
		sendRequest(t, ws, name, "run.start", tc.params)
		resp := readEnvelopeFor(t, ws, name, 5*time.Second)
		if tc.wantErr == "" {
			if resp.Error != nil {
				t.Errorf("%s: run.start error = %v", name, resp.Error.Message)
			}
			continue
		}
		if resp.Error == nil || !strings.Contains(resp.Error.Message, tc.wantErr) {
			t.Errorf("%s: run.start error = %+v, want containing %q", name, resp.Error, tc.wantErr)
		}
	}
}
