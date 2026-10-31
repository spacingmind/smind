package runs

import (
	"time"

	"github.com/spacingmind/smind/internal/gate"
)

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

// SetGate wires the picker gate (internal/gate) used to refuse a non-human send
// to a provider that is hard-gated. nil clears it (no refusal beyond the
// cooldown fast-fail).
func (reg *Registry) SetGate(fn func(provider string, now time.Time) *gate.Gate) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.gateFn = fn
}

// gateFor is provider's gate now, nil when none or no gate is wired.
func (reg *Registry) gateFor(provider string, now time.Time) *gate.Gate {
	reg.mu.Lock()
	fn := reg.gateFn
	reg.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(provider, now)
}

// SetFailureHook registers fn, called once per classified error run whose class
// can change a picker gate (a human-fix class or a plan limit), after the
// cooldown is stored. It must not block. nil clears it.
func (reg *Registry) SetFailureHook(fn func(provider, model, kind string)) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.failureHook = fn
}

func (reg *Registry) notifyFailureHook(provider, model, kind string) {
	reg.mu.Lock()
	fn := reg.failureHook
	reg.mu.Unlock()
	if fn != nil {
		fn(provider, model, kind)
	}
}

// ProviderCooldownAt is provider's merged cooldown end (stored 429 cooldown or
// a used-up plan window), cache/DB only.
func (reg *Registry) ProviderCooldownAt(provider string, now time.Time) (time.Time, bool) {
	until, ok := reg.activeProviderCooldown(provider)
	return until, ok && until.After(now)
}

// ProviderWindowEnd is the end of provider's used-up plan window from the
// cache-only source, ok false when none or unknown.
func (reg *Registry) ProviderWindowEnd(provider string, now time.Time) (time.Time, bool) {
	q := reg.providerQuotaSource()
	if q == nil {
		return time.Time{}, false
	}
	u, ok := q.ExhaustedUntil(provider, now)
	return u, ok && u.After(now)
}
