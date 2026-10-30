package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// providerClearCooldownResult is the result of provider.clearCooldown.
type providerClearCooldownResult struct {
	Provider string `json:"provider"`
	Cleared  bool   `json:"cleared"`
	Woken    int    `json:"woken"`
}

// handleProviderClearCooldown is provider.clearCooldown {provider}: the
// human's "I reset my quota, retry now" action (ADR-0041 F1). It drops the
// provider's cooldown row and releases (and delivers) its held rate-limit
// retries, then publishes provider.updated. Human-only, like
// provider.setSecret: a run-scoped (agent) caller is refused.
func handleProviderClearCooldown(runner *taskrunner.Runner, reg *runs.Registry, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.clearCooldown is not available to agents"}
		}
		var p struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.clearCooldown: invalid params")
		}
		if p.Provider == "" {
			return nil, fmt.Errorf("provider.clearCooldown: provider is required")
		}
		if _, ok := runner.Providers().Get(p.Provider); !ok {
			return nil, fmt.Errorf("provider.clearCooldown: unknown provider %q", p.Provider)
		}
		cleared, woken, err := reg.ClearProviderCooldown(p.Provider)
		if err != nil {
			return nil, fmt.Errorf("provider.clearCooldown: %w", err)
		}
		if bus != nil {
			bus.Publish(Event{Topic: TopicProviderUpdated, Payload: newProviderUpdatedPayload(runner, p.Provider)})
		}
		return providerClearCooldownResult{Provider: p.Provider, Cleared: cleared, Woken: woken}, nil
	}
}
