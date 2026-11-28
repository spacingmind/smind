package quota

import (
	"context"
	"errors"
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
	r := &resumeRig{
		fetcher:  &fakeReportFetcher{},
		sleepHit: make(chan struct{}, 16),
		release:  make(chan struct{}),
		jits:     jits,
	}
	r.env = newEnv(t, func(*accounts.Registry, *fakeClock) ReportFetcher { return r.fetcher },
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

func Test_poller_expired_token_after_resume_is_not_an_expiry_error(t *testing.T) {
	t.Run("refresh succeeds", func(t *testing.T) {
		h := newFetcherHarness(t)
		h.refresher.cred = accounts.OAuthCredential{AccessToken: "access-new", RefreshToken: "refresh-new", ExpiresAt: time.Now().Add(time.Hour)}
		acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(-time.Minute))
		got, err := h.fetcher.Fetch(context.Background(), acc)
		if err != nil || got.Status != StatusAvailable {
			t.Fatalf("Fetch() = %+v, %v, want available", got, err)
		}
		if got.Problem != nil && got.Problem.Kind == ProblemExpired {
			t.Errorf("problem = %+v, must not be expired", got.Problem)
		}
		if n := h.refresher.calls.Load(); n != 1 {
			t.Errorf("refresher calls = %d, want 1", n)
		}
	})
	t.Run("refresh hits a network error", func(t *testing.T) {
		h := newFetcherHarness(t)
		h.refresher.err = errors.New("anthropic oauth refresh request: dial tcp: network is unreachable")
		acc := h.oauthAccount(t, "anthropic", "a", time.Now().Add(-time.Hour))
		got, err := h.fetcher.Fetch(context.Background(), acc)
		var fe *FetchError
		if !errors.As(err, &fe) || got.Status != StatusError {
			t.Fatalf("Fetch() = %+v, %v, want the transient error report", got, err)
		}
		if got.Problem != nil && got.Problem.Kind == ProblemExpired {
			t.Errorf("problem = %+v, must not be expired", got.Problem)
		}
		if n := h.requests.Load(); n != 0 {
			t.Errorf("usage requests = %d, want 0", n)
		}
	})
}
