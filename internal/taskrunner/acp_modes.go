package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spacingmind/smind/internal/codex"
	"log"
	"os"
	"time"

	"github.com/spacingmind/smind/internal/acp"
)

// acpModeProbeTimeout bounds one background mode-discovery probe (spawn,
// initialize, session/new). Generous because an npx-launched agent may
// have to download itself on first run.
const acpModeProbeTimeout = 60 * time.Second

// acpModeProbeRetry is how long after a failed probe ProviderCatalog waits
// before kicking another one.
const acpModeProbeRetry = 5 * time.Minute

// acpModeConfigCategory is ACP's SessionConfigOption category for an
// agent that exposes its modes as a select config option instead of
// SessionModeState (refs/paseo/.../acp-agent.ts's deriveModesFromACP).
const acpModeConfigCategory = "mode"

// acpModeCatalog is one ACP provider's discovered permission modes.
// configID is set when the modes came from a category-"mode" config
// option rather than SessionModeState -- runACP then applies a mode with
// session/set_config_option instead of session/set_mode.
type acpModeCatalog struct {
	modes       []ModeInfo
	defaultMode string
	configID    string
}

// acpModeCacheEntry is Runner's per-provider discovery bookkeeping.
type acpModeCacheEntry struct {
	catalog     *acpModeCatalog
	models      *modelCatalog // ACP: the model option; Codex: model/list
	probing     bool
	lastAttempt time.Time
}

// WithACPModeProbe makes ProviderCatalog kick a background probe (spawn
// the agent, session/new in a scratch dir, read its advertised modes)
// for any ACP provider whose modes aren't known yet. Off by default so
// tests constructing a Runner never spawn a real agent behind their back;
// the daemon (cmd/smind serve) turns it on. Real runs populate the cache
// regardless (see recordACPModes).
func WithACPModeProbe() Option {
	return func(r *Runner) { r.acpModeProbe = true }
}

// deriveACPModes builds a catalog from what a session advertised:
// SessionModeState first, else a category-"mode" select config option,
// else nothing (ok false).
func deriveACPModes(modes *acp.SessionModeState, options []acp.ConfigOption) (acpModeCatalog, bool) {
	if modes != nil && len(modes.AvailableModes) > 0 {
		c := acpModeCatalog{defaultMode: modes.CurrentModeID}
		for _, m := range modes.AvailableModes {
			c.modes = append(c.modes, ModeInfo{ID: m.ID, Label: m.Name, Description: m.Description, AutoApproves: acpBypassLike(m.ID)})
		}
		if c.defaultMode == "" {
			c.defaultMode = c.modes[0].ID
		}
		return c, true
	}
	for _, o := range options {
		if o.Category != acpModeConfigCategory || o.Type != "select" || len(o.Options) == 0 {
			continue
		}
		c := acpModeCatalog{configID: o.ConfigID, defaultMode: configCurrentValue(o.CurrentValue)}
		for _, so := range o.Options {
			c.modes = append(c.modes, ModeInfo{ID: so.Value, Label: so.Name, Description: so.Description, AutoApproves: acpBypassLike(so.Value)})
		}
		if c.defaultMode == "" {
			c.defaultMode = c.modes[0].ID
		}
		return c, true
	}
	return acpModeCatalog{}, false
}

// configCurrentValue decodes a select config option's currentValue, which
// agents send either as a bare string or as {"type":"id","value":...}.
func configCurrentValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var v struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &v) == nil {
		return v.Value
	}
	return ""
}

// recordACPModes caches what a real (or probe) session advertised for
// provider. A session that advertised nothing leaves any earlier catalog
// in place.
func (r *Runner) recordACPModes(provider Provider, modes *acp.SessionModeState, options []acp.ConfigOption) {
	c, modesOK := deriveACPModes(modes, options)
	m, modelsOK := deriveACPModels(options)
	if !modesOK && !modelsOK {
		return
	}
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	e := r.acpModes[provider]
	if modesOK {
		e.catalog = &c
	}
	if modelsOK {
		e.models = &m
	}
	r.acpModes[provider] = e
}

// acpModelCatalog returns provider's discovered model catalog, if any.
func (r *Runner) acpModelCatalog(provider Provider) (modelCatalog, bool) {
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	e := r.acpModes[provider]
	if e.models == nil {
		return modelCatalog{}, false
	}
	return *e.models, true
}

// acpCatalog returns provider's discovered catalog, if any.
func (r *Runner) acpCatalog(provider Provider) (acpModeCatalog, bool) {
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	e := r.acpModes[provider]
	if e.catalog == nil {
		return acpModeCatalog{}, false
	}
	return *e.catalog, true
}

// maybeProbeACPModes starts a background discovery probe for provider
// unless probing is disabled, its modes are already known, a probe is in
// flight, or the last one failed less than acpModeProbeRetry ago.
func (r *Runner) maybeProbeACPModes(provider Provider) {
	r.maybeProbe(provider, r.probeACPModes)
}

// maybeProbe is the shared lazy-probe gate for ACP mode/model discovery
// and Codex model discovery: at most one probe in flight per provider, and
// a failed one is retried only after acpModeProbeRetry.
func (r *Runner) maybeProbe(provider Provider, probe func(context.Context, Provider) error) {
	if !r.acpModeProbe {
		return
	}
	r.modeMu.Lock()
	e := r.acpModes[provider]
	if e.catalog != nil || e.models != nil || e.probing || (!e.lastAttempt.IsZero() && time.Since(e.lastAttempt) < acpModeProbeRetry) {
		r.modeMu.Unlock()
		return
	}
	e.probing = true
	e.lastAttempt = time.Now()
	r.acpModes[provider] = e
	r.modeMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), acpModeProbeTimeout)
		defer cancel()
		if err := probe(ctx, provider); err != nil {
			log.Printf("taskrunner: probe %s modes/models: %v", provider, err)
		}
		r.modeMu.Lock()
		e := r.acpModes[provider]
		e.probing = false
		r.acpModes[provider] = e
		r.modeMu.Unlock()
	}()
}

// probeACPModes spawns provider's agent, opens a throwaway session in a
// scratch directory, and records whatever modes it advertises -- Paseo's
// listModes probe pattern. Every permission request is denied: a probe
// never prompts, so none should arrive.
func (r *Runner) probeACPModes(ctx context.Context, provider Provider) error {
	command, ok := r.acpCommandFor(provider)
	if !ok {
		return fmt.Errorf("no ACP command configured for provider %q", provider)
	}
	dir, err := os.MkdirTemp("", "smind-acp-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	popts := []acp.Option{acp.WithPermissionPolicy(acp.AutoDenyPolicy{})}
	if env := r.acpEnvEntries(provider); len(env) > 0 {
		popts = append(popts, acp.WithEnv(env...))
	}
	client, err := r.newACPClient(command, popts...)
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	defer client.Close()
	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	sessionID, options, err := client.NewSession(ctx, dir, nil) // probe: never spawn user MCP servers
	if err != nil {
		return fmt.Errorf("session/new: %w", err)
	}
	r.recordSessionModes(provider, client, sessionID, options)
	return nil
}

// recordSessionModes feeds sessionID's advertised modes (and config
// options) from client into the catalog cache.
func (r *Runner) recordSessionModes(provider Provider, client acpBackend, sessionID string, options []acp.ConfigOption) {
	var modes *acp.SessionModeState
	if m, ok := client.SessionModes(sessionID); ok {
		modes = &m
	}
	r.recordACPModes(provider, modes, options)
}

// probeCodexModels spawns a short-lived Codex app-server and records its
// model/list. A server without model/list (an older codex) errors here, so
// the provider keeps reporting no models and accepts any syntax-valid
// string (ADR-0025).
func (r *Runner) probeCodexModels(ctx context.Context, provider Provider) error {
	var copts []codex.Option
	if env := r.providerEnvEntries(provider); len(env) > 0 {
		copts = append(copts, codex.WithEnv(env...))
	}
	client, err := r.newCodexClient(r.codexCommandFor(provider), copts...)
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	defer client.Close()
	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	listed, err := client.ListModels(ctx)
	if err != nil {
		return err
	}
	if len(listed) == 0 {
		return nil
	}
	c := modelCatalog{}
	for _, m := range listed {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		c.models = append(c.models, ModelInfo{ID: m.ID, Label: label, Description: m.Description, Default: m.IsDefault})
		if m.IsDefault {
			c.current = m.ID
		}
	}
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	e := r.acpModes[provider]
	e.models = &c
	r.acpModes[provider] = e
	return nil
}
