package taskrunner

import (
	"strings"
	"testing"

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
