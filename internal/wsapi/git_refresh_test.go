package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/gitstate"
)

// prListStub answers gh for a repo whose one open PR (#7) has headRefName
// branch, and counts every gh call by kind.
type prListStub struct {
	branch string
	repos  atomic.Int32
	lists  atomic.Int32
	views  atomic.Int32
}

func (s *prListStub) run(_ context.Context, _ string, args ...string) (string, error) {
	switch {
	case args[0] == "repo":
		s.repos.Add(1)
		return `{"nameWithOwner":"o/r","url":"https://github.com/o/r"}`, nil
	case args[0] == "pr" && args[1] == "list":
		s.lists.Add(1)
		return `[{"number":7,"url":"https://github.com/o/r/pull/7","title":"Fix","state":"OPEN","isDraft":false,` +
			`"headRefName":"` + s.branch + `","headRefOid":"abc","baseRefName":"develop","isCrossRepository":false,` +
			`"mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[]}]`, nil
	case args[0] == "pr" && args[1] == "view":
		s.views.Add(1)
	}
	return "", context.DeadlineExceeded
}

func TestTaskGitRefresh_RPC(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	if ws, err := wm.GetWorkspace(task.WorkspaceID); err != nil {
		t.Fatal(err)
	} else {
		// A forge remote, so createPr's origin blocker (U7) does not hide pr_exists.
		runGitT(t, ws.Path, "remote", "add", "origin", "https://github.com/o/r.git")
	}
	stub := &prListStub{branch: *task.Branch}
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(stub.run)))
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	// A plain read never reaches gh, and carries no PR yet.
	var g gitstate.TaskGit
	ec.call("1", "task.git", map[string]any{"taskId": task.ID}, &g)
	if g.PR != nil || stub.lists.Load() != 0 || stub.repos.Load() != 0 {
		t.Fatalf("task.git: pr %+v, gh calls repo %d list %d; want none", g.PR, stub.repos.Load(), stub.lists.Load())
	}

	// The refresh runs one list, adopts the PR for the task's branch and
	// returns the TaskGit with it.
	ec.call("2", "task.gitRefresh", map[string]any{"taskId": task.ID}, &g)
	if g.PR == nil || g.PR.Number != 7 || g.PR.State != "open" || g.PR.URL != "https://github.com/o/r/pull/7" || g.Forge != "github" {
		t.Fatalf("task.gitRefresh pr = %+v forge %q", g.PR, g.Forge)
	}
	if stub.lists.Load() != 1 || stub.views.Load() != 0 {
		t.Fatalf("gh pr list %d, pr view %d; want 1, 0", stub.lists.Load(), stub.views.Load())
	}
	if got, _ := db.GetTask(task.ID); got.PRNumber != 7 || got.PRRepo != "o/r" || got.PRForge != "github" {
		t.Fatalf("task identity after adoption = %+v", got)
	}
	if g.Actions["createPr"].Code != "pr_exists" {
		t.Fatalf("createPr = %+v, want pr_exists", g.Actions["createPr"])
	}

	// task.git now serves the stored snapshot without another call.
	ec.call("3", "task.git", map[string]any{"taskId": task.ID}, &g)
	if g.PR == nil || g.PR.Number != 7 || stub.lists.Load() != 1 {
		t.Fatalf("task.git after refresh: pr %+v, lists %d", g.PR, stub.lists.Load())
	}

	// A second refresh straight away is over the 5 s floor: typed error.
	sendRequest(t, ec.ws, "4", "task.gitRefresh", map[string]any{"taskId": task.ID})
	resp := ec.nextResponse("4", 5*time.Second)
	if resp.Error == nil || resp.Error.Code != "refresh_limited" {
		t.Fatalf("second refresh error = %+v, want refresh_limited", resp.Error)
	}
	data, _ := json.Marshal(resp.Error.Data)
	if !strings.Contains(string(data), `"retryAt"`) {
		t.Fatalf("refresh_limited data = %s, want retryAt", data)
	}
	if stub.lists.Load() != 1 {
		t.Fatalf("a limited refresh called gh (lists %d)", stub.lists.Load())
	}
}

func TestTaskGitRefresh_ForgeErrorTypedWhenNoSnapshot(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(func(context.Context, string, ...string) (string, error) {
		return "", context.DeadlineExceeded
	})))
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	sendRequest(t, ec.ws, "1", "task.gitRefresh", map[string]any{"taskId": task.ID})
	resp := ec.nextResponse("1", 5*time.Second)
	if resp.Error == nil || resp.Error.Code != "forge_timeout" {
		t.Fatalf("error = %+v, want forge_timeout", resp.Error)
	}
}

func TestRefresh_RejectedForRunScoped(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	stub := &prListStub{branch: *task.Branch}
	api, _ := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(stub.run)))

	raw, _ := json.Marshal(map[string]any{"taskId": task.ID})
	_, err := api.hs["task.gitRefresh"](context.Background(), &requestContext{runScoped: true}, raw)
	if err == nil {
		t.Fatal("a run-scoped caller refreshed")
	}
	if c := codedError(err); c.Code != codeForbidden {
		t.Fatalf("code = %q, want %q", c.Code, codeForbidden)
	}
	if stub.repos.Load()+stub.lists.Load() != 0 {
		t.Fatal("a refused refresh called gh")
	}
	// The same call from a user surface works.
	if _, err := api.hs["task.gitRefresh"](context.Background(), &requestContext{}, raw); err != nil {
		t.Fatalf("user refresh: %v", err)
	}
}

// TestPRIdentity_AttachmentDoesNotSet: a `pr` attachment is context for the
// agent, never the task's own PR (ADR-0039 §2).
func TestPRIdentity_AttachmentDoesNotSet(t *testing.T) {
	t.Parallel()
	stub := &forgeStub{}
	e := newForgeEnv(t, "", stub)
	runID := e.startOK(t, map[string]any{"attachments": []any{prReq(nil)}})
	e.waitDone(t, runID)
	if stub.views.Load() != 1 {
		t.Fatalf("pr view ran %d times, want the one accept-time fetch", stub.views.Load())
	}
	got, err := e.db.GetTask(e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PRNumber != 0 || got.PRURL != "" || got.PRRepo != "" || got.PRForge != "" || got.PRHeadRef != "" {
		t.Fatalf("a pr attachment set the task's PR identity: %+v", got)
	}
	if _, ok, _ := e.db.GetTaskPRState(e.task.ID); ok {
		t.Fatal("a pr attachment created poller state")
	}
}
