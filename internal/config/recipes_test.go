package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

func recipeFor(t *testing.T, id string) providers.Recipe {
	t.Helper()
	rec, ok := providers.RecipeByID(id)
	if !ok {
		t.Fatalf("recipe %q not in the catalog", id)
	}
	return rec
}

func TestRecipeEntry_ZAIOnlyNeedsKey(t *testing.T) {
	entry, placeholders, err := BuildRecipeEntry(recipeFor(t, "zai-glm"), RecipeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(placeholders) != 0 {
		t.Fatalf("placeholders = %v, want none", placeholders)
	}
	if got := entry.Env["ANTHROPIC_AUTH_TOKEN"]; got.Secret != RecipeSecretName || got.Literal != "" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %+v, want a %q secret reference", got, RecipeSecretName)
	}
	if got := entry.Env["ANTHROPIC_BASE_URL"]; got.Literal != "https://api.z.ai/api/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %+v", got)
	}
	if entry.Vendor != "zai" || entry.Recipe != "zai-glm" || entry.Thinking != "supported" {
		t.Fatalf("entry = %+v", entry)
	}
	if !strings.Contains(strings.Join(entry.DisallowedTools, ","), "WebSearch") {
		t.Fatalf("disallowedTools = %v", entry.DisallowedTools)
	}
	if err := ValidateCustomProviders([]CustomProvider{entry}); err != nil {
		t.Fatalf("ValidateCustomProviders: %v", err)
	}
}

func TestRecipeEntry_AllClaudeRecipesValidate(t *testing.T) {
	for _, rec := range providers.Recipes() {
		if rec.Harness != "claude-code" {
			continue
		}
		entry, _, err := BuildRecipeEntry(rec, RecipeInput{BaseURL: "http://127.0.0.1:9", Models: []string{"m1"}})
		if err != nil {
			t.Fatalf("%s: %v", rec.ID, err)
		}
		if err := ValidateCustomProviders([]CustomProvider{entry}); err != nil {
			t.Fatalf("%s: ValidateCustomProviders: %v", rec.ID, err)
		}
	}
}

func TestRecipeEntry_PlaceholderSecretForLocal(t *testing.T) {
	entry, placeholders, err := BuildRecipeEntry(recipeFor(t, "ollama"), RecipeInput{Models: []string{"qwen3.5"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(placeholders) != 1 || placeholders[RecipeSecretName] != "ollama" {
		t.Fatalf("placeholders = %v", placeholders)
	}
	if got := entry.Env["ANTHROPIC_AUTH_TOKEN"]; got.Secret != RecipeSecretName || got.Literal != "" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %+v, want a secret reference", got)
	}
	raw, _ := yaml.Marshal(entry.Env)
	if strings.Contains(string(raw), "ollama") {
		t.Fatalf("the placeholder leaked into the entry's env: %s", raw)
	}
}

func TestRecipeEntry_UserBaseURLRequired(t *testing.T) {
	for _, id := range []string{"litellm", "anthropic-compatible"} {
		_, _, err := BuildRecipeEntry(recipeFor(t, id), RecipeInput{Models: []string{"m1"}})
		if err == nil || !strings.Contains(err.Error(), "baseUrl") {
			t.Fatalf("%s without a baseUrl: err = %v", id, err)
		}
	}
	if entry, _, err := BuildRecipeEntry(recipeFor(t, "litellm"), RecipeInput{BaseURL: "http://127.0.0.1:4000", Models: []string{"m1"}}); err != nil {
		t.Fatalf("litellm with a baseUrl: %v", err)
	} else if got := entry.Env["ANTHROPIC_BASE_URL"]; got.Literal != "http://127.0.0.1:4000" {
		t.Fatalf("ANTHROPIC_BASE_URL = %+v", got)
	}
}

func TestRecipeEntry_ModelsRequired(t *testing.T) {
	_, _, err := BuildRecipeEntry(recipeFor(t, "ollama"), RecipeInput{})
	if err == nil || !strings.Contains(err.Error(), "models") {
		t.Fatalf("ollama without models: err = %v", err)
	}
}

func TestRecipeEntry_CodexEndpoint(t *testing.T) {
	rec := recipeFor(t, "openai-responses-compatible")
	if _, _, err := BuildRecipeEntry(rec, RecipeInput{Models: []string{"m1"}}); err == nil || !strings.Contains(err.Error(), "baseUrl") {
		t.Fatalf("codex recipe without a baseUrl: err = %v", err)
	}
	entry, _, err := BuildRecipeEntry(rec, RecipeInput{BaseURL: "http://127.0.0.1:9", Models: []string{"m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Extends != providers.IDCodexNative || entry.Endpoint == nil || entry.Endpoint.Kind != "openai-responses" || entry.Endpoint.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("entry = %+v (endpoint %+v)", entry, entry.Endpoint)
	}
	if _, ok := entry.Env["OPENAI_BASE_URL"]; ok {
		t.Fatal("OPENAI_BASE_URL must ride on endpoint.baseUrl, not the env")
	}
	if err := ValidateCustomProviders([]CustomProvider{entry}); err != nil {
		t.Fatalf("ValidateCustomProviders: %v", err)
	}
}

// The chat-completions recipe builds an opencode instance: the URL rides on
// endpoint.baseUrl (no env), the key is a secret reference and never a value,
// and models are required.
func TestRecipeEntry_ChatCompletionsViaOpenCode(t *testing.T) {
	rec := recipeFor(t, "chat-completions-via-opencode")
	if _, _, err := BuildRecipeEntry(rec, RecipeInput{Models: []string{"m1"}}); err == nil || !strings.Contains(err.Error(), "baseUrl: required") {
		t.Fatalf("no base URL: err = %v", err)
	}
	if _, _, err := BuildRecipeEntry(rec, RecipeInput{BaseURL: "https://api.vendor.test/v1"}); err == nil || !strings.Contains(err.Error(), "models: required") {
		t.Fatalf("no models: err = %v", err)
	}
	entry, placeholders, err := BuildRecipeEntry(rec, RecipeInput{ID: "vendor-x", BaseURL: "https://api.vendor.test/v1", Models: []string{"chat-1", "chat-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Extends != providers.IDOpenCode || entry.Endpoint == nil || entry.Endpoint.Kind != "chat-completions" || entry.Endpoint.BaseURL != "https://api.vendor.test/v1" {
		t.Fatalf("entry = %+v (endpoint %+v)", entry, entry.Endpoint)
	}
	if v := entry.Env[providers.OpenCodeKeyEnv]; v.Secret != RecipeSecretName || v.Literal != "" {
		t.Fatalf("key env = %+v; want a reference to the stored secret and no literal", v)
	}
	if len(placeholders) != 0 {
		t.Fatalf("placeholders = %v; a keyed recipe stores none", placeholders)
	}
	if len(entry.Models) != 2 || entry.Models[0].ID != "chat-1" || !entry.Models[0].Default {
		t.Fatalf("models = %+v", entry.Models)
	}
	if err := ValidateCustomProviders([]CustomProvider{entry}); err != nil {
		t.Fatalf("ValidateCustomProviders: %v", err)
	}
}

// Written to config.yaml, the instance carries the reference and not a key.
func TestAddRecipeInstance_ChatCompletionsViaOpenCode_WritesNoKey(t *testing.T) {
	path := useHome(t, "")
	res, err := AddRecipeInstance(recipeFor(t, "chat-completions-via-opencode"), RecipeInput{ID: "vendor-x", BaseURL: "https://api.vendor.test/v1", Models: []string{"chat-1"}})
	if err != nil || !res.Created {
		t.Fatalf("AddRecipeInstance = %+v, %v", res, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"extends: opencode", "kind: chat-completions", "baseUrl: https://api.vendor.test/v1", "SMIND_OPENCODE_API_KEY:", "secret: api-key"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, b)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("the written file does not load: %v", err)
	}
	if len(cfg.Providers.Custom) != 1 || cfg.Providers.Custom[0].Extends != providers.IDOpenCode {
		t.Fatalf("custom = %+v", cfg.Providers.Custom)
	}
}

func TestAddRecipeInstance_WritesConfigNoKey(t *testing.T) {
	path := useHome(t, "")
	res, err := AddRecipeInstance(recipeFor(t, "zai-glm"), RecipeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Entry.ID != "zai-glm" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.SecretNames) != 1 || res.SecretNames[0] != RecipeSecretName {
		t.Fatalf("secret names = %v", res.SecretNames)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"recipe: zai-glm", "secret: api-key", "vendor: zai"} {
		if !strings.Contains(text, want) {
			t.Fatalf("config.yaml missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sentinel") {
		t.Fatalf("config.yaml carries a key value:\n%s", text)
	}
	// The written file loads back as the same entry.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers.Custom) != 1 || cfg.Providers.Custom[0].Recipe != "zai-glm" {
		t.Fatalf("custom = %+v", cfg.Providers.Custom)
	}
}

func TestAddRecipeInstance_Idempotent(t *testing.T) {
	path := useHome(t, "")
	rec := recipeFor(t, "zai-glm")
	if _, err := AddRecipeInstance(rec, RecipeInput{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AddRecipeInstance(rec, RecipeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created {
		t.Fatalf("second call Created = true (%+v)", res)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("the second call rewrote the file:\n%s\n%s", before, after)
	}
}

func TestAddRecipeInstance_IDCollision(t *testing.T) {
	useHome(t, "providers:\n  custom:\n    - id: zai-glm\n      extends: claude-native\n")
	_, err := AddRecipeInstance(recipeFor(t, "zai-glm"), RecipeInput{})
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("collision: err = %v", err)
	}
}

func TestCustomProvider_InstanceRecipeFields(t *testing.T) {
	useHome(t, `providers:
  custom:
    - id: glmv
      label: GLM Vendored
      extends: claude-native
      recipe: zai-glm
      vendor: zai
      thinking: supported
      inheritClaudeConfig: true
      env:
        ANTHROPIC_AUTH_TOKEN: {secret: api-key}
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := cfg.Providers.Custom[0]
	if entry.Recipe != "zai-glm" || entry.Vendor != "zai" || entry.Thinking != "supported" || !entry.InheritClaudeConfig {
		t.Fatalf("round trip: %+v", entry)
	}
	specs := ProviderSpecs(cfg.Providers.Custom)
	if specs[0].Vendor != "zai" || specs[0].Thinking != "supported" || specs[0].Recipe != "zai-glm" || !specs[0].InheritClaudeConfig {
		t.Fatalf("spec = %+v", specs[0])
	}
	// The instance-only fields are refused on a non-instance entry.
	useHome(t, "providers:\n  custom:\n    - id: a\n      command: x\n      recipe: zai-glm\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "providers.custom[0].recipe: only applies to an instance") {
		t.Fatalf("acp entry with a recipe: err = %v", err)
	}
	// inheritClaudeConfig needs a claude-native instance; a bad vendor is named.
	useHome(t, "providers:\n  custom:\n    - id: c\n      extends: codex-native\n      inheritClaudeConfig: true\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "inheritClaudeConfig") {
		t.Fatalf("inheritClaudeConfig on codex: err = %v", err)
	}
	useHome(t, "providers:\n  custom:\n    - id: c\n      extends: claude-native\n      vendor: nope\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "providers.custom[0].vendor") {
		t.Fatalf("bad vendor: err = %v", err)
	}
}
