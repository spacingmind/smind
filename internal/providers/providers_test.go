package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
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
		"extends unknown":     {[]Spec{{ID: "e", Extends: "nope"}}, "[0].extends"},
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

func TestKimiCommand_KimiCode(t *testing.T) {
	t.Parallel()
	k, ok := New().Get(IDKimi)
	if !ok || strings.Join(k.Command, " ") != "kimi acp" || k.Runner != RunnerACP {
		t.Fatalf("kimi spec = %+v", k)
	}
	if got := strings.Join(acp.KimiCommand(), " "); got != "kimi acp" {
		t.Fatalf("acp.KimiCommand() = %q", got)
	}
	// The docs name Kimi Code (MoonshotAI/kimi-code), not the archived kimi-cli install.
	doc, err := os.ReadFile(filepath.Join("..", "..", "public-docs", "providers.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	if !strings.Contains(text, "MoonshotAI/kimi-code") || strings.Contains(text, "pip install kimi-cli") {
		t.Fatal("public-docs/providers.md must name Kimi Code (MoonshotAI/kimi-code) and not `pip install kimi-cli`")
	}
	src, err := os.ReadFile(filepath.Join("..", "acp", "kimi.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "MoonshotAI/kimi-code") || strings.Contains(string(src), "pip install kimi-cli") {
		t.Fatal("internal/acp/kimi.go comments must name Kimi Code")
	}
}

func TestSetCustom_InstanceOverlays(t *testing.T) {
	t.Parallel()
	r := New()
	err := r.SetCustom([]Spec{
		{ID: "base-acp", Command: []string{"base-acp", "--acp"}, Models: []Model{{ID: "a"}, {ID: "b"}}},
		{ID: "acp-fast", Extends: "base-acp", Command: []string{"other", "--acp"}, ModelsMode: ModelsAdd, Models: []Model{{ID: "b", Label: "B2"}, {ID: "c"}}},
		{ID: "kimi-2", Extends: IDKimi, Command: []string{"kimi-beta", "acp"}},
		{ID: "cl", Extends: IDClaudeNative, DisallowedTools: []string{"WebSearch"}, ModelAliases: map[string]string{"opus": "glm-5.3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := r.Get("acp-fast")
	if f.Runner != RunnerACP || f.Base() != "base-acp" || f.Command[0] != "other" || f.HealthCommand[0] != "other" {
		t.Fatalf("acp instance = %+v", f)
	}
	if len(f.Models) != 3 || f.Models[1].Label != "B2" || f.Models[2].ID != "c" {
		t.Fatalf("add mode models = %+v", f.Models)
	}
	k, _ := r.Get("kimi-2")
	if k.Runner != RunnerACP || k.Command[0] != "kimi-beta" || k.Delivery != DeliveryPreface {
		t.Fatalf("kimi instance = %+v", k)
	}
	if base, _ := r.Get("base-acp"); len(base.Models) != 2 || base.Command[0] != "base-acp" {
		t.Fatalf("base changed: %+v", base)
	}
	c, _ := r.Get("cl")
	if c.Harness() != HarnessClaudeCode || len(c.DisallowedTools) != 1 || c.ModelAliases["opus"] != "glm-5.3" {
		t.Fatalf("claude instance = %+v", c)
	}
	// An instance of an unknown or instance provider is refused, naming the field.
	if err := r.SetCustom([]Spec{{ID: "x", Extends: "nope"}}); err == nil || !strings.Contains(err.Error(), "[0].extends") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstance_EndpointCarried(t *testing.T) {
	t.Parallel()
	r := New()
	err := r.SetCustom([]Spec{
		{ID: "codex-gw", Extends: IDCodexNative, Endpoint: Endpoint{Kind: EndpointOpenAIResponses, BaseURL: "https://gw.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	inst, ok := r.Get("codex-gw")
	if !ok {
		t.Fatal("codex-gw: not in the registry")
	}
	if !inst.HasEndpoint() || inst.Endpoint.Kind != EndpointOpenAIResponses || inst.Endpoint.BaseURL != "https://gw.example" {
		t.Fatalf("instance endpoint = %+v", inst.Endpoint)
	}
	if base, _ := r.Get(IDCodexNative); base.HasEndpoint() || base.Endpoint != (Endpoint{}) {
		t.Fatalf("base spec endpoint = %+v", base.Endpoint)
	}
}
