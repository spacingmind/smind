package routing

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

type fakeFetcher struct {
	mu    sync.Mutex
	usage map[int64]quota.Usage
	calls map[int64]int
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{usage: map[int64]quota.Usage{}, calls: map[int64]int{}}
}

func (f *fakeFetcher) Fetch(ctx context.Context, account store.Account) (quota.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[account.ID]++
	return f.usage[account.ID], nil
}

func (f *fakeFetcher) setAvailable(accountID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage[accountID] = quota.Usage{TokensUsed: 10, TokensLimit: 100}
}

func (f *fakeFetcher) setExhausted(accountID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage[accountID] = quota.Usage{TokensUsed: 100, TokensLimit: 100}
}

func newTestRouter(t *testing.T, opts ...Option) (*Router, *store.Store, *accounts.Registry, *fakeFetcher) {
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
	fetcher := newFakeFetcher()
	poller := quota.New(s, fetcher)
	r := New(s, reg, poller, opts...)
	return r, s, reg, fetcher
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
		setup   func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) (candidateIDs []int64, wantID int64)
		wantErr bool
	}{
		{
			name:   "hard policy success",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				fetcher.setAvailable(a)
				return []int64{a}, a
			},
		},
		{
			name:   "hard policy zero candidates errors",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				return nil, 0
			},
			wantErr: true,
		},
		{
			name:   "hard policy multiple candidates errors",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				fetcher.setAvailable(a)
				fetcher.setAvailable(b)
				return []int64{a, b}, 0
			},
			wantErr: true,
		},
		{
			name:   "hard policy exhausted account errors with no failover",
			policy: PolicyHard,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				fetcher.setExhausted(a)
				return []int64{a}, 0
			},
			wantErr: true,
		},
		{
			name:   "pool policy picks first non-exhausted candidate",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				fetcher.setAvailable(a)
				fetcher.setAvailable(b)
				return []int64{a, b}, a
			},
		},
		{
			name:   "pool policy fails over past exhausted candidates",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				c := newAccount(t, reg, "c")
				fetcher.setExhausted(a)
				fetcher.setExhausted(b)
				fetcher.setAvailable(c)
				return []int64{a, b, c}, c
			},
		},
		{
			name:   "pool policy all exhausted errors",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				b := newAccount(t, reg, "b")
				fetcher.setExhausted(a)
				fetcher.setExhausted(b)
				return []int64{a, b}, 0
			},
			wantErr: true,
		},
		{
			name:   "pool policy empty candidates errors",
			policy: PolicyPool,
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				return nil, 0
			},
			wantErr: true,
		},
		{
			name:   "unknown policy errors",
			policy: "bogus",
			setup: func(t *testing.T, reg *accounts.Registry, fetcher *fakeFetcher) ([]int64, int64) {
				a := newAccount(t, reg, "a")
				fetcher.setAvailable(a)
				return []int64{a}, 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, s, reg, fetcher := newTestRouter(t, WithQuotaTTL(0))
			candidateIDs, wantID := tt.setup(t, reg, fetcher)

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

	r, s, reg, fetcher := newTestRouter(t, WithQuotaTTL(0))
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fetcher.setAvailable(a)
	fetcher.setAvailable(b)

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

	r, s, reg, fetcher := newTestRouter(t, WithQuotaTTL(0), WithRoutingTTL(20*time.Millisecond))
	a := newAccount(t, reg, "a")
	fetcher.setAvailable(a)

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

func TestRouter_Route_SessionAffinityBrokenByExhaustion(t *testing.T) {
	t.Parallel()

	r, s, reg, fetcher := newTestRouter(t, WithQuotaTTL(0))
	a := newAccount(t, reg, "a")
	b := newAccount(t, reg, "b")
	fetcher.setAvailable(a)
	fetcher.setAvailable(b)

	ctx := context.Background()
	first, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil {
		t.Fatalf("first Route() error = %v", err)
	}
	if first.ID != a {
		t.Fatalf("first Route() account = %d, want %d", first.ID, a)
	}

	fetcher.setExhausted(a)

	second, err := r.Route(ctx, "sess", PolicyPool, []int64{a, b})
	if err != nil {
		t.Fatalf("second Route() error = %v", err)
	}
	if second.ID != b {
		t.Errorf("second Route() account = %d, want %d (should fail over once affinity account is exhausted)", second.ID, b)
	}

	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 2 {
		t.Errorf("routing decisions = %d, want 2 (exhaustion should re-decide)", len(decisions))
	}
}

func TestRouter_Route_ConcurrentDifferentSessions(t *testing.T) {
	t.Parallel()

	r, _, reg, fetcher := newTestRouter(t, WithQuotaTTL(0))
	a := newAccount(t, reg, "a")
	fetcher.setAvailable(a)

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
