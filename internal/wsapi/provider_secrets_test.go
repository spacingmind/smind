package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// sentinel is a fake key, never a real one (the same rule as the
// taskrunner and store secret tests).
const keySentinel = "sentinel-NOT-A-REAL-KEY-12345"

// secretsHarness is the pdmHarness shape with the one thing it cannot
// build: a Runner whose secret source is the same real temp store the
// handlers write through.
type secretsHarness struct {
	api    *API
	runner *taskrunner.Runner
	db     *store.Store
	path   string
}

func newSecretsHarness(t *testing.T) *secretsHarness {
	t.Helper()
	resetProviders(t)
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	wm, db := newTestWorkspaceManager(t)
	runner := taskrunner.New(wm,
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithProviderSecrets(db))
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return &secretsHarness{api: api, runner: runner, db: db, path: filepath.Join(home, "config.yaml")}
}

func (h *secretsHarness) call(t *testing.T, method string, rc *requestContext, params any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return h.api.hs[method](context.Background(), rc, raw)
}

// writeCustom replaces providers.custom in config.yaml with body's entries.
func (h *secretsHarness) writeCustom(t *testing.T, entries ...string) {
	t.Helper()
	body := "providers:\n  custom:\n"
	if len(entries) == 0 {
		body += "    []\n"
	}
	for _, e := range entries {
		body += e
	}
	if err := os.WriteFile(h.path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, "provider.reload", &requestContext{}, nil); err != nil {
		t.Fatal(err)
	}
}

// zaiInstance is one claude-native instance whose ANTHROPIC_AUTH_TOKEN comes
// from the stored secret "zai-key".
const zaiInstance = "    - id: claude-zai\n      label: Claude Zai\n      extends: claude-native\n      env:\n          ANTHROPIC_AUTH_TOKEN: {secret: zai-key}\n"

// otherInstance is a second instance with its own secret, "other-key".
const otherInstance = "    - id: claude-other\n      label: Claude Other\n      extends: claude-native\n      env:\n          ANTHROPIC_API_KEY: {secret: other-key}\n"

func (h *secretsHarness) setSecret(t *testing.T, rc *requestContext, instanceID, name, value string) (any, error) {
	t.Helper()
	return h.call(t, "provider.setSecret", rc, map[string]string{"instanceId": instanceID, "name": name, "value": value})
}

func (h *secretsHarness) stored(t *testing.T, instanceID, name string) bool {
	t.Helper()
	v, ok, err := h.db.GetProviderSecret(instanceID, name)
	if err != nil {
		t.Fatal(err)
	}
	return ok && v != ""
}

// listedSecretEntry is the instance's single listed secret view.
func (h *secretsHarness) listedSecretEntry(t *testing.T, instanceID string) providerSecretView {
	t.Helper()
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.(providerListResult).Providers {
		if string(p.ID) == instanceID {
			if len(p.Secrets) != 1 {
				t.Fatalf("%s secrets = %+v", instanceID, p.Secrets)
			}
			return p.Secrets[0]
		}
	}
	t.Fatalf("provider %q not listed", instanceID)
	return providerSecretView{}
}

func TestProviderSecret_RefusesAgentCaller(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	agent := &requestContext{runScoped: true}
	for method, params := range map[string]any{
		"provider.setSecret":   map[string]string{"instanceId": "claude-zai", "name": "zai-key", "value": keySentinel},
		"provider.clearSecret": map[string]string{"instanceId": "claude-zai", "name": "zai-key"},
	} {
		_, err := h.call(t, method, agent, params)
		if err == nil || codedError(err).Code != codeForbidden {
			t.Errorf("%s: err = %v, want forbidden", method, err)
		}
	}
	if h.stored(t, "claude-zai", "zai-key") {
		t.Fatal("an agent caller stored a secret")
	}
	for _, m := range RunScopedMethods {
		if m == "provider.setSecret" || m == "provider.clearSecret" {
			t.Fatalf("%s is on the run-scoped allowlist", m)
		}
	}
}

func TestProviderSecret_ListShowsFingerprintOnly(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	entry := func() providerSecretView {
		t.Helper()
		e := h.listedSecretEntry(t, "claude-zai")
		return e
	}
	// Before any value: the secret is listed (name and env only), not set.
	e := entry()
	if e.Name != "zai-key" || e.Env != "ANTHROPIC_AUTH_TOKEN" || e.Set || e.Fingerprint != "" {
		t.Fatalf("before set: %+v", e)
	}
	if _, err := h.setSecret(t, &requestContext{}, "claude-zai", "zai-key", keySentinel); err != nil {
		t.Fatal(err)
	}
	e = entry()
	if !e.Set || e.Fingerprint == "" || len(e.Fingerprint) != 4 {
		t.Fatalf("after set: %+v", e)
	}
	// The whole provider.list answer carries the fingerprint and never the value.
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"fingerprint"`) || strings.Contains(string(raw), keySentinel) {
		t.Fatalf("provider.list = %s", raw)
	}
}

func TestProviderSecret_RelayResponseCarriesNoValue(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	res, err := h.setSecret(t, &requestContext{}, "claude-zai", "zai-key", keySentinel)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(providerSecretResult); got.InstanceID != "claude-zai" || got.Name != "zai-key" || !got.Set || got.Fingerprint == "" {
		t.Fatalf("result = %+v", got)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("setSecret result carries the value: %s", raw)
	}
	ev := queued(sub)
	if len(ev) != 1 || ev[0].Topic != TopicProviderUpdated {
		t.Fatalf("events = %+v", ev)
	}
	payload, _ := json.Marshal(ev[0].Payload)
	if strings.Contains(string(payload), keySentinel) {
		t.Fatalf("provider.updated payload carries the value: %s", payload)
	}
}

func TestProviderSecret_UnknownReferenceRejected(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	// A name no env entry references.
	_, err := h.setSecret(t, &requestContext{}, "claude-zai", "other", keySentinel)
	if err == nil || !strings.Contains(err.Error(), `"other"`) || strings.Contains(err.Error(), keySentinel) {
		t.Fatalf("unknown name: err = %v", err)
	}
	// A built-in provider is not an instance.
	if _, err := h.setSecret(t, &requestContext{}, "claude-native", "zai-key", keySentinel); err == nil || !strings.Contains(err.Error(), "not a custom provider") {
		t.Fatalf("built-in: err = %v", err)
	}
	// Bad values: rejected without echoing the value.
	for name, value := range map[string]string{
		"empty":   "",
		"spaces":  "   ",
		"control": "a\x00b\x1fc",
		"long":    strings.Repeat("k", 5000),
	} {
		_, err := h.setSecret(t, &requestContext{}, "claude-zai", "zai-key", value)
		if err == nil || (value != "" && strings.Contains(err.Error(), value)) {
			t.Errorf("%s value: err = %v", name, err)
		}
	}
	if h.stored(t, "claude-zai", "zai-key") || h.stored(t, "claude-zai", "other") {
		t.Fatal("a rejected call stored a secret")
	}
}

func TestProviderSecret_DeletedWithInstance(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance, otherInstance)
	for instance, name := range map[string]string{"claude-zai": "zai-key", "claude-other": "other-key"} {
		if _, err := h.setSecret(t, &requestContext{}, instance, name, keySentinel); err != nil {
			t.Fatal(err)
		}
	}
	// Removing the claude-zai entry prunes its secret; the other instance's stays.
	h.writeCustom(t, otherInstance)
	if h.stored(t, "claude-zai", "zai-key") {
		t.Fatal("the removed instance's secret survived the reload")
	}
	if !h.stored(t, "claude-other", "other-key") {
		t.Fatal("the surviving instance's secret was pruned")
	}
}

func TestProviderSecret_ClearRemovesRow(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, zaiInstance)
	if _, err := h.setSecret(t, &requestContext{}, "claude-zai", "zai-key", keySentinel); err != nil {
		t.Fatal(err)
	}
	res, err := h.call(t, "provider.clearSecret", &requestContext{}, map[string]string{"instanceId": "claude-zai", "name": "zai-key"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(providerSecretResult); got.Set || got.Fingerprint != "" || got.Name != "zai-key" {
		t.Fatalf("clear result = %+v", got)
	}
	if h.stored(t, "claude-zai", "zai-key") {
		t.Fatal("clear left the row behind")
	}
	if e := h.listedSecretEntry(t, "claude-zai"); e.Set || e.Fingerprint != "" {
		t.Fatalf("after clear: %+v", e)
	}
}
