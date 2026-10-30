package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
)

// readAgentEnvDump reads a FAKEAGENT_ENV_DUMP file (one KEY=value per
// line, the spawned fake agent's own environment) into a map.
func readAgentEnvDump(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	return env
}

// TestACPProvider_ProbeUsesNoBrowser proves the mode/model probe always
// sets NO_BROWSER in the spawned agent's environment (a probe must never
// open a browser), while a provider-configured NO_BROWSER still wins over
// the probe's own default.
func TestACPProvider_ProbeUsesNoBrowser(t *testing.T) {
	if v := os.Getenv("NO_BROWSER"); v != "" {
		t.Skipf("host environment already sets NO_BROWSER=%s", v)
	}
	dump := filepath.Join(t.TempDir(), "env-dump")
	t.Setenv("FAKEAGENT_ENV_DUMP", dump)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("probe default", func(t *testing.T) {
		r := New(nil, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath}))
		if err := r.probeACPModes(ctx, ProviderGLM); err != nil {
			t.Fatalf("probeACPModes() error = %v", err)
		}
		if got := readAgentEnvDump(t, dump)["NO_BROWSER"]; got != "true" {
			t.Fatalf("probe agent NO_BROWSER = %q, want true", got)
		}
	})

	t.Run("configured value wins", func(t *testing.T) {
		r, _ := customRunner(t, nil, providers.Spec{
			ID: "browserful-agent", Command: []string{fakeACPAgentPath},
			Env: map[string]string{"NO_BROWSER": "0"},
		})
		if err := r.probeACPModes(ctx, "browserful-agent"); err != nil {
			t.Fatalf("probeACPModes() error = %v", err)
		}
		if got := readAgentEnvDump(t, dump)["NO_BROWSER"]; got != "0" {
			t.Fatalf("probe agent NO_BROWSER = %q, want the configured 0 to win over the probe default", got)
		}
	})
}

// TestACPProvider_RunDoesNotSetNoBrowser is the flip side of the probe:
// a real run never injects NO_BROWSER, so a user's agent keeps whatever
// browser behavior it has (unless the provider's own env configures it).
func TestACPProvider_RunDoesNotSetNoBrowser(t *testing.T) {
	if v := os.Getenv("NO_BROWSER"); v != "" {
		t.Skipf("host environment already sets NO_BROWSER=%s", v)
	}
	dump := filepath.Join(t.TempDir(), "env-dump")
	t.Setenv("FAKEAGENT_ENV_DUMP", dump)

	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	r := New(wm, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath}))
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if v, ok := readAgentEnvDump(t, dump)["NO_BROWSER"]; ok {
		t.Fatalf("run agent env NO_BROWSER = %q, want unset", v)
	}
}

// waitForDiscoveredModes polls ProviderCatalog (which kicks the
// background probe) until provider's modes are discovered.
func waitForDiscoveredModes(t *testing.T, r *Runner, provider Provider) ProviderInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p, _ := r.ProviderCatalogFor(provider); p.ModesDiscovered {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s modes never discovered", provider)
	return ProviderInfo{}
}

// S4: the probe reads SessionModeState from session/new, caches it, and
// a second catalog read doesn't respawn the agent.
func TestProviderCatalog_ProbesACPSessionModes(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, "modes:session"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))

	p := waitForDiscoveredModes(t, r, ProviderGLM)
	if got := strings.Join(modeIDs(p.Modes), ","); got != "default,accept_edits,bypass_permissions" {
		t.Fatalf("glm modes = %s", got)
	}
	if p.DefaultMode != "default" {
		t.Fatalf("glm default = %q, want default", p.DefaultMode)
	}
	if !p.Modes[2].AutoApproves || p.Modes[1].AutoApproves {
		t.Fatalf("autoApproves = %+v, want only bypass_permissions marked", p.Modes)
	}

	// Cached: pointing the command at something unspawnable changes
	// nothing, since no second probe runs.
	r.acpCommands[ProviderGLM] = []string{"/nonexistent/agent"}
	if p, _ := r.ProviderCatalogFor(ProviderGLM); !p.ModesDiscovered {
		t.Fatal("catalog lost discovered modes on second read")
	}
}

// S5: an agent exposing its modes as a category-"mode" select config
// option (no SessionModeState) gets a catalog from those choices.
func TestProviderCatalog_ProbesACPModeConfigOption(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, "modes:config"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p := waitForDiscoveredModes(t, r, ProviderGLM)
	if got := strings.Join(modeIDs(p.Modes), ","); got != "default,accept_edits,bypass_permissions" {
		t.Fatalf("glm modes = %s", got)
	}
	c, _ := r.acpCatalog(ProviderGLM)
	if c.configID != "mode" {
		t.Fatalf("configID = %q, want mode", c.configID)
	}
}

// S6: a probe that can't spawn (or an agent advertising no modes) never
// blocks the catalog, which keeps serving the fallback.
func TestProviderCatalog_ProbeFailureKeepsFallback(t *testing.T) {
	t.Parallel()
	for name, cmd := range map[string][]string{
		"unspawnable": {"/nonexistent/agent"},
		"no modes":    {fakeACPAgentPath},
	} {
		t.Run(name, func(t *testing.T) {
			r := New(nil, WithACPModeProbe(), WithACPCommand(ProviderGLM, cmd), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
			start := time.Now()
			p, _ := r.ProviderCatalogFor(ProviderGLM)
			if time.Since(start) > time.Second {
				t.Fatalf("ProviderCatalog blocked %v", time.Since(start))
			}
			if p.ModesDiscovered || strings.Join(modeIDs(p.Modes), ",") != "default" {
				t.Fatalf("catalog = %+v, want the [default] fallback", p)
			}
			// Let the probe finish; still the fallback, and no retry
			// storm (lastAttempt is recent).
			time.Sleep(500 * time.Millisecond)
			if p, _ := r.ProviderCatalogFor(ProviderGLM); p.ModesDiscovered {
				t.Fatalf("catalog = %+v after failed probe, want fallback", p)
			}
		})
	}
}

// Without WithACPModeProbe no agent is ever spawned just to list modes.
func TestProviderCatalog_NoProbeByDefault(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, "modes:session"}))
	_ = r.ProviderCatalog()
	time.Sleep(300 * time.Millisecond)
	if p, _ := r.ProviderCatalogFor(ProviderGLM); p.ModesDiscovered {
		t.Fatal("modes discovered without WithACPModeProbe")
	}
}
