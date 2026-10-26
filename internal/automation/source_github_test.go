package automation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// fakeForge is a forge.Provider (only what the source calls) that serves
// scripted workflow runs and counts every forge call.
type fakeForge struct {
	forge.Provider
	mu        sync.Mutex
	runs      []forge.WorkflowRun
	err       error
	listCalls int
	repoCalls int
	lastQuery forge.WorkflowRunQuery
}

func (f *fakeForge) Name() string { return "github" }
func (f *fakeForge) Repo(context.Context, string) (forge.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repoCalls++
	return forge.Repo{Host: "github.com", NameWithOwner: "o/r"}, nil
}
func (f *fakeForge) WorkflowRuns(_ context.Context, _ string, q forge.WorkflowRunQuery) ([]forge.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	f.lastQuery = q
	if f.err != nil {
		return nil, f.err
	}
	return append([]forge.WorkflowRun(nil), f.runs...), nil
}

func run(id int64, status, concl, sha string) forge.WorkflowRun {
	return forge.WorkflowRun{ID: id, Workflow: "Windows full suite", WorkflowID: 7, Branch: "develop", HeadSha: sha, Status: status, Conclusion: concl,
		Event: "schedule", URL: fmt.Sprintf("https://github.com/o/r/actions/runs/%d", id)}
}

type ghEnv struct {
	*env
	ff  *fakeForge
	src *GitHubSource
}

func newGHEnv(t *testing.T) *ghEnv {
	e := newEnv(t)
	ff := &fakeForge{}
	src := &GitHubSource{Provider: func() forge.Provider { return ff }, Workspace: func(int64) (string, error) { return "/ws", nil },
		Ledger: e.st, Now: func() time.Time { return e.now }}
	e.eng = New(Deps{Store: e.st, Starter: e.fs, Memory: Memory{Home: e.home}, Sources: []Source{src},
		Config: func() Config { return e.cfg }, Sink: e.sink, Now: func() time.Time { return e.now }})
	return &ghEnv{env: e, ff: ff, src: src}
}

func (g *ghEnv) poll(d time.Duration) { g.advance(d); g.tick() }

func TestGHSource_PrimesCursorFiresNothing_ThenFiresOnNewFailure(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(300, "completed", "failure", "aaaaaaa"), run(299, "completed", "success", "bbbbbbb")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	cur, _ := g.st.GetAutomation(a.ID)
	if g.fs.count() != 0 || cur.PollCursor != "300" {
		t.Fatalf("priming: starts=%d cursor=%q", g.fs.count(), cur.PollCursor)
	}
	if g.ff.lastQuery.Workflow != "Windows full suite" || g.ff.lastQuery.Branch != "develop" {
		t.Fatalf("query = %+v", g.ff.lastQuery)
	}
	g.ff.runs = append([]forge.WorkflowRun{run(301, "completed", "failure", "ccccccc")}, g.ff.runs...)
	g.poll(6 * time.Minute)
	if g.fs.count() != 1 {
		t.Fatalf("starts = %d", g.fs.count())
	}
	rows := g.audit(a.ID)
	if rows[0].Fingerprint != "gh:o/r:Windows full suite:develop:failure:ccccccc" || rows[0].Decision != "fire" {
		t.Fatalf("row = %+v", rows[0])
	}
	if !strings.Contains(g.fs.started[0].Prompt, `"runId":301`) {
		t.Fatalf("prompt lacks the run id: %q", g.fs.started[0].Prompt)
	}
	cur, _ = g.st.GetAutomation(a.ID)
	if cur.PollCursor != "301" {
		t.Fatalf("cursor = %q", cur.PollCursor)
	}
}

func TestGHSource_FilterBranchWorkflowConclusion(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	g.create(g.def(KindWorkflowRun), true)
	g.tick()
	// success, cancelled (not listed), and in-progress runs are no signal.
	g.ff.runs = []forge.WorkflowRun{run(14, "in_progress", "", "d"), run(13, "completed", "cancelled", "c"), run(12, "completed", "success", "b"), run(10, "completed", "success", "a")}
	g.poll(6 * time.Minute)
	if g.fs.count() != 0 {
		t.Fatalf("non-matching runs fired %d tasks", g.fs.count())
	}
}

func TestGHSource_InProgressRunSeenWhenItLaterFails(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	g.ff.runs = []forge.WorkflowRun{run(11, "in_progress", "", "b"), run(10, "completed", "success", "a")}
	g.poll(6 * time.Minute)
	if cur, _ := g.st.GetAutomation(a.ID); cur.PollCursor != "10" {
		t.Fatalf("cursor passed an unfinished run: %q", cur.PollCursor)
	}
	g.ff.runs = []forge.WorkflowRun{run(11, "completed", "failure", "b"), run(10, "completed", "success", "a")}
	g.poll(6 * time.Minute)
	if g.fs.count() != 1 {
		t.Fatalf("a run that failed after being seen in progress was lost (starts %d)", g.fs.count())
	}
}

func TestGHSource_SameShaNewRunIdDeduped_NewShaFires(t *testing.T) {
	g := newGHEnv(t)
	d := g.def(KindWorkflowRun)
	d.Rails.MaxRunsPerHour = 12
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(d, true)
	g.tick()
	g.ff.runs = append([]forge.WorkflowRun{run(11, "completed", "failure", "bad1111")}, g.ff.runs...)
	g.poll(6 * time.Minute)
	g.fs.set(1, RunStatus{State: "done"})
	g.ff.runs = append([]forge.WorkflowRun{run(12, "completed", "failure", "bad1111")}, g.ff.runs...) // re-run of the same commit
	g.poll(6 * time.Minute)
	g.ff.runs = append([]forge.WorkflowRun{run(13, "completed", "failure", "new2222")}, g.ff.runs...)
	g.poll(6 * time.Minute)
	if got := decisions(g.audit(a.ID)); got != "fire,deduped,fire" {
		t.Fatalf("decisions = %q", got)
	}
}

func TestGHSource_CallCounts_UnderBudget_AndOneListPerPoll(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	g.create(g.def(KindWorkflowRun), true)
	for i := 0; i < 24; i++ { // two hours of ticks at the 5-minute interval
		g.poll(5*time.Minute + time.Second)
	}
	if g.ff.repoCalls < 1 || g.ff.repoCalls > 3 {
		t.Fatalf("repo lookups over two hours = %d, want about 2 (cached for an hour)", g.ff.repoCalls)
	}
	if g.ff.listCalls < 20 || g.ff.listCalls > 25 {
		t.Fatalf("list calls over two hours = %d, want about 24", g.ff.listCalls)
	}
	n, _, _ := g.st.GetForgeBudget("automations:github.com", g.now)
	if n > AutomationHourlyCalls {
		t.Fatalf("automations booked %d calls in the window, ceiling %d", n, AutomationHourlyCalls)
	}
	host, _, _ := g.st.GetForgeBudget("github.com", g.now)
	if host != n {
		t.Fatalf("real host ledger %d != automations ledger %d: the PR poller cannot see these calls", host, n)
	}
}

func TestGHSource_BudgetCeilingDefersWithoutCalling(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(g.def(KindWorkflowRun), true)
	if _, err := g.st.AddForgeCalls("automations:github.com", AutomationHourlyCalls, g.now); err != nil {
		t.Fatal(err)
	}
	g.tick()
	if g.ff.listCalls != 0 {
		t.Fatalf("called the forge over the automations ceiling (%d)", g.ff.listCalls)
	}
	cur, _ := g.st.GetAutomation(a.ID)
	if cur.PollNextAllowedAt == nil || !cur.PollNextAllowedAt.After(g.now) || cur.PollFailures != 0 {
		t.Fatalf("deferral not recorded: %+v", cur)
	}
	// The real host's budget is a ceiling too.
	g2 := newGHEnv(t)
	g2.create(g2.def(KindWorkflowRun), true)
	_, _ = g2.st.AddForgeCalls("github.com", 240, g2.now)
	g2.tick()
	if g2.ff.listCalls != 0 {
		t.Fatal("called the forge over the host budget")
	}
}

func TestGHSource_RepoFloor(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	d1, d2 := g.def(KindWorkflowRun), g.def(KindWorkflowRun)
	d2.Name = "second"
	g.create(d1, true)
	g.create(d2, true)
	g.tick() // two automations of one repo in the same tick: the 15 s floor defers the second
	if g.ff.listCalls != 1 {
		t.Fatalf("list calls = %d, want 1 inside the floor", g.ff.listCalls)
	}
	g.advance(20 * time.Second)
	g.tick()
	if g.ff.listCalls != 2 {
		t.Fatalf("list calls after the floor = %d", g.ff.listCalls)
	}
}

func TestGHSource_BackoffOnAuthError_RateLimitDeferred(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	g.ff.err = forge.NewError(forge.CodeUnauthenticated, "not logged in")
	g.poll(6 * time.Minute)
	cur, _ := g.st.GetAutomation(a.ID)
	if cur.PollFailures != 1 {
		t.Fatalf("failures = %d", cur.PollFailures)
	}
	calls := g.ff.listCalls
	g.poll(time.Minute) // inside the backoff
	if g.ff.listCalls != calls {
		t.Fatal("polled inside the backoff window")
	}
	g.ff.err = forge.NewError(forge.CodeRateLimited, "rate limit")
	g.poll(30 * time.Minute)
	cur, _ = g.st.GetAutomation(a.ID)
	if cur.PollFailures != 1 || cur.PollNextAllowedAt == nil || cur.PollNextAllowedAt.Sub(g.now) < 9*time.Minute {
		t.Fatalf("rate limit not deferred ~10 min without a failure: %+v", cur)
	}
}

func TestGHSource_NoForgeClient(t *testing.T) {
	g := newGHEnv(t)
	g.src.Provider = func() forge.Provider { return nil }
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	cur, _ := g.st.GetAutomation(a.ID)
	if cur.PollFailures != 1 || g.fs.count() != 0 {
		t.Fatalf("failures %d starts %d", cur.PollFailures, g.fs.count())
	}
}

func TestManualFire_LatestRunOneCall(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(30, "completed", "success", "z"), run(29, "completed", "failure", "yyyyyyy"), run(28, "completed", "failure", "xxxxxxx")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	before := g.ff.listCalls
	g.advance(time.Minute)
	row, err := g.eng.Fire(context.Background(), a.ID)
	if err != nil || row.Decision != "fire" || !row.Manual {
		t.Fatalf("fire = %+v err %v", row, err)
	}
	if g.ff.listCalls != before+1 {
		t.Fatalf("fire now used %d forge list calls, want 1", g.ff.listCalls-before)
	}
	if !strings.Contains(g.fs.started[0].Prompt, `"runId":29`) || !strings.Contains(g.fs.started[0].Prompt, `"headSha":"yyyyyyy"`) {
		t.Fatalf("fire now did not use the latest failed run: %q", g.fs.started[0].Prompt)
	}
	// No matching run: a clear error, no task.
	g.ff.runs = []forge.WorkflowRun{run(31, "completed", "success", "s")}
	g.fs.set(1, RunStatus{State: "done"})
	g.advance(time.Minute)
	if _, err := g.eng.Fire(context.Background(), a.ID); err == nil {
		t.Fatal("fire now with no failed run succeeded")
	}
}

func TestGHSource_CursorSurvivesRestart(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	g.eng = New(Deps{Store: g.st, Starter: g.fs, Memory: Memory{Home: g.home}, Sources: []Source{g.src},
		Config: func() Config { return g.cfg }, Now: func() time.Time { return g.now }})
	g.poll(6 * time.Minute)
	if g.fs.count() != 0 {
		t.Fatal("a restart re-fired history")
	}
	if cur, _ := g.st.GetAutomation(a.ID); cur.PollCursor != "10" {
		t.Fatalf("cursor = %q", cur.PollCursor)
	}
}

// A crash between the audit write and the cursor write must re-deliver the
// signal (deduped by its fingerprint), never lose it: the cursor still holds
// its old value while the task is being started.
func TestGHSource_CursorAdvancesOnlyAfterAuditAndLaunch(t *testing.T) {
	g := newGHEnv(t)
	g.ff.runs = []forge.WorkflowRun{run(10, "completed", "success", "a")}
	a := g.create(g.def(KindWorkflowRun), true)
	g.tick()
	var atLaunch string
	g.fs.onStart = func() {
		cur, _ := g.st.GetAutomation(a.ID)
		atLaunch = cur.PollCursor
	}
	g.ff.runs = append([]forge.WorkflowRun{run(11, "completed", "failure", "bad1111")}, g.ff.runs...)
	g.poll(6 * time.Minute)
	if atLaunch != "10" {
		t.Fatalf("cursor was %q while the task started, want the old 10", atLaunch)
	}
	if rows := g.audit(a.ID); len(rows) != 1 || rows[0].Decision != "fire" {
		t.Fatalf("rows = %v", decisions(rows))
	}
}
