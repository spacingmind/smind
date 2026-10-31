package wsapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

const chatRecipe = "chat-completions-via-opencode"

// The catalog row asks for exactly what the form needs: a key, a base URL and
// the vendor's model ids; and it never carries a value for the key.
func TestRecipesRPC_ChatCompletionsViaOpenCodeAsksForKeyURLAndModels(t *testing.T) {
	h := newSecretsHarness(t)
	row, ok := recipeList(t, h)[chatRecipe]
	if !ok {
		t.Fatalf("%s is not in the catalog", chatRecipe)
	}
	if !row.NeedsKey || !row.NeedsBaseURL || !row.ModelsRequired {
		t.Fatalf("row = needsKey %v needsBaseUrl %v modelsRequired %v; want all true", row.NeedsKey, row.NeedsBaseURL, row.ModelsRequired)
	}
	if row.Harness != "acp" || row.EndpointKind != "chat-completions" {
		t.Errorf("harness %q endpoint %q", row.Harness, row.EndpointKind)
	}
	var key *recipeEnvView
	for i := range row.Env {
		if row.Env[i].Name == providers.OpenCodeKeyEnv {
			key = &row.Env[i]
		}
	}
	if key == nil || key.Kind != "secret" || key.Value != "" {
		t.Fatalf("key env = %+v; want a secret entry with no value", key)
	}
	if !row.Unverified || len(row.UnverifiedAspects) == 0 {
		t.Errorf("the row must say no real vendor was called: %+v", row)
	}
}

// Adding the recipe makes a runnable opencode instance: the endpoint and models
// are in config.yaml, the key only in the secret store, and the key is in no
// result, no provider.list and no file.
func TestAddFromRecipe_ChatCompletionsViaOpenCode_CreatesARunnableInstance(t *testing.T) {
	h := newSecretsHarness(t)
	res, err := h.call(t, "provider.addFromRecipe", &requestContext{}, map[string]any{
		"recipe":  chatRecipe,
		"id":      "vendor-x",
		"baseUrl": "https://api.vendor.test/v1",
		"models":  []string{"chat-1", "qwen/qwen3-coder"},
		"secrets": map[string]string{"api-key": keySentinel},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(addFromRecipeResult)
	if r.ID != "vendor-x" || !r.Created || len(r.Secrets) != 1 || r.Secrets[0].Fingerprint != store.SecretFingerprint(keySentinel) {
		t.Fatalf("result = %+v", r)
	}
	if !h.stored(t, "vendor-x", "api-key") {
		t.Fatal("the key was not stored")
	}
	spec, ok := h.runner.Providers().Get("vendor-x")
	if !ok || !spec.IsOpenCodeChat() {
		t.Fatalf("spec = %+v, ok = %v; want an opencode chat-completions instance", spec, ok)
	}
	if spec.Endpoint.BaseURL != "https://api.vendor.test/v1" || len(spec.Models) != 2 || !spec.Models[0].Default {
		t.Errorf("endpoint %+v models %+v", spec.Endpoint, spec.Models)
	}
	if spec.SecretEnv[providers.OpenCodeKeyEnv] != "api-key" {
		t.Errorf("SecretEnv = %v; want the key env bound to the stored secret", spec.SecretEnv)
	}
	if spec.Env["OPENCODE_PERMISSION"] == "" {
		t.Errorf("the base's ask policy is missing from the instance env: %v", spec.Env)
	}
	for _, blob := range []any{res} {
		raw, _ := json.Marshal(blob)
		if strings.Contains(string(raw), keySentinel) {
			t.Fatalf("the result carries the key: %s", raw)
		}
	}
	list, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), keySentinel) {
		t.Fatalf("provider.list carries the key: %s", raw)
	}
	by := h.listWire(t)
	if by["vendor-x"] == nil || by["vendor-x"]["endpointKind"] != "chat-completions" {
		t.Errorf("provider.list entry = %v; want endpointKind chat-completions", by["vendor-x"])
	}
	text, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"extends: opencode", "kind: chat-completions", "baseUrl: https://api.vendor.test/v1", "secret: api-key"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(string(text), keySentinel) {
		t.Fatalf("config.yaml carries the key:\n%s", text)
	}
}

// Every refused input writes nothing, and no error echoes the key or the URL.
func TestAddFromRecipe_ChatCompletionsViaOpenCode_RefusalsWriteNothing(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"no base url", map[string]any{"models": []string{"m"}, "secrets": map[string]string{"api-key": keySentinel}}, "baseUrl"},
		{"no models", map[string]any{"baseUrl": "https://api.vendor.test/v1", "secrets": map[string]string{"api-key": keySentinel}}, "models"},
		{"no key", map[string]any{"baseUrl": "https://api.vendor.test/v1", "models": []string{"m"}}, "a key is required"},
		{"plain http to another host", map[string]any{"baseUrl": "http://api.vendor.test/v1", "models": []string{"m"}, "secrets": map[string]string{"api-key": keySentinel}}, "https"},
		{"token in the url", map[string]any{"baseUrl": "https://u:hunter2@api.vendor.test/v1?token=hunter2", "models": []string{"m"}, "secrets": map[string]string{"api-key": keySentinel}}, "no userinfo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSecretsHarness(t)
			params := map[string]any{"recipe": chatRecipe, "id": "vendor-x"}
			for k, v := range tc.params {
				params[k] = v
			}
			_, err := h.call(t, "provider.addFromRecipe", &requestContext{}, params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want it to mention %q", err, tc.want)
			}
			for _, leak := range []string{keySentinel, "hunter2"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the error echoes %q: %v", leak, err)
				}
			}
			if _, ok := h.runner.Providers().Get("vendor-x"); ok {
				t.Error("an instance was registered")
			}
			if h.stored(t, "vendor-x", "api-key") {
				t.Error("a key was stored")
			}
			if _, err := os.Stat(h.path); !os.IsNotExist(err) {
				t.Errorf("config.yaml was written: %v", err)
			}
		})
	}
}
