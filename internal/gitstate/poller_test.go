package gitstate

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// fakeForge is a forge.Provider that counts every call per method and per
// directory, so a test can assert how many calls a scenario cost.
type fakeForge struct {
	mu      sync.Mutex
	calls   map[string]int
	repo    forge.Repo
	repoErr error
	openErr error
	viewErr error
	open    map[string][]forge.PR // by dir
	views   map[int]forge.PR
	entered chan struct{} // signalled on every OpenPRs entry when non-nil
	gate    chan struct{} // OpenPRs blocks until it is closed when non-nil
}

func newFakeForge() *fakeForge {
	return &fakeForge{
		calls: map[string]int{}, open: map[string][]forge.PR{}, views: map[int]forge.PR{},
		repo: forge.Repo{Host: "github.com", NameWithOwner: "org/repo"},
	}
}

func (f *fakeForge) Name() string { return "github" }

func (f *fakeForge) Search(context.Context, string, string, string, int) (forge.SearchResult, error) {
	return forge.SearchResult{}, errors.New("not used")
}

func (f *fakeForge) Get(context.Context, string, string, int, string) (*forge.Snapshot, error) {
	return nil, errors.New("not used")
}

func (f *fakeForge) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeForge) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, c := range f.calls {
		if k == "Repo" || k == "OpenPRs" || k == "PR" {
			n += c
		}
	}
	return n
}

func (f *fakeForge) Repo(_ context.Context, dir string) (forge.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Repo"]++
	f.calls["Repo:"+dir]++
	return f.repo, f.repoErr
}

func (f *fakeForge) OpenPRs(_ context.Context, dir string) ([]forge.PR, error) {
	f.mu.Lock()
	f.calls["OpenPRs"]++
	f.calls["OpenPRs:"+dir]++
	entered, gate := f.entered, f.gate
	list, err := append([]forge.PR(nil), f.open[dir]...), f.openErr
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	return list, err
}

func (f *fakeForge) PR(_ context.Context, _ string, number int) (forge.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["PR"]++
	pr, ok := f.views[number]
	if f.viewErr != nil {
		return forge.PR{}, f.viewErr
	}
	if !ok {
		return forge.PR{}, forge.NewError(forge.CodeNotFound, "")
	}
	return pr, nil
}

func (f *fakeForge) CreatePR(context.Context, string, forge.CreatePRInput) (forge.PR, error) {
	return forge.PR{}, errors.New("not used")
}

func (f *fakeForge) setOpen(dir string, prs ...forge.PR) {
	f.mu.Lock()
	f.open[dir] = prs
	f.mu.Unlock()
}

func mkPR(n int, head string, mod ...func(*forge.PR)) forge.PR {
	pr := forge.PR{
		Number: n, Repo: "org/repo", URL: "https://github.com/org/repo/pull/" + itoa(n), Title: "t",
		State: forge.PRStateOpen, HeadRef: head, HeadSha: "sha" + itoa(n), BaseRef: "develop",
		Mergeable: forge.MergeableYes, ReviewDecision: forge.ReviewNone,
		Checks: &forge.Checks{State: forge.ChecksSuccess, Passed: 1, Total: 1},
	}
	for _, m := range mod {
		m(&pr)
	}
	return pr
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// storeSource is a Source over a real store.
type storeSource struct{ db *store.Store }

func (s storeSource) GetTask(id int64) (store.Task, error) { return s.db.GetTask(id) }

func (s storeSource) GetWorkspace(id int64) (store.Workspace, error) { return s.db.GetWorkspace(id) }

func (s storeSource) ListTasks(ws int64) ([]store.Task, error) { return s.db.ListTasksByWorkspace(ws) }

func (s storeSource) TaskDiffStat(int64) (int, int, int, error) { return 0, 0, 0, errors.New("nope") }

type pollerFix struct {
	t   *testing.T
	db  *store.Store
	fg  *fakeForge
	clk *fakeClock
	in  *fakeInterest
	p   *Poller

	mu      sync.Mutex
	ids     []int64
	changed []int64
	adopted []int64
	jitter  float64
	tip     string
}

func newPollerFix(t *testing.T) *pollerFix {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := &pollerFix{t: t, db: db, fg: newFakeForge(), clk: newClock(), in: &fakeInterest{watched: true}}
	f.restart()
	return f
}

// restart builds a fresh Poller over the same store, as a daemon restart does.
func (f *pollerFix) restart() {
	if f.p != nil {
		f.p.Stop()
	}
	f.p = NewPoller(PollerOptions{
		Source: storeSource{f.db}, Interest: f.in,
		Provider: func() forge.Provider { return f.fg }, Ledger: f.db, Now: f.clk.Now,
		Jitter: func() float64 { f.mu.Lock(); defer f.mu.Unlock(); return f.jitter },
		Tasks: func() []int64 {
			f.mu.Lock()
			defer f.mu.Unlock()
			return append([]int64(nil), f.ids...)
		},
		OnChange: func(id int64) { f.mu.Lock(); f.changed = append(f.changed, id); f.mu.Unlock() },
		OnAdopt:  func(id int64) { f.mu.Lock(); f.adopted = append(f.adopted, id); f.mu.Unlock() },
		Tip: func(context.Context, string, string) string {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.tip
		},
	})
	f.t.Cleanup(f.p.Stop)
}

func (f *pollerFix) workspace(path string) store.Workspace {
	f.t.Helper()
	w, err := f.db.CreateWorkspace(store.Workspace{Path: path, Title: path, RoutingPolicy: "hard"})
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

// task creates a watched task with a worktree on branch.
func (f *pollerFix) task(ws store.Workspace, branch string) store.Task {
	f.t.Helper()
	wt := "/wt/" + ws.Path + "/" + branch
	tk, err := f.db.CreateTask(store.Task{
		WorkspaceID: ws.ID, Title: branch, Status: "running", WorktreePath: &wt, Branch: &branch,
		BaseRef: "develop", BaseSha: "0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.ids = append(f.ids, tk.ID)
	f.mu.Unlock()
	return tk
}

// withPR gives the task a stored PR identity.
func (f *pollerFix) withPR(tk store.Task, n int) store.Task {
	f.t.Helper()
	if err := f.db.SetTaskPR(tk.ID, "github", "org/repo", int64(n), "https://github.com/org/repo/pull/"+itoa(n), *tk.Branch); err != nil {
		f.t.Fatal(err)
	}
	out, err := f.db.GetTask(tk.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *pollerFix) reload(tk store.Task) store.Task {
	f.t.Helper()
	out, err := f.db.GetTask(tk.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *pollerFix) state(tk store.Task) store.TaskPRState {
	f.t.Helper()
	st, _, err := f.db.GetTaskPRState(tk.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *pollerFix) tick() { f.p.Tick() }

// step advances the clock by d and runs one beat.
func (f *pollerFix) step(d time.Duration) {
	f.clk.Advance(d)
	f.p.Tick()
}

func (f *pollerFix) calls(method string) int { return f.fg.count(method) }

func TestPoller_OneCallPerRepoPerTick_CountAsserting(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	b := f.withPR(f.task(ws, "b"), 2)
	c := f.task(ws, "c") // no PR yet: adoption rides the same list
	f.fg.setOpen("/repo", mkPR(1, "a"), mkPR(2, "b"), mkPR(3, "c"))
	f.in.focused = []int64{a.ID}

	f.tick()
	if f.calls("OpenPRs") != 1 || f.calls("Repo") != 1 || f.calls("PR") != 0 {
		t.Fatalf("first tick: Repo %d OpenPRs %d PR %d, want 1 1 0", f.calls("Repo"), f.calls("OpenPRs"), f.calls("PR"))
	}
	if got := f.reload(c); got.PRNumber != 3 {
		t.Fatalf("task c adopted PR %d, want 3", got.PRNumber)
	}
	for _, tk := range []store.Task{a, b} {
		if pr := f.p.PR(f.reload(tk)); pr == nil || pr.Number != int(tk.PRNumber) || pr.FetchedAt.IsZero() {
			t.Fatalf("task %d snapshot = %+v", tk.ID, pr)
		}
	}

	f.step(5 * time.Second)
	f.step(10 * time.Second)
	if f.calls("OpenPRs") != 1 {
		t.Fatalf("OpenPRs after 15 s = %d, want 1 (focused task is due at 120 s)", f.calls("OpenPRs"))
	}
	f.step(106 * time.Second) // 121 s after the first poll
	if f.calls("OpenPRs") != 2 || f.calls("Repo") != 1 || f.calls("PR") != 0 {
		t.Fatalf("second poll: Repo %d OpenPRs %d PR %d, want 1 2 0 (repo cached)", f.calls("Repo"), f.calls("OpenPRs"), f.calls("PR"))
	}
}

func TestPoller_ManyClientsSameCalls(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.fg.setOpen("/repo", mkPR(1, "a"))
	// Eight connections all focus the same task, and the loop beats from
	// several goroutines at once: still one call.
	f.in.focused = []int64{a.ID, a.ID, a.ID, a.ID, a.ID, a.ID, a.ID, a.ID}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); f.tick() }()
	}
	wg.Wait()
	if f.calls("OpenPRs") != 1 || f.calls("Repo") != 1 {
		t.Fatalf("Repo %d OpenPRs %d, want 1 1 however many clients", f.calls("Repo"), f.calls("OpenPRs"))
	}
}

func TestPoller_NoSubscribersNoCalls(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.task(ws, "b")
	f.fg.setOpen("/repo", mkPR(1, "a"))
	f.in.watched = false
	f.in.focused = []int64{a.ID}
	for range 20 {
		f.step(time.Minute)
	}
	if f.fg.total() != 0 {
		t.Fatalf("forge calls with no subscriber = %d, want 0", f.fg.total())
	}
	f.in.watched = true
	f.tick()
	if f.fg.total() == 0 {
		t.Fatal("no call once someone watches: the test is not exercising the poller")
	}
}

func TestPoller_CadencePendingFocusedWatched(t *testing.T) {
	f := newPollerFix(t)
	wsA, wsB, wsC := f.workspace("/a"), f.workspace("/b"), f.workspace("/c")
	ta := f.withPR(f.task(wsA, "a"), 1)
	tb := f.withPR(f.task(wsB, "b"), 1)
	f.withPR(f.task(wsC, "c"), 1)
	pending := mkPR(1, "a", func(p *forge.PR) { p.Checks = &forge.Checks{State: forge.ChecksPending, Pending: 1, Total: 1} })
	f.fg.setOpen("/a", pending)
	f.fg.setOpen("/b", mkPR(1, "b"))
	f.fg.setOpen("/c", mkPR(1, "c"))
	f.in.focused = []int64{ta.ID, tb.ID}

	for range 63 { // 0 s .. 310 s in 5 s beats
		f.tick()
		f.clk.Advance(5 * time.Second)
	}
	// Polls at t=0: all; focused+pending every 20 s (0..300 = 16), focused
	// every 120 s (0, 120, 240), watched every 300 s (0, 300).
	for dir, want := range map[string]int{"/a": 16, "/b": 3, "/c": 2} {
		if got := f.calls("OpenPRs:" + dir); got != want {
			t.Errorf("OpenPRs %s = %d, want %d", dir, got, want)
		}
	}
}

func TestPoller_TerminalStateStopsPolling(t *testing.T) {
	t.Run("merged", func(t *testing.T) {
		f := newPollerFix(t)
		ws := f.workspace("/repo")
		a := f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"))
		f.tick()
		f.fg.setOpen("/repo") // merged: it left the open list
		f.fg.views[1] = mkPR(1, "a", func(p *forge.PR) { p.State = forge.PRStateMerged })
		f.step(301 * time.Second)
		if f.calls("OpenPRs") != 2 || f.calls("PR") != 1 {
			t.Fatalf("OpenPRs %d PR %d, want 2 1 (the transition fetch)", f.calls("OpenPRs"), f.calls("PR"))
		}
		if pr := f.p.PR(f.reload(a)); pr == nil || pr.State != forge.PRStateMerged {
			t.Fatalf("snapshot = %+v, want merged", pr)
		}
		before := f.fg.total()
		for range 40 {
			f.step(10 * time.Minute)
		}
		if f.fg.total() != before {
			t.Fatalf("calls after a terminal PR: %d -> %d, want none", before, f.fg.total())
		}
	})

	t.Run("closed, branch moved re-arms one fetch", func(t *testing.T) {
		f := newPollerFix(t)
		ws := f.workspace("/repo")
		a := f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"))
		f.tick()
		f.fg.setOpen("/repo")
		f.fg.views[1] = mkPR(1, "a", func(p *forge.PR) { p.State = forge.PRStateClosed; p.HeadSha = "sha1" })
		f.step(301 * time.Second)
		if f.calls("PR") != 1 {
			t.Fatalf("PR calls = %d, want 1", f.calls("PR"))
		}
		f.tip = "sha1" // local tip equals the PR head: nothing to re-arm
		for range 10 {
			f.step(time.Minute)
		}
		if f.calls("PR") != 1 {
			t.Fatalf("PR calls with an unmoved branch = %d, want 1", f.calls("PR"))
		}
		f.tip = "sha2" // a push moved the branch
		f.fg.views[1] = mkPR(1, "a", func(p *forge.PR) { p.State = forge.PRStateClosed; p.HeadSha = "sha2" })
		f.step(time.Minute)
		f.step(time.Minute)
		if f.calls("PR") != 2 {
			t.Fatalf("PR calls after the branch moved = %d, want 2", f.calls("PR"))
		}
		if pr := f.p.PR(f.reload(a)); pr == nil || pr.HeadSha != "sha2" {
			t.Fatalf("snapshot = %+v, want head sha2", pr)
		}
		before := f.fg.total()
		for range 10 {
			f.step(time.Minute)
		}
		if f.fg.total() != before {
			t.Fatalf("calls after the re-armed fetch: %d -> %d, want none (one fetch only)", before, f.fg.total())
		}
	})
}

func TestPoller_PRLeftOpenList_OneViewCall(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	b := f.withPR(f.task(ws, "b"), 2)
	f.fg.setOpen("/repo", mkPR(1, "a"), mkPR(2, "b"))
	f.tick()
	f.fg.setOpen("/repo", mkPR(2, "b")) // #1 closed
	f.fg.views[1] = mkPR(1, "a", func(p *forge.PR) { p.State = forge.PRStateClosed })
	f.step(301 * time.Second)
	if f.calls("PR") != 1 || f.calls("OpenPRs") != 2 {
		t.Fatalf("OpenPRs %d PR %d, want 2 1", f.calls("OpenPRs"), f.calls("PR"))
	}
	if pr := f.p.PR(f.reload(a)); pr.State != forge.PRStateClosed {
		t.Fatalf("a = %+v, want closed", pr)
	}
	if pr := f.p.PR(f.reload(b)); pr.State != forge.PRStateOpen {
		t.Fatalf("b = %+v, want open", pr)
	}
	f.step(301 * time.Second) // b polls again; a is terminal and costs nothing more
	if f.calls("PR") != 1 || f.calls("OpenPRs") != 3 {
		t.Fatalf("OpenPRs %d PR %d, want 3 1", f.calls("OpenPRs"), f.calls("PR"))
	}
}

func TestPoller_FloorAndHourlyBudget_ServesCache(t *testing.T) {
	t.Run("repo floor", func(t *testing.T) {
		f := newPollerFix(t)
		ws := f.workspace("/repo")
		f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"), mkPR(2, "b"))
		f.tick()                     // polls at t0
		f.withPR(f.task(ws, "b"), 2) // appears after the poll: due at once, but the floor holds it
		f.step(3 * time.Second)
		f.step(10 * time.Second) // 13 s after the last poll: still under the 15 s floor
		if f.calls("OpenPRs") != 1 {
			t.Fatalf("OpenPRs inside the floor = %d, want 1", f.calls("OpenPRs"))
		}
		f.step(2 * time.Second) // 15 s
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("OpenPRs at the floor = %d, want 2", f.calls("OpenPRs"))
		}
	})

	t.Run("hourly budget", func(t *testing.T) {
		f := newPollerFix(t)
		ws := f.workspace("/repo")
		a := f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"))
		f.tick()
		if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "" {
			t.Fatalf("fresh snapshot = %+v", pr)
		}
		// The first poll cost 2 (Repo, OpenPRs); 237 more leave one call
		// before the 240 cap. The next poll fits (Repo is cached), the one
		// after does not.
		if _, err := f.db.AddForgeCalls("github.com", 240-2-1, f.clk.Now()); err != nil {
			t.Fatal(err)
		}
		f.step(121 * time.Second) // watched: due at 300 s, so not yet
		f.in.focused = []int64{a.ID}
		f.step(0) // focused: due; the 240th call is allowed
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("OpenPRs at the cap = %d, want 2", f.calls("OpenPRs"))
		}
		f.step(121 * time.Second) // due again, but the budget is spent
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("OpenPRs over budget = %d, want 2 (no call, nothing queued)", f.calls("OpenPRs"))
		}
		pr := f.p.PR(f.reload(a))
		if pr == nil || pr.Stale != "budget" || pr.RefreshAvailableAt == "" {
			t.Fatalf("over budget snapshot = %+v, want stale budget with refreshAvailableAt", pr)
		}
		at, err := time.Parse(time.RFC3339, pr.RefreshAvailableAt)
		if err != nil || !at.After(f.clk.Now()) || at.After(f.clk.Now().Add(time.Hour)) {
			t.Fatalf("refreshAvailableAt = %q (%v), want within the next hour", pr.RefreshAvailableAt, err)
		}
		for range 5 { // more beats: still nothing, and no retry storm
			f.step(30 * time.Second)
		}
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("OpenPRs while blocked = %d, want 2", f.calls("OpenPRs"))
		}
		// An explicit refresh is blocked by the same budget and returns the cache.
		if err := f.p.Refresh(a.ID); err != nil {
			t.Fatalf("Refresh over budget error = %v, want nil with a stale cache", err)
		}
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("Refresh over budget made a call (OpenPRs %d)", f.calls("OpenPRs"))
		}
		f.clk.Advance(time.Hour)
		f.tick()
		if f.calls("OpenPRs") != 3 {
			t.Fatalf("OpenPRs after the window rolled = %d, want 3", f.calls("OpenPRs"))
		}
		if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "" {
			t.Fatalf("snapshot after the window rolled = %+v, want fresh", pr)
		}
	})
}

func TestPoller_BackoffSequence(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.fg.setOpen("/repo", mkPR(1, "a"))
	f.tick() // a good snapshot to serve while failing
	f.in.focused = []int64{a.ID}
	f.fg.openErr = forge.NewError(forge.CodeError, "boom")

	wantWaits := []time.Duration{30, 60, 120, 300, 300, 300}
	f.step(121 * time.Second) // the focused task is due again
	for i, w := range wantWaits {
		w *= time.Second
		st := f.state(a)
		if st.ConsecutiveFailures != i+1 || st.LastErrorCode != forge.CodeError {
			t.Fatalf("failure %d: ledger = %+v", i+1, st)
		}
		if got := st.NextAllowedAt.Sub(f.clk.Now()); got != w {
			t.Fatalf("after failure %d next_allowed in %v, want %v", i+1, got, w)
		}
		if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "backoff" || pr.RefreshAvailableAt == "" {
			t.Fatalf("snapshot while backing off = %+v, want stale backoff", pr)
		}
		calls := f.calls("OpenPRs")
		f.step(w - time.Second)
		if f.calls("OpenPRs") != calls {
			t.Fatalf("call %v before next_allowed", time.Second)
		}
		f.step(time.Second)
		if f.calls("OpenPRs") != calls+1 {
			t.Fatalf("OpenPRs at next_allowed = %d, want %d", f.calls("OpenPRs"), calls+1)
		}
	}
	// A success resets the count and clears the stale flag.
	f.fg.openErr = nil
	f.step(300 * time.Second)
	st := f.state(a)
	if st.ConsecutiveFailures != 0 || st.LastErrorCode != "" || !st.NextAllowedAt.IsZero() {
		t.Fatalf("after success ledger = %+v", st)
	}
	if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "" {
		t.Fatalf("after success snapshot = %+v", pr)
	}
}

func TestPoller_RateLimited_BackoffMinimum(t *testing.T) {
	f := newPollerFix(t)
	wsA, wsB := f.workspace("/a"), f.workspace("/b")
	a := f.withPR(f.task(wsA, "a"), 1)
	f.withPR(f.task(wsB, "b"), 1)
	f.fg.setOpen("/a", mkPR(1, "a"))
	f.fg.setOpen("/b", mkPR(1, "b"))
	f.tick()
	f.in.focused = []int64{a.ID}

	now := f.clk.Now()
	f.fg.openErr = &forge.Error{Code: forge.CodeRateLimited, Message: "x", RetryAt: now.Add(125 * time.Second)}
	f.step(121 * time.Second)
	st := f.state(a)
	if st.LastErrorCode != forge.CodeRateLimited {
		t.Fatalf("ledger = %+v", st)
	}
	if got := st.NextAllowedAt.Sub(f.clk.Now()); got != 10*time.Minute {
		t.Fatalf("next_allowed in %v, want the 10 min minimum although RetryAt is sooner", got)
	}
	if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "rate_limited" {
		t.Fatalf("snapshot = %+v, want stale rate_limited", pr)
	}
	calls := f.fg.total()
	f.step(9 * time.Minute) // the other workspace shares the host: paused too
	if f.fg.total() != calls {
		t.Fatalf("calls while rate limited: %d -> %d, want none", calls, f.fg.total())
	}

	// A later reset time wins over the minimum.
	f.clk.Advance(2 * time.Minute) // 11 min after the first failure: the host is free again
	f.fg.openErr = &forge.Error{Code: forge.CodeRateLimited, Message: "x", RetryAt: f.clk.Now().Add(30 * time.Minute)}
	f.tick()
	st = f.state(a)
	if got := st.NextAllowedAt.Sub(f.clk.Now()); got != 30*time.Minute {
		t.Fatalf("next_allowed in %v, want RetryAt (30 min)", got)
	}
}

func TestPoller_Unauthenticated_SuspendsHost(t *testing.T) {
	f := newPollerFix(t)
	wsA, wsB := f.workspace("/a"), f.workspace("/b")
	a := f.withPR(f.task(wsA, "a"), 1)
	f.withPR(f.task(wsB, "b"), 1)
	f.fg.setOpen("/a", mkPR(1, "a"))
	f.fg.setOpen("/b", mkPR(1, "b"))
	// Seed a snapshot, then lose the login.
	f.tick()
	if f.fg.total() != 4 {
		t.Fatalf("seed calls = %d, want 4 (Repo+OpenPRs per workspace)", f.fg.total())
	}
	f.fg.openErr = forge.NewError(forge.CodeUnauthenticated, "")
	f.in.focused = []int64{a.ID}
	f.step(121 * time.Second)
	if got := f.fg.total(); got != 5 {
		t.Fatalf("calls after the unauthenticated poll = %d, want 5 (one failing call, host suspended)", got)
	}
	if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "unauthenticated" || pr.RefreshAvailableAt == "" {
		t.Fatalf("snapshot = %+v, want stale unauthenticated", pr)
	}
	f.step(9*time.Minute + 50*time.Second)
	f.step(time.Second)
	if got := f.fg.total(); got != 5 {
		t.Fatalf("calls during the suspension = %d, want 5", got)
	}
	f.fg.openErr = nil
	f.step(10 * time.Second) // 10 min after the failure: the re-probe
	if got := f.fg.total(); got < 6 {
		t.Fatalf("no re-probe after the suspension (calls %d)", got)
	}
	if pr := f.p.PR(f.reload(a)); pr == nil || pr.Stale != "" {
		t.Fatalf("after re-probe snapshot = %+v, want fresh", pr)
	}
}

func TestPoller_LedgerSurvivesRestart(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.fg.setOpen("/repo", mkPR(1, "a"))
	f.tick() // success: snapshot and 2 budgeted calls (Repo, OpenPRs)
	f.in.focused = []int64{a.ID}
	f.fg.openErr = forge.NewError(forge.CodeError, "")
	f.step(121 * time.Second) // failure 1
	failedAt := f.clk.Now()

	f.restart()
	f.fg.openErr = nil
	calls, _, err := f.db.GetForgeBudget("github.com", f.clk.Now())
	if err != nil || calls != 3+0 { // Repo + OpenPRs + the failing OpenPRs
		t.Fatalf("budget after restart = %d (%v), want 3", calls, err)
	}
	pr := f.p.PR(f.reload(a))
	if pr == nil || pr.Number != 1 || pr.Stale != "backoff" {
		t.Fatalf("snapshot after restart = %+v, want the persisted PR, stale backoff", pr)
	}
	before := f.fg.total()
	f.step(20 * time.Second) // inside the persisted 30 s backoff
	if f.fg.total() != before {
		t.Fatalf("a call %v after the failure despite next_allowed_at", f.clk.Now().Sub(failedAt))
	}
	f.step(11 * time.Second)
	if f.calls("OpenPRs") != 3 {
		t.Fatalf("OpenPRs after next_allowed = %d, want 3", f.calls("OpenPRs"))
	}
	if calls, _, _ := f.db.GetForgeBudget("github.com", f.clk.Now()); calls != 5 {
		// Repo is not cached across a restart: Repo + OpenPRs add two.
		t.Fatalf("budget after the restart's poll = %d, want 5", calls)
	}
}

func TestPoller_StartupJitterRespectsNextAllowed(t *testing.T) {
	t.Run("never before next_allowed_at", func(t *testing.T) {
		f := newPollerFix(t)
		ws := f.workspace("/repo")
		a := f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"))
		start := f.clk.Now()
		if err := f.db.SaveTaskPRState(store.TaskPRState{
			TaskID: a.ID, FetchedAt: start.Add(-time.Hour), NextAllowedAt: start.Add(5 * time.Minute),
			ConsecutiveFailures: 2, LastErrorCode: forge.CodeError,
		}); err != nil {
			t.Fatal(err)
		}
		f.restart()
		f.tick()
		f.step(4*time.Minute + 59*time.Second)
		if f.fg.total() != 0 {
			t.Fatalf("a call before next_allowed_at (calls %d)", f.fg.total())
		}
		f.step(2 * time.Second)
		if f.calls("OpenPRs") != 1 {
			t.Fatalf("OpenPRs at next_allowed_at = %d, want 1", f.calls("OpenPRs"))
		}
	})

	t.Run("start-up stagger and ±10 % spread", func(t *testing.T) {
		f := newPollerFix(t)
		f.jitter = 1
		ws := f.workspace("/repo")
		a := f.withPR(f.task(ws, "a"), 1)
		f.fg.setOpen("/repo", mkPR(1, "a"))
		start := f.clk.Now()
		if err := f.db.SaveTaskPRState(store.TaskPRState{TaskID: a.ID, FetchedAt: start.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		f.restart()
		f.tick()
		if f.fg.total() != 0 {
			t.Fatalf("an overdue task polled at once after a restart (calls %d); jitter should stagger it", f.fg.total())
		}
		f.step(31 * time.Second) // 10 % of the 300 s watched interval
		if f.calls("OpenPRs") != 1 {
			t.Fatalf("OpenPRs after the stagger = %d, want 1", f.calls("OpenPRs"))
		}
		polled := f.clk.Now()
		f.step(329 * time.Second) // 300 s + 10 % - 1 s
		if f.calls("OpenPRs") != 1 {
			t.Fatalf("OpenPRs inside the spread interval = %d, want 1", f.calls("OpenPRs"))
		}
		f.step(2 * time.Second)
		if f.calls("OpenPRs") != 2 {
			t.Fatalf("OpenPRs after interval+10%% (%v) = %d, want 2", f.clk.Now().Sub(polled), f.calls("OpenPRs"))
		}
	})
}

func TestPoller_AdoptsOpenPRByHeadRef(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	byBranch := f.task(ws, "smind/task-x")
	byHeadRef := f.task(ws, "smind/task-y")
	if err := f.db.SetTaskPR(byHeadRef.ID, "", "", 0, "", "smind/pr-task-9"); err != nil {
		t.Fatal(err)
	}
	f.task(ws, "unrelated")
	f.fg.setOpen("/repo", mkPR(11, "smind/task-x"), mkPR(12, "smind/pr-task-9"), mkPR(13, "other"))
	f.tick()

	got := f.reload(byBranch)
	if got.PRNumber != 11 || got.PRForge != "github" || got.PRRepo != "org/repo" ||
		got.PRURL != "https://github.com/org/repo/pull/11" || got.PRHeadRef != "smind/task-x" {
		t.Fatalf("adopted by branch = %+v", got)
	}
	if got := f.reload(byHeadRef); got.PRNumber != 12 || got.PRHeadRef != "smind/pr-task-9" {
		t.Fatalf("adopted by pr_head_ref = %+v", got)
	}
	f.mu.Lock()
	n := len(f.adopted)
	f.mu.Unlock()
	if n != 2 {
		t.Fatalf("OnAdopt calls = %d, want 2", n)
	}
	if pr := f.p.PR(f.reload(byBranch)); pr == nil || pr.Number != 11 {
		t.Fatalf("adopted snapshot = %+v", pr)
	}
	if f.calls("OpenPRs") != 1 || f.calls("PR") != 0 {
		t.Fatalf("adoption cost OpenPRs %d PR %d, want 1 0 (no extra call)", f.calls("OpenPRs"), f.calls("PR"))
	}
}

func TestPoller_DoesNotAdoptForkPR(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	fork := f.task(ws, "feature")
	foreignURL := f.task(ws, "evil")
	f.fg.setOpen("/repo",
		mkPR(21, "feature", func(p *forge.PR) { p.FromFork = true }),
		mkPR(22, "evil", func(p *forge.PR) { p.URL = "https://example.com/org/repo/pull/22" }),
	)
	f.tick()
	for _, tk := range []store.Task{fork, foreignURL} {
		if got := f.reload(tk); got.PRNumber != 0 || got.PRURL != "" {
			t.Fatalf("task %s adopted %+v, want no identity", *tk.Branch, got)
		}
		if f.p.PR(f.reload(tk)) != nil {
			t.Fatalf("task %s has a PR view without an identity", *tk.Branch)
		}
	}
	if len(f.adopted) != 0 {
		t.Fatalf("OnAdopt called %v", f.adopted)
	}
}

// waitJoined blocks until n callers wait on workspace ws's poll in flight. It
// polls state, not the clock: the outcome never depends on how long it takes.
func (f *pollerFix) waitJoined(ws int64, n int) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.p.mu.Lock()
		got := 0
		if fl := f.p.flights[ws]; fl != nil {
			got = fl.joined
		}
		f.p.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("only %d of %d callers joined the poll in flight", got, n)
		}
		runtime.Gosched()
	}
}

func TestPoller_SingleFlightConcurrentRefresh(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	b := f.withPR(f.task(ws, "b"), 2)
	f.fg.setOpen("/repo", mkPR(1, "a"), mkPR(2, "b"))
	f.fg.entered = make(chan struct{}, 8)
	f.fg.gate = make(chan struct{})

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	run := func(id int64) {
		defer wg.Done()
		errs <- f.p.Refresh(id)
	}
	wg.Add(1)
	go run(a.ID)
	<-f.fg.entered // the first refresh is inside OpenPRs
	for _, id := range []int64{a.ID, b.ID, b.ID} {
		wg.Add(1)
		go run(id) // join the call in flight
	}
	f.waitJoined(ws.ID, 3) // all three joiners are waiting on the flight
	close(f.fg.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Refresh error = %v", err)
		}
	}
	if f.calls("OpenPRs") != 1 || f.calls("Repo") != 1 {
		t.Fatalf("Repo %d OpenPRs %d, want 1 1: concurrent refreshes must share one call", f.calls("Repo"), f.calls("OpenPRs"))
	}
	if calls, _, _ := f.db.GetForgeBudget("github.com", f.clk.Now()); calls != 2 {
		t.Fatalf("budget = %d, want 2 (joiners spend nothing)", calls)
	}
}

func TestRefresh_PerTaskFloorAndHourly(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	other := f.withPR(f.task(ws, "b"), 2)
	f.fg.setOpen("/repo", mkPR(1, "a"), mkPR(2, "b"))
	start := f.clk.Now()

	if err := f.p.Refresh(a.ID); err != nil {
		t.Fatal(err)
	}
	var lim *RefreshLimitedError
	f.clk.Advance(2 * time.Second)
	err := f.p.Refresh(a.ID)
	if !errors.As(err, &lim) || lim.Hourly || !lim.RetryAt.Equal(start.Add(refreshFloor)) {
		t.Fatalf("refresh 2 s later = %v, want floor error retryAt %v", err, start.Add(refreshFloor))
	}
	if lim.RPCCode() != "refresh_limited" {
		t.Fatalf("code = %q", lim.RPCCode())
	}
	if err := f.p.Refresh(other.ID); err != nil { // another task: its own ledger
		t.Fatalf("refresh of another task = %v", err)
	}
	calls := f.calls("OpenPRs")

	// 12 refreshes an hour, 6 s apart.
	for i := 1; i < 12; i++ {
		f.clk.Advance(6 * time.Second)
		if err := f.p.Refresh(a.ID); err != nil {
			t.Fatalf("refresh %d: %v", i+1, err)
		}
	}
	f.clk.Advance(6 * time.Second)
	err = f.p.Refresh(a.ID)
	if !errors.As(err, &lim) || !lim.Hourly || !lim.RetryAt.Equal(start.Add(time.Hour)) {
		t.Fatalf("13th refresh = %v (retryAt %v), want hourly error at %v", err, lim, start.Add(time.Hour))
	}
	if f.calls("OpenPRs") != calls+11 {
		t.Fatalf("a limited refresh made a call: OpenPRs %d, want %d", f.calls("OpenPRs"), calls+11)
	}
	if st := f.state(a); len(st.ForcedAt) != 12 {
		t.Fatalf("persisted forced_at = %d stamps, want 12", len(st.ForcedAt))
	}
	f.clk.Advance(time.Hour)
	if err := f.p.Refresh(a.ID); err != nil {
		t.Fatalf("refresh after the hour = %v", err)
	}
}

func TestTaskGit_ReadNeverSpawnsForge(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.fg.setOpen("/repo", mkPR(1, "a"))
	f.tick() // seeds the persisted snapshot
	seeded := f.fg.total()

	r := &countRunner{origin: "https://github.com/org/repo.git"}
	src := storeSource{f.db}
	s := New(Options{
		Source: src, Runner: r.run, Now: f.clk.Now, Interest: f.in,
		Forge: func() forge.Provider { return f.fg }, Ledger: f.db,
	})
	t.Cleanup(s.Stop)
	for range 3 {
		g, err := s.Get(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if g.PR == nil || g.PR.Number != 1 || g.Forge != "github" {
			t.Fatalf("TaskGit = pr %+v forge %q, want the persisted snapshot", g.PR, g.Forge)
		}
		if _, err := s.List(ws.ID); err != nil {
			t.Fatal(err)
		}
		s.InvalidateSync(a.ID)
		s.Invalidate(a.ID)
	}
	s.Wait()
	if f.fg.total() != seeded {
		t.Fatalf("reads called the forge: %d -> %d", seeded, f.fg.total())
	}
	g, _ := s.Get(a.ID)
	if g.Actions["createPr"].Code != "pr_exists" {
		t.Fatalf("createPr action = %+v, want pr_exists from the PR", g.Actions["createPr"])
	}
}

func TestCanonicalKey_IgnoresPRFetchTimes(t *testing.T) {
	g := TaskGit{PR: &PRInfo{Number: 1, State: "open", FetchedAt: time.Unix(1, 0), RefreshAvailableAt: "x"}}
	h := g
	pr := *g.PR
	pr.FetchedAt, pr.RefreshAvailableAt = time.Unix(99, 0), "y"
	h.PR = &pr
	if canonicalKey(g) != canonicalKey(h) {
		t.Fatal("fetchedAt/refreshAvailableAt change the canonical key")
	}
	pr.Stale = "budget"
	h.PR = &pr
	if canonicalKey(g) == canonicalKey(h) {
		t.Fatal("a stale flag change is not news to the canonical key")
	}
}

func TestService_RefreshReturnsTaskGit(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.withPR(f.task(ws, "a"), 1)
	f.fg.setOpen("/repo", mkPR(1, "a"))
	r := &countRunner{}
	s := New(Options{
		Source: storeSource{f.db}, Runner: r.run, Now: f.clk.Now, Interest: f.in,
		Forge: func() forge.Provider { return f.fg }, Ledger: f.db,
	})
	t.Cleanup(s.Stop)

	g, err := s.Refresh(a.ID)
	if err != nil || g.PR == nil || g.PR.Number != 1 {
		t.Fatalf("Refresh = %+v, %v", g.PR, err)
	}
	f.clk.Advance(6 * time.Second)
	f.fg.openErr = forge.NewError(forge.CodeUnauthenticated, "")
	g, err = s.Refresh(a.ID)
	if err != nil || g.PR == nil || g.PR.Stale != "unauthenticated" {
		t.Fatalf("Refresh with a cached snapshot = pr %+v, err %v; want the snapshot, stale unauthenticated", g.PR, err)
	}

	// No snapshot to show: the forge error surfaces, typed.
	lone := f.task(ws, "lone")
	f.clk.Advance(11 * time.Minute)
	if _, err := s.Refresh(lone.ID); err == nil || forgeCode(err) != forge.CodeUnauthenticated {
		t.Fatalf("Refresh without a snapshot error = %v, want forge_unauthenticated", err)
	}
}

func TestPoller_ForgeBlock_UnauthenticatedAndCLIMissing(t *testing.T) {
	f := newPollerFix(t)
	wsA, wsB := f.workspace("/a"), f.workspace("/b")
	a := f.task(wsA, "a")
	b := f.task(wsB, "b")
	if got := f.p.ForgeBlock(f.reload(a)); got != "" {
		t.Fatalf("ForgeBlock before any poll = %q, want none", got)
	}
	f.fg.setOpen("/a", mkPR(1, "other"))
	f.in.focused = []int64{a.ID}
	f.tick()
	if got := f.p.ForgeBlock(f.reload(a)); got != "" {
		t.Fatalf("ForgeBlock after a good poll = %q, want none", got)
	}

	// Signed out: the host is suspended, and every workspace on it is blocked.
	f.fg.openErr = forge.NewError(forge.CodeUnauthenticated, "")
	f.step(121 * time.Second)
	for _, tk := range []store.Task{a, b} {
		if got := f.p.ForgeBlock(f.reload(tk)); got != forge.CodeUnauthenticated {
			t.Fatalf("task %d ForgeBlock = %q, want forge_unauthenticated", tk.ID, got)
		}
	}
	// The suspension ends by itself; the next poll is the re-probe.
	f.fg.openErr = nil
	f.step(11 * time.Minute)
	if got := f.p.ForgeBlock(f.reload(a)); got != "" {
		t.Fatalf("ForgeBlock after the suspension = %q, want none", got)
	}

	// gh missing: a per-task backoff, not a host suspension.
	f.fg.openErr = forge.NewError(forge.CodeCLIMissing, "")
	f.step(121 * time.Second)
	if got := f.p.ForgeBlock(f.reload(a)); got != forge.CodeCLIMissing {
		t.Fatalf("ForgeBlock = %q, want forge_cli_missing", got)
	}
	f.fg.openErr = nil // gh got installed
	f.step(10 * time.Minute)
	if got := f.p.ForgeBlock(f.reload(a)); got != "" {
		t.Fatalf("ForgeBlock after the backoff and a good poll = %q, want none", got)
	}
	var nilPoller *Poller
	if nilPoller.ForgeBlock(a) != "" {
		t.Fatal("nil poller must report no block")
	}
}

func TestPoller_AdoptAndRecord(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	a := f.task(ws, "a")
	pr := mkPR(7, "a")
	f.p.Adopt(f.reload(a), pr, forge.Repo{Host: "github.com", NameWithOwner: "org/repo"})
	got := f.reload(a)
	if got.PRNumber != 7 || got.PRRepo != "org/repo" || got.PRURL != "https://github.com/org/repo/pull/7" || got.PRHeadRef != "a" || got.PRForge != "github" {
		t.Fatalf("identity = %+v", got)
	}
	if len(f.adopted) != 1 || f.adopted[0] != a.ID {
		t.Fatalf("OnAdopt = %v", f.adopted)
	}
	if v := f.p.PR(got); v == nil || v.Number != 7 || v.FetchedAt.IsZero() {
		t.Fatalf("snapshot after adopt = %+v", v)
	}

	// A URL off the forge host is never stored.
	b := f.task(ws, "b")
	evil := mkPR(8, "b", func(p *forge.PR) { p.URL = "https://evil.example.com/org/repo/pull/8" })
	f.p.Adopt(f.reload(b), evil, forge.Repo{Host: "github.com", NameWithOwner: "org/repo"})
	if f.reload(b).PRNumber != 0 {
		t.Fatal("a PR with a foreign url was adopted")
	}

	// Record refreshes the snapshot and leaves the identity alone.
	merged := mkPR(7, "a", func(p *forge.PR) { p.State = forge.PRStateMerged })
	f.p.Record(got, merged, forge.Repo{Host: "github.com", NameWithOwner: "org/repo"})
	if v := f.p.PR(got); v == nil || v.State != forge.PRStateMerged {
		t.Fatalf("snapshot after Record = %+v", v)
	}
	if len(f.adopted) != 1 {
		t.Fatalf("Record must not signal an adoption: %v", f.adopted)
	}

	f.p.NoteCalls("github.com", 2)
	if calls, _, _ := f.db.GetForgeBudget("github.com", f.clk.Now()); calls != 2 {
		t.Fatalf("budget = %d, want the 2 noted calls", calls)
	}
}
