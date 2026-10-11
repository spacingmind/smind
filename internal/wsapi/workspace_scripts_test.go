package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestWorkspaceSetScripts_RoundTrip: workspace.setScripts stores both
// scripts, null/omitted keeps, "" clears; workspace.get is the only method
// returning the text, list/task.get only the Has* flags.
func TestWorkspaceSetScripts_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	call := func(id, method string, params any) json.RawMessage {
		t.Helper()
		sendRequest(t, ws, id, method, params)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("%s error = %v", method, resp.Error.Message)
		}
		return resp.Result
	}
	wid := task.WorkspaceID

	setRes := call("1", "workspace.setScripts", map[string]any{"workspaceId": wid, "setupScript": "npm ci", "teardownScript": "rm -rf node_modules"})
	if strings.Contains(string(setRes), "npm ci") || !strings.Contains(string(setRes), `"HasSetupScript":true`) || !strings.Contains(string(setRes), `"HasTeardownScript":true`) {
		t.Fatalf("setScripts result = %s, want flags and no text", setRes)
	}

	// Omitted teardown keeps; "" clears setup.
	call("2", "workspace.setScripts", map[string]any{"workspaceId": wid, "setupScript": ""})
	var got struct {
		SetupScript, TeardownScript       string
		HasSetupScript, HasTeardownScript bool
	}
	if err := json.Unmarshal(call("3", "workspace.get", map[string]any{"id": wid}), &got); err != nil {
		t.Fatal(err)
	}
	if got.SetupScript != "" || got.HasSetupScript || got.TeardownScript != "rm -rf node_modules" || !got.HasTeardownScript {
		t.Fatalf("workspace.get = %+v", got)
	}

	// Text never leaks through list / task.get / task.list.
	for i, c := range []struct {
		method string
		params any
	}{
		{"workspace.list", nil},
		{"task.get", map[string]any{"id": task.ID}},
		{"task.list", map[string]any{"workspaceId": wid}},
	} {
		out := call(string(rune('4'+i)), c.method, c.params)
		if strings.Contains(string(out), "node_modules") {
			t.Fatalf("%s leaked script text: %s", c.method, out)
		}
	}
	if out := call("9", "workspace.list", nil); !strings.Contains(string(out), `"HasTeardownScript":true`) {
		t.Fatalf("workspace.list = %s, want HasTeardownScript", out)
	}
}
