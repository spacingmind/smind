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
		if err := applyACPMode(ctx, client, sessionID, options, mode); err != nil {
			return fmt.Errorf("taskrunner: set permission mode on chat %d: %w", chatID, err)
		}
		return nil
	default:
		return fmt.Errorf("taskrunner: set permission mode: %s: %w", provider, ErrModeSwitchNotSupported)
	}
}

// applyACPMode puts sessionID into mode, using whichever mechanism the
// agent advertised: session/set_mode for SessionModeState modes, or
// session/set_config_option for a category-"mode" config option. An empty
// mode, or the mode the session is already in, sends nothing. An agent
// advertising no modes at all accepts only ACPModeDefault (the "leave it
// as it starts" fallback) and fails for anything else.
func applyACPMode(ctx context.Context, client acpBackend, sessionID string, options []acp.ConfigOption, mode string) error {
	if mode == "" {
		return nil
	}
	if m, ok := client.SessionModes(sessionID); ok && len(m.AvailableModes) > 0 {
		if m.CurrentModeID == mode {
			return nil
		}
		return client.SetSessionMode(ctx, sessionID, mode)
	}
	if c, ok := deriveACPModes(nil, options); ok {
		if c.defaultMode == mode {
			return nil
		}
		_, err := client.SetSessionConfigOption(ctx, sessionID, c.configID, mode)
		return err
	}
	if mode == ACPModeDefault {
		return nil
	}
	return fmt.Errorf("agent advertises no permission modes")
}
