package wsapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
)

// Throwaway repos with a local bare remote (prEnv serves its https `origin`
// from one through insteadOf): no network.

func (e *prEnv) push() (taskPushResult, error) {
	e.t.Helper()
	v, err := e.call("task.push", map[string]any{"taskId": e.task.ID})
	if err != nil {
		return taskPushResult{}, err
	}
	res, ok := v.(taskPushResult)
	if !ok {
		e.t.Fatalf("task.push result = %#v", v)
	}
	return res, nil
}

func (e *prEnv) update(params map[string]any) (taskUpdateFromBaseResult, error) {
	e.t.Helper()
	p := map[string]any{"taskId": e.task.ID}
	for k, v := range params {
		p[k] = v
	}
	v, err := e.call("task.updateFromBase", p)
	if err != nil {
		return taskUpdateFromBaseResult{}, err
	}
	res, ok := v.(taskUpdateFromBaseResult)
	if !ok {
		e.t.Fatalf("task.updateFromBase result = %#v", v)
	}
	return res, nil
}

// advanceBase adds a commit to origin/develop through the main checkout.
func (e *prEnv) advanceBase(path, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.repo, path), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	runGitT(e.t, e.repo, "add", path)
	runGitT(e.t, e.repo, "commit", "-m", "base moves: "+path)
	runGitT(e.t, e.repo, "push", "origin", "develop")
}

func (e *prEnv) parents() int {
	return len(strings.Fields(runGitOutputT(e.t, e.wt, "rev-list", "--parents", "-n", "1", "HEAD"))) - 1
}

func (e *prEnv) hasRef(ref string) bool {
	cmd := exec.Command("git", "rev-parse", "-q", "--verify", ref)
	cmd.Dir = e.wt
	return cmd.Run() == nil
}

func (e *prEnv) mergeHeadExists() bool { return e.hasRef("MERGE_HEAD") }

func (e *prEnv) upstream() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", e.branch+"@{upstream}")
	cmd.Dir = e.wt
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestPushPublishesBranch(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	if e.upstream() != "" {
		t.Fatal("fixture branch already has an upstream")
	}
	res, err := e.push()
	if err != nil {
		t.Fatalf("task.push: %v", err)
	}
	if res != (taskPushResult{Branch: e.branch, Remote: "origin", UpstreamCreated: true, Pushed: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if e.remoteSha(e.branch) != e.head() {
		t.Fatalf("origin's %s = %q, want %q", e.branch, e.remoteSha(e.branch), e.head())
	}
	if got := e.upstream(); got != "origin/"+e.branch {
		t.Fatalf("upstream = %q", got)
	}
	// A later push no longer creates the upstream.
	e.commit("b.txt", "b\n", "more")
	res, err = e.push()
	if err != nil || res.UpstreamCreated || res.Pushed != 1 {
		t.Fatalf("second push = %+v, %v", res, err)
	}
}

func TestPushAhead(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "one")
	if _, err := e.push(); err != nil {
		t.Fatal(err)
	}
	e.commit("b.txt", "b\n", "two")
	e.commit("c.txt", "c\n", "three")
	res, err := e.push()
	if err != nil || res.Pushed != 2 || res.UpstreamCreated {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if e.remoteSha(e.branch) != e.head() {
		t.Fatal("origin does not have the commits")
	}
	g, _ := e.api.git.Get(e.task.ID)
	if a := g.Actions["push"]; a.Enabled || a.Code != forge.CodeNothingToPush {
		t.Fatalf("push action after push = %+v", a)
	}
}

func TestPushNothingToPushRefused(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	_, err := e.push()
	wantErrCode(t, err, forge.CodeNothingToPush) // unpublished, no commits
	if e.remoteSha(e.branch) != "" {
		t.Fatal("a refused push published the branch")
	}
	e.commit("a.txt", "a\n", "one")
	if _, err := e.push(); err != nil {
		t.Fatal(err)
	}
	_, err = e.push()
	wantErrCode(t, err, forge.CodeNothingToPush) // published and current
}

func TestPushNonFastForward(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "one")
	if _, err := e.push(); err != nil {
		t.Fatal(err)
	}
	// Someone else moves origin's branch.
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, filepath.Dir(other), "clone", "-q", e.bare, other)
	runGitT(t, other, "config", "user.email", "o@example.com")
	runGitT(t, other, "config", "user.name", "O")
	runGitT(t, other, "checkout", "-q", e.branch)
	if err := os.WriteFile(filepath.Join(other, "theirs.txt"), []byte("t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, other, "add", "theirs.txt")
	runGitT(t, other, "commit", "-m", "theirs")
	runGitT(t, other, "push", "-q", "origin", e.branch)
	moved := e.remoteSha(e.branch)

	// The local view is stale, so the action allows it and git refuses.
	e.commit("b.txt", "b\n", "mine")
	_, err := e.push()
	r := wantErrCode(t, err, forge.CodeNonFastForward)
	if !strings.Contains(r.Message, "Bring those changes in first") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.remoteSha(e.branch) != moved {
		t.Fatal("a refused push moved origin")
	}
	// Once fetched, the action refuses with the same code before git runs.
	runGitT(t, e.wt, "fetch", "-q", "origin")
	_, err = e.push()
	wantErrCode(t, err, forge.CodeNonFastForward)
	if e.remoteSha(e.branch) != moved {
		t.Fatal("a refused push moved origin")
	}
}

func TestUpdateFromBaseMerge(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	e.advanceBase("base.txt", "base\n")
	res, err := e.update(nil)
	if err != nil || res != (taskUpdateFromBaseResult{Result: "merged", Commits: 1}) {
		t.Fatalf("updateFromBase = %+v, %v", res, err)
	}
	if e.parents() != 2 {
		t.Fatalf("HEAD has %d parents, want a merge commit", e.parents())
	}
	if _, err := os.Stat(filepath.Join(e.wt, "base.txt")); err != nil {
		t.Fatalf("base change not in the worktree: %v", err)
	}
	if e.mergeHeadExists() {
		t.Fatal("a clean merge left MERGE_HEAD")
	}
}

func TestUpdateFromBaseUpToDate(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	before := e.head()
	res, err := e.update(nil)
	if err != nil || res != (taskUpdateFromBaseResult{Result: "up_to_date"}) {
		t.Fatalf("updateFromBase = %+v, %v", res, err)
	}
	if e.head() != before {
		t.Fatal("up_to_date moved HEAD")
	}
	// A stale local view is not a refusal: the RPC fetches and answers.
	e.advanceBase("base.txt", "base\n")
	res, err = e.update(nil)
	if err != nil || res.Result != "merged" || res.Commits != 1 {
		t.Fatalf("after the base moved: %+v, %v", res, err)
	}
}

func (e *prEnv) conflict() {
	e.t.Helper()
	e.commit("a.txt", "task\n", "task a")
	e.advanceBase("a.txt", "base\n")
}

func TestUpdateFromBaseConflictLeavesMergeState(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.conflict()
	e.commit("z.txt", "z\n", "later")
	before := e.head()

	_, err := e.update(nil)
	r := wantErrCode(t, err, forge.CodeConflict)
	data, _ := r.Data.(map[string]any)
	files, _ := data["files"].([]string)
	if len(files) != 1 || files[0] != "a.txt" || data["operation"] != "merge" || data["base"] != "origin/develop" || data["filesTruncated"] != false {
		t.Fatalf("data = %#v", r.Data)
	}
	if !strings.Contains(r.Message, "`git merge --abort`") || !strings.Contains(r.Message, "`origin/develop`") {
		t.Fatalf("message = %q", r.Message)
	}
	// Nothing was aborted: the worktree is mid-merge with the conflict markers.
	if !e.mergeHeadExists() {
		t.Fatal("MERGE_HEAD is gone: the conflicted merge was aborted")
	}
	if e.head() != before {
		t.Fatal("HEAD moved")
	}
	b, _ := os.ReadFile(filepath.Join(e.wt, "a.txt"))
	if !strings.Contains(string(b), "<<<<<<<") {
		t.Fatalf("a.txt has no conflict markers: %q", b)
	}
	// task.git carries the conflict: the action now says why.
	g, _ := e.api.git.Get(e.task.ID)
	if a := g.Actions["updateFromBase"]; a.Enabled || a.Code != forge.CodeMergeInProgress {
		t.Fatalf("updateFromBase action = %+v", a)
	}
}

func TestUpdateFromBaseMergeInProgress(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.conflict()
	_, err := e.update(nil)
	wantErrCode(t, err, forge.CodeConflict)
	_, err = e.update(nil)
	r := wantErrCode(t, err, forge.CodeMergeInProgress)
	if data, _ := r.Data.(map[string]any); data["operation"] != "merge" {
		t.Fatalf("data = %#v", r.Data)
	}
	if !e.mergeHeadExists() {
		t.Fatal("the second call disturbed the merge")
	}
}

func TestUpdateFromBaseRebase(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	e.advanceBase("base.txt", "base\n")
	res, err := e.update(map[string]any{"strategy": "rebase"})
	if err != nil || res != (taskUpdateFromBaseResult{Result: "rebased", Commits: 1}) {
		t.Fatalf("updateFromBase = %+v, %v", res, err)
	}
	if e.parents() != 1 {
		t.Fatalf("HEAD has %d parents, want a linear history", e.parents())
	}
	if _, err := os.Stat(filepath.Join(e.wt, "base.txt")); err != nil {
		t.Fatalf("base change not in the worktree: %v", err)
	}
}

func TestUpdateFromBaseRebaseConflict(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.conflict()
	_, err := e.update(map[string]any{"strategy": "rebase"})
	r := wantErrCode(t, err, forge.CodeConflict)
	if data, _ := r.Data.(map[string]any); data["operation"] != "rebase" || !strings.Contains(r.Message, "`git rebase --abort`") {
		t.Fatalf("data = %#v, message = %q", r.Data, r.Message)
	}
	_, err = e.update(nil)
	r = wantErrCode(t, err, forge.CodeMergeInProgress)
	if data, _ := r.Data.(map[string]any); data["operation"] != "rebase" {
		t.Fatalf("data = %#v", r.Data)
	}
}

func TestUpdateFromBaseDefaultsToMerge(t *testing.T) {
	t.Parallel()
	for _, strategy := range []any{nil, "", "merge"} {
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "mine")
		e.advanceBase("base.txt", "base\n")
		params := map[string]any{}
		if strategy != nil {
			params["strategy"] = strategy
		}
		res, err := e.update(params)
		if err != nil || res.Result != "merged" || e.parents() != 2 {
			t.Fatalf("strategy %v: %+v, %v, %d parents", strategy, res, err, e.parents())
		}
	}
	e := newPREnv(t)
	_, err := e.update(map[string]any{"strategy": "squash"})
	wantErrCode(t, err, forge.CodeInvalidStrategy)
}

func TestUpdateFromBaseRefusals(t *testing.T) {
	t.Parallel()
	t.Run("run active", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "mine")
		e.advanceBase("base.txt", "base\n")
		runGitT(t, e.wt, "fetch", "-q", "origin")
		chats, err := e.wm.ListChats(e.task.ID, false)
		if err != nil || len(chats) == 0 {
			t.Fatalf("ListChats() = %+v, %v", chats, err)
		}
		if _, err := e.db.EnqueueChatPrompt(chats[0].ID, "waiting prompt", "{}", "human", 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		before := e.head()
		_, err = e.update(nil)
		wantErrCode(t, err, forge.CodeRunActive)
		if e.head() != before {
			t.Fatal("a refused update moved HEAD")
		}
	})
	t.Run("dirty", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "mine")
		e.advanceBase("base.txt", "base\n")
		runGitT(t, e.wt, "fetch", "-q", "origin")
		if err := os.WriteFile(filepath.Join(e.wt, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := e.head()
		_, err := e.update(nil)
		wantErrCode(t, err, forge.CodeWorktreeDirty)
		if e.head() != before {
			t.Fatal("a refused update moved HEAD")
		}
	})
	t.Run("push while a run is active is allowed", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "mine")
		if _, err := e.push(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSyncRPCsRefuseRunScoped(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	e.advanceBase("base.txt", "base\n")
	before := e.head()
	for _, m := range []string{"task.push", "task.updateFromBase"} {
		_, err := e.callAs(&requestContext{runScoped: true}, m, map[string]any{"taskId": e.task.ID})
		wantErrCode(t, err, codeForbidden)
	}
	if e.remoteSha(e.branch) != "" || e.head() != before {
		t.Fatal("a refused agent call pushed or changed the worktree")
	}
}

func TestSyncRPCsRegistered(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	for _, m := range []string{"task.push", "task.updateFromBase"} {
		if e.api.hs[m] == nil {
			t.Fatalf("%s is not registered", m)
		}
	}
}

// TestSyncNoSecretsInArgvOrErrors records every git command line through a
// shim on PATH, seeds a token-shaped string in a hook's stderr, and asserts the
// token reaches neither argv nor any wire error, and that no argv text does.
func TestSyncNoSecretsInArgvOrErrors(t *testing.T) { // not parallel: sets PATH
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	const token = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	log := filepath.Join(shimDir, "argv.log")
	shim := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q \"$@\"\n", log, realGit)
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	e := newPREnv(t)
	e.commit("a.txt", "a\n", "mine")
	hook := filepath.Join(e.bare, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'remote: denied for https://x-access-token:" + token + "@github.com/acme/repo.git' >&2\nexit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, perr := e.push()
	r := wantErrCode(t, perr, forge.CodePushRejected)
	b, _ := json.Marshal(r)
	for _, leak := range []string{token, "x-access-token:", "core.fsmonitor", "refs/heads/", "git push", "--set-upstream", "exit status"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("wire error leaks %q: %s", leak, b)
		}
	}
	if d := errDetail(r); !strings.Contains(d, "denied") {
		t.Fatalf("detail = %q, want the scrubbed stderr tail", d)
	}

	// A conflict error carries paths and sentences only.
	e2 := newPREnv(t)
	e2.conflict()
	_, cerr := e2.update(nil)
	cr := wantErrCode(t, cerr, forge.CodeConflict)
	cb, _ := json.Marshal(cr)
	for _, leak := range []string{"core.fsmonitor", "--no-edit", "--end-of-options", "exit status", "refs/remotes"} {
		if strings.Contains(string(cb), leak) {
			t.Fatalf("conflict error leaks %q: %s", leak, cb)
		}
	}

	logged, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var pushes int
	for _, line := range strings.Split(string(logged), "\n") {
		if strings.Contains(line, token) || strings.Contains(line, "x-access-token") {
			t.Fatalf("a token reached argv: %q", line)
		}
		if !strings.Contains(line, " push ") {
			continue
		}
		pushes++
		for _, bad := range []string{"--force", "-f ", "force-with-lease", "--mirror", "--delete", "+refs", ":+"} {
			if strings.Contains(line, bad) {
				t.Fatalf("push argv contains %q: %q", bad, line)
			}
		}
		if !strings.Contains(line, "origin refs/heads/"+e.branch+":refs/heads/"+e.branch) {
			t.Fatalf("push argv = %q, want the plain branch refspec", line)
		}
	}
	if pushes == 0 {
		t.Fatalf("shim recorded no push; log:\n%s", logged)
	}
}

func TestSyncPublishesTaskGit(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	ec := newEventConn(t, dialWS(t, newSyncServer(e), "tok"))
	ec.call("prime", "task.git", map[string]any{"taskId": e.task.ID}, nil)
	ec.subscribe("sub", TopicTaskGit)

	e.commit("a.txt", "a\n", "mine")
	if _, err := e.push(); err != nil {
		t.Fatal(err)
	}
	g := waitGit(t, ec, "published and level with origin", func(g gitstate.TaskGit) bool {
		return g.TaskID == e.task.ID && g.Origin != nil && g.Origin.Published && g.Origin.Ahead != nil && *g.Origin.Ahead == 0
	})
	if a := g.Actions["push"]; a.Enabled || a.Code != forge.CodeNothingToPush {
		t.Fatalf("push action = %+v", a)
	}

	e.advanceBase("base.txt", "base\n")
	if _, err := e.update(nil); err != nil {
		t.Fatal(err)
	}
	waitGit(t, ec, "merged the base in", func(g gitstate.TaskGit) bool {
		return g.TaskID == e.task.ID && g.Commits != nil && g.Commits.BehindBase != nil && *g.Commits.BehindBase == 0 &&
			g.Origin != nil && g.Origin.Ahead != nil && *g.Origin.Ahead > 0
	})
}

func newSyncServer(e *prEnv) *httptest.Server {
	srv := httptest.NewServer(e.api.Handler)
	e.t.Cleanup(srv.Close)
	return srv
}

func TestSyncNoForceInSource(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"sync_handlers.go", "../workspace/sync.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"--force", "force-with-lease", "--mirror", "--delete", "+refs"} {
			if strings.Contains(string(b), bad) {
				t.Fatalf("%s contains %q", f, bad)
			}
		}
	}
}

// publishAndRebase publishes one commit, moves the base on origin and rebases
// the (now published) branch onto it.
func (e *prEnv) publishAndRebase() {
	e.t.Helper()
	e.commit("a.txt", "a\n", "mine")
	if _, err := e.push(); err != nil {
		e.t.Fatal(err)
	}
	e.advanceBase("base.txt", "base\n")
	if res, err := e.update(map[string]any{"strategy": "rebase"}); err != nil || res.Result != "rebased" {
		e.t.Fatalf("updateFromBase = %+v, %v", res, err)
	}
}

func TestPushAfterRebaseReason(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.publishAndRebase()
	published := e.remoteSha(e.branch)

	g, err := e.api.git.Fresh(e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := g.Actions["push"]
	if a.Enabled || a.Code != forge.CodeNonFastForward || a.Reason != forge.MsgRebasedDiverged {
		t.Fatalf("push action = %+v", a)
	}
	_, err = e.push()
	r := wantErrCode(t, err, forge.CodeNonFastForward)
	if r.Message != forge.MsgRebasedDiverged {
		t.Fatalf("message = %q", r.Message)
	}
	if e.remoteSha(e.branch) != published {
		t.Fatal("a refused push moved origin")
	}
}

func TestPushOriginMovedKeepsOldSentence(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "one")
	if _, err := e.push(); err != nil {
		t.Fatal(err)
	}
	e.commit("b.txt", "b\n", "two")
	e.moveOrigin("theirs.txt")
	runGitT(t, e.wt, "fetch", "-q", "origin")
	g, _ := e.api.git.Fresh(e.task.ID)
	if a := g.Actions["push"]; a.Code != forge.CodeNonFastForward || !strings.Contains(a.Reason, "origin has newer changes") {
		t.Fatalf("push action = %+v", a)
	}
	_, err := e.push()
	if r := wantErrCode(t, err, forge.CodeNonFastForward); !strings.Contains(r.Message, "Bring those changes in first") {
		t.Fatalf("message = %q", r.Message)
	}

	// After a rebase the marker only holds while origin still has the tip it
	// had then: once someone else moves it, the plain sentence is back.
	e2 := newPREnv(t)
	e2.publishAndRebase()
	e2.moveOrigin("theirs.txt")
	runGitT(t, e2.wt, "fetch", "-q", "origin")
	g, _ = e2.api.git.Fresh(e2.task.ID)
	if a := g.Actions["push"]; a.Code != forge.CodeNonFastForward || a.Reason == forge.MsgRebasedDiverged {
		t.Fatalf("push action after origin moved = %+v", a)
	}
}

// moveOrigin has someone else add a commit to origin's copy of the task branch.
func (e *prEnv) moveOrigin(path string) {
	e.t.Helper()
	other := filepath.Join(e.t.TempDir(), "other")
	runGitT(e.t, filepath.Dir(other), "clone", "-q", e.bare, other)
	runGitT(e.t, other, "config", "user.email", "o@example.com")
	runGitT(e.t, other, "config", "user.name", "O")
	runGitT(e.t, other, "checkout", "-q", e.branch)
	if err := os.WriteFile(filepath.Join(other, path), []byte("t\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	runGitT(e.t, other, "add", path)
	runGitT(e.t, other, "commit", "-m", "theirs")
	runGitT(e.t, other, "push", "-q", "origin", e.branch)
}
