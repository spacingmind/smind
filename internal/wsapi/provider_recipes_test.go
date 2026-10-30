package wsapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// recipeList fetches provider.recipes as a map by id.
func recipeList(t *testing.T, h *secretsHarness) map[string]recipeView {
	t.Helper()
	res, err := h.call(t, "provider.recipes", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := res.(recipesResult).Recipes
	out := make(map[string]recipeView, len(list))
	for _, r := range list {
		out[r.ID] = r
	}
	return out
}

func TestRecipesRPC_ListsCatalog(t *testing.T) {
	h := newSecretsHarness(t)
	byID := recipeList(t, h)
	if len(byID) != 12 {
		t.Fatalf("provider.recipes listed %d recipes, want 12", len(byID))
	}
	if !byID["zai-glm"].NeedsKey || byID["zai-glm"].NeedsBaseURL {
		t.Fatalf("zai-glm = %+v", byID["zai-glm"])
	}
	if byID["ollama"].NeedsKey {
		t.Fatalf("ollama = %+v, want needsKey false", byID["ollama"])
	}
	if !byID["litellm"].NeedsBaseURL || !byID["openai-responses-compatible"].NeedsBaseURL {
		t.Fatalf("needsBaseUrl: litellm/openai-responses-compatible = %+v / %+v", byID["litellm"], byID["openai-responses-compatible"])
	}
	if !byID["kimi-moonshot"].Unverified || len(byID["kimi-moonshot"].UnverifiedAspects) == 0 {
		t.Fatalf("kimi-moonshot = %+v", byID["kimi-moonshot"])
	}
	// No secret or placeholder value is ever sent.
	for id, r := range byID {
		for _, e := range r.Env {
			if (e.Kind == "secret" || e.Kind == "secretDefault") && e.Value != "" {
				t.Fatalf("%s env %s: value %q sent for a %s env", id, e.Name, e.Value, e.Kind)
			}
		}
	}
}

func TestAddFromRecipe_WritesInstanceAndSecret(t *testing.T) {
	h := newSecretsHarness(t)
	res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "zai-glm",
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(addFromRecipeResult)
	if r.ID != "zai-glm" || !r.Created || r.NeedsKey || len(r.Secrets) != 1 {
		t.Fatalf("result = %+v", r)
	}
	s := r.Secrets[0]
	if s.Name != "api-key" || !s.Set || s.Fingerprint != store.SecretFingerprint(keySentinel) || len(s.Fingerprint) != 4 {
		t.Fatalf("secrets = %+v", r.Secrets)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("the result carries the key: %s", raw)
	}
	spec, ok := h.runner.Providers().Get("zai-glm")
	if !ok || spec.Vendor != "zai" || spec.Recipe != "zai-glm" {
		t.Fatalf("spec = %+v, ok = %v", spec, ok)
	}
	if !h.stored(t, "zai-glm", "api-key") {
		t.Fatal("the key was not stored")
	}
	text, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "recipe: zai-glm") || strings.Contains(string(text), keySentinel) {
		t.Fatalf("config.yaml = %s", text)
	}
}

func TestAddFromRecipe_RefusesAgentCaller(t *testing.T) {
	h := newSecretsHarness(t)
	_, err := h.call(t, "provider.addFromRecipe", &requestContext{runScoped: true}, map[string]any{
		"recipe":  "zai-glm",
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("agent caller: err = %v, want forbidden", err)
	}
	if _, ok := h.runner.Providers().Get("zai-glm"); ok {
		t.Fatal("an agent caller created an instance")
	}
	if h.stored(t, "zai-glm", "api-key") {
		t.Fatal("an agent caller stored a secret")
	}
	if _, err := os.Stat(h.path); !os.IsNotExist(err) {
		t.Fatalf("config.yaml was written: %v", err)
	}
	for _, m := range RunScopedMethods {
		if m == "provider.addFromRecipe" {
			t.Fatal("provider.addFromRecipe is on the run-scoped allowlist")
		}
	}
}

func TestAddFromRecipe_SecretWriteOnly(t *testing.T) {
	h := newSecretsHarness(t)
	// A failing call never echoes the pasted key.
	_, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "does-not-exist",
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") || strings.Contains(err.Error(), keySentinel) {
		t.Fatalf("unknown recipe: err = %v", err)
	}
	// A pasted key with an invalid value: refused without echoing it.
	_, err = h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "zai-glm",
		"secrets": map[string]string{"api-key": keySentinel + "\x00"},
	})
	if err == nil || !strings.Contains(err.Error(), "a key is required") || strings.Contains(err.Error(), keySentinel) {
		t.Fatalf("invalid key: err = %v", err)
	}
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("provider.list carries the key: %s", raw)
	}
	// A successful add never echoes the pasted key either.
	res2, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "zai-glm",
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(res2)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("the success result carries the key: %s", raw)
	}
	res, err = h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(res)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("provider.list carries the key: %s", raw)
	}
	text, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), keySentinel) {
		t.Fatalf("config.yaml carries the key: %s", text)
	}
}

func TestAddFromRecipe_MissingKeyWritesNothing(t *testing.T) {
	h := newSecretsHarness(t)
	_, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{"recipe": "zai-glm"})
	if err == nil || !strings.Contains(err.Error(), "a key is required") {
		t.Fatalf("no key: err = %v", err)
	}
	if _, ok := h.runner.Providers().Get("zai-glm"); ok {
		t.Fatal("the instance was created without a key")
	}
	if h.stored(t, "zai-glm", "api-key") {
		t.Fatal("a secret was stored without a key")
	}
	if _, err := os.Stat(h.path); !os.IsNotExist(err) {
		t.Fatalf("config.yaml was written: %v", err)
	}
}

func TestAddFromRecipe_IDCollision(t *testing.T) {
	h := newSecretsHarness(t)
	h.writeCustom(t, "    - id: zai-glm\n      label: Mine\n      extends: claude-native\n")
	_, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "zai-glm",
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err == nil || !strings.Contains(err.Error(), "already used") || strings.Contains(err.Error(), keySentinel) {
		t.Fatalf("collision: err = %v", err)
	}
	if h.stored(t, "zai-glm", "api-key") {
		t.Fatal("a refused call stored a secret")
	}
	text, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "label: Mine") || strings.Contains(string(text), "recipe:") {
		t.Fatalf("config.yaml = %s", text)
	}
}

func TestAddFromRecipe_Idempotent(t *testing.T) {
	h := newSecretsHarness(t)
	params := func(key string) map[string]any {
		return map[string]any{"recipe": "zai-glm", "secrets": map[string]string{"api-key": key}}
	}
	if res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, params(keySentinel)); err != nil {
		t.Fatal(err)
	} else if !res.(addFromRecipeResult).Created {
		t.Fatal("first call did not create")
	}
	res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, params("sentinel-SECOND-KEY-678"))
	if err != nil {
		t.Fatal(err)
	}
	if res.(addFromRecipeResult).Created {
		t.Fatal("second call created again")
	}
	n := 0
	for _, s := range h.runner.Providers().List() {
		if s.ID == "zai-glm" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d instances named zai-glm", n)
	}
	// A new key replaces the stored one; the file still has one entry.
	v, ok, err := h.db.GetProviderSecret("zai-glm", "api-key")
	if err != nil || !ok || v != "sentinel-SECOND-KEY-678" {
		t.Fatalf("stored = %q ok=%v err=%v", v, ok, err)
	}
	text, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(text), "id: zai-glm") != 1 {
		t.Fatalf("config.yaml = %s", text)
	}
}

func TestAddFromRecipe_LocalPlaceholder(t *testing.T) {
	h := newSecretsHarness(t)
	res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe": "ollama",
		"models": []string{"qwen3.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(addFromRecipeResult); !r.Created || r.NeedsKey || len(r.Secrets) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if v, ok, err := h.db.GetProviderSecret("ollama", "api-key"); err != nil || !ok || v != "ollama" {
		t.Fatalf("placeholder stored = %q ok=%v err=%v", v, ok, err)
	}
	// The listing shows the fingerprint only, never the placeholder value.
	e := h.listedSecretEntry(t, "ollama")
	if e.Name != "api-key" || e.Env != "ANTHROPIC_AUTH_TOKEN" || !e.Set || e.Fingerprint != store.SecretFingerprint("ollama") || e.Fingerprint == "ollama" {
		t.Fatalf("listed secret = %+v", e)
	}
}

func TestAddFromRecipe_UserBaseURL(t *testing.T) {
	h := newSecretsHarness(t)
	_, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "litellm",
		"models":  []string{"m1"},
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err == nil || !strings.Contains(err.Error(), "baseUrl") || strings.Contains(err.Error(), keySentinel) {
		t.Fatalf("no baseUrl: err = %v", err)
	}
	if _, ok := h.runner.Providers().Get("litellm"); ok {
		t.Fatal("the instance was created without a baseUrl")
	}
	if _, err := os.Stat(h.path); !os.IsNotExist(err) {
		t.Fatalf("config.yaml was written: %v", err)
	}
	res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  "litellm",
		"baseUrl": "http://127.0.0.1:4000",
		"models":  []string{"m1"},
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(addFromRecipeResult); r.ID != "litellm" || !r.Created {
		t.Fatalf("result = %+v", r)
	}
	if e := h.listedSecretEntry(t, "litellm"); !e.Set {
		t.Fatalf("litellm secret = %+v", e)
	}
}
