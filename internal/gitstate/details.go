package gitstate

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// The PR panel's reads (docs/plans/active/pr-panel.md, decisions 3-5, 7, 9).
// Nothing here runs on a timer: the cache is filled by reads, and a read only
// fetches for a connection that focused the PR tab.
const (
	detailsSettledEvery = pollFocused        // first read after a fresh focus
	detailsPendingEvery = pollFocusedPending // checks still running
	// detailsCalls is what one detail fetch costs the host budget: the
	// `gh pr view` and the GraphQL call.
	detailsCalls = 2
)

// detailsFlight is one in-flight fetch of a task's details; concurrent reads
// join it and share its outcome, spending nothing.
type detailsFlight struct {
	done chan struct{}
	d    forge.PRDetails
	err  error
}

// logFlight is one in-flight check-log fetch; the log is never cached beyond
// the callers that joined it.
type logFlight struct {
	done chan struct{}
	tail forge.CheckLogTail
	err  error
}

// detailsMem is a task's in-memory PR panel state. It is never persisted: a
// restart simply refetches on the next focused read.
type detailsMem struct {
	number   int64
	d        *forge.PRDetails
	at       time.Time
	fresh    bool // a connection focused the tab and has not read yet
	flight   *detailsFlight
	logs     map[string]*logFlight
	failures int
	// nextAllowed/lastCode are the per-task backoff after a failed fetch;
	// a rate limit or missing login also suspends the whole host.
	nextAllowed time.Time
	lastCode    string
	// forced and logAt are the explicit-refresh and check-log ledgers: the
	// times of the calls made in the last hour (in memory only; the daemon
	// restarting forgets them, which is safe: the host budget is persisted).
	forced []time.Time
	logAt  map[string]time.Time
	logged []time.Time
}

func (p *Poller) detLocked(id int64) *detailsMem {
	if p.det == nil {
		p.det = map[int64]*detailsMem{}
	}
	m := p.det[id]
	if m == nil {
		m = &detailsMem{logs: map[string]*logFlight{}, logAt: map[string]time.Time{}}
		p.det[id] = m
	}
	return m
}

// NoteDetailsFocus records that a connection just focused the task's PR tab:
// the next read may refetch once if the cache is older than 120 s. It calls
// nothing.
func (p *Poller) NoteDetailsFocus(taskID int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.detLocked(taskID).fresh = true
	p.mu.Unlock()
}

// reserveN books n forge calls against host's hourly budget atomically: all
// or none. When they do not fit nothing is booked and until is when the
// window resets. The host-wide budget stop (which also pauses the list poll)
// is only set when the budget is fully spent.
func (p *Poller) reserveN(host string, n int, now time.Time) (ok bool, until time.Time) {
	p.budgetMu.Lock()
	defer p.budgetMu.Unlock()
	p.mu.Lock()
	h := p.hostLocked(host)
	if h.budgetUntil.After(now) {
		until = h.budgetUntil
		p.mu.Unlock()
		return false, until
	}
	p.mu.Unlock()
	calls, start, err := p.o.Ledger.GetForgeBudget(host, now)
	if err != nil {
		slog.Warn("gitstate: read forge budget", "err", err)
		return false, now.Add(backoffSteps[0])
	}
	if calls+n > hourlyBudget {
		until = start.Add(time.Hour)
		if calls >= hourlyBudget {
			p.mu.Lock()
			p.hostLocked(host).budgetUntil = until
			p.mu.Unlock()
		}
		return false, until
	}
	if _, err := p.o.Ledger.AddForgeCalls(host, n, now); err != nil {
		slog.Warn("gitstate: book forge calls", "err", err)
		return false, now.Add(backoffSteps[0])
	}
	return true, time.Time{}
}

// limitLocked is refreshLimitLocked over an in-memory ledger: 5 s between
// calls and 12 per hour.
func limitLocked(times []time.Time, now time.Time) (kept []time.Time, lim *RefreshLimitedError) {
	for _, t := range times {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	if n := len(kept); n > 0 && now.Sub(kept[n-1]) < refreshFloor {
		return kept, &RefreshLimitedError{RetryAt: kept[n-1].Add(refreshFloor)}
	}
	if len(kept) >= refreshPerHour {
		return kept, &RefreshLimitedError{RetryAt: kept[0].Add(time.Hour), Hourly: true}
	}
	return kept, nil
}

// detailsTask resolves taskID to the task, its workspace dir and its PR
// number, with the typed errors of the PR panel.
func (p *Poller) detailsTask(taskID int64) (t store.Task, dir string, err error) {
	t, err = p.o.Source.GetTask(taskID)
	if err != nil || t.Kind == store.TaskKindSystem || t.Status == "archived" {
		return t, "", ErrNotTracked
	}
	if t.PRNumber <= 0 {
		return t, "", forge.NewPRError(forge.CodeNoPR, "")
	}
	if t.WorktreePath == nil || *t.WorktreePath == "" {
		return t, "", forge.NewPRError(forge.CodeTaskNoWorktree, "")
	}
	w, werr := p.o.Source.GetWorkspace(t.WorkspaceID)
	if werr != nil {
		return t, "", ErrNotTracked
	}
	return t, w.Path, nil
}

// blockLocked says why a forge call for a task of ws must not be made now:
// a host suspension, the task's backoff or the host budget. stale is the
// wire reason and until when a call becomes possible.
func (p *Poller) blockLocked(m *detailsMem, ws int64, now time.Time) (stale string, until time.Time) {
	if h := p.hosts[p.hostOfLocked(ws)]; h != nil {
		switch {
		case h.suspendedUntil.After(now):
			return h.suspendStale, h.suspendedUntil
		case h.budgetUntil.After(now):
			return forge.StaleBudget, h.budgetUntil
		}
	}
	if m.nextAllowed.After(now) {
		return staleFor(m.lastCode), m.nextAllowed
	}
	return "", time.Time{}
}

// codeForStale rebuilds a typed error for a read that has no cache to show.
func codeForStale(stale string, until time.Time) *forge.Error {
	e := forge.NewError(forge.CodeError, "")
	switch stale {
	case forge.StaleUnauthenticated:
		e = forge.NewError(forge.CodeUnauthenticated, "")
	case forge.StaleRateLimited:
		e = forge.NewError(forge.CodeRateLimited, "")
		e.RetryAt = until
	case forge.StaleUnavailable:
		e = forge.NewError(forge.CodeNotFound, "")
	}
	return e
}

// detailsView is the cached details decorated for the caller.
func detailsView(m *detailsMem, t store.Task, stale string, until time.Time) forge.PRDetails {
	var d forge.PRDetails
	if m.d != nil {
		d = *m.d // slices are shared and never mutated
	} else {
		d = forge.PRDetails{
			Number: int(t.PRNumber), Repo: t.PRRepo, URL: t.PRURL,
			Checks:  forge.DetailChecks{Items: []forge.CheckItem{}},
			Reviews: forge.DetailReviews{Items: []forge.ReviewItem{}, Awaiting: []string{}},
			Threads: forge.DetailThreads{Items: []forge.Thread{}},
		}
	}
	d.TaskID = t.ID
	d.Stale, d.RefreshAvailableAt = stale, ""
	if stale != "" && !until.IsZero() {
		// RFC3339 has second resolution: round UP, so a client that waits
		// until this time is never early and re-answered with the same value.
		u := until.UTC()
		if u.Nanosecond() > 0 {
			u = u.Truncate(time.Second).Add(time.Second)
		}
		d.RefreshAvailableAt = u.Format(time.RFC3339)
	}
	return d
}

// PRDetails is the PR panel's read. focused says the calling connection
// focused this task's PR tab (task.prDetailsFocus); only then may it fetch.
// refresh is the explicit refresh: it bypasses the cadence below, never the
// floors. It returns the cached details (with the stale flag) alongside a
// *RefreshLimitedError; a typed forge error comes back only when there is
// nothing cached to show.
//
// Fetch rule for a focused read: no cache; the first read after a fresh focus
// with a cache older than 120 s; checks pending and cache older than 20 s.
// Otherwise the cache is served. One fetch = two gh calls, booked together
// against the host budget, under one single-flight per task and one repo-floor
// slot (the same lastPoll the list poll uses).
func (p *Poller) PRDetails(taskID int64, focused, refresh bool) (forge.PRDetails, error) {
	t, dir, err := p.detailsTask(taskID)
	if err != nil {
		return forge.PRDetails{}, err
	}
	now := p.now()

	p.mu.Lock()
	m := p.detLocked(taskID)
	if m.d != nil && int64(m.number) != t.PRNumber {
		m.d, m.at = nil, time.Time{}
	}
	m.number = t.PRNumber
	fresh := m.fresh
	if focused {
		m.fresh = false
	}
	if !focused {
		v := detailsView(m, t, forge.StaleNotFocused, time.Time{})
		p.mu.Unlock()
		return v, nil
	}
	if f := m.flight; f != nil {
		p.mu.Unlock()
		return p.joinDetails(f)
	}
	age := now.Sub(m.at)
	need := refresh || m.d == nil ||
		(fresh && age >= detailsSettledEvery) ||
		(m.d.Checks.State == forge.ChecksPending && age >= detailsPendingEvery)
	if !need {
		v := detailsView(m, t, "", time.Time{})
		p.mu.Unlock()
		return v, nil
	}
	if refresh {
		kept, lim := limitLocked(m.forced, now)
		m.forced = kept
		if lim != nil {
			v := detailsView(m, t, "", time.Time{})
			p.mu.Unlock()
			return v, lim
		}
	}
	wm := p.wsLocked(t.WorkspaceID)
	if stale, until := p.blockLocked(m, t.WorkspaceID, now); stale != "" {
		return p.blockedLocked(m, t, stale, until)
	}
	if floorEnd := wm.lastPoll.Add(repoFloor); floorEnd.After(now) {
		if refresh && m.d != nil {
			v := detailsView(m, t, "", time.Time{})
			p.mu.Unlock()
			return v, &RefreshLimitedError{RetryAt: floorEnd}
		}
		if m.d == nil {
			// Nothing to show yet: wait out the floor rather than an empty tab.
			v := detailsView(m, t, forge.StaleBackoff, floorEnd)
			p.mu.Unlock()
			return v, nil
		}
		v := detailsView(m, t, "", floorEnd)
		p.mu.Unlock()
		return v, nil
	}
	f := &detailsFlight{done: make(chan struct{})}
	m.flight = f
	p.mu.Unlock()

	f.d, f.err = p.fetchDetails(t, dir, m, refresh, now)
	p.mu.Lock()
	m.flight = nil
	p.mu.Unlock()
	close(f.done)
	return f.d, f.err
}

func (p *Poller) joinDetails(f *detailsFlight) (forge.PRDetails, error) {
	select {
	case <-f.done:
		return f.d, f.err
	case <-p.ctx.Done():
		return forge.PRDetails{}, p.ctx.Err()
	}
}

// blockedLocked answers a read that needed a fetch the host state forbids:
// the cache with its stale reason, or the typed error when there is none.
// It unlocks p.mu.
func (p *Poller) blockedLocked(m *detailsMem, t store.Task, stale string, until time.Time) (forge.PRDetails, error) {
	if m.d == nil && stale != forge.StaleBudget {
		p.mu.Unlock()
		return forge.PRDetails{}, codeForStale(stale, until)
	}
	v := detailsView(m, t, stale, until)
	p.mu.Unlock()
	return v, nil
}

// fetchDetails runs one budgeted detail fetch. The caller holds the task's
// flight; p.mu is not held.
func (p *Poller) fetchDetails(t store.Task, dir string, m *detailsMem, refresh bool, now time.Time) (forge.PRDetails, error) {
	prov := p.provider()
	if prov == nil || p.ctx.Err() != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if m.d == nil {
			return forge.PRDetails{}, forge.NewError(forge.CodeCLIMissing, "")
		}
		return detailsView(m, t, forge.StaleUnavailable, time.Time{}), nil
	}
	p.mu.Lock()
	host := p.hostOfLocked(t.WorkspaceID)
	p.mu.Unlock()
	repo, ok, rerr := p.resolveRepo(prov, t.WorkspaceID, dir, host, now)
	if rerr != nil {
		return p.detailsFailed(t, m, host, rerr)
	}
	if !ok {
		return p.detailsBudget(t, m, host, now)
	}
	if ok, until := p.reserveN(repo.Host, detailsCalls, now); !ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		return detailsView(m, t, forge.StaleBudget, until), nil
	}
	p.mu.Lock()
	if refresh {
		m.forced = append(m.forced, now)
	}
	p.wsLocked(t.WorkspaceID).lastPoll = now
	p.mu.Unlock()

	d, err := prov.PRDetails(p.ctx, dir, int(t.PRNumber))
	if err != nil {
		return p.detailsFailed(t, m, repo.Host, err)
	}
	d.TaskID = t.ID
	d.Stale, d.RefreshAvailableAt = "", ""
	d.FetchedAt = now.UTC()
	p.mu.Lock()
	defer p.mu.Unlock()
	m.d, m.at = &d, now
	m.failures, m.lastCode, m.nextAllowed = 0, "", time.Time{}
	return detailsView(m, t, "", time.Time{}), nil
}

func (p *Poller) detailsBudget(t store.Task, m *detailsMem, host string, now time.Time) (forge.PRDetails, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	until := now.Add(backoffSteps[0])
	if h := p.hosts[host]; h != nil && h.budgetUntil.After(now) {
		until = h.budgetUntil
	}
	return detailsView(m, t, forge.StaleBudget, until), nil
}

// hostFailLocked books a failed call: a rate limit or missing login suspends
// the host for every poll (the same state the list poll's fail sets); other
// failures only back this task off. It returns the code.
func (p *Poller) hostFailLocked(host string, err error, now time.Time) (code string, until time.Time) {
	code = forgeCode(err)
	var fe *forge.Error
	var retryAt time.Time
	if errors.As(err, &fe) {
		retryAt = fe.RetryAt
	}
	until = now.Add(hostSuspendFor)
	if code == forge.CodeRateLimited {
		until = laterOf(until, retryAt)
		if limit := now.Add(rateLimitMax); until.After(limit) {
			until = limit
		}
	}
	if code == forge.CodeRateLimited || code == forge.CodeUnauthenticated {
		h := p.hostLocked(host)
		h.suspendedUntil, h.suspendStale = until, staleFor(code)
	}
	return code, until
}

// detailsFailed books a failed detail fetch and returns the cache with the
// stale reason when there is one, else the typed error.
func (p *Poller) detailsFailed(t store.Task, m *detailsMem, host string, err error) (forge.PRDetails, error) {
	if p.ctx.Err() != nil {
		return forge.PRDetails{}, err
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	code, until := p.hostFailLocked(host, err, now)
	m.failures++
	m.lastCode = code
	if code == forge.CodeRateLimited || code == forge.CodeUnauthenticated {
		m.nextAllowed = until
	} else {
		m.nextAllowed = now.Add(backoff(m.failures))
	}
	if m.d == nil {
		return forge.PRDetails{}, err
	}
	return detailsView(m, t, staleFor(code), m.nextAllowed), nil
}

// CheckLog is task.prCheckLog: the tail of a failing check's log, fetched
// only when asked (one budgeted call). checkID must be an id the daemon issued
// in the task's cached details; nothing client-supplied reaches the forge CLI.
// A click is limited like an explicit refresh (5 s per check, 12 per hour per
// task, kept in memory) and, like every forge call, by the host's suspension
// and hourly budget. The log is not cached beyond callers that joined it.
func (p *Poller) CheckLog(taskID int64, checkID string) (forge.CheckLogTail, error) {
	t, dir, err := p.detailsTask(taskID)
	if err != nil {
		return forge.CheckLogTail{}, err
	}
	now := p.now()
	p.mu.Lock()
	m := p.detLocked(taskID)
	known := false
	if m.d != nil && m.number == t.PRNumber {
		for _, it := range m.d.Checks.Items {
			if it.ID != "" && it.ID == checkID {
				known = true
				break
			}
		}
	}
	runID, jobID, parsed := forge.ParseCheckID(checkID)
	if !known || !parsed {
		p.mu.Unlock()
		return forge.CheckLogTail{}, forge.CheckUnknown()
	}
	if f := m.logs[checkID]; f != nil {
		p.mu.Unlock()
		select {
		case <-f.done:
			return f.tail, f.err
		case <-p.ctx.Done():
			return forge.CheckLogTail{}, p.ctx.Err()
		}
	}
	var lim *RefreshLimitedError
	m.logged, lim = limitLocked(m.logged, now)
	if lim == nil {
		if at, ok := m.logAt[checkID]; ok && now.Sub(at) < refreshFloor {
			lim = &RefreshLimitedError{RetryAt: at.Add(refreshFloor)}
		}
	}
	if lim != nil {
		p.mu.Unlock()
		return forge.CheckLogTail{}, lim
	}
	if stale, until := p.blockLocked(m, t.WorkspaceID, now); stale != "" && stale != forge.StaleBackoff && stale != forge.StaleUnavailable {
		p.mu.Unlock()
		if stale == forge.StaleBudget {
			return forge.CheckLogTail{}, &RefreshLimitedError{RetryAt: until, Hourly: true}
		}
		return forge.CheckLogTail{}, codeForStale(stale, until)
	}
	host := p.hostOfLocked(t.WorkspaceID)
	f := &logFlight{done: make(chan struct{})}
	m.logs[checkID] = f
	p.mu.Unlock()

	f.tail, f.err = p.fetchLog(t, dir, m, host, checkID, runID, jobID, now)
	p.mu.Lock()
	delete(m.logs, checkID)
	p.mu.Unlock()
	close(f.done)
	return f.tail, f.err
}

func (p *Poller) fetchLog(t store.Task, dir string, m *detailsMem, host, checkID string, runID, jobID int64, now time.Time) (forge.CheckLogTail, error) {
	prov := p.provider()
	if prov == nil || p.ctx.Err() != nil {
		return forge.CheckLogTail{}, forge.NewError(forge.CodeCLIMissing, "")
	}
	if ok, until := p.reserveN(host, 1, now); !ok {
		return forge.CheckLogTail{}, &RefreshLimitedError{RetryAt: until, Hourly: true}
	}
	p.mu.Lock()
	m.logged = append(m.logged, now)
	m.logAt[checkID] = now
	p.mu.Unlock()
	raw, err := prov.CheckLog(p.ctx, dir, runID, jobID)
	if err != nil {
		if p.ctx.Err() == nil {
			p.mu.Lock()
			p.hostFailLocked(host, err, p.now())
			p.mu.Unlock()
		}
		return forge.CheckLogTail{}, err
	}
	cut := strings.HasPrefix(raw, forge.LogCutMarker)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, forge.LogCutMarker), "\n")
	text, cut2 := forge.TailLog(raw)
	lines := 0
	if text != "" {
		lines = strings.Count(text, "\n") + 1
	}
	return forge.CheckLogTail{CheckID: checkID, Text: text, Lines: lines, Truncated: cut || cut2, FetchedAt: now.UTC()}, nil
}

var errNoForge = forge.NewError(forge.CodeCLIMissing, "")

// NoteDetailsFocus tells the poller a connection focused taskID's PR tab.
func (s *Service) NoteDetailsFocus(taskID int64) {
	if s.poller != nil {
		s.poller.NoteDetailsFocus(taskID)
	}
}

// PRDetails is task.prDetails; see Poller.PRDetails.
func (s *Service) PRDetails(taskID int64, focused, refresh bool) (forge.PRDetails, error) {
	if s.poller == nil {
		return forge.PRDetails{}, errNoForge
	}
	return s.poller.PRDetails(taskID, focused, refresh)
}

// PRCheckLog is task.prCheckLog; see Poller.CheckLog.
func (s *Service) PRCheckLog(taskID int64, checkID string) (forge.CheckLogTail, error) {
	if s.poller == nil {
		return forge.CheckLogTail{}, errNoForge
	}
	return s.poller.CheckLog(taskID, checkID)
}
