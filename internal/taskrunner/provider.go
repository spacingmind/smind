// Package taskrunner ties a store.Task to a real running agent, spawning
// either internal/acp (GLM, Kimi, or any other ACP-speaking agent) or
// github.com/spacingmind/claude-agent-sdk-go (Claude Code's native headless
// protocol) and exposing one unified streaming interface to callers
// regardless of which backend a given task's agent actually speaks.
package taskrunner

import "github.com/spacingmind/smind/internal/providers"

// Provider identifies which backend agent protocol drives a task's turn.
type Provider string

const (
	// ProviderClaudeNative drives a task via Claude Code's native headless
	// CLI protocol (github.com/spacingmind/claude-agent-sdk-go).
	ProviderClaudeNative Provider = "claude-native"

	// ProviderGLM drives a task via the GLM ACP agent, spoken over the
	// Agent Client Protocol (internal/acp).
	ProviderGLM Provider = "glm"

	// ProviderKimi drives a task via Moonshot AI's Kimi CLI, also spoken
	// over the Agent Client Protocol (internal/acp) -- Kimi's CLI speaks
	// ACP natively, the same as GLM's, just a different spawned command.
	ProviderKimi Provider = "kimi"

	// ProviderCodexNative drives a task via OpenAI's Codex CLI, spoken over
	// its own native "app-server" JSON-RPC-over-stdio protocol
	// (internal/codex) -- not ACP, unlike GLM/Kimi; see internal/codex's
	// package doc comment for why.
	ProviderCodexNative Provider = "codex-native"
)

// ProviderKind classifies how a Provider's authentication is managed, for
// clients that need to render it (see ProviderInfo.Kind).
type ProviderKind string

// ProviderKindCLI marks a Provider that's spawned as an external CLI
// subprocess which manages its own authentication out of band (e.g. GLM's
// `npx -y glm-acp-agent`, see internal/acp/glm.go) -- the daemon tracks no
// credential for it at all, so there's nothing for account.add/
// account.oauthStart to do. Clients should render this as "managed
// externally", not offer a credential form or flag a missing key.
const ProviderKindCLI ProviderKind = "cli"

// ProviderCredentialKind classifies how a Provider's credential (when it
// has one -- see ProviderInfo.AccountProvider) is obtained, for clients
// deciding whether to offer a "Connect" (OAuth) button, a manual-paste form,
// or both.
type ProviderCredentialKind string

const (
	// CredentialKindOAuth marks a Provider with a real browser-based OAuth
	// login flow wired up (account.oauthStart via
	// internal/accounts.LoginCoordinator) -- clients should offer a Connect
	// button, with the manual-paste form as a fallback.
	CredentialKindOAuth ProviderCredentialKind = "oauth"

	// CredentialKindAPIKey marks a Provider with no OAuth login flow yet --
	// clients should offer only the manual-paste form (account.add).
	CredentialKindAPIKey ProviderCredentialKind = "api-key"
)

// ProviderInfo describes one supported Provider for clients (the wsapi
// provider.list method's wire shape): the Provider id itself, plus an
// optional human-facing label and an optional Kind.
//
// Kind is empty for providers whose auth is handled through the separate
// account-credential system (internal/accounts) rather than by this
// package; for those, CredentialKind and AccountProvider are set instead.
//
// AccountProvider bridges two distinct, independently-evolved ID
// vocabularies: this package's Provider (claude-native/glm/kimi/codex-native
// -- which task-execution backend runs a turn) and internal/accounts' own
// provider strings (anthropic/openai/kimi/xai/antigravity -- which
// credential-store row an account belongs to, matched by
// internal/server/proxy.go and internal/accounts.LoginCoordinator). The two
// vocabularies aren't 1:1: xai and antigravity are account-only providers
// with no task-execution counterpart here at all, so they have no
// ProviderInfo entry and can't be rendered by deriving purely from
// SupportedProviders (see accounts-dialog.tsx's doc comment and
// docs/plans/active/task-permission-ux.md's Item 7d note for that gap).
// AccountProvider is empty for providers with no credential row (kind:
// "cli") and equal to the underlying internal/accounts ID for the three
// that do -- account.add/account.oauthStart must always be called with
// AccountProvider, never ID, since that's the vocabulary those RPCs and the
// systems behind them actually match against.
type ProviderInfo struct {
	ID              Provider               `json:"id"`
	Label           string                 `json:"label,omitempty"`
	Kind            ProviderKind           `json:"kind,omitempty"`
	CredentialKind  ProviderCredentialKind `json:"credentialKind,omitempty"`
	AccountProvider string                 `json:"accountProvider,omitempty"`

	// Images reports whether the provider takes images natively (ADR-0038
	// section 3): "yes", "no" (path fallback only) or "unknown" (depends on
	// what the agent advertises at initialize).
	Images string `json:"images,omitempty"`

	// Modes is the provider's own permission mode catalog (ADR-0019), in
	// display order, and DefaultMode the id a run gets when it names none.
	// For an ACP provider these are the agent's own advertised session
	// modes once discovered (ModesDiscovered), or a single-entry fallback
	// until then -- see Runner.ProviderCatalog.
	Modes           []ModeInfo `json:"modes,omitempty"`
	DefaultMode     string     `json:"defaultMode,omitempty"`
	ModesDiscovered bool       `json:"modesDiscovered,omitempty"`
	// Models is the provider's model catalog (ADR-0025). For claude-native it
	// is static and ModelsDiscovered is false (any syntax-valid id is still
	// accepted); for ACP providers and Codex it is what the lazy probe found,
	// and ModelsDiscovered true means an id outside it is rejected up front.
	Models []ModelInfo `json:"models,omitempty"`
	// DefaultModel is the configured default model (config providers.<id>.
	// defaultModel): what a run that names no model resolves to. Empty when
	// unset, in which case the provider's own default applies.
	DefaultModel     string `json:"defaultModel,omitempty"`
	ModelsDiscovered bool   `json:"modelsDiscovered,omitempty"`
	// SupportsAutoAccept marks an ACP provider, which additionally takes
	// PermissionSettings.AutoAccept (approve every permission prompt).
	SupportsAutoAccept bool `json:"supportsAutoAccept,omitempty"`
	// LiveModeSwitch reports whether a running run's mode can be changed
	// (run.setPermissionMode) -- false for Codex, whose mode only applies
	// at thread start.
	LiveModeSwitch bool `json:"liveModeSwitch,omitempty"`

	// Runner is how the provider's turns run: "claude-native",
	// "codex-native" or "acp" (an instance reports its base's). Kind above is
	// the separate auth classification. Extends names the built-in provider
	// an instance extends (empty otherwise); Custom marks a providers.custom
	// entry (an ACP provider or an instance). All three are additive wire
	// fields (ADR-0042, AC31/AC37).
	Runner  string `json:"runner,omitempty"`
	Extends string `json:"extends,omitempty"`
	Custom  bool   `json:"custom,omitempty"`
	// Harness is the engine kind (ADR-0046): "claude-code",
	// "codex-app-server" or "acp"; Runner's value under its ADR name (additive).
	Harness string `json:"harness,omitempty"`

	// Recipe names the catalog recipe an instance was made from, and the
	// fields below it describe that recipe (internal/providers): Vendor is
	// the rate-limit profile, Thinking whether the endpoint can turn
	// thinking off ("supported" | "hidden"; claude-code only), EndpointKind
	// the wire protocol it speaks ("anthropic", ...), Notes the recipe's
	// caveats and Unverified whether its row was never re-verified. All are
	// additive wire fields, empty for an entry with no Recipe (except
	// Thinking, set for every claude-code provider).
	Recipe       string   `json:"recipe,omitempty"`
	EndpointKind string   `json:"endpointKind,omitempty"`
	Thinking     string   `json:"thinking,omitempty"`
	Notes        []string `json:"notes,omitempty"`
	Unverified   bool     `json:"unverified,omitempty"`
	Vendor       string   `json:"vendor,omitempty"`

	// Instance is an instance's own overlay as configured (not the merged
	// catalog), for editing; nil for a non-instance provider. Additive.
	Instance *InstanceOverlay `json:"instance,omitempty"`
}

// InstanceOverlay is an instance's own editable overlay (provider.updateInstance).
type InstanceOverlay struct {
	Models              []InstanceModel `json:"models,omitempty"`
	AdditionalModels    []InstanceModel `json:"additionalModels,omitempty"`
	DisallowedTools     []string        `json:"disallowedTools,omitempty"`
	InheritClaudeConfig bool            `json:"inheritClaudeConfig,omitempty"`
}

// InstanceModel is one model of an InstanceOverlay.
type InstanceModel struct {
	ID          string `json:"id"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// SupportedProviders lists the registered providers (internal/providers,
// the single source of truth for which providers the daemon supports) in
// dropdown display order. provider.list serves it through Runner.ProviderCatalog;
// the web UI renders its run/task provider dropdown and (via
// CredentialKind/AccountProvider) its accounts dialog from it. A provider
// registered there shows up everywhere without a code edit here.
//
// Each entry carries its static permission mode catalog (see
// staticModes); Runner.ProviderCatalog layers ACP agents' discovered modes
// on top.
func SupportedProviders() []ProviderInfo { return providerInfos(providers.Default()) }

func providerInfos(reg *providers.Registry) []ProviderInfo {
	specs := reg.List()
	out := make([]ProviderInfo, len(specs))
	for i, s := range specs {
		out[i] = infoFromSpec(s)
	}
	return out
}

// infoFromSpec is a provider's static catalog entry.
func infoFromSpec(s providers.Spec) ProviderInfo {
	info := ProviderInfo{
		ID: Provider(s.ID), Label: s.Label, Kind: ProviderKind(s.Kind),
		CredentialKind: ProviderCredentialKind(s.CredentialKind), AccountProvider: s.AccountProvider,
		Images: s.Images, SupportsAutoAccept: s.SupportsAutoAccept, LiveModeSwitch: s.LiveModeSwitch,
		Runner: string(s.Runner), Extends: s.Extends, Custom: s.Custom, Harness: string(s.Harness()),
	}
	info.Modes, info.DefaultMode = staticModes(s)
	info.Models = staticModels(s)
	applyRecipeFields(&info, s)
	if s.IsInstance() {
		info.Instance = instanceOverlay(s)
	}
	return info
}

// instanceOverlay is s's own overlay exactly as configured. The loader fills a
// missing label with the id, so a label equal to its id is omitted.
func instanceOverlay(s providers.Spec) *InstanceOverlay {
	o := &InstanceOverlay{
		DisallowedTools:     append([]string(nil), s.OwnDisallowedTools...),
		InheritClaudeConfig: s.InheritClaudeConfig,
	}
	if len(o.DisallowedTools) == 0 {
		o.DisallowedTools = nil
	}
	var models []InstanceModel
	for _, m := range s.OwnModels {
		im := InstanceModel{ID: m.ID, Label: m.Label, Description: m.Description, Default: m.Default}
		if im.Label == im.ID {
			im.Label = ""
		}
		models = append(models, im)
	}
	switch s.ModelsMode {
	case providers.ModelsAdd:
		o.AdditionalModels = models
	default:
		o.Models = models
	}
	return o
}

// applyRecipeFields fills info's recipe-derived additive fields from spec
// and the recipe catalog: Thinking for every claude-code provider (clients
// hide the control on "hidden"), and Recipe/Vendor/EndpointKind/Notes/
// Unverified only for an instance that names a Recipe. The endpoint-kind
// fallback reads the instance's own launch env (spec.Env) -- the value a
// recipe instance is registered with.
func applyRecipeFields(info *ProviderInfo, spec providers.Spec) {
	if spec.Harness() == providers.HarnessClaudeCode {
		info.Thinking = "supported"
		if spec.Thinking == "hidden" {
			info.Thinking = "hidden"
		}
	}
	if spec.Recipe == "" {
		return
	}
	info.Recipe = spec.Recipe
	info.Vendor = spec.Vendor
	if spec.Endpoint.Kind != "" {
		info.EndpointKind = string(spec.Endpoint.Kind)
	} else if spec.Harness() == providers.HarnessClaudeCode && spec.Env["ANTHROPIC_BASE_URL"] != "" {
		info.EndpointKind = "anthropic"
	}
	if recipe, ok := providers.RecipeByID(spec.Recipe); ok {
		info.Notes = recipe.Notes
		info.Unverified = recipe.IsUnverified()
	}
}

// ProviderInfoFor returns the registered entry for provider, and false for
// an unknown one.
func ProviderInfoFor(provider Provider) (ProviderInfo, bool) {
	s, ok := providers.Default().Get(string(provider))
	if !ok {
		return ProviderInfo{}, false
	}
	return infoFromSpec(s), true
}
