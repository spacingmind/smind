package quota

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type fakeFetcher struct {
	calls int32
	usage Usage
	err   error
}

func (f *fakeFetcher) Fetch(ctx context.Context, account store.Account) (Usage, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return Usage{}, f.err
	}
	return f.usage, nil
}

func (f *fakeFetcher) callCount() int {
	return int(atomic.LoadInt32(&f.calls))
}

func newTestStore(t *testing.T) *store.Store {
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
	return s
}

func newTestAccount(t *testing.T, s *store.Store) store.Account {
	t.Helper()
	a, err := s.CreateAccount(store.Account{
		Provider:       "anthropic",
		Label:          "personal",
		CredentialType: "oauth",
		CredentialData: "refresh-token-abc",
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return a
}

func TestPoller_Get(t *testing.T) {
	t.Parallel()

	freshUsage := Usage{TokensUsed: 100, TokensLimit: 1000, ResetsAt: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)}
	cachedUsage := Usage{TokensUsed: 500, TokensLimit: 1000, ResetsAt: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)}

	tests := []struct {
		name          string
		seedSnapshot  func(now time.Time) *store.QuotaSnapshot // nil = no snapshot seeded
		fetcher       *fakeFetcher
		wantUsage     Usage
		wantErr       bool
		wantFetchCall int
	}{
		{
			name: "cache hit returns cached usage without fetching",
			seedSnapshot: func(now time.Time) *store.QuotaSnapshot {
				data, _ := cachedUsage.Marshal()
				return &store.QuotaSnapshot{
					UsageData: data,
					PolledAt:  now,
					ExpiresAt: now.Add(5 * time.Minute),
				}
			},
			fetcher:       &fakeFetcher{usage: freshUsage},
			wantUsage:     cachedUsage,
			wantFetchCall: 0,
		},
		{
			name:          "cache miss fetches and persists",
			seedSnapshot:  nil,
			fetcher:       &fakeFetcher{usage: freshUsage},
			wantUsage:     freshUsage,
			wantFetchCall: 1,
		},
		{
			name: "cache expired fetches again",
			seedSnapshot: func(now time.Time) *store.QuotaSnapshot {
				data, _ := cachedUsage.Marshal()
				return &store.QuotaSnapshot{
					UsageData: data,
					PolledAt:  now.Add(-time.Hour),
					ExpiresAt: now.Add(-time.Minute),
				}
			},
			fetcher:       &fakeFetcher{usage: freshUsage},
			wantUsage:     freshUsage,
			wantFetchCall: 1,
		},
		{
			name:          "fetch error propagates and writes nothing",
			seedSnapshot:  nil,
			fetcher:       &fakeFetcher{err: context.DeadlineExceeded},
			wantErr:       true,
			wantFetchCall: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			account := newTestAccount(t, s)

			if tt.seedSnapshot != nil {
				snap := tt.seedSnapshot(time.Now().UTC())
				snap.AccountID = account.ID
				if _, err := s.CreateQuotaSnapshot(*snap); err != nil {
					t.Fatalf("CreateQuotaSnapshot() error = %v", err)
				}
			}

			beforeSnapshots, err := s.ListQuotaSnapshots()
			if err != nil {
				t.Fatalf("ListQuotaSnapshots() error = %v", err)
			}

			p := New(s, tt.fetcher)
			got, err := p.Get(context.Background(), account.ID, 5*time.Minute)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Get() error = nil, want error")
				}
			} else if err != nil {
				t.Fatalf("Get() error = %v", err)
			}

			if tt.fetcher.callCount() != tt.wantFetchCall {
				t.Errorf("Fetch call count = %d, want %d", tt.fetcher.callCount(), tt.wantFetchCall)
			}

			if !tt.wantErr {
				if got.TokensUsed != tt.wantUsage.TokensUsed || got.TokensLimit != tt.wantUsage.TokensLimit || !got.ResetsAt.Equal(tt.wantUsage.ResetsAt) {
					t.Errorf("Get() = %+v, want %+v", got, tt.wantUsage)
				}
			}

			afterSnapshots, err := s.ListQuotaSnapshots()
			if err != nil {
				t.Fatalf("ListQuotaSnapshots() error = %v", err)
			}

			switch {
			case tt.wantErr:
				if len(afterSnapshots) != len(beforeSnapshots) {
					t.Errorf("snapshot count after error = %d, want unchanged %d", len(afterSnapshots), len(beforeSnapshots))
				}
			case tt.wantFetchCall == 1:
				if len(afterSnapshots) != len(beforeSnapshots)+1 {
					t.Errorf("snapshot count after fetch = %d, want %d", len(afterSnapshots), len(beforeSnapshots)+1)
				}
			default:
				if len(afterSnapshots) != len(beforeSnapshots) {
					t.Errorf("snapshot count after cache hit = %d, want unchanged %d", len(afterSnapshots), len(beforeSnapshots))
				}
			}
		})
	}
}

func TestUsage_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	u := Usage{TokensUsed: 42, TokensLimit: 100, ResetsAt: time.Now().UTC().Truncate(time.Second)}

	data, err := u.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.TokensUsed != u.TokensUsed || got.TokensLimit != u.TokensLimit || !got.ResetsAt.Equal(u.ResetsAt) {
		t.Errorf("Unmarshal(Marshal(u)) = %+v, want %+v", got, u)
	}
}

func TestUsage_UnmarshalMalformedData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "malformed json", data: `{"tokens_used":`},
		{name: "wrong resets_at type", data: `{"tokens_used":1,"tokens_limit":2,"resets_at":false}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Unmarshal(tt.data); err == nil {
				t.Fatalf("Unmarshal(%q) error = nil, want error", tt.data)
			}
		})
	}
}

func TestPoller_GetReturnsCachedUnmarshalError(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account := newTestAccount(t, s)
	fetcher := &fakeFetcher{usage: Usage{TokensUsed: 1, TokensLimit: 10}}

	if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{
		AccountID: account.ID,
		UsageData: `{"tokens_used":`,
		PolledAt:  time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	_, err := New(s, fetcher).Get(context.Background(), account.ID, time.Minute)
	if err == nil {
		t.Fatalf("Get() error = nil, want cached unmarshal error")
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("Fetch call count = %d, want 0", fetcher.callCount())
	}
}

func TestPoller_GetZeroOrNegativeTTLDoesNotHitCacheAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{name: "zero", ttl: 0},
		{name: "negative", ttl: -time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			account := newTestAccount(t, s)
			fetcher := &fakeFetcher{usage: Usage{TokensUsed: 7, TokensLimit: 100}}
			p := New(s, fetcher)

			if _, err := p.Get(context.Background(), account.ID, tt.ttl); err != nil {
				t.Fatalf("first Get() error = %v", err)
			}
			if _, err := p.Get(context.Background(), account.ID, tt.ttl); err != nil {
				t.Fatalf("second Get() error = %v", err)
			}
			if fetcher.callCount() != 2 {
				t.Fatalf("Fetch call count = %d, want 2", fetcher.callCount())
			}
		})
	}
}

func TestPoller_GetCachesZeroValueUsage(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account := newTestAccount(t, s)
	fetcher := &fakeFetcher{usage: Usage{}}
	p := New(s, fetcher)

	got, err := p.Get(context.Background(), account.ID, time.Minute)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if got != (Usage{}) {
		t.Fatalf("first Get() = %+v, want zero-value Usage", got)
	}

	got, err = p.Get(context.Background(), account.ID, time.Minute)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if got != (Usage{}) {
		t.Fatalf("second Get() = %+v, want zero-value Usage", got)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("Fetch call count = %d, want 1", fetcher.callCount())
	}
}

func TestPoller_GetUsesLatestSnapshotForAccountByPolledAt(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account := newTestAccount(t, s)
	other := newTestAccount(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	oldUsage := Usage{TokensUsed: 1, TokensLimit: 100, ResetsAt: now.Add(time.Hour)}
	newUsage := Usage{TokensUsed: 2, TokensLimit: 100, ResetsAt: now.Add(2 * time.Hour)}
	otherUsage := Usage{TokensUsed: 99, TokensLimit: 100, ResetsAt: now.Add(3 * time.Hour)}

	for _, snap := range []struct {
		accountID int64
		usage     Usage
		polledAt  time.Time
	}{
		{accountID: account.ID, usage: newUsage, polledAt: now.Add(-time.Minute)},
		{accountID: other.ID, usage: otherUsage, polledAt: now},
		{accountID: account.ID, usage: oldUsage, polledAt: now.Add(-time.Hour)},
	} {
		data, err := snap.usage.Marshal()
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{
			AccountID: snap.accountID,
			UsageData: data,
			PolledAt:  snap.polledAt,
			ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateQuotaSnapshot() error = %v", err)
		}
	}

	fetcher := &fakeFetcher{usage: Usage{TokensUsed: 1000, TokensLimit: 1000}}
	got, err := New(s, fetcher).Get(context.Background(), account.ID, time.Minute)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.TokensUsed != newUsage.TokensUsed || got.TokensLimit != newUsage.TokensLimit || !got.ResetsAt.Equal(newUsage.ResetsAt) {
		t.Fatalf("Get() = %+v, want %+v", got, newUsage)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("Fetch call count = %d, want 0", fetcher.callCount())
	}
}

type blockingFetcher struct {
	calls   int32
	usage   Usage
	started chan struct{}
	release chan struct{}
}

func (f *blockingFetcher) Fetch(ctx context.Context, account store.Account) (Usage, error) {
	atomic.AddInt32(&f.calls, 1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return Usage{}, ctx.Err()
	case <-f.release:
		return f.usage, nil
	}
}

func (f *blockingFetcher) callCount() int {
	return int(atomic.LoadInt32(&f.calls))
}

func TestPoller_GetConcurrentExpiredCacheMisses(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account := newTestAccount(t, s)
	cachedUsage := Usage{TokensUsed: 1, TokensLimit: 100}
	data, err := cachedUsage.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{
		AccountID: account.ID,
		UsageData: data,
		PolledAt:  time.Now().UTC().Add(-time.Hour),
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	const workers = 2
	fetcher := &blockingFetcher{
		usage:   Usage{TokensUsed: 5, TokensLimit: 100},
		started: make(chan struct{}, workers),
		release: make(chan struct{}),
	}
	p := New(s, fetcher)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := p.Get(context.Background(), account.ID, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if got.TokensUsed != fetcher.usage.TokensUsed || got.TokensLimit != fetcher.usage.TokensLimit {
				errs <- errors.New("Get returned unexpected usage")
			}
		}()
	}

	deadline := time.After(2 * time.Second)
	for started := 0; started < workers; started++ {
		select {
		case <-fetcher.started:
		case <-deadline:
			t.Fatalf("timed out waiting for concurrent fetches; started %d of %d", started, workers)
		}
	}
	close(fetcher.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Get() error = %v", err)
		}
	}

	if fetcher.callCount() != workers {
		t.Fatalf("Fetch call count = %d, want %d simultaneous cache misses to fetch independently", fetcher.callCount(), workers)
	}
	snapshots, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(snapshots) != workers+1 {
		t.Fatalf("snapshot count = %d, want %d", len(snapshots), workers+1)
	}
}
