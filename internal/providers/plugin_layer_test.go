package providers

import (
	"strings"
	"testing"
)

func pluginSpec(plugin, id string) Spec {
	return Spec{ID: plugin + "/" + id, Label: id, Plugin: plugin, Runner: RunnerACP, Command: []string{"bin/" + id}}
}

func TestRegistry_Layers_ReloadKeepsOtherLayer(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.SetCustom([]Spec{{ID: "agent-x", Command: []string{"agent-x"}}}); err != nil {
		t.Fatal(err)
	}
	if p := r.SetPlugins(map[string][]Spec{"acme": {pluginSpec("acme", "a1")}}); len(p) != 0 {
		t.Fatalf("problems = %v", p)
	}
	// A provider.reload replaces the custom layer: the plugin layer stays.
	if err := r.SetCustom([]Spec{{ID: "agent-y", Command: []string{"agent-y"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("acme/a1"); !ok {
		t.Fatal("a custom reload dropped the plugin provider")
	}
	if _, ok := r.Get("agent-x"); ok {
		t.Fatal("old custom entry survived its own reload")
	}
	// A plugin change replaces the plugin layer: custom stays.
	r.SetPlugins(nil)
	if _, ok := r.Get("agent-y"); !ok {
		t.Fatal("a plugin change dropped the custom entry")
	}
	if _, ok := r.Get("acme/a1"); ok {
		t.Fatal("plugin provider survived an empty plugin layer")
	}
}

func TestRegistry_SetPlugins_Atomic_BadEntryRejectsThatPluginOnly(t *testing.T) {
	t.Parallel()
	r := New()
	bad := []Spec{pluginSpec("bad", "ok"), {ID: "claude-native", Plugin: "bad", Runner: RunnerACP, Command: []string{"x"}}}
	problems := r.SetPlugins(map[string][]Spec{
		"good": {pluginSpec("good", "one"), pluginSpec("good", "two")},
		"bad":  bad,
	})
	if len(problems) != 1 || problems[0].Plugin != "bad" || !strings.Contains(problems[0].Err.Error(), "claude-native") {
		t.Fatalf("problems = %+v", problems)
	}
	if _, ok := r.Get("good/two"); !ok {
		t.Fatal("a good plugin was dropped with the bad one")
	}
	if _, ok := r.Get("bad/ok"); ok {
		t.Fatal("a bad plugin's first entry registered: not atomic per plugin")
	}
}

func TestRegistry_SetPlugins_OrderAndShape(t *testing.T) {
	t.Parallel()
	r := New()
	r.SetPlugins(map[string][]Spec{"zed": {pluginSpec("zed", "z")}, "acme": {pluginSpec("acme", "b"), pluginSpec("acme", "a")}})
	var got []string
	for _, s := range r.List() {
		if s.Plugin != "" {
			got = append(got, s.ID)
		}
	}
	if strings.Join(got, ",") != "acme/b,acme/a,zed/z" {
		t.Fatalf("order = %v", got)
	}
	s, _ := r.Get("acme/a")
	if !s.Custom || s.Runner != RunnerACP || s.Reflect || s.AccountProvider != "" || s.Delivery != DeliveryPreface {
		t.Fatalf("spec = %+v", s)
	}
}

func TestSetPlugins_RefusesWrongNamespaceAndExtends(t *testing.T) {
	t.Parallel()
	r := New()
	cases := map[string]Spec{
		"no slash":       {ID: "glm", Plugin: "acme", Runner: RunnerACP, Command: []string{"x"}},
		"other plugin":   {ID: "other/x", Plugin: "acme", Runner: RunnerACP, Command: []string{"x"}},
		"two slashes":    {ID: "acme/a/b", Plugin: "acme", Runner: RunnerACP, Command: []string{"x"}},
		"extends a core": {ID: "acme/x", Plugin: "acme", Extends: IDClaudeNative},
	}
	for name, s := range cases {
		if p := r.SetPlugins(map[string][]Spec{"acme": {s}}); len(p) != 1 {
			t.Errorf("%s: want one problem, got %v", name, p)
		}
	}
}

func TestPluginProvider_ID_CannotCollideWithCoreOrCustom(t *testing.T) {
	t.Parallel()
	r := New()
	custom := []Spec{{ID: "agent-x", Command: []string{"x"}}, {ID: "my.agent_2", Command: []string{"x"}}}
	if err := r.SetCustom(custom); err != nil {
		t.Fatal(err)
	}
	// No core or custom id contains "/", and every plugin id does, so no
	// "<plugin>/<provider>" can equal one; a plugin entry claiming such an id is refused.
	for _, s := range r.List() {
		if strings.Contains(s.ID, "/") {
			t.Errorf("core/custom id %q contains a slash", s.ID)
		}
	}
	for _, id := range append(BuiltinIDs(), "agent-x", "my.agent_2") {
		if p := r.SetPlugins(map[string][]Spec{"acme": {{ID: id, Plugin: "acme", Runner: RunnerACP, Command: []string{"x"}}}}); len(p) != 1 {
			t.Errorf("a plugin entry named %q was accepted", id)
		}
	}
}
