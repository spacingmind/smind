package providers

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
)

func TestBuiltinsAreRegistered(t *testing.T) {
	t.Parallel()
	got := strings.Join(New().IDs(), ",")
	if got != "claude-native,cline,codex-native,copilot,cursor,glm,goose,kimi,opencode" {
		t.Fatalf("built-in ids = %s", got)
	}
	wantOrder := strings.Join([]string{IDClaudeNative, IDGLM, IDKimi, IDCodexNative, IDOpenCode, IDCopilot, IDCline, IDGoose, IDCursor}, ",")
	if got := strings.Join(BuiltinIDs(), ","); got != wantOrder {
		t.Fatalf("BuiltinIDs order = %s, want %s", got, wantOrder)
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
		if _, ok := r.Get("ok"); !ok || len(r.List()) != 10 {
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

// TestSetCustom_InstanceInheritsNoMCPServers: NoMCPServers is sticky -- an
// instance of a base that declares it cannot take mcpServers inherits the
// flag (and cannot re-enable them), and an instance may declare it itself.
func TestSetCustom_InstanceInheritsNoMCPServers(t *testing.T) {
	t.Parallel()
	r := New()
	err := r.SetCustom([]Spec{
		{ID: "base-acp", Command: []string{"base-acp"}, NoMCPServers: true},
		{ID: "inst", Extends: "base-acp"},
		{ID: "inst-no-mcp", Extends: IDKimi, NoMCPServers: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Get("inst"); !s.NoMCPServers {
		t.Fatalf("inst = %+v, want NoMCPServers inherited from the base", s)
	}
	if s, _ := r.Get("inst-no-mcp"); !s.NoMCPServers {
		t.Fatalf("inst-no-mcp = %+v, want its own NoMCPServers", s)
	}
	if s, _ := r.Get(IDKimi); s.NoMCPServers {
		t.Fatalf("kimi = %+v, want NoMCPServers false", s)
	}
}

func TestACPProvider_AllFive_RowsRegister(t *testing.T) {
	t.Parallel()
	r := New()
	wantCommand := map[string][]string{
		IDOpenCode: {"opencode", "acp"},
		IDCopilot:  {"copilot", "--acp"},
		IDCline:    {"cline", "--acp"},
		IDGoose:    {"goose", "acp"},
		IDCursor:   {"cursor-agent", "acp"},
	}
	for _, id := range []string{IDOpenCode, IDCopilot, IDCline, IDGoose, IDCursor} {
		s, ok := r.Get(id)
		if !ok {
			t.Fatalf("%s: not registered", id)
		}
		if cmd, ok := wantCommand[id]; !ok || !reflect.DeepEqual(cmd, s.Command) {
			t.Errorf("%s: command = %v, want %v", id, s.Command, cmd)
		}
		if s.Runner != RunnerACP || len(s.Command) < 1 || s.HealthCommand[0] != s.Command[0] {
			t.Errorf("%s: acp spec = %+v", id, s)
		}
		if s.Kind != "cli" || s.Delivery != DeliveryPreface {
			t.Errorf("%s: catalog = %+v", id, s)
		}
		if s.Health.Install == "" {
			t.Errorf("%s: no Health.Install", id)
		}
		if s.Health.VersionArg != "--version" {
			t.Errorf("%s: Health.VersionArg = %q, want --version", id, s.Health.VersionArg)
		}
		if !IsBuiltin(id) || !ReservedID(id) {
			t.Errorf("%s: built-in but not reserved", id)
		}
	}
	if s, _ := r.Get(IDCursor); s.Health.MinVersion != "2026.01.01" {
		t.Errorf("cursor: Health.MinVersion = %q, want 2026.01.01", s.Health.MinVersion)
	}
	for _, id := range []string{IDCopilot, IDCline, IDGoose, IDCursor} {
		if s, _ := r.Get(id); !s.Health.Unverified {
			t.Errorf("%s: Health.Unverified must be true", id)
		}
	}
	if s, _ := r.Get(IDOpenCode); s.Health.Unverified {
		t.Error("opencode: Health.Unverified must be false")
	}
}

func TestACPProvider_OpenCode_SpecRegistersWithoutSwitchEdits(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.Register(Spec{ID: "opencode-lab", Runner: RunnerACP, Command: []string{"opencode", "acp"}}); err != nil {
		t.Fatal(err)
	}
	lab, _ := r.Get("opencode-lab")
	if lab.Runner != RunnerACP || !lab.ToolEvents || lab.Delivery != DeliveryPreface {
		t.Fatalf("registered spec = %+v", lab)
	}
	b, _ := r.Get(IDOpenCode)
	if len(b.Command) != 2 || b.Command[0] != "opencode" || b.Command[1] != "acp" {
		t.Fatalf("builtin opencode command = %v", b.Command)
	}
	if err := r.Register(Spec{ID: "oc2", Extends: IDOpenCode, Env: map[string]string{"X": "1"}}); err != nil {
		t.Fatal(err)
	}
	inst, _ := r.Get("oc2")
	if inst.Runner != RunnerACP || strings.Join(inst.Command, " ") != "opencode acp" || inst.Health.Install != b.Health.Install {
		t.Fatalf("opencode instance = %+v", inst)
	}
	if inst.Env["X"] != "1" {
		t.Fatalf("instance env = %v", inst.Env)
	}
}

func TestACPProvider_OpenCodePermissionEnvAsks(t *testing.T) {
	t.Parallel()
	s, _ := New().Get(IDOpenCode)
	if got := s.Env["OPENCODE_PERMISSION"]; got != `{"bash":"ask","edit":"ask"}` {
		t.Fatalf("OPENCODE_PERMISSION = %q", got)
	}
	if s.Env["OPENCODE_DISABLE_AUTOUPDATE"] != "1" {
		t.Fatalf("OPENCODE_DISABLE_AUTOUPDATE = %q", s.Env["OPENCODE_DISABLE_AUTOUPDATE"])
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
