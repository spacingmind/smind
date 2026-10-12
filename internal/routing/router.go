// Package routing selects which account should handle a request for a given
// session, applying session affinity and per-policy account selection over
// internal/accounts and internal/quota. It does not serve HTTP.
package routing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

// Policy names accepted by Route.
const (
	// PolicyHard routes to a single, fixed account with no failover: if that
	// account is exhausted, routing fails rather than falling back.
	PolicyHard = "hard"
	// PolicyPool fills from an ordered list of candidate accounts, routing to
	// the first one that isn't exhausted.
	PolicyPool = "pool"
)

const defaultRoutingTTL = 24 * time.Hour

// Router selects an account for a session, honoring session affinity and the
// given routing policy.
type Router struct {
	store    *store.Store
	registry *accounts.Registry
	reader   quota.Reader

	routingTTL time.Duration

	usageRouting bool
	thresholdPct int
	clock        func() time.Time
}

// RouteOption adjusts a single Route call.
type RouteOption func(*routeConfig)

type routeConfig struct {
	model string
}

// WithModel tells Route which model the request is for, so model-scoped
// usage windows (e.g. weekly Opus) count only for matching requests. Without
// it only account-wide windows count.
func WithModel(model string) RouteOption {
	return func(c *routeConfig) { c.model = model }
}

// Option configures a Router constructed via New.
type Option func(*Router)

// WithRoutingTTL overrides the session affinity window for routing
// decisions. Defaults to 24h.
func WithRoutingTTL(ttl time.Duration) Option {
	return func(r *Router) { r.routingTTL = ttl }
}

// WithUsageRouting toggles exhaustion-aware routing on cached usage windows.
// Defaults to true; when false the Reader is never consulted.
func WithUsageRouting(enabled bool) Option {
	return func(r *Router) { r.usageRouting = enabled }
}

// WithExhaustThresholdPct sets the used percentage at or above which a window
// counts as full. Defaults to 100; a value outside 1..100 is ignored, so a
// bad config can never make every account look exhausted.
func WithExhaustThresholdPct(pct int) Option {
	return func(r *Router) {
		if pct >= 1 && pct <= 100 {
			r.thresholdPct = pct
		}
	}
}

// WithClock overrides the clock used to compare window reset times.
func WithClock(now func() time.Time) Option {
	return func(r *Router) { r.clock = now }
}

// New returns a Router backed by s and reg. reader is the quota read view
// consulted for exhaustion (ADR-0028 §4). It holds a quota.Reader, which has
// no fetch method, so routing can never reach a usage endpoint.
func New(s *store.Store, reg *accounts.Registry, reader quota.Reader, opts ...Option) *Router {
	r := &Router{
		store:      s,
		registry:   reg,
		reader:     reader,
		routingTTL: defaultRoutingTTL,

		usageRouting: true,
		thresholdPct: 100,
		clock:        time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Route returns the account that should handle a request for sessionKey.
//
// If a routing decision for sessionKey exists, hasn't expired, and its
// account isn't currently exhausted, that account is reused (session
// affinity). Otherwise a new account is selected from candidateAccountIDs
// per policy, persisted as the new decision, and returned.
//
// Exhaustion is judged from the cached usage windows only; Route never
// fetches and never fails because usage data is missing or stale.
func (r *Router) Route(ctx context.Context, sessionKey string, policy string, candidateAccountIDs []int64, opts ...RouteOption) (accounts.Account, error) {
	var rc routeConfig
	for _, opt := range opts {
		opt(&rc)
	}
	decision, found, err := r.latestDecision(sessionKey)
	if err != nil {
		return accounts.Account{}, fmt.Errorf("latest routing decision for session %q: %w", sessionKey, err)
	}
	// Affinity only holds while the pinned account is still a candidate:
	// model-aware routing (ADR-0020 §5) narrows candidates per request, and
	// a pin to an account outside that set (e.g. a glm-* account for a
	// claude-* request) must not override it.
	if found && time.Now().UTC().Before(decision.ExpiresAt) && slices.Contains(candidateAccountIDs, decision.AccountID) {
		if exhausted, _ := r.exhaustion(decision.AccountID, rc.model, r.clock()); !exhausted {
			return r.registry.Get(decision.AccountID)
		}
	}

	accountID, err := r.selectAccount(policy, candidateAccountIDs, rc.model)
	if err != nil {
		return accounts.Account{}, err
	}

	now := time.Now().UTC()
	if _, err := r.store.CreateRoutingDecision(store.RoutingDecision{
		SessionKey: sessionKey,
		AccountID:  accountID,
		Policy:     policy,
		DecidedAt:  now,
		ExpiresAt:  now.Add(r.routingTTL),
	}); err != nil {
		return accounts.Account{}, fmt.Errorf("persist routing decision for session %q: %w", sessionKey, err)
	}

	return r.registry.Get(accountID)
}

// selectAccount picks a new account for policy from candidateAccountIDs,
// without consulting or persisting any routing decision.
func (r *Router) selectAccount(policy string, candidateAccountIDs []int64, model string) (int64, error) {
	now := r.clock()
	switch policy {
	case PolicyHard:
		if len(candidateAccountIDs) != 1 {
			return 0, fmt.Errorf("routing: hard policy requires exactly one candidate account, got %d", len(candidateAccountIDs))
		}
		accountID := candidateAccountIDs[0]
		if exhausted, until := r.exhaustion(accountID, model, now); exhausted {
			return 0, fmt.Errorf("routing: account %d is exhausted until %s; hard policy has no failover", accountID, until.UTC().Format(time.RFC3339))
		}
		return accountID, nil

	case PolicyPool:
		var earliest time.Time
		for _, accountID := range candidateAccountIDs {
			exhausted, until := r.exhaustion(accountID, model, now)
			if !exhausted {
				return accountID, nil
			}
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
		}
		if len(candidateAccountIDs) == 0 {
			return 0, errors.New("routing: no non-exhausted candidate accounts for pool policy (0 checked)")
		}
		return 0, fmt.Errorf("routing: all %d candidate accounts are exhausted; earliest reset %s", len(candidateAccountIDs), earliest.UTC().Format(time.RFC3339))

	default:
		return 0, fmt.Errorf("routing: unknown policy %q", policy)
	}
}

// exhaustion reports whether accountID cannot serve model right now according
// to the cached usage report, and if so the time it becomes usable again (the
// latest reset among the windows that exhaust it). It only reads the cache:
// anything unknown (cold, error, unavailable, legacy, disabled) is not
// exhausted.
func (r *Router) exhaustion(accountID int64, model string, now time.Time) (exhausted bool, until time.Time) {
	if !r.usageRouting {
		return false, time.Time{}
	}
	res := r.reader.Peek(accountID)
	rep := res.Report
	if !res.Enabled || rep == nil || rep.Status != quota.StatusAvailable {
		return false, time.Time{}
	}
	// Extra usage lets the account keep serving past its windows (ADR-0028 D9).
	if rep.OverageEnabled != nil && *rep.OverageEnabled {
		return false, time.Time{}
	}
	threshold := float64(r.thresholdPct)
	for _, w := range rep.Windows {
		if !w.AppliesTo(model) {
			continue
		}
		full := (w.UsedPct != nil && *w.UsedPct >= threshold) || (w.RemainingPct != nil && *w.RemainingPct <= 0)
		if !full {
			continue
		}
		var reset time.Time
		if w.ResetsAt != nil {
			// A passed reset means the window rolled over; trust that
			// without refetching.
			if !now.Before(*w.ResetsAt) {
				continue
			}
			reset = *w.ResetsAt
		} else {
			reset = rep.FetchedAt.Add(2 * r.reader.TTL())
			if now.After(reset) {
				continue
			}
		}
		exhausted = true
		if reset.After(until) {
			until = reset
		}
	}
	return exhausted, until
}

// latestDecision returns the most recent routing decision for sessionKey.
// store only exposes ListRoutingDecisions (all sessions, ordered by id), so
// the latest decision for one session is found by scanning for the highest
// DecidedAt among matching rows — mirrors quota.Poller.latestSnapshot.
func (r *Router) latestDecision(sessionKey string) (store.RoutingDecision, bool, error) {
	decisions, err := r.store.ListRoutingDecisions()
	if err != nil {
		return store.RoutingDecision{}, false, err
	}

	var latest store.RoutingDecision
	found := false
	for _, d := range decisions {
		if d.SessionKey != sessionKey {
			continue
		}
		if !found || d.DecidedAt.After(latest.DecidedAt) {
			latest = d
			found = true
		}
	}
	return latest, found, nil
}
