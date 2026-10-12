package quota

import "time"

// The per-account snapshot cache lives on acct and holds the decoded Report
// of the latest quota_snapshots row, treated as immutable: every value handed
// out is a deep clone. Account ids are never reused and Peek returns early
// for a removed account, so a cached entry can never be served for a
// different account.

// cachedSnapshot returns the account's latest report and its PolledAt,
// filling the cache from the store on first use. A store read error is not
// cached (the next Peek retries); "no row" and an undecodable payload are
// cached as a negative result so a cold account costs one read, not one read
// per Peek. The returned Report is a clone; callers may mutate it freely.
//
// a.mu must NOT be held: the store read happens without it, matching today's
// Peek behaviour of doing snapshot I/O unlocked.
func (p *Poller) cachedSnapshot(id int64, a *acct) (rep Report, have bool, polledAt time.Time) {
	a.mu.Lock()
	if a.snapLoaded {
		defer a.mu.Unlock()
		return a.snapshotLocked()
	}
	a.mu.Unlock()

	p.snapshotLoads.Add(1)
	snap, ok, err := p.store.GetLatestQuotaSnapshot(id)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.snapLoaded { // filled by a concurrent Peek or write-through
		return a.snapshotLocked()
	}
	if err != nil {
		return Report{}, false, time.Time{}
	}
	a.snapLoaded = true
	if ok {
		if r, decoded := DecodeReport(snap.UsageData); decoded {
			a.snap, a.snapAt = &r, snap.PolledAt
		}
	}
	return a.snapshotLocked()
}

// snapshotLocked returns a clone of the cached entry. a.mu must be held.
func (a *acct) snapshotLocked() (rep Report, have bool, polledAt time.Time) {
	if a.snap == nil {
		return Report{}, false, time.Time{}
	}
	return cloneReport(*a.snap), true, a.snapAt
}

// setSnapshotLocked write-throughs a just-persisted report. a.mu must be held.
func (a *acct) setSnapshotLocked(rep Report, polledAt time.Time) {
	a.snap, a.snapAt, a.snapLoaded = &rep, polledAt, true
}

// invalidateSnapshotLocked drops the entry so the next Peek reloads from the
// store. a.mu must be held.
func (a *acct) invalidateSnapshotLocked() {
	a.snap, a.snapAt, a.snapLoaded = nil, time.Time{}, false
}

// cloneReport deep-copies every slice and pointer field so a cached Report is
// never shared with a caller.
func cloneReport(r Report) Report {
	if r.Windows != nil {
		ws := make([]Window, len(r.Windows))
		for i, w := range r.Windows {
			w.Scope = clonePtr(w.Scope)
			w.UsedPct = clonePtr(w.UsedPct)
			w.RemainingPct = clonePtr(w.RemainingPct)
			w.ResetsAt = clonePtr(w.ResetsAt)
			w.DurationSeconds = clonePtr(w.DurationSeconds)
			ws[i] = w
		}
		r.Windows = ws
	}
	if r.Balances != nil {
		bs := make([]Balance, len(r.Balances))
		for i, b := range r.Balances {
			b.Used = clonePtr(b.Used)
			b.Remaining = clonePtr(b.Remaining)
			b.Limit = clonePtr(b.Limit)
			bs[i] = b
		}
		r.Balances = bs
	}
	if r.Details != nil {
		r.Details = append([]Detail(nil), r.Details...)
	}
	r.Problem = clonePtr(r.Problem)
	r.OverageEnabled = clonePtr(r.OverageEnabled)
	return r
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
