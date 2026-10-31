package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
)

// usageCooldown is the passive rate-limit cooldown the proxy learned from a
// rejected response (ADR-0028 §7). It is present only while a non-transient
// cooldown is active: a bare-429 back-off is never shown.
type usageCooldown struct {
	Until  string `json:"until"`            // RFC3339 UTC
	Scope  string `json:"scope"`            // "account" | "model"
	Model  string `json:"model,omitempty"`  // set when Scope is "model"
	Reason string `json:"reason"`           // "rejected" | "fallback"
	Window string `json:"window,omitempty"` // "five_hour" | "weekly" | "weekly_model" | "unified"; omitted when unknown
}

// AccountUsage is the wire shape of one account's usage (RPC account.usage
// and the account.usage event). It never carries a credential, an email
// beyond the user's own display label, a fingerprint or a provider body.
type AccountUsage struct {
	AccountID      int64          `json:"accountId"`
	Key            string         `json:"key"`
	Provider       string         `json:"provider"`
	DisplayName    string         `json:"displayName"`
	CredentialType string         `json:"credentialType"`
	Stale          bool           `json:"stale"`
	NextRefreshAt  *string        `json:"nextRefreshAt"`
	RefreshBlocked *string        `json:"refreshBlocked"`
	InFlight       bool           `json:"inFlight"`
	LastError      *string        `json:"lastError"`
	Report         *quota.Report  `json:"report"`
	Cooldown       *usageCooldown `json:"cooldown,omitempty"`
}

// usageWaitTimeout bounds how long touch and refresh wait for a fetch before
// answering with what the cache holds (inFlight true). A var so tests can
// shorten it.
var usageWaitTimeout = 20 * time.Second

type accountUsageParams struct {
	AccountIDs []int64 `json:"accountIds"`
	Mode       string  `json:"mode"`
}

type accountUsageResult struct {
	Enabled    bool           `json:"enabled"`
	TTLSeconds int64          `json:"ttlSeconds"`
	Reports    []AccountUsage `json:"reports"`
}

type accountUsagePayload struct {
	Usage AccountUsage `json:"usage"`
}

// PassiveUsage is the read side of the passive observer the usage rows are
// enriched from (*quota.Passive). It has no method that reaches the network.
type PassiveUsage interface {
	// Active lists the account's un-expired non-transient cooldowns, the
	// account-wide one first, then the model-scoped ones, latest end first.
	Active(accountID int64, now time.Time) []quota.Cooldown
	// Observed is the latest rate-limit observation, for display only.
	Observed(accountID int64) (quota.Observation, bool)
	// SetOnChange registers the callback for a cooldown or observation change.
	SetOnChange(fn func(accountID int64))
}

// AttachUsage wires the usage poller into the API: it registers the
// account.usage RPC and publishes an account.usage event after every
// completed network fetch. With passive (nil is allowed: usage rows then
// carry no cooldown and the fetched report only) the rows also carry the
// passive cooldown and the display-rule report, and an event is published
// whenever the passive observer reports a change. Neither path calls
// Poller.Touch or Poller.Refresh: nothing here makes an HTTP call.
//
// Call it once, before the API serves any connection (the handler table is
// read when a connection starts) and before the poller and the passive
// observer are used (SetOnFetch and SetOnChange are not safe to change while
// serving).
func (a *API) AttachUsage(p *quota.Poller, reg *accounts.Registry, passive PassiveUsage) {
	if isNilPassive(passive) {
		passive = nil // a typed-nil *quota.Passive must not reach a method call
	}
	p.SetOnFetch(func(res quota.Result) {
		acc, err := reg.Get(res.AccountID)
		if err != nil {
			return // removed meanwhile; account.removed covers it
		}
		a.bus.Publish(Event{Topic: TopicAccountUsage, Payload: accountUsagePayload{Usage: usageRow(acc, res, passive, p.TTL(), time.Now())}})
		a.gate.notifyIfChanged()
	})
	if passive != nil {
		n := &usageNotifier{}
		passive.SetOnChange(func(id int64) {
			// Called from the proxy goroutine: only queue, never read a store
			// or publish here (see usageNotifier).
			n.notify(id, func(id int64) {
				if !p.Enabled() {
					return
				}
				acc, err := reg.Get(id)
				if err != nil {
					return // removed meanwhile
				}
				a.bus.Publish(Event{Topic: TopicAccountUsage, Payload: accountUsagePayload{Usage: usageRow(acc, p.Peek(id), passive, p.TTL(), time.Now())}})
				a.gate.notifyIfChanged()
			})
		})
	}
	a.hs["account.usage"] = handleAccountUsage(p, reg, passive)
}

// isNilPassive reports whether passive is nil or an interface holding a nil
// pointer (the typed-nil trap when a *quota.Passive variable is nil).
func isNilPassive(passive PassiveUsage) bool {
	if passive == nil {
		return true
	}
	v := reflect.ValueOf(passive)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// usageNotifier moves the work behind a passive change off the caller's
// goroutine, which is the proxy's: building the row reads the registry and
// possibly the store, and publishing takes bus locks. One short-lived drain
// goroutine serves the pending account ids (a burst for one account is one
// event built from the latest state) and exits when idle, so there is nothing
// to stop.
type usageNotifier struct {
	mu      sync.Mutex
	pending map[int64]struct{}
	running bool
}

func (n *usageNotifier) notify(id int64, fn func(int64)) {
	n.mu.Lock()
	if n.pending == nil {
		n.pending = map[int64]struct{}{}
	}
	n.pending[id] = struct{}{}
	if n.running {
		n.mu.Unlock()
		return
	}
	n.running = true
	n.mu.Unlock()
	go n.drain(fn)
}

func (n *usageNotifier) drain(fn func(int64)) {
	for {
		n.mu.Lock()
		var id int64
		found := false
		for id = range n.pending {
			found = true
			break
		}
		if !found {
			n.running = false
			n.mu.Unlock()
			return
		}
		delete(n.pending, id)
		n.mu.Unlock()
		fn(id)
	}
}

func usageFrom(acc accounts.Account, res quota.Result) AccountUsage {
	u := AccountUsage{
		AccountID:      acc.ID,
		Key:            quota.AccountKey(acc),
		Provider:       acc.Provider,
		DisplayName:    quota.DisplayName(acc),
		CredentialType: acc.CredentialType,
		Stale:          res.Stale,
		InFlight:       res.InFlight,
		Report:         res.Report,
	}
	if res.NextRefreshAt != nil {
		// Round up to the next whole second: a client that re-enables
		// Refresh at the stamp must not land just inside the floor.
		s := res.NextRefreshAt.Add(time.Second - 1).UTC().Truncate(time.Second).Format(time.RFC3339)
		u.NextRefreshAt = &s
	}
	if res.RefreshBlocked != "" {
		s := string(res.RefreshBlocked)
		u.RefreshBlocked = &s
	}
	if res.LastError != "" {
		s := res.LastError
		u.LastError = &s
	}
	return u
}

// usageRow is usageFrom plus the passive data: the cooldown to show and, when
// an observation exists, the display-rule report (a fresh fetched report keeps
// its numbers; see quota.DisplayReport). passive may be nil.
func usageRow(acc accounts.Account, res quota.Result, passive PassiveUsage, ttl time.Duration, now time.Time) AccountUsage {
	u := usageFrom(acc, res)
	if passive == nil {
		return u
	}
	if active := passive.Active(acc.ID, now); len(active) > 0 {
		// The card must not understate the block: the cooldown that ends last,
		// the account-wide one on a tie (Active lists it first).
		c := active[0]
		for _, o := range active[1:] {
			if o.Until.After(c.Until) {
				c = o
			}
		}
		u.Cooldown = &usageCooldown{
			Until:  c.Until.UTC().Format(time.RFC3339),
			Scope:  string(c.Scope),
			Model:  c.Model,
			Reason: string(c.Reason),
			Window: c.Window,
		}
	}
	if obs, ok := passive.Observed(acc.ID); ok {
		if rep, ok := quota.DisplayReport(res, &obs, ttl, now); ok {
			u.Report = &rep
		}
	}
	return u
}

func handleAccountUsage(p *quota.Poller, reg *accounts.Registry, passive PassiveUsage) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var params accountUsageParams
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, fmt.Errorf("account.usage: invalid params: %w", err)
			}
		}
		mode := params.Mode
		switch mode {
		case "":
			mode = "cached"
		case "cached", "touch", "refresh":
		default:
			return nil, fmt.Errorf("account.usage: unknown mode %q", mode)
		}
		res := accountUsageResult{Enabled: p.Enabled(), TTLSeconds: int64(p.TTL() / time.Second), Reports: []AccountUsage{}}
		if !res.Enabled {
			return res, nil
		}

		accs, err := resolveUsageAccounts(reg, params.AccountIDs)
		if err != nil {
			return nil, err
		}
		if mode == "refresh" && rc != nil && rc.runScoped {
			mode = "touch"
		}

		results := make([]quota.Result, len(accs))
		switch mode {
		case "cached":
			for i, acc := range accs {
				results[i] = p.Peek(acc.ID)
			}
		default:
			waitCtx, cancel := context.WithTimeout(ctx, usageWaitTimeout)
			defer cancel()
			var wg sync.WaitGroup
			for i, acc := range accs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if mode == "refresh" {
						results[i] = p.Refresh(waitCtx, acc.ID)
					} else {
						results[i] = p.Touch(waitCtx, acc.ID)
					}
				}()
			}
			wg.Wait()
		}
		for i, acc := range accs {
			// Re-read: an account removed (or renamed) during the wait is
			// dropped (account.removed already told clients) or shown by its
			// current name.
			cur, err := reg.Get(acc.ID)
			if err != nil {
				continue
			}
			res.Reports = append(res.Reports, usageRow(cur, results[i], passive, p.TTL(), time.Now()))
		}
		return res, nil
	}
}

// resolveUsageAccounts returns the requested accounts, deduplicated in
// request order; no ids means every account in id order.
func resolveUsageAccounts(reg *accounts.Registry, ids []int64) ([]accounts.Account, error) {
	if len(ids) == 0 {
		all, err := reg.List()
		if err != nil {
			return nil, fmt.Errorf("account.usage: %w", err)
		}
		sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
		return all, nil
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]accounts.Account, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		acc, err := reg.Get(id)
		if err != nil {
			return nil, fmt.Errorf("account.usage: unknown account %d", id)
		}
		out = append(out, acc)
	}
	return out, nil
}
