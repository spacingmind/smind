package quota

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInstanceFetcher struct {
	calls   atomic.Int64
	block   chan struct{} // when non-nil, Fetch waits for it to close
	mu      sync.Mutex
	nextRep Report
	nextErr error
}

func (f *fakeInstanceFetcher) set(rep Report, err error) {
	f.mu.Lock()
	f.nextRep, f.nextErr = rep, err
	f.mu.Unlock()
}

func (f *fakeInstanceFetcher) Fetch(ctx context.Context, instanceID string) (Report, error) {
	f.calls.Add(1)
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nextRep, f.nextErr
}

type fakeUsageClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeUsageClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeUsageClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func availableReport(windows ...Window) Report {
	return Report{Version: ReportVersion, Status: StatusAvailable, Windows: windows}
}

func newTestUsage(f InstanceFetcher, extra ...ProviderUsageOption) (*ProviderUsage, *fakeUsageClock) {
	clk := &fakeUsageClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	opts := append([]ProviderUsageOption{
		WithProviderUsageClock(clk.now),
		WithProviderUsageJitter(func() float64 { return 0 }),
	}, extra...)
	u := NewProviderUsage(f, opts...)
	return u, clk
}

func pct(v float64) *float64 { return &v }

func TestProviderUsage_TouchSingleFlight(t *testing.T) {
	f := &fakeInstanceFetcher{block: make(chan struct{})}
	f.set(availableReport(), nil)
	u, _ := newTestUsage(f)
	defer u.Close()

	results := make([]Result, 16)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = u.Touch(context.Background(), "inst")
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(f.block)
	wg.Wait()

	if got := f.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	for i, r := range results {
		if r.Report == nil {
			t.Fatalf("result %d has no report", i)
		}
	}
}

func TestProviderUsage_TouchWithinTTL(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(), nil)
	u, clk := newTestUsage(f)
	defer u.Close()

	u.Touch(context.Background(), "inst")
	clk.advance(time.Minute)
	u.Touch(context.Background(), "inst")
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	clk.advance(4*time.Minute + time.Second)
	u.Touch(context.Background(), "inst")
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestProviderUsage_PeekNeverFetches(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(), nil)
	u, _ := newTestUsage(f)
	defer u.Close()

	if r := u.Peek("inst"); r.Report != nil {
		t.Fatalf("cold Peek returned a report")
	}
	u.Touch(context.Background(), "inst")
	before := f.calls.Load()
	if r := u.Peek("inst"); r.Report == nil {
		t.Fatalf("warm Peek returned no report")
	}
	if got := f.calls.Load(); got != before || got != 1 {
		t.Fatalf("calls = %d, want 1 (Peek must not fetch)", got)
	}
}

func TestProviderUsage_RefreshFloorAndBudget(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(), nil)
	u, clk := newTestUsage(f)
	defer u.Close()
	ctx := context.Background()

	u.Refresh(ctx, "inst")
	clk.advance(10 * time.Second)
	r := u.Refresh(ctx, "inst")
	if f.calls.Load() != 1 || r.RefreshBlocked != BlockFloor || !r.Stale {
		t.Fatalf("calls=%d blocked=%q stale=%v, want 1/floor/true", f.calls.Load(), r.RefreshBlocked, r.Stale)
	}
	for range 5 {
		clk.advance(31 * time.Second)
		u.Refresh(ctx, "inst")
	}
	if got := f.calls.Load(); got != 6 {
		t.Fatalf("calls = %d, want 6", got)
	}
	clk.advance(31 * time.Second)
	r = u.Refresh(ctx, "inst")
	if got := f.calls.Load(); got != 6 {
		t.Fatalf("calls = %d, want 6 after budget exhausted", got)
	}
	if r.RefreshBlocked != BlockBudget {
		t.Fatalf("blocked = %q, want budget", r.RefreshBlocked)
	}
}

func TestProviderUsage_FailureBackoff(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(errorReport("http 503", time.Time{}), &FetchError{msg: "http 503"})
	u, clk := newTestUsage(f)
	defer u.Close()
	ctx := context.Background()

	r := u.Touch(ctx, "inst")
	if f.calls.Load() != 1 || r.LastError != "http 503" {
		t.Fatalf("calls=%d lastError=%q", f.calls.Load(), r.LastError)
	}
	// Failures 2..4 each wait past the TTL; the 4th failure backs off 8m.
	for want := int64(2); want <= 4; want++ {
		clk.advance(5*time.Minute + time.Second)
		u.Touch(ctx, "inst")
		if got := f.calls.Load(); got != want {
			t.Fatalf("calls = %d, want %d", got, want)
		}
	}
	clk.advance(5*time.Minute + time.Second)
	r = u.Touch(ctx, "inst")
	if got := f.calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4 while in backoff", got)
	}
	if r.LastError != "http 503" {
		t.Fatalf("lastError = %q", r.LastError)
	}
	clk.advance(3 * time.Minute)
	u.Touch(ctx, "inst")
	if got := f.calls.Load(); got != 5 {
		t.Fatalf("calls = %d, want 5 after backoff", got)
	}
}

func TestProviderUsage_RetryAfter429(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(errorReport("http 429", time.Time{}), &FetchError{Status: 429, RetryAfter: 10 * time.Minute, msg: "http 429"})
	u, clk := newTestUsage(f)
	defer u.Close()
	ctx := context.Background()

	u.Touch(ctx, "inst")
	clk.advance(9 * time.Minute)
	u.Touch(ctx, "inst")
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 at +9m", got)
	}
	clk.advance(time.Minute + time.Second)
	u.Touch(ctx, "inst")
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 at +10m01s", got)
	}
}

func TestProviderUsage_FailureKeepsLastGood(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(newWindow("five_hour", "5h", "5h", true, pct(10), nil)), nil)
	u, clk := newTestUsage(f)
	defer u.Close()
	ctx := context.Background()

	u.Touch(ctx, "inst")
	f.set(errorReport("http 500", time.Time{}), &FetchError{msg: "http 500"})
	clk.advance(5*time.Minute + time.Second)
	u.Touch(ctx, "inst")
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	r := u.Peek("inst")
	if r.Report == nil || r.Report.Status != StatusAvailable || len(r.Report.Windows) != 1 {
		t.Fatalf("last good report lost: %+v", r.Report)
	}
	if !r.Stale || r.LastError != "http 500" {
		t.Fatalf("stale=%v lastError=%q", r.Stale, r.LastError)
	}
}

func TestProviderUsage_ExhaustedCacheOnly(t *testing.T) {
	f := &fakeInstanceFetcher{}
	u, clk := newTestUsage(f)
	defer u.Close()
	ctx := context.Background()
	now := clk.now()

	if _, ok := u.Exhausted("inst", 100, now); ok || f.calls.Load() != 0 {
		t.Fatalf("cold Exhausted: ok=%v calls=%d", ok, f.calls.Load())
	}

	set := func(ws ...Window) {
		f.set(availableReport(ws...), nil)
		clk.advance(5*time.Minute + time.Second)
		u.Touch(ctx, "inst")
		now = clk.now()
	}
	var soon, later, past time.Time
	set(newWindow("warm", "w", "w", false, nil, nil))
	soon, later, past = now.Add(7*time.Hour), now.Add(9*time.Hour), now.Add(-time.Hour)
	calls := func() int64 { return f.calls.Load() }

	set(newWindow("five_hour", "5h", "5h", true, pct(100), &soon))
	n := calls()
	got, ok := u.Exhausted("inst", 100, now)
	if !ok || !got.Equal(soon) {
		t.Fatalf("full window: got %v ok=%v, want %v", got, ok, soon)
	}
	if calls() != n {
		t.Fatalf("Exhausted fetched")
	}

	set(newWindow("five_hour", "5h", "5h", true, pct(99), &soon))
	if _, ok := u.Exhausted("inst", 100, now); ok {
		t.Fatalf("99%% at threshold 100 reported exhausted")
	}

	set(newWindow("five_hour", "5h", "5h", true, pct(100), &past))
	if _, ok := u.Exhausted("inst", 100, now); ok {
		t.Fatalf("past reset reported exhausted")
	}

	scoped := newWindow("weekly_model", "w", "w", false, pct(100), &soon)
	scoped.Scope = &Scope{Kind: "model", Name: "opus"}
	set(scoped)
	if _, ok := u.Exhausted("inst", 100, now); ok {
		t.Fatalf("model-scoped window counted")
	}

	set(
		newWindow("five_hour", "5h", "5h", true, pct(100), &soon),
		newWindow("weekly", "w", "w", false, pct(100), &later),
		newWindow("noreset", "n", "n", false, pct(100), nil),
	)
	got, ok = u.Exhausted("inst", 100, now)
	if !ok || !got.Equal(later) {
		t.Fatalf("two windows: got %v ok=%v, want %v", got, ok, later)
	}
}

func TestProviderUsage_DisabledMakesNoRequest(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(), nil)
	u, _ := newTestUsage(f, WithProviderUsageEnabled(false))
	defer u.Close()
	ctx := context.Background()

	for _, r := range []Result{u.Touch(ctx, "inst"), u.Refresh(ctx, "inst"), u.Peek("inst")} {
		if r.Enabled || r.Report != nil {
			t.Fatalf("disabled result = %+v, want zero", r)
		}
	}
	if got := f.calls.Load(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestProviderUsage_CloseStopsFetching(t *testing.T) {
	f := &fakeInstanceFetcher{}
	f.set(availableReport(), nil)
	u, clk := newTestUsage(f)
	ctx := context.Background()

	u.Touch(ctx, "inst")
	u.Close()
	clk.advance(time.Hour)
	r := u.Touch(ctx, "inst")
	u.Refresh(ctx, "inst")
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 after Close", got)
	}
	if r.Report == nil {
		t.Fatalf("closed Touch should answer from the cache")
	}
}
