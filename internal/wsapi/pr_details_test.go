package wsapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/ghctx"
)

const detailsPRView = `{"number":7,"title":"Fix","url":"https://github.com/acme/repo/pull/7","state":"OPEN","isDraft":false,
"mergeable":"MERGEABLE","reviewDecision":"","body":"b","reviews":[],"reviewRequests":[],"statusCheckRollup":[
{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://github.com/acme/repo/actions/runs/11/job/22"}]}`

const detailsPRThreads = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"totalCount":0,"nodes":[]},"baseRef":null}}}}`

// detailsGH answers the three gh calls of the PR panel and counts them; fail,
// when set, is returned for every call after `gh repo view`.
type detailsGH struct {
	mu    sync.Mutex
	calls map[string]int
	fail  error
}

func (g *detailsGH) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.calls == nil {
		g.calls = map[string]int{}
	}
	key := args[0]
	if len(args) > 1 {
		key += " " + args[1]
	}
	g.calls[key]++
	if args[0] == "repo" {
		return `{"nameWithOwner":"acme/repo","url":"https://github.com/acme/repo"}`, nil
	}
	if g.fail != nil {
		return "", g.fail
	}
	switch key {
	case "pr view":
		return detailsPRView, nil
	case "api graphql":
		return detailsPRThreads, nil
	case "run view":
		return "boom", nil
	}
	return "", errors.New("unexpected gh call " + strings.Join(args, " "))
}

func (g *detailsGH) count(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls[key]
}

func newDetailsEnv(t *testing.T, withPR bool) (*prEnv, *detailsGH, *requestContext) {
	t.Helper()
	e := newPREnv(t)
	gh := &detailsGH{}
	e.api.SetGithubClient(ghctx.New(ghctx.WithRunner(gh.run)))
	if withPR {
		if err := e.db.SetTaskPR(e.task.ID, "github", "acme/repo", 7, "https://github.com/acme/repo/pull/7", e.branch); err != nil {
			t.Fatal(err)
		}
	}
	return e, gh, &requestContext{conn: &conn{}}
}

func TestTaskPRDetails_RunScopedRefused(t *testing.T) {
	t.Parallel()
	e, gh, _ := newDetailsEnv(t, true)
	rs := &requestContext{runScoped: true, conn: &conn{}}
	for method, params := range map[string]map[string]any{
		"task.prDetailsFocus": {"taskId": e.task.ID},
		"task.prDetails":      {"taskId": e.task.ID},
		"task.prCheckLog":     {"taskId": e.task.ID, "checkId": "run:11:job:22"},
	} {
		_, err := e.callAs(rs, method, params)
		wantErrCode(t, err, codeForbidden)
	}
	if len(gh.calls) != 0 || e.api.prFocus.has(rs.conn, e.task.ID) {
		t.Fatalf("a refused call reached gh %v or the focus registry", gh.calls)
	}
}

func TestTaskPRDetails_NoPR(t *testing.T) {
	t.Parallel()
	e, gh, rc := newDetailsEnv(t, false)
	if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, "no_pr")
	_, err = e.callAs(rc, "task.prCheckLog", map[string]any{"taskId": e.task.ID, "checkId": "run:1"})
	wantErrCode(t, err, "no_pr")
	if len(gh.calls) != 0 {
		t.Fatalf("gh called for a task without a PR: %v", gh.calls)
	}
}

func TestTaskPRDetails_FocusGateAndCheckLog(t *testing.T) {
	t.Parallel()
	e, gh, rc := newDetailsEnv(t, true)
	// Not focused: no fetch, flagged.
	res, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mustJSON(t, res)); !strings.Contains(got, `"stale":"not_focused"`) || len(gh.calls) != 0 {
		t.Fatalf("unfocused read = %s, gh calls %v", got, gh.calls)
	}
	// Focused on another task is still not focused for this one.
	if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": e.task.ID + 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID}); err != nil || len(gh.calls) != 0 {
		t.Fatalf("read for another task's focus: err %v gh %v", err, gh.calls)
	}
	if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatal(err)
	}
	res, err = e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := string(mustJSON(t, res))
	if !strings.Contains(got, `"number":7`) || !strings.Contains(got, `"id":"run:11:job:22"`) || gh.count("pr view") != 1 || gh.count("api graphql") != 1 {
		t.Fatalf("focused read = %s, gh calls %v", got, gh.calls)
	}
	if gh.count("run view") != 0 {
		t.Fatal("a read fetched a log")
	}
	res, err = e.callAs(rc, "task.prCheckLog", map[string]any{"taskId": e.task.ID, "checkId": "run:11:job:22"})
	if err != nil || !strings.Contains(string(mustJSON(t, res)), `"text":"boom"`) || gh.count("run view") != 1 {
		t.Fatalf("check log = %v, %v, gh %v", res, err, gh.calls)
	}
	_, err = e.callAs(rc, "task.prCheckLog", map[string]any{"taskId": e.task.ID, "checkId": "run:1;x"})
	wantErrCode(t, err, "check_unknown")
	if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": nil}); err != nil {
		t.Fatal(err)
	}
	if e.api.prFocus.has(rc.conn, e.task.ID) {
		t.Fatal("focus not cleared by null")
	}
}

func TestTaskPRDetailsFocus_DroppedOnDisconnect(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	api, srv := newGitTestAPI(t, wm, db, nil)
	ws := dialWS(t, srv, "tok")
	ec := newEventConn(t, ws)

	ec.call("1", "task.prDetailsFocus", map[string]any{"taskId": task.ID}, nil)
	if got := api.prFocus.ids(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("prFocus after task.prDetailsFocus = %v, want [%d]", got, task.ID)
	}
	if got := api.focus.ids(); len(got) != 0 {
		t.Fatalf("task.gitFocus registry changed: %v", got)
	}
	ws.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(api.prFocus.ids()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("pr focus not dropped after disconnect: %v", api.prFocus.ids())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTaskPRDetails_TypedErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		code string
	}{
		"unauthenticated": {errors.New("gh: exit status 1: To get started with GitHub CLI, please run:  gh auth login"), "forge_unauthenticated"},
		"rate limited":    {errors.New("gh: exit status 1: HTTP 429: API rate limit exceeded"), "forge_rate_limited"},
		"not found":       {errors.New("gh: exit status 1: Could not resolve to a PullRequest with the number of 7."), "forge_not_found"},
		"cli missing":     {errors.New("exec: \"gh\": executable file not found in $PATH"), "forge_cli_missing"},
		"timeout":         {context.DeadlineExceeded, "forge_timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, gh, rc := newDetailsEnv(t, true)
			gh.fail = tc.err
			if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": e.task.ID}); err != nil {
				t.Fatal(err)
			}
			_, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID})
			wantErrCode(t, err, tc.code)
			if tc.code == "forge_rate_limited" {
				if r := codedError(err); r.Data == nil {
					t.Fatalf("rate limit carries no retryAt: %+v", r)
				}
			}
		})
	}

	// Over the explicit-refresh floor: refresh_limited, with a retryAt.
	e, _, rc := newDetailsEnv(t, true)
	if _, err := e.callAs(rc, "task.prDetailsFocus", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.callAs(rc, "task.prDetails", map[string]any{"taskId": e.task.ID, "refresh": true})
	wantErrCode(t, err, "refresh_limited")
}
