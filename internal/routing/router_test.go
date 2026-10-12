package routing

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

// neverFetcher is the ReportFetcher behind the routing tests' poller: it must
// never be called. WithEnabled(false) means the poller never reaches it, so
// any call is a shim bug.
type neverFetcher struct {
	t *testing.T
}

func (f neverFetcher) Fetch(ctx context.Context, account accounts.Account) (quota.Report, error) {
	f.t.Errorf("Fetch(%d) called, want zero fetches under the shim", account.ID)
	return quota.Report{}, errors.New("unreachable")
}

// newShimPoller builds the disabled poller every routing test routes over:
// enabled=false, so nothing is fetched, so Peek reports "no data" and
// nothing is exhausted.
func newShimPoller(t *testing.T, s *store.Store, reg *accounts.Registry) *quota.Poller {
	t.Helper()
	return quota.New(s, reg, neverFetcher{t: t}, quota.WithEnabled(false))
}

func newTestRouter(t *testing.T, opts ...Option) (*Router, *store.Store, *accounts.Registry) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	reg := accounts.New(s)
	poller := newShimPoller(t, s, reg)
	t.Cleanup(poller.Close)
	r := New(s, reg, poller, opts...)
	return r, s, reg
}

func newAccount(t *testing.T, reg *accounts.Registry, label string) int64 {
	t.Helper()
	a, err := reg.AddAPIKey("anthropic", label, "sk-test-"+label)
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	return a.ID
}

func TestRouter_Route_PolicySelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  string
		setup   func(t *testing.T, reg *accounts.Registry, fr *fakeReader) (candidateIDs []int64, wantID int64)
		wantErr bool
	}{
		{
			name:   "hard policy success",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				return []int64{a}, a
			},
		},
		{
			name:   "hard policy zero candidates errors",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				return nil, 0
			},
			wantErr: true,
		},
		{
			name:   "hard policy multiple candidates errors",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				return []int64{a, b}, 0
			},
			wantErr: true,
		},
		{
			name:   "hard policy exhausted account errors with no failover",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
				return []int64{a}, 0
			},
			wantErr: true,
		},
		{
			name:   "pool policy picks first non-exhausted candidate",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				return []int64{a, b}, a
			},
		},
		{
			name:   "pool policy fails over past exhausted candidates",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				c := newAccount(t, reg, "c")
				fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
				fr.set(b, exhaustedResult(testNow.Add(2*time.Hour)))
				return []int64{a, b, c}, c
			},
		},
		{
			name:   "pool policy all exhausted errors",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				fr.set(a, exhaustedResult(testNow.Add(time.Hour)))
				fr.set(b, exhaustedResult(testNow.Add(2*time.Hour)))
				return []int64{a, b}, 0
			},
			wantErr: true,
		},
		{
			name:   "pool policy empty candidates errors",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				return nil, 0
			},
			wantErr: true,
		},
		{
			name:   "unknown policy errors",
			policy: "bogus",
			setup: func(t *testing.T, reg *accounts.Registry, fr *fakeReader) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				return []int64{a}, 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, s, reg, fr, _ := newFakeRouter(t)
			candidateIDs, wantID := tt.setup(t, reg, fr)

			got, err := r.Route(context.Background(), "session-"+tt.name, tt.policy, candidateIDs)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Route() error = nil, want error")
				}
				decisions, lerr := s.ListRoutingDecisions()
				if lerr != nil {
					t.Fatalf("ListRoutingDecisions() error = %v", lerr)
				}
				if len(decisions) != 0 {
					t.Errorf("routing decisions after error = %d, want 0", len(decisions))
				}
				return
			}
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if got.ID != wantID {
				t.Errorf("Route() account ID = %d, want %d", got.ID, wantID)
			}

			decisions, err := s.ListRoutingDecisions()
			if err != nil {
				t.Fatalf("ListRoutingDecisions() error = %v", err)
			}
			if len(decisions) != 1 {
				t.Errorf("routing decisions = %d, want 1", len(decisions))
			}
		})
	}
}

func TestRouter_Route_SessionAffinityHolds(t *testing.T) {
	t.Parallel()

	r, s, reg := newTestRouter(t)
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")

	ctx := context.Background()
	first, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	second, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil {
		t.Fatalf("second Route() error = %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("account changed across calls: %d then %d, want session affinity", first.ID, second.ID)
	}

	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Errorf("routing decisions = %d, want 1 (affinity should not re-decide)", len(decisions))
	}
}

func TestRouter_Route_SessionAffinityExpires(t *testing.T) {
	t.Parallel()

	r, s, reg := newTestRouter(t, WithRoutingTTL(20*time.Millisecond))
	a := newAccount(t, reg, "a")

	ctx := context.Background()
	if _, err := r.Route(ctx, "sess", PolicyHard, []int64{a}); err != nil {
		t.Fatalf("first Route() error = %v", err)
	}

	time.Sleep(40 * time.Millisecond)

	if _, err := r.Route(ctx, "sess", PolicyHard, []int64{a}); err != nil {
		t.Fatalf("second Route() error = %v", err)
	}

	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 2 {
		t.Errorf("routing decisions = %d, want 2 (expiry should re-decide)", len(decisions))
	}
}

func TestRouter_Route_ConcurrentDifferentSessions(t *testing.T) {
	t.Parallel()

	r, _, reg := newTestRouter(t)
	a := newAccount(t, reg, "a")

	const sessions = 8
	var wg sync.WaitGroup
	errs := make(chan error, sessions)
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := r.Route(context.Background(), fmt.Sprintf("sess-%d", i), PolicyHard, []int64{a}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Route() error = %v", err)
	}
}

// TestRouter_Route_UnknownQuotaFailsOpen: with no report at all (a disabled
// poller), routing still succeeds.
func TestRouter_Route_UnknownQuotaFailsOpen(t *testing.T) {
	t.Parallel()

	r, _, reg := newTestRouter(t)
	a := newAccount(t, reg, "a")

	got, err := r.Route(context.Background(), "sess", PolicyHard, []int64{a})
	if err != nil {
		t.Fatalf("Route() error = %v, want fail-open success for unknown quota", err)
	}
	if got.ID != a {
		t.Errorf("Route() account = %d, want %d", got.ID, a)
	}
}

func TestRouter_Route_ConcurrentSameSession(t *testing.T) {
	t.Parallel()

	r, _, reg := newTestRouter(t)
	a := newAccount(t, reg, "a")

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan accounts.Account, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := r.Route(context.Background(), "shared-session", PolicyHard, []int64{a})
			if err != nil {
				errs <- err
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Errorf("concurrent Route() error = %v", err)
	}
	for got := range results {
		if got.ID != a {
			t.Errorf("Route() account = %d, want %d (only candidate)", got.ID, a)
		}
	}
}
