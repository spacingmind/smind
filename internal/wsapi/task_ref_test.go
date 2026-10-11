package wsapi

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTaskCreate_RefModesMutuallyExclusive proves task.create rejects
// supplying more than one of baseRef/branch/prNumber before anything
// reaches the workspace manager.
func TestTaskCreate_RefModesMutuallyExclusive(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, nil, db, "tok")
	ws := dialWS(t, srv, "tok")

	cases := []map[string]any{
		{"baseRef": "origin/develop", "branch": "develop"},
		{"baseRef": "origin/develop", "prNumber": 7},
		{"branch": "develop", "prNumber": 7},
		{"baseRef": "origin/develop", "branch": "develop", "prNumber": 7},
	}
	for i, params := range cases {
		params["workspaceId"] = int64(1)
		params["title"] = "T"
		sendRequest(t, ws, "1", "task.create", params)
		resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
		if resp.Error == nil {
			t.Fatalf("case %d (%v): error = nil, want the mutual-exclusion rejection", i, params)
		}
		if !strings.Contains(resp.Error.Message, "at most one") {
			t.Fatalf("case %d: error = %q, want the mutual-exclusion message", i, resp.Error.Message)
		}
	}
}

// TestTaskCreate_BaseRefReachesManager proves a lone baseRef round-trips
// into the workspace manager: the task's worktree is actually created at
// the named ref's commit, not the checkout's HEAD.
func TestTaskCreate_BaseRefReachesManager(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	headSHA := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", "second.txt")
	runGitT(t, repo, "commit", "-m", "second")
	runGitT(t, repo, "push", "origin", "develop")
	baseSHA := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	runGitT(t, repo, "reset", "--hard", headSHA)

	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	srv := newTestWSServer(t, wm, nil, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.create", map[string]any{
		"workspaceId": w.ID, "title": "Off develop", "baseRef": "origin/develop",
	})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.create error = %v", resp.Error.Message)
	}
	var task struct {
		ID           int64  `json:"ID"`
		WorktreePath string `json:"WorktreePath"`
	}
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("decode task.create result: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, task.WorktreePath, "rev-parse", "HEAD")); got != baseSHA {
		t.Fatalf("worktree HEAD = %s, want origin/develop's commit %s", got, baseSHA)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
