package quota

import (
	"context"
	"path/filepath"
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
