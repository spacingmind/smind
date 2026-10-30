package codex

import "testing"

// TestCodexEndpoint_BaseURLNormalization covers NormalizeBaseURL's
// canonical form: the single appended "/v1", every trailing slash stripped
// first so an existing /v1 is never doubled, and the rejected shapes.
func TestCodexEndpoint_BaseURLNormalization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "https://h", want: "https://h/v1"},
		{in: "https://h/", want: "https://h/v1"},
		{in: "https://h/v1", want: "https://h/v1"},
		{in: "https://h/v1/", want: "https://h/v1"},
		{in: "https://h/openai", want: "https://h/openai/v1"},
		{in: "https://h///", want: "https://h/v1"},
		{in: "https://h/v1///", want: "https://h/v1"},
		{in: "  https://h/v1  ", want: "https://h/v1"},
	} {
		if got, err := NormalizeBaseURL(tc.in); err != nil || got != tc.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, %v; want %q, <nil>", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"ftp://h", "", "https://", "https://h?x=1"} {
		if got, err := NormalizeBaseURL(in); err == nil {
			t.Errorf("NormalizeBaseURL(%q) = %q, <nil>; want an error", in, got)
		}
	}
}

// TestCodexEndpoint_NoKeyNoEnvKey proves the key-bearing fields are a
// HasKey decision: without a key the provider entry carries neither
// env_key nor requires_openai_auth, with one it carries both.
func TestCodexEndpoint_NoKeyNoEnvKey(t *testing.T) {
	t.Parallel()

	providerOf := func(hasKey bool) map[string]any {
		t.Helper()
		modelProvider, config, err := (Endpoint{ID: "smind1", BaseURL: "https://x.example", HasKey: hasKey}).ThreadConfig()
		if err != nil {
			t.Fatalf("ThreadConfig(hasKey=%v) error = %v", hasKey, err)
		}
		if modelProvider != "smind1" {
			t.Fatalf("modelProvider = %q, want %q", modelProvider, "smind1")
		}
		providers, ok := config["model_providers"].(map[string]any)
		if !ok {
			t.Fatalf("model_providers = %#v, want a map", config["model_providers"])
		}
		provider, ok := providers["smind1"].(map[string]any)
		if !ok {
			t.Fatalf("model_providers[smind1] = %#v, want a map", providers["smind1"])
		}
		return provider
	}

	noKey := providerOf(false)
	if _, ok := noKey["env_key"]; ok {
		t.Fatalf("env_key = %v, want absent", noKey["env_key"])
	}
	if _, ok := noKey["requires_openai_auth"]; ok {
		t.Fatalf("requires_openai_auth = %v, want absent", noKey["requires_openai_auth"])
	}

	withKey := providerOf(true)
	if withKey["env_key"] != EndpointKeyEnv {
		t.Fatalf("env_key = %v, want %q", withKey["env_key"], EndpointKeyEnv)
	}
	if withKey["requires_openai_auth"] != false {
		t.Fatalf("requires_openai_auth = %v, want false", withKey["requires_openai_auth"])
	}
	if withKey["wire_api"] != "responses" {
		t.Fatalf("wire_api = %v, want %q", withKey["wire_api"], "responses")
	}
}

// TestCodexEndpoint_BadID proves an id outside Codex's model_providers key
// shape is rejected before any config is built.
func TestCodexEndpoint_BadID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "Bad ID"} {
		if _, _, err := (Endpoint{ID: id, BaseURL: "https://h"}).ThreadConfig(); err == nil {
			t.Fatalf("ThreadConfig() with id %q: error = nil, want error", id)
		}
	}
}
