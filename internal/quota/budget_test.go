package quota

import (
	"context"
	"fmt"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
	"net/http"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Call-budget tests (ADR-0028 §3, plan "Go tests: call budget"). Every test
// drives the real AccountFetcher against a counting httptest server and
// asserts an exact request count; no real endpoint is ever reachable.

// ---- Group 1: entry points and floors ----

func Test_budget_reader_has_no_fetch_surface(t *testing.T) {
	t.Parallel()
	var r interface{ Peek(int64) Result } = (*Poller)(nil)
	_ = r
	if n := reflect.TypeOf((*Reader)(nil)).Elem().NumMethod(); n != 1 {
		t.Fatalf("Reader has %d methods, want exactly 1 (Peek)", n)
	}
	m := reflect.TypeOf((*Reader)(nil)).Elem().Method(0)
	if m.Name != "Peek" {
		t.Fatalf("Reader method = %q, want Peek", m.Name)
	}
}

func Test_budget_peek_never_calls(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	srv.Set(pathAnthropic, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer bad":
			respondStatus(503)(w, r)
		case "Bearer rej":
			respondStatus(401)(w, r)
		default:
			respondUsageOK(w, r)
		}
	})
	cold := e.addOAuth("cold")
	stale := e.addOAuth("stale")
	backoff := e.addOAuth("bad")
	rejected := e.addOAuth("rej")

	e.touch(stale)
	e.touch(backoff)
	e.touch(rejected)
	e.clk.Advance(time.Hour) // stale is now long past its TTL; backoff is over but the row is kept
	a3aWantCalls(t, srv, 3, "setup")
	if got := e.p.Peek(rejected); got.RefreshBlocked != BlockRejected {
		t.Fatalf("rejected account RefreshBlocked = %q, want rejected", got.RefreshBlocked)
	}

	for i := 0; i < 250; i++ {
		for _, id := range []int64{cold, stale, backoff, rejected} {
			e.p.Peek(id)
		}
	}
	a3aWantCalls(t, srv, 3, "after 1000 Peeks")
}

func Test_budget_touch_within_ttl_zero_calls(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	e.touch(id)
	a3aWantCalls(t, srv, 1, "first Touch")
	for i := 0; i < 100; i++ {
		e.touch(id)
	}
	a3aWantCalls(t, srv, 1, "100 more Touches inside the TTL")
}

func Test_budget_touch_ttl_boundary(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	e.touch(id)
	e.clk.Advance(e.p.TTL() - time.Second)
	e.touch(id)
	a3aWantCalls(t, srv, 1, "TTL-1s")
	e.clk.Advance(time.Second)
	e.touch(id)
	a3aWantCalls(t, srv, 2, "at TTL")
	e.touch(id)
	a3aWantCalls(t, srv, 2, "again right after the TTL fetch")
}

func Test_budget_touch_ttl_counts_from_last_attempt_not_last_success(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")
	ttl := e.p.TTL()

	e.touch(id) // success at t0
	a3aWantCalls(t, srv, 1, "success")
	srv.Set(pathAnthropic, respondStatus(503))
	e.clk.Advance(ttl)
	e.touch(id) // failed attempt at t0+TTL
	a3aWantCalls(t, srv, 2, "failing attempt")

	// Failure backoff is 1 m (no jitter) and is over after a minute, but the
	// TTL counts from that failed attempt, not from the last success.
	e.clk.Advance(time.Minute)
	e.touch(id)
	a3aWantCalls(t, srv, 2, "backoff over, TTL from failed attempt not elapsed")
	e.clk.Advance(ttl - time.Minute - time.Second)
	e.touch(id)
	a3aWantCalls(t, srv, 2, "one second before attempt+TTL")
	e.clk.Advance(time.Second)
	e.touch(id)
	a3aWantCalls(t, srv, 3, "at attempt+TTL")
}

func Test_budget_burst_50_concurrent_touch_one_call(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	gate := newA3aGate(1)
	srv.Set(pathAnthropic, gate.Handler())
	t.Cleanup(gate.Open)
	id := e.addOAuth("tok")

	results := make([]Result, 50)
	var wg sync.WaitGroup
	call := func(i int) {
		defer wg.Done()
		results[i] = e.touch(id)
	}
	wg.Add(1)
	go call(0)
	a3aWait(t, gate.entered, 1, "the leader inside the handler")

	// The handler stays parked while the other 49 callers pile on, so they
	// all find the flight open. (A caller scheduled after the release would
	// meet a fresh cache instead: also zero requests.)
	var ready sync.WaitGroup
	start := make(chan struct{})
	for i := 1; i < 50; i++ {
		wg.Add(1)
		ready.Add(1)
		go func() {
			ready.Done()
			<-start
			call(i)
		}()
	}
	ready.Wait()
	close(start)
	gate.Open()
	wg.Wait()

	a3aWantCalls(t, srv, 1, "50 concurrent Touches")
	for i := 1; i < 50; i++ {
		if !reflect.DeepEqual(results[i], results[0]) {
			t.Fatalf("result %d = %+v, want identical to result 0 = %+v", i, results[i], results[0])
		}
	}
	if results[0].Report == nil || results[0].Report.Status != StatusAvailable {
		t.Fatalf("shared result = %+v, want an available report", results[0])
	}
}

func Test_budget_burst_50_concurrent_mixed_touch_refresh_one_call(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	gate := newA3aGate(1)
	srv.Set(pathAnthropic, gate.Handler())
	t.Cleanup(gate.Open)
	id := e.addOAuth("tok")

	results := make([]Result, 50)
	var wg sync.WaitGroup
	// A Touch leads, so the attempt itself is unforced; the Refresh callers
	// that join (or arrive after it, under the floor) must not spend forced
	// budget either.
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0] = e.touch(id)
	}()
	a3aWait(t, gate.entered, 1, "the Touch leader inside the handler")

	var ready sync.WaitGroup
	start := make(chan struct{})
	for i := 1; i < 50; i++ {
		wg.Add(1)
		ready.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			if i%2 == 0 {
				results[i] = e.refresh(id)
			} else {
				results[i] = e.touch(id)
			}
		}()
	}
	ready.Wait()
	close(start)
	gate.Open()
	wg.Wait()

	a3aWantCalls(t, srv, 1, "25 Touch + 25 Refresh")
	if st := e.state(id); len(st.ForcedAt) != 0 {
		t.Fatalf("ForcedAt = %v, want empty: joiners must not spend forced budget", st.ForcedAt)
	}
	for i, r := range results {
		if r.Report == nil || !r.Report.FetchedAt.Equal(results[0].Report.FetchedAt) {
			t.Fatalf("result %d = %+v, want the one shared report", i, r)
		}
	}
}

func Test_budget_single_flight_is_per_account(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	// The global cap is 2 in flight: release once two requests are parked.
	gate := newA3aGate(8)
	srv.Set(pathAnthropic, gate.Handler())
	t.Cleanup(gate.Open)
	go func() {
		<-gate.entered
		<-gate.entered
		gate.Open()
	}()

	ids := make([]int64, 5)
	for i := range ids {
		ids[i] = e.addOAuth(fmt.Sprintf("tok%d", i))
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e.touch(id)
			}()
		}
	}
	wg.Wait()

	a3aWantCalls(t, srv, 5, "50 Touches over 5 accounts")
	for i := range ids {
		if n := srv.TokenCalls(fmt.Sprintf("tok%d", i)); n != 1 {
			t.Errorf("account %d requests = %d, want 1", i, n)
		}
	}
}

func Test_budget_forced_floor_30s(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	t0 := e.clk.Now()
	first := e.refresh(id)
	if first.RefreshBlocked != BlockFloor {
		t.Fatalf("after the first Refresh RefreshBlocked = %q, want floor", first.RefreshBlocked)
	}
	for i := 1; i < 10; i++ {
		e.clk.Advance(time.Second) // 1..9 s: all inside the floor
		res := e.refresh(id)
		if res.RefreshBlocked != BlockFloor || !res.Stale {
			t.Fatalf("Refresh #%d: RefreshBlocked/Stale = %q/%v, want floor/true", i+1, res.RefreshBlocked, res.Stale)
		}
		if res.NextRefreshAt == nil || !res.NextRefreshAt.Equal(t0.Add(30*time.Second)) {
			t.Fatalf("Refresh #%d: NextRefreshAt = %v, want t0+30s", i+1, res.NextRefreshAt)
		}
	}
	a3aWantCalls(t, srv, 1, "10 Refreshes within 30 s")

	e.clk.Set(t0.Add(30 * time.Second))
	e.refresh(id)
	a3aWantCalls(t, srv, 2, "Refresh at +30 s")
}

func Test_budget_forced_hourly_cap(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	t0 := e.clk.Now()
	var last Result
	for k := 0; time.Duration(k)*31*time.Second < time.Hour; k++ {
		e.clk.Set(t0.Add(time.Duration(k) * 31 * time.Second))
		last = e.refresh(id)
		if k == 6 {
			if last.RefreshBlocked != BlockBudget {
				t.Fatalf("7th Refresh RefreshBlocked = %q, want budget", last.RefreshBlocked)
			}
			if last.NextRefreshAt == nil || !last.NextRefreshAt.Equal(t0.Add(time.Hour)) {
				t.Fatalf("7th Refresh NextRefreshAt = %v, want oldest forced + 1h = %v", last.NextRefreshAt, t0.Add(time.Hour))
			}
		}
	}
	a3aWantCalls(t, srv, 6, "Refresh every 31 s for one hour")
	if last.RefreshBlocked != BlockBudget {
		t.Fatalf("last Refresh of the hour RefreshBlocked = %q, want budget", last.RefreshBlocked)
	}

	// The window is rolling: the oldest forced attempt leaves it at +1h.
	e.clk.Set(t0.Add(time.Hour))
	e.refresh(id)
	a3aWantCalls(t, srv, 7, "Refresh once the oldest forced attempt aged out")
}

func Test_budget_excess_refresh_not_queued(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	for k := 0; k < 8; k++ { // 6 spend the budget, 2 are refused
		e.refresh(id)
		e.clk.Advance(31 * time.Second)
	}
	a3aWantCalls(t, srv, 6, "6 allowed, 2 refused")

	e.clk.Advance(2 * time.Hour) // nobody calls: nothing may run
	a3aWantCalls(t, srv, 6, "two idle hours")
	e.p.Peek(id)
	a3aWantCalls(t, srv, 6, "Peek after the idle hours")
}

func Test_budget_hourly_ceiling_adversarial_client(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	// An adversary alternates Touch (even seconds) and Refresh (odd seconds)
	// for one hour. The Touch TTL counts from any attempt, so Touches yield at
	// most 3600/300 = 12; forced attempts add at most 6.
	t0 := e.clk.Now()
	for s := 0; s < 3600; s++ {
		e.clk.Set(t0.Add(time.Duration(s) * time.Second))
		if s%2 == 0 {
			e.touch(id)
		} else {
			e.refresh(id)
		}
	}
	got := srv.Calls()
	if got > 18 {
		t.Fatalf("requests in one hour = %d, want <= 18 (12 TTL + 6 forced)", got)
	}
	// Exact yield: Touch at 0 s; forced at 31, 61, 91, 121, 151, 181 s (first
	// odd second past each 30 s floor); then the forced budget is spent until
	// 31 s+1h, so only Touches remain, each 5 min after the last attempt, on an
	// even second: 482, 782, ..., 3482 s = 11 more. 1 + 6 + 11 = 18.
	if got != 18 {
		t.Fatalf("requests in one hour = %d, want exactly 18", got)
	}
}

func Test_budget_refresh_all_max_two_in_flight(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	// The first two requests park until both are inside; the rest pass.
	var seen atomic.Int32
	bothIn := make(chan struct{})
	srv.Set(pathAnthropic, func(w http.ResponseWriter, r *http.Request) {
		if seen.Add(1) <= 2 {
			if seen.Load() >= 2 {
				select {
				case <-bothIn:
				default:
					close(bothIn)
				}
			}
			select {
			case <-bothIn:
			case <-time.After(5 * time.Second):
				t.Error("two concurrent fetches never overlapped")
			}
		}
		respondUsageOK(w, r)
	})
	ids := make([]int64, 6)
	for i := range ids {
		ids[i] = e.addOAuth(fmt.Sprintf("tok%d", i))
	}

	results := e.p.RefreshAll(context.Background(), ids)

	a3aWantCalls(t, srv, 6, "RefreshAll of 6 accounts")
	if got := srv.MaxConcurrent(); got != 2 {
		t.Fatalf("max concurrent requests = %d, want exactly 2 (cap)", got)
	}
	for i, res := range results {
		if res.AccountID != ids[i] || res.Report == nil {
			t.Fatalf("result %d = %+v, want account %d with a report", i, res, ids[i])
		}
	}
}

func Test_budget_refresh_all_skips_blocked_accounts(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	srv.Set(pathAnthropic, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer bad":
			respondStatus(503)(w, r)
		case "Bearer rej":
			respondStatus(401)(w, r)
		default:
			respondUsageOK(w, r)
		}
	})
	backoff := e.addOAuth("bad")
	rejected := e.addOAuth("rej")
	okA, okB, okC := e.addOAuth("a"), e.addOAuth("b"), e.addOAuth("c")

	e.touch(backoff)
	e.touch(rejected)
	a3aWantCalls(t, srv, 2, "setup")
	if e.p.Peek(backoff).RefreshBlocked != BlockBackoff || e.p.Peek(rejected).RefreshBlocked != BlockRejected {
		t.Fatalf("setup did not leave backoff/rejected states")
	}

	res := e.p.RefreshAll(context.Background(), []int64{backoff, okA, rejected, okB, okC})

	a3aWantCalls(t, srv, 5, "RefreshAll: 2 blocked, 3 fresh")
	for tok, want := range map[string]int{"bad": 1, "rej": 1, "a": 1, "b": 1, "c": 1} {
		if got := srv.TokenCalls(tok); got != want {
			t.Errorf("requests for %q = %d, want %d", tok, got, want)
		}
	}
	if res[0].RefreshBlocked != BlockBackoff || res[2].RefreshBlocked != BlockRejected {
		t.Errorf("blocked results = %q / %q, want backoff / rejected", res[0].RefreshBlocked, res[2].RefreshBlocked)
	}
}

// ---- Group 2: no stray triggers ----

func Test_budget_startup_zero_calls(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	a, b, c := e.addOAuth("a"), e.addOAuth("b"), e.addOAuth("c")
	for _, id := range []int64{a, b, c} {
		e.touch(id) // leaves a snapshot and a poll-state row per account
	}
	a3aWantCalls(t, srv, 3, "seeding")

	e.restart() // a Poller built over existing snapshots and state
	e.clk.Advance(24 * time.Hour)
	a3aWantCalls(t, srv, 3, "24 h after startup")
	if n := e.p.stateLoads.Load(); n != 0 {
		t.Fatalf("stateLoads after startup = %d, want 0 (state is read lazily)", n)
	}

	e.p.Peek(a)
	e.p.Peek(a)
	if n := e.p.stateLoads.Load(); n != 1 {
		t.Fatalf("stateLoads after Peek(a) = %d, want 1 (account a only)", n)
	}
	e.p.Peek(b)
	if n := e.p.stateLoads.Load(); n != 2 {
		t.Fatalf("stateLoads after Peek(b) = %d, want 2", n)
	}
	a3aWantCalls(t, srv, 3, "after Peeks")
}

type a3aNotifier struct{ updated, removed atomic.Int32 }

func (n *a3aNotifier) NotifyAccountUpdated(store.Account) { n.updated.Add(1) }
func (n *a3aNotifier) NotifyAccountRemoved(int64)         { n.removed.Add(1) }

func Test_budget_account_events_zero_calls(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	n := &a3aNotifier{}
	e.reg.SetNotifier(n)

	id := e.addOAuth("tok")
	victim := e.addOAuth("victim")
	if _, err := e.reg.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if _, err := e.reg.ReplaceCredential(id, "sk-ant-new", "https://llm.example.com"); err != nil {
		t.Fatalf("ReplaceCredential() error = %v", err)
	}
	if err := e.reg.Delete(victim); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if n.updated.Load() < 2 || n.removed.Load() != 1 {
		t.Fatalf("notifications updated/removed = %d/%d, want >=2/1", n.updated.Load(), n.removed.Load())
	}
	e.clk.Advance(time.Hour)
	a3aWantCalls(t, srv, 0, "after account events")

	// Back to an OAuth credential; only now does a caller trigger a request.
	id2 := e.addOAuth("tok2")
	a3aWantCalls(t, srv, 0, "after AddOAuth")
	e.touch(id2)
	a3aWantCalls(t, srv, 1, "first Touch")
}

func Test_budget_no_background_timer(t *testing.T) {
	// Not parallel: runtime.NumGoroutine is process-wide.
	srv := newUsageServer(t)
	clk := newFakeClock()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	reg := accounts.New(st)
	before := runtime.NumGoroutine()

	ids := make([]int64, 3)
	for i := range ids {
		a, err := reg.AddOAuth("anthropic", "acct", accounts.OAuthCredential{
			AccessToken: fmt.Sprintf("tok%d", i), RefreshToken: "r", ExpiresAt: time.Now().Add(100000 * time.Hour),
		})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}
		ids[i] = a.ID
	}
	p := New(st, reg, a3aFetcher(srv, reg, clk), WithClock(clk.Now))
	t.Cleanup(func() {
		p.Close()
		st.Close()
	})

	for h := 0; h < 24; h++ {
		clk.Advance(time.Hour)
		for _, id := range ids {
			p.Peek(id)
		}
	}
	a3aWantCalls(t, srv, 0, "24 fake hours")
	a3aEventually(t, "goroutines back to the pre-poller count", func() bool {
		return runtime.NumGoroutine() <= before
	})
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d, want <= %d", got, before)
	}
}

func Test_budget_disabled_zero_calls(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t, WithEnabled(false))
	id := e.addOAuth("tok")

	for i := 0; i < 3; i++ {
		e.p.Peek(id)
		e.touch(id)
		e.refresh(id)
		e.p.RefreshAll(context.Background(), []int64{id})
		e.clk.Advance(time.Hour)
	}
	a3aWantCalls(t, srv, 0, "disabled poller")
	if _, found, err := e.st.GetQuotaPollState(id); err != nil || found {
		t.Fatalf("poll state found/err = %v/%v, want no row", found, err)
	}
	if n := e.p.stateLoads.Load(); n != 0 {
		t.Fatalf("stateLoads = %d, want 0", n)
	}
}

func Test_budget_closed_poller_never_fetches(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	e.p.Close()
	e.touch(id)
	e.refresh(id)
	e.p.RefreshAll(context.Background(), []int64{id})
	e.clk.Advance(time.Hour)
	e.touch(id)
	a3aWantCalls(t, srv, 0, "closed poller")
}

// ---- Group 3: the floors survive restarts and crashes ----

func Test_budget_ttl_persists_across_restart(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	e.touch(id)
	e.restart()
	e.touch(id)
	a3aWantCalls(t, srv, 1, "Touch right after a restart")
	e.clk.Advance(e.p.TTL())
	e.touch(id)
	a3aWantCalls(t, srv, 2, "Touch at the TTL after a restart")
}

func Test_budget_forced_budget_persists_across_restart(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	for k := 0; k < 6; k++ {
		e.refresh(id)
		e.clk.Advance(31 * time.Second)
	}
	a3aWantCalls(t, srv, 6, "6 forced")
	e.restart()
	res := e.refresh(id)
	if res.RefreshBlocked != BlockBudget || !res.Stale {
		t.Fatalf("7th Refresh after restart RefreshBlocked/Stale = %q/%v, want budget/true", res.RefreshBlocked, res.Stale)
	}
	a3aWantCalls(t, srv, 6, "7th Refresh after restart")
}

func Test_budget_write_ahead_crash_mid_fetch(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	gate := newA3aGate(2)
	srv.Set(pathAnthropic, gate.Handler())
	t.Cleanup(gate.Open) // registered after the server: runs before it closes
	id := e.addOAuth("tok")

	done := make(chan Result, 1)
	go func() { done <- e.touch(id) }()
	a3aWait(t, gate.entered, 1, "the fetch inside the handler")

	// The attempt is on disk before the response exists.
	if st := e.state(id); st.LastAttemptAt.IsZero() {
		t.Fatalf("LastAttemptAt not persisted while the fetch is in flight")
	}

	// "Crash": a second Poller over a reopened store, the first untouched.
	st2, err := store.Open(e.path)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	reg2 := accounts.New(st2)
	p2 := New(st2, reg2, a3aFetcher(srv, reg2, e.clk), e.opts...)
	t.Cleanup(func() {
		p2.Close()
		st2.Close()
	})
	e.clk.Advance(e.p.TTL() - time.Second)
	p2.Touch(context.Background(), id)
	a3aWantCalls(t, srv, 1, "second Poller inside the TTL of the crashed attempt")

	gate.Open()
	<-done
	a3aWantCalls(t, srv, 1, "after the first fetch finished")
}

func Test_budget_state_row_deleted_with_account(t *testing.T) {
	t.Parallel()
	e, srv := a3aEnv(t)
	id := e.addOAuth("tok")

	e.touch(id)
	if _, found, _ := e.st.GetQuotaPollState(id); !found {
		t.Fatalf("poll state row missing after a Touch")
	}
	if err := e.reg.Delete(id); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, found, _ := e.st.GetQuotaPollState(id); found {
		t.Fatalf("poll state row survived the account")
	}
	e.clk.Advance(time.Hour)
	e.touch(id)
	e.refresh(id)
	e.p.Peek(id)
	a3aWantCalls(t, srv, 1, "after the account was deleted")
	if _, found, _ := e.st.GetQuotaPollState(id); found {
		t.Fatalf("poll state row recreated for a deleted account")
	}
}

func Test_budget_save_failure_does_not_fetch(t *testing.T) {
	t.Parallel()
	t.Run("save fails", func(t *testing.T) {
		t.Parallel()
		e, srv := a3aEnv(t)
		real := e.addOAuth("tok")
		acc, err := e.reg.Get(real)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		acc.ID = real + 1000 // no account row: the foreign key rejects the save
		p := New(e.st, a3aStubSource{acc}, a3aFetcher(srv, e.reg, e.clk), e.opts...)
		t.Cleanup(p.Close)

		for i := 0; i < 3; i++ {
			p.Touch(context.Background(), acc.ID)
			p.Refresh(context.Background(), acc.ID)
			e.clk.Advance(time.Hour)
		}
		a3aWantCalls(t, srv, 0, "unsaveable write-ahead")
	})
	t.Run("state unreadable", func(t *testing.T) {
		t.Parallel()
		e, srv := a3aEnv(t)
		real := e.addOAuth("tok")
		acc, err := e.reg.Get(real)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		p := New(e.st, a3aStubSource{acc}, a3aFetcher(srv, e.reg, e.clk), e.opts...)
		t.Cleanup(p.Close)
		if err := e.st.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}

		p.Touch(context.Background(), acc.ID)
		p.Refresh(context.Background(), acc.ID)
		a3aWantCalls(t, srv, 0, "unreadable state")
	})
}

func Test_budget_clock_skew_clamped(t *testing.T) {
	t.Parallel()
	t.Run("next_allowed_at three days ahead", func(t *testing.T) {
		t.Parallel()
		e, srv := a3aEnv(t)
		id := e.addOAuth("tok")
		now := e.clk.Now()
		if err := e.st.SaveQuotaPollState(store.QuotaPollState{
			AccountID: id, LastAttemptAt: now.Add(-time.Hour), NextAllowedAt: now.Add(72 * time.Hour), ConsecutiveFailures: 3,
		}); err != nil {
			t.Fatalf("SaveQuotaPollState() error = %v", err)
		}

		res := e.p.Peek(id)
		if res.NextRefreshAt == nil || res.NextRefreshAt.After(now.Add(6*time.Hour)) {
			t.Fatalf("NextRefreshAt = %v, want <= now+6h", res.NextRefreshAt)
		}
		e.touch(id)
		a3aWantCalls(t, srv, 0, "inside the clamped backoff")
		e.clk.Advance(6 * time.Hour)
		e.touch(id)
		a3aWantCalls(t, srv, 1, "once the clamped backoff is over")
	})
	t.Run("last_attempt_at in the future", func(t *testing.T) {
		t.Parallel()
		e, srv := a3aEnv(t)
		id := e.addOAuth("tok")
		now := e.clk.Now()
		if err := e.st.SaveQuotaPollState(store.QuotaPollState{AccountID: id, LastAttemptAt: now.Add(2 * time.Hour)}); err != nil {
			t.Fatalf("SaveQuotaPollState() error = %v", err)
		}

		e.touch(id)
		a3aWantCalls(t, srv, 0, "future attempt clamped to now: inside the TTL")
		e.clk.Advance(e.p.TTL())
		e.touch(id)
		a3aWantCalls(t, srv, 1, "TTL counted from the clamped attempt, not from +2h")
	})
	t.Run("clock moving backwards never shortens a backoff", func(t *testing.T) {
		t.Parallel()
		e, srv := a3aEnv(t)
		id := e.addOAuth("tok")
		srv.Set(pathAnthropic, respondStatus(503))
		e.touch(id)
		a3aWantCalls(t, srv, 1, "failing attempt")

		remaining := func() time.Duration {
			res := e.p.Peek(id)
			if res.RefreshBlocked != BlockBackoff || res.NextRefreshAt == nil {
				t.Fatalf("Peek = %q/%v, want backoff with a time", res.RefreshBlocked, res.NextRefreshAt)
			}
			return res.NextRefreshAt.Sub(e.clk.Now())
		}
		before := remaining()
		e.clk.Advance(-10 * time.Minute)
		if after := remaining(); after < before {
			t.Fatalf("remaining wait after a backwards jump = %v, want >= %v", after, before)
		}
		e.touch(id)
		e.refresh(id)
		a3aWantCalls(t, srv, 1, "backwards clock must not reopen the backoff")

		// A 6 h Retry-After rewound by an hour is capped at 6 h: not shorter.
		e2, srv2 := a3aEnv(t)
		id2 := e2.addOAuth("tok")
		srv2.Set(pathAnthropic, respondStatus(429, "Retry-After", "21600"))
		e2.touch(id2)
		res := e2.p.Peek(id2)
		long := res.NextRefreshAt.Sub(e2.clk.Now())
		e2.clk.Advance(-time.Hour)
		res = e2.p.Peek(id2)
		if got := res.NextRefreshAt.Sub(e2.clk.Now()); got < long {
			t.Fatalf("429 remaining wait after a backwards jump = %v, want >= %v", got, long)
		}
		e2.touch(id2)
		a3aWantCalls(t, srv2, 1, "429 backoff after a backwards jump")
	})
}
