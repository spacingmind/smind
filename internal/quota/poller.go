package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Poller serves account usage from a TTL-cached quota_snapshots table,
// falling back to Fetcher when the cache is empty or expired.
type Poller struct {
	store   *store.Store
	fetcher Fetcher
}

// New returns a Poller backed by s, calling fetcher on cache misses.
func New(s *store.Store, fetcher Fetcher) *Poller {
	return &Poller{store: s, fetcher: fetcher}
}

// Get returns cached usage for accountID if a quota_snapshot exists and
// hasn't expired; otherwise it fetches fresh usage, persists it with the
// given ttl, and returns it.
func (p *Poller) Get(ctx context.Context, accountID int64, ttl time.Duration) (Usage, error) {
	snapshot, found, err := p.latestSnapshot(accountID)
	if err != nil {
		return Usage{}, fmt.Errorf("latest quota snapshot: %w", err)
	}
	if found && time.Now().UTC().Before(snapshot.ExpiresAt) {
		return Unmarshal(snapshot.UsageData)
	}

	account, err := p.store.GetAccount(accountID)
	if err != nil {
		return Usage{}, fmt.Errorf("get account %d: %w", accountID, err)
	}

	usage, err := p.fetcher.Fetch(ctx, account)
	if err != nil {
		return Usage{}, fmt.Errorf("fetch usage for account %d: %w", accountID, err)
	}

	usageData, err := usage.Marshal()
	if err != nil {
		return Usage{}, fmt.Errorf("marshal usage: %w", err)
	}

	now := time.Now().UTC()
	_, err = p.store.CreateQuotaSnapshot(store.QuotaSnapshot{
		AccountID: accountID,
		UsageData: usageData,
		PolledAt:  now,
		ExpiresAt: now.Add(ttl),
	})
	if err != nil {
		return Usage{}, fmt.Errorf("persist quota snapshot: %w", err)
	}

	return usage, nil
}

// latestSnapshot returns the most recently polled quota snapshot for
// accountID. store only exposes ListQuotaSnapshots (all accounts, ordered by
// id), so the latest snapshot for one account is found by scanning for the
// highest PolledAt among matching rows.
func (p *Poller) latestSnapshot(accountID int64) (store.QuotaSnapshot, bool, error) {
	snapshots, err := p.store.ListQuotaSnapshots()
	if err != nil {
		return store.QuotaSnapshot{}, false, err
	}

	var latest store.QuotaSnapshot
	found := false
	for _, s := range snapshots {
		if s.AccountID != accountID {
			continue
		}
		if !found || s.PolledAt.After(latest.PolledAt) {
			latest = s
			found = true
		}
	}
	return latest, found, nil
}
