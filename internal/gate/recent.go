package gate

import (
	"sync"
	"time"
)

// RecentTTL is how long a failed run keeps a row gated (advisory).
const RecentTTL = time.Hour

type recentKey struct{ provider, model string }

type recentEntry struct {
	reason    string
	accountID int64
	at        time.Time
}

// Recent remembers, in memory only, that a run just failed with a human-fix
// class (auth_expired, model_not_in_plan), so the row gates right after the
// first failure instead of inviting the same failing send (D2). It is cleared
// per provider on a credential change and expires after RecentTTL.
type Recent struct {
	mu sync.Mutex
	m  map[recentKey]recentEntry
}

// NewRecent returns an empty Recent.
func NewRecent() *Recent { return &Recent{m: map[recentKey]recentEntry{}} }

// Mark records reason for (provider, model); model "" is the whole provider.
func (r *Recent) Mark(provider, model, reason string, accountID int64, now time.Time) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.m[recentKey{provider, model}] = recentEntry{reason: reason, accountID: accountID, at: now}
	r.mu.Unlock()
}

// Clear drops every mark of provider.
func (r *Recent) Clear(provider string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	for k := range r.m {
		if k.provider == provider {
			delete(r.m, k)
		}
	}
	r.mu.Unlock()
}

func (r *Recent) get(provider, model, reason string, now time.Time) (recentEntry, bool) {
	if r == nil {
		return recentEntry{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.m[recentKey{provider, model}]
	if !ok || e.reason != reason || now.Sub(e.at) > RecentTTL {
		return recentEntry{}, false
	}
	return e, true
}

// ClearAll drops every mark (a credential changed somewhere).
func (r *Recent) ClearAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.m = map[recentKey]recentEntry{}
	r.mu.Unlock()
}
