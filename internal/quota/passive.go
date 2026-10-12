package quota

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Passive is the real implementation behind the Observer and CooldownSource
// contracts (ADR-0028 §7, D14). It turns the upstream responses the proxy
// already receives into
//
//   - a routing signal: a declared hard rejection starts a persisted cooldown,
//     read through Cooldown (Poller.Cooldown answers from it);
//   - a display-only observation of the rate-limit headers, read through
//     Observed and never by Cooldown or any routing code.
//
// It makes zero network calls: it holds a narrow store view and nothing that
// can reach a usage endpoint. A fetched report never touches it; a cooldown
// ends only with time, or with ClearCooldown (credential changed).

var (
	_ Observer       = (*Passive)(nil)
	_ CooldownSource = (*Passive)(nil)
)

const (
	passiveProvider = "anthropic"

	// passiveMaxScoped bounds the model-keyed entries (cooldown rows and
	// bare-429 back-offs) kept per account: the model comes from the client's
	// request body, so it must not grow memory without limit.
	passiveMaxScoped = 64
	// passiveMaxModelLen: a longer model is treated as unknown.
	passiveMaxModelLen = 128
	// passiveMaxLevel is the fallback level whose duration already hits the
	// 30 min cap (FallbackDuration(6) = 30 min).
	passiveMaxLevel = 6
	// passiveObsMinInterval is the minimum time between two observation
	// writes for one account, unless the verdict changes.
	passiveObsMinInterval = 60 * time.Second
)

// PassiveStore is the narrow store view Passive needs, so tests can inject a
// slow or failing store. *store.Store satisfies it.
type PassiveStore interface {
	SaveQuotaCooldown(store.QuotaCooldown) error
	ListQuotaCooldowns(accountID int64) ([]store.QuotaCooldown, error)
	DeleteQuotaCooldowns(accountID int64) error
	DeleteQuotaCooldown(accountID int64, scopeModel string) error
	ReplaceQuotaSnapshot(store.QuotaSnapshot) (store.QuotaSnapshot, error)
	GetLatestQuotaSnapshotKind(accountID int64, kind string) (store.QuotaSnapshot, bool, error)
}

var _ PassiveStore = (*store.Store)(nil)

// PassiveOption configures a Passive.
type PassiveOption func(*Passive)

// WithPassiveEnabled turns the observer on or off (default on). Off, every
// method is a no-op that never touches the store.
func WithPassiveEnabled(enabled bool) PassiveOption { return func(p *Passive) { p.enabled = enabled } }

// WithPassiveClock injects the clock used when a Response has no At (tests).
func WithPassiveClock(now func() time.Time) PassiveOption { return func(p *Passive) { p.clock = now } }

// WithPassiveGrace injects the grace source added to a cooldown end (tests).
func WithPassiveGrace(g GraceFunc) PassiveOption { return func(p *Passive) { p.grace = g } }

// WithPassiveOnChange registers a callback invoked, outside every lock, when a
// cooldown starts, is extended or is cleared (ClearCooldown), or when the
// parsed observation changed. It must not block. It is NOT invoked when a
// cooldown ends: that is time-based, so a caller that shows cooldown state
// must schedule its own refresh for Cooldown.Until.
func WithPassiveOnChange(fn func(accountID int64)) PassiveOption {
	return func(p *Passive) { p.onChange = fn }
}

// cooldownRow is one cooldown entry: credential-wide (scope "") or one model.
type cooldownRow struct {
	until      time.Time
	reason     CooldownReason
	level      int
	observedAt time.Time
}

// cooldownKey addresses a persisted cooldown row.
type cooldownKey struct {
	account int64
	scope   string // "" credential-wide, else the lower-cased model
}

// passiveAcct is the per-account state. mu guards every field; it is held only
// for memory work, never across a store call.
type passiveAcct struct {
	mu sync.Mutex

	// gen is bumped by ClearCooldown; a write that started under an older
	// generation is skipped or undone.
	gen uint64
	// loaded: the persisted rows were merged into rows (or the account was
	// cleared). A failed read leaves it false so the next Cooldown retries.
	loaded bool
	// resetPending: a non-rejected response arrived before the persisted rows
	// were loaded, so their fallback level must be reset when they are.
	resetPending bool
	resetAt      time.Time

	rows    map[string]cooldownRow // scope -> row
	backoff map[string]time.Time   // lower-cased model -> end of a bare-429 back-off (memory only)
	failed  map[string]struct{}    // scopes whose last write failed; retried with the next write

	// Observation (display only).
	obs                 *Observation
	obsLoaded           bool
	obsPending          *Observation // handed to the writer, not yet written
	obsPersisted        *Observation // last observation queued for writing; nil = unknown
	obsPersistedVerdict string
	obsLastWrite        time.Time
}

func newPassiveAcct() *passiveAcct {
	return &passiveAcct{
		rows:    map[string]cooldownRow{},
		backoff: map[string]time.Time{},
		failed:  map[string]struct{}{},
	}
}

// Passive implements Observer and CooldownSource. See the file comment.
type Passive struct {
	store    PassiveStore
	enabled  bool
	clock    func() time.Time
	grace    GraceFunc
	onChange func(accountID int64)

	mu    sync.RWMutex // guards accts only
	accts map[int64]*passiveAcct

	// Async writer. wmu guards the dirty sets and the lifecycle flags; it is
	// taken while holding an account lock (never the other way round).
	wmu      sync.Mutex
	dirtyCD  map[cooldownKey]struct{}
	dirtyObs map[int64]struct{}
	started  bool
	closed   bool
	busy     bool          // the writer holds a batch it has not finished (tests wait on it)
	wake     chan struct{} // cap 1
	stop     chan struct{}
	done     chan struct{}

	// unnamedModelOnly counts 7d_oi-only rejections not recorded because the
	// request's model is unknown (tests).
	unnamedModelOnly atomic.Int64
}

// NewPassive builds a Passive over s. It starts no goroutine until the first
// write is queued.
func NewPassive(s PassiveStore, opts ...PassiveOption) *Passive {
	p := &Passive{
		store:    s,
		enabled:  true,
		clock:    time.Now,
		grace:    DefaultGrace,
		accts:    map[int64]*passiveAcct{},
		dirtyCD:  map[cooldownKey]struct{}{},
		dirtyObs: map[int64]struct{}{},
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *Passive) acct(id int64) *passiveAcct {
	p.mu.RLock()
	a := p.accts[id]
	p.mu.RUnlock()
	if a != nil {
		return a
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if a = p.accts[id]; a == nil {
		a = newPassiveAcct()
		p.accts[id] = a
	}
	return a
}

// normalizeModel is the cooldown key of a model: lower-cased and trimmed, ""
// when unknown or absurdly long.
func normalizeModel(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if len(m) > passiveMaxModelLen {
		return ""
	}
	return m
}

// Observe implements Observer. It parses the header synchronously, updates
// memory so the very next Cooldown sees a new cooldown, queues persistence and
// returns; it never waits for the store. The one exception is a declared
// rejection on an account whose persisted rows were not read yet: it reads them
// once, to continue the fallback level and to never shorten a persisted
// cooldown. See reject.
func (p *Passive) Observe(r Response) {
	if !p.enabled || r.Provider != passiveProvider {
		return
	}
	at := r.At
	if at.IsZero() {
		at = p.clock()
	}
	rej := ClassifyRejection(r.Status, r.Header)
	model := normalizeModel(r.Model)
	obs, hasObs := ParseAnthropicObservation(r.Header, at)

	a := p.acct(r.AccountID)
	changed := false
	if rej.Rejected {
		changed = p.reject(r, a, rej, model, at)
	} else {
		var backoff time.Duration
		var ok bool
		if r.Status == 429 {
			backoff, ok = RetryAfterBackoff(r.Header, at)
		}
		p.notRejected(r.AccountID, a, model, at, backoff, ok)
	}
	if hasObs && p.observe(r.AccountID, a, obs, at) {
		changed = true
	}
	if changed && p.onChange != nil {
		p.onChange(r.AccountID)
	}
}

// reject starts or extends a cooldown for a declared rejection. It reports
// whether a cooldown changed.
func (p *Passive) reject(r Response, a *passiveAcct, rej Rejection, model string, at time.Time) bool {
	scope := ""
	if rej.ModelOnly {
		if model == "" {
			// Fail open: never park a whole account for a model we cannot name.
			p.unnamedModelOnly.Add(1)
			return false
		}
		scope = model
	}
	deadline, hasDeadline := RejectionDeadline(r.Header, at, rej)
	var grace time.Duration
	if hasDeadline {
		grace = p.grace()
	}
	// Merge the persisted rows first, on both branches, so a rejection right
	// after a restart neither restarts the fallback level nor shortens a
	// persisted cooldown. This is the one place Observe can wait for the store,
	// and only on the first rejection of an account whose rows were never loaded
	// (the router's Cooldown() normally loads them before any request is sent,
	// so this is a no-op there; it bites when accountUsage.routing is off or an
	// earlier load failed). It is bounded by the store's busy timeout.
	p.ensureLoaded(r.AccountID, a)

	a.mu.Lock()
	defer a.mu.Unlock()
	cur, have := a.rows[scope]
	active := have && cur.until.After(at)

	var next cooldownRow
	if hasDeadline {
		next = cooldownRow{until: CooldownEnd(deadline, at, grace), reason: ReasonRejected}
	} else {
		if active {
			// A second rejection while a cooldown runs must not escalate.
			return false
		}
		level := 1
		if have {
			level = min(cur.level+1, passiveMaxLevel)
		}
		next = cooldownRow{until: at.Add(FallbackDuration(level)), reason: ReasonFallback, level: level}
	}
	if active && !next.until.After(cur.until) {
		return false // never shorten
	}
	if !have && len(a.rows) >= passiveMaxScoped && !a.pruneRowsLocked(at) {
		return false
	}
	next.observedAt = at
	a.rows[scope] = next
	p.markCooldownLocked(r.AccountID, a, scope)
	return true
}

// notRejected handles every response that is not a declared rejection: a bare
// 429 with Retry-After records a transient in-memory back-off, and any such
// response resets the fallback level without touching an active cooldown.
func (p *Passive) notRejected(id int64, a *passiveAcct, model string, at time.Time, backoff time.Duration, hasBackoff bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if hasBackoff && model != "" {
		until := at.Add(backoff)
		cur, have := a.backoff[model]
		if !have || until.After(cur) {
			if !have {
				a.pruneBackoffLocked(at)
			}
			if have || len(a.backoff) < passiveMaxScoped {
				a.backoff[model] = until
			}
		}
	}
	if !a.loaded {
		a.resetPending, a.resetAt = true, at
	}
	for scope, row := range a.rows {
		if row.level == 0 {
			continue
		}
		row.level = 0
		if row.until.After(at) {
			a.rows[scope] = row
		} else {
			delete(a.rows, scope)
		}
		p.markCooldownLocked(id, a, scope)
	}
}

// pruneRowsLocked drops expired rows that carry no level and reports whether
// there is room for one more. a.mu must be held.
func (a *passiveAcct) pruneRowsLocked(now time.Time) bool {
	for scope, row := range a.rows {
		if row.level == 0 && !row.until.After(now) {
			delete(a.rows, scope)
		}
	}
	return len(a.rows) < passiveMaxScoped
}

func (a *passiveAcct) pruneBackoffLocked(now time.Time) {
	for m, until := range a.backoff {
		if !until.After(now) {
			delete(a.backoff, m)
		}
	}
}

// Cooldown implements CooldownSource. It answers the applicable entry with the
// latest end among the credential-wide row, the row of model and the bare-429
// back-off of model; entries that ended by now are ignored and an end beyond
// now+MaxCooldown (a clock jump) is clamped. It never makes a request; its only
// store access is reading the account's persisted rows once.
func (p *Passive) Cooldown(accountID int64, model string, now time.Time) (Cooldown, bool) {
	if !p.enabled {
		return Cooldown{}, false
	}
	a := p.acct(accountID)
	p.ensureLoaded(accountID, a)
	m := normalizeModel(model)

	a.mu.Lock()
	defer a.mu.Unlock()
	limit := now.Add(MaxCooldown)
	var best Cooldown
	found := false
	consider := func(c Cooldown) {
		if !c.Until.After(now) {
			return
		}
		if c.Until.After(limit) {
			c.Until = limit
		}
		if !found || c.Until.After(best.Until) {
			best, found = c, true
		}
	}
	if row, ok := a.rows[""]; ok {
		consider(Cooldown{Until: row.until, Scope: CooldownAccount, Reason: row.reason, Level: row.level})
	}
	if m != "" {
		if row, ok := a.rows[m]; ok {
			consider(Cooldown{Until: row.until, Scope: CooldownModel, Model: m, Reason: row.reason, Level: row.level})
		}
		if until, ok := a.backoff[m]; ok {
			consider(Cooldown{Until: until, Scope: CooldownModel, Model: m, Reason: ReasonBackoff})
		}
	}
	return best, found
}

// ensureLoaded merges the account's persisted rows into memory, once. A read
// error is not cached (the next call retries) and the answer comes from memory
// only. The store read happens without any lock held.
func (p *Passive) ensureLoaded(id int64, a *passiveAcct) {
	a.mu.Lock()
	loaded := a.loaded
	a.mu.Unlock()
	if loaded {
		return
	}
	rows, err := p.store.ListQuotaCooldowns(id)
	if err != nil {
		log.Printf("quota: account %d passive cooldown read failed", id)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded { // loaded concurrently, or cleared meanwhile
		return
	}
	p.mergeStoredLocked(id, a, rows)
	a.loaded = true
}

// mergeStoredLocked folds persisted rows into memory. Memory is newer than the
// store for anything recorded since startup, but an end is never shortened, so
// the later end of the two wins. a.mu must be held.
func (p *Passive) mergeStoredLocked(id int64, a *passiveAcct, rows []store.QuotaCooldown) {
	for _, r := range rows {
		row := cooldownRow{
			until:      r.Until,
			reason:     sanitizeReason(r.Reason),
			level:      max(0, min(r.Level, passiveMaxLevel)),
			observedAt: r.ObservedAt,
		}
		scope := r.ScopeModel
		reset := a.resetPending && row.level > 0
		if reset {
			row.level = 0
		}
		expired := reset && !row.until.After(a.resetAt)
		if cur, have := a.rows[scope]; have {
			if row.until.After(cur.until) && !expired {
				a.rows[scope] = row
				p.markCooldownLocked(id, a, scope) // the store may hold the shorter in-memory row by now
			}
			continue
		}
		if expired {
			p.markCooldownLocked(id, a, scope) // no row in memory: the writer deletes it
			continue
		}
		if len(a.rows) >= passiveMaxScoped {
			continue
		}
		a.rows[scope] = row
		if reset {
			p.markCooldownLocked(id, a, scope)
		}
	}
	a.resetPending = false
}

func sanitizeReason(r string) CooldownReason {
	if CooldownReason(r) == ReasonFallback {
		return ReasonFallback
	}
	return ReasonRejected
}

// ClearCooldown drops the account's cooldowns and back-offs from memory, the
// persisted rows, and any queued write, because its credential changed. A write
// already running is undone by the writer (generation check), so nothing can
// resurrect a cleared cooldown. The store delete is synchronous; this is not a
// proxy-path call. It is a no-op while the observer is disabled.
func (p *Passive) ClearCooldown(accountID int64) {
	if !p.enabled {
		return
	}
	a := p.acct(accountID)
	now := p.clock()
	a.mu.Lock()
	had := false
	for _, row := range a.rows {
		if row.until.After(now) {
			had = true
		}
	}
	a.gen++
	a.rows = map[string]cooldownRow{}
	a.backoff = map[string]time.Time{}
	a.failed = map[string]struct{}{}
	a.loaded, a.resetPending = true, false
	p.dropPending(accountID)
	a.mu.Unlock()

	if err := p.store.DeleteQuotaCooldowns(accountID); err != nil {
		log.Printf("quota: account %d passive cooldown delete failed", accountID)
	}
	if had && p.onChange != nil {
		p.onChange(accountID)
	}
}

// --- async persistence -----------------------------------------------------
//
// One writer goroutine, started lazily, drains a coalescing dirty set: the key
// of a cooldown row (or of an account's observation) is queued, and the writer
// reads the CURRENT in-memory value when it gets to it, so a burst becomes one
// write and a cooldown write is never dropped. Observe only touches the dirty
// set (a mutex and a cap-1 channel), so a slow store never delays the proxy.
//
// After Close, new writes are skipped (memory is still updated): Close runs at
// shutdown, and blocking a late response on the store is worse than losing the
// write of a response that arrives after the drain.

// markCooldownLocked queues the row at scope (and any row whose last write
// failed) for writing. A scope with no row in memory is a delete. a.mu must be
// held.
func (p *Passive) markCooldownLocked(id int64, a *passiveAcct, scope string) {
	p.wmu.Lock()
	if p.closed {
		p.wmu.Unlock()
		return
	}
	for s := range a.failed {
		p.dirtyCD[cooldownKey{id, s}] = struct{}{}
	}
	clear(a.failed)
	p.dirtyCD[cooldownKey{id, scope}] = struct{}{}
	p.startLocked()
	p.wmu.Unlock()
	p.signal()
}

// markObservationLocked queues the account's pending observation. a.mu must be
// held.
func (p *Passive) markObservationLocked(id int64) {
	p.wmu.Lock()
	if p.closed {
		p.wmu.Unlock()
		return
	}
	p.dirtyObs[id] = struct{}{}
	p.startLocked()
	p.wmu.Unlock()
	p.signal()
}

// dropPending forgets the queued cooldown writes of an account. a.mu is held by
// the caller.
func (p *Passive) dropPending(id int64) {
	p.wmu.Lock()
	for k := range p.dirtyCD {
		if k.account == id {
			delete(p.dirtyCD, k)
		}
	}
	p.wmu.Unlock()
}

func (p *Passive) startLocked() {
	if !p.started {
		p.started = true
		go p.run()
	}
}

func (p *Passive) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// run is the writer goroutine. After Close it still drains what was queued
// before Close, then exits.
func (p *Passive) run() {
	defer close(p.done)
	for {
		p.wmu.Lock()
		cds, obs := p.dirtyCD, p.dirtyObs
		if len(cds) == 0 && len(obs) == 0 {
			closed := p.closed
			p.wmu.Unlock()
			if closed {
				return
			}
			select {
			case <-p.wake:
			case <-p.stop:
			}
			continue
		}
		p.dirtyCD, p.dirtyObs = map[cooldownKey]struct{}{}, map[int64]struct{}{}
		p.busy = true
		p.wmu.Unlock()

		for k := range cds {
			p.writeCooldown(k)
		}
		for id := range obs {
			p.writeObservation(id)
		}
		p.wmu.Lock()
		p.busy = false
		p.wmu.Unlock()
	}
}

// writeCooldown persists the current in-memory state of one row (a missing row
// is deleted). If ClearCooldown ran while the write was in flight, the write is
// undone so it cannot resurrect the cleared cooldown.
func (p *Passive) writeCooldown(k cooldownKey) {
	a := p.acct(k.account)
	a.mu.Lock()
	gen := a.gen
	row, have := a.rows[k.scope]
	a.mu.Unlock()

	var err error
	if have {
		err = p.store.SaveQuotaCooldown(store.QuotaCooldown{
			AccountID:  k.account,
			ScopeModel: k.scope,
			Until:      row.until,
			Reason:     string(row.reason),
			Level:      row.level,
			ObservedAt: row.observedAt,
		})
	} else {
		err = p.store.DeleteQuotaCooldown(k.account, k.scope)
	}

	a.mu.Lock()
	stale := a.gen != gen
	if err != nil && !stale {
		a.failed[k.scope] = struct{}{}
	}
	a.mu.Unlock()

	if err != nil {
		log.Printf("quota: account %d passive cooldown write failed", k.account)
		return
	}
	if stale && have {
		if err := p.store.DeleteQuotaCooldown(k.account, k.scope); err != nil {
			log.Printf("quota: account %d passive cooldown undo failed", k.account)
		}
	}
}

// writeObservation persists the account's pending observation as its observed
// snapshot row. On failure the "persisted" baseline is forgotten so the next
// response with a signal retries.
func (p *Passive) writeObservation(id int64) {
	a := p.acct(id)
	a.mu.Lock()
	o := a.obsPending
	a.obsPending = nil
	a.mu.Unlock()
	if o == nil {
		return
	}
	data, err := EncodeReport(o.Report())
	if err == nil {
		_, err = p.store.ReplaceQuotaSnapshot(store.QuotaSnapshot{
			AccountID: id,
			UsageData: data,
			PolledAt:  o.At,
			ExpiresAt: o.At,
			Kind:      store.QuotaKindObserved,
		})
	}
	if err != nil {
		a.mu.Lock()
		a.obsPersisted = nil
		a.mu.Unlock()
		log.Printf("quota: account %d passive observation write failed", id)
	}
}

// Close flushes everything queued so far, then stops the writer. It blocks
// until the store accepted (or refused) those writes, and is idempotent.
func (p *Passive) Close() {
	p.wmu.Lock()
	if !p.closed {
		p.closed = true
		close(p.stop)
	}
	started := p.started
	p.wmu.Unlock()
	if started {
		<-p.done
	}
}

// --- observation ------------------------------------------------------------

// observe replaces the account's in-memory observation at once (replaced, not
// merged) and decides whether to persist it: only when the parsed values differ
// from what was last queued for writing, and then at most once per
// passiveObsMinInterval unless the verdict changed. It reports whether the
// parsed values changed. Display only: nothing here reads or writes a cooldown.
func (p *Passive) observe(id int64, a *passiveAcct, obs Observation, at time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := cloneObservation(obs)
	changed := a.obs == nil || !a.obs.Equal(o)
	a.obs, a.obsLoaded = &o, true

	prev := a.obsPersisted
	if prev == nil ||
		(!o.Equal(*prev) && (at.Sub(a.obsLastWrite) >= passiveObsMinInterval || o.Verdict() != a.obsPersistedVerdict)) {
		a.obsPersisted, a.obsPersistedVerdict, a.obsLastWrite = &o, o.Verdict(), at
		a.obsPending = &o
		p.markObservationLocked(id)
	}
	return changed
}

// Observed returns the account's latest observation for display: the one in
// memory, else the stored observed snapshot (read once; an error is not
// cached). Routing never calls it, and Reader has no such method.
func (p *Passive) Observed(accountID int64) (Observation, bool) {
	if !p.enabled {
		return Observation{}, false
	}
	a := p.acct(accountID)
	a.mu.Lock()
	if a.obs != nil || a.obsLoaded {
		defer a.mu.Unlock()
		return a.observedLocked()
	}
	a.mu.Unlock()

	snap, ok, err := p.store.GetLatestQuotaSnapshotKind(accountID, store.QuotaKindObserved)
	if err != nil {
		log.Printf("quota: account %d passive observation read failed", accountID)
		return Observation{}, false
	}
	var stored *Observation
	if ok {
		if rep, decoded := DecodeReport(snap.UsageData); decoded {
			if o, has := ObservationFromReport(rep); has {
				stored = &o
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.obs == nil && !a.obsLoaded {
		a.obs, a.obsLoaded = stored, true
	}
	return a.observedLocked()
}

func (a *passiveAcct) observedLocked() (Observation, bool) {
	if a.obs == nil {
		return Observation{}, false
	}
	return cloneObservation(*a.obs), true
}

// cloneObservation deep-copies the windows so a stored or returned observation
// is never shared.
func cloneObservation(o Observation) Observation {
	o.Windows = cloneReport(Report{Windows: o.Windows}).Windows
	return o
}
