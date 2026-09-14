package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestServer_TaskStats proves task.stats' wire shape end to end: one entry
// per task in the workspace, carrying the branch and the counts of the
// same diff task.diff renders, with untracked files included.
func TestServer_TaskStats(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	// A modification to the repo's README plus a file git has never seen.
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "README.md"), []byte("hello\nmore\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatalf("write new.txt: %v", err)
	}

	sendRequest(t, ws, "stats", "task.stats", map[string]any{"workspaceId": task.WorkspaceID})
	resp := readEnvelopeFor(t, ws, "stats", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.stats error = %v", resp.Error.Message)
	}
	var result taskStatsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode task.stats result: %v", err)
	}
	if len(result.Stats) != 1 {
		t.Fatalf("task.stats stats = %+v, want exactly one entry", result.Stats)
	}
	got := result.Stats[0]
	if got.TaskID != task.ID {
		t.Fatalf("TaskID = %d, want %d", got.TaskID, task.ID)
	}
	if got.Branch != *task.Branch {
		t.Fatalf("Branch = %q, want %q", got.Branch, *task.Branch)
	}
	if got.FilesChanged != 2 {
		t.Fatalf("FilesChanged = %d, want 2 (the untracked file counts too): %+v", got.FilesChanged, got)
	}
	if got.Insertions != 2 || got.Deletions != 0 {
		t.Fatalf("stat = %+v, want 2 insertions and no deletions", got)
	}
}

// TestServer_TaskStats_UnknownWorkspaceIsEmptyNotError pins the empty case:
// a workspace with no tasks (or one that does not exist) answers with an
// empty list rather than an error, so the sidebar's fetch never turns a
// freshly created workspace into an error row.
func TestServer_TaskStats_UnknownWorkspaceIsEmptyNotError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "stats", "task.stats", map[string]any{"workspaceId": 4242})
	resp := readEnvelopeFor(t, ws, "stats", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.stats error = %v", resp.Error.Message)
	}
	var result taskStatsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode task.stats result: %v", err)
	}
	if len(result.Stats) != 0 {
		t.Fatalf("task.stats stats = %+v, want empty", result.Stats)
	}
}
