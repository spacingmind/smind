package taskrunner

import "testing"

func TestRunnerDefaultModel(t *testing.T) {
	r := New(nil, WithDefaultModel(ProviderClaudeNative, "sonnet"))
	if got := r.DefaultModel(ProviderClaudeNative); got != "sonnet" {
		t.Fatalf("DefaultModel(claude-native) = %q, want sonnet", got)
	}
	if got := r.DefaultModel(ProviderGLM); got != "" {
		t.Fatalf("DefaultModel(glm) = %q, want empty", got)
	}
	// The catalog reports it, so the UI shows what a run resolves to.
	for _, p := range r.ProviderCatalog() {
		want := ""
		if p.ID == ProviderClaudeNative {
			want = "sonnet"
		}
		if p.DefaultModel != want {
			t.Errorf("ProviderCatalog()[%s].DefaultModel = %q, want %q", p.ID, p.DefaultModel, want)
		}
	}
	// An empty value clears it.
	r = New(nil, WithDefaultModel(ProviderClaudeNative, "sonnet"), WithDefaultModel(ProviderClaudeNative, ""))
	if got := r.DefaultModel(ProviderClaudeNative); got != "" {
		t.Fatalf("DefaultModel after clearing = %q, want empty", got)
	}
}
