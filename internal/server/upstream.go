package server

import "strings"

// Default upstream bases, following refs/cliproxyapi's convention: the
// Anthropic base is host (+ optional prefix) and the incoming path is
// appended whole; the OpenAI base includes /v1 and the incoming path's
// leading /v1 is dropped.
const (
	anthropicDefaultBase = "https://api.anthropic.com"
	openaiDefaultBase    = "https://api.openai.com/v1"
)

// normalizeBaseURL trims a trailing slash from base and, for backward
// compatibility with base_urls stored when they were the FULL endpoint
// (anthropic ".../v1/messages", openai ".../chat/completions"), strips that
// endpoint suffix so the stored value reads as a base. No data migration:
// this runs at read time.
func normalizeBaseURL(provider, base string) string {
	base = strings.TrimRight(base, "/")
	switch provider {
	case providerAnthropic:
		base = strings.TrimSuffix(base, "/v1/messages")
	case providerOpenAI:
		base = strings.TrimSuffix(base, "/chat/completions")
	}
	return strings.TrimRight(base, "/")
}

// resolveUpstreamURL returns the upstream URL for an incoming request path
// given the account's base ("" = provider default). Anthropic: base +
// incomingPath. OpenAI: base + incomingPath minus its leading "/v1".
func resolveUpstreamURL(provider, base, incomingPath string) string {
	if base == "" {
		switch provider {
		case providerAnthropic:
			base = anthropicDefaultBase
		case providerOpenAI:
			base = openaiDefaultBase
		}
	}
	base = normalizeBaseURL(provider, base)
	if provider == providerOpenAI {
		incomingPath = strings.TrimPrefix(incomingPath, "/v1")
	}
	return base + incomingPath
}
