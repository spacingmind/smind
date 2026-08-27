package accounts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/spacingmind/smind/internal/transport"
)

const (
	anthropicTokenURL = "https://platform.claude.com/v1/oauth/token"
	anthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicScope    = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"

	openaiTokenURL = "https://auth.openai.com/oauth/token"
	openaiClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// AnthropicRefresher refreshes Claude Code OAuth credentials against
// Anthropic's platform.claude.com token endpoint.
type AnthropicRefresher struct {
	httpClient *http.Client
}

// AnthropicRefresherOption configures an AnthropicRefresher.
type AnthropicRefresherOption func(*AnthropicRefresher)

// WithAnthropicHTTPClient overrides the HTTP client used for refresh
// requests. Intended for tests to redirect requests at a local server.
func WithAnthropicHTTPClient(client *http.Client) AnthropicRefresherOption {
	return func(r *AnthropicRefresher) {
		r.httpClient = client
	}
}

// NewAnthropicRefresher returns an AnthropicRefresher. By default it issues
// requests over a Firefox-fingerprinted uTLS client: Anthropic's OAuth
// domain sits behind Cloudflare bot detection that blocks Go's stdlib TLS
// fingerprint, so a plain http.Client gets refused before the request body
// is even considered.
func NewAnthropicRefresher(opts ...AnthropicRefresherOption) *AnthropicRefresher {
	r := &AnthropicRefresher{httpClient: transport.Client(utls.HelloFirefox_Auto)}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

type anthropicTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Refresh exchanges cred.RefreshToken for a new access token.
func (r *AnthropicRefresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	// Anthropic's token endpoint parses a JSON body, not the form-encoded
	// body the standard OAuth2 refresh-token grant sends, so this can't
	// reuse OAuth2Refresher.
	reqBody, err := json.Marshal(map[string]string{
		"client_id":     anthropicClientID,
		"grant_type":    "refresh_token",
		"refresh_token": cred.RefreshToken,
		"scope":         anthropicScope,
	})
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("marshal anthropic oauth refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("create anthropic oauth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "axios/1.15.2")
	req.Header.Set("Accept-Encoding", "gzip, compress, deflate, br")
	req.Header.Set("Connection", "close")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("anthropic oauth refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("read anthropic oauth refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return OAuthCredential{}, fmt.Errorf("anthropic oauth refresh failed with status %d: %s", resp.StatusCode, body)
	}

	var tok anthropicTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return OAuthCredential{}, fmt.Errorf("parse anthropic oauth refresh response: %w", err)
	}

	refreshToken := tok.RefreshToken
	if refreshToken == "" {
		refreshToken = cred.RefreshToken
	}
	return OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}, nil
}

// OpenAIRefresher refreshes ChatGPT/Codex OAuth credentials against
// OpenAI's auth.openai.com token endpoint.
type OpenAIRefresher struct {
	httpClient *http.Client
}

// OpenAIRefresherOption configures an OpenAIRefresher.
type OpenAIRefresherOption func(*OpenAIRefresher)

// WithOpenAIHTTPClient overrides the HTTP client used for refresh requests.
// Intended for tests to redirect requests at a local server.
func WithOpenAIHTTPClient(client *http.Client) OpenAIRefresherOption {
	return func(r *OpenAIRefresher) {
		r.httpClient = client
	}
}

// NewOpenAIRefresher returns an OpenAIRefresher. By default it issues
// requests over a Chrome-fingerprinted uTLS client, for consistency with
// AnthropicRefresher rather than a documented Cloudflare requirement on this
// endpoint.
func NewOpenAIRefresher(opts ...OpenAIRefresherOption) *OpenAIRefresher {
	r := &OpenAIRefresher{httpClient: transport.Client(utls.HelloChrome_Auto)}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

type openAITokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Refresh exchanges cred.RefreshToken for a new access token. OpenAI's
// refresh response carries no expires_in field, so ExpiresAt is derived
// from the exp claim of the returned access token, itself a JWT.
func (r *OpenAIRefresher) Refresh(ctx context.Context, cred OAuthCredential) (OAuthCredential, error) {
	reqBody, err := json.Marshal(map[string]string{
		"client_id":     openaiClientID,
		"grant_type":    "refresh_token",
		"refresh_token": cred.RefreshToken,
	})
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("marshal openai oauth refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("create openai oauth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("openai oauth refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("read openai oauth refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return OAuthCredential{}, fmt.Errorf("openai oauth refresh failed with status %d: %s", resp.StatusCode, body)
	}

	var tok openAITokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return OAuthCredential{}, fmt.Errorf("parse openai oauth refresh response: %w", err)
	}

	expiresAt, err := jwtExpiry(tok.AccessToken)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("parse openai access token expiry: %w", err)
	}

	refreshToken := tok.RefreshToken
	if refreshToken == "" {
		refreshToken = cred.RefreshToken
	}
	return OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// jwtExpiry reads the exp claim out of a JWT's payload segment without
// verifying its signature. That's fine here: the token was just obtained
// directly from OpenAI's own token endpoint over TLS, so there's no
// untrusted party whose claims need verifying.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("access token is not a JWT: want 3 dot-separated segments, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse JWT payload: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("JWT payload has no exp claim")
	}

	return time.Unix(int64(claims.Exp), 0), nil
}
