package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

func TestCustomProvider_ConfigValidation_FieldNamed(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"no command", "providers:\n  custom:\n    - id: a\n      command: x\n    - id: b\n    - id: c\n      command: z\n", "providers.custom[1].command: required"},
		{"no id", "providers:\n  custom:\n    - command: x\n", "providers.custom[0].id: required"},
		{"bad id", "providers:\n  custom:\n    - id: \"Bad Id\"\n      command: x\n", "providers.custom[0].id"},
		{"duplicate id", "providers:\n  custom:\n    - {id: a, command: x}\n    - {id: a, command: y}\n", "providers.custom[1].id: duplicate"},
		{"shadows glm", "providers:\n  custom:\n    - {id: glm, command: x}\n", "providers.custom[0].id: \"glm\" is reserved"},
		{"shadows claude-native", "providers:\n  custom:\n    - {id: claude-native, extends: claude-native}\n", "providers.custom[0].id"},
		{"reserved prefix", "providers:\n  custom:\n    - {id: smind-agent, command: x}\n", "providers.custom[0].id"},
		{"extends unknown", "providers:\n  custom:\n    - {id: g2, extends: nope}\n", "providers.custom[0].extends"},
		{"instance args without command", "providers:\n  custom:\n    - {id: c2, extends: claude-native, args: [x]}\n", "providers.custom[0].args: needs command"},
		{"bad env name", "providers:\n  custom:\n    - id: a\n      command: x\n      env: {\"BAD-NAME\": v}\n", "providers.custom[0].env.BAD-NAME"},
		{"bad model id", "providers:\n  custom:\n    - id: a\n      command: x\n      models: [{id: \"-bad\"}]\n", "providers.custom[0].models[0].id"},
		{"duplicate mode", "providers:\n  custom:\n    - id: a\n      command: x\n      modes: [{id: m}, {id: m}]\n", "providers.custom[0].modes[1].id: duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, tt.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestCustomProvider_ErrorsNeverEchoEnvValues(t *testing.T) {
	useHome(t, "providers:\n  custom:\n    - id: a\n      command: x\n      env: {\"BAD NAME\": s3cret-token}\n")
	_, err := Load()
	if err == nil || strings.Contains(err.Error(), "s3cret-token") {
		t.Fatalf("err = %v, want an error without the env value", err)
	}
	useHome(t, "providers:\n  custom:\n    - id: i\n      extends: claude-native\n      env: {ANTHROPIC_API_KEY: s3cret-token}\n")
	_, err = Load()
	if err == nil || strings.Contains(err.Error(), "s3cret-token") {
		t.Fatalf("instance err = %v, want an error without the env value", err)
	}
}

func TestCustomProvider_LoadsACPAndInstance(t *testing.T) {
	// An absolute temp dir (a bare "/Users/x" is not absolute on Windows), single-quoted in the YAML.
	claudeS := filepath.Join(t.TempDir(), ".claude-s")
	useHome(t, `providers:
  claude-native:
    defaultModel: sonnet
  custom:
    - id: my-agent
      label: My agent
      command: my-agent
      args: ["--acp"]
      env: {MY_FLAG: abc}
      models: [{id: m1, label: Model 1, default: true}]
      modes: [{id: plan, label: Plan}, {id: yolo, autoApproves: true}]
    - id: claude-s
      label: Claude S
      extends: claude-native
      env: {CLAUDE_CONFIG_DIR: '`+claudeS+`'}
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel("claude-native") != "sonnet" || len(cfg.Providers.Custom) != 2 {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	specs := ProviderSpecs(cfg.Providers.Custom)
	if got := strings.Join(specs[0].Command, " "); got != "my-agent --acp" || specs[0].Models[0].ID != "m1" || !specs[0].Modes[1].AutoApproves {
		t.Fatalf("acp spec = %+v", specs[0])
	}
	if specs[1].Extends != "claude-native" || specs[1].Env["CLAUDE_CONFIG_DIR"] != claudeS || len(specs[1].Command) != 0 {
		t.Fatalf("instance spec = %+v", specs[1])
	}
}

// An instance's env is the extended provider's allowlist and nothing else:
// the same keys as providers.<id>.env (#603), so no token, PATH or other
// provider's variable.
func TestProviderInstance_EnvAllowlistOnly(t *testing.T) {
	cases := []struct {
		extends, key string
		ok           bool
	}{
		{"claude-native", "CLAUDE_CONFIG_DIR", true},
		{"codex-native", "CODEX_HOME", true},
		{"claude-native", "CODEX_HOME", false},
		{"codex-native", "CLAUDE_CONFIG_DIR", false},
		{"claude-native", "ANTHROPIC_API_KEY", false},
		{"claude-native", "PATH", false},
		{"codex-native", "OPENAI_API_KEY", false},
	}
	dir := filepath.Join(t.TempDir(), "dir") // absolute on every OS
	for _, c := range cases {
		list := []CustomProvider{{ID: "inst", Extends: c.extends, Env: map[string]EnvValue{c.key: {Literal: dir}}}}
		err := ValidateCustomProviders(list)
		if c.ok && err != nil {
			t.Errorf("%s %s: %v", c.extends, c.key, err)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "providers.custom[0].env."+c.key)) {
			t.Errorf("%s %s: err = %v, want a config error naming the field", c.extends, c.key, err)
		}
	}
}

// An instance's overlay is validated field by field, like every other
// providers.custom entry: the error names the offending field.
func TestCustomProvider_InstanceOverlayValidation_FieldNamed(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"blank tool name", "providers:\n  custom:\n    - {id: a, extends: claude-native}\n    - {id: b, extends: claude-native, disallowedTools: [\"WebSearch\", \"\"]}\n", "providers.custom[1].disallowedTools[1]: required"},
		{"first tool name blank", "providers:\n  custom:\n    - {id: a, extends: claude-native}\n    - {id: b, extends: claude-native, disallowedTools: [\"\"]}\n", "providers.custom[1].disallowedTools[0]: required"},
		{"disallowedTools on codex", "providers:\n  custom:\n    - {id: a, extends: codex-native, disallowedTools: [WebSearch]}\n", "providers.custom[0].disallowedTools: only applies to an instance of claude-native"},
		{"models with additionalModels", "providers:\n  custom:\n    - {id: a, extends: claude-native, models: [{id: m1}], additionalModels: [{id: m2}]}\n", "providers.custom[0].additionalModels: not allowed together with models"},
		{"bad additionalModels id", "providers:\n  custom:\n    - {id: a, extends: claude-native, additionalModels: [{id: \"-bad\"}]}\n", "providers.custom[0].additionalModels[0].id"},
		{"duplicate overlay model id", "providers:\n  custom:\n    - {id: a, extends: claude-native, models: [{id: m1}, {id: m1}]}\n", "providers.custom[0].models[1].id: duplicate model id"},
		{"instance args without command", "providers:\n  custom:\n    - {id: a, extends: claude-native, args: [x]}\n", "providers.custom[0].args: needs command"},
		{"args on a claude instance", "providers:\n  custom:\n    - {id: a, extends: claude-native, command: x, args: [y]}\n", "providers.custom[0].args: not allowed for an instance of claude-native"},
		{"modes with extends", "providers:\n  custom:\n    - {id: a, extends: claude-native, modes: [{id: m}]}\n", "providers.custom[0].modes: not allowed with extends"},
		{"extends an instance", "providers:\n  custom:\n    - {id: i1, extends: claude-native}\n    - {id: i2, extends: i1}\n", "providers.custom[1].extends"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, tt.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// An instance's command override reaches the registry: an ACP base keeps
// its args, a glm instance swaps the executable, a claude-native instance
// takes only the executable plus its disallowedTools overlay.
func TestCustomProvider_ACPInstanceCommandOverride(t *testing.T) {
	useHome(t, `providers:
  custom:
    - id: my-agent
      command: my-agent
      args: ["--acp"]
    - id: fast
      extends: my-agent
      command: other-agent
      args: ["--acp", "--fast"]
      env: {SOME_FLAG: "1"}
      models: [{id: m1, label: M1, default: true}]
    - id: glm-fast
      extends: glm
      command: my-glm
      additionalModels: [{id: extra-1}]
    - id: claude-wrapped
      extends: claude-native
      command: my-claude
      disallowedTools: [WebSearch]
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	specs := ProviderSpecs(cfg.Providers.Custom)
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	fast, ok := reg.Get("fast")
	if !ok {
		t.Fatal("fast: not in the registry")
	}
	if fast.Runner != providers.RunnerACP || fast.Extends != "my-agent" {
		t.Fatalf("fast = %+v", fast)
	}
	if !reflect.DeepEqual(fast.Command, []string{"other-agent", "--acp", "--fast"}) {
		t.Fatalf("fast.Command = %q", fast.Command)
	}
	if fast.Env["SOME_FLAG"] != "1" {
		t.Fatalf("fast.Env = %v", fast.Env)
	}
	if len(fast.Models) != 1 || fast.Models[0].ID != "m1" || !fast.Models[0].Default {
		t.Fatalf("fast.Models = %+v", fast.Models)
	}
	glmFast, ok := reg.Get("glm-fast")
	if !ok {
		t.Fatal("glm-fast: not in the registry")
	}
	if glmFast.Runner != providers.RunnerACP || glmFast.Extends != "glm" || glmFast.Command[0] != "my-glm" {
		t.Fatalf("glm-fast = %+v", glmFast)
	}
	if glmFast.ModelsMode != providers.ModelsAdd || len(glmFast.Models) != 1 || glmFast.Models[0].ID != "extra-1" {
		t.Fatalf("glm-fast models = %s %+v", glmFast.ModelsMode, glmFast.Models)
	}
	wrapped, ok := reg.Get("claude-wrapped")
	if !ok {
		t.Fatal("claude-wrapped: not in the registry")
	}
	if wrapped.Runner != providers.RunnerClaude || !reflect.DeepEqual(wrapped.Command, []string{"my-claude"}) {
		t.Fatalf("claude-wrapped = %+v", wrapped)
	}
	if !reflect.DeepEqual(wrapped.DisallowedTools, []string{"WebSearch"}) {
		t.Fatalf("claude-wrapped.DisallowedTools = %q", wrapped.DisallowedTools)
	}
}

// acp.supportsMcpServers: false marks an agent that cannot take mcpServers;
// true (or leaving it out) keeps the agent's own capabilities in charge.
// The flag reaches the registry spec, and an instance inherits it.
func TestCustomProvider_ACPSupportsMcpServersFalse(t *testing.T) {
	useHome(t, `providers:
  custom:
    - id: my-agent
      command: my-agent
      acp: {supportsMcpServers: false}
    - id: my-agent-fast
      extends: my-agent
    - id: supports-them
      command: other
      acp: {supportsMcpServers: true}
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	reg := providers.New()
	if err := reg.SetCustom(ProviderSpecs(cfg.Providers.Custom)); err != nil {
		t.Fatal(err)
	}
	if s, _ := reg.Get("my-agent"); !s.NoMCPServers {
		t.Fatalf("my-agent spec = %+v, want NoMCPServers", s)
	}
	if s, _ := reg.Get("my-agent-fast"); !s.NoMCPServers {
		t.Fatalf("my-agent-fast spec = %+v, want NoMCPServers inherited from the base", s)
	}
	if s, _ := reg.Get("supports-them"); s.NoMCPServers {
		t.Fatalf("supports-them spec = %+v, want NoMCPServers false", s)
	}
}

// The acp block applies only to an ACP provider, not to a claude-native
// or codex-native instance.
func TestCustomProvider_ACPFieldOnClaudeInstanceRejected(t *testing.T) {
	useHome(t, "providers:\n  custom:\n    - {id: i, extends: claude-native, acp: {supportsMcpServers: false}}\n")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "providers.custom[0].acp: only applies to an ACP provider") {
		t.Fatalf("Load() error = %v, want providers.custom[0].acp: only applies to an ACP provider", err)
	}
}

func TestAddProviderInstance_WritesPreservingCommentsAndValidates(t *testing.T) {
	useHome(t, "# my settings\nserver:\n  port: 4650 # keep\n")
	home := t.TempDir()
	entry, err := AddProviderInstance("claude-native", "claude-work", "Claude Work", filepath.Join(home, ".claude-work"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Env["CLAUDE_CONFIG_DIR"].Literal != filepath.Join(home, ".claude-work") {
		t.Fatalf("entry = %+v", entry)
	}
	body, _ := os.ReadFile(Path())
	for _, want := range []string{"# my settings", "port: 4650 # keep", "custom:", "id: claude-work", "extends: claude-native", "CLAUDE_CONFIG_DIR"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, body)
		}
	}
	cfg, err := Load()
	if err != nil || len(cfg.Providers.Custom) != 1 {
		t.Fatalf("Load() = %+v, %v", cfg.Providers, err)
	}
	before, _ := os.ReadFile(Path())
	for name, args := range map[string][4]string{
		"duplicate":   {"claude-native", "claude-work", "", home},
		"shadow":      {"claude-native", "glm", "", home},
		"bad extends": {"glm", "x", "", home},
		"relative":    {"claude-native", "rel", "", "relative/dir"},
	} {
		if _, err := AddProviderInstance(args[0], args[1], args[2], args[3]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if after, _ := os.ReadFile(Path()); string(after) != string(before) {
		t.Fatalf("a rejected add changed config.yaml:\n%s", after)
	}
}

// An endpoint points a codex-native instance at a Responses-compatible API:
// nothing is discovered for it, so models must come from the config.
func TestCodexEndpoint_ModelsRequired(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"no models", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://gw.example'}}\n", "providers.custom[0].models: required for a codex-native instance with an endpoint"},
		{"additionalModels instead", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://gw.example'}, additionalModels: [{id: m1}]}\n", "providers.custom[0].additionalModels: not allowed with endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, tt.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
	useHome(t, "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://gw.example'}, models: [{id: m1}]}\n")
	if _, err := Load(); err != nil {
		t.Fatalf("endpoint with models rejected: %v", err)
	}
}

// Codex has no chat-completions wire protocol: the error says so and names a
// way out, whatever chat-flavoured kind was given.
func TestCodexEndpoint_ChatOnlyUnsupportedMessage(t *testing.T) {
	for _, kind := range []string{"openai-chat", "chat-completions", "openai-chat-completions"} {
		useHome(t, "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: "+kind+", baseUrl: 'https://gw.example'}}\n")
		_, err := Load()
		if err == nil {
			t.Fatalf("%s: accepted", kind)
		}
		for _, want := range []string{"providers.custom[0].endpoint.kind", `"` + kind + `"`, "unsupported by the Codex harness", "Responses"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q lacks %q", kind, err, want)
			}
		}
	}
}

// An endpoint is validated field by field and never echoes the URL (it may
// carry a token in its userinfo).
func TestCodexEndpoint_ConfigValidation(t *testing.T) {
	tests := []struct{ name, yaml, want, notWant string }{
		{"claude-native instance", "providers:\n  custom:\n    - {id: cl, extends: claude-native, endpoint: {kind: openai-responses, baseUrl: 'https://gw.example'}}\n", "providers.custom[0].endpoint: only applies to an instance of codex-native", ""},
		{"missing kind", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {baseUrl: 'https://gw.example'}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.kind: required", ""},
		{"missing baseUrl", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.baseUrl: required", ""},
		{"unknown kind", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: grpc, baseUrl: 'https://gw.example'}, models: [{id: m1}]}\n", `providers.custom[0].endpoint.kind: unknown kind "grpc"`, ""},
		{"non-http scheme", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'ftp://gw.example'}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.baseUrl", ""},
		{"no host", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://'}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.baseUrl", ""},
		{"query string", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://gw.example?k=v'}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.baseUrl", ""},
		{"userinfo", "providers:\n  custom:\n    - {id: cx, extends: codex-native, endpoint: {kind: openai-responses, baseUrl: 'https://user:pw@h/'}, models: [{id: m1}]}\n", "providers.custom[0].endpoint.baseUrl", "pw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, tt.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
			if tt.notWant != "" && strings.Contains(err.Error(), tt.notWant) {
				t.Fatalf("Load() error = %v, echoes %q", err, tt.notWant)
			}
		})
	}
}

// A config.yaml endpoint survives the unmarshal → validate → ProviderSpecs
// path and lands on the registry spec.
func TestCodexEndpoint_YAMLRoundTrip(t *testing.T) {
	src := `providers:
  custom:
    - id: codex-gw
      extends: codex-native
      endpoint:
        kind: openai-responses
        baseUrl: https://gw.example
      models: [{id: gpt-x, label: GPT X, default: true}]
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	list := cfg.Providers.Custom
	if len(list) != 1 {
		t.Fatalf("providers.custom = %+v", list)
	}
	if err := ValidateCustomProviders(list); err != nil {
		t.Fatal(err)
	}
	specs := ProviderSpecs(list)
	if len(specs) != 1 {
		t.Fatalf("specs = %+v", specs)
	}
	s := specs[0]
	if !s.HasEndpoint() || s.Endpoint.Kind != providers.EndpointOpenAIResponses || s.Endpoint.BaseURL != "https://gw.example" {
		t.Fatalf("spec endpoint = %+v", s.Endpoint)
	}
	if len(s.Models) != 1 || s.Models[0].ID != "gpt-x" || !s.Models[0].Default {
		t.Fatalf("spec models = %+v", s.Models)
	}
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	inst, ok := reg.Get("codex-gw")
	if !ok || !inst.HasEndpoint() || inst.Endpoint.Kind != providers.EndpointOpenAIResponses {
		t.Fatalf("registry instance = %+v", inst.Endpoint)
	}
}
