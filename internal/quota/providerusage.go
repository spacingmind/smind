package quota

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// InstanceFetcher fetches the usage of one provider instance. It has the same
// return contract as ReportFetcher.Fetch.
type InstanceFetcher interface {
	Fetch(ctx context.Context, instanceID string) (Report, error)
}

// ProviderUsageOption configures a ProviderUsage.
type ProviderUsageOption func(*ProviderUsage)

// WithProviderUsageEnabled turns the service on or off. Off, no entry point
// makes a request.
func WithProviderUsageEnabled(enabled bool) ProviderUsageOption {
	return func(u *ProviderUsage) { u.enabled = enabled }
}

// WithProviderUsageClock injects the clock (tests).
func WithProviderUsageClock(now func() time.Time) ProviderUsageOption {
	return func(u *ProviderUsage) { u.clock = now }
}

// WithProviderUsageJitter injects the jitter source, returning [0,1) (tests).
func WithProviderUsageJitter(jit func() float64) ProviderUsageOption {
	return func(u *ProviderUsage) { u.jitter = jit }
}

// ProviderUsage is the per-provider-instance counterpart of Poller: the same
// call-budget rules (ADR-0028 §3), kept in memory only.
//
//   - Peek never touches the network.
//   - Touch fetches at most once per instance per TTL from the last attempt.
//   - Refresh bypasses the TTL under a 30 s floor and 6/hour budget.
//   - concurrent callers for one instance share one in-flight fetch.
//   - failure backoff and the attempt ledger live in memory.
//
// New does no I/O and starts no goroutine; nothing happens until a caller asks.
type ProviderUsage struct {
	fetcher InstanceFetcher

	enabled bool
	ttl     time.Duration
	clock   func() time.Time
	jitter  func() float64
	onFetch func(instanceID string, res Result)

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{} // global in-flight cap
	wg     sync.WaitGroup

	mu     sync.Mutex // guards insts and closed
	insts  map[string]*instUsage
	closed bool
}

// instUsage is one instance's in-memory ledger and cached report.
type instUsage struct {
	mu       sync.Mutex
	st       store.QuotaPollState
	report   *Report // immutable; handed out as clones
	inflight *flight
}

// NewProviderUsage returns a ProviderUsage. Defaults: enabled, 5 minute TTL.
func NewProviderUsage(f InstanceFetcher, opts ...ProviderUsageOption) *ProviderUsage {
	ctx, cancel := context.WithCancel(context.Background())
	u := &ProviderUsage{
		fetcher: f,
		enabled: true,
		ttl:     touchTTLFloor,
		clock:   time.Now,
		jitter:  rand.Float64,
		ctx:     ctx,
		cancel:  cancel,
		sem:     make(chan struct{}, maxInFlight),
		insts:   make(map[string]*instUsage),
	}
	for _, o := range opts {
		o(u)
	}
	return u
}

// Enabled reports whether the service may fetch at all.
func (u *ProviderUsage) Enabled() bool { return u.enabled }

// TTL is the cache TTL: always the 5 minute floor.
func (u *ProviderUsage) TTL() time.Duration { return u.ttl }

// SetOnFetch registers a callback invoked after every completed fetch (success,
// unavailable or error), never on a cache hit and never while a lock is held.
// It must not block. Call it before the service is used.
func (u *ProviderUsage) SetOnFetch(fn func(instanceID string, res Result)) { u.onFetch = fn }

// Close cancels in-flight fetches and waits for their goroutines. Afterwards
// Touch and Refresh never fetch.
func (u *ProviderUsage) Close() {
	u.mu.Lock()
	u.closed = true
	u.mu.Unlock()
	u.cancel()
	u.wg.Wait()
}

// Forget drops an instance's ledger and cached report.
func (u *ProviderUsage) Forget(instanceID string) {
	u.mu.Lock()
	delete(u.insts, instanceID)
	u.mu.Unlock()
}

func (u *ProviderUsage) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

func (u *ProviderUsage) entry(id string) *instUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	e := u.insts[id]
	if e == nil {
		e = &instUsage{}
		u.insts[id] = e
	}
	return e
}

func (u *ProviderUsage) lookup(id string) *instUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.insts[id]
}

// build turns a ledger into a Result. e.mu must be held.
func (u *ProviderUsage) build(e *instUsage, now time.Time, refused, ctxEnded bool) Result {
	clampState(&e.st, now)
	res := Result{Enabled: true, LastError: e.st.LastError, InFlight: e.inflight != nil || ctxEnded}
	res.RefreshBlocked, res.NextRefreshAt = forcedGate(e.st, "", now)
	if e.report != nil {
		clone := cloneReport(*e.report)
		res.Report = &clone
		if res.LastError != "" || now.Sub(e.st.LastAttemptAt) > u.ttl {
			res.Stale = true
		}
	}
	if refused {
		res.Stale = true
	}
	return res
}

func (u *ProviderUsage) buildLocked(e *instUsage, now time.Time, refused, ctxEnded bool) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	return u.build(e, now, refused, ctxEnded)
}

// Peek returns what the cache holds. It never makes a request.
func (u *ProviderUsage) Peek(instanceID string) Result {
	if !u.enabled {
		return Result{}
	}
	e := u.lookup(instanceID)
	if e == nil {
		return Result{Enabled: true}
	}
	return u.buildLocked(e, u.clock(), false, false)
}

// Touch is for a user surface asking for data: at most one fetch per instance
// per TTL, counted from the last attempt.
func (u *ProviderUsage) Touch(ctx context.Context, instanceID string) Result {
	return u.request(ctx, instanceID, false)
}

// Refresh is an explicit refresh press: it bypasses the TTL but stays under
// the 30 s floor and the 6-per-hour budget. A refused Refresh returns the
// cache marked stale; it is never queued.
func (u *ProviderUsage) Refresh(ctx context.Context, instanceID string) Result {
	return u.request(ctx, instanceID, true)
}

func (u *ProviderUsage) request(ctx context.Context, id string, forced bool) Result {
	if !u.enabled {
		return Result{}
	}
	if u.isClosed() {
		return u.Peek(id)
	}
	e := u.entry(id)
	now := u.clock()

	e.mu.Lock()
	if fl := e.inflight; fl != nil { // join: no budget, no state change
		e.mu.Unlock()
		return u.wait(ctx, e, fl)
	}
	clampState(&e.st, now)
	st := &e.st

	inBackoff := now.Before(st.NextAllowedAt)
	refused := false
	switch {
	case inBackoff:
		refused = forced
	case !forced:
		if !st.LastAttemptAt.IsZero() && now.Before(st.LastAttemptAt.Add(u.ttl)) {
			defer e.mu.Unlock() // plain cache hit
			return u.build(e, now, false, false)
		}
	default:
		if blocked, _ := forcedGate(*st, "", now); blocked != "" {
			refused = true
		}
	}
	if inBackoff || refused {
		defer e.mu.Unlock()
		return u.build(e, now, refused, false)
	}

	// Allowed: record the attempt, then fetch in a detached goroutine.
	st.LastAttemptAt = now
	if forced {
		st.ForcedAt = appendForced(st.ForcedAt, now)
	}
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		defer e.mu.Unlock()
		return u.build(e, now, false, false)
	}
	u.wg.Add(1)
	u.mu.Unlock()
	fl := &flight{done: make(chan struct{})}
	e.inflight = fl
	e.mu.Unlock()

	go u.run(id, e, fl)
	return u.wait(ctx, e, fl)
}

// wait blocks until the flight completes or the caller's context ends, then
// answers from the ledger. The caller's context never cancels the fetch.
func (u *ProviderUsage) wait(ctx context.Context, e *instUsage, fl *flight) Result {
	ended := false
	select {
	case <-fl.done:
	case <-ctx.Done():
		ended = true
	}
	return u.buildLocked(e, u.clock(), false, ended)
}

// run is the detached fetch goroutine of one flight.
func (u *ProviderUsage) run(id string, e *instUsage, fl *flight) {
	defer u.wg.Done()

	select {
	case u.sem <- struct{}{}:
	case <-u.ctx.Done():
		u.release(e, fl)
		return
	}
	rep, err := u.safeFetch(id)
	<-u.sem

	if u.ctx.Err() != nil || errors.Is(err, context.Canceled) {
		u.release(e, fl)
		return
	}
	now := u.clock()
	rep.FetchedAt = now.UTC()

	if err == nil && rep.Status == StatusError {
		// ReportFetcher's contract pairs an error report with an error;
		// never let one through as a success.
		err = &FetchError{msg: rep.Error}
	}
	kind := u.complete(e, fl, rep, err, now)
	log.Printf("quota: provider instance fetch %s", kind)

	if u.onFetch != nil {
		u.onFetch(id, u.buildLocked(e, now, false, false))
	}
}

// safeFetch calls the fetcher and turns a panic into a transient failure, so a
// bug in a fetcher cannot leave the flight open or the semaphore held.
func (u *ProviderUsage) safeFetch(id string) (rep Report, err error) {
	defer func() {
		if recover() != nil {
			log.Printf("quota: provider instance fetch panicked")
			rep, err = errorReport("error", u.clock()), &FetchError{msg: "error"}
		}
	}()
	return u.fetcher.Fetch(u.ctx, id)
}

func (u *ProviderUsage) release(e *instUsage, fl *flight) {
	e.mu.Lock()
	e.inflight = nil
	e.mu.Unlock()
	close(fl.done)
}

// complete applies a finished fetch to the ledger and cached report, releases
// the flight and returns the outcome kind for the log.
func (u *ProviderUsage) complete(e *instUsage, fl *flight, rep Report, fetchErr error, now time.Time) string {
	e.mu.Lock()
	st := &e.st
	kind := "ok"
	storeReport := true
	switch {
	case fetchErr != nil:
		kind = "error"
		st.ConsecutiveFailures++
		wait := backoffWait(st.ConsecutiveFailures, u.jitter())
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
		if e.report != nil && e.report.Status == StatusAvailable {
			storeReport = false
		}
	case rep.Status == StatusUnavailable:
		kind = "unavailable"
		st.LastError = ""
		st.ConsecutiveFailures = 0
		st.NextAllowedAt = time.Time{}
	default:
		st.ConsecutiveFailures = 0
		st.NextAllowedAt = time.Time{}
		st.LastError = ""
	}
	if storeReport {
		clone := cloneReport(rep)
		e.report = &clone
	}
	e.inflight = nil
	e.mu.Unlock()
	close(fl.done)
	return kind
}

// Exhausted reports whether the cached report shows an account-wide window at
// or over thresholdPct that has not reset yet, and the latest such reset. It
// reads only the cache and never fetches.
func (u *ProviderUsage) Exhausted(instanceID string, thresholdPct float64, now time.Time) (resetsAt time.Time, ok bool) {
	if !u.enabled {
		return time.Time{}, false
	}
	e := u.lookup(instanceID)
	if e == nil {
		return time.Time{}, false
	}
	e.mu.Lock()
	rep := e.report
	e.mu.Unlock()
	if rep == nil || rep.Status != StatusAvailable {
		return time.Time{}, false
	}
	for _, w := range rep.Windows {
		if w.Scope != nil || w.UsedPct == nil || *w.UsedPct < thresholdPct {
			continue
		}
		if w.ResetsAt == nil || !w.ResetsAt.After(now) {
			continue
		}
		if !ok || w.ResetsAt.After(resetsAt) {
			resetsAt, ok = *w.ResetsAt, true
		}
	}
	return resetsAt, ok
}
