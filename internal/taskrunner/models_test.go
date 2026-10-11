package taskrunner

import (
	"strings"
	"testing"
	"time"
)

func TestModel_ValidationRejectsBadStrings(t *testing.T) {
	t.Parallel()
	for name, bad := range map[string]string{
		"leading dash": "-x",
		"flag":         "--model",
		"space":        "claude opus",
		"newline":      "opus\n--evil",
		"too long":     strings.Repeat("a", 101),
		"semicolon":    "a;b",
		"quote":        `a"b`,
		"unicode":      "mödel",
	} {
		if err := ValidateModelSyntax(bad); err == nil {
			t.Errorf("%s: ValidateModelSyntax(%q) = nil, want error", name, bad)
		}
		claude, _ := ProviderInfoFor(ProviderClaudeNative)
		if err := ValidateModel(claude, bad); err == nil {
			t.Errorf("%s: ValidateModel(claude, %q) = nil, want error", name, bad)
		}
	}
	for _, ok := range []string{"", "sonnet", "claude-opus-5-5", "glm-4.6", "a/b:c@d[1m]", strings.Repeat("a", 100), "gpt_5.1-codex"} {
		if err := ValidateModelSyntax(ok); err != nil {
			t.Errorf("ValidateModelSyntax(%q) = %v, want nil", ok, err)
		}
	}
}

func TestProviderList_ClaudeStaticModels(t *testing.T) {
	t.Parallel()
	r := New(nil)
	p, ok := r.ProviderCatalogFor(ProviderClaudeNative)
	if !ok {
		t.Fatal("no claude-native provider")
	}
	if p.ModelsDiscovered {
		t.Fatal("claude catalog is static: modelsDiscovered must be false")
	}
	ids := map[string]bool{}
	for _, m := range p.Models {
		ids[m.ID] = true
	}
	for _, want := range []string{"sonnet", "opus", "haiku", "claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		if !ids[want] {
			t.Errorf("claude models missing %q: %+v", want, p.Models)
		}
	}
	// Not an allowlist: a model released after this build is accepted.
	if err := ValidateModel(p, "claude-future-9-9"); err != nil {
		t.Errorf("ValidateModel(claude, future id) = %v, want nil", err)
	}
}

func waitForDiscoveredModels(t *testing.T, r *Runner, provider Provider) ProviderInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p, _ := r.ProviderCatalogFor(provider); p.ModelsDiscovered {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s models never discovered", provider)
	return ProviderInfo{}
}

func TestProviderList_ACPModelsFromProbe(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, "models"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p := waitForDiscoveredModels(t, r, ProviderGLM)
	if len(p.Models) != 2 || p.Models[0].ID != "fake-model-a" || !p.Models[0].Default || p.Models[1].ID != "fake-model-b" || p.Models[1].Default {
		t.Fatalf("glm models = %+v, want fake-model-a (default), fake-model-b", p.Models)
	}
	c, _ := r.acpModelCatalog(ProviderGLM)
	if c.configID != "model" {
		t.Fatalf("model configID = %q, want model", c.configID)
	}
	if err := ValidateModel(p, "fake-model-b"); err != nil {
		t.Errorf("ValidateModel(known) = %v", err)
	}
	err := ValidateModel(p, "nope")
	if err == nil || !strings.Contains(err.Error(), "fake-model-a, fake-model-b") {
		t.Errorf("ValidateModel(unknown) = %v, want an error listing the valid ids", err)
	}
}

func TestProviderList_UndiscoveredAcceptsString(t *testing.T) {
	t.Parallel()
	// No probe enabled: nothing is discovered for any provider.
	r := New(nil)
	for _, id := range []Provider{ProviderGLM, ProviderKimi, ProviderCodexNative} {
		p, _ := r.ProviderCatalogFor(id)
		if p.ModelsDiscovered || len(p.Models) != 0 {
			t.Fatalf("%s catalog = %+v, want no discovered models", id, p)
		}
		if err := ValidateModel(p, "whatever-the-agent-knows"); err != nil {
			t.Errorf("%s: ValidateModel(undiscovered) = %v, want nil", id, err)
		}
		if err := ValidateModel(p, "-bad"); err == nil {
			t.Errorf("%s: syntax is still checked when undiscovered", id)
		}
	}
}

func TestProviderList_CodexModelsFromModelList(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithCodexCommand([]string{fakeCodexAgentPath}), WithACPCommand(ProviderGLM, []string{"/nonexistent/glm"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p := waitForDiscoveredModels(t, r, ProviderCodexNative)
	var ids []string
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	// Both pages, hidden model skipped.
	if got := strings.Join(ids, ","); got != "gpt-fake-a,gpt-fake-b" {
		t.Fatalf("codex models = %s, want gpt-fake-a,gpt-fake-b", got)
	}
	if !p.Models[0].Default || p.Models[0].Label != "GPT Fake A" {
		t.Fatalf("codex first model = %+v", p.Models[0])
	}
	if err := ValidateModel(p, "gpt-unknown"); err == nil {
		t.Error("discovered codex catalog should reject an unknown id")
	}
}

// An app-server without model/list leaves the provider with no models and
// any syntax-valid string accepted.
func TestProviderList_CodexModelListUnavailableFallback(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithCodexCommand([]string{fakeCodexAgentPath, "nomodellist"}), WithACPCommand(ProviderGLM, []string{"/nonexistent/glm"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p, _ := r.ProviderCatalogFor(ProviderCodexNative) // kicks the probe
	time.Sleep(500 * time.Millisecond)
	p, _ = r.ProviderCatalogFor(ProviderCodexNative)
	if p.ModelsDiscovered || len(p.Models) != 0 {
		t.Fatalf("catalog = %+v, want no models", p)
	}
	if err := ValidateModel(p, "gpt-anything"); err != nil {
		t.Fatalf("ValidateModel() = %v, want the string accepted", err)
	}
}
