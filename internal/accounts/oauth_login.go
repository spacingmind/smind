package accounts

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/spacingmind/smind/internal/transport"
)

const (
	anthropicCallbackAddr = "localhost:54545"
	anthropicCallbackPath = "/callback"
	anthropicAuthorizeURL = "https://claude.ai/oauth/authorize"

	openaiCallbackAddr = "localhost:1455"
	openaiCallbackPath = "/auth/callback"
	openaiAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	openaiScope        = "openid email profile offline_access"
)

// PKCECodes is one RFC 7636 PKCE verifier/challenge pair (S256 method).
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// GeneratePKCECodes returns a new PKCE verifier/challenge pair: a 96-byte
// random verifier, and its S256 challenge (base64url, no padding, per RFC
// 7636), for a single login attempt.
func GeneratePKCECodes() (PKCECodes, error) {
	raw := make([]byte, 96)
	if _, err := rand.Read(raw); err != nil {
		return PKCECodes{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	verifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(raw)

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(sum[:])

	return PKCECodes{CodeVerifier: verifier, CodeChallenge: challenge}, nil
}

// GenerateState returns a random, URL-safe, opaque token for an OAuth
// authorize request's state parameter, to be compared for exact equality
// against whatever the vendor's callback echoes back.
func GenerateState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(raw), nil
}

// Login is one provider's OAuth authorization-code login flow: building the
// authorize URL a browser is sent to, and exchanging the code the vendor's
// redirect eventually delivers for tokens. It does not itself run the local
// callback listener -- see LoginCoordinator, which owns that and calls
// CallbackAddr/CallbackPath to know where to bind.
type Login interface {
	// CallbackAddr is the host:port the vendor's redirect_uri points at --
	// vendor-fixed, not smind's choice.
	CallbackAddr() string
	// CallbackPath is the path segment of the vendor's redirect_uri.
	CallbackPath() string
	// AuthorizeURL builds the URL to send a browser to, embedding state and
	// pkce's challenge.
	AuthorizeURL(state string, pkce PKCECodes) string
	// Exchange trades an authorization code (plus the PKCE verifier that
	// produced its challenge) for an OAuthCredential. state is the same
	// value passed to AuthorizeURL, already validated against the
	// callback's own state by the caller (LoginCoordinator) -- it's passed
	// through here too because Anthropic's token endpoint additionally
	// expects it as a request body field (unlike OpenAI's, which has no
	// state field in the token request at all -- see OpenAILogin.Exchange).
	Exchange(ctx context.Context, code, state string, pkce PKCECodes) (OAuthCredential, error)
}

// AnthropicLogin implements Login against claude.ai/platform.claude.com's
// authorization-code OAuth flow, reusing anthropicClientID/anthropicScope/
// anthropicTokenURL from refresh_providers.go.
type AnthropicLogin struct {
	httpClient *http.Client
}

// AnthropicLoginOption configures an AnthropicLogin.
type AnthropicLoginOption func(*AnthropicLogin)

// WithAnthropicLoginHTTPClient overrides the HTTP client used for the token
// exchange request. Intended for tests to redirect requests at a local
// server.
func WithAnthropicLoginHTTPClient(client *http.Client) AnthropicLoginOption {
	return func(l *AnthropicLogin) {
		l.httpClient = client
	}
}

// NewAnthropicLogin returns an AnthropicLogin. By default it issues the
// token exchange request over the same Firefox-fingerprinted uTLS client
// AnthropicRefresher uses, for the same Cloudflare bot-detection reason
// (see NewAnthropicRefresher's doc comment).
func NewAnthropicLogin(opts ...AnthropicLoginOption) *AnthropicLogin {
	l := &AnthropicLogin{httpClient: transport.Client(utls.HelloFirefox_Auto)}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// CallbackAddr implements Login.
func (l *AnthropicLogin) CallbackAddr() string { return anthropicCallbackAddr }

// CallbackPath implements Login.
func (l *AnthropicLogin) CallbackPath() string { return anthropicCallbackPath }

// AuthorizeURL implements Login.
func (l *AnthropicLogin) AuthorizeURL(state string, pkce PKCECodes) string {
	q := url.Values{
		"code":                  {"true"},
		"client_id":             {anthropicClientID},
		"response_type":         {"code"},
		"redirect_uri":          {"http://" + anthropicCallbackAddr + anthropicCallbackPath},
		"scope":                 {anthropicScope},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return anthropicAuthorizeURL + "?" + q.Encode()
}

// Exchange implements Login. Anthropic's token endpoint parses a JSON body
// for the authorization_code grant too, exactly like AnthropicRefresher's
// refresh_token grant -- see that type's Refresh doc comment. Unlike
// OpenAI's, Anthropic's token request body also carries state.
func (l *AnthropicLogin) Exchange(ctx context.Context, code, state string, pkce PKCECodes) (OAuthCredential, error) {
	reqBody, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  "http://" + anthropicCallbackAddr + anthropicCallbackPath,
		"client_id":     anthropicClientID,
		"code_verifier": pkce.CodeVerifier,
		"state":         state,
	})
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("marshal anthropic oauth exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("create anthropic oauth exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("anthropic oauth exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("read anthropic oauth exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return OAuthCredential{}, fmt.Errorf("anthropic oauth exchange failed with status %d: %s", resp.StatusCode, body)
	}

	var tok anthropicTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return OAuthCredential{}, fmt.Errorf("parse anthropic oauth exchange response: %w", err)
	}

	return OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}, nil
}

// OpenAILogin implements Login against auth.openai.com's authorization-code
// OAuth flow, reusing openaiClientID/openaiTokenURL from
// refresh_providers.go.
type OpenAILogin struct {
	httpClient *http.Client
}

// OpenAILoginOption configures an OpenAILogin.
type OpenAILoginOption func(*OpenAILogin)

// WithOpenAILoginHTTPClient overrides the HTTP client used for the token
// exchange request. Intended for tests to redirect requests at a local
// server.
func WithOpenAILoginHTTPClient(client *http.Client) OpenAILoginOption {
	return func(l *OpenAILogin) {
		l.httpClient = client
	}
}

// NewOpenAILogin returns an OpenAILogin. By default it issues the token
// exchange request over the same Chrome-fingerprinted uTLS client
// OpenAIRefresher uses (see NewOpenAIRefresher's doc comment).
func NewOpenAILogin(opts ...OpenAILoginOption) *OpenAILogin {
	l := &OpenAILogin{httpClient: transport.Client(utls.HelloChrome_Auto)}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// CallbackAddr implements Login.
func (l *OpenAILogin) CallbackAddr() string { return openaiCallbackAddr }

// CallbackPath implements Login.
func (l *OpenAILogin) CallbackPath() string { return openaiCallbackPath }

// AuthorizeURL implements Login.
func (l *OpenAILogin) AuthorizeURL(state string, pkce PKCECodes) string {
	q := url.Values{
		"client_id":                  {openaiClientID},
		"response_type":              {"code"},
		"redirect_uri":               {"http://" + openaiCallbackAddr + openaiCallbackPath},
		"scope":                      {openaiScope},
		"state":                      {state},
		"code_challenge":             {pkce.CodeChallenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
	}
	return openaiAuthorizeURL + "?" + q.Encode()
}

// Exchange implements Login. Unlike AnthropicLogin, OpenAI's token endpoint
// parses a form-encoded body for the authorization_code grant, and there is
// no state field in the token request itself (state is unused here --
// state validation already happened at the callback-listener step, in
// LoginCoordinator.Login). ExpiresAt is derived from the returned access
// token's JWT exp claim, exactly like OpenAIRefresher.Refresh (OpenAI's
// token response carries no expires_in field for either grant).
func (l *OpenAILogin) Exchange(ctx context.Context, code, _ string, pkce PKCECodes) (OAuthCredential, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {openaiClientID},
		"code":          {code},
		"redirect_uri":  {"http://" + openaiCallbackAddr + openaiCallbackPath},
		"code_verifier": {pkce.CodeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("create openai oauth exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("openai oauth exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("read openai oauth exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return OAuthCredential{}, fmt.Errorf("openai oauth exchange failed with status %d: %s", resp.StatusCode, body)
	}

	var tok openAITokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return OAuthCredential{}, fmt.Errorf("parse openai oauth exchange response: %w", err)
	}

	expiresAt, err := jwtExpiry(tok.AccessToken)
	if err != nil {
		return OAuthCredential{}, fmt.Errorf("parse openai access token expiry: %w", err)
	}

	return OAuthCredential{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}
