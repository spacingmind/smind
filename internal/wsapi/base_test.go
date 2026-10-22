package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func TestServer_WorkspaceDefaultBase(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "main")
	runGitT(t, repo, "branch", "release")
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	call := func(id, method string, params any) envelope {
		sendRequest(t, ws, id, method, params)
		return readEnvelopeFor(t, ws, id, 5*time.Second)
	}
	decode := func(env envelope) defaultBaseResult {
		t.Helper()
		if env.Error != nil {
			t.Fatalf("unexpected error: %+v", env.Error)
		}
		var r defaultBaseResult
		if err := json.Unmarshal(env.Result, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	if r := decode(call("1", "workspace.defaultBase", map[string]any{"workspaceId": w.ID})); r != (defaultBaseResult{Ref: "main", Detected: true}) {
		t.Fatalf("defaultBase = %+v, want main detected", r)
	}
	if r := decode(call("2", "workspace.setDefaultBase", map[string]any{"workspaceId": w.ID, "branch": "release"})); r != (defaultBaseResult{Ref: "release", Override: "release"}) {
		t.Fatalf("setDefaultBase = %+v, want release override", r)
	}
	if r := decode(call("3", "workspace.defaultBase", map[string]any{"workspaceId": w.ID})); r.Ref != "release" || r.Detected {
		t.Fatalf("defaultBase after set = %+v", r)
	}

	env := call("4", "workspace.setDefaultBase", map[string]any{"workspaceId": w.ID, "branch": "no-such"})
	if env.Error == nil || !strings.HasPrefix(env.Error.Message, "workspace.setDefaultBase: ") {
		t.Fatalf("setDefaultBase(no-such) error = %+v, want a method-prefixed failure", env.Error)
	}
	env = call("5", "workspace.defaultBase", map[string]any{"workspaceId": "x"})
	if env.Error == nil || !strings.HasPrefix(env.Error.Message, "workspace.defaultBase: invalid params") {
		t.Fatalf("defaultBase(bad params) error = %+v", env.Error)
	}

	if r := decode(call("6", "workspace.setDefaultBase", map[string]any{"workspaceId": w.ID, "branch": ""})); r != (defaultBaseResult{Ref: "main", Detected: true}) {
		t.Fatalf("cleared = %+v, want main detected", r)
	}
}

func TestServer_TaskSetBase(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	ws0, _ := wm.GetWorkspace(task.WorkspaceID)
	runGitT(t, ws0.Path, "branch", "other")
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.setBase", map[string]any{"taskId": task.ID, "baseRef": "other"})
	env := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if env.Error != nil {
		t.Fatalf("task.setBase error = %+v", env.Error)
	}
	var got store.Task
	if err := json.Unmarshal(env.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.BaseRef != "other" || len(got.BaseSha) != 40 {
		t.Fatalf("task.setBase result = %+v, want base other + a 40-hex sha", got)
	}

	sendRequest(t, ws, "2", "task.setBase", map[string]any{"taskId": task.ID, "baseRef": "missing"})
	env = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if env.Error == nil || env.Error.Code != "base_not_found" || !strings.HasPrefix(env.Error.Message, "task.setBase: ") {
		t.Fatalf("task.setBase(missing) error = %+v, want code base_not_found", env.Error)
	}

	sendRequest(t, ws, "3", "task.setBase", map[string]any{"taskId": "x"})
	env = readEnvelopeFor(t, ws, "3", 5*time.Second)
	if env.Error == nil || !strings.HasPrefix(env.Error.Message, "task.setBase: invalid params") {
		t.Fatalf("task.setBase(bad params) error = %+v", env.Error)
	}
}

func TestServer_TaskDiff_BaseUnknownCode(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	if err := db.SetTaskBase(task.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.diff", map[string]any{"taskId": task.ID})
	env := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if env.Error == nil || env.Error.Code != "base_unknown" || !strings.Contains(env.Error.Message, "task.setBase") {
		t.Fatalf("task.diff error = %+v, want code base_unknown with the task.setBase sentence", env.Error)
	}
}
