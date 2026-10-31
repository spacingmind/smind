package runs

import (
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/gate"
	"github.com/spacingmind/smind/internal/store"
)

// ProviderCooldownError is the fast-fail for a non-human send to a provider
// that an earlier run showed to be rate limited until Until (ADR-0041 F1).
// No run row is created and nothing is queued.
type ProviderCooldownError struct {
	Provider string
	Until    time.Time
	// Gate is the typed picker gate of the same state (nil when no gate is wired).
	Gate *gate.Gate
}

// RPCCode makes the fast-fail the same typed "gated" error as GatedError.
func (e *ProviderCooldownError) RPCCode() string { return gatedCode }

// RPCData is the gate, when known.
func (e *ProviderCooldownError) RPCData() any {
	if e.Gate == nil {
		return nil
	}
	return *e.Gate
}

// gatedCode is the wire code of a send refused by a gate.
const gatedCode = "gated"

// GatedError refuses a non-human send to a provider whose gate is hard
// (docs/plans/active/picker-gate-reasons.md): not signed in, unsupported by the
// harness. No run row is created and nothing is queued; there is no silent
// fallback to another provider.
type GatedError struct {
	Provider string
	Gate     gate.Gate
}

// RPCCode is the wire code.
func (e *GatedError) RPCCode() string { return gatedCode }

// RPCData is the typed gate.
func (e *GatedError) RPCData() any { return e.Gate }

func (e *GatedError) Error() string {
	next := ""
	if e.Gate.Action != nil && e.Gate.Action.Label != "" {
		next = " Next: " + e.Gate.Action.Label + "."
	}
	return fmt.Sprintf("runs: start: provider %s is gated: %s.%s A human must fix this (provider_list shows the gate): do not retry and do not fall back to another provider on your own.", e.Provider, e.Gate.Reason, next)
}

func (e *ProviderCooldownError) Error() string {
	return formatProviderCooldownError(e.Provider, e.Until, time.Now())
}

// formatProviderCooldownError renders the error text against an explicit
// now (the clock is local, like the provider's own reset message).
func formatProviderCooldownError(provider string, until, now time.Time) string {
	return fmt.Sprintf("runs: start: provider %s is rate-limited until %s (in %s): start the work on another provider (provider_list) or retry after that time",
		provider, until.In(now.Location()).Format("15:04"), formatCooldownWait(until.Sub(now)))
}

func formatCooldownWait(d time.Duration) string {
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	if mins >= 60 {
		return fmt.Sprintf("%dh %dm", mins/60, mins%60)
	}
	return fmt.Sprintf("%dm", mins)
}

// providerCooldownNote is the session note a human send gets when its
// provider is cooling down (a human is never blocked).
func providerCooldownNote(provider string, until time.Time) string {
	return fmt.Sprintf("provider %s was rate-limited until %s by an earlier run; this run may fail the same way", provider, until.In(time.Now().Location()).Format("15:04"))
}

// activeProviderCooldown reports provider's cooldown, if any: the later of
// the stored 429 cooldown and the reset of a used-up plan window from the
// cache-only ProviderQuota (capped at store.MaxProviderCooldown). A store
// error counts as no stored cooldown: the cooldown is an optimisation, never
// a gate that can wedge sends.
func (reg *Registry) activeProviderCooldown(provider string) (time.Time, bool) {
	now := time.Now()
	until, ok, err := reg.st.GetProviderCooldown(provider, now)
	if err != nil || !ok {
		until, ok = time.Time{}, false
	}
	if q := reg.providerQuotaSource(); q != nil {
		if w, wok := q.ExhaustedUntil(provider, now); wok && w.After(now) {
			if w = capCooldown(w, now); !ok || w.After(until) {
				until, ok = w, true
			}
		}
	}
	return until, ok
}

// capCooldown caps a window reset at now+store.MaxProviderCooldown.
func capCooldown(until, now time.Time) time.Time {
	if limit := now.Add(store.MaxProviderCooldown); until.After(limit) {
		return limit
	}
	return until
}

// ProviderCooldowns returns the providers currently cooling down with their
// reset times (UTC), for provider.list and provider_list: the stored 429
// cooldowns merged with the cached used-up plan windows, the later reset
// per provider.
func (reg *Registry) ProviderCooldowns() map[string]time.Time {
	now := time.Now()
	m, err := reg.st.ListProviderCooldowns(now)
	if err != nil || m == nil {
		m = map[string]time.Time{}
	}
	if q := reg.providerQuotaSource(); q != nil {
		for provider, w := range q.ExhaustedProviders(now) {
			if !w.After(now) {
				continue
			}
			if w = capCooldown(w, now); w.After(m[provider]) {
				m[provider] = w
			}
		}
	}
	return m
}

// ClearProviderCooldown is the human's "I reset my quota, retry now"
// action: drop provider's cooldown row and release every held rate-limit
// retry of that provider, delivering them at once. cleared reports whether
// a cooldown row was removed, woken the number of released retries. Safe to
// call twice: the second call clears nothing and wakes nobody. Holds no
// locks, so DeliverQueued can be called at the end.
func (reg *Registry) ClearProviderCooldown(provider string) (cleared bool, woken int, err error) {
	cleared, err = reg.st.ClearProviderCooldown(provider)
	if err != nil {
		return false, 0, fmt.Errorf("runs: clear provider cooldown for %s: %w", provider, err)
	}
	chats, err := reg.st.ReleaseHeldRetries(provider, time.Now())
	if err != nil {
		return false, 0, fmt.Errorf("runs: release held retries for %s: %w", provider, err)
	}
	seen := map[int64]bool{}
	for _, chatID := range chats {
		if !seen[chatID] {
			seen[chatID] = true
			reg.notifyQueueUpdated(chatID)
		}
	}
	if len(chats) > 0 {
		reg.DeliverQueued()
	}
	return cleared, len(chats), nil
}

// nonHumanSend reports whether a StartWhenBusy caller is not a human at the
// keyboard: a non-human source, or any task/chat provenance (a profile send
// carries from ids even when its source is human).
func nonHumanSend(source string, fromTaskID int64) bool {
	return source != store.ChatQueueSourceHuman || fromTaskID != 0
}

// windowBlocksProvider reports whether a rate limit of this window restricts
// the whole provider. A model-scoped window ("weekly-opus", "weekly-sonnet",
// any "weekly-<model>") only restricts that model, so another model's send
// to the same provider must not be blocked by it.
func windowBlocksProvider(window string) bool {
	return !strings.HasPrefix(window, "weekly-")
}
