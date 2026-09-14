package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestServer_TaskSearchIndex proves task.searchIndex serves the
// quick-open path list (ui-redesign-parity plan, Item 18) over the real
// WS wire: committed files, a fresh untracked one, and (implicitly, via
// workspace.TaskSearchIndex's own tests) gitignore exclusion the daemon
// doesn't have to reimplement.
func TestServer_TaskSearchIndex(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	task := newTestTask(t, wm, "")
	wt := *task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.searchIndex", map[string]any{"taskId": task.ID})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.searchIndex error = %v", resp.Error.Message)
	}
	var result struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode task.searchIndex result: %v", err)
	}

	found := map[string]bool{}
	for _, p := range result.Paths {
		found[p] = true
	}
	if !found["README.md"] || !found["notes.txt"] {
		t.Fatalf("task.searchIndex paths = %v, want README.md and notes.txt present", result.Paths)
	}
}

// TestServer_TaskSearchIndex_UnknownTask proves an unknown taskId is a
// wire error, not a panic or an empty success.
func TestServer_TaskSearchIndex_UnknownTask(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.searchIndex", map[string]any{"taskId": 999})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("task.searchIndex(999) error = nil, want an error for an unknown task")
	}
}
