// Package routing selects which account should handle a request for a given
// session, applying session affinity and per-policy account selection over
// internal/accounts and internal/quota. It does not serve HTTP.
package routing

import (
	"context"
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
}

// Option configures a Router constructed via New.
type Option func(*Router)

// WithRoutingTTL overrides the session affinity window for routing
// decisions. Defaults to 24h.
func WithRoutingTTL(ttl time.Duration) Option {
	return func(r *Router) { r.routingTTL = ttl }
}

// New returns a Router backed by s and reg. reader is the quota read view;
// it is accepted for ADR-0028 PR-B (plan-usage windows) and is not consulted
// yet.
func New(s *store.Store, reg *accounts.Registry, reader quota.Reader, opts ...Option) *Router {
	r := &Router{
		store:      s,
		registry:   reg,
		reader:     reader,
		routingTTL: defaultRoutingTTL,
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
	// Affinity only holds while the pinned account is still a candidate:
	// model-aware routing (ADR-0020 §5) narrows candidates per request, and
	// a pin to an account outside that set (e.g. a glm-* account for a
	// claude-* request) must not override it.
	if found && time.Now().UTC().Before(decision.ExpiresAt) && slices.Contains(candidateAccountIDs, decision.AccountID) {
		if !r.isExhausted(decision.AccountID) {
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
		if r.isExhausted(accountID) {
			return 0, fmt.Errorf("routing: account %d is exhausted; hard policy has no failover", accountID)
		}
		return accountID, nil

	case PolicyPool:
		for _, accountID := range candidateAccountIDs {
			if !r.isExhausted(accountID) {
				return accountID, nil
			}
		}
		return 0, fmt.Errorf("routing: no non-exhausted candidate accounts for pool policy (%d checked)", len(candidateAccountIDs))

	default:
		return 0, fmt.Errorf("routing: unknown policy %q", policy)
	}
}

// isExhausted reports whether accountID has used up its known quota.
// Plan-usage windows are not consulted yet (ADR-0028 PR-B); until then no
// account is ever exhausted, which is exactly what the noop fetcher gave in
// production. It never errors, so a usage failure can no longer turn into a
// routing 503.
func (r *Router) isExhausted(accountID int64) bool { return false }

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
