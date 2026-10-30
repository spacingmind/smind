package taskrunner

import (
	"fmt"
	"os"
	"strings"

	"github.com/spacingmind/smind/internal/providers"
)

// ModeInfo is one of a provider's own permission modes (ADR-0019): the id
// is the provider's native vocabulary (Claude Code's --permission-mode
// value, a Codex preset name, or an ACP agent's advertised session mode
// id), and Label/Description are the provider's own words for it -- smind
// never invents a generic tier on top.
type ModeInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// AutoApproves marks a mode in which the provider itself lets tool
	// calls run without ever asking a human (a bypass mode, Claude's
	// classifier-driven "auto", Codex full-access). An orchestrating agent
	// may not pick one of these on its own (ADR-0019 decision 6).
	AutoApproves bool `json:"autoApproves,omitempty"`
}

// Claude Code's permission mode ids -- the CLI's own --permission-mode
// values (refs/paseo/.../providers/claude/agent.ts's DEFAULT_MODES).
const (
	ClaudeModeDefault     = "default"
	ClaudeModeAcceptEdits = "acceptEdits"
	ClaudeModePlan        = "plan"
	ClaudeModeAuto        = "auto"
	ClaudeModeBypass      = "bypassPermissions"
)

// Codex preset ids (refs/paseo/.../providers/codex-app-server-agent.ts's
// CODEX_MODES), each mapped to Codex's own approvalPolicy/sandbox pair by
// codexModePresets.
const (
	CodexModeAuto       = "auto"
	CodexModeFullAccess = "full-access"
)

// ACPModeDefault is the fallback mode id for an ACP agent that advertises
// no session modes at all (or whose modes haven't been discovered yet): it
// means "leave the agent in whatever mode it starts in" -- runACP never
// sends session/set_mode for it unless the agent actually advertises a
// mode by that id.
const ACPModeDefault = "default"

// claudeModes is Claude Code's permission mode catalog. acceptEdits comes
// first and is the default (ADR-0019 resolved decision 1): under plain
// "default", the CLI was observed (2026-09-11) blocking file edits itself
// in headless mode without ever emitting a can_use_tool request.
func claudeModes() []ModeInfo {
	modes := []ModeInfo{
		{ID: ClaudeModeAcceptEdits, Label: "Accept File Edits", Description: "Automatically approves edit-focused tools without prompting"},
		{ID: ClaudeModeDefault, Label: "Always Ask", Description: "Prompts for permission the first time a tool is used"},
		{ID: ClaudeModePlan, Label: "Plan Mode", Description: "Analyze the codebase without executing tools or edits"},
	}
	if claudeAutoModeAvailable() {
		modes = append(modes, ModeInfo{ID: ClaudeModeAuto, Label: "Auto mode", Description: "Uses a model classifier to review permission prompts automatically", AutoApproves: true})
	}
	return append(modes, ModeInfo{ID: ClaudeModeBypass, Label: "Bypass", Description: "Skip all permission prompts (use with caution)", AutoApproves: true})
}

// claudeAutoModeAvailable mirrors Paseo's claudeAutoModeUnavailableOn:
// Claude Code's auto mode needs the Anthropic API directly, not Bedrock or
// Vertex.
func claudeAutoModeAvailable() bool {
	for _, env := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(env))) {
		case "", "0", "false", "no":
		default:
			return false
		}
	}
	return true
}

func codexModes() []ModeInfo {
	return []ModeInfo{
		{ID: CodexModeAuto, Label: "Default Permissions", Description: "Edit files and run commands with Codex's default approval flow."},
		{ID: CodexModeFullAccess, Label: "Full Access", Description: "Edit files, run commands, and access the network without additional prompts.", AutoApproves: true},
	}
}

// codexModePreset is the approvalPolicy/sandbox pair a Codex mode sends
// on thread/start and thread/resume.
type codexModePreset struct {
	approvalPolicy string
	sandbox        string
}

var codexModePresets = map[string]codexModePreset{
	CodexModeAuto:       {approvalPolicy: "on-request", sandbox: "workspace-write"},
	CodexModeFullAccess: {approvalPolicy: "never", sandbox: "danger-full-access"},
}

// acpFallbackModes is the catalog for an ACP provider whose agent's own
// modes haven't been discovered (see Runner.ProviderCatalog).
func acpFallbackModes() []ModeInfo {
	return []ModeInfo{{ID: ACPModeDefault, Label: "Default", Description: "The agent's own starting mode"}}
}

// acpBypassLike reports whether an ACP agent-advertised mode id reads as
// a bypass/auto-approve mode (e.g. GLM's "bypass_permissions"), for
// ModeInfo.AutoApproves -- the agent's schema carries no such flag, so
// this is a conservative name heuristic that only ever marks *more*
// modes as auto-approving, never fewer.
func acpBypassLike(id string) bool {
	id = strings.ToLower(id)
	for _, s := range []string{"bypass", "yolo", "full", "auto", "dangerous", "skip"} {
		if strings.Contains(id, s) {
			return true
		}
	}
	return false
}

// staticModes returns s's built-in mode catalog and default mode id -- the
// discovered-at-runtime ACP catalog aside (see Runner.ProviderCatalog), this
// is the full answer. A custom ACP provider's own modes (providers.custom
// `modes`) win over the single-entry fallback.
func staticModes(s providers.Spec) (modes []ModeInfo, defaultMode string) {
	switch s.Runner {
	case providers.RunnerClaude:
		return claudeModes(), ClaudeModeAcceptEdits
	case providers.RunnerCodex:
		return codexModes(), CodexModeAuto
	}
	if len(s.Modes) > 0 {
		for _, m := range s.Modes {
			modes = append(modes, ModeInfo{ID: m.ID, Label: m.Label, Description: m.Description, AutoApproves: m.AutoApproves})
		}
		return modes, s.DefaultMode
	}
	return acpFallbackModes(), ACPModeDefault
}

// PermissionSettings is a run's provider-native permission configuration
// (ADR-0019): Mode is one of the provider's own ModeInfo ids ("" means
// the provider's default), and AutoAccept -- ACP providers only --
// approves every session/request_permission without asking a human
// (Paseo's auto_accept toggle).
type PermissionSettings struct {
	Mode       string
	AutoAccept bool
}

// autoApprovingModeHint points a caller who asked for autoAccept on a provider
// without it at that provider's own auto-approving modes (ADR-0019: Claude and
// Codex have modes for this, only ACP agents have the autoAccept toggle), so the
// error says what to use instead. "" when the catalog has no such mode.
func autoApprovingModeHint(catalog ProviderInfo) string {
	var ids []string
	for _, m := range catalog.Modes {
		if m.AutoApproves {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return fmt.Sprintf(" (this provider has its own permission modes: pass --mode with one that approves for you: %s; a human must choose it)", strings.Join(ids, ", "))
}

// ValidatePermissionSettings checks s against provider's catalog, as
// served by catalog (Runner.ProviderCatalog's entry for provider). An
// unknown mode fails with the valid ids listed; AutoAccept on a provider
// that doesn't support it fails too. For an ACP provider whose modes
// haven't been discovered (catalog.ModesDiscovered false), any mode id is
// accepted -- the agent itself is the only authority, and runACP fails the
// run with the agent's own error if it rejects the id.
func ValidatePermissionSettings(catalog ProviderInfo, s PermissionSettings) error {
	if s.AutoAccept && !catalog.SupportsAutoAccept {
		return fmt.Errorf("autoAccept is not supported for provider %q%s", catalog.ID, autoApprovingModeHint(catalog))
	}
	if s.Mode == "" {
		return nil
	}
	if catalog.Runner == string(providers.RunnerACP) && !catalog.ModesDiscovered {
		return nil
	}
	ids := make([]string, len(catalog.Modes))
	for i, m := range catalog.Modes {
		if m.ID == s.Mode {
			return nil
		}
		ids[i] = m.ID
	}
	return fmt.Errorf("invalid permission mode %q for provider %q (valid: %s)", s.Mode, catalog.ID, strings.Join(ids, ", "))
}

// ModeAutoApproves reports whether s lets provider run tool calls with no
// human asked: AutoAccept, or a Mode catalog marks AutoApproves. An empty
// Mode resolves to catalog.DefaultMode first.
func ModeAutoApproves(catalog ProviderInfo, s PermissionSettings) bool {
	if s.AutoAccept {
		return true
	}
	mode := s.Mode
	if mode == "" {
		mode = catalog.DefaultMode
	}
	for _, m := range catalog.Modes {
		if m.ID == mode {
			return m.AutoApproves
		}
	}
	return catalog.Runner == string(providers.RunnerACP) && acpBypassLike(mode)
}

// ProviderCatalog returns SupportedProviders with every ACP provider's
// discovered session modes layered on (see acpModeCache), for
// provider.list and for run-start validation. It never blocks on
// discovery.
func (r *Runner) ProviderCatalog() []ProviderInfo {
	infos := providerInfos(r.providerRegistry())
	for i := range infos {
		infos[i].DefaultModel = r.configuredDefaultModel(infos[i].ID)
	}
	reg := r.providerRegistry()
	for i, p := range infos {
		spec, _ := reg.Get(string(p.ID))
		// An instance that sets models replaces the agent's own list
		// (ADR-0046 precedence: models > discovery); additionalModels merge
		// into the discovered one below.
		pinned := spec.IsInstance() && len(spec.Models) > 0 && spec.ModelsMode != providers.ModelsAdd
		add := spec.IsInstance() && len(spec.Models) > 0 && spec.ModelsMode == providers.ModelsAdd
		switch providers.Runner(p.Runner) {
		case providers.RunnerACP:
			c, ok := r.acpCatalog(p.ID)
			m, mok := r.acpModelCatalog(p.ID)
			if !ok && !mok {
				r.maybeProbeACPModes(p.ID)
			}
			if ok {
				infos[i].Modes = append([]ModeInfo(nil), c.modes...)
				infos[i].DefaultMode = c.defaultMode
				infos[i].ModesDiscovered = true
			}
			if mok && !pinned {
				infos[i].Models = discoveredWithExtras(m.models, spec, add)
				infos[i].ModelsDiscovered = true
			}
		case providers.RunnerCodex:
			m, ok := r.acpModelCatalog(p.ID)
			if !ok {
				r.maybeProbe(p.ID, r.probeCodexModels)
				continue
			}
			if !pinned {
				infos[i].Models = discoveredWithExtras(m.models, spec, add)
				infos[i].ModelsDiscovered = true
			}
		}
	}
	return infos
}

// discoveredWithExtras is a discovered catalog, plus (add) the instance's
// additionalModels merged by id.
func discoveredWithExtras(discovered []ModelInfo, spec providers.Spec, add bool) []ModelInfo {
	out := append([]ModelInfo(nil), discovered...)
	if add {
		out = mergeModelInfos(out, configModels(spec.Models))
	}
	return out
}

// configuredDefaultModel is the model a run of provider resolves to when it
// names none and nothing falls back to a base: providers.<id>.defaultModel,
// else, for an instance that sets its own models (replace mode), that list's
// default entry (the one marked default, else the first) -- never the base's
// default, which names a model the instance's endpoint may not serve.
func (r *Runner) configuredDefaultModel(provider Provider) string {
	if m := r.DefaultModel(provider); m != "" {
		return m
	}
	spec, ok := r.providerRegistry().Get(string(provider))
	if !ok || !spec.IsInstance() || len(spec.Models) == 0 || spec.ModelsMode == providers.ModelsAdd {
		return ""
	}
	for _, m := range spec.Models {
		if m.Default {
			return m.ID
		}
	}
	return spec.Models[0].ID
}

// DefaultModel is the configured default model for provider
// (WithDefaultModel), "" when none is set: what a run that names no model
// resolves to. Runs and ProviderInfo.DefaultModel both read it, so the UI shows
// exactly what a run will use.
func (r *Runner) DefaultModel(provider Provider) string {
	r.defaultModelsMu.RLock()
	defer r.defaultModelsMu.RUnlock()
	return r.defaultModels[provider]
}

// ProviderCatalogFor returns ProviderCatalog's entry for provider, and
// false for an unknown one.
func (r *Runner) ProviderCatalogFor(provider Provider) (ProviderInfo, bool) {
	for _, p := range r.ProviderCatalog() {
		if p.ID == provider {
			return p, true
		}
	}
	return ProviderInfo{}, false
}
