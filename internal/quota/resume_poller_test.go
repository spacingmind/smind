package quota

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
)

// resumeRig is an env whose resume watch reads the fake wall clock and a
// test-driven monotonic counter, with a recording, blocking sleep.
type resumeRig struct {
	*env
	fetcher *fakeReportFetcher
	mono    time.Duration
	monoMu  sync.Mutex

	sleepMu  sync.Mutex
	sleeps   []time.Duration
	sleepHit chan struct{}
	release  chan struct{}

	jitMu sync.Mutex
	jits  []float64
}

func newResumeRig(t *testing.T, jits ...float64) *resumeRig {
	t.Helper()
	return newResumeRigMk(t, nil, jits...)
}

// newResumeRigMk is newResumeRig with a custom fetcher maker (nil = the fake).
func newResumeRigMk(t *testing.T, mk func(*accounts.Registry, *fakeClock) ReportFetcher, jits ...float64) *resumeRig {
	t.Helper()
	r := &resumeRig{
		fetcher:  &fakeReportFetcher{},
		sleepHit: make(chan struct{}, 16),
		release:  make(chan struct{}),
		jits:     jits,
	}
	if mk == nil {
		mk = func(*accounts.Registry, *fakeClock) ReportFetcher { return r.fetcher }
	}
	r.env = newEnv(t, mk,
		WithResumeClocks(func() time.Time { return r.clk.Now() }, func() time.Duration {
			r.monoMu.Lock()
			defer r.monoMu.Unlock()
			return r.mono
		}),
		WithJitter(func() float64 {
			r.jitMu.Lock()
			defer r.jitMu.Unlock()
			if len(r.jits) == 0 {
				return 0
			}
			j := r.jits[0]
			r.jits = r.jits[1:]
			return j
		}),
		WithSleep(func(ctx context.Context, d time.Duration) error {
			r.sleepMu.Lock()
			r.sleeps = append(r.sleeps, d)
			r.sleepMu.Unlock()
			r.sleepHit <- struct{}{}
			select {
			case <-r.release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	return r
}

func (r *resumeRig) advance(wall, mono time.Duration) {
	r.clk.Advance(wall)
	r.monoMu.Lock()
	r.mono += mono
	r.monoMu.Unlock()
}

// primeAndSleep observes once (no resume), then simulates a 10 minute sleep.
func (r *resumeRig) primeAndSleep() {
	r.touch(r.addOAuth("prime"))
	r.advance(10*time.Minute, time.Second)
}

func Test_poller_spreads_fetches_after_a_resume(t *testing.T) {
	r := newResumeRig(t, 0.1, 0.5, 0.9)
	r.primeAndSleep()
	if n := r.fetcher.Calls(); n != 1 {
		t.Fatalf("prime fetch calls = %d, want 1", n)
	}
	ids := []int64{r.addOAuth("a"), r.addOAuth("b"), r.addOAuth("c")}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() { defer wg.Done(); r.touch(id) }()
	}
	for range ids {
		select {
		case <-r.sleepHit:
		case <-time.After(5 * time.Second):
			t.Fatal("not every fetch asked to sleep")
		}
	}
	if n := r.fetcher.Calls(); n != 1 {
		t.Errorf("fetcher calls while sleeping = %d, want 1 (prime only)", n)
	}
	if n := len(r.p.sem); n != 0 {
		t.Errorf("in-flight slots held while sleeping = %d, want 0", n)
	}
	r.sleepMu.Lock()
	got := append([]time.Duration(nil), r.sleeps...)
	r.sleepMu.Unlock()
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	want := []time.Duration{3 * time.Second, 15 * time.Second, 27 * time.Second}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("sleeps = %v, want %v", got, want)
	}
	close(r.release)
	wg.Wait()
	if n := r.fetcher.Calls(); n != 4 {
		t.Errorf("total fetcher calls = %d, want 4 (1 prime + 3)", n)
	}
}

func Test_poller_forced_refresh_is_not_delayed_after_a_resume(t *testing.T) {
	r := newResumeRig(t, 0.5)
	r.primeAndSleep()
	id := r.addOAuth("a")
	r.refresh(id)
	r.sleepMu.Lock()
	n := len(r.sleeps)
	r.sleepMu.Unlock()
	if n != 0 {
		t.Errorf("sleep called %d times for a forced refresh, want 0", n)
	}
	if c := r.fetcher.Calls(); c != 2 {
		t.Errorf("fetcher calls = %d, want 2 (prime + forced)", c)
	}
}

// A token that expired while the machine slept is refreshed like any other and
// is never reported as "login expired": the poller, the real fetcher and a
// refresher that succeeds, through a resume. Provider calls are counted.
func Test_poller_expired_token_after_resume_is_not_an_expiry_error(t *testing.T) {
	srv := newUsageServer(t)
	ref := &fetcherRefresher{cred: accounts.OAuthCredential{AccessToken: "access-new", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(100000 * time.Hour)}}
	r := newResumeRigMk(t, func(reg *accounts.Registry, clk *fakeClock) ReportFetcher {
		src := newAnthropicSource(srv.srv.Client())
		src.url = srv.srv.URL + pathAnthropic
		return newAccountFetcher(reg, map[string]providerSpec{"anthropic": {source: src, refresher: ref}}, clk.Now)
	}, 0)
	close(r.release) // the spread wait ends at once
	r.touch(r.addOAuth("prime"))
	r.advance(10*time.Minute, time.Second)
	id := r.addOAuthExpiring("anthropic", "stale", time.Now().Add(-time.Minute))
	res := r.touch(id)
	if res.Report != nil && res.Report.Problem != nil && res.Report.Problem.Kind == ProblemExpired {
		t.Fatalf("report = %+v, must not be expired", res.Report.Problem)
	}
	if n := ref.calls.Load(); n != 1 {
		t.Errorf("refresher calls = %d, want 1", n)
	}
	if n := srv.Calls(); n != 2 {
		t.Errorf("usage calls = %d, want 2 (prime + the refreshed account)", n)
	}
	if st := r.state(id); st.ConsecutiveFailures != 0 {
		t.Errorf("consecutive failures = %d, want 0", st.ConsecutiveFailures)
	}
}

// A forced refresh that joins a flight already waiting in the spread is not
// delayed: it ends the wait.
func Test_poller_forced_refresh_joining_a_spread_flight_skips_the_wait(t *testing.T) {
	r := newResumeRig(t, 0.9)
	r.primeAndSleep()
	id := r.addOAuth("a")
	go r.touch(id) // non-forced: enters the spread wait
	select {
	case <-r.sleepHit:
	case <-time.After(5 * time.Second):
		t.Fatal("the touch never entered the spread wait")
	}
	done := make(chan struct{})
	go func() { r.refresh(id); close(done) }() // joins; never released by the test
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a forced refresh stayed blocked behind the spread wait")
	}
	if c := r.fetcher.Calls(); c != 2 {
		t.Errorf("fetcher calls = %d, want 2 (prime + the one flight)", c)
	}
}

// Closing the poller during the spread ends the wait, leaves no flight open
// and makes no provider call.
func Test_poller_close_during_the_spread_makes_no_call_and_leaves_no_flight(t *testing.T) {
	r := newResumeRig(t, 0.9)
	r.primeAndSleep()
	id := r.addOAuth("a")
	got := make(chan Result, 1)
	go func() { got <- r.touch(id) }()
	select {
	case <-r.sleepHit:
	case <-time.After(5 * time.Second):
		t.Fatal("the touch never entered the spread wait")
	}
	r.p.Close()
	select {
	case res := <-got:
		if res.InFlight {
			t.Errorf("touch returned in flight after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("touch still blocked after Close")
	}
	if c := r.fetcher.Calls(); c != 1 {
		t.Errorf("fetcher calls = %d, want 1 (prime only)", c)
	}
}

// Inside the TTL a second touch after a resume is a cache hit: no second wait,
// no second provider call.
func Test_poller_ttl_still_applies_after_a_resume(t *testing.T) {
	r := newResumeRig(t, 0.5, 0.5)
	close(r.release)
	r.primeAndSleep()
	id := r.addOAuth("a")
	r.touch(id)
	r.touch(id)
	if c := r.fetcher.Calls(); c != 2 {
		t.Errorf("fetcher calls = %d, want 2 (prime + one for the account)", c)
	}
}
