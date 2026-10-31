package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/wsapi"
)

// zaiInstanceSource lists the Z.AI provider instances that have a plan-usage
// source and resolves what a usage call for one needs. The key comes from the
// instance's stored secret and goes nowhere but the fetcher.
type zaiInstanceSource struct {
	reg     *providers.Registry
	secrets taskrunner.ProviderSecretSource
	// quotaURL derives the quota endpoint from a base URL; nil means
	// quota.ZaiQuotaURL.
	quotaURL func(base string) (string, bool)
}

var (
	_ quota.InstanceCreds          = (*zaiInstanceSource)(nil)
	_ wsapi.ProviderUsageInstances = (*zaiInstanceSource)(nil)
	_ runs.ProviderQuota           = (*providerQuotaAdapter)(nil)
)

func (s *zaiInstanceSource) derive(base string) (string, bool) {
	if s.quotaURL != nil {
		return s.quotaURL(base)
	}
	return quota.ZaiQuotaURL(base)
}

func instanceBaseURL(spec providers.Spec) string {
	if v := spec.Env["ANTHROPIC_BASE_URL"]; v != "" {
		return v
	}
	return spec.Endpoint.BaseURL
}

// eligible returns the quota URL of spec when it is a Z.AI custom instance
// whose base URL maps to an allowlisted quota endpoint.
func (s *zaiInstanceSource) eligible(spec providers.Spec) (string, bool) {
	if !spec.Custom || spec.Vendor != providers.VendorZAI {
		return "", false
	}
	return s.derive(instanceBaseURL(spec))
}

// UsageInstances lists the eligible instances, sorted by id.
func (s *zaiInstanceSource) UsageInstances() []wsapi.ProviderUsageInstance {
	var out []wsapi.ProviderUsageInstance
	for _, spec := range s.reg.List() {
		if _, ok := s.eligible(spec); ok {
			out = append(out, wsapi.ProviderUsageInstance{ID: spec.ID, Label: spec.Label})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Creds implements quota.InstanceCreds.
func (s *zaiInstanceSource) Creds(instanceID string) (string, string, error) {
	spec, ok := s.reg.Get(instanceID)
	if !ok {
		return "", "", quota.ErrInstanceUnsupported
	}
	url, ok := s.eligible(spec)
	if !ok {
		return "", "", quota.ErrInstanceUnsupported
	}
	name := spec.SecretEnv["ANTHROPIC_AUTH_TOKEN"]
	if name == "" {
		name = spec.SecretEnv["ANTHROPIC_API_KEY"]
	}
	if name == "" {
		return "", "", quota.ErrInstanceNoKey
	}
	val, found, err := s.secrets.GetProviderSecret(instanceID, name)
	if err != nil {
		// Deliberately no value and no underlying text.
		return "", "", fmt.Errorf("read provider secret")
	}
	if !found || strings.TrimSpace(val) == "" {
		return "", "", quota.ErrInstanceNoKey
	}
	return url, val, nil
}

// providerQuotaAdapter feeds routing from the ProviderUsage cache only: it
// never fetches, so routing adds no usage call.
type providerQuotaAdapter struct {
	pu           *quota.ProviderUsage
	inst         *zaiInstanceSource
	routing      bool
	thresholdPct float64
}

func (a *providerQuotaAdapter) ExhaustedUntil(provider string, now time.Time) (time.Time, bool) {
	if !a.routing {
		return time.Time{}, false
	}
	for _, in := range a.inst.UsageInstances() {
		if in.ID == provider {
			return a.pu.Exhausted(provider, a.thresholdPct, now)
		}
	}
	return time.Time{}, false
}

func (a *providerQuotaAdapter) ExhaustedProviders(now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	if !a.routing {
		return out
	}
	for _, in := range a.inst.UsageInstances() {
		if at, ok := a.pu.Exhausted(in.ID, a.thresholdPct, now); ok {
			out[in.ID] = at
		}
	}
	return out
}
