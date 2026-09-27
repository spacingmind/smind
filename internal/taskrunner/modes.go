package taskrunner

import (
	"fmt"
	"os"
	"strings"
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

// staticModes returns provider's built-in mode catalog and default mode id
// -- the discovered-at-runtime ACP catalog aside (see
// Runner.ProviderCatalog), this is the full answer.
func staticModes(provider Provider) (modes []ModeInfo, defaultMode string) {
	switch provider {
	case ProviderClaudeNative:
		return claudeModes(), ClaudeModeAcceptEdits
	case ProviderCodexNative:
		return codexModes(), CodexModeAuto
	default:
		return acpFallbackModes(), ACPModeDefault
	}
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

// ValidatePermissionSettings checks s against provider's catalog, as
// served by catalog (Runner.ProviderCatalog's entry for provider). An
// unknown mode fails with the valid ids listed; AutoAccept on a provider
// that doesn't support it fails too. For an ACP provider whose modes
// haven't been discovered (catalog.ModesDiscovered false), any mode id is
// accepted -- the agent itself is the only authority, and runACP fails the
// run with the agent's own error if it rejects the id.
func ValidatePermissionSettings(catalog ProviderInfo, s PermissionSettings) error {
	if s.AutoAccept && !catalog.SupportsAutoAccept {
		return fmt.Errorf("autoAccept is not supported for provider %q", catalog.ID)
	}
	if s.Mode == "" {
		return nil
	}
	if acpProvider(catalog.ID) && !catalog.ModesDiscovered {
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
	return acpProvider(catalog.ID) && acpBypassLike(mode)
}

// ProviderCatalog returns SupportedProviders with every ACP provider's
// discovered session modes layered on (see acpModeCache), for
// provider.list and for run-start validation. It never blocks on
// discovery.
func (r *Runner) ProviderCatalog() []ProviderInfo {
	return SupportedProviders()
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
