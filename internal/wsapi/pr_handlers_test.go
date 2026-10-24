package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// ghStub plays the gh CLI for task.prPreview / createPr / updatePr: `repo
// view`, `pr view` and (on the stdin runner) `pr create`, recording every call.
type ghStub struct {
	mu        sync.Mutex
	plain     [][]string    // argv of every plain call
	creates   []ghCreate    // every `pr create`
	nextNum   int           // PR number the next create returns
	createErr error         // returned by `pr create` when set
	repoErr   error         // returned by `repo view` when set
	viewHead  string        // headRefOid `pr view` reports
	viewState string        // state `pr view` reports (default OPEN)
	listOut   string        // stdout of `pr list` (default no PRs)
	listErr   error         // returned by `pr list` when set
	gate      chan struct{} // `pr create` blocks until closed when non-nil
	entered   chan struct{} // signalled when a `pr create` is entered
}

type ghCreate struct {
	dir   string
	args  []string
	stdin string
}

func newGhStub() *ghStub { return &ghStub{nextNum: 7, viewHead: "abc", viewState: "OPEN"} }

func (g *ghStub) client() *ghctx.Client {
	return ghctx.New(ghctx.WithRunner(g.run), ghctx.WithStdinRunner(g.runIn))
}

func (g *ghStub) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.plain = append(g.plain, append([]string(nil), args...))
	switch {
	case args[0] == "repo":
		if g.repoErr != nil {
			return "", g.repoErr
		}
		return `{"nameWithOwner":"acme/repo","url":"https://github.com/acme/repo"}`, nil
	case args[0] == "pr" && args[1] == "list":
		if g.listErr != nil {
			return "", g.listErr
		}
		if g.listOut == "" {
			return "[]", nil
		}
		return g.listOut, nil
	case args[0] == "pr" && args[1] == "view":
		n, _ := strconv.Atoi(args[2])
		return fmt.Sprintf(`{"number":%d,"url":"https://github.com/acme/repo/pull/%d","title":"t","state":%q,"isDraft":true,`+
			`"headRefName":"h","headRefOid":%q,"baseRefName":"develop","isCrossRepository":false,"mergeable":"MERGEABLE",`+
			`"reviewDecision":"","statusCheckRollup":[]}`, n, n, g.viewState, g.viewHead), nil
	}
	return "", errors.New("unexpected gh call " + strings.Join(args, " "))
}

func (g *ghStub) runIn(_ context.Context, dir, stdin string, args ...string) (string, error) {
	g.mu.Lock()
	g.creates = append(g.creates, ghCreate{dir: dir, args: append([]string(nil), args...), stdin: stdin})
	err, n, gate, entered := g.createErr, g.nextNum, g.gate, g.entered
	g.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/acme/repo/pull/%d\n", n), nil
}

func (g *ghStub) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.plain) + len(g.creates)
}

func (g *ghStub) plainCalls(first string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, a := range g.plain {
		if a[0] == first {
			n++
		}
	}
	return n
}

func (g *ghStub) createdArgs(i int) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.creates[i].args
}

// prEnv is a workspace whose `origin` is an https GitHub URL that git
// transparently serves from a local bare repository (insteadOf), one task with
// its own worktree, and a daemon API wired to a stub gh.
type prEnv struct {
	t      *testing.T
	wm     *workspace.Manager
	db     *store.Store
	api    *API
	gh     *ghStub
	repo   string
	bare   string
	task   store.Task
	branch string
	wt     string
}

const originURL = "https://github.com/acme/repo.git"

func newPREnv(t *testing.T) *prEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", originURL)
	runGitT(t, repo, "config", "url."+bare+".insteadOf", originURL)
	runGitT(t, repo, "push", "origin", "develop")
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Add feature")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	api, _ := newGitTestAPI(t, wm, db, nil)
	gh := newGhStub()
	api.SetGithubClient(gh.client())
	return &prEnv{t: t, wm: wm, db: db, api: api, gh: gh, repo: repo, bare: bare, task: task, branch: *task.Branch, wt: *task.WorktreePath}
}

func (e *prEnv) commit(path, content, msg string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.wt, path), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	runGitT(e.t, e.wt, "add", path)
	runGitT(e.t, e.wt, "commit", "-m", msg)
}

func (e *prEnv) remoteSha(branch string) string {
	cmd := exec.Command("git", "--git-dir", e.bare, "rev-parse", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (e *prEnv) head() string {
	return strings.TrimSpace(runGitOutputT(e.t, e.wt, "rev-parse", "HEAD"))
}

// call invokes a handler directly as a user surface.
func (e *prEnv) call(method string, params map[string]any) (any, error) {
	return e.callAs(&requestContext{}, method, params)
}

func (e *prEnv) callAs(rc *requestContext, method string, params map[string]any) (any, error) {
	e.t.Helper()
	raw, _ := json.Marshal(params)
	return e.api.hs[method](context.Background(), rc, raw)
}

func (e *prEnv) create(extra map[string]any) (any, error) {
	e.t.Helper()
	p := map[string]any{"taskId": e.task.ID, "title": "Add a feature", "body": "Body text", "draft": true}
	for k, v := range extra {
		p[k] = v
	}
	return e.call("task.createPr", p)
}

// wantCode asserts err carries the wire code and returns its wire shape.
func wantErrCode(t *testing.T, err error, code string) *rpcError {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	r := codedError(err)
	if r.Code != code {
		t.Fatalf("error = %q (code %q), want code %q", err, r.Code, code)
	}
	return r
}

func errDetail(r *rpcError) string {
	if m, ok := r.Data.(map[string]any); ok {
		d, _ := m["detail"].(string)
		return d
	}
	return ""
}

// seedPR gives the task a stored PR identity and an open snapshot, as the
// poller would after adopting it.
func (e *prEnv) seedPR(number int, head string, mod func(*forge.PR)) forge.PR {
	e.t.Helper()
	pr := forge.PR{
		Number: number, Repo: "acme/repo", URL: fmt.Sprintf("https://github.com/acme/repo/pull/%d", number), Title: "t",
		State: forge.PRStateOpen, HeadRef: head, HeadSha: "abc", BaseRef: "develop",
		Mergeable: forge.MergeableYes, ReviewDecision: forge.ReviewNone, FetchedAt: time.Now().UTC(),
	}
	if mod != nil {
		mod(&pr)
	}
	if err := e.db.SetTaskPR(e.task.ID, "github", "acme/repo", int64(number), pr.URL, head); err != nil {
		e.t.Fatal(err)
	}
	b, _ := json.Marshal(pr)
	if err := e.db.SaveTaskPRState(store.TaskPRState{TaskID: e.task.ID, Snapshot: string(b), FetchedAt: time.Now().UTC()}); err != nil {
		e.t.Fatal(err)
	}
	return pr
}

func decodePreview(t *testing.T, v any) prPreviewResult {
	t.Helper()
	res, ok := v.(prPreviewResult)
	if !ok {
		t.Fatalf("result is %T, want prPreviewResult", v)
	}
	return res
}

// --- task.prPreview ---

func TestPrPreview_CommitsAndUncommitted(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "first change")
	e.commit("b.txt", "b\n", "second change")
	// staged, unstaged and untracked work: reported, never part of the commits
	if err := os.WriteFile(filepath.Join(e.wt, "staged.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, e.wt, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(e.wt, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"u1.txt", "u2.txt"} {
		if err := os.WriteFile(filepath.Join(e.wt, n), []byte("u\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	v, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatalf("task.prPreview: %v", err)
	}
	res := decodePreview(t, v)
	if want := strings.TrimSpace(runGitOutputT(t, e.repo, "rev-parse", "refs/remotes/origin/develop")); res.Base != (prRef{Ref: "develop", Sha: want}) {
		t.Fatalf("base = %+v, want develop@%s", res.Base, want)
	}
	if res.Head != (prHead{Branch: e.branch, Ref: e.branch, Strategy: "direct"}) {
		t.Fatalf("head = %+v", res.Head)
	}
	if res.CommitCount != 2 || len(res.Commits) != 2 || res.Commits[0].Subject != "first change" || res.Commits[1].Subject != "second change" || res.Commits[1].Sha != e.head() {
		t.Fatalf("commits = %+v (count %d), want oldest first", res.Commits, res.CommitCount)
	}
	if res.Uncommitted != (prUncommitted{Staged: 1, Unstaged: 1, Untracked: 2}) {
		t.Fatalf("uncommitted = %+v", res.Uncommitted)
	}
	// the stat is the pull request's own change (two committed files), not the worktree's
	if res.Diff == nil || *res.Diff != (prDiffStat{Files: 2, Insertions: 2, Deletions: 0}) {
		t.Fatalf("diff = %+v, want the committed change only (2 files, +2 -0)", res.Diff)
	}
	if len(res.Blockers) != 0 || !res.Defaults.Draft || res.Defaults.Title != "Add feature" || res.ExistingPR != nil {
		t.Fatalf("blockers %+v defaults %+v existing %+v", res.Blockers, res.Defaults, res.ExistingPR)
	}
	if e.gh.calls() != 0 {
		t.Fatalf("preview called gh %d times; it is local git and cache only", e.gh.calls())
	}
	// JSON never has null lists: the dialog can map them.
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), `"commits":null`) || strings.Contains(string(b), `"blockers":null`) {
		t.Fatalf("preview JSON has null lists: %s", b)
	}
}

func TestPrPreview_DiverganceIsCherryPickNoFetch(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	// Unpushed work on the checkout, then a task forked from it.
	if err := os.WriteFile(filepath.Join(e.repo, "unrelated.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, e.repo, "add", "unrelated.txt")
	runGitT(t, e.repo, "commit", "-m", "unrelated")
	task, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "Diverged")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.SetTaskBase(task.ID, "develop", strings.TrimSpace(runGitOutputT(t, *task.WorktreePath, "rev-parse", "HEAD"))); err != nil {
		t.Fatal(err)
	}
	e.task, e.branch, e.wt = task, *task.Branch, *task.WorktreePath
	e.commit("tw.txt", "t\n", "task work")
	if err := os.RemoveAll(e.bare); err != nil { // a preview that fetched would fail
		t.Fatal(err)
	}

	v, err := e.call("task.prPreview", map[string]any{"taskId": task.ID})
	if err != nil {
		t.Fatalf("task.prPreview: %v", err)
	}
	res := decodePreview(t, v)
	if res.Head.Strategy != "cherry-pick" || res.Head.Branch != "smind/pr-task-"+strconv.FormatInt(task.ID, 10) || res.Head.Ref != e.branch {
		t.Fatalf("head = %+v", res.Head)
	}
	if res.CommitCount != 1 || res.Commits[0].Subject != "task work" {
		t.Fatalf("commits = %+v", res.Commits)
	}
}

func TestPrPreview_DefaultBodyDeterministic(t *testing.T) {
	t.Parallel()
	// Pure function: twenty bullets then "…and N more", the footer last.
	var commits []workspace.PRCommit
	for i := 1; i <= 25; i++ {
		commits = append(commits, workspace.PRCommit{SHA: strings.Repeat("a", 40), Subject: "change " + strconv.Itoa(i)})
	}
	got := defaultPRBody(commits, 25, 12)
	var want strings.Builder
	want.WriteString("## Summary\n")
	for i := 1; i <= 20; i++ {
		want.WriteString("- change " + strconv.Itoa(i) + "\n")
	}
	want.WriteString("- …and 5 more\n\nOpened from smind task #12.")
	if got != want.String() {
		t.Fatalf("body =\n%s\nwant\n%s", got, want.String())
	}
	if got != defaultPRBody(commits, 25, 12) {
		t.Fatal("body is not deterministic")
	}
	if g := defaultPRBody(commits[:2], 2, 3); g != "## Summary\n- change 1\n- change 2\n\nOpened from smind task #3." {
		t.Fatalf("short body = %q", g)
	}
	// A subject with control characters or a huge length cannot break the limits.
	long := defaultPRBody([]workspace.PRCommit{{Subject: "a\x00b\nc" + strings.Repeat("x", 500)}}, 1, 1)
	if strings.ContainsRune(long, 0) || len(long) > 400 {
		t.Fatalf("subject not cleaned/clipped: %d bytes", len(long))
	}

	for in, want := range map[string]string{
		"":                       "smind task 9",
		"  \n\t ":                "smind task 9",
		"Fix\nthe   bug\x00 now": "Fix the bug now",
		strings.Repeat("é", 300): strings.Repeat("é", 255) + "…",
		"Add a feature":          "Add a feature",
		"Line one\r\nLine two":   "Line one Line two",
		" weird separator ":      "weird separator",
	} {
		if got := defaultPRTitle(in, 9); got != want {
			t.Errorf("defaultPRTitle(%q) = %q, want %q", in, got, want)
		}
	}

	// And end to end: the preview of a real task carries it.
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "only change")
	v, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if body := decodePreview(t, v).Defaults.Body; body != fmt.Sprintf("## Summary\n- only change\n\nOpened from smind task #%d.", e.task.ID) {
		t.Fatalf("default body = %q", body)
	}
}

func TestPrPreview_ExistingPrCacheFirst(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(7, e.branch, nil)

	v, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatalf("task.prPreview: %v", err)
	}
	res := decodePreview(t, v)
	if res.ExistingPR == nil || res.ExistingPR.Number != 7 || res.ExistingPR.URL != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("existingPr = %+v, want the cached #7", res.ExistingPR)
	}
	if len(res.Blockers) != 1 || res.Blockers[0].Code != "pr_exists" {
		t.Fatalf("blockers = %+v, want pr_exists", res.Blockers)
	}
	if res.Forge == nil || res.Forge.Name != "github" || res.Forge.Repo != "acme/repo" || res.Forge.Host != "github.com" {
		t.Fatalf("forge = %+v", res.Forge)
	}
	if e.gh.calls() != 0 {
		t.Fatalf("preview called gh %d times; the PR half is cache-first", e.gh.calls())
	}
}

func TestPrPreview_BlockersAreTheActionReason(t *testing.T) {
	t.Parallel()
	e := newPREnv(t) // no commits yet
	v, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	res := decodePreview(t, v)
	if len(res.Blockers) != 1 || res.Blockers[0].Code != "no_commits" || !strings.Contains(res.Blockers[0].Reason, "no commits beyond `develop`") {
		t.Fatalf("blockers = %+v, want no_commits", res.Blockers)
	}

	// No usable origin (U7): the same sentence the action carries.
	runGitT(t, e.repo, "config", "remote.origin.url", e.bare)
	e.commit("a.txt", "a\n", "change")
	v, err = e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	res = decodePreview(t, v)
	if len(res.Blockers) != 1 || res.Blockers[0].Code != "forge_no_remote" {
		t.Fatalf("blockers = %+v, want forge_no_remote", res.Blockers)
	}
	g, err := e.api.git.Get(e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.Actions["createPr"]; a.Reason != res.Blockers[0].Reason {
		t.Fatalf("action reason %q != blocker %q", a.Reason, res.Blockers[0].Reason)
	}

	// A task without a base commit still previews, with the blocker and nothing local.
	if err := e.db.SetTaskBase(e.task.ID, "develop", ""); err != nil {
		t.Fatal(err)
	}
	v, err = e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatalf("preview with no base: %v", err)
	}
	if res = decodePreview(t, v); len(res.Blockers) != 1 || res.Blockers[0].Code != "base_unknown" || len(res.Commits) != 0 {
		t.Fatalf("no base: %+v", res)
	}

	if _, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID + 999}); err == nil {
		t.Fatal("preview of a missing task succeeded")
	}
	if _, err := e.call("task.prPreview", map[string]any{"taskId": e.task.ID, "baseBranch": "a..b"}); err == nil {
		t.Fatal("preview with an invalid base succeeded")
	}
}

// --- task.createPr ---

func TestCreatePr_Draft(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")

	v, err := e.create(map[string]any{"draft": true})
	if err != nil {
		t.Fatalf("task.createPr: %v", err)
	}
	res, ok := v.(prCreateResult)
	if !ok || res.PR.Number != 7 || res.PR.URL != "https://github.com/acme/repo/pull/7" || res.PR.Repo != "acme/repo" {
		t.Fatalf("result = %#v", v)
	}
	args := e.gh.createdArgs(0)
	want := []string{"pr", "create", "--base", "develop", "--head", e.branch, "--title", "Add a feature", "--draft", "--body-file", "-"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("gh argv = %v, want %v", args, want)
	}
	// Pushed, identity stored, task.git carries the PR.
	if e.remoteSha(e.branch) != e.head() {
		t.Fatalf("origin's %s = %q, want the local tip", e.branch, e.remoteSha(e.branch))
	}
	got, _ := e.db.GetTask(e.task.ID)
	if got.PRForge != "github" || got.PRRepo != "acme/repo" || got.PRNumber != 7 || got.PRURL != "https://github.com/acme/repo/pull/7" || got.PRHeadRef != e.branch {
		t.Fatalf("stored identity = %+v", got)
	}
	g, err := e.api.git.Get(e.task.ID)
	if err != nil || g.PR == nil || g.PR.Number != 7 || g.Forge != "github" {
		t.Fatalf("task.git after create: %+v, %v", g.PR, err)
	}
	if a := g.Actions["createPr"]; a.Code != "pr_exists" {
		t.Fatalf("createPr after create = %+v, want pr_exists", a)
	}
	if a := g.Actions["updatePr"]; !a.Enabled {
		t.Fatalf("updatePr after create = %+v, want enabled", a)
	}

	// Not a draft: no flag.
	e2 := newPREnv(t)
	e2.commit("a.txt", "a\n", "change")
	if _, err := e2.create(map[string]any{"draft": false}); err != nil {
		t.Fatal(err)
	}
	for _, a := range e2.gh.createdArgs(0) {
		if a == "--draft" {
			t.Fatalf("a non-draft create passed --draft: %v", e2.gh.createdArgs(0))
		}
	}
	// The call is booked against the host budget: create + view.
	if calls, _, _ := e.db.GetForgeBudget("github.com", time.Now()); calls < 2 {
		t.Fatalf("budget = %d, want the 2 explicit calls booked", calls)
	}
}

func TestCreatePr_BodyOnStdinNeverArgv(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	body := "## Summary\n- \"quoted\" $(touch /tmp/pwned) `x`; rm -rf /\n\nSECRET-BODY-MARKER"
	if _, err := e.create(map[string]any{"body": body, "title": "T; $(evil)"}); err != nil {
		t.Fatalf("task.createPr: %v", err)
	}
	e.gh.mu.Lock()
	defer e.gh.mu.Unlock()
	if len(e.gh.creates) != 1 || e.gh.creates[0].stdin != body {
		t.Fatalf("creates = %+v, want the body verbatim on stdin", e.gh.creates)
	}
	for _, a := range e.gh.creates[0].args {
		if strings.Contains(a, "SECRET-BODY-MARKER") || strings.Contains(a, "rm -rf") {
			t.Fatalf("body in argv: %v", e.gh.creates[0].args)
		}
	}
	if e.gh.creates[0].dir != e.repo {
		t.Fatalf("gh ran in %q, want the workspace repo %q", e.gh.creates[0].dir, e.repo)
	}
}

func TestCreatePr_TitleBodyValidation(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	cases := []struct {
		name  string
		extra map[string]any
		code  string
	}{
		{"empty title", map[string]any{"title": ""}, "invalid_title"},
		{"blank title", map[string]any{"title": "  \t "}, "invalid_title"},
		{"newline title", map[string]any{"title": "a\nb"}, "invalid_title"},
		{"CR title", map[string]any{"title": "a\rb"}, "invalid_title"},
		{"NUL title", map[string]any{"title": "a\x00b"}, "invalid_title"},
		{"257 runes", map[string]any{"title": strings.Repeat("é", 257)}, "invalid_title"},
		{"NUL body", map[string]any{"body": "a\x00b"}, "invalid_body"},
		{"64 KiB + 1 body", map[string]any{"body": strings.Repeat("x", 64<<10+1)}, "invalid_body"},
		{"invalid base", map[string]any{"baseBranch": "a..b"}, "invalid_branch"},
		{"option-like base", map[string]any{"baseBranch": "--upload-pack=x"}, "invalid_branch"},
		{"long base", map[string]any{"baseBranch": strings.Repeat("b", 300)}, "invalid_branch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.create(tc.extra)
			wantErrCode(t, err, tc.code)
		})
	}
	if e.gh.calls() != 0 || e.remoteSha(e.branch) != "" {
		t.Fatalf("a refused request reached gh (%d calls) or pushed (%q)", e.gh.calls(), e.remoteSha(e.branch))
	}
	// The limits themselves are valid: 256 runes, exactly 64 KiB.
	if _, err := e.create(map[string]any{"title": strings.Repeat("é", 256), "body": strings.Repeat("x", 64<<10)}); err != nil {
		t.Fatalf("limit-sized input refused: %v", err)
	}
}

func TestCreatePr_PrExists_TypedAndAdopts(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(7, e.branch, nil)

	_, err := e.create(nil)
	r := wantErrCode(t, err, "pr_exists")
	data, _ := r.Data.(map[string]any)
	if data["url"] != "https://github.com/acme/repo/pull/7" || data["number"] != 7 {
		t.Fatalf("data = %#v, want {url, number}", r.Data)
	}
	if r.Message != "A pull request is already open for this branch." {
		t.Fatalf("message = %q", r.Message)
	}
	if len(e.gh.creates) != 0 || e.gh.calls() != 0 {
		t.Fatalf("gh ran for a task that already has an open PR: %+v", e.gh.creates)
	}
	if e.remoteSha(e.branch) != "" {
		t.Fatal("the branch was pushed for a task that already has an open PR")
	}
	// The existing PR stays the task's: identity unchanged, still served.
	got, _ := e.db.GetTask(e.task.ID)
	g, _ := e.api.git.Get(e.task.ID)
	if got.PRNumber != 7 || g.PR == nil || g.PR.Number != 7 {
		t.Fatalf("identity %+v pr %+v", got, g.PR)
	}
}

func TestCreatePr_NoCommits_Code(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	_, err := e.create(nil)
	r := wantErrCode(t, err, "no_commits")
	if !strings.Contains(r.Message, "no commits beyond `develop`") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.gh.calls() != 0 || e.remoteSha(e.branch) != "" {
		t.Fatal("a refused create reached gh or pushed")
	}
}

func TestCreatePr_RunActive_Code(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	chats, err := e.wm.ListChats(e.task.ID, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("ListChats() = %+v, %v", chats, err)
	}
	if _, err := e.db.EnqueueChatPrompt(chats[0].ID, "waiting prompt", "{}", "human", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	_, err = e.create(nil)
	r := wantErrCode(t, err, "run_active")
	if !strings.Contains(r.Message, "agent is still working") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.gh.calls() != 0 || e.remoteSha(e.branch) != "" {
		t.Fatal("a refused create reached gh or pushed")
	}
	// The preview reports the same blocker.
	v, _ := e.call("task.prPreview", map[string]any{"taskId": e.task.ID})
	if res := decodePreview(t, v); len(res.Blockers) != 1 || res.Blockers[0].Code != "run_active" {
		t.Fatalf("preview blockers = %+v", res.Blockers)
	}
}

func TestCreatePr_NoOrigin_TypedCode(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	runGitT(t, e.repo, "remote", "remove", "origin")
	_, err := e.create(nil)
	r := wantErrCode(t, err, "forge_no_remote")
	if !strings.Contains(r.Message, "no `origin` remote") || !strings.Contains(r.Message, "git remote add origin <url>") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.gh.calls() != 0 {
		t.Fatal("gh ran without an origin")
	}
}

func TestCreatePr_ForgeErrors_Codes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"unauthenticated", errors.New("gh: To get started with GitHub CLI, please run: gh auth login"), forge.CodeUnauthenticated},
		{"cli missing", exec.ErrNotFound, forge.CodeCLIMissing},
		{"rate limited", errors.New("gh: API rate limit exceeded"), forge.CodeRateLimited},
		{"timeout", context.DeadlineExceeded, forge.CodeTimeout},
		{"no remote", errors.New("gh: none of the git remotes point to a known GitHub host"), forge.CodeNoRemote},
		{"other", errors.New("gh: GraphQL: something unexpected"), forge.CodeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newPREnv(t)
			e.commit("a.txt", "a\n", "change")
			e.gh.createErr = tc.err
			_, err := e.create(nil)
			r := wantErrCode(t, err, tc.code)
			if tc.code == forge.CodeRateLimited {
				if m, _ := r.Data.(map[string]any); m["retryAt"] == nil {
					t.Fatalf("rate limit data = %#v, want retryAt", r.Data)
				}
			}
			// Nothing was recorded for a PR that was not opened.
			if got, _ := e.db.GetTask(e.task.ID); got.PRNumber != 0 {
				t.Fatalf("identity stored after a failed create: %+v", got)
			}
		})
	}
	t.Run("repo lookup fails", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "change")
		e.gh.repoErr = errors.New("gh: none of the git remotes point to a known GitHub host")
		_, err := e.create(nil)
		wantErrCode(t, err, forge.CodeNoRemote)
	})
}

func TestCreatePr_ErrorsNeverContainArgvOrToken(t *testing.T) {
	t.Parallel()
	const token = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	assertClean := func(t *testing.T, err error, extra ...string) {
		t.Helper()
		b, _ := json.Marshal(codedError(err))
		s := string(b)
		for _, leak := range append([]string{token, "x-access-token", "--body-file", "--title", "core.fsmonitor", "refs/heads/", "SECRET-TITLE", "SECRET-BODY"}, extra...) {
			if strings.Contains(s, leak) {
				t.Fatalf("wire error leaks %q: %s", leak, s)
			}
		}
	}
	t.Run("forge", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "change")
		e.gh.createErr = errors.New("gh: HTTP 500: server error, token " + token + "\nAuthorization: bearer " + token)
		_, err := e.create(map[string]any{"title": "SECRET-TITLE", "body": "SECRET-BODY"})
		wantErrCode(t, err, forge.CodeError)
		assertClean(t, err)
	})
	t.Run("git push", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("pre-receive hook needs a POSIX shell")
		}
		e := newPREnv(t)
		e.commit("a.txt", "a\n", "change")
		hook := filepath.Join(e.bare, "hooks", "pre-receive")
		if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\necho 'remote: denied for https://x-access-token:" + token + "@github.com/acme/repo.git' >&2\nexit 1\n"
		if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := e.create(map[string]any{"title": "SECRET-TITLE", "body": "SECRET-BODY"})
		r := wantErrCode(t, err, forge.CodePushRejected)
		if d := errDetail(r); !strings.Contains(d, "denied") || len(d) > 301+len("…") {
			t.Fatalf("detail = %q, want the scrubbed stderr tail", d)
		}
		assertClean(t, err)
	})
}

func TestCreatePr_RunScopedRefused(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	for _, m := range []string{"task.createPr", "task.updatePr"} {
		_, err := e.callAs(&requestContext{runScoped: true}, m, map[string]any{"taskId": e.task.ID, "title": "t", "body": "b"})
		wantErrCode(t, err, codeForbidden)
	}
	if e.gh.calls() != 0 || e.remoteSha(e.branch) != "" {
		t.Fatal("a refused agent call reached gh or pushed")
	}
	// The preview is a read: allowed.
	if _, err := e.callAs(&requestContext{runScoped: true}, "task.prPreview", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatalf("run-scoped preview: %v", err)
	}
}

func TestCreatePr_CherryPickPath_PushesPrTaskBranch(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	if err := os.WriteFile(filepath.Join(e.repo, "unrelated.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, e.repo, "add", "unrelated.txt")
	runGitT(t, e.repo, "commit", "-m", "unrelated, unpushed")
	task, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "Diverged")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.SetTaskBase(task.ID, "develop", strings.TrimSpace(runGitOutputT(t, *task.WorktreePath, "rev-parse", "HEAD"))); err != nil {
		t.Fatal(err)
	}
	e.task, e.branch, e.wt = task, *task.Branch, *task.WorktreePath
	e.commit("taskwork.txt", "mine\n", "task work")

	if _, err := e.create(nil); err != nil {
		t.Fatalf("task.createPr: %v", err)
	}
	prBranch := "smind/pr-task-" + strconv.FormatInt(task.ID, 10)
	args := e.gh.createdArgs(0)
	if !hasFlag(args, "--head", prBranch) || !hasFlag(args, "--base", "develop") {
		t.Fatalf("gh argv = %v, want --head %s --base develop", args, prBranch)
	}
	if e.remoteSha(prBranch) == "" || e.remoteSha(e.branch) != "" {
		t.Fatalf("origin: %s=%q, raw task branch=%q; want only the clean branch", prBranch, e.remoteSha(prBranch), e.remoteSha(e.branch))
	}
	got, _ := e.db.GetTask(task.ID)
	if got.PRHeadRef != prBranch || got.PRNumber != 7 {
		t.Fatalf("identity = %+v, want head ref %s", got, prBranch)
	}
}

func hasFlag(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestCreatePr_SecondConcurrentCallIsBusy(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.gh.gate = make(chan struct{})
	e.gh.entered = make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		_, err := e.create(nil)
		done <- err
	}()
	<-e.gh.entered // the first call is inside gh pr create
	_, err := e.create(nil)
	wantErrCode(t, err, "pr_busy")
	_, err = e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, "pr_busy")
	close(e.gh.gate)
	if err := <-done; err != nil {
		t.Fatalf("first create: %v", err)
	}
	if len(e.gh.creates) != 1 {
		t.Fatalf("gh pr create ran %d times, want 1", len(e.gh.creates))
	}
	// The latch is released: a later call is judged on its merits (a PR exists now).
	_, err = e.create(nil)
	wantErrCode(t, err, "pr_exists")
}

// --- task.updatePr ---

// createdTask is an env whose task already went through task.createPr.
func createdTask(t *testing.T) *prEnv {
	t.Helper()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	if _, err := e.create(nil); err != nil {
		t.Fatalf("task.createPr: %v", err)
	}
	return e
}

func TestUpdatePr_FastForwardPush(t *testing.T) {
	t.Parallel()
	e := createdTask(t)
	e.commit("b.txt", "b\n", "more")
	e.gh.viewHead = "newhead"

	v, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatalf("task.updatePr: %v", err)
	}
	res, ok := v.(prUpdateResult)
	if !ok || res.Pushed != (prPushed{From: e.branch, To: e.branch}) || res.PR.Number != 7 || res.PR.HeadSha != "newhead" {
		t.Fatalf("result = %#v", v)
	}
	if e.remoteSha(e.branch) != e.head() {
		t.Fatalf("origin's %s = %q, want %q", e.branch, e.remoteSha(e.branch), e.head())
	}
	if len(e.gh.creates) != 1 {
		t.Fatalf("update ran gh pr create (%d creates)", len(e.gh.creates))
	}
	if g, _ := e.api.git.Get(e.task.ID); g.PR == nil || g.PR.HeadSha != "newhead" {
		t.Fatalf("task.git pr after update = %+v, want the refreshed snapshot", g.PR)
	}
}

func TestUpdatePr_PushesToThePrHeadRef(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(9, "feature/head", nil) // an adopted PR whose head differs from the task branch
	if _, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatalf("task.updatePr: %v", err)
	}
	if e.remoteSha("feature/head") != e.head() || e.remoteSha(e.branch) != "" {
		t.Fatalf("origin feature/head = %q, task branch = %q; want only the PR's head ref updated", e.remoteSha("feature/head"), e.remoteSha(e.branch))
	}
}

func TestUpdatePr_ForgeFailureAfterPushStillReportsThePush(t *testing.T) {
	t.Parallel()
	e := createdTask(t)
	e.commit("b.txt", "b\n", "more")
	e.gh.mu.Lock()
	e.gh.viewState = "OPEN"
	e.gh.mu.Unlock()
	// `pr view` now fails: the cached snapshot is returned, the push stands.
	e.api.SetGithubClient(ghctx.New(ghctx.WithRunner(func(context.Context, string, ...string) (string, error) {
		return "", errors.New("gh: HTTP 500")
	}), ghctx.WithStdinRunner(e.gh.runIn)))
	v, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatalf("task.updatePr: %v", err)
	}
	if res := v.(prUpdateResult); res.PR.Number != 7 || res.Pushed.To != e.branch {
		t.Fatalf("result = %#v", res)
	}
	if e.remoteSha(e.branch) != e.head() {
		t.Fatal("the push did not happen")
	}
}

func TestUpdatePr_NeverForce(t *testing.T) {
	t.Parallel()
	e := createdTask(t)
	// Someone else pushes to the PR branch.
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, t.TempDir(), "clone", "-b", e.branch, e.bare, other)
	runGitT(t, other, "config", "user.email", "o@example.com")
	runGitT(t, other, "config", "user.name", "O")
	if err := os.WriteFile(filepath.Join(other, "theirs.txt"), []byte("t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, other, "add", "theirs.txt")
	runGitT(t, other, "commit", "-m", "their change")
	runGitT(t, other, "push", "origin", e.branch)
	theirs := e.remoteSha(e.branch)

	e.commit("b.txt", "b\n", "mine")
	_, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, "push_rejected")
	if got := e.remoteSha(e.branch); got != theirs {
		t.Fatalf("origin moved to %s after a rejected update, want it untouched at %s", got, theirs)
	}
}

func TestUpdatePr_PushRejected(t *testing.T) {
	t.Parallel()
	e := createdTask(t)
	e.commit("b.txt", "b\n", "more")
	// The remote refuses every update (a protected branch).
	if runtime.GOOS == "windows" {
		t.Skip("pre-receive hook needs a POSIX shell")
	}
	hook := filepath.Join(e.bare, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'protected branch' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	r := wantErrCode(t, err, "push_rejected")
	if r.Message != "GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first." {
		t.Fatalf("message = %q", r.Message)
	}
	if !strings.Contains(errDetail(r), "protected branch") {
		t.Fatalf("detail = %q", errDetail(r))
	}
}

func TestUpdatePr_PrNotOpen(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(7, e.branch, func(p *forge.PR) { p.State = forge.PRStateMerged })
	_, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	r := wantErrCode(t, err, "pr_not_open")
	if !strings.Contains(r.Message, "merged") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.remoteSha(e.branch) != "" || e.gh.calls() != 0 {
		t.Fatal("a refused update pushed or called gh")
	}

	// And a task with no PR at all.
	e2 := newPREnv(t)
	e2.commit("a.txt", "a\n", "change")
	_, err = e2.call("task.updatePr", map[string]any{"taskId": e2.task.ID})
	wantErrCode(t, err, "no_pr")
}

func TestUpdatePr_ForkPrReadOnly(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(7, e.branch, func(p *forge.PR) { p.FromFork = true })
	_, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, "fork_pr_readonly")
	if e.remoteSha(e.branch) != "" {
		t.Fatal("a fork PR update pushed")
	}
}

func TestUpdatePr_CherryPickPrUnsupported(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "change")
	e.seedPR(7, "smind/pr-task-12", nil)
	_, err := e.call("task.updatePr", map[string]any{"taskId": e.task.ID})
	r := wantErrCode(t, err, "update_unsupported")
	if r.Message != "This pull request was opened from a rebased copy (`smind/pr-task-12`), so smind can't update it automatically." {
		t.Fatalf("message = %q", r.Message)
	}
	if e.remoteSha(e.branch) != "" || e.remoteSha("smind/pr-task-12") != "" {
		t.Fatal("a refused update pushed")
	}
	g, _ := e.api.git.Get(e.task.ID)
	if a := g.Actions["updatePr"]; a.Enabled || a.Code != "update_unsupported" || a.Reason != r.Message {
		t.Fatalf("updatePr action = %+v, want the same disabled reason", a)
	}
}

// TestPRHandlers_ListedAndOldHandlerGone pins the wire: the three methods
// exist and the string-URL task.createPr shape is gone.
func TestPRHandlers_ListedAndOldHandlerGone(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	for _, m := range []string{"task.prPreview", "task.createPr", "task.updatePr"} {
		if e.api.hs[m] == nil {
			t.Fatalf("%s is not registered", m)
		}
	}
	e.commit("a.txt", "a\n", "change")
	v, err := e.create(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["pr"]; !ok || len(wire) != 1 {
		t.Fatalf("task.createPr result = %s, want exactly {pr}", b)
	}
}

// runGitOutputT is runGitT returning stdout.
func runGitOutputT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return string(out)
}
