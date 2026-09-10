package wsapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"time"
)

// emptyListResult sends each request sequentially on ws and asserts, for the
// response to id, that the RAW result bytes start with want and are never
// "null". Assertions deliberately compare the raw json.RawMessage rather
// than decoding into a Go slice -- a typed decode accepts null and leaves a
// nil slice indistinguishable from an empty one, which is exactly how this
// bug class stayed invisible to the existing Go suite (see
// docs/research/test-gap-audit.md §2).
func emptyListResult(t *testing.T, ws *websocket.Conn, id, method string, params any, want string) {
	t.Helper()
	sendRequest(t, ws, id, method, params)
	resp := readEnvelopeFor(t, ws, id, 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("%s: error = %v, want empty-success result", method, resp.Error)
	}
	got := strings.TrimSpace(string(resp.Result))
	if got == "null" {
		t.Fatalf("%s: raw result = null, want %q", method, want)
	}
	if !strings.HasPrefix(got, want) {
		t.Fatalf("%s: raw result = %s, want prefix %q", method, got, want)
	}
}

// TestServer_EmptyDaemonListResultsAreArrays is the wire-shape contract
// test for the fresh-install case: against a completely empty store, every
// list-shaped RPC must wire-marshal to an array (or the correct empty
// object), never JSON null.
func TestServer_EmptyDaemonListResultsAreArrays(t *testing.T) {
	t.Parallel()

	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	emptyListResult(t, ws, "1", "workspace.list", nil, "[]")
	// No workspace exists, so workspaceId 1 matches nothing.
	emptyListResult(t, ws, "2", "space.list", map[string]any{"workspaceId": 1}, "[]")
	emptyListResult(t, ws, "3", "task.list", map[string]any{"workspaceId": 1}, "[]")
	emptyListResult(t, ws, "4", "run.list", nil, "[]")
	emptyListResult(t, ws, "5", "terminal.list", map[string]any{"taskId": 1}, "[]")
	emptyListResult(t, ws, "6", "account.list", nil, "[]")
	// provider.list's shape is an object with a "providers" array; the
	// full literal varies with provider info, so assert the object+array
	// prefix (a nil Providers slice would marshal as {"providers":null}).
	emptyListResult(t, ws, "7", "provider.list", nil, `{"providers":[`)
}

// TestServer_EmptyTaskListResultsAreArrays covers the two list RPCs whose
// empty case inherently requires an existing task (the handler resolves the
// task's worktree first): task.files with a clean worktree, and file.list
// on an empty directory inside the worktree.
func TestServer_EmptyTaskListResultsAreArrays(t *testing.T) {
	t.Parallel()

	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	task := newTestTask(t, wm, "")
	if err := os.Mkdir(filepath.Join(*task.WorktreePath, "empty-dir"), 0o755); err != nil {
		t.Fatalf("mkdir empty-dir: %v", err)
	}

	emptyListResult(t, ws, "1", "task.files", map[string]any{"taskId": task.ID}, `{"files":[]}`)
	emptyListResult(t, ws, "2", "file.list", map[string]any{"taskId": task.ID, "path": "empty-dir"}, "[]")
}
