package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// maxProviderSecretLen bounds one secret value (ADR-0046 section 4).
const maxProviderSecretLen = 4096

// providerSecretResult is the result of provider.setSecret and
// provider.clearSecret: names and a fingerprint only -- a secret value is
// never in a response, event or log line.
type providerSecretResult struct {
	InstanceID  string `json:"instanceId"`
	Name        string `json:"name"`
	Set         bool   `json:"set"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// validProviderSecretValue is the write-side value rule: non-empty after
// trimming, at most maxProviderSecretLen bytes, no control characters. The
// returned reason never echoes the value.
func validProviderSecretValue(value string) error {
	switch {
	case value == "":
		return fmt.Errorf("the value is empty")
	case len(value) > maxProviderSecretLen:
		return fmt.Errorf("the value is too long (at most %d bytes)", maxProviderSecretLen)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return fmt.Errorf("the value has control characters")
		}
	}
	return nil
}

// handleProviderSetSecret is provider.setSecret {instanceId, name, value}:
// store one named secret of a custom provider instance (a providers.custom
// env `{secret: name}` reference) in smind.db, then publish
// provider.updated. Human-only and write-only: a run-scoped (agent) caller
// is refused, and no response, event or error ever carries the value --
// only the store's last-4 fingerprint (ADR-0046 section 4).
func handleProviderSetSecret(runner *taskrunner.Runner, db *store.Store, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.setSecret is not available to agents"}
		}
		var p struct {
			InstanceID string `json:"instanceId"`
			Name       string `json:"name"`
			Value      string `json:"value"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.setSecret: invalid params")
		}
		spec, ok := runner.Providers().Get(p.InstanceID)
		if !ok || !spec.Custom {
			return nil, fmt.Errorf("provider.setSecret: %q is not a custom provider", p.InstanceID)
		}
		referenced := false
		for _, secretName := range spec.SecretEnv {
			if secretName == p.Name {
				referenced = true
				break
			}
		}
		if !referenced {
			return nil, fmt.Errorf("provider.setSecret: instance %q has no env entry referencing secret %q", p.InstanceID, p.Name)
		}
		value := strings.TrimSpace(p.Value)
		if err := validProviderSecretValue(value); err != nil {
			return nil, fmt.Errorf("provider.setSecret: %w", err)
		}
		if err := db.SetProviderSecret(p.InstanceID, p.Name, value); err != nil {
			return nil, fmt.Errorf("provider.setSecret: store failed")
		}
		res := newProviderUpdatedPayload(runner, p.InstanceID)
		if bus != nil {
			bus.Publish(Event{Topic: TopicProviderUpdated, Payload: res})
		}
		return providerSecretResult{
			InstanceID:  p.InstanceID,
			Name:        p.Name,
			Set:         true,
			Fingerprint: store.SecretFingerprint(value),
		}, nil
	}
}

// handleProviderClearSecret is provider.clearSecret {instanceId, name}:
// remove one stored secret (clearing a secret that is not stored is not an
// error), then publish provider.updated. Human-only, same guard as
// provider.setSecret.
func handleProviderClearSecret(runner *taskrunner.Runner, db *store.Store, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.clearSecret is not available to agents"}
		}
		var p struct {
			InstanceID string `json:"instanceId"`
			Name       string `json:"name"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.clearSecret: invalid params")
		}
		spec, ok := runner.Providers().Get(p.InstanceID)
		if !ok || !spec.Custom {
			return nil, fmt.Errorf("provider.clearSecret: %q is not a custom provider", p.InstanceID)
		}
		if err := db.ClearProviderSecret(p.InstanceID, p.Name); err != nil {
			return nil, fmt.Errorf("provider.clearSecret: store failed")
		}
		res := newProviderUpdatedPayload(runner, p.InstanceID)
		if bus != nil {
			bus.Publish(Event{Topic: TopicProviderUpdated, Payload: res})
		}
		return providerSecretResult{InstanceID: p.InstanceID, Name: p.Name, Set: false}, nil
	}
}
