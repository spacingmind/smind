//go:build !windows

package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/store"
)

// prTaskEnv is a task started from pull request 7 (head branch feat/x) of a
// repo whose origin is a local bare repository.
type prTaskEnv struct {
	t    *testing.T
	api  *API
	bare string
	task store.Task
}

func newPRTaskEnv(t *testing.T, fork bool) *prTaskEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	runGitT(t, repo, "push", "origin", "HEAD:refs/pull/7/head")
	runGitT(t, repo, "push", "origin", "HEAD:refs/heads/feat/x")
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner((&prViewStub{fork: fork}).run)))
	var task store.Task
	newEventConn(t, dialWS(t, srv, "tok")).call("1", "task.create", map[string]any{"workspaceId": w.ID, "title": "PR task", "prNumber": 7}, &task)
	if task.PRNumber != 7 || task.PRHeadRef != "feat/x" || task.UntrustedSource != fork {
		t.Fatalf("task = pr %d head %q untrusted %v", task.PRNumber, task.PRHeadRef, task.UntrustedSource)
	}
	return &prTaskEnv{t: t, api: api, bare: bare, task: task}
}

func (e *prTaskEnv) commit(name string) string {
	e.t.Helper()
	wt := *e.task.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, name), []byte(name+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	runGitT(e.t, wt, "add", name)
	runGitT(e.t, wt, "commit", "-m", name)
	return strings.TrimSpace(runGitOutputT(e.t, wt, "rev-parse", "HEAD"))
}

func (e *prTaskEnv) push() (taskPushResult, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"taskId": e.task.ID})
	v, err := e.api.hs["task.push"](context.Background(), &requestContext{}, raw)
	if err != nil {
		return taskPushResult{}, err
	}
	return v.(taskPushResult), nil
}

func bareRef(bare, ref string) string {
	out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", "-q", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestPushPRStartTaskPushesToPRHead(t *testing.T) {
	t.Parallel()
	e := newPRTaskEnv(t, false)
	head := e.commit("a.txt")
	res, err := e.push()
	if err != nil {
		t.Fatalf("task.push: %v", err)
	}
	if res.Branch != "feat/x" || res.Remote != "origin" || res.Pushed != 1 {
		t.Fatalf("result = %+v, want feat/x on origin with 1 commit", res)
	}
	if got := bareRef(e.bare, "refs/heads/feat/x"); got != head {
		t.Fatalf("origin's feat/x = %q, want %q", got, head)
	}
	if got := bareRef(e.bare, "refs/heads/"+*e.task.Branch); got != "" {
		t.Fatalf("origin got the task's own branch (%s)", got)
	}
}

func TestPushForkPRRefused(t *testing.T) {
	t.Parallel()
	e := newPRTaskEnv(t, true)
	before := bareRef(e.bare, "refs/heads/feat/x")
	e.commit("a.txt")

	g, err := e.api.git.Fresh(e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.Actions["push"]; a.Enabled || a.Code != forge.CodeForkPRReadonly || a.Reason != forge.MsgForkPushReadonly {
		t.Fatalf("push action = %+v", a)
	}
	_, err = e.push()
	if r := wantErrCode(t, err, forge.CodeForkPRReadonly); r.Message != forge.MsgForkPushReadonly {
		t.Fatalf("message = %q", r.Message)
	}
	if bareRef(e.bare, "refs/heads/feat/x") != before || bareRef(e.bare, "refs/heads/"+*e.task.Branch) != "" {
		t.Fatal("a refused push changed origin")
	}
}

func TestPushHonoursPushRemote(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	forkBare := t.TempDir()
	runGitT(t, forkBare, "init", "--bare")
	runGitT(t, e.repo, "remote", "add", "fork", forkBare)
	runGitT(t, e.wt, "config", "branch."+e.branch+".pushRemote", "fork")
	e.commit("a.txt", "a\n", "mine")

	res, err := e.push()
	if err != nil {
		t.Fatalf("task.push: %v", err)
	}
	if res.Remote != "fork" || res.Branch != e.branch || !res.UpstreamCreated || res.Pushed != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := bareRef(forkBare, "refs/heads/"+e.branch); got != e.head() {
		t.Fatalf("fork's branch = %q, want %q", got, e.head())
	}
	if e.remoteSha(e.branch) != "" {
		t.Fatal("origin received a push meant for the fork remote")
	}
	if up := e.upstream(); up != "fork/"+e.branch {
		t.Fatalf("upstream = %q", up)
	}

	// remote.pushDefault applies when the branch names none; a name that is
	// not a remote of the repository (or looks like an option) is refused.
	runGitT(t, e.wt, "config", "--unset", "branch."+e.branch+".pushRemote")
	for _, bad := range []string{"nope", "--upload-pack=touch /tmp/x"} {
		runGitT(t, e.wt, "config", "remote.pushDefault", bad)
		_, err = e.push()
		wantErrCode(t, err, forge.CodeNoRemote)
	}
	if e.remoteSha(e.branch) != "" {
		t.Fatal("a refused push reached origin")
	}
}

func TestPushNormalTaskUnchanged(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	// A PR identity whose head is smind's throwaway rebased copy is never a
	// push target: the task's own branch is.
	e.seedPR(5, forge.CherryPickBranchPrefix+"1", nil)
	res, err := e.push()
	if err != nil {
		t.Fatal(err)
	}
	if res != (taskPushResult{Branch: e.branch, Remote: "origin", UpstreamCreated: true, Pushed: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if e.remoteSha(e.branch) != e.head() {
		t.Fatal("origin does not have the task's branch")
	}
}
