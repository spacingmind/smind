package quota

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

const testGrace = 5 * time.Second

func fixedGrace() time.Duration { return testGrace }

// hdr builds a canonical header from name/value pairs.
func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

const uf = "anthropic-ratelimit-unified-"

// reject5h is a 429 header set declaring a 5h rejection that resets at reset.
func reject5h(reset time.Time) http.Header {
	return hdr(uf+"status", "rejected", uf+"5h-status", "rejected", uf+"5h-reset", epoch(reset))
}

// rejectOIOnly is the 7d_oi-only rejection shape with a Retry-After deadline
// (the only deadline that applies to it).
func rejectOIOnly(retryAfterSec int) http.Header {
	return hdr(uf+"status", "rejected", uf+"5h-status", "allowed", uf+"7d-status", "allowed",
		uf+"7d_oi-status", "rejected", "Retry-After", strconv.Itoa(retryAfterSec))
}

// rejectNoDeadline is a declared 5h rejection with no usable reset.
func rejectNoDeadline() http.Header {
	return hdr(uf+"status", "rejected", uf+"5h-status", "rejected", uf+"5h-reset", "garbage")
}

// graceEnd is the end a deadline gets with the test grace; resets travel as
// epoch seconds, so the deadline is truncated to the second.
func graceEnd(reset time.Time) time.Time {
	return time.Unix(reset.Unix(), 0).Add(testGrace)
}

// passiveEnv is a Passive on the env's real store, accounts and fake clock.
// The env's Poller is built with the late-bound cooldown source, so
// e.p.Cooldown answers from pe.p.
type passiveEnv struct {
	*env
	f   *fakeReportFetcher
	p   *Passive
	id  int64
	src *lateSource

	mu      sync.Mutex
	changes []int64
}

// lateSource lets a Poller be built before the Passive that it reads from.
type lateSource struct{ p atomic.Pointer[Passive] }

func (l *lateSource) Cooldown(id int64, model string, now time.Time) (Cooldown, bool) {
	if p := l.p.Load(); p != nil {
		return p.Cooldown(id, model, now)
	}
	return Cooldown{}, false
}

func newPassiveEnv(t *testing.T, opts ...PassiveOption) *passiveEnv {
	t.Helper()
	pe := &passiveEnv{f: &fakeReportFetcher{}, src: &lateSource{}}
	pe.env = newEnv(t, func(*accounts.Registry, *fakeClock) ReportFetcher { return pe.f }, WithCooldowns(pe.src))
	pe.id = pe.addOAuth("tok")
	pe.p = pe.newPassive(opts...)
	t.Cleanup(func() { pe.p.Close() }) // runs before newEnv's store close
	return pe
}

// newPassive builds a Passive over the env's current store (after restart too).
func (pe *passiveEnv) newPassive(opts ...PassiveOption) *Passive {
	all := append([]PassiveOption{
		WithPassiveClock(pe.clk.Now),
		WithPassiveGrace(fixedGrace),
		WithPassiveOnChange(func(id int64) {
			pe.mu.Lock()
			pe.changes = append(pe.changes, id)
			pe.mu.Unlock()
		}),
	}, opts...)
	p := NewPassive(pe.st, all...)
	pe.src.p.Store(p)
	return p
}

func (pe *passiveEnv) changeCount() int {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	return len(pe.changes)
}

// observe sends one response for the env's account at the fake clock's time.
func (pe *passiveEnv) observe(model string, status int, h http.Header) {
	pe.p.Observe(Response{AccountID: pe.id, Provider: "anthropic", Model: model, Status: status, Header: h, At: pe.clk.Now()})
}

func (pe *passiveEnv) cooldown(model string) (Cooldown, bool) {
	return pe.p.Cooldown(pe.id, model, pe.clk.Now())
}

// flush waits until the writer has nothing queued and no batch in flight.
func flush(t *testing.T, p *Passive) {
	t.Helper()
	a3aEventually(t, "passive writer idle", func() bool {
		p.wmu.Lock()
		defer p.wmu.Unlock()
		return len(p.dirtyCD) == 0 && len(p.dirtyObs) == 0 && !p.busy
	})
}

func wantCooldown(t *testing.T, got Cooldown, ok bool, wantUntil time.Time, scope CooldownScope, model string, reason CooldownReason) {
	t.Helper()
	if !ok {
		t.Fatalf("Cooldown() = none, want %s/%q until %v", scope, model, wantUntil)
	}
	if !got.Until.Equal(wantUntil) || got.Scope != scope || got.Model != model || got.Reason != reason {
		t.Fatalf("Cooldown() = %+v, want until %v scope %s model %q reason %s", got, wantUntil, scope, model, reason)
	}
}

func wantNoCooldown(t *testing.T, p *Passive, id int64, model string, now time.Time) {
	t.Helper()
	if c, ok := p.Cooldown(id, model, now); ok {
		t.Fatalf("Cooldown(%q) = %+v, want none", model, c)
	}
}

// fakeStore is an in-memory PassiveStore a test can slow down, fail or count.
type fakeStore struct {
	mu    sync.Mutex
	rows  map[cooldownKey]store.QuotaCooldown
	snaps map[int64]store.QuotaSnapshot

	calls      map[string]int
	saveGate   chan struct{} // when non-nil, Save and Replace park until it is closed
	saveEnter  chan struct{} // receives one value per parked write
	failWrites bool
	failReads  bool
	errText    string
}

var errFake = errors.New("fake store failure")

func newFakeStore() *fakeStore {
	return &fakeStore{
		rows:  map[cooldownKey]store.QuotaCooldown{},
		snaps: map[int64]store.QuotaSnapshot{},
		calls: map[string]int{},
	}
}

func (f *fakeStore) err() error {
	if f.errText != "" {
		return errors.New(f.errText)
	}
	return errFake
}

func (f *fakeStore) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeStore) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fakeStore) setFail(writes, reads bool) {
	f.mu.Lock()
	f.failWrites, f.failReads = writes, reads
	f.mu.Unlock()
}

func (f *fakeStore) park() {
	f.mu.Lock()
	gate, enter := f.saveGate, f.saveEnter
	f.mu.Unlock()
	if gate == nil {
		return
	}
	if enter != nil {
		select {
		case enter <- struct{}{}:
		default:
		}
	}
	<-gate
}

func (f *fakeStore) row(id int64, scope string) (store.QuotaCooldown, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[cooldownKey{id, scope}]
	return r, ok
}

func (f *fakeStore) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func (f *fakeStore) SaveQuotaCooldown(c store.QuotaCooldown) error {
	f.mu.Lock()
	f.calls["SaveQuotaCooldown"]++
	f.mu.Unlock()
	f.park()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrites {
		return f.err()
	}
	f.rows[cooldownKey{c.AccountID, c.ScopeModel}] = c
	return nil
}

func (f *fakeStore) ListQuotaCooldowns(id int64) ([]store.QuotaCooldown, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ListQuotaCooldowns"]++
	if f.failReads {
		return nil, f.err()
	}
	var out []store.QuotaCooldown
	for k, r := range f.rows {
		if k.account == id {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) DeleteQuotaCooldowns(id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["DeleteQuotaCooldowns"]++
	for k := range f.rows {
		if k.account == id {
			delete(f.rows, k)
		}
	}
	return nil
}

func (f *fakeStore) DeleteQuotaCooldown(id int64, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["DeleteQuotaCooldown"]++
	delete(f.rows, cooldownKey{id, scope})
	return nil
}

func (f *fakeStore) ReplaceQuotaSnapshot(q store.QuotaSnapshot) (store.QuotaSnapshot, error) {
	f.mu.Lock()
	f.calls["ReplaceQuotaSnapshot"]++
	f.mu.Unlock()
	f.park()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrites {
		return store.QuotaSnapshot{}, f.err()
	}
	f.snaps[q.AccountID] = q
	return q, nil
}

func (f *fakeStore) GetLatestQuotaSnapshotKind(id int64, kind string) (store.QuotaSnapshot, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetLatestQuotaSnapshotKind"]++
	if f.failReads {
		return store.QuotaSnapshot{}, false, f.err()
	}
	q, ok := f.snaps[id]
	return q, ok, nil
}

// newFakePassive is a Passive over a fakeStore on a fake clock, with its own
// cleanup.
func newFakePassive(t *testing.T, fs *fakeStore, opts ...PassiveOption) (*Passive, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	all := append([]PassiveOption{WithPassiveClock(clk.Now), WithPassiveGrace(fixedGrace)}, opts...)
	p := NewPassive(fs, all...)
	t.Cleanup(p.Close)
	return p, clk
}

func respAt(id int64, model string, status int, h http.Header, at time.Time) Response {
	return Response{AccountID: id, Provider: "anthropic", Model: model, Status: status, Header: h, At: at}
}

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLog redirects the standard logger. The test must not be parallel:
// the logger is process-global.
func captureLog() (*lockedBuffer, func()) {
	buf := &lockedBuffer{}
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	return buf, func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }
}
