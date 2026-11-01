package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const providerSecretSentinel = "SENTINEL-provider-secret-5521"

// providerPluginEnv is a real API server with plugin "acme" installed and
// enabled; it contributes the ACP provider acme/agent (a secret slot, a
// model, a mode). These tests share the process-wide provider registry, so
// they do not run in parallel.
func newProviderPluginEnv(t *testing.T) *contribEnv {
	t.Helper()
	return newProviderPluginEnvWith(t, "#!/bin/sh\nexit 0\n", "")
}

// newProviderPluginEnvWith is the same with the agent file's body and an
// extra provider-entry fragment (for example a health block).
func newProviderPluginEnvWith(t *testing.T, agentBody, extra string) *contribEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("plugins:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithProviderSecrets(db))
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	t.Cleanup(func() { providers.Default().SetPlugins(nil) })

	src := t.TempDir()
	manifest := `{"id":"acme","name":"Acme","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},
		"contributes":{"providers":[{"id":"agent","label":"Acme agent","icon":"Bot",
		"acp":{"command":["bin/agent","--acp"],"secretEnv":[{"name":"ACME_API_KEY","label":"Acme key","required":true}]},
		"models":[{"id":"big","default":true}],"modes":[{"id":"ask"}]`+extra+`}]}}`
	for name, body := range map[string]string{"smind-plugin.json": manifest, "bin/agent": agentBody} {
		full := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := &contribEnv{api: api, db: db, task: task, home: home}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": src})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	in := res.(plugins.Info)
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": "acme", "confirm": in.Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	return e
}

func (e *contribEnv) providerRows(t *testing.T, params any) map[string]map[string]any {
	t.Helper()
	var out struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal([]byte(e.json(t, "provider.list", localRC(), params)), &out); err != nil {
		t.Fatal(err)
	}
	rows := map[string]map[string]any{}
	for _, p := range out.Providers {
		rows[p["id"].(string)] = p
	}
	return rows
}

func TestPluginProvider_ListCarriesPluginBadgeAndSecretSlots(t *testing.T) {
	e := newProviderPluginEnv(t)
	rows := e.providerRows(t, nil)
	row, ok := rows["acme/agent"]
	if !ok {
		t.Fatalf("acme/agent is not in provider.list: %v", rows)
	}
	if row["plugin"] != "acme" || row["icon"] != "Bot" || row["custom"] != true || row["runner"] != "acp" || row["harness"] != "acp" || row["label"] != "Acme agent" {
		t.Fatalf("row = %v", row)
	}
	if _, has := row["inactive"]; has {
		t.Fatal("an active provider is marked inactive")
	}
	slots, _ := row["secretSlots"].([]any)
	if len(slots) != 1 || slots[0].(map[string]any)["name"] != "ACME_API_KEY" || slots[0].(map[string]any)["required"] != true {
		t.Fatalf("secretSlots = %v", row["secretSlots"])
	}
	secrets, _ := row["secrets"].([]any)
	if len(secrets) != 1 || secrets[0].(map[string]any)["set"] == true {
		t.Fatalf("secrets = %v", row["secrets"])
	}
	if _, has := rows["claude-native"]["plugin"]; has {
		t.Fatal("a core provider carries a plugin field")
	}
}

func TestProviderList_IncludeInactive_OnlyInstalledInactive(t *testing.T) {
	e := newProviderPluginEnv(t)
	if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.providerRows(t, nil)["acme/agent"]; ok {
		t.Fatal("DefaultOmitsInactive: a disabled plugin's provider is listed by default")
	}
	row, ok := e.providerRows(t, map[string]any{"includeInactive": true})["acme/agent"]
	if !ok || row["inactive"] != true || row["pluginState"] != "disabled" || row["plugin"] != "acme" || row["label"] != "Acme agent" {
		t.Fatalf("includeInactive row = %v", row)
	}
	if err := e.api.plugins.Remove("acme", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.providerRows(t, map[string]any{"includeInactive": true})["acme/agent"]; ok {
		t.Fatal("a removed plugin's provider is listed")
	}
}

func TestPluginProvider_UpdateInstanceRefused_ReadOnlyOwned(t *testing.T) {
	e := newProviderPluginEnv(t)
	calls := []struct {
		method string
		params map[string]any
	}{
		{"provider.updateInstance", map[string]any{"id": "acme/agent", "additionalModels": []map[string]any{{"id": "x"}}}},
		{"provider.setEnv", map[string]any{"provider": "acme/agent", "key": "K", "value": "v"}},
		{"provider.setDefaultModel", map[string]any{"provider": "acme/agent", "model": "big"}},
	}
	check := func(when string) {
		for _, c := range calls {
			_, err := e.call(c.method, localRC(), c.params)
			ce, ok := err.(*rpcCodeError)
			if !ok || ce.RPCCode() != codeReadOnlyOwned || !strings.Contains(err.Error(), "acme") {
				t.Errorf("%s %s: want read_only_owned naming acme, got %v", when, c.method, err)
			} else if d, _ := ce.RPCData().(map[string]string); d["plugin"] != "acme" {
				t.Errorf("%s %s: data = %v", when, c.method, ce.RPCData())
			}
		}
	}
	check("active")
	e.call("plugin.disable", localRC(), map[string]any{"id": "acme"})
	check("inactive")
}

func TestPluginProvider_Secret_WriteOnly_ListShowsFingerprintOnly(t *testing.T) {
	e := newProviderPluginEnv(t)
	if _, err := e.call("provider.setSecret", agentRC(), map[string]any{"instanceId": "acme/agent", "name": "ACME_API_KEY", "value": providerSecretSentinel}); err == nil {
		t.Fatal("an agent set a plugin provider's secret")
	}
	raw := e.json(t, "provider.setSecret", localRC(), map[string]any{"instanceId": "acme/agent", "name": "ACME_API_KEY", "value": providerSecretSentinel})
	if strings.Contains(raw, providerSecretSentinel) {
		t.Fatalf("setSecret answered with the value: %s", raw)
	}
	list := e.json(t, "provider.list", localRC(), nil)
	if strings.Contains(list, providerSecretSentinel) || !strings.Contains(list, `"set":true`) {
		t.Fatalf("provider.list: set must be true and the value absent: %s", list)
	}
	if _, err := e.call("provider.setSecret", localRC(), map[string]any{"instanceId": "acme/agent", "name": "NOT_A_SLOT", "value": "x"}); err == nil {
		t.Fatal("a secret for an undeclared slot was accepted")
	}
	// Disabling keeps the key; a provider.reload (which prunes secrets) must not drop it.
	e.call("plugin.disable", localRC(), map[string]any{"id": "acme"})
	if _, err := e.call("provider.reload", localRC(), nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.db.ListProviderSecrets("acme/agent"); len(got) != 1 {
		t.Fatalf("the secret did not survive disable + reload: %v", got)
	}
}

func TestPluginProvider_ReloadKeepsPluginLayer_AndEventsPublished(t *testing.T) {
	e := newProviderPluginEnv(t)
	sub := subscribeTopics(e.api, TopicProviderUpdated)
	if _, err := e.call("provider.reload", localRC(), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.providerRows(t, nil)["acme/agent"]; !ok {
		t.Fatal("provider.reload dropped the plugin provider")
	}
	sub.mu.Lock()
	sub.queue = nil
	sub.mu.Unlock()
	if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "acme"}); err != nil {
		t.Fatal(err)
	}
	sub.mu.Lock()
	n := len(sub.queue)
	sub.mu.Unlock()
	if n != 1 {
		t.Fatalf("disable published %d provider.updated events, want 1", n)
	}
}

// markerPlugin is a plugin whose agent file writes a marker when EXECUTED
// (by anything: a version probe, a spawn). Nothing may ever run it before the
// scrubbed launch exists.
func markerPlugin(t *testing.T, health string) (*contribEnv, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "executed")
	body := "#!/bin/sh\necho ran > '" + marker + "'\necho 'agent 9.9.9'\n"
	return newProviderPluginEnvWith(t, body, health), marker
}

func (e *contribEnv) providerTest(t *testing.T, id string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(e.json(t, "provider.test", localRC(), map[string]any{"provider": id})), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestProviderTest_PluginProvider_NeverExecutesTheCommand: provider.test on a
// plugin provider reports the lookup of argv[0] and the hints, and never runs
// the plugin's binary (no version probe), with a versionArg or without one.
func TestProviderTest_PluginProvider_NeverExecutesTheCommand(t *testing.T) {
	for name, health := range map[string]string{
		"version arg set":   `,"health":{"install":"npm i -g acme","login":"run acme login","versionArg":"--version","minVersion":"1.0.0"}`,
		"version arg unset": `,"health":{"install":"npm i -g acme","login":"run acme login"}`,
		"no health":         "",
	} {
		t.Run(name, func(t *testing.T) {
			e, marker := markerPlugin(t, health)
			res := e.providerTest(t, "acme/agent")
			if res["ok"] != true || !strings.Contains(res["detail"].(string), "agent") {
				t.Fatalf("the lookup must still report the file: %v", res)
			}
			if strings.Contains(res["detail"].(string), "9.9.9") {
				t.Fatalf("a version was read, so the binary ran: %v", res)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("provider.test EXECUTED the plugin's command")
			}
			if strings.Contains(health, "versionArg") && !strings.Contains(res["detail"].(string), "version check") {
				t.Fatalf("the result should say the version check is not available yet: %v", res)
			}
			if strings.Contains(health, "login") && !strings.Contains(res["detail"].(string), "run acme login") {
				t.Fatalf("login hint missing: %v", res)
			}
		})
	}
}

func TestProviderTest_PluginProvider_RelaySessionStillNeverExecutes(t *testing.T) {
	e, marker := markerPlugin(t, `,"health":{"versionArg":"--version"}`)
	if _, err := e.call("provider.test", relayRC(), map[string]any{"provider": "acme/agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a relay provider.test executed the plugin's command")
	}
	// An agent connection cannot reach provider.test at all.
	if !runScopedSet["provider.list"] || runScopedSet["provider.test"] {
		t.Fatal("provider.test must not be callable by run-scoped agents")
	}
}
