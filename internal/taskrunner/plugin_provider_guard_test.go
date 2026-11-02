package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
)

// The generic launch env builder INHERITS the daemon's environment, so it must
// never build the env of a plugin provider: a caller that did not branch to
// pluginLaunch fails closed (the backstop behind runACP and the probe).
func TestPluginProvider_GenericLaunchEnvRefusesPluginProviders(t *testing.T) {
	reg := providers.New()
	if p := reg.SetPlugins(map[string][]providers.Spec{"acme": {{ID: "acme/agent", Plugin: "acme", Runner: providers.RunnerACP, Command: []string{"bin/agent"}}}}); len(p) != 0 {
		t.Fatal(p)
	}
	r := New(nil, WithProviderRegistry(reg))
	if _, err := r.launchEnvEntries("acme/agent"); err == nil || !strings.Contains(err.Error(), "plugin acme") {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.launchEnvEntries(ProviderGLM); err != nil && strings.Contains(err.Error(), "plugin") {
		t.Fatalf("a core provider was refused: %v", err)
	}
}

// TestPluginProvider_NoExecutionSite: a plugin provider that is NOT registered
// (disabled, removed, switch off) never executes at any site, and the lazy
// mode probe (provider.list, callable by an agent) never starts a registered
// plugin's code either.
func TestPluginProvider_NoExecutionSite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script marker")
	}
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := providers.Spec{ID: "acme/agent", Plugin: "acme", Runner: providers.RunnerACP, Command: []string{"agent"}, CommandForm: "path", PluginRoot: filepath.Dir(script), PluginData: t.TempDir()}
	reg := providers.New() // NOT registered: the plugin is disabled or gone
	r := New(nil, WithProviderRegistry(reg), WithACPModeProbe())
	sites := map[string]func() error{
		"probeACPModes": func() error { return r.probeACPModes(context.Background(), "acme/agent") },
		"runACP": func() error {
			return r.runACP(context.Background(), 1, 1, "acme/agent", t.TempDir(), "hi", nil, PermissionSettings{}, "", make(chan Event, 16))
		},
	}
	for name, site := range sites {
		if err := site(); err == nil {
			t.Errorf("%s: an unregistered plugin provider launched without error", name)
		}
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Fatalf("%s EXECUTED the plugin's command", name)
		}
	}
	// Registered: the lazy probe (reached from provider.list) must not run it.
	if p := reg.SetPlugins(map[string][]providers.Spec{"acme": {spec}}); len(p) != 0 {
		t.Fatal(p)
	}
	r.maybeProbeACPModes("acme/agent")
	r.ProviderCatalog()
	time.Sleep(500 * time.Millisecond)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the lazy probe (provider.list) EXECUTED a registered plugin's command")
	}
}
