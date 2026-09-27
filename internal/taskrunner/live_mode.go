package taskrunner

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/acp"
)

// claudeAllowBypassFlag is Claude Code's --allow-dangerously-skip-permissions
// (sent via claudecode.WithExtraArgs, which adds the leading dashes): it
// lets a session later switch into bypassPermissions mid-run. Paseo always
// launches with it (allowDangerouslySkipPermissions: true) for the same
// reason; on its own it changes nothing about what the session may do.
const claudeAllowBypassFlag = "allow-dangerously-skip-permissions"

// ErrModeSwitchNotSupported is returned (wrapped) by SetPermissionMode for
// a provider whose permission mode only applies when its session starts
// -- Codex, whose approvalPolicy/sandbox ride on thread/start.
var ErrModeSwitchNotSupported = errors.New("switching permission mode mid-run is not supported for this provider")

// trackClaudeClient records chatID's live Claude Code client so
// SetPermissionMode can reach it mid-turn.
func (r *Runner) trackClaudeClient(chatID int64, client claudeBackend) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	r.claudeClients[chatID] = client
}

func (r *Runner) untrackClaudeClient(chatID int64) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	delete(r.claudeClients, chatID)
}

// SetPermissionMode switches chatID's live session to mode, using the
// provider's own mechanism (ADR-0019): Claude Code's set_permission_mode
// control request, or an ACP agent's session/set_mode (or its mode config
// option). Codex fails with ErrModeSwitchNotSupported. A chat with no live
// session fails rather than silently no-opping.
func (r *Runner) SetPermissionMode(ctx context.Context, chatID int64, provider Provider, mode string) error {
	switch {
	case provider == ProviderClaudeNative:
		r.sessionMu.Lock()
		client := r.claudeClients[chatID]
		r.sessionMu.Unlock()
		if client == nil {
			return fmt.Errorf("taskrunner: set permission mode: no live claude-native session for chat %d", chatID)
		}
		if err := client.SetPermissionMode(ctx, mode); err != nil {
			return fmt.Errorf("taskrunner: set permission mode on chat %d: %w", chatID, err)
		}
		return nil
	case acpProvider(provider):
		r.sessionMu.Lock()
		s, ok := r.acpSessions[chatID]
		var (
			client    acpBackend
			sessionID string
			options   []acp.ConfigOption
		)
		if ok {
			client, sessionID, options = s.client, s.sessionID, s.options
		}
		r.sessionMu.Unlock()
		if client == nil {
			return fmt.Errorf("taskrunner: set permission mode: no live ACP session for chat %d", chatID)
		}
		updated, err := applyACPMode(ctx, client, sessionID, options, mode)
		if err != nil {
			return fmt.Errorf("taskrunner: set permission mode on chat %d: %w", chatID, err)
		}
		r.updateACPSessionOptions(chatID, sessionID, updated)
		return nil
	default:
		return fmt.Errorf("taskrunner: set permission mode: %s: %w", provider, ErrModeSwitchNotSupported)
	}
}

// applyACPMode puts sessionID into mode, using whichever mechanism the
// agent advertised: session/set_mode for SessionModeState modes (whose
// current mode the acp.Client keeps up to date, including the agent's own
// current_mode_update notifications), or session/set_config_option for a
// category-"mode" config option. An empty mode, or the mode the session is
// already in, sends nothing. An agent advertising no modes at all accepts
// only ACPModeDefault (the "leave it as it starts" fallback) and fails for
// anything else.
//
// It returns options as they stand after the call -- merged with the
// agent's set_config_option response when that path was taken -- so the
// caller can keep its tracked copy current; otherwise the next switch
// would compare against the currentValue captured at session start.
func applyACPMode(ctx context.Context, client acpBackend, sessionID string, options []acp.ConfigOption, mode string) ([]acp.ConfigOption, error) {
	if mode == "" {
		return options, nil
	}
	if m, ok := client.SessionModes(sessionID); ok && len(m.AvailableModes) > 0 {
		if m.CurrentModeID == mode || (mode == ACPModeDefault && !hasSessionMode(m, mode)) {
			return options, nil
		}
		return options, client.SetSessionMode(ctx, sessionID, mode)
	}
	if c, ok := deriveACPModes(nil, options); ok {
		if c.defaultMode == mode || (mode == ACPModeDefault && !hasCatalogMode(c, mode)) {
			return options, nil
		}
		returned, err := client.SetSessionConfigOption(ctx, sessionID, c.configID, mode)
		if err != nil {
			return options, err
		}
		return mergeConfigOptions(options, returned), nil
	}
	if mode == ACPModeDefault {
		return options, nil
	}
	return options, fmt.Errorf("agent advertises no permission modes")
}

// hasSessionMode / hasCatalogMode report whether the agent actually
// advertises a mode with id -- ACPModeDefault is only ever *sent* to an
// agent that has a mode by that name; for any other agent it is smind's
// "leave the agent in its own start mode" fallback (the id a run gets
// before the agent's own modes are discovered).
func hasSessionMode(m acp.SessionModeState, id string) bool {
	for _, am := range m.AvailableModes {
		if am.ID == id {
			return true
		}
	}
	return false
}

func hasCatalogMode(c acpModeCatalog, id string) bool {
	for _, cm := range c.modes {
		if cm.ID == id {
			return true
		}
	}
	return false
}

// currentACPMode reports the mode sessionID is actually in, from the
// agent's SessionModeState or its mode config option; ok is false for an
// agent that advertises no modes at all.
func currentACPMode(client acpBackend, sessionID string, options []acp.ConfigOption) (string, bool) {
	if m, ok := client.SessionModes(sessionID); ok && len(m.AvailableModes) > 0 {
		return m.CurrentModeID, m.CurrentModeID != ""
	}
	if c, ok := deriveACPModes(nil, options); ok {
		return c.defaultMode, c.defaultMode != ""
	}
	return "", false
}

// mergeConfigOptions overlays an agent's set_config_option response onto
// the tracked option list. ACP says the response is the full list, but
// agents in the wild echo only the changed option, sometimes without its
// category/type/choices -- so a returned option replaces its tracked twin
// field by field, keeping whatever the response left empty, and a
// returned option the list didn't have is appended.
func mergeConfigOptions(existing, returned []acp.ConfigOption) []acp.ConfigOption {
	out := append([]acp.ConfigOption(nil), existing...)
	for _, r := range returned {
		i := -1
		for j := range out {
			if out[j].ConfigID == r.ConfigID {
				i = j
				break
			}
		}
		if i < 0 {
			out = append(out, r)
			continue
		}
		old := out[i]
		if r.Name == "" {
			r.Name = old.Name
		}
		if r.Description == "" {
			r.Description = old.Description
		}
		if r.Category == "" {
			r.Category = old.Category
		}
		if r.Type == "" {
			r.Type = old.Type
		}
		if len(r.CurrentValue) == 0 {
			r.CurrentValue = old.CurrentValue
		}
		if len(r.Options) == 0 {
			r.Options = old.Options
		}
		out[i] = r
	}
	return out
}
