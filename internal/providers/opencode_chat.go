package providers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Chat-completions endpoints through OpenCode (ADR-0046 section 2, the
// "chat-only vendors" route). A vendor that exposes only OpenAI's
// /chat/completions has no Responses API for Codex and no Anthropic API for
// Claude Code; OpenCode speaks chat-completions natively, so an instance of
// the opencode provider with an endpoint of this kind runs on it.

// EndpointChatCompletions is an OpenAI chat-completions API (POST
// <baseUrl>/chat/completions), served by an OpenCode instance.
const EndpointChatCompletions EndpointKind = "chat-completions"

// OpenCodeKeyEnv is the env name an OpenCode chat-completions instance reads
// its vendor key from (a {secret: api-key} reference in config.yaml). The
// generated OpenCode config refers to it as {env:SMIND_OPENCODE_API_KEY}; the
// key itself is never written into a config, an argv or a log.
const OpenCodeKeyEnv = "SMIND_OPENCODE_API_KEY"

// OpenCodeManagedEnv are the env names smind sets itself for a chat-completions
// instance; an instance may not set them, or it could re-enable the repo's
// opencode.json, point OpenCode at another config or models catalog, or move
// its private home.
var OpenCodeManagedEnv = []string{
	"OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT", "OPENCODE_CONFIG_DIR",
	"OPENCODE_DISABLE_PROJECT_CONFIG", "OPENCODE_DISABLE_MODELS_FETCH",
	"OPENCODE_DISABLE_AUTOUPDATE", "OPENCODE_DISABLE_SHARE", "OPENCODE_DISABLE_LSP_DOWNLOAD",
	"OPENCODE_MODELS_URL", "OPENCODE_MODELS_PATH",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
}

// IsOpenCodeChat reports whether s is an OpenCode instance pointed at a
// chat-completions endpoint.
func (s Spec) IsOpenCodeChat() bool {
	return s.IsInstance() && s.Base() == IDOpenCode && s.Endpoint.Kind == EndpointChatCompletions
}

// OpenCodeProviderKey is the provider id the generated OpenCode config uses
// for instance id; the model ids OpenCode shows are "<key>/<model>".
func OpenCodeProviderKey(instanceID string) string { return "smind-" + instanceID }

// OpenCodeModelID maps a model id of the instance's list to the id OpenCode
// knows it by. OpenCode splits at the first slash, so a vendor id that itself
// contains slashes (qwen/qwen3-coder) is unambiguous.
func OpenCodeModelID(s Spec, model string) string {
	if model == "" {
		return ""
	}
	return OpenCodeProviderKey(s.ID) + "/" + model
}

// openCodeDefaultModel is the instance's default model id: the one marked
// Default, else the first.
func openCodeDefaultModel(s Spec) string {
	for _, m := range s.Models {
		if m.Default {
			return m.ID
		}
	}
	if len(s.Models) > 0 {
		return s.Models[0].ID
	}
	return ""
}

// ValidateOpenCodeEndpointURL checks a chat-completions base URL: http(s),
// a host, no userinfo, query or fragment (a token in the URL would be echoed
// and logged), and https unless the host is a loopback address -- a key sent
// over plain http to another machine is readable on the wire.
func ValidateOpenCodeEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("must be an http(s) URL with a host and no userinfo, query or fragment")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("must use https unless the host is a loopback address (a key sent over plain http is readable on the network; tunnel it or use https)")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type openCodeConfig struct {
	Schema           string                      `json:"$schema"`
	Provider         map[string]openCodeProvider `json:"provider"`
	Model            string                      `json:"model"`
	SmallModel       string                      `json:"small_model"`
	EnabledProviders []string                    `json:"enabled_providers"`
	Autoupdate       bool                        `json:"autoupdate"`
	Share            string                      `json:"share"`
}

type openCodeProvider struct {
	NPM     string                   `json:"npm"`
	Name    string                   `json:"name"`
	Options openCodeProviderOptions  `json:"options"`
	Models  map[string]openCodeModel `json:"models"`
}

type openCodeProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
}

type openCodeModel struct {
	Name string `json:"name"`
}

// OpenCodeChatConfig renders the OPENCODE_CONFIG_CONTENT of a chat-completions
// instance. It defines exactly one provider (the instance), selects its
// default model for the main and the small (title) model, and enables no other
// provider, so a stray default or a repo-supplied provider can never receive
// the key. The key is a reference, {env:SMIND_OPENCODE_API_KEY}, present only
// when hasKey; its value is never an input here.
func OpenCodeChatConfig(s Spec, hasKey bool) (string, error) {
	if !s.IsOpenCodeChat() {
		return "", fmt.Errorf("provider %q is not an OpenCode chat-completions instance", s.ID)
	}
	if len(s.Models) == 0 {
		return "", fmt.Errorf("provider %q: models are required (a chat-completions endpoint has no model discovery)", s.ID)
	}
	if err := ValidateOpenCodeEndpointURL(s.Endpoint.BaseURL); err != nil {
		return "", fmt.Errorf("provider %q: endpoint.baseUrl: %w", s.ID, err)
	}
	key := OpenCodeProviderKey(s.ID)
	models := make(map[string]openCodeModel, len(s.Models))
	for _, m := range s.Models {
		name := m.Label
		if name == "" {
			name = m.ID
		}
		models[m.ID] = openCodeModel{Name: name}
	}
	opts := openCodeProviderOptions{BaseURL: strings.TrimRight(s.Endpoint.BaseURL, "/")}
	if hasKey {
		opts.APIKey = "{env:" + OpenCodeKeyEnv + "}"
	}
	label := s.Label
	if label == "" {
		label = s.ID
	}
	def := key + "/" + openCodeDefaultModel(s)
	cfg := openCodeConfig{
		Schema:           "https://opencode.ai/config.json",
		Provider:         map[string]openCodeProvider{key: {NPM: "@ai-sdk/openai-compatible", Name: label, Options: opts, Models: models}},
		Model:            def,
		SmallModel:       def,
		EnabledProviders: []string{key},
		Autoupdate:       false,
		Share:            "disabled",
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("provider %q: render opencode config: %w", s.ID, err)
	}
	return string(out), nil
}
