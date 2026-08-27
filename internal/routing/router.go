// Package routing selects which account should handle a request for a given
// session, applying session affinity and per-policy account selection over
// internal/accounts and internal/quota. It does not serve HTTP.
package routing

import (
	"context"
	"fmt"
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

const (
	defaultRoutingTTL = 24 * time.Hour
	defaultQuotaTTL   = 5 * time.Minute
)

// Router selects an account for a session, honoring session affinity and the
// given routing policy.
type Router struct {
	store    *store.Store
	registry *accounts.Registry
	poller   *quota.Poller

	routingTTL time.Duration
	quotaTTL   time.Duration
}

// Option configures a Router constructed via New.
type Option func(*Router)

// WithRoutingTTL overrides the session affinity window for routing
// decisions. Defaults to 24h.
func WithRoutingTTL(ttl time.Duration) Option {
	return func(r *Router) { r.routingTTL = ttl }
}

// WithQuotaTTL overrides the TTL used when checking account quota via
// quota.Poller.Get. Defaults to 5m.
func WithQuotaTTL(ttl time.Duration) Option {
	return func(r *Router) { r.quotaTTL = ttl }
}

// New returns a Router backed by s, reg, and poller.
func New(s *store.Store, reg *accounts.Registry, poller *quota.Poller, opts ...Option) *Router {
	r := &Router{
		store:      s,
		registry:   reg,
		poller:     poller,
		routingTTL: defaultRoutingTTL,
		quotaTTL:   defaultQuotaTTL,
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
func (r *Router) Route(ctx context.Context, sessionKey string, policy string, candidateAccountIDs []int64) (accounts.Account, error) {
	decision, found, err := r.latestDecision(sessionKey)
	if err != nil {
		return accounts.Account{}, fmt.Errorf("latest routing decision for session %q: %w", sessionKey, err)
	}
	if found && time.Now().UTC().Before(decision.ExpiresAt) {
		exhausted, err := r.isExhausted(ctx, decision.AccountID)
		if err != nil {
			return accounts.Account{}, err
		}
		if !exhausted {
			return r.registry.Get(decision.AccountID)
		}
	}

	accountID, err := r.selectAccount(ctx, policy, candidateAccountIDs)
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
func (r *Router) selectAccount(ctx context.Context, policy string, candidateAccountIDs []int64) (int64, error) {
	switch policy {
	case PolicyHard:
		if len(candidateAccountIDs) != 1 {
			return 0, fmt.Errorf("routing: hard policy requires exactly one candidate account, got %d", len(candidateAccountIDs))
		}
		accountID := candidateAccountIDs[0]
		exhausted, err := r.isExhausted(ctx, accountID)
		if err != nil {
			return 0, err
		}
		if exhausted {
			return 0, fmt.Errorf("routing: account %d is exhausted; hard policy has no failover", accountID)
		}
		return accountID, nil

	case PolicyPool:
		for _, accountID := range candidateAccountIDs {
			exhausted, err := r.isExhausted(ctx, accountID)
			if err != nil {
				return 0, err
			}
			if !exhausted {
				return accountID, nil
			}
		}
		return 0, fmt.Errorf("routing: no non-exhausted candidate accounts for pool policy (%d checked)", len(candidateAccountIDs))

	default:
		return 0, fmt.Errorf("routing: unknown policy %q", policy)
	}
}

// isExhausted reports whether accountID has used up its known quota. An
// account with no known limit (TokensLimit == 0) is treated as available,
// since the absence of data shouldn't block routing.
func (r *Router) isExhausted(ctx context.Context, accountID int64) (bool, error) {
	usage, err := r.poller.Get(ctx, accountID, r.quotaTTL)
	if err != nil {
		return false, fmt.Errorf("check quota for account %d: %w", accountID, err)
	}
	return usage.TokensLimit > 0 && usage.TokensUsed >= usage.TokensLimit, nil
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
