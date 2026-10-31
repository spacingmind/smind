package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// ProviderUsageInstance is one provider instance eligible for a plan-usage row.
type ProviderUsageInstance struct {
	ID    string
	Label string
}

// ProviderUsageInstances lists the eligible provider instances, in id order.
type ProviderUsageInstances interface {
	UsageInstances() []ProviderUsageInstance
}

// ProviderUsage is the wire shape of one provider instance's plan usage (RPC
// provider.usage). It mirrors AccountUsage so the web row shape fits; it never
// carries a credential or a provider body.
type ProviderUsage struct {
	InstanceID     string        `json:"instanceId"`
	Key            string        `json:"key"`
	Provider       string        `json:"provider"`
	DisplayName    string        `json:"displayName"`
	CredentialType string        `json:"credentialType"`
	AccountID      int64         `json:"accountId"`
	Stale          bool          `json:"stale"`
	NextRefreshAt  *string       `json:"nextRefreshAt"`
	RefreshBlocked *string       `json:"refreshBlocked"`
	InFlight       bool          `json:"inFlight"`
	LastError      *string       `json:"lastError"`
	Report         *quota.Report `json:"report"`
	CooldownUntil  *string       `json:"cooldownUntil"`
}

type providerUsageParams struct {
	InstanceIDs []string `json:"instanceIds"`
	Mode        string   `json:"mode"`
}

type providerUsageResult struct {
	Enabled    bool            `json:"enabled"`
	TTLSeconds int64           `json:"ttlSeconds"`
	Reports    []ProviderUsage `json:"reports"`
}

// AttachProviderUsage registers the human-only provider.usage RPC. It is not in
// RunScopedMethods and refuses a run-scoped caller. Call it before the API
// serves any connection, like AttachUsage.
func (a *API) AttachProviderUsage(pu *quota.ProviderUsage, inst ProviderUsageInstances) {
	a.hs["provider.usage"] = handleProviderUsage(pu, inst)
}

func providerUsageFrom(in ProviderUsageInstance, res quota.Result) ProviderUsage {
	name := in.Label
	if name == "" {
		name = in.ID
	}
	u := ProviderUsage{
		InstanceID:     in.ID,
		Key:            "instance-" + in.ID,
		Provider:       in.ID,
		DisplayName:    name,
		CredentialType: "api_key",
		Stale:          res.Stale,
		InFlight:       res.InFlight,
		Report:         res.Report,
	}
	if res.NextRefreshAt != nil {
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

func handleProviderUsage(pu *quota.ProviderUsage, inst ProviderUsageInstances) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.usage is not available to agents"}
		}
		var params providerUsageParams
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, fmt.Errorf("provider.usage: invalid params: %w", err)
			}
		}
		mode := params.Mode
		switch mode {
		case "":
			mode = "cached"
		case "cached", "touch", "refresh":
		default:
			return nil, fmt.Errorf("provider.usage: unknown mode %q", mode)
		}
		res := providerUsageResult{Enabled: pu.Enabled(), TTLSeconds: int64(pu.TTL() / time.Second), Reports: []ProviderUsage{}}
		if !res.Enabled {
			return res, nil
		}

		insts, err := resolveProviderUsageInstances(inst, params.InstanceIDs)
		if err != nil {
			return nil, err
		}
		results := make([]quota.Result, len(insts))
		if mode == "cached" {
			for i, in := range insts {
				results[i] = pu.Peek(in.ID)
			}
		} else {
			waitCtx, cancel := context.WithTimeout(ctx, usageWaitTimeout)
			defer cancel()
			var wg sync.WaitGroup
			for i, in := range insts {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if mode == "refresh" {
						results[i] = pu.Refresh(waitCtx, in.ID)
					} else {
						results[i] = pu.Touch(waitCtx, in.ID)
					}
				}()
			}
			wg.Wait()
		}
		for i, in := range insts {
			res.Reports = append(res.Reports, providerUsageFrom(in, results[i]))
		}
		return res, nil
	}
}

// resolveProviderUsageInstances returns the requested eligible instances,
// deduplicated in request order; no ids means every eligible instance.
func resolveProviderUsageInstances(inst ProviderUsageInstances, ids []string) ([]ProviderUsageInstance, error) {
	all := inst.UsageInstances()
	if len(ids) == 0 {
		return all, nil
	}
	byID := make(map[string]ProviderUsageInstance, len(all))
	for _, in := range all {
		byID[in.ID] = in
	}
	seen := make(map[string]bool, len(ids))
	out := make([]ProviderUsageInstance, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		in, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("provider.usage: unknown instance %q", id)
		}
		out = append(out, in)
	}
	return out, nil
}
