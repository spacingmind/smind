package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestServer_TaskDiff_RoundTrip proves task.diff's wire shape -- a
// {taskId} request producing a {diff} result -- round-trips correctly over
// a real WebSocket connection: an untracked change written into the task's
// worktree shows up in the returned diff text, and a task with no changes
// gets back an empty diff, not an error.
func TestServer_TaskDiff_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)

	t.Run("changed worktree", func(t *testing.T) {
		task := newTestTask(t, wm, "diff me")
		srv := newTestWSServer(t, wm, runner, db, "tok")
		ws := dialWS(t, srv, "tok")

		sendRequest(t, ws, "1", "task.diff", map[string]any{"taskId": task.ID})
		resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("task.diff error = %v", resp.Error.Message)
		}
		var result taskDiffResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("decode task.diff result: %v", err)
		}
		if result.Diff == "" {
			t.Fatal("task.diff result.Diff is empty, want the scenario file's diff text")
		}
		for _, want := range []string{"scenario", "diff me"} {
			if !strings.Contains(result.Diff, want) {
				t.Fatalf("task.diff result.Diff = %q, want it to contain %q", result.Diff, want)
			}
		}
	})

	t.Run("unchanged worktree", func(t *testing.T) {
		task := newTestTask(t, wm, "")
		srv := newTestWSServer(t, wm, runner, db, "tok")
		ws := dialWS(t, srv, "tok")

		sendRequest(t, ws, "1", "task.diff", map[string]any{"taskId": task.ID})
		resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("task.diff error = %v", resp.Error.Message)
		}
		var result taskDiffResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("decode task.diff result: %v", err)
		}
		if result.Diff != "" {
			t.Fatalf("task.diff result.Diff = %q, want empty for an unmodified worktree", result.Diff)
		}
	})
}
