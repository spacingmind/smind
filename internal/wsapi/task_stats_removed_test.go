package wsapi

import (
	"testing"
	"time"
)

// TestTaskStatsRemoved: task.stats was replaced by task.gitList (ADR-0039), so
// calling it is the ordinary unknown-method error.
func TestTaskStatsRemoved(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "stats", "task.stats", map[string]any{"workspaceId": 1})
	resp := readEnvelopeFor(t, ws, "stats", 5*time.Second)
	if resp.Error == nil || resp.Error.Message != "method not found: task.stats" {
		t.Fatalf("task.stats response = %+v, want the unknown-method error", resp)
	}
}
