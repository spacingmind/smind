package gitstate

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// The poller's cadences, floors and budgets (ADR-0039 §4).
const (
	// PollEvery is the poller's own beat; a workspace is polled only when it
	// is due, so most beats do nothing.
	PollEvery = 5 * time.Second

	pollFocusedPending = 20 * time.Second
	pollFocused        = 120 * time.Second
	pollWatched        = 300 * time.Second

	repoFloor      = 15 * time.Second
	hourlyBudget   = 240
	hostSuspendFor = 10 * time.Minute
	rateLimitMin   = 10 * time.Minute
	rateLimitMax   = time.Hour
	repoCacheTTL   = time.Hour
	refreshFloor   = 5 * time.Second
	refreshPerHour = 12
	tipCheckEvery  = 30 * time.Second
	jitterFraction = 0.1

	// defaultForgeHost names the host a failure is booked against before the
	// workspace's repo (and so its real host) has been resolved.
	defaultForgeHost = "github.com"
)

// backoffSteps is the wait after the 1st, 2nd, 3rd and later failures.
var backoffSteps = []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second, 300 * time.Second}

func backoff(failures int) time.Duration {
	i := max(failures, 1) - 1
	return backoffSteps[min(i, len(backoffSteps)-1)]
}

// Ledger is the slice of *store.Store the poller persists through.
type Ledger interface {
	GetTaskPRState(taskID int64) (store.TaskPRState, bool, error)
	SaveTaskPRState(st store.TaskPRState) error
	AddForgeCalls(host string, n int, now time.Time) (int, error)
	GetForgeBudget(host string, now time.Time) (int, time.Time, error)
	SetTaskPR(id int64, forge, repo string, number int64, url, headRef string) error
}

// PollerOptions configures a Poller. Source, Interest, Provider and Ledger are
// required; the rest default.
type PollerOptions struct {
	Source   Source
	Interest Interest
	Runner   Runner
	// Provider returns the current forge client (nil when there is none); wsapi
	// lets tests swap it after the handler table is built.
	Provider func() forge.Provider
	Ledger   Ledger
	Now      func() time.Time
	// Jitter returns a value in [-1, 1]; intervals are spread by ±10 % of it.
	Jitter func() float64
	// Tasks lists the task ids someone has looked at (the service's cache);
	// focused tasks are added from Interest.
	Tasks func() []int64
	// OnChange is called after a poll changed what TaskGit.pr would show.
	OnChange func(taskID int64)
	// OnAdopt is called after the poller wrote a task's PR identity.
	OnAdopt func(taskID int64)
	// Tip returns the local tip of branch in repoDir ("" if unknown); it
	// defaults to rev-parse through Runner.
	Tip func(ctx context.Context, repoDir, branch string) string
}

// RefreshLimitedError is task.gitRefresh over its per-task floor or hourly
// cap (ADR-0039 §6 `refresh_limited`).
type RefreshLimitedError struct {
	RetryAt time.Time
	Hourly  bool
}

func (e *RefreshLimitedError) Error() string {
	if e.Hourly {
		return "This pull request was refreshed too often in the last hour. Try again later."
	}
	return "This pull request was refreshed a moment ago. Try again in a few seconds."
}

// RPCCode is the wire error code.
func (e *RefreshLimitedError) RPCCode() string { return "refresh_limited" }

// RPCData carries when the next refresh is allowed.
func (e *RefreshLimitedError) RPCData() any {
	return map[string]any{"retryAt": e.RetryAt.UTC().Format(time.RFC3339)}
}

type taskMem struct {
	st        store.TaskPRState
	pr        *forge.PR // decoded st.Snapshot
	jitter    float64   // drawn once per fetch, so a due time does not flicker
	notBefore time.Time // start-up stagger
	rearm     bool      // a closed PR's branch moved: one more fetch
	tip       string    // last observed local tip (closed PRs only)
	tipAt     time.Time
}

type wsMem struct {
	repo     forge.Repo
	repoAt   time.Time
	haveRepo bool
	lastPoll time.Time // start of the last poll that made a call (the 15 s floor)
	lastList time.Time // last successful OpenPRs (cadence of tasks with no PR)
}

type hostMem struct {
	suspendedUntil time.Time
	suspendStale   string
	budgetUntil    time.Time
}

type wsFlight struct {
	done   chan struct{}
	err    error
	joined int // callers waiting on this flight instead of polling (tests wait on it)
}

type tracked struct {
	t       store.Task
	focused bool
}

// Poller owns every periodic forge call (ADR-0039 §4): one OpenPRs per due
// workspace, one PR call per PR that left the open list, under a per-host
// hourly budget, a per-workspace floor, backoff and a persisted ledger.
// Reads (PR) never call the forge.
type Poller struct {
	o      PollerOptions
	now    func() time.Time
	ctx    context.Context
	cancel context.CancelFunc

	budgetMu sync.Mutex // serialises budget check + add

	mu      sync.Mutex
	mem     map[int64]*taskMem
	ws      map[int64]*wsMem
	hosts   map[string]*hostMem
	flights map[int64]*wsFlight
}

// NewPoller builds a Poller; drive it with Tick.
func NewPoller(o PollerOptions) *Poller {
	p := &Poller{
		o: o, now: o.Now,
		mem: map[int64]*taskMem{}, ws: map[int64]*wsMem{},
		hosts: map[string]*hostMem{}, flights: map[int64]*wsFlight{},
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.o.Jitter == nil {
		p.o.Jitter = func() float64 { return rand.Float64()*2 - 1 }
	}
	if p.o.Tip == nil {
		run := o.Runner
		if run == nil {
			run = DefaultRunner
		}
		p.o.Tip = func(ctx context.Context, dir, branch string) string {
			return revParse(ctx, run, dir, "refs/heads/"+branch)
		}
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	return p
}

// Stop cancels in-flight forge calls and makes later polls no-ops.
func (p *Poller) Stop() {
	if p != nil {
		p.cancel()
	}
}

// Forget drops a task's in-memory state (archive/delete).
func (p *Poller) Forget(taskID int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	delete(p.mem, taskID)
	p.mu.Unlock()
}

func (p *Poller) provider() forge.Provider {
	if p.o.Provider == nil {
		return nil
	}
	return p.o.Provider()
}

func (p *Poller) memLocked(id int64) *taskMem {
	if m := p.mem[id]; m != nil {
		return m
	}
	m := &taskMem{}
	m.st.TaskID = id
	st, ok, err := p.o.Ledger.GetTaskPRState(id)
	switch {
	case err != nil:
		slog.Warn("gitstate: read task pr state", "task", id, "err", err)
	case ok:
		m.st = st
		if st.Snapshot != "" {
			var pr forge.PR
			if json.Unmarshal([]byte(st.Snapshot), &pr) == nil {
				m.pr = &pr
			}
		}
	}
	m.jitter = p.o.Jitter()
	m.notBefore = p.now().Add(time.Duration(float64(pollWatched) * jitterFraction * math.Abs(p.o.Jitter())))
	p.mem[id] = m
	return m
}

func (p *Poller) wsLocked(id int64) *wsMem {
	w := p.ws[id]
	if w == nil {
		w = &wsMem{}
		p.ws[id] = w
	}
	return w
}

func (p *Poller) hostLocked(host string) *hostMem {
	h := p.hosts[host]
	if h == nil {
		h = &hostMem{}
		p.hosts[host] = h
	}
	return h
}

func (p *Poller) hostOfLocked(ws int64) string {
	if w := p.ws[ws]; w != nil && w.haveRepo {
		return w.repo.Host
	}
	return defaultForgeHost
}

// current returns the snapshot only while it is the PR the task points at.
func (m *taskMem) current(t store.Task) *forge.PR {
	if m.pr == nil || t.PRNumber == 0 || int64(m.pr.Number) != t.PRNumber {
		return nil
	}
	return m.pr
}

func terminal(pr *forge.PR) bool {
	return pr != nil && (pr.State == forge.PRStateMerged || pr.State == forge.PRStateClosed)
}

// PR returns the task's PR snapshot decorated with the stale flag, from the
// persisted or cached snapshot only: it never calls the forge.
func (p *Poller) PR(t store.Task) *forge.PR {
	if p == nil || t.PRNumber == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.decorateLocked(t, p.memLocked(t.ID), p.now())
}

// ForgeBlock names the forge_* code that currently keeps the forge unusable
// for t's workspace, or "": the host is suspended because nobody is signed
// in, or the last attempt found the CLI missing or the origin not a forge
// remote and its backoff has not passed. It reads the poller's state only
// and never calls the forge.
func (p *Poller) ForgeBlock(t store.Task) string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if h := p.hosts[p.hostOfLocked(t.WorkspaceID)]; h != nil && h.suspendedUntil.After(now) && h.suspendStale == staleFor(forge.CodeUnauthenticated) {
		return forge.CodeUnauthenticated
	}
	m := p.memLocked(t.ID)
	if m.st.NextAllowedAt.After(now) {
		switch m.st.LastErrorCode {
		case forge.CodeCLIMissing, forge.CodeNoRemote:
			return m.st.LastErrorCode
		}
	}
	return ""
}

// RepoOf returns the forge repo the poller resolved for a workspace, from its
// in-memory cache only.
func (p *Poller) RepoOf(ws int64) (forge.Repo, bool) {
	if p == nil {
		return forge.Repo{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if w := p.ws[ws]; w != nil && w.haveRepo {
		return w.repo, true
	}
	return forge.Repo{}, false
}

// Adopt stores pr as t's pull request: its identity (forge, repo, number,
// validated url, head ref), the snapshot, and the task.updated notification.
// Create PR uses it for the PR it just opened.
func (p *Poller) Adopt(t store.Task, pr forge.PR, repo forge.Repo) {
	if p == nil {
		return
	}
	p.adopt(t, pr, repo, p.now())
}

// Record stores a freshly fetched pr as t's snapshot without touching its
// identity (Update PR refreshes the PR it just pushed to).
func (p *Poller) Record(t store.Task, pr forge.PR, repo forge.Repo) {
	if p == nil {
		return
	}
	p.succeed(t, pr, repo, p.now())
}

// NoteCalls books n explicit forge calls (Create PR, Update PR) against
// host's hourly budget so the poller sees them. A ledger failure is logged,
// not returned: the action already happened.
func (p *Poller) NoteCalls(host string, n int) {
	if p == nil || n <= 0 {
		return
	}
	if _, err := p.o.Ledger.AddForgeCalls(host, n, p.now()); err != nil {
		slog.Warn("gitstate: book forge calls", "err", err)
	}
}

func (p *Poller) decorateLocked(t store.Task, m *taskMem, now time.Time) *forge.PR {
	snap := m.current(t)
	if snap == nil {
		return nil
	}
	cp := *snap
	if snap.Checks != nil {
		c := *snap.Checks
		cp.Checks = &c
	}
	cp.Stale, cp.RefreshAvailableAt = "", ""
	until := time.Time{}
	h := p.hosts[p.hostOfLocked(t.WorkspaceID)]
	switch {
	case h != nil && h.suspendedUntil.After(now):
		cp.Stale, until = h.suspendStale, h.suspendedUntil
	case m.st.LastErrorCode != "" && m.st.NextAllowedAt.After(now):
		cp.Stale, until = staleFor(m.st.LastErrorCode), m.st.NextAllowedAt
	case h != nil && h.budgetUntil.After(now):
		cp.Stale, until = "budget", h.budgetUntil
	}
	if cp.Stale != "" {
		cp.RefreshAvailableAt = until.UTC().Format(time.RFC3339)
	}
	return &cp
}

func staleFor(code string) string {
	switch code {
	case forge.CodeRateLimited:
		return "rate_limited"
	case forge.CodeUnauthenticated:
		return "unauthenticated"
	case forge.CodeCLIMissing, forge.CodeNoRemote, forge.CodeNotFound:
		return "unavailable"
	}
	return "backoff"
}

// prKey is a PR view's canonical JSON without the fields that change on every
// fetch (fetchedAt, refreshAvailableAt); equal keys are not news.
func prKey(pr *forge.PR) string {
	if pr == nil {
		return ""
	}
	c := *pr
	c.FetchedAt, c.RefreshAvailableAt = time.Time{}, ""
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(b)
}

func openInterval(focused bool, pr *forge.PR) time.Duration {
	switch {
	case focused && pr != nil && pr.Checks != nil && pr.Checks.State == forge.ChecksPending:
		return pollFocusedPending
	case focused:
		return pollFocused
	}
	return pollWatched
}

func spread(d time.Duration, j float64) time.Duration {
	return time.Duration(float64(d) * jitterFraction * j)
}

// collect resolves task ids (watched, focused and extra) to the tasks the
// poller tracks: not archived, not system, with a worktree.
func (p *Poller) collect(extra ...int64) map[int64][]tracked {
	focused := map[int64]bool{}
	ids := map[int64]bool{}
	if in := p.o.Interest; in != nil {
		for _, id := range in.Focused() {
			focused[id], ids[id] = true, true
		}
	}
	if p.o.Tasks != nil {
		for _, id := range p.o.Tasks() {
			ids[id] = true
		}
	}
	for _, id := range extra {
		if id != 0 {
			ids[id] = true
		}
	}
	order := make([]int64, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := map[int64][]tracked{}
	for _, id := range order {
		t, err := p.o.Source.GetTask(id)
		if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" || t.WorktreePath == nil || *t.WorktreePath == "" {
			continue
		}
		out[t.WorkspaceID] = append(out[t.WorkspaceID], tracked{t: t, focused: focused[id]})
	}
	return out
}

// Tick is one beat: nothing without a watcher; otherwise poll each due
// workspace, one after the other, so a failure that pauses the host (rate
// limit, no login) is seen before the next workspace would repeat the call.
func (p *Poller) Tick() {
	if p == nil || p.o.Interest == nil || !p.o.Interest.Watched() || p.ctx.Err() != nil {
		return
	}
	now := p.now()
	groups := p.collect()
	wsIDs := make([]int64, 0, len(groups))
	for ws := range groups {
		wsIDs = append(wsIDs, ws)
	}
	sort.Slice(wsIDs, func(i, j int) bool { return wsIDs[i] < wsIDs[j] })
	for _, ws := range wsIDs {
		p.rearmCheck(ws, groups[ws], now)
		if due, ok := p.wsDue(ws, groups[ws]); ok && !now.Before(due) {
			_ = p.pollWS(ws, 0)
		}
	}
}

// rearmCheck re-arms one fetch for a closed (not merged) PR whose local
// branch tip moved to something other than the PR's head (a push to a closed
// PR's branch). The first observation is only a baseline.
func (p *Poller) rearmCheck(ws int64, ts []tracked, now time.Time) {
	for _, tr := range ts {
		p.mu.Lock()
		m := p.memLocked(tr.t.ID)
		snap := m.current(tr.t)
		need := snap != nil && snap.State == forge.PRStateClosed && !m.rearm &&
			tr.t.Branch != nil && now.Sub(m.tipAt) >= tipCheckEvery
		head := ""
		if snap != nil {
			head = snap.HeadSha
		}
		p.mu.Unlock()
		if !need {
			continue
		}
		w, err := p.o.Source.GetWorkspace(ws)
		if err != nil {
			continue
		}
		tip := p.o.Tip(p.ctx, w.Path, *tr.t.Branch)
		p.mu.Lock()
		m.tipAt = now
		if tip != "" {
			if m.tip != "" && tip != m.tip && tip != head {
				m.rearm = true
			}
			m.tip = tip
		}
		p.mu.Unlock()
	}
}

// wsDue is when the workspace may next be polled: the earliest due time of
// its tasks, never before a task's next_allowed_at or start-up stagger, the
// workspace's 15 s floor or a host suspension. ok is false when nothing in it
// needs polling (every PR is terminal).
func (p *Poller) wsDue(ws int64, ts []tracked) (due time.Time, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	wm := p.wsLocked(ws)
	for _, tr := range ts {
		d, need := p.taskDueLocked(tr, wm)
		if need && (!ok || d.Before(due)) {
			due, ok = d, true
		}
	}
	if !ok {
		return time.Time{}, false
	}
	due = laterOf(due, wm.lastPoll.Add(repoFloor))
	if h := p.hosts[p.hostOfLocked(ws)]; h != nil {
		due = laterOf(due, laterOf(h.suspendedUntil, h.budgetUntil))
	}
	return due, true
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (p *Poller) taskDueLocked(tr tracked, wm *wsMem) (time.Time, bool) {
	m := p.memLocked(tr.t.ID)
	var base time.Time
	switch snap := m.current(tr.t); {
	case tr.t.PRNumber > 0 && terminal(snap) && !m.rearm:
		return time.Time{}, false
	case tr.t.PRNumber > 0 && snap != nil && !m.rearm:
		iv := openInterval(tr.focused, snap)
		base = m.st.FetchedAt.Add(iv + spread(iv, m.jitter))
	case tr.t.PRNumber > 0:
		// identity without a snapshot yet, or a re-armed closed PR: due now
	default:
		if tr.t.Branch == nil || *tr.t.Branch == "" {
			return time.Time{}, false
		}
		iv := pollWatched
		if tr.focused {
			iv = pollFocused
		}
		base = wm.lastList.Add(iv + spread(iv, m.jitter))
	}
	return laterOf(laterOf(base, m.st.NextAllowedAt), m.notBefore), true
}

// Refresh is task.gitRefresh: one forced poll of the task's workspace under
// the per-task floor (5 s) and hourly cap (12), counting against the host
// budget. A poll already in flight for the workspace is joined at no cost.
// It returns the forge error of the poll, if any; callers decide whether a
// cached snapshot makes that moot.
func (p *Poller) Refresh(taskID int64) error {
	t, err := p.o.Source.GetTask(taskID)
	if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" {
		return ErrNotTracked
	}
	p.mu.Lock()
	if p.flights[t.WorkspaceID] == nil {
		if lim := p.refreshLimitLocked(p.memLocked(taskID), p.now()); lim != nil {
			p.mu.Unlock()
			return lim
		}
	}
	p.mu.Unlock()
	return p.pollWS(t.WorkspaceID, taskID)
}

func (p *Poller) refreshLimitLocked(m *taskMem, now time.Time) *RefreshLimitedError {
	nowMs := now.UnixMilli()
	var inHour []int64
	for _, ms := range m.st.ForcedAt {
		if nowMs-ms < time.Hour.Milliseconds() {
			inHour = append(inHour, ms)
		}
	}
	if n := len(inHour); n > 0 {
		last := inHour[n-1]
		if nowMs-last < refreshFloor.Milliseconds() {
			return &RefreshLimitedError{RetryAt: time.UnixMilli(last).Add(refreshFloor)}
		}
	}
	if len(inHour) >= refreshPerHour {
		return &RefreshLimitedError{RetryAt: time.UnixMilli(inHour[0]).Add(time.Hour), Hourly: true}
	}
	return nil
}

func (p *Poller) stampForced(taskID int64, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.memLocked(taskID)
	m.st.ForcedAt = append(m.st.ForcedAt, now.UnixMilli())
	p.saveLocked(m)
}

func (p *Poller) saveLocked(m *taskMem) {
	if err := p.o.Ledger.SaveTaskPRState(m.st); err != nil {
		slog.Warn("gitstate: save task pr state", "task", m.st.TaskID, "err", err)
	}
}

// pollWS runs one poll of a workspace, single-flight: a concurrent caller
// joins the call in progress and spends nothing. forceTask is the task of an
// explicit refresh (0 for a tick).
func (p *Poller) pollWS(ws, forceTask int64) error {
	p.mu.Lock()
	if f := p.flights[ws]; f != nil {
		f.joined++
		p.mu.Unlock()
		select {
		case <-f.done:
			return f.err
		case <-p.ctx.Done():
			return p.ctx.Err()
		}
	}
	f := &wsFlight{done: make(chan struct{})}
	p.flights[ws] = f
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.flights, ws)
		p.mu.Unlock()
		close(f.done)
	}()
	f.err = p.runPoll(ws, forceTask)
	return f.err
}

func (p *Poller) viewKeys(ts []tracked) map[int64]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	out := make(map[int64]string, len(ts))
	for _, tr := range ts {
		out[tr.t.ID] = prKey(p.decorateLocked(tr.t, p.memLocked(tr.t.ID), now))
	}
	return out
}

func (p *Poller) runPoll(ws, forceTask int64) (err error) {
	prov := p.provider()
	if prov == nil || p.ctx.Err() != nil {
		return nil
	}
	w, werr := p.o.Source.GetWorkspace(ws)
	if werr != nil {
		return nil
	}
	ts := p.collect(forceTask)[ws]
	if len(ts) == 0 {
		return nil
	}
	if forceTask == 0 {
		// Two ticks can both find the workspace due; the one that gets here
		// second sees the first's result and has nothing left to do.
		if due, ok := p.wsDue(ws, ts); !ok || p.now().Before(due) {
			return nil
		}
	}
	before := p.viewKeys(ts)
	defer func() {
		// A task whose pr view changed (new snapshot, stale flag) is recomputed
		// and published through the service's coalesced path.
		after := p.viewKeys(ts)
		for _, tr := range ts {
			if before[tr.t.ID] != after[tr.t.ID] && p.o.OnChange != nil {
				p.o.OnChange(tr.t.ID)
			}
		}
	}()

	now := p.now()
	p.mu.Lock()
	host := p.hostOfLocked(ws)
	h := p.hosts[host]
	blocked := h != nil && (h.suspendedUntil.After(now) || h.budgetUntil.After(now))
	if forceTask != 0 && p.memLocked(forceTask).st.NextAllowedAt.After(now) {
		blocked = true // a forced poll bypasses the interval, not a backoff
	}
	p.mu.Unlock()
	if blocked {
		return nil
	}

	repo, ok, rerr := p.resolveRepo(prov, ws, w.Path, host, now)
	if rerr != nil {
		p.fail(ts, host, rerr)
		return rerr
	}
	if !ok {
		return nil // over budget
	}
	if !p.reserve(repo.Host, now) {
		return nil
	}
	if forceTask != 0 {
		p.stampForced(forceTask, now)
	}
	p.mu.Lock()
	p.wsLocked(ws).lastPoll = now
	p.mu.Unlock()

	open, lerr := prov.OpenPRs(p.ctx, w.Path)
	if lerr != nil {
		p.fail(ts, repo.Host, lerr)
		return lerr
	}
	return p.applyList(prov, w.Path, ws, repo, ts, open)
}

// resolveRepo returns the workspace's forge repo, calling Repo at most once
// per hour. ok is false when the call was not allowed by the budget.
func (p *Poller) resolveRepo(prov forge.Provider, ws int64, dir, host string, now time.Time) (repo forge.Repo, ok bool, err error) {
	p.mu.Lock()
	wm := p.wsLocked(ws)
	if wm.haveRepo && now.Sub(wm.repoAt) < repoCacheTTL {
		repo = wm.repo
		p.mu.Unlock()
		return repo, true, nil
	}
	p.mu.Unlock()
	if !p.reserve(host, now) {
		return forge.Repo{}, false, nil
	}
	repo, err = prov.Repo(p.ctx, dir)
	if err != nil {
		return forge.Repo{}, false, err
	}
	p.mu.Lock()
	wm.repo, wm.repoAt, wm.haveRepo = repo, now, true
	p.mu.Unlock()
	return repo, true, nil
}

// reserve books one forge call against host's hourly budget; false means no
// call may be made (the cache is served, nothing is queued).
func (p *Poller) reserve(host string, now time.Time) bool {
	p.budgetMu.Lock()
	defer p.budgetMu.Unlock()
	p.mu.Lock()
	h := p.hostLocked(host)
	if h.budgetUntil.After(now) {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()
	calls, start, err := p.o.Ledger.GetForgeBudget(host, now)
	if err != nil {
		slog.Warn("gitstate: read forge budget", "err", err)
		return false
	}
	if calls >= hourlyBudget {
		p.mu.Lock()
		p.hostLocked(host).budgetUntil = start.Add(time.Hour)
		p.mu.Unlock()
		return false
	}
	if _, err := p.o.Ledger.AddForgeCalls(host, 1, now); err != nil {
		slog.Warn("gitstate: book forge call", "err", err)
		return false
	}
	return true
}

// applyList folds one OpenPRs answer into the workspace's tasks: refresh the
// ones in the list, adopt unowned tasks whose branch has an open same-repo PR,
// and look up (one PR call each) the open PRs that left the list.
func (p *Poller) applyList(prov forge.Provider, dir string, ws int64, repo forge.Repo, ts []tracked, open []forge.PR) error {
	now := p.now()
	byNumber := make(map[int64]forge.PR, len(open))
	byHead := map[string]forge.PR{}
	for _, pr := range open {
		byNumber[int64(pr.Number)] = pr
		if !pr.FromFork && pr.HeadRef != "" {
			if _, dup := byHead[pr.HeadRef]; !dup {
				byHead[pr.HeadRef] = pr
			}
		}
	}
	p.mu.Lock()
	p.wsLocked(ws).lastList = now
	for _, tr := range ts {
		// The list call worked: an earlier failure of the workspace is over.
		if m := p.memLocked(tr.t.ID); m.st.ConsecutiveFailures != 0 || m.st.LastErrorCode != "" || !m.st.NextAllowedAt.IsZero() {
			m.st.ConsecutiveFailures, m.st.LastErrorCode, m.st.NextAllowedAt = 0, "", time.Time{}
			p.saveLocked(m)
		}
	}
	p.mu.Unlock()

	var lookups []tracked
	for _, tr := range ts {
		t := tr.t
		switch {
		case t.PRNumber > 0:
			if t.PRRepo != "" && t.PRRepo != repo.NameWithOwner {
				continue // another repo's PR: not this list's to answer
			}
			if pr, ok := byNumber[t.PRNumber]; ok {
				p.succeed(t, pr, repo, now)
				continue
			}
			p.mu.Lock()
			m := p.memLocked(t.ID)
			snap := m.current(t)
			need := snap == nil || !terminal(snap) || m.rearm
			p.mu.Unlock()
			if need {
				lookups = append(lookups, tr)
			}
		default:
			head := t.PRHeadRef
			if head == "" && t.Branch != nil {
				head = *t.Branch
			}
			pr, ok := byHead[head]
			if ok && head != "" {
				p.adopt(t, pr, repo, now)
			}
		}
	}

	var lastErr error
	for _, tr := range lookups {
		if !p.reserve(repo.Host, p.now()) {
			break
		}
		pr, err := prov.PR(p.ctx, dir, int(tr.t.PRNumber))
		if err != nil {
			scope := []tracked{tr}
			if c := forgeCode(err); c == forge.CodeRateLimited || c == forge.CodeUnauthenticated {
				scope = ts
			}
			p.fail(scope, repo.Host, err)
			lastErr = err
			break // one failing view ends the poll; nothing is retried
		}
		p.succeed(tr.t, pr, repo, p.now())
	}
	return lastErr
}

func forgeCode(err error) string {
	var fe *forge.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return forge.CodeError
}

// succeed stores a fetched PR as the task's snapshot and resets its failures.
func (p *Poller) succeed(t store.Task, pr forge.PR, repo forge.Repo, now time.Time) {
	pr.Stale, pr.RefreshAvailableAt = "", ""
	pr.FetchedAt = now.UTC()
	if pr.Repo == "" {
		pr.Repo = repo.NameWithOwner
	}
	b, err := json.Marshal(pr)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.memLocked(t.ID)
	m.pr = &pr
	m.st.Snapshot = string(b)
	m.st.FetchedAt = now.UTC()
	m.st.NextAllowedAt = time.Time{}
	m.st.ConsecutiveFailures, m.st.LastErrorCode = 0, ""
	m.jitter = p.o.Jitter()
	m.rearm = false
	p.saveLocked(m)
}

// adopt writes the PR identity of a task whose branch has an open same-repo PR
// and stores the snapshot. A URL off the forge host is never stored.
func (p *Poller) adopt(t store.Task, pr forge.PR, repo forge.Repo, now time.Time) {
	url, ok := forge.ValidPRURL(pr.URL, repo.Host)
	if !ok {
		return
	}
	if err := p.o.Ledger.SetTaskPR(t.ID, "github", repo.NameWithOwner, int64(pr.Number), url, pr.HeadRef); err != nil {
		slog.Warn("gitstate: adopt pr", "task", t.ID, "err", err)
		return
	}
	pr.URL = url
	t.PRNumber = int64(pr.Number)
	p.succeed(t, pr, repo, now)
	if p.o.OnAdopt != nil {
		p.o.OnAdopt(t.ID)
	}
}

// fail books a failed forge call on tasks: count, code and the next time a
// call is allowed. A rate limit or a missing login also pauses the whole host.
func (p *Poller) fail(tasks []tracked, host string, err error) {
	if p.ctx.Err() != nil {
		return
	}
	now := p.now()
	code := forge.CodeError
	var retryAt time.Time
	var fe *forge.Error
	if errors.As(err, &fe) {
		code, retryAt = fe.Code, fe.RetryAt
	}
	until := now.Add(hostSuspendFor)
	if code == forge.CodeRateLimited {
		until = laterOf(until, retryAt)
		if limit := now.Add(rateLimitMax); until.After(limit) {
			until = limit
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	hostWide := code == forge.CodeRateLimited || code == forge.CodeUnauthenticated
	if hostWide {
		h := p.hostLocked(host)
		h.suspendedUntil, h.suspendStale = until, staleFor(code)
	}
	for _, tr := range tasks {
		m := p.memLocked(tr.t.ID)
		m.st.ConsecutiveFailures++
		m.st.LastErrorCode = code
		if hostWide {
			m.st.NextAllowedAt = until
		} else {
			m.st.NextAllowedAt = now.Add(backoff(m.st.ConsecutiveFailures))
		}
		p.saveLocked(m)
	}
}
