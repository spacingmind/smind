package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/acp"
)

// claudeModelConfigID is the id (and category) of the synthetic model
// config option a live claude-native session lists (ADR-0025): Claude Code
// has no ACP config options, so smind presents its model switch through the
// same surface ACP agents use.
const claudeModelConfigID = "model"

// ErrCodexModelNextRun is what run.setConfigOption answers for a Codex
// "model": the model only applies when a thread starts (thread/start's
// `model`), so it cannot change mid-run.
var ErrCodexModelNextRun = errors.New("taskrunner: set config option: the codex model applies from the next run: pass `model` to task_send")

func (r *Runner) setClaudeModel(chatID int64, model string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	r.claudeModels[chatID] = model
}

// claudeModelOption is the synthetic select option: the static catalog
// preceded by a "default" choice that resets to the provider default.
func claudeModelOption(current string) acp.ConfigOption {
	if current == "" {
		current = ClaudeModelDefault
	}
	choices := []acp.ConfigSelectOption{{Value: ClaudeModelDefault, Name: "Default", Description: "The Claude Code default model"}}
	for _, m := range claudeModels() {
		choices = append(choices, acp.ConfigSelectOption{Value: m.ID, Name: m.Label, Description: m.Description})
	}
	cur, _ := json.Marshal(current)
	return acp.ConfigOption{
		ConfigID:     claudeModelConfigID,
		Name:         "Model",
		Category:     acpModelConfigCategory,
		Type:         "select",
		CurrentValue: cur,
		Options:      choices,
	}
}

// claudeConfigOptions lists the synthetic model option for chatID's live
// claude-native session, and nothing once the turn is over (there is no
// session left to switch).
func (r *Runner) claudeConfigOptions(chatID int64) []acp.ConfigOption {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if r.claudeClients[chatID] == nil {
		return nil
	}
	return []acp.ConfigOption{claudeModelOption(r.claudeModels[chatID])}
}

// setClaudeConfigOption routes the synthetic "model" option to
// Client.SetModel: the catalog's default (or an empty value) selects the
// configured default model; anything else must pass ValidateModelSyntax (the
// catalog is not an allowlist). Takes effect for the session's next turn.
func (r *Runner) setClaudeConfigOption(ctx context.Context, chatID int64, configID, value string) ([]acp.ConfigOption, error) {
	if configID != claudeModelConfigID {
		return nil, fmt.Errorf("taskrunner: set config option: %s: no such config option %q: %w", ProviderClaudeNative, configID, ErrConfigOptionsNotSupported)
	}
	if err := ValidateModelSyntax(value); err != nil {
		return nil, fmt.Errorf("taskrunner: set config option: %w", err)
	}
	r.sessionMu.Lock()
	client := r.claudeClients[chatID]
	r.sessionMu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("taskrunner: set config option: no live claude-native session for chat %d", chatID)
	}
	// "default" (or empty) is the configured default, never SetModel(nil): a
	// reset would hand the session to the CLI's own default, possibly Opus.
	// With none configured there is nothing to reset to, so it is refused.
	if value == "" || value == ClaudeModelDefault {
		def, err := r.ResolveModel(ProviderClaudeNative, "")
		if err != nil {
			return nil, fmt.Errorf("taskrunner: set config option: %w", err)
		}
		value = def
	}
	model := &value
	if err := client.SetModel(ctx, model); err != nil {
		return nil, fmt.Errorf("taskrunner: set model on chat %d: %w", chatID, err)
	}
	r.sessionMu.Lock()
	if r.claudeClients[chatID] != nil {
		r.claudeModels[chatID] = value
	}
	r.sessionMu.Unlock()
	return []acp.ConfigOption{claudeModelOption(value)}, nil
}
