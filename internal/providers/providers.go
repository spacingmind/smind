// Package providers is the daemon's provider registry (ADR-0042, plugins plan
// P0.1): every provider is ONE Spec, and everything that used to switch on a
// provider id (the runner's dispatch, permission modes, the health check,
// learning delivery, the config allowlist) reads the Spec instead.
//
// It is a leaf package (it imports nothing from smind) so config, learning and
// taskrunner can all depend on it. A built-in provider, a providers.custom ACP
// entry and a provider instance (an entry that extends another provider with
// its own overlay) are all the same type; adding an ACP provider needs a Spec
// and no code edits elsewhere.
package providers

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
)

// Runner is how a provider's turns are driven.
type Runner string

const (
	// RunnerClaude is Claude Code's native headless CLI protocol.
	RunnerClaude Runner = "claude-native"
	// RunnerCodex is Codex's app-server protocol.
	RunnerCodex Runner = "codex-native"
	// RunnerACP is any Agent Client Protocol agent, spawned from a command.
	RunnerACP Runner = "acp"
)

// Harness is the kind of engine that drives a provider's turns (ADR-0046
// section 2). It is the Runner under its ADR name; the wire `runner` field
// keeps the Runner values, `harness` is additive.
type Harness string

const (
	HarnessClaudeCode     Harness = "claude-code"
	HarnessCodexAppServer Harness = "codex-app-server"
	HarnessACP            Harness = "acp"
)

// Harness is the harness kind of r ("" for an unknown runner).
func (r Runner) Harness() Harness {
	switch r {
	case RunnerClaude:
		return HarnessClaudeCode
	case RunnerCodex:
		return HarnessCodexAppServer
	case RunnerACP:
		return HarnessACP
	}
	return ""
}

// Built-in provider ids.
const (
	IDClaudeNative = "claude-native"
	IDCodexNative  = "codex-native"
	IDGLM          = "glm"
	IDKimi         = "kimi"
	IDOpenCode     = "opencode"
	IDCopilot      = "copilot"
	IDCline        = "cline"
	IDGoose        = "goose"
	IDCursor       = "cursor"
)

// Health is what provider.test reports beyond "the binary is on PATH": no model call is ever made.
type Health struct {
	Install    string // how to install, said when the binary is missing
	Login      string // what login/credential the agent needs, said when the binary is found ("" = none needed)
	Unverified bool   // best effort: not verified against a real login
	MinVersion string // "" = no gate; dotted/dated version the binary must be at least (compared by numeric segments)
	VersionArg string // argument that makes the binary print its version, e.g. "--version"; "" = skip the version check
}

// Learning-delivery modes (Spec.Delivery): how learned skills reach a run.
const (
	// DeliveryProjection hands the CLI a read-only skills directory (its own
	// native skill listing); loads are observable, so offers are measured.
	DeliveryProjection = "projection"
	// DeliveryPreface prepends an index to the first prompt of a session;
	// loads cannot be observed, so offers are recorded unmeasured.
	DeliveryPreface = "preface"
)

// Model is one entry of a provider's static model catalog.
type Model struct {
	ID, Label, Description string
	Default                bool
}

// ModelsMode says how a Spec's Models combine with the model list the
// provider would otherwise have (ADR-0046 section 1).
type ModelsMode string

const (
	// ModelsReplace (the zero value): Models is the whole list.
	ModelsReplace ModelsMode = "replace"
	// ModelsAdd: Models are merged by id into the provider's own list
	// (Claude's static catalog, an ACP agent's discovered models).
	ModelsAdd ModelsMode = "add"
)

// Mode is one entry of a provider's static permission-mode catalog.
type Mode struct {
	ID, Label, Description string
	AutoApproves           bool
}

// EndpointKind is the wire protocol an instance's endpoint speaks.
type EndpointKind string

const EndpointOpenAIResponses EndpointKind = "openai-responses"

// Endpoint points an instance at a compatible API (ADR-0046).
type Endpoint struct {
	Kind    EndpointKind
	BaseURL string
}

// SecretSlot is a secret environment variable a plugin provider declares.
type SecretSlot struct {
	Name, Label string
	Required    bool
}

// Spec is one provider. The zero values of the optional fields mean "none".
type Spec struct {
	ID    string
	Label string

	// Runner is how turns run; for an instance it is the extended
	// provider's, copied in at registration.
	Runner Runner
	// Extends names the built-in provider this entry is an instance of
	// (same runner, own id/label/launch env); empty for everything else.
	Extends string
	// Custom marks an entry that came from providers.custom (or a plugin),
	// not the built-in table.
	Custom bool
	// Plugin is the id of the plugin that contributes this provider (Wave D,
	// ADR-0042 section 7); empty for everything else. The spec's id is
	// "<plugin>/<provider>".
	Plugin string
	// Icon is a Lucide name a plugin provider asks for ("" = the generic
	// provider icon); SecretSlots are its declared secret variables.
	Icon        string
	SecretSlots []SecretSlot

	// Command is the argv a turn spawns (ACP agent, or `codex app-server`;
	// unused for claude-native, which the SDK launches). Env is a custom ACP
	// provider's free-form command environment; an instance's allowlisted
	// launch env is not here but in the runner's per-provider env.
	Command []string
	Env     map[string]string

	// SecretEnv maps an env NAME to a stored secret name (ADR-0046 section
	// 4): the value lives in smind.db, resolved into the child env at
	// launch, never inline in config.yaml.
	SecretEnv map[string]string
	// PassthroughEnv lists env NAMES copied from the daemon's own
	// environment at launch, sorted.
	PassthroughEnv []string

	// Models and Modes are a custom ACP provider's static catalogs (ACP
	// discovery layers the agent's own list on top, as for glm/kimi). On an
	// instance, Models + ModelsMode are its overlay: replace the base's
	// list, or add to it (additionalModels).
	Models      []Model
	ModelsMode  ModelsMode
	Modes       []Mode
	DefaultMode string

	// ModelAliases maps a model slot (opus, sonnet, haiku, subagent) to an
	// endpoint model id; carried now, consumed by the typed env (PR2).
	ModelAliases map[string]string
	// DisallowedTools is passed to a Claude Code launch as --disallowedTools
	// (an instance's list is appended to its base's).
	DisallowedTools []string
	// OwnModels and OwnDisallowedTools are an instance's own overlay exactly
	// as configured, for provider.list; Models/DisallowedTools hold the
	// merged result.
	OwnModels          []Model
	OwnDisallowedTools []string

	// Vendor is the rate-limit vendor profile: whose 429 wording the
	// provider speaks ("zai" | "anthropic" | "openai" | "generic"; "" =
	// generic).
	Vendor string
	// Thinking is whether the run may set a thinking level: "" or
	// "supported" = it may; "hidden" = the endpoint cannot turn thinking
	// off, clients hide the control.
	Thinking string
	// Recipe is the id of the recipe the instance was made from
	// (informational).
	Recipe string
	// InheritClaudeConfig marks a claude-code instance that uses ~/.claude
	// instead of a private CLAUDE_CONFIG_DIR.
	InheritClaudeConfig bool

	// Endpoint points an instance at a compatible API (ADR-0046).
	Endpoint Endpoint

	// Catalog flags served by provider.list.
	Kind               string // "cli": authentication is managed outside smind
	CredentialKind     string // "oauth" | "api-key"
	AccountProvider    string // internal/accounts vocabulary ("anthropic", ...)
	Images             string // "yes" | "no" | "unknown"
	SupportsAutoAccept bool
	LiveModeSwitch     bool

	// HealthCommand is the executable provider.test looks up on PATH
	// (argv; only [0] is looked up). Empty: a credential check against
	// AccountProvider instead.
	HealthCommand []string

	// Health is what provider.test says beyond the PATH lookup.
	Health Health
	// NoMCPServers: an ACP agent that cannot take mcpServers in session/new;
	// smind then sends none.
	NoMCPServers bool

	// Learning capabilities (ADR-0026).
	ToolEvents bool   // the run streams tool-call events
	Reflect    bool   // runs may feed the reflector
	Delivery   string // DeliveryProjection | DeliveryPreface | ""
}

// Harness is the harness kind of s.
func (s Spec) Harness() Harness { return s.Runner.Harness() }

// Base is the id of the provider whose code runs this one: Extends for an
// instance, else the id itself.
func (s Spec) Base() string {
	if s.Extends != "" {
		return s.Extends
	}
	return s.ID
}

// IsInstance reports whether s extends another provider.
func (s Spec) IsInstance() bool { return s.Extends != "" }

// overlayEnv returns base with over applied on top (over wins); nil when both
// are empty. Neither input is modified.
func overlayEnv(base, over map[string]string) map[string]string {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// HasEndpoint reports whether s points at its own API endpoint.
func (s Spec) HasEndpoint() bool { return s.Endpoint.Kind != "" }

func builtins() []Spec {
	return []Spec{
		{ID: IDClaudeNative, Label: "Claude Code", Runner: RunnerClaude, CredentialKind: "oauth", AccountProvider: "anthropic",
			LiveModeSwitch: true, Images: "yes", HealthCommand: []string{"claude"},
			ToolEvents: true, Reflect: true, Delivery: DeliveryProjection},
		{ID: IDGLM, Label: "GLM", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "no",
			Command: acp.GLMCommand(), HealthCommand: acp.GLMCommand(), ToolEvents: true, Delivery: DeliveryPreface},
		{ID: IDKimi, Label: "Kimi", Runner: RunnerACP, CredentialKind: "api-key", AccountProvider: "kimi", SupportsAutoAccept: true,
			LiveModeSwitch: true, Images: "unknown", Command: acp.KimiCommand(), ToolEvents: true, Delivery: DeliveryPreface},
		{ID: IDCodexNative, Label: "Codex", Runner: RunnerCodex, CredentialKind: "oauth", AccountProvider: "openai", Images: "yes",
			Command: codex.DefaultCommand(), HealthCommand: []string{"codex"}},
		{ID: IDOpenCode, Label: "OpenCode", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "yes",
			Command: []string{"opencode", "acp"}, HealthCommand: []string{"opencode"}, ToolEvents: true, Delivery: DeliveryPreface,
			// OpenCode auto-allows tools by default; this makes it ask so smind's permission modes apply.
			Env:    map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_PERMISSION": `{"bash":"ask","edit":"ask"}`},
			Health: Health{Install: "npm i -g opencode-ai (or brew install sst/tap/opencode)", VersionArg: "--version"}},
		{ID: IDCopilot, Label: "GitHub Copilot", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "unknown",
			Command: []string{"copilot", "--acp"}, HealthCommand: []string{"copilot"}, ToolEvents: true, Delivery: DeliveryPreface,
			Health: Health{Install: "npm i -g @github/copilot", Login: "needs a paid GitHub Copilot plan: run `copilot` once and use /login (ACP is a public preview)", Unverified: true, VersionArg: "--version"}},
		{ID: IDCline, Label: "Cline", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "unknown",
			Command: []string{"cline", "--acp"}, HealthCommand: []string{"cline"}, ToolEvents: true, Delivery: DeliveryPreface,
			Health: Health{Install: "npm i -g cline", Login: "needs a provider key configured in Cline: run `cline auth` or set CLINE_API_KEY", Unverified: true, VersionArg: "--version"}},
		{ID: IDGoose, Label: "Goose", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "unknown",
			Command: []string{"goose", "acp"}, HealthCommand: []string{"goose"}, ToolEvents: true, Delivery: DeliveryPreface,
			Health: Health{Install: "brew install block-goose-cli, or the binary from block.github.io/goose", Login: "run `goose configure` to pick a provider and key", Unverified: true, VersionArg: "--version"}},
		{ID: IDCursor, Label: "Cursor", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "unknown",
			Command: []string{"cursor-agent", "acp"}, HealthCommand: []string{"cursor-agent"}, ToolEvents: true, Delivery: DeliveryPreface,
			// The 2025.09.18 build has no `acp` subcommand.
			Health: Health{Install: "curl https://cursor.com/install -fsS | bash", Login: "needs a paid Cursor account: run `cursor-agent login` or set CURSOR_API_KEY", Unverified: true, MinVersion: "2026.01.01", VersionArg: "--version"}},
	}
}

// BuiltinIDs lists the built-in provider ids in display order.
func BuiltinIDs() []string {
	var ids []string
	for _, s := range builtins() {
		ids = append(ids, s.ID)
	}
	return ids
}

// IsBuiltin reports whether id is a built-in provider id.
func IsBuiltin(id string) bool {
	for _, s := range builtins() {
		if s.ID == id {
			return true
		}
	}
	return false
}

// ReservedID reports whether id may not be used by a custom entry: a
// built-in id, the config key "custom", or a reserved prefix.
func ReservedID(id string) bool {
	if IsBuiltin(id) || id == "custom" {
		return true
	}
	for _, p := range []string{"smind", "smind-", "smind/", "plugin/", "plugin:"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// Registry holds the live provider table. Safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	specs []Spec
	// The custom and plugin layers are kept apart so that replacing one
	// (provider.reload, a plugin change) never drops the other.
	custom  []Spec
	plugins map[string][]Spec
}

// New returns a registry holding the built-in providers.
func New() *Registry { return &Registry{specs: builtinsWithVendors()} }

var (
	defaultOnce sync.Once
	defaultReg  *Registry
)

// Default is the process-wide registry: the daemon's runner, learning and the
// profile validator all read it, and config load/reload writes it.
func Default() *Registry {
	defaultOnce.Do(func() { defaultReg = New() })
	return defaultReg
}

func cloneSpec(s Spec) Spec {
	s.SecretSlots = append([]SecretSlot(nil), s.SecretSlots...)
	s.Command = append([]string(nil), s.Command...)
	s.HealthCommand = append([]string(nil), s.HealthCommand...)
	s.Models = append([]Model(nil), s.Models...)
	s.Modes = append([]Mode(nil), s.Modes...)
	s.DisallowedTools = append([]string(nil), s.DisallowedTools...)
	s.OwnModels = append([]Model(nil), s.OwnModels...)
	s.OwnDisallowedTools = append([]string(nil), s.OwnDisallowedTools...)
	if s.ModelAliases != nil {
		al := make(map[string]string, len(s.ModelAliases))
		for k, v := range s.ModelAliases {
			al[k] = v
		}
		s.ModelAliases = al
	}
	if s.Env != nil {
		env := make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			env[k] = v
		}
		s.Env = env
	}
	if s.SecretEnv != nil {
		secrets := make(map[string]string, len(s.SecretEnv))
		for k, v := range s.SecretEnv {
			secrets[k] = v
		}
		s.SecretEnv = secrets
	}
	s.PassthroughEnv = append([]string(nil), s.PassthroughEnv...)
	return s
}

// List returns every provider in display order: built-ins first, then custom
// entries and instances in registration order.
func (r *Registry) List() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Spec, len(r.specs))
	for i, s := range r.specs {
		out[i] = cloneSpec(s)
	}
	return out
}

// Get returns the provider with id.
func (r *Registry) Get(id string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.specs {
		if s.ID == id {
			return cloneSpec(s), true
		}
	}
	return Spec{}, false
}

// RunnerOf is the runner of id, "" for an unknown provider.
func (r *Registry) RunnerOf(id string) Runner {
	s, _ := r.Get(id)
	return s.Runner
}

// BaseOf is the id of the provider whose code runs id ("" if unknown).
func (r *Registry) BaseOf(id string) string {
	s, ok := r.Get(id)
	if !ok {
		return ""
	}
	return s.Base()
}

// resolve fills an entry's derived fields and checks it against the table
// built so far (taken by value: the caller owns the slice).
func resolve(s Spec, table []Spec) (Spec, error) {
	if !ValidVendor(s.Vendor) {
		return s, fmt.Errorf("vendor %q: must be zai, anthropic, openai or generic", s.Vendor)
	}
	s = cloneSpec(s)
	if strings.TrimSpace(s.ID) == "" {
		return s, fmt.Errorf("id: required")
	}
	for _, t := range table {
		if t.ID == s.ID {
			return s, fmt.Errorf("id %q: duplicate provider id", s.ID)
		}
	}
	if s.Label == "" {
		s.Label = s.ID
	}
	if s.Extends != "" {
		var base *Spec
		for i := range table {
			// Any harness-bearing provider that is not itself an instance:
			// a built-in, or an earlier custom ACP entry.
			if table[i].ID == s.Extends && !table[i].IsInstance() {
				base = &table[i]
			}
		}
		if base == nil || base.Runner == "" {
			return s, fmt.Errorf("extends %q: must name a built-in provider or an earlier providers.custom entry that is not itself an instance", s.Extends)
		}
		// An instance is its base's code with its own id, label and
		// overlay: everything else is inherited, but the reflector (which
		// runs on the base login) and the smind-held account row stay the
		// base's.
		inst := *base
		inst.ID, inst.Label, inst.Extends, inst.Custom = s.ID, s.Label, base.ID, true
		// The instance's own launch env (config providers.custom[].env) is an
		// overlay on its base's, not a replacement: an opencode instance must
		// keep the base's OPENCODE_PERMISSION ask policy for bash and edit.
		inst.Env = overlayEnv(base.Env, s.Env)
		inst.SecretEnv = s.SecretEnv
		inst.PassthroughEnv = s.PassthroughEnv
		if len(s.Command) > 0 {
			inst.Command = s.Command
			inst.HealthCommand = []string{s.Command[0]}
		}
		inst.ModelsMode = s.ModelsMode
		inst.OwnModels = append([]Model(nil), s.Models...)
		inst.OwnDisallowedTools = append([]string(nil), s.DisallowedTools...)
		switch {
		case len(s.Models) == 0:
			// keep the base's list (Claude's catalog lives in taskrunner)
		case s.ModelsMode == ModelsAdd:
			inst.Models = MergeModels(base.Models, s.Models)
		default:
			inst.Models = s.Models
		}
		inst.ModelAliases = s.ModelAliases
		inst.NoMCPServers = base.NoMCPServers || s.NoMCPServers
		inst.Endpoint = s.Endpoint
		inst.DisallowedTools = append(append([]string(nil), base.DisallowedTools...), s.DisallowedTools...)
		inst.Thinking, inst.Recipe, inst.InheritClaudeConfig = s.Thinking, s.Recipe, s.InheritClaudeConfig
		switch {
		case s.Vendor != "":
			inst.Vendor = s.Vendor
		case s.Env["ANTHROPIC_BASE_URL"] != "" || s.Env["OPENAI_BASE_URL"] != "" || s.Endpoint.Kind != "":
			inst.Vendor = VendorGeneric
		default:
			// keep the base's vendor (inst is a copy of base)
		}
		inst.Reflect = false
		inst.AccountProvider = ""
		inst.CredentialKind = ""
		return cloneSpec(inst), nil
	}
	if s.Runner == "" {
		s.Runner = RunnerACP
	}
	if s.Runner != RunnerACP {
		return s, fmt.Errorf("runner %q: only %q is supported for a custom provider", s.Runner, RunnerACP)
	}
	if len(s.Command) == 0 || strings.TrimSpace(s.Command[0]) == "" {
		return s, fmt.Errorf("command: required")
	}
	s.Custom = true
	if s.Images == "" {
		s.Images = "unknown"
	}
	if s.HealthCommand == nil {
		s.HealthCommand = []string{s.Command[0]}
	}
	// Custom ACP agents use the ACP session machinery like glm/kimi.
	s.SupportsAutoAccept, s.LiveModeSwitch = true, true
	s.ToolEvents, s.Delivery = true, DeliveryPreface
	if s.DefaultMode == "" && len(s.Modes) > 0 {
		s.DefaultMode = s.Modes[0].ID
	}
	return s, nil
}

// Register adds one provider (a test double, a plugin's entry): the id must
// be unused, the entry well formed. It never replaces an existing entry.
func (r *Registry) Register(s Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := resolve(s, r.specs)
	if err != nil {
		return err
	}
	r.specs = append(r.specs, s)
	r.custom = append(r.custom, s)
	return nil
}

// Unregister removes a non-built-in provider; it reports whether one was removed.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.specs {
		if s.ID == id && !IsBuiltin(id) {
			r.specs = append(r.specs[:i:i], r.specs[i+1:]...)
			for j, c := range r.custom {
				if c.ID == id {
					r.custom = append(r.custom[:j:j], r.custom[j+1:]...)
					break
				}
			}
			return true
		}
	}
	return false
}

// SetCustom atomically replaces every custom entry (the providers.custom
// config) with specs, in order, keeping the built-ins and the plugin layer.
// An invalid entry rejects the whole set and changes nothing; the error names
// the index and field ("[2].command: required").
func (r *Registry) SetCustom(specs []Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	table := builtinsWithVendors()
	var custom []Spec
	for i, s := range specs {
		if ReservedID(s.ID) {
			return fmt.Errorf("[%d].id: %q is reserved (built-in or reserved prefix)", i, s.ID)
		}
		res, err := resolve(s, table)
		if err != nil {
			return fmt.Errorf("[%d].%w", i, err)
		}
		table = append(table, res)
		custom = append(custom, res)
	}
	r.custom = custom
	return r.rebuildLocked()
}

// PluginProblem is one plugin's providers that were left out and why.
type PluginProblem struct {
	Plugin string
	Err    error
}

// SetPlugins atomically replaces the plugin layer: the providers of each
// active plugin, keyed by plugin id (Wave D). The table is rebuilt as
// built-ins, custom entries, then plugins sorted by id, each in manifest
// order. A plugin whose entries do not resolve (a bad id, a duplicate) is left
// out whole and reported; the others and the other layers are unaffected.
func (r *Registry) SetPlugins(byPlugin map[string][]Spec) []PluginProblem {
	r.mu.Lock()
	defer r.mu.Unlock()
	table := append(builtinsWithVendors(), r.custom...)
	ids := make([]string, 0, len(byPlugin))
	for id := range byPlugin {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	layer := map[string][]Spec{}
	var problems []PluginProblem
	for _, pid := range ids {
		var resolved []Spec
		scratch := append([]Spec(nil), table...)
		var perr error
		for _, s := range byPlugin[pid] {
			if s.Plugin != pid || !strings.HasPrefix(s.ID, pid+"/") || strings.Count(s.ID, "/") != 1 {
				perr = fmt.Errorf("provider %q: a plugin provider id must be %q/<provider>", s.ID, pid)
				break
			}
			if s.Extends != "" {
				perr = fmt.Errorf("provider %q: a plugin provider cannot extend another provider", s.ID)
				break
			}
			res, err := resolve(s, scratch)
			if err != nil {
				perr = fmt.Errorf("provider %q: %w", s.ID, err)
				break
			}
			res.Plugin, res.Reflect, res.AccountProvider, res.CredentialKind = pid, false, "", ""
			scratch = append(scratch, res)
			resolved = append(resolved, res)
		}
		if perr != nil {
			problems = append(problems, PluginProblem{Plugin: pid, Err: perr})
			continue
		}
		table = append(table, resolved...)
		layer[pid] = resolved
	}
	r.plugins = layer
	_ = r.rebuildLocked()
	return problems
}

// rebuildLocked recomputes specs from the layers. Callers hold r.mu.
func (r *Registry) rebuildLocked() error {
	table := builtinsWithVendors()
	table = append(table, r.custom...)
	ids := make([]string, 0, len(r.plugins))
	for id := range r.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		table = append(table, r.plugins[id]...)
	}
	r.specs = table
	return nil
}

// IDs lists every registered id, sorted (diagnostics, tests).
func (r *Registry) IDs() []string {
	var ids []string
	for _, s := range r.List() {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

// The package-level helpers read the Default registry; they exist for
// callers that hold only a provider id string (learning, the run registry).

// ToolEvents reports whether provider streams tool-call events. Unknown
// providers do not (fail closed).
func ToolEvents(provider string) bool { s, _ := Default().Get(provider); return s.ToolEvents }

// CanReflect reports whether provider's runs may feed the reflector.
func CanReflect(provider string) bool { s, _ := Default().Get(provider); return s.Reflect }

// Delivery is the learning-delivery mode of provider ("" for none/unknown).
func Delivery(provider string) string { s, _ := Default().Get(provider); return s.Delivery }

// IsClaude reports whether provider runs on the Claude Code CLI (the base
// claude-native provider or an instance of it).
func IsClaude(provider string) bool { return Default().RunnerOf(provider) == RunnerClaude }

// MergeModels returns base plus extra merged by id: an extra entry replaces a
// base entry of the same id in place, new ids append in order.
func MergeModels(base, extra []Model) []Model {
	out := append([]Model(nil), base...)
	for _, m := range extra {
		replaced := false
		for i := range out {
			if out[i].ID == m.ID {
				out[i], replaced = m, true
				break
			}
		}
		if !replaced {
			out = append(out, m)
		}
	}
	return out
}

// BuiltinRunner is the runner of a built-in provider id ("" if id is not one).
func BuiltinRunner(id string) Runner {
	for _, s := range builtins() {
		if s.ID == id {
			return s.Runner
		}
	}
	return ""
}
