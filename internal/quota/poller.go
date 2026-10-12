// Package quota polls and caches per-account LLM provider usage, backed by
// internal/store's quota_snapshots and quota_poll_state tables.
package quota

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

// AccountSource looks an account up by id. *accounts.Registry satisfies it.
type AccountSource interface {
	Get(id int64) (accounts.Account, error)
}

// Option configures a Poller.
type Option func(*Poller)

// WithEnabled turns the poller on or off. Off, no entry point touches the
// store or the network.
func WithEnabled(enabled bool) Option { return func(p *Poller) { p.enabled = enabled } }

// WithTTL sets the cache TTL. Values under 5 minutes are raised to 5 minutes:
// the floor is a constant, not a setting.
func WithTTL(ttl time.Duration) Option { return func(p *Poller) { p.ttl = ttl } }

// WithClock injects the clock (tests).
func WithClock(now func() time.Time) Option { return func(p *Poller) { p.clock = now } }

// WithJitter injects the jitter source, returning [0,1) (tests).
func WithJitter(jit func() float64) Option { return func(p *Poller) { p.jitter = jit } }

// WithOnFetch registers a callback invoked after every completed network
// fetch (success, unavailable or error), never on a cache hit and never while
// a poller lock is held. It must not block.
func WithOnFetch(fn func(Result)) Option { return func(p *Poller) { p.onFetch = fn } }

// Poller serves account usage from the store and is the only code that may
// cause a usage request. The call-budget rules live here (ADR-0028 §3):
//
//   - Peek never touches the network.
//   - Touch fetches at most once per account per TTL.
//   - Refresh bypasses the TTL under a 30 s floor and 6/hour budget.
//   - concurrent callers for one account share one in-flight fetch.
//   - failure backoff, rejected-credential state and the attempt ledger are
//     persisted (written before the request goes out), so a restart resets
//     nothing.
//
// New does no I/O and starts no goroutine or timer; nothing happens until a
// caller asks about an account.
type Poller struct {
	store   *store.Store
	src     AccountSource
	fetcher ReportFetcher

	enabled bool
	ttl     time.Duration
	clock   func() time.Time
	jitter  func() float64
	onFetch func(Result)

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{} // global in-flight cap
	wg     sync.WaitGroup

	mu     sync.Mutex // guards accts and closed
	accts  map[int64]*acct
	closed bool

	stateLoads atomic.Int64 // quota_poll_state reads (tests)
}

// acct is one account's in-memory ledger. After the first load it is
// authoritative; every mutation is written through to the store.
type acct struct {
	mu       sync.Mutex
	loaded   bool
	st       store.QuotaPollState
	inflight *flight
}

type flight struct{ done chan struct{} }

// New returns a Poller. Defaults: enabled, 5 minute TTL.
func New(s *store.Store, src AccountSource, f ReportFetcher, opts ...Option) *Poller {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Poller{
		store:   s,
		src:     src,
		fetcher: f,
		enabled: true,
		ttl:     touchTTLFloor,
		clock:   time.Now,
		jitter:  rand.Float64,
		ctx:     ctx,
		cancel:  cancel,
		sem:     make(chan struct{}, maxInFlight),
		accts:   make(map[int64]*acct),
	}
	for _, o := range opts {
		o(p)
	}
	if p.ttl < touchTTLFloor {
		p.ttl = touchTTLFloor
	}
	return p
}

// Enabled reports whether the poller may fetch at all.
func (p *Poller) Enabled() bool { return p.enabled }

// TTL is the effective cache TTL (never under 5 minutes).
func (p *Poller) TTL() time.Duration { return p.ttl }

// Close cancels in-flight fetches and waits for their goroutines. Afterwards
// Touch and Refresh never fetch.
func (p *Poller) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.cancel()
	p.wg.Wait()
}

func (p *Poller) entry(id int64) *acct {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.accts[id]
	if a == nil {
		a = &acct{}
		p.accts[id] = a
	}
	return a
}

// load reads the persisted state once. a.mu must be held. It reports false
// when the read failed (the caller must not fetch on unknown state).
func (p *Poller) load(id int64, a *acct) bool {
	if a.loaded {
		return true
	}
	p.stateLoads.Add(1)
	st, _, err := p.store.GetQuotaPollState(id)
	if err != nil {
		log.Printf("quota: account %d state unreadable", id)
		return false
	}
	st.AccountID = id
	a.st, a.loaded = st, true
	return true
}

// view is a snapshot of an account's ledger taken under its lock.
type view struct {
	st       store.QuotaPollState
	inflight bool
	known    bool // state was readable
}

func (p *Poller) viewOf(id int64, a *acct, now time.Time) view {
	a.mu.Lock()
	defer a.mu.Unlock()
	return p.viewLocked(id, a, now)
}

func (p *Poller) viewLocked(id int64, a *acct, now time.Time) view {
	known := p.load(id, a)
	if known {
		clampState(&a.st, now)
	}
	return view{st: cloneState(a.st), inflight: a.inflight != nil, known: known}
}

func cloneState(st store.QuotaPollState) store.QuotaPollState {
	st.ForcedAt = append([]int64(nil), st.ForcedAt...)
	return st
}

// build turns a ledger view into a Result, reading the stored report.
func (p *Poller) build(id int64, v view, fp string, now time.Time, refused, ctxEnded bool) Result {
	res := Result{AccountID: id, Enabled: true, LastError: v.st.LastError, InFlight: v.inflight || ctxEnded}
	if v.known {
		res.RefreshBlocked, res.NextRefreshAt = forcedGate(v.st, fp, now)
	} else {
		res.RefreshBlocked = BlockBackoff
	}
	snap, ok, err := p.store.GetLatestQuotaSnapshot(id)
	if err == nil && ok {
		if rep, decoded := DecodeReport(snap.UsageData); decoded {
			res.Report = &rep
			if res.LastError != "" || now.Sub(snap.PolledAt) > p.ttl {
				res.Stale = true
			}
		}
	}
	if refused {
		res.Stale = true
	}
	return res
}

// Peek returns what the cache holds. It never makes a request and never
// starts a fetch.
func (p *Poller) Peek(accountID int64) Result {
	if !p.enabled {
		return Result{AccountID: accountID}
	}
	acc, err := p.src.Get(accountID)
	if err != nil {
		// Unknown (or just removed) account. Its ledger is not dropped: ids
		// are never reused, the entry is tiny, and dropping it could let a
		// racing request build a second ledger for the same account.
		return Result{AccountID: accountID, Enabled: true}
	}
	now := p.clock()
	fp := fingerprint(acc)
	v := p.viewOf(accountID, p.entry(accountID), now)
	return p.build(accountID, v, fp, now, false, false)
}

// Touch is for a user surface asking for data: at most one fetch per account
// per TTL, counted from the last attempt.
func (p *Poller) Touch(ctx context.Context, accountID int64) Result {
	return p.request(ctx, accountID, false)
}

// Refresh is an explicit refresh press: it bypasses the TTL but stays under
// the 30 s floor and the 6-per-hour budget. A refused Refresh returns the
// cache marked stale; it is never queued.
func (p *Poller) Refresh(ctx context.Context, accountID int64) Result {
	return p.request(ctx, accountID, true)
}

// RefreshAll runs Refresh for every account, results in input order. The
// global in-flight cap bounds how many fetches run at once.
func (p *Poller) RefreshAll(ctx context.Context, accountIDs []int64) []Result {
	out := make([]Result, len(accountIDs))
	var wg sync.WaitGroup
	for i, id := range accountIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = p.Refresh(ctx, id)
		}()
	}
	wg.Wait()
	return out
}

func (p *Poller) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *Poller) request(ctx context.Context, id int64, forced bool) Result {
	if !p.enabled {
		return Result{AccountID: id}
	}
	if p.isClosed() {
		return p.Peek(id)
	}
	acc, err := p.src.Get(id)
	if err != nil {
		return Result{AccountID: id, Enabled: true}
	}
	fp := fingerprint(acc)
	a := p.entry(id)
	now := p.clock()

	a.mu.Lock()
	if !p.load(id, a) {
		v := p.viewLocked(id, a, now)
		a.mu.Unlock()
		return p.build(id, v, fp, now, true, false)
	}
	if fl := a.inflight; fl != nil { // join: no budget, no state change
		a.mu.Unlock()
		return p.wait(ctx, id, a, fl, fp)
	}
	clampState(&a.st, now)
	st := &a.st

	// Blocks that apply to both entry points.
	rejected := st.RejectedFP != "" && st.RejectedFP == fp
	inBackoff := now.Before(st.NextAllowedAt)
	refused := false
	switch {
	case rejected || inBackoff:
		refused = forced
	case !forced:
		if !st.LastAttemptAt.IsZero() && now.Before(st.LastAttemptAt.Add(p.ttl)) {
			return p.finishUnlocked(id, a, fp, now, false) // plain cache hit
		}
	default:
		if blocked, _ := forcedGate(*st, fp, now); blocked != "" {
			refused = true
		}
	}
	if rejected || inBackoff || refused {
		return p.finishUnlocked(id, a, fp, now, refused)
	}

	// Allowed: write-ahead, then fetch in a detached goroutine.
	prev := cloneState(*st)
	st.LastAttemptAt = now
	if forced {
		st.ForcedAt = appendForced(st.ForcedAt, now)
	}
	if st.RejectedFP != "" && st.RejectedFP != fp {
		st.RejectedFP = ""
	}
	if err := p.store.SaveQuotaPollState(cloneState(*st)); err != nil {
		a.st = prev
		log.Printf("quota: account %d state save failed", id)
		v := p.viewLocked(id, a, now)
		a.mu.Unlock()
		res := p.build(id, v, fp, now, true, false)
		res.RefreshBlocked = BlockBackoff
		return res
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		a.mu.Unlock() // attempt stays written ahead; harmless over-count on shutdown
		return p.Peek(id)
	}
	p.wg.Add(1)
	p.mu.Unlock()
	fl := &flight{done: make(chan struct{})}
	a.inflight = fl
	a.mu.Unlock()

	go p.run(id, a, fl, acc)
	return p.wait(ctx, id, a, fl, fp)
}

// finishUnlocked builds a Result from a held ledger and releases the lock
// before reading the snapshot.
func (p *Poller) finishUnlocked(id int64, a *acct, fp string, now time.Time, refused bool) Result {
	v := p.viewLocked(id, a, now)
	a.mu.Unlock()
	return p.build(id, v, fp, now, refused, false)
}

// wait blocks until the flight completes or the caller's context ends, then
// answers from the stored state. The caller's context never cancels the fetch.
func (p *Poller) wait(ctx context.Context, id int64, a *acct, fl *flight, fp string) Result {
	ended := false
	select {
	case <-fl.done:
	case <-ctx.Done():
		ended = true
	}
	now := p.clock()
	// The fetch may have refreshed the token: report against the current
	// credential, not the one the request started with.
	if cur, err := p.src.Get(id); err == nil {
		fp = fingerprint(cur)
	}
	v := p.viewOf(id, a, now)
	return p.build(id, v, fp, now, false, ended)
}

// run is the detached fetch goroutine of one flight.
func (p *Poller) run(id int64, a *acct, fl *flight, acc accounts.Account) {
	defer p.wg.Done()

	select {
	case p.sem <- struct{}{}:
	case <-p.ctx.Done():
		p.release(a, fl)
		return
	}
	rep, err := p.safeFetch(acc)
	<-p.sem

	if p.ctx.Err() != nil || errors.Is(err, context.Canceled) {
		p.release(a, fl)
		return
	}
	now := p.clock()
	rep.FetchedAt = now.UTC()

	if err == nil && rep.Status == StatusError {
		// ReportFetcher's contract pairs an error report with an error;
		// never let one through as a success.
		err = &FetchError{msg: rep.Error}
	}
	kind := p.complete(id, a, fl, acc, rep, err, now)
	log.Printf("quota: account %d fetch %s", id, kind)

	if p.onFetch != nil {
		cur, cerr := p.src.Get(id)
		if cerr != nil {
			p.onFetch(Result{AccountID: id, Enabled: true})
			return
		}
		fp := fingerprint(cur)
		p.onFetch(p.build(id, p.viewOf(id, a, now), fp, now, false, false))
	}
}

// safeFetch calls the fetcher and turns a panic into a transient failure, so
// a bug in a fetcher cannot leave the flight open or the semaphore held.
func (p *Poller) safeFetch(acc accounts.Account) (rep Report, err error) {
	defer func() {
		if recover() != nil {
			log.Printf("quota: account %d fetch panicked", acc.ID)
			rep, err = errorReport("error", p.clock()), &FetchError{msg: "error"}
		}
	}()
	return p.fetcher.Fetch(p.ctx, acc)
}

func (p *Poller) release(a *acct, fl *flight) {
	a.mu.Lock()
	a.inflight = nil
	a.mu.Unlock()
	close(fl.done)
}

// complete applies a finished fetch to the snapshot and ledger, persists the
// ledger, releases the flight and returns the outcome kind for the log.
func (p *Poller) complete(id int64, a *acct, fl *flight, acc accounts.Account, rep Report, fetchErr error, now time.Time) string {
	// The rejected fingerprint is that of the credential the fetch actually
	// sent (it may have refreshed the token itself), never a re-read of the
	// account: a credential replaced while the request was in flight was not
	// the one refused and must stay eligible.
	usedFP := rep.usedFP
	if usedFP == "" {
		usedFP = fingerprint(acc)
	}

	a.mu.Lock()
	st := &a.st
	kind := "ok"
	storeReport := true
	switch {
	case fetchErr != nil:
		kind = "error"
		st.ConsecutiveFailures++
		wait := backoffWait(st.ConsecutiveFailures, p.jitter())
		var fe *FetchError
		if errors.As(fetchErr, &fe) {
			st.LastError = fe.Error()
			if fe.Status == 429 {
				wait = retryAfterWait(fe.RetryAfter, wait)
			}
		} else {
			st.LastError = "error"
		}
		st.NextAllowedAt = now.Add(wait)
		// A failure never overwrites a stored available report.
		if snap, ok, err := p.store.GetLatestQuotaSnapshot(id); err == nil && ok {
			if prev, decoded := DecodeReport(snap.UsageData); decoded && prev.Status == StatusAvailable {
				storeReport = false
			}
		}
	case rep.Status == StatusUnavailable:
		kind = "unavailable"
		st.LastError = ""
		// The endpoint answered: this is not a transient failure.
		st.ConsecutiveFailures = 0
		st.NextAllowedAt = time.Time{}
		if rep.Problem != nil && (rep.Problem.Kind == ProblemRejected || rep.Problem.Kind == ProblemExpired) {
			kind = "rejected"
			st.RejectedFP = usedFP
		}
	default:
		st.ConsecutiveFailures = 0
		st.NextAllowedAt = time.Time{}
		st.LastError = ""
	}
	// Store errors for an account deleted meanwhile (foreign key) are
	// swallowed: there is nothing left to keep consistent.
	if storeReport {
		if data, err := EncodeReport(rep); err == nil {
			_, _ = p.store.ReplaceQuotaSnapshot(store.QuotaSnapshot{
				AccountID: id, UsageData: data, PolledAt: now, ExpiresAt: now.Add(p.ttl),
			})
		}
	}
	// One retry: losing this row would lose a Retry-After across a restart.
	// A failure for a removed account (foreign key) is expected and silent.
	if err := p.store.SaveQuotaPollState(cloneState(*st)); err != nil {
		if err := p.store.SaveQuotaPollState(cloneState(*st)); err != nil {
			if _, gerr := p.src.Get(id); gerr == nil {
				log.Printf("quota: account %d state save failed", id)
			} else {
				log.Printf("quota: account %d removed during fetch", id)
			}
		}
	}
	a.inflight = nil
	a.mu.Unlock()
	close(fl.done)
	return kind
}
