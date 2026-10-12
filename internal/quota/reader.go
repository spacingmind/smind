package quota

import "time"

// BlockReason says why a forced Refresh made now would not reach the network.
type BlockReason string

const (
	BlockFloor    BlockReason = "floor"    // under the 30 s per-account floor
	BlockBudget   BlockReason = "budget"   // 6 forced attempts already in the last hour
	BlockBackoff  BlockReason = "backoff"  // failure backoff / Retry-After in force
	BlockRejected BlockReason = "rejected" // credential rejected, unchanged since
)

// Result is what every poller entry point returns. It never carries an error:
// a failed fetch is the last stored report marked Stale, with LastError.
type Result struct {
	AccountID int64
	// Enabled is false when accountUsage.enabled is false.
	Enabled bool
	// Report is the latest stored report; nil means "no report" (cold,
	// legacy or too-new payload, or disabled).
	Report *Report
	// Stale: the report is older than the TTL, or the last attempt failed,
	// or this call was a refused Refresh.
	Stale bool
	// LastError is the fixed-vocabulary error of the last failed attempt.
	LastError string
	// NextRefreshAt is the earliest time a forced Refresh could reach the
	// network; nil when it never will (rejected) or when disabled.
	NextRefreshAt *time.Time
	// RefreshBlocked is why a Refresh made now would not reach the network,
	// "" if it would.
	RefreshBlocked BlockReason
	// InFlight: a network fetch for this account is in progress, or the
	// caller's context ended while it was.
	InFlight bool
}

// Reader is the read-only view of the poller handed to routing, the proxy and
// the event path. It has no method that can reach the network, so no fetch
// path exists from those callers at compile time.
type Reader interface {
	// Peek returns what the cache holds. It never makes a request and never
	// starts a fetch.
	Peek(accountID int64) Result
	// TTL is the cache TTL the poller enforces (never under 5 minutes).
	// Routing uses it to age out an exhausted window that has no reset time.
	TTL() time.Duration
}

var _ Reader = (*Poller)(nil)
