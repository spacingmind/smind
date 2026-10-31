package config

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func openCodeChatEntry() CustomProvider {
	return CustomProvider{
		ID: "vendor-x", Extends: providers.IDOpenCode,
		Endpoint: &CustomEndpoint{Kind: "chat-completions", BaseURL: "https://api.vendor.test/v1"},
		Models:   []CustomModel{{ID: "chat-1", Default: true}},
		Env:      map[string]EnvValue{providers.OpenCodeKeyEnv: {Secret: "api-key"}},
	}
}

func TestValidateOpenCodeEndpoint_AcceptsAChatCompletionsInstance(t *testing.T) {
	for _, url := range []string{
		"https://api.vendor.test/v1",
		"https://api.vendor.test",
		"http://127.0.0.1:8080/v1",
		"http://localhost:1234/v1",
		"http://[::1]:8080/v1",
	} {
		e := openCodeChatEntry()
		e.Endpoint.BaseURL = url
		if err := ValidateCustomProviders([]CustomProvider{e}); err != nil {
			t.Errorf("%s: %v", url, err)
		}
	}
}

// Everything an opencode endpoint instance refuses, with the field named and
// no URL or key echoed.
func TestValidateOpenCodeEndpoint_Refusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CustomProvider)
		want   string
	}{
		{"responses kind", func(e *CustomProvider) { e.Endpoint.Kind = "openai-responses" }, "endpoint.kind"},
		{"alias kind", func(e *CustomProvider) { e.Endpoint.Kind = "openai-chat" }, `use "chat-completions"`},
		{"unknown kind", func(e *CustomProvider) { e.Endpoint.Kind = "anthropic" }, "unknown kind"},
		{"empty kind", func(e *CustomProvider) { e.Endpoint.Kind = "" }, "endpoint.kind: required"},
		{"no base url", func(e *CustomProvider) { e.Endpoint.BaseURL = "  " }, "endpoint.baseUrl: required"},
		{"plain http to another host", func(e *CustomProvider) { e.Endpoint.BaseURL = "http://api.vendor.test/v1" }, "https unless the host is a loopback"},
		{"plain http to a LAN address", func(e *CustomProvider) { e.Endpoint.BaseURL = "http://192.168.1.20:8080/v1" }, "https unless the host is a loopback"},
		{"userinfo in the url", func(e *CustomProvider) { e.Endpoint.BaseURL = "https://user:pw@api.vendor.test/v1" }, "no userinfo"},
		{"query in the url", func(e *CustomProvider) { e.Endpoint.BaseURL = "https://api.vendor.test/v1?key=abc" }, "no userinfo, query or fragment"},
		{"fragment in the url", func(e *CustomProvider) { e.Endpoint.BaseURL = "https://api.vendor.test/v1#x" }, "no userinfo, query or fragment"},
		{"not http", func(e *CustomProvider) { e.Endpoint.BaseURL = "file:///etc/passwd" }, "http(s) URL"},
		{"no models", func(e *CustomProvider) { e.Models = nil }, "models: required"},
		{"additional models", func(e *CustomProvider) {
			e.Models = nil
			e.AdditionalModels = []CustomModel{{ID: "x"}}
		}, "additionalModels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := openCodeChatEntry()
			tc.mutate(&e)
			err := ValidateCustomProviders([]CustomProvider{e})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want it to contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "providers.custom[0].") {
				t.Errorf("the error does not name the field: %v", err)
			}
			for _, leak := range []string{"user:pw", "key=abc"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the error echoes part of the URL: %v", err)
				}
			}
		})
	}
}

// An endpoint on any other base is refused, with the supported bases named.
func TestValidateOpenCodeEndpoint_OnlyOpenCodeAndCodexTakeAnEndpoint(t *testing.T) {
	for _, base := range []string{providers.IDClaudeNative, providers.IDKimi} {
		e := openCodeChatEntry()
		e.Extends = base
		err := ValidateCustomProviders([]CustomProvider{e})
		if err == nil || !strings.Contains(err.Error(), "only applies to an instance of codex-native or opencode") {
			t.Errorf("extends %s: err = %v", base, err)
		}
	}
}

// The names smind sets itself cannot be overridden (any case: Windows env
// names are case-insensitive), or an instance could switch the repo's
// opencode.json back on or move OpenCode's private home.
func TestValidateOpenCodeEndpoint_ManagedEnvCannotBeSet(t *testing.T) {
	for _, name := range providers.OpenCodeManagedEnv {
		for _, spelled := range []string{name, strings.ToLower(name)} {
			e := openCodeChatEntry()
			e.Env[spelled] = EnvValue{Literal: "x"}
			err := ValidateCustomProviders([]CustomProvider{e})
			if err == nil || !strings.Contains(err.Error(), "env."+spelled) || !strings.Contains(err.Error(), "cannot be overridden") {
				t.Errorf("env %s: err = %v", spelled, err)
			}
		}
	}
}

// A plain opencode instance (no endpoint) is untouched by all of this.
func TestValidateOpenCodeEndpoint_PlainInstanceUnchanged(t *testing.T) {
	e := CustomProvider{ID: "oc-plain", Extends: providers.IDOpenCode, Env: map[string]EnvValue{"XDG_DATA_HOME": {Literal: "/x"}}}
	if err := ValidateCustomProviders([]CustomProvider{e}); err != nil {
		t.Fatalf("a plain opencode instance may set what it likes: %v", err)
	}
}
