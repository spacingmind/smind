package wsapi

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkspaceBranches_RPC(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	runGitT(t, repo, "push", "origin", "develop:refs/heads/feat/remote")
	runGitT(t, repo, "fetch", "origin")
	runGitT(t, repo, "remote", "set-head", "origin", "develop")
	runGitT(t, repo, "branch", "feat/local")
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestWSServer(t, wm, nil, db, "tok")
	ws := dialWS(t, srv, "tok")

	type result struct {
		DefaultBase string `json:"defaultBase"`
		Current     string `json:"current"`
		Branches    []struct {
			Name   string `json:"name"`
			Remote string `json:"remote"`
			Ahead  *int   `json:"ahead"`
			Behind *int   `json:"behind"`
		} `json:"branches"`
	}
	call := func(id string, params map[string]any) (result, envelope) {
		t.Helper()
		sendRequest(t, ws, id, "workspace.branches", params)
		env := readEnvelopeFor(t, ws, id, 5*time.Second)
		var r result
		if env.Error == nil {
			if err := json.Unmarshal(env.Result, &r); err != nil {
				t.Fatal(err)
			}
		}
		return r, env
	}

	r, env := call("1", map[string]any{"workspaceId": w.ID})
	if env.Error != nil {
		t.Fatalf("workspace.branches error = %+v", env.Error)
	}
	if r.DefaultBase != "develop" || r.Current != "develop" {
		t.Fatalf("defaultBase=%q current=%q", r.DefaultBase, r.Current)
	}
	seen := map[string]string{}
	for _, b := range r.Branches {
		seen[b.Name] = b.Remote
		if b.Ahead == nil || b.Behind == nil || *b.Ahead != 0 || *b.Behind != 0 {
			t.Fatalf("%s ahead/behind = %v/%v, want 0/0 (all branches sit on develop)", b.Name, b.Ahead, b.Behind)
		}
	}
	if seen["develop"] != "local" || seen["feat/local"] != "local" || seen["feat/remote"] != "origin" || len(seen) != 3 {
		t.Fatalf("branches = %v, want develop+feat/local local and feat/remote origin only (no origin/HEAD)", seen)
	}

	r, _ = call("2", map[string]any{"workspaceId": w.ID, "query": "REMOTE", "limit": 5})
	if len(r.Branches) != 1 || r.Branches[0].Name != "feat/remote" {
		t.Fatalf("query REMOTE = %+v", r.Branches)
	}

	if _, env = call("3", map[string]any{"workspaceId": "x"}); env.Error == nil {
		t.Fatal("bad params accepted")
	}
	if _, env = call("4", map[string]any{"workspaceId": 99999}); env.Error == nil {
		t.Fatal("unknown workspace accepted")
	}
}
