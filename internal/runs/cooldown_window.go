package runs

import "time"

// ProviderQuota answers, from a cache only (it must never fetch), whether a provider's plan window is used up.
type ProviderQuota interface {
	// ExhaustedUntil: the reset time of the used-up window of provider, ok false when none or unknown.
	ExhaustedUntil(provider string, now time.Time) (time.Time, bool)
	// ExhaustedProviders lists every provider with a used-up window now, keyed by provider id.
	ExhaustedProviders(now time.Time) map[string]time.Time
}

// SetProviderQuota wires the cache-only plan-window source; nil clears it.
func (reg *Registry) SetProviderQuota(q ProviderQuota) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.providerQuota = q
}

func (reg *Registry) providerQuotaSource() ProviderQuota {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.providerQuota
}
