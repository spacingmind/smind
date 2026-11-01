package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
)

// PD1 registers plugin providers; the scrubbed, confined launch is PD2. Until
// it lands the launch env builder (every spawn and the ACP probe go through
// it) refuses a plugin provider, so no plugin command is ever executed.
func TestPluginProvider_LaunchRefusedUntilScrubbedLaunch(t *testing.T) {
	reg := providers.New()
	if p := reg.SetPlugins(map[string][]providers.Spec{"acme": {{ID: "acme/agent", Plugin: "acme", Runner: providers.RunnerACP, Command: []string{"/x/agent"}}}}); len(p) != 0 {
		t.Fatal(p)
	}
	r := New(nil, WithProviderRegistry(reg))
	if _, err := r.launchEnvEntries("acme/agent"); err == nil || !strings.Contains(err.Error(), "cannot run in this build yet") {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.launchEnvEntries(ProviderGLM); err != nil && strings.Contains(err.Error(), "cannot run in this build yet") {
		t.Fatalf("a core provider was refused: %v", err)
	}
}

// TestPluginProvider_NoExecutionSite is the table over every place a provider
// command can be executed from the taskrunner: for a plugin provider whose
// argv[0] writes a marker when run, none of them may run it before PD2.
// (provider.test's version probe is the wsapi site: see
// TestProviderTest_PluginProvider_NeverExecutesTheCommand. The other
// exec sites of the daemon run git or ps, never a provider command.)
func TestPluginProvider_NoExecutionSite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script marker")
	}
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := providers.New()
	if p := reg.SetPlugins(map[string][]providers.Spec{"acme": {{ID: "acme/agent", Plugin: "acme", Runner: providers.RunnerACP, Command: []string{script}}}}); len(p) != 0 {
		t.Fatal(p)
	}
	r := New(nil, WithProviderRegistry(reg))
	sites := map[string]func() error{
		"launchEnvEntries": func() error { _, err := r.launchEnvEntries("acme/agent"); return err },
		"probeACPModes":    func() error { return r.probeACPModes(context.Background(), "acme/agent") },
		"runACP": func() error {
			return r.runACP(context.Background(), 1, 1, "acme/agent", t.TempDir(), "hi", nil, PermissionSettings{}, "", make(chan Event, 16))
		},
		"maybeProbeACPModes": func() error {
			r.maybeProbeACPModes("acme/agent")
			time.Sleep(300 * time.Millisecond)
			return errors.New("async")
		},
	}
	for name, site := range sites {
		err := site()
		if err == nil {
			t.Errorf("%s: a plugin provider launched without error", name)
		}
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Fatalf("%s EXECUTED the plugin's command", name)
		}
	}
}
