package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
	"github.com/spacingmind/smind/internal/providers"
)

const demoProviderEntry = `{"id":"agent","label":"Demo agent","icon":"Bot","acp":{"command":["bin/agent","--acp"],"env":{"DEMO_MODE":"acp"},"secretEnv":[{"name":"DEMO_API_KEY","label":"Key","required":true}],"supportsMcpServers":false},"models":[{"id":"big","default":true}],"modes":[{"id":"ask"},{"id":"auto","autoApproves":true}],"defaultMode":"ask","health":{"install":"npm i -g demo"}}`

func providerPluginDir(t *testing.T, id, version string, entries ...string) string {
	t.Helper()
	src := writePluginDir(t, manifestWith(id, version, `"contributes":{"providers":[`+strings.Join(entries, ",")+`]}`), map[string]string{"bin/agent": "#!/bin/sh\nexit 0\n"})
	if err := os.Chmod(filepath.Join(src, "bin", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	return src
}

func enableProviderPlugin(t *testing.T, e *env, id string, entries ...string) {
	t.Helper()
	in := mustInstall(t, e, providerPluginDir(t, id, "1.0.0", entries...))
	if _, err := e.m.Enable(id, in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
}

func pluginProviderIDs(reg *providers.Registry) string {
	var ids []string
	for _, s := range reg.List() {
		if s.Plugin != "" {
			ids = append(ids, s.ID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func TestPluginProvider_NamespacedId_NoShadowing_VanishesWithPlugin(t *testing.T) {
	e := newEnv(t)
	reg := providers.New()
	reg.SetCustom([]providers.Spec{{ID: "agent", Command: []string{"x"}}}) // a custom id with the same provider part
	in := mustInstall(t, e, providerPluginDir(t, "demo", "1.0.0", demoProviderEntry))

	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "" {
		t.Fatal("a provider registered before the plugin was enabled")
	}
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if !e.m.SyncProviders(reg) || pluginProviderIDs(reg) != "demo/agent" {
		t.Fatalf("ids = %q", pluginProviderIDs(reg))
	}
	if _, ok := reg.Get("agent"); !ok {
		t.Fatal("the custom provider 'agent' was shadowed or dropped")
	}
	if e.m.SyncProviders(reg) {
		t.Fatal("a second sync with nothing changed reported a change")
	}
	e.m.Disable("demo")
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "" {
		t.Fatalf("a disabled plugin's provider is still registered: %q", pluginProviderIDs(reg))
	}
	e.m.Enable("demo", in.Consent.Fingerprint)
	e.m.SyncProviders(reg)
	*e.on = false
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "" {
		t.Fatal("providers registered with the global switch off")
	}
	*e.on = true
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "demo/agent" {
		t.Fatal("providers did not return with the switch")
	}
	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "" {
		t.Fatal("a removed plugin's provider is still registered")
	}
	if _, ok := reg.Get("agent"); !ok {
		t.Fatal("the custom provider was dropped by plugin changes")
	}
}

func TestPluginRemove_DeletesProviderSecrets_DisableKeeps(t *testing.T) {
	e := newEnv(t)
	enableProviderPlugin(t, e, "demo", demoProviderEntry)
	if err := e.st.SetProviderSecret("demo/agent", "DEMO_API_KEY", "v-1234"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetProviderSecret("demo-x/agent", "DEMO_API_KEY", "other"); err != nil {
		t.Fatal(err)
	}

	got, err := e.m.Disable("demo")
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := e.st.ListProviderSecrets("demo/agent"); len(l) != 1 {
		t.Fatalf("a disabled plugin's secrets were deleted: %v", l)
	}
	if _, err := e.m.Enable("demo", got.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}

	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.ListProviderSecrets("demo/agent"); len(got) != 0 {
		t.Fatalf("a removed plugin's secrets survived: %v", got)
	}
	if got, _ := e.st.ListProviderSecrets("demo-x/agent"); len(got) != 1 {
		t.Fatalf("an unrelated plugin's secrets were deleted: %v", got)
	}

	// A linked plugin's secrets go with it too.
	src := providerPluginDir(t, "linked", "1.0.0", demoProviderEntry)
	lin, err := e.m.Link(bg, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Enable("linked", lin.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetProviderSecret("linked/agent", "DEMO_API_KEY", "v-5678"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Remove("linked", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.ListProviderSecrets("linked/agent"); len(got) != 0 {
		t.Fatalf("a removed linked plugin's secrets survived: %v", got)
	}
	if _, err := os.Stat(filepath.Join(src, ManifestFile)); err != nil {
		t.Fatal("remove deleted the linked source")
	}
}

func TestPluginProviders_RegistryMatchesActiveSet(t *testing.T) {
	e := newEnv(t)
	reg := providers.New()
	enableProviderPlugin(t, e, "alpha", demoProviderEntry)
	enableProviderPlugin(t, e, "beta", demoProviderEntry, strings.Replace(demoProviderEntry, `"id":"agent"`, `"id":"two"`, 1))
	check := func(step string) {
		t.Helper()
		e.m.SyncProviders(reg)
		var want []string
		for id, pp := range e.m.ProviderSpecs() {
			for _, s := range pp.Specs {
				want = append(want, s.ID)
			}
			_ = id
		}
		sort.Strings(want)
		if got := pluginProviderIDs(reg); got != strings.Join(want, ",") {
			t.Fatalf("%s: registry %q != derived %q", step, got, strings.Join(want, ","))
		}
	}
	check("start")
	e.m.Disable("alpha")
	check("disable alpha")
	*e.on = false
	check("switch off")
	*e.on = true
	e.m.Remove("beta", false)
	check("remove beta")
}

func TestPluginProviders_FailedReload_KeepsLastGood(t *testing.T) {
	e := newEnv(t)
	reg := providers.New()
	src := providerPluginDir(t, "demo", "1.0.0", demoProviderEntry)
	in := mustInstall(t, e, src)
	e.m.Enable("demo", in.Consent.Fingerprint)
	e.m.SyncProviders(reg)
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "2.0.0", `"contributes":{"providers":[{"id":"agent"}]}`)))
	if _, err := e.m.Update(bg, "demo", "", false); err == nil {
		t.Fatal("an invalid manifest updated")
	}
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "demo/agent" {
		t.Fatalf("a failed update dropped the last good provider: %q", pluginProviderIDs(reg))
	}
}

func TestPluginProviders_UnsafeCommand_LeftOutAndShownOnPlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the executable bit is not checked on Windows")
	}
	e := newEnv(t)
	reg := providers.New()
	enableProviderPlugin(t, e, "demo", demoProviderEntry)
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "demo/agent" {
		t.Fatal("setup")
	}
	if err := os.Chmod(filepath.Join(e.home, "plugins", "demo", "bin", "agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The installed copy's hash changed too, so the plugin may fail to load at
	// all; either way the provider must not stay registered.
	e.m.SyncProviders(reg)
	if pluginProviderIDs(reg) != "" {
		t.Fatalf("a non-executable command stayed registered: %q", pluginProviderIDs(reg))
	}
}

func TestConfinedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and the executable bit differ on Windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bin", "agent"), []byte("x"))
	os.Chmod(filepath.Join(root, "bin", "agent"), 0o755)
	// The wrapper delegates to pluginagent.ConfinedExecutable, whose own
	// table (TestPluginAgent_ConfinedExecutable) covers the refusal cases.
	abs, err := confinedExecutable(root, "bin/agent")
	if err != nil {
		t.Fatalf("confinedExecutable: %v", err)
	}
	if !filepath.IsAbs(abs) || !strings.HasSuffix(filepath.ToSlash(abs), "/bin/agent") {
		t.Fatalf("abs = %q", abs)
	}
}

func TestProviderSpecs_ShapeFromManifest(t *testing.T) {
	e := newEnv(t)
	enableProviderPlugin(t, e, "demo", demoProviderEntry)
	pp := e.m.ProviderSpecs()["demo"]
	if pp.Err != nil || len(pp.Specs) != 1 {
		t.Fatalf("%+v", pp)
	}
	s := pp.Specs[0]
	if s.ID != "demo/agent" || s.Plugin != "demo" || s.Runner != providers.RunnerACP || s.Icon != "Bot" || s.DefaultMode != "ask" ||
		!s.NoMCPServers || s.Env["DEMO_MODE"] != "acp" || s.SecretEnv["DEMO_API_KEY"] != "DEMO_API_KEY" ||
		len(s.SecretSlots) != 1 || !s.SecretSlots[0].Required || len(s.Models) != 1 || len(s.Modes) != 2 || !s.Modes[1].AutoApproves {
		t.Fatalf("spec = %+v", s)
	}
	if s.Command[0] != "bin/agent" || s.Command[1] != "--acp" {
		t.Fatalf("command = %v", s.Command)
	}
	if !strings.HasSuffix(filepath.ToSlash(s.PluginRoot), "/plugins/demo") ||
		!strings.HasSuffix(filepath.ToSlash(s.PluginData), "/plugins/demo/data") ||
		s.CommandForm != "path" || !filepath.IsAbs(s.HealthCommand[0]) {
		t.Fatalf("plugin fields = %q %q %q %v", s.PluginRoot, s.PluginData, s.CommandForm, s.HealthCommand)
	}
	if s.Health.Install != "npm i -g demo" {
		t.Fatalf("health = %+v", s.Health)
	}
}

func TestInactiveProviders_ListsDisabledAndSwitchOff(t *testing.T) {
	e := newEnv(t)
	in := mustInstall(t, e, providerPluginDir(t, "demo", "1.0.0", demoProviderEntry))
	got := e.m.InactiveProviders()
	if len(got) != 1 || got[0].ID != "demo/agent" || got[0].State != "disabled" || got[0].Label != "Demo agent" {
		t.Fatalf("disabled: %+v", got)
	}
	e.m.Enable("demo", in.Consent.Fingerprint)
	if got := e.m.InactiveProviders(); len(got) != 0 {
		t.Fatalf("an active plugin's provider listed as inactive: %+v", got)
	}
	*e.on = false
	if got := e.m.InactiveProviders(); len(got) != 1 || got[0].State != "global_off" {
		t.Fatalf("switch off: %+v", got)
	}
}

func TestConsent_Provider_ListsCommandEnvKeys_NoValues(t *testing.T) {
	e := newEnv(t)
	in := mustInstall(t, e, providerPluginDir(t, "demo", "1.0.0", strings.Replace(demoProviderEntry, `"DEMO_MODE":"acp"`, `"DEMO_MODE":"sentinel-env-value-77"`, 1)))
	c := in.Consent
	if len(c.AcpProviders) != 1 {
		t.Fatalf("%+v", c.AcpProviders)
	}
	a := c.AcpProviders[0]
	if a.ID != "demo/agent" || a.Resolves != "plugin file" || len(a.EnvKeys) != 1 || a.EnvKeys[0] != "DEMO_MODE" || len(a.SecretEnv) != 1 || a.EnvDigest == "" || c.DataOnly {
		t.Fatalf("%+v", a)
	}
	if strings.Contains(strings.Join(append([]string{a.EnvDigest}, a.EnvKeys...), " "), "sentinel-env-value-77") {
		t.Fatal("an env value is in the consent summary")
	}
	// A changed literal value changes the fingerprint; the summary still has no value.
	in2 := mustInstall(t, newEnv(t), providerPluginDir(t, "demo", "1.0.0", strings.Replace(demoProviderEntry, `"DEMO_MODE":"acp"`, `"DEMO_MODE":"other"`, 1)))
	in3 := mustInstall(t, newEnv(t), providerPluginDir(t, "demo", "1.0.0", demoProviderEntry))
	if in2.Consent.Fingerprint == in3.Consent.Fingerprint {
		t.Fatal("a changed env value did not change the consent fingerprint")
	}
	in4 := mustInstall(t, newEnv(t), providerPluginDir(t, "demo", "1.0.0", strings.Replace(demoProviderEntry, `"supportsMcpServers":false`, `"inheritHome":true`, 1)))
	if in4.Consent.Fingerprint == in3.Consent.Fingerprint {
		t.Fatal("inheritHome is not in the fingerprint")
	}
}

func TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome(t *testing.T) {
	// Every case installs the SAME id and version, so only the provider
	// entry (or the plugin description) differs from the baseline.
	fpOf := func(t *testing.T, entry, manifestExtra string, extraFiles map[string]string) string {
		t.Helper()
		files := map[string]string{"bin/agent": "#!/bin/sh\nexit 0\n"}
		for n, b := range extraFiles {
			files[n] = b
		}
		src := writePluginDir(t, manifestWith("demo", "1.0.0", `"contributes":{"providers":[`+entry+`]}`+manifestExtra), files)
		for n := range files {
			if err := os.Chmod(filepath.Join(src, filepath.FromSlash(n)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return mustInstall(t, newEnv(t), src).Consent.Fingerprint
	}
	base := fpOf(t, demoProviderEntry, "", nil)
	cases := []struct {
		name          string
		entry         string
		manifestExtra string
		extraFiles    map[string]string
		expectSame    bool
	}{
		{"extra argv entry", strings.Replace(demoProviderEntry, `"--acp"]`, `"--acp","-x"]`, 1), "", nil, false},
		{"env value", strings.Replace(demoProviderEntry, `"DEMO_MODE":"acp"`, `"DEMO_MODE":"other"`, 1), "", nil, false},
		{"env key renamed", strings.Replace(demoProviderEntry, `"DEMO_MODE":"acp"`, `"DEMO_KIND":"acp"`, 1), "", nil, false},
		{"secret slot name", strings.Replace(demoProviderEntry, `"name":"DEMO_API_KEY"`, `"name":"DEMO_TOKEN"`, 1), "", nil, false},
		{"inheritHome", strings.Replace(demoProviderEntry, `"acp":{`, `"acp":{"inheritHome":true,`, 1), "", nil, false},
		{"vendor", strings.Replace(demoProviderEntry, `{"id":"agent"`, `{"id":"agent","vendor":"zai"`, 1), "", nil, false},
		{"label", strings.Replace(demoProviderEntry, `"label":"Demo agent"`, `"label":"Demo agent two"`, 1), "", nil, false},
		{"command file name", strings.Replace(demoProviderEntry, `"command":["bin/agent"`, `"command":["bin/agent2"`, 1), "", map[string]string{"bin/agent2": "#!/bin/sh\nexit 0\n"}, false},
		// Descriptions are shown to the Human, never executed, so they are
		// not part of the fingerprint (AcpEntry carries none).
		{"provider description", strings.Replace(demoProviderEntry, `{"id":"agent"`, `{"id":"agent","description":"runs things"`, 1), "", nil, true},
		{"plugin description", demoProviderEntry, `,"description":"a demo plugin"`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp := fpOf(t, tc.entry, tc.manifestExtra, tc.extraFiles)
			if tc.expectSame && fp != base {
				t.Fatal("a change that runs nothing moved the fingerprint")
			}
			if !tc.expectSame && fp == base {
				t.Fatal("a consent-relevant change kept the fingerprint")
			}
		})
	}
}

func TestConsent_Provider_ValueNeverInSummaryOrLogs(t *testing.T) {
	const sentinel = "SENTINEL-env-value-9931"
	e := newHostEnv(t, host.DenyAll)
	entry := strings.Replace(demoProviderEntry, `"DEMO_MODE":"acp"`, `"DEMO_MODE":"`+sentinel+`"`, 1)
	// A process besides the provider, so the plugin log has real lines.
	cmd, _ := json.Marshal(hosttest.Command("report"))
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"process":{"command":`+string(cmd)+`},"contributes":{"providers":[`+entry+`]}`),
		map[string]string{"bin/agent": "#!/bin/sh\nexit 0\n"})
	if err := os.Chmod(filepath.Join(src, "bin", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := mustInstall(t, e, src)

	b, err := json.Marshal(in.Consent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), sentinel) {
		t.Fatalf("the env value is in the consent summary: %s", b)
	}
	if s := fmt.Sprintf("%+v", in.Consent); strings.Contains(s, sentinel) {
		t.Fatalf("the env value is in the formatted consent: %s", s)
	}

	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	waitProc(t, e.m, "demo", host.StateRunning)
	logs, err := e.m.Logs("demo", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Lines) == 0 {
		t.Fatal("setup: the plugin never logged anything")
	}
	for _, l := range logs.Lines {
		if strings.Contains(l.Text, sentinel) {
			t.Fatalf("the env value is in the plugin log: %q", l.Text)
		}
	}
}

func TestPluginProviders_GlobalSwitchOff_NoProviders(t *testing.T) {
	e := newEnv(t)
	reg := providers.New()
	enableProviderPlugin(t, e, "demo", demoProviderEntry)
	*e.on = false
	e.m.SyncProviders(reg)
	if got := pluginProviderIDs(reg); got != "" {
		t.Fatalf("registered with the switch off: %q", got)
	}
}

func TestPluginProviders_DuplicateAcrossPlugins_Impossible(t *testing.T) {
	e := newEnv(t)
	reg := providers.New()
	enableProviderPlugin(t, e, "alpha", demoProviderEntry)
	enableProviderPlugin(t, e, "beta", demoProviderEntry)
	e.m.SyncProviders(reg)
	if got := pluginProviderIDs(reg); got != "alpha/agent,beta/agent" {
		t.Fatalf("ids = %q: two plugins with the same provider id must stay distinct", got)
	}
}
