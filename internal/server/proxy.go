package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	utls "github.com/refraction-networking/utls"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/transport"
)

const (
	providerAnthropic = "anthropic"
	providerOpenAI    = "openai"

	anthropicMessagesURL     = "https://api.anthropic.com/v1/messages"
	openaiChatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	defaultProxySessionKey   = "default"
)

// requiredAnthropicBetas are the anthropic-beta flags Anthropic's API
// requires to accept an OAuth-authenticated request at all. See
// claudeCodeCLIBetas in
// refs/cliproxyapi/internal/runtime/executor/claude_executor_request.go for
// the full (much longer) conditional beta list; replicating that logic is
// out of scope here, this is just the unconditional baseline.
var requiredAnthropicBetas = []string{"claude-code-20250219", "oauth-2025-04-20"}

// proxy handles the /v1/messages and /v1/chat/completions endpoints,
// selecting an account via router and forwarding the request to the real
// provider.
type proxy struct {
	registry *accounts.Registry
	router   *routing.Router

	anthropicClient    *http.Client
	openaiClient       *http.Client
	anthropicRefresher accounts.OAuthRefresher
	openaiRefresher    accounts.OAuthRefresher
}

// proxyOption configures a proxy constructed via newProxy.
type proxyOption func(*proxy)

// withAnthropicHTTPClient overrides the client used to reach Anthropic.
// Intended for tests to redirect requests at a local server.
func withAnthropicHTTPClient(c *http.Client) proxyOption {
	return func(p *proxy) { p.anthropicClient = c }
}

// withOpenAIHTTPClient overrides the client used to reach OpenAI. Intended
// for tests to redirect requests at a local server.
func withOpenAIHTTPClient(c *http.Client) proxyOption {
	return func(p *proxy) { p.openaiClient = c }
}

// newProxy returns a proxy backed by reg and router. By default it reaches
// providers over uTLS-fingerprinted clients, matching internal/accounts'
// refreshers: Anthropic over Firefox, OpenAI over Chrome.
func newProxy(reg *accounts.Registry, router *routing.Router, opts ...proxyOption) *proxy {
	p := &proxy{
		registry:           reg,
		router:             router,
		anthropicClient:    transport.Client(utls.HelloFirefox_Auto),
		openaiClient:       transport.Client(utls.HelloChrome_Auto),
		anthropicRefresher: accounts.NewAnthropicRefresher(),
		openaiRefresher:    accounts.NewOpenAIRefresher(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *proxy) handleAnthropic(w http.ResponseWriter, r *http.Request) {
	p.serve(w, r, providerAnthropic, anthropicMessagesURL, p.anthropicClient, p.anthropicRefresher)
}

func (p *proxy) handleOpenAI(w http.ResponseWriter, r *http.Request) {
	p.serve(w, r, providerOpenAI, openaiChatCompletionsURL, p.openaiClient, p.openaiRefresher)
}

// serve routes r to an account for provider and forwards it to upstreamURL
// over client, injecting that account's credentials.
//
// Errors that mean smind itself can't route the request (no accounts, all
// exhausted, refresh failure) are reported as 503: they're a temporary
// smind-side condition the caller should retry. A failure to reach the
// upstream provider itself is reported as 502 (bad gateway), the standard
// distinction between "the proxy has no one to ask" and "the proxy asked and
// the answer didn't come back".
func (p *proxy) serve(w http.ResponseWriter, r *http.Request, provider, upstreamURL string, client *http.Client, refresher accounts.OAuthRefresher) {
	ctx := r.Context()

	all, err := p.registry.List()
	if err != nil {
		writeProviderError(w, provider, http.StatusServiceUnavailable, fmt.Sprintf("list accounts: %v", err))
		return
	}

	var candidateIDs []int64
	for _, a := range all {
		if a.Provider == provider {
			candidateIDs = append(candidateIDs, a.ID)
		}
	}
	if len(candidateIDs) == 0 {
		writeProviderError(w, provider, http.StatusServiceUnavailable, fmt.Sprintf("no %s accounts configured", provider))
		return
	}

	// Per-workspace hard/pool policy assignment needs the Workspace concept
	// (Phase 2). Until then, every configured account of the right provider
	// is a pool candidate; this is a documented Phase 1 simplification, not
	// a bug.
	account, err := p.router.Route(ctx, sessionKey(r), routing.PolicyPool, candidateIDs)
	if err != nil {
		writeProviderError(w, provider, http.StatusServiceUnavailable, fmt.Sprintf("route request: %v", err))
		return
	}

	outReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, r.Body)
	if err != nil {
		writeProviderError(w, provider, http.StatusServiceUnavailable, fmt.Sprintf("build upstream request: %v", err))
		return
	}
	outReq.ContentLength = r.ContentLength
	forwardHeaders(outReq.Header, r.Header)

	if err := p.injectCredentials(ctx, provider, account, refresher, outReq, r); err != nil {
		writeProviderError(w, provider, http.StatusServiceUnavailable, fmt.Sprintf("credential refresh: %v", err))
		return
	}

	resp, err := client.Do(outReq)
	if err != nil {
		writeProviderError(w, provider, http.StatusBadGateway, fmt.Sprintf("upstream request: %v", err))
		return
	}
	defer resp.Body.Close()

	copyResponse(w, resp)
}

// injectCredentials sets outReq's auth headers for account, refreshing an
// oauth credential first if needed. It assumes forwardHeaders has already
// stripped whatever credential header the incoming caller sent.
func (p *proxy) injectCredentials(ctx context.Context, provider string, account accounts.Account, refresher accounts.OAuthRefresher, outReq, incoming *http.Request) error {
	switch account.CredentialType {
	case accounts.CredentialTypeAPIKey:
		if account.APIKey == nil {
			return fmt.Errorf("account %d: api_key credential missing key data", account.ID)
		}
		switch provider {
		case providerAnthropic:
			outReq.Header.Set("x-api-key", account.APIKey.Key)
		case providerOpenAI:
			outReq.Header.Set("Authorization", "Bearer "+account.APIKey.Key)
		}
		return nil

	case accounts.CredentialTypeOAuth:
		fresh, err := p.registry.EnsureFresh(ctx, account.ID, refresher)
		if err != nil {
			return err
		}
		outReq.Header.Set("Authorization", "Bearer "+fresh.OAuth.AccessToken)
		if provider == providerAnthropic {
			outReq.Header.Set("anthropic-beta", mergeAnthropicBetas(incoming.Header.Get("anthropic-beta")))
		}
		return nil

	default:
		return fmt.Errorf("account %d: unknown credential type %q", account.ID, account.CredentialType)
	}
}

// mergeAnthropicBetas returns an anthropic-beta header value containing
// requiredAnthropicBetas followed by any values the incoming client request
// already sent, deduplicated.
func mergeAnthropicBetas(existing string) string {
	seen := make(map[string]bool)
	betas := make([]string, 0, len(requiredAnthropicBetas))
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		betas = append(betas, v)
	}
	for _, b := range requiredAnthropicBetas {
		add(b)
	}
	for _, b := range strings.Split(existing, ",") {
		add(b)
	}
	return strings.Join(betas, ",")
}

// sessionKey hashes whatever credential the incoming caller sent, for
// routing.Router's session affinity. The raw header value is never used
// directly or logged, only its SHA-256 hex digest.
func sessionKey(r *http.Request) string {
	cred := r.Header.Get("Authorization")
	if cred == "" {
		cred = r.Header.Get("x-api-key")
	}
	if cred == "" {
		return defaultProxySessionKey
	}
	sum := sha256.Sum256([]byte(cred))
	return hex.EncodeToString(sum[:])
}

// forwardHeaders copies src into dst, dropping hop-by-hop headers (Connection,
// Host) and the credential headers serve/injectCredentials own.
func forwardHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch k {
		case "Authorization", "X-Api-Key", "Connection", "Host":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// hopByHopResponseHeaders are stripped from the upstream response before
// copying it to the client; net/http's server manages framing itself
// (Content-Length/Transfer-Encoding) as headers and body are written.
var hopByHopResponseHeaders = map[string]bool{
	"Connection":        true,
	"Keep-Alive":        true,
	"Transfer-Encoding": true,
	"Upgrade":           true,
	"Trailer":           true,
}

// copyResponse copies resp's status, headers, and body to w. The body is
// streamed via io.Copy through a flushing writer so both regular JSON
// responses and SSE streaming responses reach the client incrementally,
// without copyResponse needing to know which kind resp is.
func copyResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vv := range resp.Header {
		if hopByHopResponseHeaders[k] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	fw := flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}
	_, _ = io.Copy(fw, resp.Body)
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

// writeProviderError writes a JSON error shaped like the given provider's
// own error responses, so client SDKs parse it the way they'd parse a real
// provider error rather than a generic proxy error.
func writeProviderError(w http.ResponseWriter, provider string, status int, message string) {
	switch provider {
	case providerAnthropic:
		writeJSON(w, status, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "overloaded_error",
				"message": message,
			},
		})
	case providerOpenAI:
		writeJSON(w, status, map[string]any{
			"error": map[string]any{
				"message": message,
				"type":    "overloaded_error",
				"code":    nil,
			},
		})
	}
}
