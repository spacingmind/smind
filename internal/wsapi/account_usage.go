package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
)

// usageCooldown is reserved for ADR-0028 PR-F (observed rate-limit
// cooldowns). The type and the AccountUsage field exist so the wire shape is
// stable; nothing populates it yet.
type usageCooldown struct {
	Until  string `json:"until"`           // RFC3339 UTC
	Scope  string `json:"scope"`           // "account" | "model"
	Model  string `json:"model,omitempty"` // set when Scope is "model"
	Reason string `json:"reason"`
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

// AttachUsage wires the usage poller into the API: it registers the
// account.usage RPC and publishes an account.usage event after every
// completed network fetch. Call it once, before the API serves any
// connection (the handler table is read when a connection starts) and before
// the poller is used (SetOnFetch is not safe to change while serving).
func (a *API) AttachUsage(p *quota.Poller, reg *accounts.Registry) {
	p.SetOnFetch(func(res quota.Result) {
		acc, err := reg.Get(res.AccountID)
		if err != nil {
			return // removed meanwhile; account.removed covers it
		}
		a.bus.Publish(Event{Topic: TopicAccountUsage, Payload: accountUsagePayload{Usage: usageFrom(acc, res)}})
	})
	a.hs["account.usage"] = handleAccountUsage(p, reg)
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

func handleAccountUsage(p *quota.Poller, reg *accounts.Registry) handlerFunc {
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
			res.Reports = append(res.Reports, usageFrom(cur, results[i]))
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
