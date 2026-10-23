package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// providerSetDefaultModelMu serialises config.yaml writes + live updates so
// two concurrent calls leave the file and the Runner agreeing.
var providerSetDefaultModelMu sync.Mutex

// providerSetDefaultModelResult is the result of provider.setDefaultModel.
type providerSetDefaultModelResult = providerUpdatedPayload

// handleProviderSetDefaultModel sets providers.<id>.defaultModel live: it
// writes config.yaml through config.SetProviderDefaultModel (the same path as
// `smind config set`), and only after that succeeds updates the Runner and
// publishes provider.updated. Model "" clears it. Humans only: a run-scoped
// (agent) caller is refused, and no MCP tool exposes it.
func handleProviderSetDefaultModel(runner *taskrunner.Runner, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.setDefaultModel is not available to agents"}
		}
		var p struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.setDefaultModel: invalid params: %w", err)
		}
		model := strings.TrimSpace(p.Model)
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		if err := config.SetProviderDefaultModel(p.Provider, model); err != nil {
			return nil, fmt.Errorf("provider.setDefaultModel: %w", err)
		}
		runner.SetDefaultModel(taskrunner.Provider(p.Provider), model)
		res := providerSetDefaultModelResult{ID: p.Provider, DefaultModel: model}
		if bus != nil {
			bus.Publish(Event{Topic: TopicProviderUpdated, Payload: providerUpdatedPayload(res)})
		}
		return res, nil
	}
}
