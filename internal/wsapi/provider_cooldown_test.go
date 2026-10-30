package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProviderClearCooldown_RefusesAgentCaller(t *testing.T) {
	h := newSecretsHarness(t)
	if err := h.db.SetProviderCooldown("glm", time.Now().Add(time.Hour), "earlier", time.Now()); err != nil {
		t.Fatalf("SetProviderCooldown() error = %v", err)
	}
	_, err := h.call(t, "provider.clearCooldown", &requestContext{runScoped: true}, map[string]string{"provider": "glm"})
	if err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("agent caller: err = %v, want forbidden", err)
	}
	if _, ok, err := h.db.GetProviderCooldown("glm", time.Now()); err != nil || !ok {
		t.Fatalf("cooldown after the refusal = %v, %v, want intact", ok, err)
	}
	for _, m := range RunScopedMethods {
		if m == "provider.clearCooldown" {
			t.Fatal("provider.clearCooldown is on the run-scoped allowlist")
		}
	}
}

func TestProviderClearCooldown_ClearsBlock(t *testing.T) {
	h := newSecretsHarness(t)
	if err := h.db.SetProviderCooldown("glm", time.Now().Add(time.Hour), "earlier", time.Now()); err != nil {
		t.Fatalf("SetProviderCooldown() error = %v", err)
	}
	res, err := h.call(t, "provider.clearCooldown", &requestContext{}, map[string]string{"provider": "glm"})
	if err != nil {
		t.Fatalf("clearCooldown() error = %v", err)
	}
	if got := res.(providerClearCooldownResult); got.Provider != "glm" || !got.Cleared || got.Woken != 0 {
		t.Fatalf("result = %+v, want glm cleared with nothing held", got)
	}
	list, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatalf("provider.list() error = %v", err)
	}
	raw, _ := json.Marshal(list.(providerListResult))
	if strings.Contains(string(raw), "cooldownUntil") {
		t.Fatalf("provider.list still shows a cooldown: %s", raw)
	}
	res, err = h.call(t, "provider.clearCooldown", &requestContext{}, map[string]string{"provider": "glm"})
	if err != nil {
		t.Fatalf("second clearCooldown() error = %v", err)
	}
	if got := res.(providerClearCooldownResult); got.Cleared {
		t.Fatalf("second clear = %+v, want cleared=false", got)
	}
}

func TestProviderClearCooldown_UnknownProvider(t *testing.T) {
	h := newSecretsHarness(t)
	_, err := h.call(t, "provider.clearCooldown", &requestContext{}, map[string]string{"provider": "nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("unknown provider: err = %v", err)
	}
}
