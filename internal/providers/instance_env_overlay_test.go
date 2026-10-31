package providers

import "testing"

// An instance's env is an overlay on its base's, not a replacement: an
// opencode instance (plain or with an endpoint) must keep the base's
// OPENCODE_PERMISSION ask policy for bash and edit, or it would run OpenCode's
// own, more permissive defaults.
func TestInstance_EnvOverlaysTheBaseEnv(t *testing.T) {
	t.Parallel()
	builtin, ok := New().Get(IDOpenCode)
	if !ok || builtin.Env["OPENCODE_PERMISSION"] == "" {
		t.Fatal("the built-in opencode spec carries no OPENCODE_PERMISSION")
	}
	reg := New()
	if err := reg.SetCustom([]Spec{
		{ID: "oc-plain", Extends: IDOpenCode},
		{ID: "oc-own", Extends: IDOpenCode, Env: map[string]string{"MY_VAR": "mine"}},
		{ID: "oc-override", Extends: IDOpenCode, Env: map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "0"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"oc-plain", "oc-own", "oc-override"} {
		s, ok := reg.Get(id)
		if !ok {
			t.Fatalf("%s: not registered", id)
		}
		if s.Env["OPENCODE_PERMISSION"] != builtin.Env["OPENCODE_PERMISSION"] {
			t.Errorf("%s: OPENCODE_PERMISSION = %q, want the base's %q", id, s.Env["OPENCODE_PERMISSION"], builtin.Env["OPENCODE_PERMISSION"])
		}
	}
	if s, _ := reg.Get("oc-own"); s.Env["MY_VAR"] != "mine" {
		t.Errorf("the instance's own env was lost: %v", s.Env)
	}
	if s, _ := reg.Get("oc-override"); s.Env["OPENCODE_DISABLE_AUTOUPDATE"] != "0" {
		t.Errorf("the instance's value must win over the base's: %v", s.Env)
	}
	// The base is not modified by anything an instance sets.
	if again, _ := New().Get(IDOpenCode); again.Env["MY_VAR"] != "" || again.Env["OPENCODE_DISABLE_AUTOUPDATE"] != "1" {
		t.Errorf("an instance's env leaked into the base: %v", again.Env)
	}
}

func TestOverlayEnv(t *testing.T) {
	t.Parallel()
	if got := overlayEnv(nil, nil); got != nil {
		t.Errorf("overlayEnv(nil, nil) = %v, want nil", got)
	}
	base := map[string]string{"A": "1", "B": "2"}
	over := map[string]string{"B": "x", "C": "3"}
	got := overlayEnv(base, over)
	if got["A"] != "1" || got["B"] != "x" || got["C"] != "3" || len(got) != 3 {
		t.Errorf("overlayEnv = %v", got)
	}
	if base["B"] != "2" || len(base) != 2 || len(over) != 2 {
		t.Errorf("an input was modified: base %v over %v", base, over)
	}
}
