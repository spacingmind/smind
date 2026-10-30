package providers

import "testing"

// A Spec handed out by the registry is a deep copy: mutating its secret-env
// map or passthrough slice never reaches the registry's own entry.
func TestSpec_CloneCopiesSecretEnvAndPassthrough(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.SetCustom([]Spec{{ID: "cl", Extends: IDClaudeNative,
		SecretEnv:      map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"},
		PassthroughEnv: []string{"ANTHROPIC_AUTH_TOKEN"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Spec{ID: "agent-s", Command: []string{"agent-s"},
		Env:            map[string]string{"SOME_FLAG": "1"},
		SecretEnv:      map[string]string{"MY_VENDOR_TOKEN": "zai-key"},
		PassthroughEnv: []string{"MY_VENDOR_TOKEN"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent-s", "cl"} {
		got, ok := r.Get(id)
		if !ok {
			t.Fatalf("%s: not registered", id)
		}
		for k := range got.SecretEnv {
			got.SecretEnv[k] = "mutated"
		}
		got.PassthroughEnv[0] = "MUTATED"
		again, _ := r.Get(id)
		for _, v := range again.SecretEnv {
			if v == "mutated" {
				t.Errorf("%s: SecretEnv shared with the registry: %v", id, again.SecretEnv)
			}
		}
		if again.PassthroughEnv[0] == "MUTATED" {
			t.Errorf("%s: PassthroughEnv shared with the registry: %v", id, again.PassthroughEnv)
		}
	}
	// The zero fields stay zero (nil), not empty-but-allocated.
	if s, _ := r.Get(IDClaudeNative); s.SecretEnv != nil || s.PassthroughEnv != nil {
		t.Fatalf("claude-native zero fields = %v %v", s.SecretEnv, s.PassthroughEnv)
	}
}
