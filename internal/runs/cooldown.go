package runs

import (
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// ProviderCooldownError is the fast-fail for a non-human send to a provider
// that an earlier run showed to be rate limited until Until (ADR-0041 F1).
// No run row is created and nothing is queued.
type ProviderCooldownError struct {
	Provider string
	Until    time.Time
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

// activeProviderCooldown reports provider's cooldown, if any. A store error
// counts as no cooldown: the cooldown is an optimisation, never a gate that
// can wedge sends.
func (reg *Registry) activeProviderCooldown(provider string) (time.Time, bool) {
	until, ok, err := reg.st.GetProviderCooldown(provider, time.Now())
	if err != nil || !ok {
		return time.Time{}, false
	}
	return until, true
}

// ProviderCooldowns returns the providers currently cooling down with their
// reset times (UTC), for provider.list and provider_list.
func (reg *Registry) ProviderCooldowns() map[string]time.Time {
	m, err := reg.st.ListProviderCooldowns(time.Now())
	if err != nil {
		return map[string]time.Time{}
	}
	return m
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
