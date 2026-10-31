package gate

import (
	"time"

	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/runs/failure"
)

// Provider is the part of a provider's catalog entry the gate reads.
type Provider struct {
	ID, Label string
	// Harness is "claude-code" | "codex-app-server" | "acp" (ADR-0046).
	Harness      string
	EndpointKind string
	// AccountProvider names the smind accounts whose usage windows describe this
	// provider ("" when none).
	AccountProvider string
	// UnsetSecrets are the required provider secrets with no stored value: a
	// launch fails on them (taskrunner launchEnvEntries).
	UnsetSecrets []string
}

// Account is an account of a provider's AccountProvider.
type Account struct {
	ID    int64
	Label string
}

// WindowReader is the read-only account window verdict (routing.Router).
type WindowReader interface {
	Exhaustion(accountID int64, model string, now time.Time) routing.Verdict
}

// Inputs are the read-only sources of a derivation; any may be nil.
type Inputs struct {
	Provider func(id string) (Provider, bool)
	Accounts func(accountProvider string) []Account
	Windows  WindowReader
	// ProviderWindow is a provider instance's used-up plan window (its end).
	ProviderWindow func(provider string, now time.Time) (time.Time, bool)
	// Cooldown is the provider's merged cooldown (ADR-0041 F1), end time.
	Cooldown func(provider string, now time.Time) (time.Time, bool)
	Recent   *Recent
}

// Derive returns the gate of provider (and model; "" is the provider-level
// row), or nil when nothing blocks it. Precedence, first match wins: harness,
// signed-out, model not in plan, out of plan, cooling down.
func Derive(in Inputs, provider, model string, now time.Time) *Gate {
	if in.Provider == nil {
		return nil
	}
	p, ok := in.Provider(provider)
	if !ok {
		return nil
	}
	who := p.Label
	if who == "" {
		who = p.ID
	}
	ctx := failure.ActionContext{Provider: p.ID, ProviderLabel: who, Model: model, Now: now}

	if p.Harness == "codex-app-server" && p.EndpointKind != "" && p.EndpointKind != "openai-responses" {
		return &Gate{Reason: ReasonUnsupportedByHarness, Action: &failure.Action{
			Kind:  failure.ActionPickProvider,
			Label: who + " cannot run through the Codex harness (its endpoint is not a Responses API): pick another provider",
		}}
	}
	if len(p.UnsetSecrets) > 0 {
		return &Gate{Reason: ReasonNotSignedIn, Action: &failure.Action{
			Kind:   failure.ActionSignIn,
			Target: "settings:providers?provider=" + p.ID,
			Label:  "Add the API key for " + who,
		}}
	}
	if e, ok := in.Recent.get(p.ID, "", ReasonNotSignedIn, now); ok {
		return &Gate{Reason: ReasonNotSignedIn, AccountID: e.accountID, Advisory: true, Action: failure.NextAction(failure.KindAuthExpired, ctx)}
	}
	if model != "" {
		if _, ok := in.Recent.get(p.ID, model, ReasonModelNotInPlan, now); ok {
			return &Gate{Reason: ReasonModelNotInPlan, Advisory: true, Action: failure.NextAction(failure.KindModelNotInPlan, ctx)}
		}
	}

	cdUntil, cooling := time.Time{}, false
	if in.Cooldown != nil {
		if u, ok := in.Cooldown(p.ID, now); ok && u.After(now) {
			cdUntil, cooling = u, true
		}
	}
	if u, ok := providerWindow(in, p.ID, now); ok {
		if cooling && cdUntil.After(u) {
			u = cdUntil
		}
		return planGate(ctx, u, 0, false)
	}
	if id, u, ok := accountsExhausted(in, p, model, now); ok {
		return planGate(ctx, u, id, true)
	}
	if cooling {
		until := cdUntil.UTC()
		return &Gate{Reason: ReasonCoolingDown, Until: &until, Action: &failure.Action{
			Kind:   failure.ActionClearCooldown,
			Target: p.ID,
			Label:  "Cooling down until " + cdUntil.In(now.Location()).Format("15:04") + ": clear it if you reset the quota",
		}}
	}
	return nil
}

func providerWindow(in Inputs, provider string, now time.Time) (time.Time, bool) {
	if in.ProviderWindow == nil {
		return time.Time{}, false
	}
	u, ok := in.ProviderWindow(provider, now)
	return u, ok && u.After(now)
}

// accountsExhausted reports a plan window spent on every account of the
// provider (one usable account ungates it), with the earliest end: the first
// account to recover ungates the provider. A transient bare-429 back-off is
// never exhaustion.
func accountsExhausted(in Inputs, p Provider, model string, now time.Time) (accountID int64, until time.Time, ok bool) {
	if in.Accounts == nil || in.Windows == nil || p.AccountProvider == "" {
		return 0, time.Time{}, false
	}
	accts := in.Accounts(p.AccountProvider)
	if len(accts) == 0 {
		return 0, time.Time{}, false
	}
	for i, a := range accts {
		v := in.Windows.Exhaustion(a.ID, model, now)
		if !v.Exhausted || v.TransientOnly {
			return 0, time.Time{}, false
		}
		if i == 0 || (!v.Until.IsZero() && (until.IsZero() || v.Until.Before(until))) {
			until = v.Until
			accountID = a.ID
		}
	}
	return accountID, until, true
}

func planGate(ctx failure.ActionContext, until time.Time, accountID int64, advisory bool) *Gate {
	g := &Gate{Reason: ReasonOutOfPlan, AccountID: accountID, Advisory: advisory}
	if !until.IsZero() {
		u := until.UTC()
		g.Until = &u
		ctx.ResetAt = &u
	}
	g.Action = failure.NextAction(failure.KindPlanWindowExhausted, ctx)
	return g
}

// ModelGates returns the gates of models that differ from the provider's own:
// a model of a gated provider inherits the provider gate client-side, so it is
// not repeated here.
func ModelGates(in Inputs, provider string, models []string, now time.Time) map[string]Gate {
	pg := Derive(in, provider, "", now)
	var out map[string]Gate
	for _, m := range models {
		g := Derive(in, provider, m, now)
		if g == nil || (pg != nil && pg.Reason == g.Reason) {
			continue
		}
		if out == nil {
			out = map[string]Gate{}
		}
		out[m] = *g
	}
	return out
}
