package server

import "testing"

func TestResolveUpstreamURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, provider, base, path, want string
	}{
		{"anthropic default", providerAnthropic, "", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"anthropic count_tokens", providerAnthropic, "", "/v1/messages/count_tokens", "https://api.anthropic.com/v1/messages/count_tokens"},
		{"anthropic base with prefix", providerAnthropic, "https://gw.example/anthropic", "/v1/messages", "https://gw.example/anthropic/v1/messages"},
		{"anthropic trailing slash", providerAnthropic, "https://gw.example/", "/v1/messages", "https://gw.example/v1/messages"},
		{"anthropic legacy full endpoint", providerAnthropic, "https://gw.example/v1/messages", "/v1/messages", "https://gw.example/v1/messages"},
		{"anthropic legacy + count_tokens", providerAnthropic, "https://gw.example/v1/messages", "/v1/messages/count_tokens", "https://gw.example/v1/messages/count_tokens"},
		{"openai default", providerOpenAI, "", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"openai base with v1", providerOpenAI, "https://gw.example/v1/", "/v1/chat/completions", "https://gw.example/v1/chat/completions"},
		{"openai legacy full endpoint", providerOpenAI, "https://gw.example/v1/chat/completions", "/v1/chat/completions", "https://gw.example/v1/chat/completions"},
		{"openai perplexity legacy", providerOpenAI, "https://api.perplexity.ai/chat/completions", "/v1/chat/completions", "https://api.perplexity.ai/chat/completions"},
		{"openai perplexity base", providerOpenAI, "https://api.perplexity.ai", "/v1/chat/completions", "https://api.perplexity.ai/chat/completions"},
	}
	for _, tc := range cases {
		if got := resolveUpstreamURL(tc.provider, tc.base, tc.path); got != tc.want {
			t.Errorf("%s: resolveUpstreamURL(%q, %q, %q) = %q, want %q", tc.name, tc.provider, tc.base, tc.path, got, tc.want)
		}
	}
}
