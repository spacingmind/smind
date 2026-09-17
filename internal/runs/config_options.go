package runs

import (
	"context"
	"fmt"

	"github.com/spacingmind/smind/internal/acp"
)

// ListConfigOptions returns the config options the agent advertised for
// runID's ACP session, discovered at session creation (see
// taskrunner.Runner.ConfigOptions). A run whose provider doesn't speak
// ACP reports an empty list; an unknown runID is an error.
func (reg *Registry) ListConfigOptions(runID string) ([]acp.ConfigOption, error) {
	r, err := reg.get(runID)
	if err != nil {
		return nil, err
	}
	if r.runner == nil {
		return nil, nil
	}
	return r.runner.ConfigOptions(r.taskID, r.provider), nil
}

// SetConfigOption sets one config option on runID's live ACP session via
// taskrunner.Runner.SetSessionConfigOption, returning the session's full
// option list as the agent reports it after the change. Non-ACP providers
// fail with taskrunner.ErrConfigOptionsNotSupported; a run whose turn
// isn't live fails rather than silently no-op'ing.
func (reg *Registry) SetConfigOption(ctx context.Context, runID, configID, value string) ([]acp.ConfigOption, error) {
	r, err := reg.get(runID)
	if err != nil {
		return nil, err
	}
	if r.runner == nil {
		return nil, fmt.Errorf("runs: set config option: run %q has no live runner (daemon restarted since it ran?)", runID)
	}
	opts, err := r.runner.SetSessionConfigOption(ctx, r.taskID, r.provider, configID, value)
	if err != nil {
		return nil, fmt.Errorf("runs: set config option: %w", err)
	}
	return opts, nil
}
