package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

// The per-harness allowlist (ADR-0046 section 3): a name outside the table is
// rejected with the field named; ACP stays free-form.
func TestProviderEnv_AllowlistPerHarness(t *testing.T) {
	inst := func(extends string, env map[string]EnvValue) []CustomProvider {
		return []CustomProvider{{ID: "inst", Extends: extends, Env: env}}
	}
	if err := ValidateCustomProviders(inst("claude-native", map[string]EnvValue{"PATH": {Literal: "/usr/bin"}})); err == nil ||
		!strings.Contains(err.Error(), "providers.custom[0].env.PATH") || !strings.Contains(err.Error(), "not an allowed env name") {
		t.Fatalf("claude PATH err = %v", err)
	}
	if err := ValidateCustomProviders(inst("claude-native", map[string]EnvValue{"ANTHROPIC_MODEL": {Literal: "m1"}})); err != nil {
		t.Fatalf("claude ANTHROPIC_MODEL err = %v", err)
	}
	if err := ValidateCustomProviders(inst("codex-native", map[string]EnvValue{"ANTHROPIC_BASE_URL": {Literal: "https://api.z.ai/api/anthropic"}})); err == nil ||
		!strings.Contains(err.Error(), "providers.custom[0].env.ANTHROPIC_BASE_URL") {
		t.Fatalf("codex ANTHROPIC_BASE_URL err = %v", err)
	}
	if err := ValidateCustomProviders(inst("codex-native", map[string]EnvValue{"OPENAI_BASE_URL": {Literal: "https://api.z.ai/api/anthropic"}})); err != nil {
		t.Fatalf("codex OPENAI_BASE_URL err = %v", err)
	}
	acp := []CustomProvider{{ID: "a", Command: "x", Env: map[string]EnvValue{"WHATEVER_FLAG": {Literal: "1"}}}}
	if err := ValidateCustomProviders(acp); err != nil {
		t.Fatalf("acp free-form err = %v", err)
	}
}

// A secret-kind name never takes an inline value, under any harness: the key
// would sit in plaintext config.yaml. Errors never echo the value.
func TestProviderEnv_SecretKindRejectsLiteral(t *testing.T) {
	sentinel := "sentinel-NOT-A-REAL-KEY-12345"
	inst := []CustomProvider{{ID: "i", Extends: "claude-native", Env: map[string]EnvValue{"ANTHROPIC_AUTH_TOKEN": {Literal: sentinel}}}}
	err := ValidateCustomProviders(inst)
	if err == nil || !strings.Contains(err.Error(), "providers.custom[0].env.ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("err = %v, want the field named", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("err echoes the value: %v", err)
	}
	ok := func(env map[string]EnvValue) error {
		list := []CustomProvider{{ID: "i", Extends: "claude-native", Env: env}}
		return ValidateCustomProviders(list)
	}
	if err := ok(map[string]EnvValue{"ANTHROPIC_API_KEY": {Literal: ""}}); err != nil {
		t.Fatalf("empty ANTHROPIC_API_KEY err = %v", err)
	}
	if err := ok(map[string]EnvValue{"ANTHROPIC_AUTH_TOKEN": {Secret: "zai-key"}}); err != nil {
		t.Fatalf("secret ref err = %v", err)
	}
	// ACP has no table, but a credential-shaped name is still secret-kind.
	acp := func(v EnvValue) error {
		list := []CustomProvider{{ID: "a", Command: "x", Env: map[string]EnvValue{"MY_VENDOR_TOKEN": v}}}
		return ValidateCustomProviders(list)
	}
	if err := acp(EnvValue{Literal: sentinel}); err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("acp literal err = %v", err)
	}
	if err := acp(EnvValue{Secret: "vendor-key"}); err != nil {
		t.Fatalf("acp secret ref err = %v", err)
	}
}

// Passthrough copies a variable from the daemon env: only worth it for a
// credential, so only allowed for a secret-kind name.
func TestProviderEnv_PassthroughOnlyForSecretKinds(t *testing.T) {
	ok := func(key string) error {
		list := []CustomProvider{{ID: "i", Extends: "claude-native", Env: map[string]EnvValue{key: {Passthrough: true}}}}
		return ValidateCustomProviders(list)
	}
	if err := ok("ANTHROPIC_AUTH_TOKEN"); err != nil {
		t.Fatalf("secret-kind passthrough err = %v", err)
	}
	for _, key := range []string{"ANTHROPIC_MODEL", "CLAUDE_CONFIG_DIR"} {
		if err := ok(key); err == nil || !strings.Contains(err.Error(), "passthrough") {
			t.Fatalf("%s passthrough err = %v", key, err)
		}
	}
}

func TestProviderEnv_URLKind(t *testing.T) {
	check := func(value string) error {
		_, err := ValidateInstanceEnv(providers.RunnerClaude, "ANTHROPIC_BASE_URL", EnvValue{Literal: value})
		return err
	}
	if err := check("https://api.z.ai/api/anthropic"); err != nil {
		t.Fatalf("https err = %v", err)
	}
	for _, bad := range []string{"https://user:pass@host/x", "ftp://x", "not a url", "http://", "https://host/x\x00"} {
		err := check(bad)
		if err == nil {
			t.Errorf("%q: accepted", bad)
			continue
		}
		if strings.Contains(err.Error(), "pass") {
			t.Errorf("%q: err echoes the value: %v", bad, err)
		}
	}
}

func TestProviderEnv_IntKind(t *testing.T) {
	if _, err := ValidateInstanceEnv(providers.RunnerClaude, "API_TIMEOUT_MS", EnvValue{Literal: "3000000"}); err != nil {
		t.Fatalf("3000000 err = %v", err)
	}
	for _, bad := range []string{"-1", "abc"} {
		if _, err := ValidateInstanceEnv(providers.RunnerClaude, "API_TIMEOUT_MS", EnvValue{Literal: bad}); err == nil {
			t.Errorf("%q: accepted", bad)
		}
	}
}

func TestProviderEnv_PathKindExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := ValidateInstanceEnv(providers.RunnerClaude, "CLAUDE_CONFIG_DIR", EnvValue{Literal: "~/x"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "x"); got.Literal != want {
		t.Fatalf("path = %q, want %q", got.Literal, want)
	}
	if _, err := ValidateInstanceEnv(providers.RunnerClaude, "CLAUDE_CONFIG_DIR", EnvValue{Literal: "relative/dir"}); err == nil {
		t.Fatal("a relative path was accepted")
	}
}

func TestEnvValue_YAMLRoundTrip(t *testing.T) {
	var env map[string]EnvValue
	src := "lit: hello\nsec: {secret: zai-key}\npas: {passthrough: true}\nempty: \"\"\n"
	if err := yaml.Unmarshal([]byte(src), &env); err != nil {
		t.Fatal(err)
	}
	if env["lit"] != (EnvValue{Literal: "hello"}) || env["sec"] != (EnvValue{Secret: "zai-key"}) ||
		env["pas"] != (EnvValue{Passthrough: true}) || env["empty"] != (EnvValue{}) {
		t.Fatalf("decoded = %#v", env)
	}
	out, err := yaml.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]EnvValue
	if err := yaml.Unmarshal(out, &again); err != nil {
		t.Fatalf("re-decode %q: %v", out, err)
	}
	if len(again) != 4 || again["sec"] != (EnvValue{Secret: "zai-key"}) || again["pas"] != (EnvValue{Passthrough: true}) {
		t.Fatalf("round trip = %#v from %q", again, out)
	}
	if !strings.Contains(string(out), "secret: zai-key") || !strings.Contains(string(out), "passthrough: true") {
		t.Fatalf("marshal forms:\n%s", out)
	}
	for name, snippet := range map[string]string{
		"two keys":          "k: {literal: a, secret: b}\n",
		"passthrough false": "k: {passthrough: false}\n",
		"unknown key":       "k: {token: abc}\n",
		"empty secret":      "k: {secret: \"\"}\n",
	} {
		var v map[string]EnvValue
		if err := yaml.Unmarshal([]byte(snippet), &v); err == nil {
			t.Errorf("%s: accepted %#v", name, v)
		}
	}
}

func TestProviderSpecs_SplitsLiteralSecretPassthrough(t *testing.T) {
	list := []CustomProvider{{ID: "zai", Extends: "claude-native", Env: map[string]EnvValue{
		"ANTHROPIC_BASE_URL":   {Literal: "https://api.z.ai/api/anthropic"},
		"ANTHROPIC_AUTH_TOKEN": {Secret: "zai-key"},
		"ANTHROPIC_API_KEY":    {Passthrough: true},
	}}}
	if err := ValidateCustomProviders(list); err != nil {
		t.Fatal(err)
	}
	spec := ProviderSpecs(list)[0]
	if len(spec.Env) != 1 || spec.Env["ANTHROPIC_BASE_URL"] != "https://api.z.ai/api/anthropic" {
		t.Fatalf("Env = %v", spec.Env)
	}
	if len(spec.SecretEnv) != 1 || spec.SecretEnv["ANTHROPIC_AUTH_TOKEN"] != "zai-key" {
		t.Fatalf("SecretEnv = %v", spec.SecretEnv)
	}
	if len(spec.PassthroughEnv) != 1 || spec.PassthroughEnv[0] != "ANTHROPIC_API_KEY" {
		t.Fatalf("PassthroughEnv = %v", spec.PassthroughEnv)
	}
	for _, v := range spec.Env {
		if strings.Contains(v, "zai-key") {
			t.Fatalf("Env carries secret material: %v", spec.Env)
		}
	}
}

// No writer path can put a key in config.yaml, and an inline key in a
// hand-edited file is refused at load, without echoing it.
func TestProviderSecret_NeverInConfigYaml(t *testing.T) {
	useHome(t, "# my settings\nserver:\n  port: 4650 # keep\n")
	if _, err := AddProviderInstance("claude-native", "claude-work", "Claude Work", filepath.Join(t.TempDir(), ".claude-work")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sentinel") {
		t.Fatalf("config.yaml carries secret material:\n%s", body)
	}
	useHome(t, "providers:\n  custom:\n    - id: zai\n      extends: claude-native\n      env:\n        ANTHROPIC_AUTH_TOKEN: sentinel-NOT-A-REAL-KEY-12345\n")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "providers.custom[0].env.ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("Load() err = %v, want the inline key refused with the field named", err)
	}
	if strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("err echoes the key: %v", err)
	}
}
