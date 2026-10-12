package routing

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

var testNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

const testTTL = 5 * time.Minute

// fakeReader serves canned Results and counts Peeks. It is the Router's only
// view of usage in these tests.
type fakeReader struct {
	mu      sync.Mutex
	results map[int64]quota.Result
	peeks   atomic.Int64

	cooldowns     map[cooldownKey]quota.Cooldown
	cooldownCalls atomic.Int64
}

// cooldownKey addresses a canned cooldown; model "" answers every model.
type cooldownKey struct {
	id    int64
	model string
}

func (f *fakeReader) setCooldown(id int64, model string, cd quota.Cooldown) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cooldowns == nil {
		f.cooldowns = map[cooldownKey]quota.Cooldown{}
	}
	f.cooldowns[cooldownKey{id, model}] = cd
}

func (f *fakeReader) set(id int64, res quota.Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results == nil {
		f.results = map[int64]quota.Result{}
	}
	res.AccountID = id
	f.results[id] = res
}

func (f *fakeReader) Peek(id int64) quota.Result {
	f.peeks.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.results[id]
}

func (f *fakeReader) TTL() time.Duration { return testTTL }

func (f *fakeReader) Cooldown(id int64, model string, _ time.Time) (quota.Cooldown, bool) {
	f.cooldownCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if cd, ok := f.cooldowns[cooldownKey{id, model}]; ok {
		return cd, true
	}
	cd, ok := f.cooldowns[cooldownKey{id, ""}]
	return cd, ok
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newFakeRouter(t *testing.T, opts ...Option) (*Router, *store.Store, *accounts.Registry, *fakeReader, *fakeClock) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reg := accounts.New(s)
	fr := &fakeReader{}
	clk := &fakeClock{t: testNow}
	r := New(s, reg, fr, append([]Option{WithClock(clk.Now)}, opts...)...)
	return r, s, reg, fr, clk
}

func fptr(f float64) *float64 { return &f }

func tptr(t time.Time) *time.Time { return &t }

func window(id string, used float64, resets *time.Time) quota.Window {
	rem := 100 - used
	if rem < 0 {
		rem = 0
	}
	return quota.Window{ID: id, UsedPct: fptr(used), RemainingPct: &rem, ResetsAt: resets}
}

func scoped(w quota.Window, kind quota.ScopeKind, name string) quota.Window {
	w.Scope = &quota.Scope{Kind: kind, Name: name}
	return w
}

func availableResult(windows ...quota.Window) quota.Result {
	return quota.Result{Enabled: true, Report: &quota.Report{
		Status:    quota.StatusAvailable,
		Windows:   windows,
		FetchedAt: testNow,
	}}
}

func exhaustedResult(resets time.Time) quota.Result {
	return availableResult(window("five_hour", 100, &resets))
}

func TestRoute_UsageWindows(t *testing.T) {
	t.Parallel()

	hour := testNow.Add(time.Hour)
	week := testNow.Add(7 * 24 * time.Hour)
	overage := true
	noOverage := false

	tests := []struct {
		name      string
		res       quota.Result
		model     string
		threshold int // 0 = default
		wantFirst bool
	}{
		{name: "route_pool_skips_account_exhausted_on_five_hour", res: availableResult(window("five_hour", 100, &hour), window("weekly", 20, &week))},
		{name: "route_pool_skips_account_exhausted_on_weekly", res: availableResult(window("five_hour", 10, &hour), window("weekly", 100, &week))},
		{name: "model_scoped_matching_model_excluded", model: "claude-opus-4-1", res: availableResult(window("weekly", 30, &week), scoped(window("weekly_model_opus", 100, &week), quota.ScopeModel, "Opus"))},
		{name: "model_scoped_other_model_kept", model: "claude-sonnet-4-5", wantFirst: true, res: availableResult(window("weekly", 30, &week), scoped(window("weekly_model_opus", 100, &week), quota.ScopeModel, "Opus"))},
		{name: "route_unknown_model_ignores_model_scoped_windows", model: "", wantFirst: true, res: availableResult(scoped(window("weekly_model_opus", 100, &week), quota.ScopeModel, "Opus"))},
		{name: "route_surface_windows_never_exhaust", model: "claude-opus-4-1", wantFirst: true, res: availableResult(scoped(window("s", 100, &week), quota.ScopeSurface, "Opus"))},
		{name: "route_feature_windows_never_exhaust", model: "claude-opus-4-1", wantFirst: true, res: availableResult(scoped(window("f", 100, &week), quota.ScopeFeature, "Opus"))},
		{name: "route_threshold_90_exhausts_95", threshold: 90, res: availableResult(window("five_hour", 95, &hour))},
		{name: "route_threshold_100_keeps_95", threshold: 100, wantFirst: true, res: availableResult(window("five_hour", 95, &hour))},
		{name: "route_unknown_no_snapshot", wantFirst: true, res: quota.Result{Enabled: true}},
		{name: "route_unknown_status_error", wantFirst: true, res: quota.Result{Enabled: true, Report: &quota.Report{Status: quota.StatusError, Windows: []quota.Window{window("five_hour", 100, &hour)}, FetchedAt: testNow}}},
		{name: "route_unknown_status_unavailable", wantFirst: true, res: quota.Result{Enabled: true, Report: &quota.Report{Status: quota.StatusUnavailable, Windows: []quota.Window{window("five_hour", 100, &hour)}, FetchedAt: testNow}}},
		{name: "route_unknown_enabled_false", wantFirst: true, res: func() quota.Result { r := exhaustedResult(hour); r.Enabled = false; return r }()},
		{name: "route_exhausted_stands_while_cache_is_stale", res: func() quota.Result {
			r := exhaustedResult(hour)
			r.Stale, r.LastError = true, "timeout"
			return r
		}()},
		{name: "route_overage_enabled_not_exhausted", wantFirst: true, res: func() quota.Result { r := exhaustedResult(hour); r.Report.OverageEnabled = &overage; return r }()},
		{name: "route_overage_false_still_exhausted", res: func() quota.Result { r := exhaustedResult(hour); r.Report.OverageEnabled = &noOverage; return r }()},
		{name: "route_overage_nil_still_exhausted", res: exhaustedResult(hour)},
		{name: "route_remaining_pct_zero_is_exhausted", res: availableResult(quota.Window{ID: "w", RemainingPct: fptr(0), ResetsAt: &hour})},
		{name: "route_unknown_window_values_ignored", wantFirst: true, res: availableResult(quota.Window{ID: "w", ResetsAt: &hour})},
		{name: "route_passed_resetsat_is_reset", wantFirst: true, res: availableResult(window("five_hour", 100, tptr(testNow.Add(-time.Second))))},
		{name: "route_resetsat_equal_now_is_reset", wantFirst: true, res: availableResult(window("five_hour", 100, tptr(testNow)))},
		{name: "route_no_resetsat_within_2ttl_exhausted", res: availableResult(window("five_hour", 100, nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := []Option{}
			if tt.threshold != 0 {
				opts = append(opts, WithExhaustThresholdPct(tt.threshold))
			}
			r, _, reg, fr, _ := newFakeRouter(t, opts...)
			a := newAccount(t, reg, "a")
			b := newAccount(t, reg, "b")
			fr.set(a, tt.res)

			var ropts []RouteOption
			if tt.model != "" {
				ropts = append(ropts, WithModel(tt.model))
			}
			got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b}, ropts...)
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			want := b
			if tt.wantFirst {
				want = a
			}
			if got.ID != want {
				t.Errorf("Route() = account %d, want %d", got.ID, want)
			}
		})
	}
}

func TestRoute_ModelScopedWindowExcludesOnlyMatchingModel(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	week := testNow.Add(24 * time.Hour)
	fr.set(a, availableResult(scoped(window("weekly_model_opus", 100, &week), quota.ScopeModel, "Opus")))

	opus, err := r.Route(context.Background(), "opus-sess", PolicyPool, []int64{a, b}, WithModel("claude-opus-4-1"))
	if err != nil || opus.ID != b {
		t.Fatalf("opus Route() = %d, %v; want account %d", opus.ID, err, b)
	}
	sonnet, err := r.Route(context.Background(), "sonnet-sess", PolicyPool, []int64{a, b}, WithModel("claude-sonnet-4-5"))
	if err != nil || sonnet.ID != a {
		t.Fatalf("sonnet Route() = %d, %v; want account %d", sonnet.ID, err, a)
	}
}

func TestRoute_ExhaustedStandsWhileStaleThenResets(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	resets := testNow.Add(2 * time.Hour)
	res := exhaustedResult(resets)
	res.Stale, res.LastError = true, "timeout"
	fr.set(a, res)

	for i, adv := range []time.Duration{0, 30 * time.Minute, 89 * time.Minute} {
		clk.Advance(adv)
		got, err := r.Route(context.Background(), fmt.Sprintf("s%d", i), PolicyPool, []int64{a, b})
		if err != nil || got.ID != b {
			t.Fatalf("before reset Route() = %d, %v; want %d", got.ID, err, b)
		}
	}
	clk.Advance(time.Hour) // past ResetsAt, still no fetch and still stale
	got, err := r.Route(context.Background(), "after", PolicyPool, []int64{a, b})
	if err != nil || got.ID != a {
		t.Fatalf("after reset Route() = %d, %v; want %d", got.ID, err, a)
	}
}

func TestRoute_ExhaustedWithoutResetsAtExpiresAfter2TTL(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, clk := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fr.set(a, availableResult(window("five_hour", 100, nil)))

	clk.Advance(2 * testTTL)
	if got, err := r.Route(context.Background(), "at-2ttl", PolicyPool, []int64{a, b}); err != nil || got.ID != b {
		t.Fatalf("at 2*TTL Route() = %d, %v; want %d", got.ID, err, b)
	}
	clk.Advance(time.Second)
	if got, err := r.Route(context.Background(), "after-2ttl", PolicyPool, []int64{a, b}); err != nil || got.ID != a {
		t.Fatalf("after 2*TTL Route() = %d, %v; want %d", got.ID, err, a)
	}
}

func TestRoute_AllExhaustedErrorNamesEarliestReset(t *testing.T) {
	t.Parallel()

	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	late := testNow.Add(5 * time.Hour)
	early := testNow.Add(90 * time.Minute)
	fr.set(a, exhaustedResult(late))
	// b's until is the LATEST of its windows (weekly), which is still earlier
	// than a's.
	fr.set(b, availableResult(window("five_hour", 100, tptr(testNow.Add(time.Hour))), window("weekly", 100, &early)))

	_, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b})
	if err == nil {
		t.Fatal("Route() error = nil, want all-exhausted")
	}
	want := "routing: all 2 candidate accounts are exhausted; earliest reset " + early.Format(time.RFC3339)
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if ds, _ := s.ListRoutingDecisions(); len(ds) != 0 {
		t.Errorf("decisions = %d, want 0 on error", len(ds))
	}
}

func TestRoute_ExhaustedMultipleWindowsUntilIsLatest(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	hour := testNow.Add(time.Hour)
	week := testNow.Add(72 * time.Hour)
	fr.set(a, availableResult(window("five_hour", 100, &hour), window("weekly", 100, &week), window("other", 10, tptr(testNow.Add(500*time.Hour)))))

	_, err := r.Route(context.Background(), "s", PolicyHard, []int64{a})
	if err == nil || !strings.Contains(err.Error(), "until "+week.Format(time.RFC3339)) {
		t.Fatalf("error = %v, want until %s", err, week.Format(time.RFC3339))
	}
}

func TestRoute_HardExhaustedErrorsWithReset(t *testing.T) {
	t.Parallel()

	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	resets := testNow.Add(time.Hour)
	fr.set(a, exhaustedResult(resets))

	_, err := r.Route(context.Background(), "s", PolicyHard, []int64{a})
	want := fmt.Sprintf("routing: account %d is exhausted until %s; hard policy has no failover", a, resets.Format(time.RFC3339))
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if ds, _ := s.ListRoutingDecisions(); len(ds) != 0 {
		t.Errorf("decisions = %d, want 0 on error", len(ds))
	}
}

func TestRoute_HardUnknownRoutes(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	fr.set(a, quota.Result{Enabled: true, Report: &quota.Report{Status: quota.StatusError, FetchedAt: testNow}, Stale: true, LastError: "timeout"})

	got, err := r.Route(context.Background(), "s", PolicyHard, []int64{a})
	if err != nil || got.ID != a {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, a)
	}
}

func TestRoute_AffinityExhaustedPinnedReplaced(t *testing.T) {
	t.Parallel()

	r, s, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	first, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil || first.ID != a {
		t.Fatalf("first Route() = %d, %v; want %d", first.ID, err, a)
	}
	fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
	second, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil || second.ID != b {
		t.Fatalf("second Route() = %d, %v; want pinned account replaced by %d", second.ID, err, b)
	}
	if ds, _ := s.ListRoutingDecisions(); len(ds) != 2 {
		t.Errorf("decisions = %d, want 2", len(ds))
	}
}

func TestRoute_AffinityPinned85PctKeptOverIdleCandidate(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{b, a}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	hour := testNow.Add(time.Hour)
	fr.set(b, availableResult(window("five_hour", 85, &hour)))
	fr.set(a, availableResult(window("five_hour", 0, &hour)))
	got, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want pinned %d kept (no pressure migration)", got.ID, err, b)
	}
}

func TestRoute_AffinityPinnedOutsideCandidatesIgnored(t *testing.T) {
	t.Parallel()

	r, _, reg, _, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	ctx := context.Background()

	if _, err := r.Route(ctx, "sess", PolicyPool, []int64{a}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	got, err := r.Route(ctx, "sess", PolicyPool, []int64{b})
	if err != nil || got.ID != b {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, b)
	}
}

// TestRoute_PollerErrorNeverFailsRouting: a failing fetch only ever shows up
// as an error/stale Result, never as a Route error.
func TestRoute_PollerErrorNeverFailsRouting(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t)
	a := newAccount(t, reg, "a")
	fr.set(a, quota.Result{Enabled: true, Stale: true, LastError: "network error",
		Report: &quota.Report{Status: quota.StatusError, Error: "network error", FetchedAt: testNow}})
	for i := 0; i < 5; i++ {
		for _, policy := range []string{PolicyHard, PolicyPool} {
			if _, err := r.Route(context.Background(), fmt.Sprintf("%s-%d", policy, i), policy, []int64{a}, WithModel("claude-opus-4-1")); err != nil {
				t.Fatalf("Route(%s) error = %v, want nil", policy, err)
			}
		}
	}
}

func TestRoute_RoutingDisabledNeverConsultsWindows(t *testing.T) {
	t.Parallel()

	r, _, reg, fr, _ := newFakeRouter(t, WithUsageRouting(false))
	a := newAccount(t, reg, "a")
	fr.set(a, exhaustedResult(testNow.Add(time.Hour)))

	for i := 0; i < 3; i++ {
		got, err := r.Route(context.Background(), fmt.Sprintf("s%d", i), PolicyHard, []int64{a})
		if err != nil || got.ID != a {
			t.Fatalf("Route() = %d, %v; want %d", got.ID, err, a)
		}
	}
	if n := fr.peeks.Load(); n != 0 {
		t.Errorf("Peek calls = %d, want 0 with usage routing off", n)
	}
	if n := fr.cooldownCalls.Load(); n != 0 {
		t.Errorf("Cooldown calls = %d, want 0 with usage routing off", n)
	}
}

func TestRoute_PollingDisabledSameAsToday(t *testing.T) {
	t.Parallel()

	r, _, reg := newTestRouter(t) // real, disabled poller
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	got, err := r.Route(context.Background(), "s", PolicyPool, []int64{a, b}, WithModel("claude-opus-4-1"))
	if err != nil || got.ID != a {
		t.Fatalf("Route() = %d, %v; want %d", got.ID, err, a)
	}
}

// TestRoute_HoldsReaderOnly is a compile-time check: Router is built from a
// quota.Reader (Peek + TTL), which has no method that can fetch.
func TestRoute_HoldsReaderOnly(t *testing.T) {
	t.Parallel()
	var _ quota.Reader = (*fakeReader)(nil)
	var r Router
	var _ quota.Reader = r.reader
}

// A bad threshold (config load returns the parsed value with its validation
// error) must never make a healthy account look exhausted.
func TestRoute_ThresholdOutOfRangeIgnored(t *testing.T) {
	t.Parallel()
	for _, pct := range []int{0, -5, 101} {
		t.Run(fmt.Sprint(pct), func(t *testing.T) {
			t.Parallel()
			r, _, reg, fr, _ := newFakeRouter(t, WithExhaustThresholdPct(pct))
			a := newAccount(t, reg, "a")
			fr.set(a, availableResult(window("five_hour", 5, tptr(testNow.Add(time.Hour)))))
			got, err := r.Route(context.Background(), "s", PolicyHard, []int64{a})
			if err != nil || got.ID != a {
				t.Fatalf("Route() = %v, %v; want account %d (5%% used, threshold %d ignored)", got.ID, err, a, pct)
			}
		})
	}
}
