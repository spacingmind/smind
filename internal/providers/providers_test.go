package providers

import (
	"strings"
	"testing"
)

func TestBuiltinsAreRegistered(t *testing.T) {
	t.Parallel()
	got := strings.Join(New().IDs(), ",")
	if got != "claude-native,codex-native,glm,kimi" {
		t.Fatalf("built-in ids = %s", got)
	}
	for _, id := range BuiltinIDs() {
		if !IsBuiltin(id) || !ReservedID(id) {
			t.Errorf("%s: built-in but not reserved", id)
		}
	}
}

func TestSetCustom_ACPAndInstance(t *testing.T) {
	t.Parallel()
	r := New()
	err := r.SetCustom([]Spec{
		{ID: "agent-x", Label: "Agent X", Command: []string{"agent-x", "--acp"}, Env: map[string]string{"K": "v"}},
		{ID: "claude-s", Label: "Claude S", Extends: IDClaudeNative, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/d"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := r.Get("agent-x")
	if x.Runner != RunnerACP || !x.Custom || x.HealthCommand[0] != "agent-x" || x.Delivery != DeliveryPreface {
		t.Fatalf("acp entry = %+v", x)
	}
	c, _ := r.Get("claude-s")
	if c.Runner != RunnerClaude || c.Extends != IDClaudeNative || !c.IsInstance() || c.Base() != IDClaudeNative {
		t.Fatalf("instance = %+v", c)
	}
	// The instance runs the base's code but keeps its own reflector and account out of it.
	if c.Reflect || c.AccountProvider != "" || c.Delivery != DeliveryProjection || !c.ToolEvents {
		t.Fatalf("instance capabilities = %+v", c)
	}
	if c.Env["CLAUDE_CONFIG_DIR"] != "/d" {
		t.Fatalf("instance env = %v", c.Env)
	}
	if r.BaseOf("claude-s") != IDClaudeNative || r.BaseOf("agent-x") != "agent-x" || r.BaseOf("nope") != "" {
		t.Fatal("BaseOf")
	}
}

func TestSetCustom_RejectsAndChangesNothing(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.SetCustom([]Spec{{ID: "ok", Command: []string{"a"}}}); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		specs []Spec
		want  string
	}{
		"builtin shadow":      {[]Spec{{ID: "glm", Command: []string{"a"}}}, "[0].id"},
		"reserved prefix":     {[]Spec{{ID: "smind-x", Command: []string{"a"}}}, "reserved"},
		"duplicate":           {[]Spec{{ID: "d", Command: []string{"a"}}, {ID: "d", Command: []string{"b"}}}, "[1].id"},
		"no command":          {[]Spec{{ID: "a", Command: []string{"a"}}, {ID: "b"}}, "[1].command: required"},
		"extends unknown":     {[]Spec{{ID: "e", Extends: "glm"}}, "[0].extends"},
		"extends an instance": {[]Spec{{ID: "i1", Extends: IDClaudeNative}, {ID: "i2", Extends: "i1"}}, "[1].extends"},
	} {
		if err := r.SetCustom(tc.specs); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if _, ok := r.Get("ok"); !ok || len(r.List()) != 5 {
			t.Fatalf("%s: a rejected set changed the registry: %v", name, r.IDs())
		}
	}
}

func TestRegisterUnregister(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.Register(Spec{ID: "fake", Command: []string{"f"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Spec{ID: "fake", Command: []string{"f"}}); err == nil {
		t.Fatal("duplicate register accepted")
	}
	if !r.Unregister("fake") || r.Unregister("fake") || r.Unregister("glm") {
		t.Fatal("Unregister semantics")
	}
}
