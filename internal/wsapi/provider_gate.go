package wsapi

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/gate"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// windowHolder lets the router be attached after construction (serve.go).
type windowHolder struct{ w gate.WindowReader }

// gateSource builds the gate.Inputs of this daemon from live, read-only state
// (accounts store, provider registry, cooldowns, the cached plan windows). It
// never fetches usage (ADR-0028): see internal/gate.
type gateSource struct {
	acctReg *accounts.Registry
	runner  *taskrunner.Runner
	reg     *runs.Registry
	bus     *eventBus
	recent  *gate.Recent
	windows atomic.Pointer[windowHolder]

	mu      sync.Mutex
	lastSig string
}

func newGateSource(acctReg *accounts.Registry, runner *taskrunner.Runner, reg *runs.Registry, bus *eventBus) *gateSource {
	return &gateSource{acctReg: acctReg, runner: runner, reg: reg, bus: bus, recent: gate.NewRecent()}
}

// SetWindows attaches the account window reader (the router).
func (g *gateSource) SetWindows(w gate.WindowReader) { g.windows.Store(&windowHolder{w: w}) }

type lazyWindows struct{ g *gateSource }

func (l lazyWindows) Exhaustion(id int64, model string, now time.Time) (v routing.Verdict) {
	if h := l.g.windows.Load(); h != nil && h.w != nil {
		return h.w.Exhaustion(id, model, now)
	}
	return v
}

// inputs builds one derivation's inputs; the account list is read once per
// call (decoding credentials is not free), not once per provider.
func (g *gateSource) inputs() gate.Inputs {
	var (
		once sync.Once
		all  []accounts.Account
	)
	return gate.Inputs{
		Provider: g.provider,
		Accounts: func(ap string) []gate.Account {
			once.Do(func() {
				if g.acctReg != nil {
					all, _ = g.acctReg.List()
				}
			})
			var out []gate.Account
			for _, a := range all {
				if a.Provider == ap {
					out = append(out, gate.Account{ID: a.ID, Label: a.Label})
				}
			}
			return out
		},
		Windows: lazyWindows{g},
		ProviderWindow: func(p string, now time.Time) (time.Time, bool) {
			if g.reg == nil {
				return time.Time{}, false
			}
			return g.reg.ProviderWindowEnd(p, now)
		},
		Cooldown: func(p string, now time.Time) (time.Time, bool) {
			if g.reg == nil {
				return time.Time{}, false
			}
			return g.reg.ProviderCooldownAt(p, now)
		},
		Recent: g.recent,
	}
}

func (g *gateSource) provider(id string) (gate.Provider, bool) {
	spec, ok := g.runner.Providers().Get(id)
	if !ok {
		return gate.Provider{}, false
	}
	p := gate.Provider{
		ID: spec.ID, Label: spec.Label, Harness: string(spec.Harness()),
		EndpointKind: string(spec.Endpoint.Kind), AccountProvider: spec.AccountProvider,
	}
	for _, s := range providerSecretViews(g.runner, id) {
		if !s.Set {
			p.UnsetSecrets = append(p.UnsetSecrets, s.Name)
		}
	}
	return p, true
}

// deriveFn is runs.Registry's gate source.
func (g *gateSource) deriveFn(provider string, now time.Time) *gate.Gate {
	return gate.Derive(g.inputs(), provider, "", now)
}

// fill sets entry's gate and model gates from one shared derivation.
func (g *gateSource) fill(in gate.Inputs, e *providerListEntry, now time.Time) {
	id := string(e.ID)
	e.Gate = gate.Derive(in, id, "", now)
	models := make([]string, 0, len(e.Models))
	for _, m := range e.Models {
		models = append(models, m.ID)
	}
	e.ModelGates = gate.ModelGates(in, id, models, now)
}

// onFailure is runs.Registry's failure hook: a run that failed signed out, or
// on a model outside the plan, gates the row right away (advisory, in memory),
// and any plan-limit class may have set a cooldown.
func (g *gateSource) onFailure(provider, model, kind string) {
	now := time.Now()
	switch kind {
	case failure.KindAuthExpired:
		g.recent.Mark(provider, "", gate.ReasonNotSignedIn, 0, now)
	case failure.KindModelNotInPlan:
		if model != "" {
			g.recent.Mark(provider, model, gate.ReasonModelNotInPlan, 0, now)
		}
	}
	g.notifyIfChanged()
}

// credentialChanged drops the recent-failure marks (an account was added,
// replaced or removed) and refreshes open pickers.
func (g *gateSource) credentialChanged() {
	g.recent.ClearAll()
	g.notifyIfChanged()
}

// notifyIfChanged publishes provider.updated (id "": clients re-read
// provider.list) when any provider's gate differs from the last publish, so an
// open picker follows without a poll and an unchanged state is never re-sent.
func (g *gateSource) notifyIfChanged() {
	if g == nil || g.bus == nil || g.runner == nil {
		return
	}
	now := time.Now()
	in := g.inputs()
	type row struct {
		Gate *gate.Gate
		M    map[string]gate.Gate
	}
	sig := map[string]row{}
	for _, info := range g.runner.ProviderCatalog() {
		e := providerListEntry{ProviderInfo: info}
		g.fill(in, &e, now)
		if e.Gate != nil || len(e.ModelGates) > 0 {
			sig[string(info.ID)] = row{e.Gate, e.ModelGates}
		}
	}
	b, _ := json.Marshal(sig)
	g.mu.Lock()
	changed := string(b) != g.lastSig
	g.lastSig = string(b)
	g.mu.Unlock()
	if changed {
		g.bus.Publish(Event{Topic: TopicProviderUpdated, Payload: providerUpdatedPayload{}})
	}
}
