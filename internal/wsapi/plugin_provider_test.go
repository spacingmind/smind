package wsapi

import (
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	runner := taskrunner.New(wm, taskrunner.WithProviderSecrets(db), taskrunner.WithACPModeProbe())
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
		"models":[{"id":"big","default":true}],"modes":[{"id":"ask"}]` + extra + `}]}}`
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

// markerPlugin is a plugin whose agent file records how it was executed: its
// whole environment, its cwd and its argv go to a marker file, and it prints a
// version. Nothing may run it except the scrubbed launch.
func markerPlugin(t *testing.T, health string) (*contribEnv, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "executed")
	body := "#!/bin/sh\n{ env; echo \"PWD_REAL=$(pwd -P)\"; echo \"ARGV=$*\"; } > '" + marker + "'\necho 'agent 9.9.9'\n"
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

// TestProviderTest_PluginProvider_NeverExecutesTheCommand: without a declared
// versionArg provider.test only looks argv[0] up; an inactive or removed
// plugin's provider is not looked at at all; a relay caller gets the same.
func TestProviderTest_PluginProvider_NeverExecutesTheCommand(t *testing.T) {
	for name, health := range map[string]string{
		"version arg unset": `,"health":{"install":"npm i -g acme","login":"run acme login"}`,
		"no health":         "",
	} {
		t.Run(name, func(t *testing.T) {
			e, marker := markerPlugin(t, health)
			res := e.providerTest(t, "acme/agent")
			if res["ok"] != true || !strings.Contains(res["detail"].(string), "agent") {
				t.Fatalf("the lookup must still report the file: %v", res)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("provider.test EXECUTED the plugin's command without a versionArg")
			}
			if strings.Contains(health, "login") && !strings.Contains(res["detail"].(string), "run acme login") {
				t.Fatalf("login hint missing: %v", res)
			}
		})
	}
	t.Run("plugin disabled", func(t *testing.T) {
		e, marker := markerPlugin(t, `,"health":{"versionArg":"--version"}`)
		if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "acme"}); err != nil {
			t.Fatal(err)
		}
		if res := e.providerTest(t, "acme/agent"); res["ok"] == true {
			t.Fatalf("an unregistered plugin provider tested ok: %v", res)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a DISABLED plugin's command was executed by provider.test")
		}
	})
	t.Run("relay", func(t *testing.T) {
		e, marker := markerPlugin(t, "")
		if _, err := e.call("provider.test", relayRC(), map[string]any{"provider": "acme/agent"}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a relay provider.test executed the command without a versionArg")
		}
	})
}

// TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv: with a declared
// versionArg the probe runs, but ONLY through the scrubbed launch: built env
// (no daemon variable, no SMIND_*, no secret), private HOME, cwd the plugin
// directory, argv exactly the declared flag.
func TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv(t *testing.T) {
	t.Setenv("SMIND_PD2_SENTINEL", "daemon-leak-8812")
	t.Setenv("ANTHROPIC_API_KEY", "daemon-leak-8812")
	e, marker := markerPlugin(t, `,"health":{"versionArg":"--version","minVersion":"1.0.0"}`)
	if _, err := e.call("provider.setSecret", localRC(), map[string]any{"instanceId": "acme/agent", "name": "ACME_API_KEY", "value": providerSecretSentinel}); err != nil {
		t.Fatal(err)
	}
	res := e.providerTest(t, "acme/agent")
	if res["ok"] != true || !strings.Contains(res["detail"].(string), "9.9.9") {
		t.Fatalf("the probe should read the version: %v", res)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the probe did not run: %v", err)
	}
	dump := string(raw)
	for _, bad := range []string{"daemon-leak-8812", "SMIND_", "ANTHROPIC", providerSecretSentinel, "ACME_API_KEY"} {
		if strings.Contains(dump, bad) {
			t.Errorf("%q reached the version probe:\n%s", bad, dump)
		}
	}
	for _, want := range []string{"PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin", "/plugins/acme/data/providers/agent/home", "ARGV=--version"} {
		if !strings.Contains(dump, want) {
			t.Errorf("probe env/argv is missing %q:\n%s", want, dump)
		}
	}
	if !strings.Contains(dump, "PWD_REAL=") || !strings.Contains(dump, "/plugins/acme") {
		t.Errorf("the probe's cwd is not the plugin directory:\n%s", dump)
	}
	// Too old: the declared minimum gates it, as for core providers.
	e2, _ := markerPlugin(t, `,"health":{"versionArg":"--version","minVersion":"99.0.0","install":"update acme"}`)
	if r := e2.providerTest(t, "acme/agent"); r["ok"] != false || !strings.Contains(r["detail"].(string), "too old") {
		t.Fatalf("minVersion not enforced: %v", r)
	}
}

// TestPluginProvider_ListAndAgentCallers_NeverStartPluginCode: provider.list
// is callable by an agent and runs the lazy mode probe for providers; for a
// plugin provider that probe must never start the plugin's code. (provider.test
// and the write methods are not run-scoped at all: see
// TestRunScopedMethods_NoProviderExecOrWriteMethod.)
func TestPluginProvider_ListAndAgentCallers_NeverStartPluginCode(t *testing.T) {
	e, marker := markerPlugin(t, `,"health":{"versionArg":"--version"}`)
	for _, rc := range []*requestContext{agentRC(), localRC(), relayRC()} {
		if _, err := e.call("provider.list", rc, nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(600 * time.Millisecond) // a lazy probe is asynchronous
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("provider.list started a plugin's code")
	}
	// provider.test is refused to agents by the connection layer (it is not
	// run-scoped); the handler-level guards cover the management methods.
	if runScopedSet["provider.test"] {
		t.Error("provider.test is run-scoped: an agent could trigger the version probe")
	}
	for _, m := range []string{"plugin.enable", "plugin.install", "plugin.reload", "provider.setSecret"} {
		if _, err := e.call(m, agentRC(), map[string]any{"provider": "acme/agent", "instanceId": "acme/agent", "name": "ACME_API_KEY", "value": "x", "id": "acme", "confirm": "x", "source": "x"}); err == nil {
			t.Errorf("an agent connection could call %s", m)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an agent call executed a plugin's code")
	}
}

// TestRunScopedMethods_NoProviderExecOrWriteMethod: a method that can execute
// a provider's command (provider.test's version probe) or change what a
// provider launches with must never become callable by an agent (MCP via
// `smind mcp serve`). Only the read, provider.list, is run-scoped; a later
// widening of RunScopedMethods that adds any of these fails here.
func TestRunScopedMethods_NoProviderExecOrWriteMethod(t *testing.T) {
	never := []string{
		"provider.test", "provider.reload", "provider.addInstance", "provider.addFromRecipe", "provider.updateInstance",
		"provider.setEnv", "provider.setSecret", "provider.clearSecret", "provider.setDefaultModel", "provider.clearCooldown",
		"provider.recipes", "provider.dirSuggestions",
	}
	for _, m := range never {
		if runScopedSet[m] {
			t.Errorf("%s is run-scoped: an agent could trigger it", m)
		}
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "plugin.") {
			t.Errorf("%s is run-scoped: plugin methods are never agent-callable", m)
		}
		if strings.HasPrefix(m, "provider.") && m != "provider.list" {
			t.Errorf("%s is run-scoped: only provider.list may be (a read; its mode probe is guarded for plugin providers)", m)
		}
	}
}

// TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript: after a provider
// secret is set and every surface that could echo it is exercised (set/clear
// answers, provider.list, plugin.get, plugin.list, plugin.logs, the version
// probe with its marker argv, the daemon log, the events on the bus), the
// value is nowhere.
func TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript(t *testing.T) {
	var logBuf lockedBuf // the probe goroutine (taskrunner.maybeProbe) logs while this test reads
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	prevSlog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevSlog) })

	e, marker := markerPlugin(t, `,"health":{"versionArg":"--version"}`)
	sub := subscribeTopics(e.api, TopicProviderUpdated, TopicPluginUpdated)
	var surfaces []string
	rec := func(raw string) { surfaces = append(surfaces, raw) }

	rec(e.json(t, "provider.setSecret", localRC(), map[string]any{"instanceId": "acme/agent", "name": "ACME_API_KEY", "value": providerSecretSentinel}))
	rec(e.json(t, "provider.list", localRC(), nil))
	rec(e.json(t, "provider.list", relayRC(), map[string]any{"includeInactive": true}))
	rec(e.json(t, "plugin.get", localRC(), map[string]any{"id": "acme"}))
	rec(e.json(t, "plugin.list", relayRC(), nil))
	rec(e.json(t, "plugin.logs", localRC(), map[string]any{"id": "acme", "limit": 200}))
	rec(e.json(t, "provider.test", localRC(), map[string]any{"provider": "acme/agent"}))
	rec(e.json(t, "provider.clearSecret", localRC(), map[string]any{"instanceId": "acme/agent", "name": "ACME_API_KEY"}))
	if raw, err := os.ReadFile(marker); err == nil {
		rec(string(raw)) // the probe's whole env and argv
	} else {
		t.Fatalf("the probe did not run: %v", err)
	}
	sub.mu.Lock()
	for _, ev := range sub.queue {
		b, _ := json.Marshal(ev)
		rec(string(b))
	}
	sub.mu.Unlock()
	rec(logBuf.String())
	for i, s := range surfaces {
		if strings.Contains(s, providerSecretSentinel) {
			t.Errorf("surface %d carries the provider secret: %.300s", i, s)
		}
	}
}

// TestPluginProvider_ErrorsAndLogs_LabeledByPlugin (AC34): the problems and
// refusals about a plugin provider name the plugin.
func TestPluginProvider_ErrorsAndLogs_LabeledByPlugin(t *testing.T) {
	e := newProviderPluginEnv(t)
	_, err := e.call("provider.updateInstance", localRC(), map[string]any{"id": "acme/agent", "additionalModels": []map[string]any{{"id": "x"}}})
	if err == nil || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("refusal = %v", err)
	}
	// An unsafe command file after enable is recorded on the plugin, naming it and the provider.
	e.api.plugins.NotePluginProblem("acme", "providers-label-test", "provider acme/agent: command \"bin/agent\": is not executable (chmod +x)")
	raw := e.json(t, "plugin.get", localRC(), map[string]any{"id": "acme"})
	if !strings.Contains(raw, "acme/agent") || !strings.Contains(raw, "lastError") {
		t.Fatalf("plugin.get = %s", raw)
	}
}
