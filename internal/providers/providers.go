// Package providers is the daemon's provider registry (ADR-0042, plugins plan
// P0.1): every provider is ONE Spec, and everything that used to switch on a
// provider id (the runner's dispatch, permission modes, the health check,
// learning delivery, the config allowlist) reads the Spec instead.
//
// It is a leaf package (it imports nothing from smind) so config, learning and
// taskrunner can all depend on it. A built-in provider, a providers.custom ACP
// entry and a provider instance (an entry that extends claude-native or
// codex-native with its own launch env) are all the same type; adding an ACP
// provider needs a Spec and no code edits elsewhere.
package providers

import (
	"fmt"
	"sort"
	"strings"
	"sync"
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

// Built-in provider ids.
const (
	IDClaudeNative = "claude-native"
	IDCodexNative  = "codex-native"
	IDGLM          = "glm"
	IDKimi         = "kimi"
)

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

// Mode is one entry of a provider's static permission-mode catalog.
type Mode struct {
	ID, Label, Description string
	AutoApproves           bool
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

	// Command and Env launch an ACP provider (Command is argv; Env is the
	// free-form environment of that command). Instances use the registry's
	// per-provider launch env (config providers.custom[].env), not Env.
	Command []string
	Env     map[string]string

	// Models and Modes are a custom ACP provider's static catalogs (ACP
	// discovery layers the agent's own list on top, as for glm/kimi).
	Models      []Model
	Modes       []Mode
	DefaultMode string

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

	// Learning capabilities (ADR-0026).
	ToolEvents bool   // the run streams tool-call events
	Reflect    bool   // runs may feed the reflector
	Delivery   string // DeliveryProjection | DeliveryPreface | ""
}

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

func builtins() []Spec {
	return []Spec{
		{ID: IDClaudeNative, Label: "Claude Code", Runner: RunnerClaude, CredentialKind: "oauth", AccountProvider: "anthropic",
			LiveModeSwitch: true, Images: "yes", HealthCommand: []string{"claude"},
			ToolEvents: true, Reflect: true, Delivery: DeliveryProjection},
		{ID: IDGLM, Label: "GLM", Runner: RunnerACP, Kind: "cli", SupportsAutoAccept: true, LiveModeSwitch: true, Images: "no",
			HealthCommand: []string{"npx", "-y", "glm-acp-agent"}, ToolEvents: true, Delivery: DeliveryPreface},
		{ID: IDKimi, Label: "Kimi", Runner: RunnerACP, CredentialKind: "api-key", AccountProvider: "kimi", SupportsAutoAccept: true,
			LiveModeSwitch: true, Images: "unknown", ToolEvents: true, Delivery: DeliveryPreface},
		{ID: IDCodexNative, Label: "Codex", Runner: RunnerCodex, CredentialKind: "oauth", AccountProvider: "openai", Images: "yes",
			HealthCommand: []string{"codex"}},
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
}

// New returns a registry holding the built-in providers.
func New() *Registry { return &Registry{specs: builtins()} }

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
	s.Command = append([]string(nil), s.Command...)
	s.HealthCommand = append([]string(nil), s.HealthCommand...)
	s.Models = append([]Model(nil), s.Models...)
	s.Modes = append([]Mode(nil), s.Modes...)
	if s.Env != nil {
		env := make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			env[k] = v
		}
		s.Env = env
	}
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
			if table[i].ID == s.Extends && !table[i].Custom {
				base = &table[i]
			}
		}
		if base == nil || (base.Runner != RunnerClaude && base.Runner != RunnerCodex) {
			return s, fmt.Errorf("extends %q: must be %s or %s", s.Extends, IDClaudeNative, IDCodexNative)
		}
		// An instance is its base's code with its own id, label and env:
		// everything else is inherited, but the reflector (which runs on
		// the base login) and the smind-held account row stay the base's.
		inst := *base
		inst.ID, inst.Label, inst.Extends, inst.Custom = s.ID, s.Label, base.ID, true
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
	return nil
}

// Unregister removes a non-built-in provider; it reports whether one was removed.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.specs {
		if s.ID == id && !IsBuiltin(id) {
			r.specs = append(r.specs[:i:i], r.specs[i+1:]...)
			return true
		}
	}
	return false
}

// SetCustom atomically replaces every custom entry (the providers.custom
// config) with specs, in order, keeping the built-ins. An invalid entry
// rejects the whole set and changes nothing; the error names the index and
// field ("[2].command: required").
func (r *Registry) SetCustom(specs []Spec) error {
	var table []Spec
	for _, s := range builtins() {
		table = append(table, s)
	}
	for i, s := range specs {
		if ReservedID(s.ID) {
			return fmt.Errorf("[%d].id: %q is reserved (built-in or reserved prefix)", i, s.ID)
		}
		res, err := resolve(s, table)
		if err != nil {
			return fmt.Errorf("[%d].%w", i, err)
		}
		table = append(table, res)
	}
	r.mu.Lock()
	r.specs = table
	r.mu.Unlock()
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
