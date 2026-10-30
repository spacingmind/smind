package taskrunner

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

// acpConformanceIDs are the five built-in ACP providers registered purely
// as data in internal/providers (opencode, copilot, cline, goose, cursor):
// none of them has any vendor-specific code in this package, so each must
// pass the same fake-agent conformance scenarios through the shared runACP
// path. These tests override the provider's command through the real
// WithACPCommand seam (not the test-only newACPClient override), which is
// the one substitution mechanism RunPrompt's ACP path actually exposes.
var acpConformanceIDs = []string{
	providers.IDOpenCode,
	providers.IDCopilot,
	providers.IDCline,
	providers.IDGoose,
	providers.IDCursor,
}

// otherACPCommandOpts points every ACP provider except skip at an
// unspawnable path. ProviderCatalog under WithACPModeProbe kicks a
// background probe for every ACP provider whose catalog is unknown, and
// several of these CLIs (opencode, cursor-agent, npx for GLM) are real
// binaries on a dev host -- without this, one catalog read would spawn
// them all. Mirrors acp_modes_test.go's WithACPCommand(ProviderKimi,
// "/nonexistent/kimi") guard.
func otherACPCommandOpts(skip Provider) []Option {
	var opts []Option
	for _, id := range append([]string{providers.IDGLM, providers.IDKimi}, acpConformanceIDs...) {
		if p := Provider(id); p != skip {
			opts = append(opts, WithACPCommand(p, []string{"/nonexistent/agent"}))
		}
	}
	return opts
}

// TestACPDiscovery_ConfigOptionsModelsAndModes proves mode/model discovery
// for the five data-registered ACP providers is entirely generic: each
// provider's probe reads the same category-"mode" select option (modes) and
// category-"model" select option (models) out of the fake agent's
// session/new configOptions, with no per-vendor code anywhere in the loop.
func TestACPDiscovery_ConfigOptionsModelsAndModes(t *testing.T) {
	for _, id := range acpConformanceIDs {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			p := Provider(id)
			opts := append([]Option{
				WithACPModeProbe(),
				WithACPCommand(p, []string{fakeACPAgentPath, "modes:config", "models"}),
			}, otherACPCommandOpts(p)...)
			r := New(nil, opts...)

			info := waitForDiscoveredModes(t, r, p)
			if !info.ModesDiscovered {
				t.Fatalf("%s ModesDiscovered = false", p)
			}
			if got := strings.Join(modeIDs(info.Modes), ","); got != "default,accept_edits,bypass_permissions" {
				t.Fatalf("%s modes = %s, want default,accept_edits,bypass_permissions", p, got)
			}
			if info.DefaultMode != "default" {
				t.Fatalf("%s default mode = %q, want %q", p, info.DefaultMode, "default")
			}

			if !info.ModelsDiscovered {
				t.Fatalf("%s ModelsDiscovered = false (models: %+v)", p, info.Models)
			}
			wantModels := []string{"fake-model-a", "fake-model-b"}
			if len(info.Models) != len(wantModels) {
				t.Fatalf("%s models = %+v, want exactly %v", p, info.Models, wantModels)
			}
			for i, want := range wantModels {
				m := info.Models[i]
				if m.ID != want || m.Source != ModelSourceDiscovered {
					t.Fatalf("%s models[%d] = %+v, want id %q with source %q", p, i, m, want, ModelSourceDiscovered)
				}
				// The agent reports fake-model-a as its current value, so
				// discovery must mark it the default.
				if m.Default != (want == "fake-model-a") {
					t.Fatalf("%s models[%d].Default = %v, want %v", p, i, m.Default, want == "fake-model-a")
				}
			}
		})
	}
}
