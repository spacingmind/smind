package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
      env: {MY_TOKEN: abc}
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
		list := []CustomProvider{{ID: "inst", Extends: c.extends, Env: map[string]string{c.key: dir}}}
		err := ValidateCustomProviders(list)
		if c.ok && err != nil {
			t.Errorf("%s %s: %v", c.extends, c.key, err)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "providers.custom[0].env."+c.key)) {
			t.Errorf("%s %s: err = %v, want a config error naming the field", c.extends, c.key, err)
		}
	}
}

func TestAddProviderInstance_WritesPreservingCommentsAndValidates(t *testing.T) {
	useHome(t, "# my settings\nserver:\n  port: 4650 # keep\n")
	home := t.TempDir()
	entry, err := AddProviderInstance("claude-native", "claude-work", "Claude Work", filepath.Join(home, ".claude-work"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Env["CLAUDE_CONFIG_DIR"] != filepath.Join(home, ".claude-work") {
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
