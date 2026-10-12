package routing

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

// countingFetcher counts every Fetch. Accounts in failIDs fail at once;
// accounts in blockIDs block until the poller shuts down; others fail too (a
// fetch from Route would be a bug, whatever it returned).
type countingFetcher struct {
	calls    atomic.Int64
	failIDs  map[int64]bool
	blockIDs map[int64]bool
	entered  chan int64
}

func (f *countingFetcher) Fetch(ctx context.Context, a accounts.Account) (quota.Report, error) {
	f.calls.Add(1)
	if f.blockIDs[a.ID] {
		select {
		case f.entered <- a.ID:
		default:
		}
		<-ctx.Done()
		return quota.Report{}, ctx.Err()
	}
	return quota.Report{}, errors.New("boom")
}

// TestRoute_NeverFetches is the call-count rule of ADR-0028: a real, enabled
// Poller with a counting fetcher sees zero fetches from 1000 Route calls
// over a cold cache, a stale cache, an account in backoff and an account
// whose fetch is blocked in flight -- and Route never waits for any of them.
func TestRoute_NeverFetches(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reg := accounts.New(s)
	cold := newAccount(t, reg, "cold")
	stale := newAccount(t, reg, "stale")
	backoff := newAccount(t, reg, "backoff")
	inflight := newAccount(t, reg, "inflight")

	clk := &fakeClock{t: testNow}
	f := &countingFetcher{
		failIDs:  map[int64]bool{backoff: true},
		blockIDs: map[int64]bool{inflight: true},
		entered:  make(chan int64, 1),
	}
	p := quota.New(s, reg, f, quota.WithEnabled(true), quota.WithTTL(testTTL), quota.WithClock(clk.Now), quota.WithJitter(func() float64 { return 0 }))
	t.Cleanup(p.Close)
	r := New(s, reg, p, WithClock(clk.Now))

	// stale: an exhausted snapshot polled long before the TTL window.
	rep := availableResult(window("five_hour", 100, tptr(testNow.Add(3*time.Hour)))).Report
	data, err := quota.EncodeReport(*rep)
	if err != nil {
		t.Fatalf("EncodeReport() error = %v", err)
	}
	polled := testNow.Add(-2 * time.Hour)
	if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{AccountID: stale, UsageData: data, PolledAt: polled, ExpiresAt: polled.Add(testTTL)}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	// backoff: one failing Touch puts the account in failure backoff.
	if res := p.Touch(context.Background(), backoff); res.LastError == "" {
		t.Fatalf("Touch(backoff) LastError empty, want a recorded failure")
	}
	// inflight: a Touch whose fetch never completes.
	touchCtx, cancel := context.WithCancel(context.Background())
	touchDone := make(chan struct{})
	go func() { defer close(touchDone); p.Touch(touchCtx, inflight) }()
	t.Cleanup(func() { cancel(); <-touchDone })
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked fetch never started")
	}

	baseline := f.calls.Load()
	if baseline != 2 {
		t.Fatalf("setup fetches = %d, want 2 (backoff Touch + in-flight Touch)", baseline)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 170; i++ {
			session := "sess-" + string(rune('a'+i%20))
			pool := []int64{cold, stale, backoff, inflight}
			_, _ = r.Route(context.Background(), session, PolicyPool, pool)
			_, _ = r.Route(context.Background(), session+"m", PolicyPool, pool, WithModel("claude-opus-4-1"))
			for _, id := range pool {
				_, _ = r.Route(context.Background(), fmt.Sprintf("%sh%d", session, id), PolicyHard, []int64{id})
				_, _ = r.Route(context.Background(), fmt.Sprintf("%shm%d", session, id), PolicyHard, []int64{id}, WithModel("claude-sonnet-4-5"))
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("Route did not return promptly with a fetcher blocked in flight")
	}

	if got := f.calls.Load(); got != baseline {
		t.Errorf("fetches during ~1000 Route calls = %d, want 0", got-baseline)
	}
}

// scriptedFetcher answers the poller from a func the test swaps.
type scriptedFetcher struct {
	fn    func() (quota.Report, error)
	calls atomic.Int64
}

func (f *scriptedFetcher) Fetch(context.Context, accounts.Account) (quota.Report, error) {
	f.calls.Add(1)
	return f.fn()
}

// TestRoute_ExhaustedStandsThroughFailingRefetchRealPoller is AC21 end to end:
// a real Poller fetched an exhausted report once; every later refetch fails;
// routing keeps the account excluded until its ResetsAt and then routes to it
// again, without ever making a fetch of its own.
func TestRoute_ExhaustedStandsThroughFailingRefetchRealPoller(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reg := accounts.New(s)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")

	resets := testNow.Add(2 * time.Hour)
	f := &scriptedFetcher{}
	f.fn = func() (quota.Report, error) {
		rep := availableResult(window("five_hour", 100, &resets)).Report
		return *rep, nil
	}
	clk := &fakeClock{t: testNow}
	p := quota.New(s, reg, f, quota.WithEnabled(true), quota.WithTTL(testTTL), quota.WithClock(clk.Now), quota.WithJitter(func() float64 { return 0 }))
	t.Cleanup(p.Close)
	r := New(s, reg, p, WithClock(clk.Now))

	if res := p.Touch(context.Background(), a); res.Report == nil || res.Stale {
		t.Fatalf("Touch(a) = %+v, want a fresh exhausted report", res)
	}
	f.fn = func() (quota.Report, error) { return quota.Report{}, errors.New("boom") }
	clk.Advance(30 * time.Minute) // past the TTL: the cache is stale
	if res := p.Touch(context.Background(), a); res.LastError == "" || !res.Stale {
		t.Fatalf("second Touch(a) = %+v, want a failed, stale refetch", res)
	}
	baseline := f.calls.Load()

	for i := 0; i < 20; i++ {
		got, err := r.Route(context.Background(), fmt.Sprintf("s%d", i), PolicyPool, []int64{a, b})
		if err != nil || got.ID != b {
			t.Fatalf("Route while exhausted = %v, %v; want account %d", got.ID, err, b)
		}
	}
	clk.Advance(2 * time.Hour) // past ResetsAt, still no successful fetch
	got, err := r.Route(context.Background(), "after-reset", PolicyPool, []int64{a, b})
	if err != nil || got.ID != a {
		t.Fatalf("Route after reset = %v, %v; want account %d", got.ID, err, a)
	}
	if n := f.calls.Load(); n != baseline {
		t.Errorf("Route made %d fetches, want 0", n-baseline)
	}
}
