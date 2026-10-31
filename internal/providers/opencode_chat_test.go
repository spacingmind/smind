package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func openCodeChatSpec() Spec {
	return Spec{
		ID: "vendor-x", Label: "Vendor X", Runner: RunnerACP, Extends: IDOpenCode, Custom: true,
		Endpoint: Endpoint{Kind: EndpointChatCompletions, BaseURL: "https://api.vendor.test/v1/"},
		Models:   []Model{{ID: "chat-1", Label: "Chat One"}, {ID: "qwen/qwen3-coder", Default: true}},
	}
}

func decodeOpenCodeConfig(t *testing.T, raw string) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("the generated config is not JSON: %v\n%s", err, raw)
	}
	return cfg
}

// The config defines exactly one provider (the instance), selects its default
// model for both the main and the small (title) model, and enables nothing
// else, so no stray default or repo-supplied provider can receive the key.
func TestOpenCodeChatConfig_ShapeIsolatesTheInstance(t *testing.T) {
	t.Parallel()
	raw, err := OpenCodeChatConfig(openCodeChatSpec(), true)
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeOpenCodeConfig(t, raw)
	prov, _ := cfg["provider"].(map[string]any)
	if len(prov) != 1 {
		t.Fatalf("providers = %v; want exactly the instance", prov)
	}
	entry, _ := prov["smind-vendor-x"].(map[string]any)
	if entry == nil {
		t.Fatalf("the provider key is not smind-vendor-x: %v", prov)
	}
	if entry["npm"] != "@ai-sdk/openai-compatible" || entry["name"] != "Vendor X" {
		t.Errorf("entry = %v", entry)
	}
	opts, _ := entry["options"].(map[string]any)
	if opts["baseURL"] != "https://api.vendor.test/v1" {
		t.Errorf("baseURL = %v; want the URL with the trailing slash trimmed", opts["baseURL"])
	}
	models, _ := entry["models"].(map[string]any)
	if len(models) != 2 || models["chat-1"] == nil || models["qwen/qwen3-coder"] == nil {
		t.Errorf("models = %v", models)
	}
	if m, _ := models["chat-1"].(map[string]any); m["name"] != "Chat One" {
		t.Errorf("the label is not used as the model name: %v", m)
	}
	for _, field := range []string{"model", "small_model"} {
		if cfg[field] != "smind-vendor-x/qwen/qwen3-coder" {
			t.Errorf("%s = %v; want the default model under the instance's provider key", field, cfg[field])
		}
	}
	if en, _ := cfg["enabled_providers"].([]any); len(en) != 1 || en[0] != "smind-vendor-x" {
		t.Errorf("enabled_providers = %v; want only the instance", cfg["enabled_providers"])
	}
	if cfg["autoupdate"] != false || cfg["share"] != "disabled" {
		t.Errorf("autoupdate = %v share = %v", cfg["autoupdate"], cfg["share"])
	}
}

// The key is a reference, never a value: the config carries {env:NAME} and the
// function has no way to receive the key at all.
func TestOpenCodeChatConfig_KeyIsAReferenceOnly(t *testing.T) {
	t.Parallel()
	raw, err := OpenCodeChatConfig(openCodeChatSpec(), true)
	if err != nil {
		t.Fatal(err)
	}
	opts := decodeOpenCodeConfig(t, raw)["provider"].(map[string]any)["smind-vendor-x"].(map[string]any)["options"].(map[string]any)
	if opts["apiKey"] != "{env:SMIND_OPENCODE_API_KEY}" {
		t.Fatalf("apiKey = %v; want the env reference", opts["apiKey"])
	}
	noKey, err := OpenCodeChatConfig(openCodeChatSpec(), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noKey, "apiKey") || strings.Contains(noKey, "{env:") {
		t.Errorf("a keyless instance still carries an apiKey: %s", noKey)
	}
}

func TestOpenCodeChatConfig_DefaultModelFallsBackToTheFirst(t *testing.T) {
	t.Parallel()
	s := openCodeChatSpec()
	s.Models = []Model{{ID: "first"}, {ID: "second"}}
	raw, err := OpenCodeChatConfig(s, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeOpenCodeConfig(t, raw)["model"]; got != "smind-vendor-x/first" {
		t.Errorf("model = %v", got)
	}
}

func TestOpenCodeChatConfig_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"not an instance", func(s *Spec) { s.Extends = "" }, "not an OpenCode chat-completions instance"},
		{"another base", func(s *Spec) { s.Extends = IDKimi }, "not an OpenCode chat-completions instance"},
		{"another endpoint kind", func(s *Spec) { s.Endpoint.Kind = EndpointOpenAIResponses }, "not an OpenCode chat-completions instance"},
		{"no models", func(s *Spec) { s.Models = nil }, "models are required"},
		{"plain http remote", func(s *Spec) { s.Endpoint.BaseURL = "http://api.vendor.test/v1" }, "https unless the host is a loopback"},
		{"userinfo", func(s *Spec) { s.Endpoint.BaseURL = "https://u:p@api.vendor.test/v1" }, "no userinfo"},
	} {
		s := openCodeChatSpec()
		tc.mutate(&s)
		if _, err := OpenCodeChatConfig(s, true); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v; want %q", tc.name, err, tc.want)
		}
	}
}

// OpenCode splits a model id at the first slash, so a vendor id that has its
// own slashes stays one model of the instance's provider.
func TestOpenCodeModelID(t *testing.T) {
	t.Parallel()
	s := openCodeChatSpec()
	for model, want := range map[string]string{
		"chat-1":           "smind-vendor-x/chat-1",
		"qwen/qwen3-coder": "smind-vendor-x/qwen/qwen3-coder",
		"":                 "",
	} {
		if got := OpenCodeModelID(s, model); got != want {
			t.Errorf("OpenCodeModelID(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestValidateOpenCodeEndpointURL(t *testing.T) {
	t.Parallel()
	for url, ok := range map[string]bool{
		"https://api.vendor.test/v1":      true,
		"https://api.vendor.test":         true,
		"http://127.0.0.1:8080/v1":        true,
		"http://127.1.2.3/v1":             true,
		"http://localhost:1234/v1":        true,
		"http://LOCALHOST:1234/v1":        true,
		"http://[::1]:8080/v1":            true,
		"http://api.vendor.test/v1":       false,
		"http://localhost.evil.test/v1":   false, // a name that merely starts with localhost
		"http://127.0.0.1.evil.test/v1":   false, // a name that merely starts with a loopback address
		"http://10.0.0.5:8080/v1":         false,
		"http://192.168.1.20/v1":          false,
		"http://169.254.169.254/latest":   false,
		"https://u:p@api.vendor.test/v1":  false,
		"https://api.vendor.test/v1?k=v":  false,
		"https://api.vendor.test/v1#frag": false,
		"ftp://api.vendor.test/v1":        false,
		"file:///etc/passwd":              false,
		"//api.vendor.test/v1":            false,
		"":                                false,
	} {
		err := ValidateOpenCodeEndpointURL(url)
		if (err == nil) != ok {
			t.Errorf("%q: err = %v, want ok=%v", url, err, ok)
		}
		if err != nil && (strings.Contains(err.Error(), "u:p") || strings.Contains(err.Error(), "k=v")) {
			t.Errorf("%q: the error echoes part of the URL: %v", url, err)
		}
	}
}

// An OpenCode instance with an endpoint is the only thing IsOpenCodeChat names.
func TestSpec_IsOpenCodeChat(t *testing.T) {
	t.Parallel()
	if !openCodeChatSpec().IsOpenCodeChat() {
		t.Fatal("the chat-completions instance must be recognised")
	}
	plain := openCodeChatSpec()
	plain.Endpoint = Endpoint{}
	other := openCodeChatSpec()
	other.Extends = IDKimi
	responses := openCodeChatSpec()
	responses.Endpoint.Kind = EndpointOpenAIResponses
	for name, s := range map[string]Spec{"no endpoint": plain, "another base": other, "responses": responses, "builtin": {ID: IDOpenCode}} {
		if s.IsOpenCodeChat() {
			t.Errorf("%s: recognised as an OpenCode chat-completions instance", name)
		}
	}
}
