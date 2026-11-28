package server

import "errors"

// DefaultWebOrigin is the hosted web client's origin (ADR-0052 H10).
const DefaultWebOrigin = "https://app.spacingmind.com"

// ParseAllowedOrigins validates and normalises operator-configured browser
// origins (spec D3/D4). STUB: the real implementation lands in the next commit.
func ParseAllowedOrigins(values []string) ([]string, error) {
	return nil, errors.New("not implemented")
}

// EffectiveOrigins returns the allowlist: the default (unless noDefault) plus
// the already validated extras, de-duplicated. STUB.
func EffectiveOrigins(extras []string, noDefault bool) []string { return nil }
