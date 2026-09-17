package taskrunner

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/acp"
)

// ErrConfigOptionsNotSupported is returned (wrapped) by
// SetSessionConfigOption for providers whose agent protocol isn't ACP and
// therefore has no session config options at all -- Claude Code native and
// Codex native. Callers use errors.Is to distinguish "this provider can
// never do this" from "this ACP session isn't live right now".
var ErrConfigOptionsNotSupported = errors.New("config options are not supported for this provider")

// acpSessionState is Runner's bookkeeping for one task's ACP session.
// options is populated exactly once, at session creation, and retained
// after the turn ends (ConfigOptions keeps reporting what the agent
// advertised); client is the live session's connection and is set to nil
// the moment the turn's subprocess exits, since SetSessionConfigOption can
// only ever apply to a session that's still alive.
type acpSessionState struct {
	sessionID string
	client    acpBackend
	options   []acp.ConfigOption
}

// trackACPSession records (under Runner's own lock) the session runACP
// just created, replacing any prior state for the task -- a task's turns
// are strictly one-session-at-a-time from the Registry's perspective (a
// second Start on the same task spawns its own client), and the newest
// session is the only one a config-option call could ever reach.
func (r *Runner) trackACPSession(taskID int64, sessionID string, client acpBackend, options []acp.ConfigOption) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	r.acpSessions[taskID] = &acpSessionState{sessionID: sessionID, client: client, options: options}
}

// endACPTurn marks the task's tracked session as no longer live, keeping
// the discovered options. Called from runACP's teardown, before the client
// is closed.
func (r *Runner) endACPTurn(taskID int64) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if s, ok := r.acpSessions[taskID]; ok {
		s.client = nil
	}
}

// acpProvider reports whether provider's turns are driven over ACP -- the
// same distinction RunPrompt's dispatch switch makes, factored out here so
// the config-option methods below share one source of truth for it.
func acpProvider(provider Provider) bool {
	switch provider {
	case ProviderGLM, ProviderKimi:
		return true
	default:
		return false
	}
}

// ConfigOptions returns the config options the agent advertised for
// taskID's ACP session, discovered once at session creation. A provider
// that doesn't speak ACP (claude-native, codex-native), or a task whose
// ACP session hasn't been created yet, reports an empty list.
func (r *Runner) ConfigOptions(taskID int64, provider Provider) []acp.ConfigOption {
	if !acpProvider(provider) {
		return nil
	}
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if s, ok := r.acpSessions[taskID]; ok {
		return append([]acp.ConfigOption(nil), s.options...)
	}
	return nil
}

// SetSessionConfigOption sets one config option on taskID's live ACP
// session (acp.Client.SetSessionConfigOption), returning the session's
// full option list with current values as the agent reports them after
// the change. For a provider that doesn't speak ACP it always fails with
// an error wrapping ErrConfigOptionsNotSupported; for an ACP provider
// whose session isn't currently live (turn not running, or finished), it
// fails rather than silently no-opping.
func (r *Runner) SetSessionConfigOption(ctx context.Context, taskID int64, provider Provider, configID, value string) ([]acp.ConfigOption, error) {
	if !acpProvider(provider) {
		return nil, fmt.Errorf("taskrunner: set config option: %s: %w", provider, ErrConfigOptionsNotSupported)
	}

	r.sessionMu.Lock()
	s, ok := r.acpSessions[taskID]
	var client acpBackend
	var sessionID string
	if ok {
		client, sessionID = s.client, s.sessionID
	}
	r.sessionMu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("taskrunner: set config option: no live ACP session for task %d", taskID)
	}

	opts, err := client.SetSessionConfigOption(ctx, sessionID, configID, value)
	if err != nil {
		return nil, fmt.Errorf("taskrunner: set config option on task %d: %w", taskID, err)
	}
	return opts, nil
}
