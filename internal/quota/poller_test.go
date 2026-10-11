package quota

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

func fakeEnv(t *testing.T, f *fakeReportFetcher, opts ...Option) *env {
	t.Helper()
	return newEnv(t, func(*accounts.Registry) ReportFetcher { return f }, opts...)
}

func failing(status int) func(context.Context, accounts.Account) (Report, error) {
	return func(context.Context, accounts.Account) (Report, error) {
		fe := httpStatusError(status, 0)
		return errorReport(fe.Error(), time.Now()), fe
	}
}

func Test_poller_ttl_hit_no_fetch(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	first := e.touch(id)
	e.clk.Advance(time.Minute)
	second := e.touch(id)

	if f.Calls() != 1 {
		t.Fatalf("fetches = %d, want 1", f.Calls())
	}
	if first.Report == nil || second.Report == nil || second.Stale {
		t.Errorf("results = %+v / %+v, want a fresh report both times", first, second)
	}
	if !second.Report.FetchedAt.Equal(first.Report.FetchedAt) {
		t.Errorf("FetchedAt changed on a cache hit")
	}
}

func Test_poller_ttl_expired_touch_refetches(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	e.touch(id)
	e.clk.Advance(5 * time.Minute)
	res := e.touch(id)

	if f.Calls() != 2 {
		t.Fatalf("fetches = %d, want 2", f.Calls())
	}
	if !res.Report.FetchedAt.Equal(e.clk.Now()) {
		t.Errorf("FetchedAt = %v, want the fake clock %v", res.Report.FetchedAt, e.clk.Now())
	}
}

func Test_poller_peek_no_io(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	f := &fakeReportFetcher{fn: func(ctx context.Context, _ accounts.Account) (Report, error) {
		entered <- struct{}{}
		<-release
		return availableTestReport(), nil
	}}
	e := fakeEnv(t, f)
	a, b := e.addOAuth("a"), e.addOAuth("b")

	if res := e.p.Peek(b); res.Report != nil || res.InFlight {
		t.Fatalf("cold Peek = %+v, want no report, not in flight", res)
	}
	done := make(chan Result, 1)
	go func() { done <- e.touch(a) }()
	<-entered // the fetch for a is blocked in the fetcher

	for i := 0; i < 100; i++ {
		e.p.Peek(a)
		e.p.Peek(b)
	}
	if res := e.p.Peek(a); !res.InFlight {
		t.Errorf("Peek during fetch: InFlight = false, want true")
	}
	if f.Calls() != 1 {
		t.Errorf("fetches = %d, want exactly the one Touch", f.Calls())
	}
	close(release)
	if res := <-done; res.Report == nil {
		t.Errorf("Touch result has no report")
	}
	if f.Calls() != 1 {
		t.Errorf("fetches = %d after release, want 1", f.Calls())
	}
}

func Test_poller_error_keeps_last_good(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	good := e.touch(id)
	f.fn = failing(503)
	e.clk.Advance(5 * time.Minute)
	got := e.touch(id)

	if f.Calls() != 2 {
		t.Fatalf("fetches = %d, want 2", f.Calls())
	}
	if got.Report == nil || got.Report.Status != StatusAvailable || !got.Report.FetchedAt.Equal(good.Report.FetchedAt) {
		t.Errorf("report = %+v, want the earlier available report untouched", got.Report)
	}
	if !got.Stale || got.LastError != "http 503" {
		t.Errorf("Stale/LastError = %v/%q, want true/http 503", got.Stale, got.LastError)
	}
	snap, ok, err := e.st.GetLatestQuotaSnapshot(id)
	if err != nil || !ok {
		t.Fatalf("snapshot = %v, %v", ok, err)
	}
	if rep, _ := DecodeReport(snap.UsageData); rep.Status != StatusAvailable {
		t.Errorf("stored report status = %s, want available", rep.Status)
	}
}

func Test_poller_prunes_old_snapshots(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	a, b := e.addOAuth("a"), e.addOAuth("b")

	for i := 0; i < 5; i++ {
		e.touch(a)
		e.touch(b)
		e.clk.Advance(5 * time.Minute)
	}
	snaps, err := e.st.ListQuotaSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	per := map[int64]int{}
	for _, s := range snaps {
		per[s.AccountID]++
	}
	if per[a] != 1 || per[b] != 1 {
		t.Errorf("rows per account = %v, want one each", per)
	}
}

func Test_poller_disabled_no_calls(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f, WithEnabled(false))
	id := e.addOAuth("tok")

	for _, res := range []Result{
		e.p.Peek(id), e.touch(id), e.refresh(id), e.p.RefreshAll(context.Background(), []int64{id})[0],
	} {
		if res.Enabled || res.Report != nil || res.NextRefreshAt != nil || res.RefreshBlocked != "" {
			t.Errorf("disabled result = %+v, want zero apart from the id", res)
		}
		if res.AccountID != id {
			t.Errorf("AccountID = %d, want %d", res.AccountID, id)
		}
	}
	if f.Calls() != 0 {
		t.Errorf("fetches = %d, want 0", f.Calls())
	}
	if n := e.p.stateLoads.Load(); n != 0 {
		t.Errorf("state loads = %d, want 0 (no store access when disabled)", n)
	}
	if e.p.Enabled() {
		t.Errorf("Enabled() = true")
	}
}

func Test_poller_account_deleted(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	block := false
	f := &fakeReportFetcher{}
	f.fn = func(context.Context, accounts.Account) (Report, error) {
		if block {
			entered <- struct{}{}
			<-release
		}
		return availableTestReport(), nil
	}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	e.touch(id)

	if err := e.reg.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := e.st.GetQuotaPollState(id); ok {
		t.Errorf("poll state row survived the account")
	}
	if _, ok, _ := e.st.GetLatestQuotaSnapshot(id); ok {
		t.Errorf("snapshot survived the account")
	}
	if res := e.p.Peek(id); res.Report != nil || !res.Enabled || res.AccountID != id {
		t.Errorf("Peek of deleted account = %+v", res)
	}
	if res := e.touch(id); res.Report != nil {
		t.Errorf("Touch of deleted account = %+v", res)
	}
	if f.Calls() != 1 {
		t.Errorf("fetches = %d, want 1 (none for a deleted account)", f.Calls())
	}

	// Deleted while a fetch is in flight: the late result is swallowed.
	id2 := e.addOAuth("tok2")
	block = true
	done := make(chan Result, 1)
	go func() { done <- e.touch(id2) }()
	<-entered
	if err := e.reg.Delete(id2); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	e.p.Close() // waits for the goroutine
	if _, ok, _ := e.st.GetLatestQuotaSnapshot(id2); ok {
		t.Errorf("late fetch result was stored for a deleted account")
	}
}

func Test_poller_notifies_after_every_completed_fetch(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	notified := make(chan Result, 16)
	e := fakeEnv(t, f, WithOnFetch(func(r Result) { notified <- r }))
	id := e.addOAuth("tok")
	recv := func() Result {
		t.Helper()
		select {
		case r := <-notified:
			return r
		case <-time.After(10 * time.Second):
			t.Fatal("no notification")
			return Result{}
		}
	}

	e.touch(id) // fetch 1
	if r := recv(); r.AccountID != id || r.Report == nil {
		t.Errorf("notification 1 = %+v", r)
	}
	e.touch(id) // cache hit: no notification
	e.clk.Advance(time.Hour)
	e.refresh(id) // fetch 2, identical result
	if r := recv(); r.Report == nil || r.Stale {
		t.Errorf("notification 2 = %+v", r)
	}
	f.fn = failing(503)
	e.clk.Advance(time.Hour)
	e.refresh(id) // fetch 3, error
	if r := recv(); r.LastError != "http 503" || !r.Stale {
		t.Errorf("notification 3 = %+v", r)
	}
	e.p.Close()
	if n := len(notified); n != 0 {
		t.Errorf("%d extra notifications, want 0 (cache hit must not notify)", n)
	}
	if f.Calls() != 3 {
		t.Errorf("fetches = %d, want 3", f.Calls())
	}
}

func Test_poller_ttl_floor_clamped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want time.Duration }{
		{time.Second, 5 * time.Minute},
		{0, 5 * time.Minute},
		{10 * time.Minute, 10 * time.Minute},
	} {
		if got := New(nil, nil, nil, WithTTL(tc.in)).TTL(); got != tc.want {
			t.Errorf("WithTTL(%v).TTL() = %v, want %v", tc.in, got, tc.want)
		}
	}
	if got := New(nil, nil, nil).TTL(); got != 5*time.Minute {
		t.Errorf("default TTL = %v, want 5m", got)
	}
}

func Test_poller_legacy_payload_is_no_report(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	now := e.clk.Now()
	for _, payload := range []string{`{"tokens_used":1,"tokens_limit":2,"resets_at":"2026-01-01T00:00:00Z"}`, `{"v":99,"status":"available"}`, `not json`} {
		if _, err := e.st.CreateQuotaSnapshot(store.QuotaSnapshot{AccountID: id, UsageData: payload, PolledAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if res := e.p.Peek(id); res.Report != nil || res.Stale {
			t.Errorf("Peek(%q) = %+v, want no report", payload, res)
		}
	}
	if res := e.touch(id); res.Report == nil || res.Report.Status != StatusAvailable {
		t.Errorf("Touch over a legacy row = %+v, want a fresh report", res)
	}
	snaps, _ := e.st.ListQuotaSnapshots()
	if len(snaps) != 1 {
		t.Errorf("rows = %d, want the legacy rows replaced by 1", len(snaps))
	}
}

func Test_poller_error_report_stored_when_no_good_report(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{fn: failing(503)}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")

	res := e.touch(id)
	if res.Report == nil || res.Report.Status != StatusError || res.Report.Error != "http 503" {
		t.Fatalf("report = %+v, want the stored error report", res.Report)
	}
	if !res.Stale || res.LastError != "http 503" {
		t.Errorf("Stale/LastError = %v/%q", res.Stale, res.LastError)
	}
	if peek := e.p.Peek(id); peek.Report == nil || peek.Report.Status != StatusError {
		t.Errorf("Peek = %+v, want the error report", peek)
	}
	// A later success replaces it.
	f.fn = nil
	e.clk.Advance(time.Hour)
	if res := e.touch(id); res.Report.Status != StatusAvailable || res.Stale || res.LastError != "" {
		t.Errorf("after recovery = %+v", res)
	}
}

func Test_poller_non_fetcherror_is_fixed_vocabulary(t *testing.T) {
	t.Parallel()
	f := &fakeReportFetcher{fn: func(context.Context, accounts.Account) (Report, error) {
		return Report{}, errors.New("secret-token-abc leaked in error")
	}}
	e := fakeEnv(t, f)
	id := e.addOAuth("tok")
	res := e.touch(id)
	if res.LastError != "error" {
		t.Errorf("LastError = %q, want the fixed value", res.LastError)
	}
}
