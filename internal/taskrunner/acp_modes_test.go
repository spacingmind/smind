package taskrunner

import (
	"strings"
	"testing"
	"time"
)

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
