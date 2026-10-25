package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWSAPI_SetCommitMessageStyle_RoundTripsInWorkspacePayload(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	w, err := wm.CreateWorkspace(newTestRepo(t), "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")
	call := func(id, method string, params any) envelope {
		sendRequest(t, ws, id, method, params)
		return readEnvelopeFor(t, ws, id, 5*time.Second)
	}
	style := func(env envelope) string {
		t.Helper()
		if env.Error != nil {
			t.Fatalf("unexpected error: %+v", env.Error)
		}
		var r map[string]any
		if err := json.Unmarshal(env.Result, &r); err != nil {
			t.Fatal(err)
		}
		s, _ := r["commitMessageStyle"].(string)
		return s
	}

	if got := style(call("1", "workspace.setCommitMessageStyle", map[string]any{"workspaceId": w.ID, "style": "  Use conventional commits  "})); got != "Use conventional commits" {
		t.Fatalf("set result style = %q", got)
	}
	if got := style(call("2", "workspace.get", map[string]any{"id": w.ID})); got != "Use conventional commits" {
		t.Fatalf("workspace.get style = %q", got)
	}
	env := call("3", "workspace.list", nil)
	var list []map[string]any
	if err := json.Unmarshal(env.Result, &list); err != nil || len(list) != 1 || list[0]["commitMessageStyle"] != "Use conventional commits" {
		t.Fatalf("workspace.list = %s, %v", env.Result, err)
	}

	env = call("4", "workspace.setCommitMessageStyle", map[string]any{"workspaceId": w.ID, "style": strings.Repeat("x", 1001)})
	if env.Error == nil || !strings.Contains(env.Error.Message, "commit message style is too long (max 1000 characters)") {
		t.Fatalf("over-long error = %+v", env.Error)
	}
	if got := style(call("5", "workspace.setCommitMessageStyle", map[string]any{"workspaceId": w.ID, "style": ""})); got != "" {
		t.Fatalf("cleared style = %q", got)
	}
}
