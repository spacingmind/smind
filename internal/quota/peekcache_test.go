package quota

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

// Snapshot-cache tests (plan: "Peek snapshot cache"). snapshotLoads counts
// store reads of quota_snapshots; warm Peeks must not move it.

func TestPeekCache_SecondPeekNoStoreRead(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	e.touch(id)

	before := e.p.snapshotLoads.Load()
	for i := 0; i < 50; i++ {
		if res := e.p.Peek(id); res.Report == nil {
			t.Fatalf("Peek %d has no report", i)
		}
	}
	if n := e.p.snapshotLoads.Load() - before; n != 0 {
		t.Errorf("snapshot reads = %d across 50 warm Peeks, want 0", n)
	}
}

func TestPeekCache_WriteThroughOnFetch(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	first := e.touch(id)
	e.clk.Advance(5 * time.Minute) // past TTL: report would be stale

	before := e.p.snapshotLoads.Load()
	e.clk.Advance(time.Minute)
	second := e.touch(id)
	if second.Report == nil || second.Stale {
		t.Fatalf("second Touch = %+v, want a fresh report", second)
	}
	if !second.Report.FetchedAt.After(first.Report.FetchedAt) {
		t.Errorf("second Touch returned the old report")
	}
	if n := e.p.snapshotLoads.Load() - before; n != 0 {
		t.Errorf("snapshot reads = %d after a write-through Touch, want 0", n)
	}
}

func TestPeekCache_FailureKeepsLastGoodReport(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	good := e.touch(id)
	f.fn = failing(503)
	e.clk.Advance(time.Hour)
	got := e.touch(id)

	if got.Report == nil || got.Report.Status != StatusAvailable || !got.Report.FetchedAt.Equal(good.Report.FetchedAt) {
		t.Errorf("report = %+v, want the earlier available report", got.Report)
	}
	if !got.Stale || got.LastError != "http 503" {
		t.Errorf("Stale/LastError = %v/%q, want true/http 503", got.Stale, got.LastError)
	}
	if n := e.p.snapshotLoads.Load(); n != 0 {
		t.Errorf("snapshot reads = %d, want 0 (write-through, no store read)", n)
	}
	if peek := e.p.Peek(id); peek.Report == nil || peek.Report.Status != StatusAvailable || !peek.Stale {
		t.Errorf("Peek after failure = %+v, want stale available report", peek)
	}
}

func TestPeekCache_ColdNegativeCached(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	for i := 0; i < 50; i++ {
		if res := e.p.Peek(id); res.Report != nil {
			t.Fatalf("cold Peek %d = %+v, want no report", i, res)
		}
	}
	if n := e.p.snapshotLoads.Load(); n != 1 {
		t.Fatalf("snapshot reads = %d across 50 cold Peeks, want 1", n)
	}
	if res := e.touch(id); res.Report == nil || res.Report.Status != StatusAvailable {
		t.Fatalf("Touch after cold Peeks = %+v, want a report", res)
	}
	if res := e.p.Peek(id); res.Report == nil || res.Report.Status != StatusAvailable {
		t.Errorf("Peek after fetch = %+v, want the report", res)
	}
	if n := e.p.snapshotLoads.Load(); n != 1 {
		t.Errorf("snapshot reads = %d after fetch, want still 1 (write-through)", n)
	}
}

// Store read errors cannot be injected without a store wrapper (store.Store
// is a concrete type), so peek_cache_store_error_not_cached is not tested;
// the code path keeps snapLoaded false on error by construction.

func TestPeekCache_ReturnsClones(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	first := e.touch(id)

	first.Report.Windows[0].ID = "mutated"
	if got := e.p.Peek(id); got.Report.Windows[0].ID != "five_hour" {
		t.Errorf("second Peek window id = %q, want the unmutated value", got.Report.Windows[0].ID)
	}
	pct := 99.0
	first.Report.Windows[0].UsedPct = &pct
	if got := e.p.Peek(id); *got.Report.Windows[0].UsedPct != 10.0 {
		t.Errorf("second Peek UsedPct = %v, want the unmutated 10", *got.Report.Windows[0].UsedPct)
	}
}

func TestPeekCache_ConcurrentPeekAndFetchRace(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					e.p.Peek(id)
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		e.clk.Advance(5 * time.Minute)
		if res := e.touch(id); res.Report == nil {
			t.Errorf("Touch %d has no report", i)
		}
	}
	close(stop)
	wg.Wait()
	if f.Calls() != 3 {
		t.Errorf("fetches = %d, want 3", f.Calls())
	}
}

func BenchmarkPeekWarm(b *testing.B) {
	f := &fakeReportFetcher{}
	e := newEnv(b, func(*accounts.Registry, *fakeClock) ReportFetcher { return f })
	defer func() { e.p.Close(); e.st.Close() }()
	id := e.addOAuth("tok")
	e.touch(id)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.p.Peek(id)
	}
}

func TestPeekCache_ProblemExpiresAtCloned(t *testing.T) {
	t.Parallel()
	exp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := &fakeReportFetcher{fn: func(context.Context, accounts.Account) (Report, error) {
		return Report{Status: StatusUnavailable, Problem: &Problem{Kind: ProblemExpired, ExpiresAt: &exp}}, nil
	}}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	e.touch(id)

	first := e.p.Peek(id)
	*first.Report.Problem.ExpiresAt = time.Time{}
	if got := e.p.Peek(id); got.Report.Problem.ExpiresAt.IsZero() {
		t.Error("mutating a returned Problem.ExpiresAt changed the cached report")
	}
}
