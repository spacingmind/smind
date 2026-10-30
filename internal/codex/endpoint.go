package codex

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Endpoint is an OpenAI Responses-compatible endpoint a Codex instance
// talks to.
type Endpoint struct {
	ID      string // model provider id, e.g. the smind instance id
	Name    string // display name; defaults to ID
	BaseURL string
	HasKey  bool // a key is configured: only then env_key/requires_openai_auth are emitted
}

// EndpointKeyEnv is the environment variable a model_providers entry's
// env_key resolves the API key from.
const EndpointKeyEnv = "OPENAI_API_KEY"

// providerIDPattern is the shape a Codex model_providers key must have to
// stay referenceable from the rest of Codex's config.
var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// NormalizeBaseURL canonicalizes an endpoint base URL to the form Codex's
// model_providers base_url expects: an http(s) URL with a host, no
// query/fragment/userinfo, and a path ending in exactly one "/v1".
func NormalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// *url.Error's message quotes the whole input, which may embed
		// userinfo that must not leak into logs; unwrap to the cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			return "", fmt.Errorf("codex: parse base URL: %w", uerr.Err)
		}
		return "", fmt.Errorf("codex: parse base URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("codex: base URL scheme %q must be http or https", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("codex: base URL has no host")
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("codex: base URL must not carry a query")
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return "", fmt.Errorf("codex: base URL must not carry a fragment")
	}
	if u.User != nil {
		return "", fmt.Errorf("codex: base URL must not carry userinfo")
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	u.Path = path
	return u.String(), nil
}

// ThreadConfig expands the endpoint into the modelProvider + config pair
// WithModelProvider sends on every thread/start and thread/resume:
// config's model_providers.<ID> entry points Codex at this endpoint over
// the OpenAI Responses wire API. Only an endpoint with a configured key
// (HasKey) carries env_key/requires_openai_auth, since resolving
// OPENAI_API_KEY without one would fail every request.
func (e Endpoint) ThreadConfig() (modelProvider string, config map[string]any, err error) {
	if !providerIDPattern.MatchString(e.ID) {
		return "", nil, fmt.Errorf("codex: model provider id %q must match %s", e.ID, providerIDPattern)
	}
	baseURL, err := NormalizeBaseURL(e.BaseURL)
	if err != nil {
		return "", nil, err
	}
	name := e.Name
	if name == "" {
		name = e.ID
	}
	provider := map[string]any{
		"name":     name,
		"base_url": baseURL,
		"wire_api": "responses",
	}
	if e.HasKey {
		provider["env_key"] = EndpointKeyEnv
		provider["requires_openai_auth"] = false
	}
	return e.ID, map[string]any{
		"model_providers": map[string]any{e.ID: provider},
	}, nil
}
