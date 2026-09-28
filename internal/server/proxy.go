package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/transport"
)

const (
	providerAnthropic = "anthropic"
	providerOpenAI    = "openai"

	anthropicMessagesURL     = "https://api.anthropic.com/v1/messages"
	openaiChatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	defaultProxySessionKey   = "default"

	// maxLoggedErrorLen bounds request_log.error, per M1's "error (short
	// message, never a body)" column: these are smind's own wrapped error
	// strings (never upstream response bodies -- see writeProviderError,
	// which is a separate path), but capped anyway as a defense against a
	// pathological account label or similar ending up in one.
	maxLoggedErrorLen = 500
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
// provider. Every request also produces one request_log row, written
// asynchronously via reqLog (docs/plans/active/orchestration-and-
// metering.md, "M1") -- never on the critical path to the client.
type proxy struct {
	registry *accounts.Registry
	router   *routing.Router

	anthropicClient    *http.Client
	openaiClient       *http.Client
	anthropicRefresher accounts.OAuthRefresher
	openaiRefresher    accounts.OAuthRefresher

	reqLog              *requestLogWriter
	requestLogQueueSize int
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

// withRequestLogQueueSize overrides the async writer's channel capacity.
// Applied before the writer is constructed (see newProxy), unlike
// withRequestLogWriter which replaces it outright -- intended for tests
// that need to force a full queue deterministically.
func withRequestLogQueueSize(n int) proxyOption {
	return func(p *proxy) { p.requestLogQueueSize = n }
}

// withRequestLogWriter replaces the default request_log writer outright
// (constructed against db by newProxy) with w -- for tests that need a
// fake requestLogStore (a blocking or always-erroring one) rather than a
// real database, or that want direct access to a writer built with a
// different queueSize/db than newProxy's own default.
func withRequestLogWriter(w *requestLogWriter) proxyOption {
	return func(p *proxy) { p.reqLog = w }
}

// newProxy returns a proxy backed by reg and router. By default it reaches
// providers over uTLS-fingerprinted clients, matching internal/accounts'
// refreshers: Anthropic over Firefox, OpenAI over Chrome. db backs the
// bounded async request_log writer (see requestLogWriter); Close must be
// called on the returned proxy to drain it, typically via Server.Close.
func newProxy(reg *accounts.Registry, router *routing.Router, db *store.Store, opts ...proxyOption) *proxy {
	p := &proxy{
		registry:            reg,
		router:              router,
		anthropicClient:     transport.Client(utls.HelloFirefox_Auto),
		openaiClient:        transport.Client(utls.HelloChrome_Auto),
		anthropicRefresher:  accounts.NewAnthropicRefresher(),
		openaiRefresher:     accounts.NewOpenAIRefresher(),
		requestLogQueueSize: defaultRequestLogQueueSize,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.reqLog == nil {
		p.reqLog = newRequestLogWriter(db, p.requestLogQueueSize)
	}
	return p
}

// Close drains the proxy's async request_log writer: blocks until every
// already-enqueued row has been written (or failed and counted). Safe to
// call once, after the server has stopped accepting new requests.
func (p *proxy) Close() {
	p.reqLog.Close()
}

func (p *proxy) handleAnthropic(w http.ResponseWriter, r *http.Request) {
	p.serve(w, r, providerAnthropic, anthropicMessagesURL, p.anthropicClient, p.anthropicRefresher)
}

func (p *proxy) handleOpenAI(w http.ResponseWriter, r *http.Request) {
	p.serve(w, r, providerOpenAI, openaiChatCompletionsURL, p.openaiClient, p.openaiRefresher)
}

// serve routes r to an account for provider and forwards it over client,
// injecting that account's credentials. The upstream is
// providerDefaultURL unless the routed account's api_key credential
// carries a base_url override (see accounts.ValidateBaseURL).
//
// Errors that mean smind itself can't route the request (no accounts, all
// exhausted, refresh failure) are reported as 503: they're a temporary
// smind-side condition the caller should retry. A failure to reach the
// upstream provider itself is reported as 502 (bad gateway), the standard
// distinction between "the proxy has no one to ask" and "the proxy asked and
// the answer didn't come back".
//
// Every exit path enqueues exactly one request_log row (see p.reqLog) --
// M1's per-request trace -- before returning. account_id is NULL for
// every route-failure exit (nothing was ever routed to), and set for
// every exit from the point an account was actually chosen onward,
// including an upstream-side failure.
func (p *proxy) serve(w http.ResponseWriter, r *http.Request, provider, providerDefaultURL string, client *http.Client, refresher accounts.OAuthRefresher) {
	ctx := r.Context()
	start := time.Now()

	row := store.RequestLog{
		StartedAt:  start,
		Provider:   provider,
		SessionKey: sessionKey(r),
	}
	routeError := func(status int, err error) {
		row.Status = status
		row.Outcome = store.RequestLogOutcomeRouteError
		row.Error = truncatedError(err)
		row.DurationMs = time.Since(start).Milliseconds()
		p.reqLog.enqueue(row)
		writeProviderError(w, provider, status, err.Error())
	}

	forwardBody, contentLength, meta, metaOK := readRequestBody(r, provider)
	if metaOK {
		if meta.Model != "" {
			model := meta.Model
			row.Model = &model
		}
		stream := meta.Stream
		row.Stream = &stream
	}

	all, err := p.registry.List()
	if err != nil {
		routeError(http.StatusServiceUnavailable, fmt.Errorf("list accounts: %w", err))
		return
	}

	var candidateIDs []int64
	for _, a := range all {
		if a.Provider == provider {
			candidateIDs = append(candidateIDs, a.ID)
		}
	}
	if len(candidateIDs) == 0 {
		routeError(http.StatusServiceUnavailable, fmt.Errorf("no %s accounts configured", provider))
		return
	}

	// Per-workspace hard/pool policy assignment needs the Workspace concept
	// (Phase 2). Until then, every configured account of the right provider
	// is a pool candidate; this is a documented Phase 1 simplification, not
	// a bug.
	account, err := p.router.Route(ctx, row.SessionKey, routing.PolicyPool, candidateIDs)
	if err != nil {
		routeError(http.StatusServiceUnavailable, fmt.Errorf("route request: %w", err))
		return
	}

	upstreamURL := providerDefaultURL
	if account.APIKey != nil && account.APIKey.BaseURL != "" {
		upstreamURL = account.APIKey.BaseURL
	}

	outReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, forwardBody)
	if err != nil {
		routeError(http.StatusServiceUnavailable, fmt.Errorf("build upstream request: %w", err))
		return
	}
	outReq.ContentLength = contentLength
	forwardHeaders(outReq.Header, r.Header)

	if err := p.injectCredentials(ctx, provider, account, refresher, outReq, r); err != nil {
		routeError(http.StatusServiceUnavailable, fmt.Errorf("credential refresh: %w", err))
		return
	}

	// From here on, an account has been chosen: every remaining exit path
	// records it, regardless of whether the upstream call itself succeeds.
	accountID := account.ID
	row.AccountID = &accountID

	resp, err := client.Do(outReq)
	if err != nil {
		row.Status = http.StatusBadGateway
		row.Outcome = store.RequestLogOutcomeUpstreamError
		row.Error = truncatedError(fmt.Errorf("upstream request: %w", err))
		row.DurationMs = time.Since(start).Milliseconds()
		p.reqLog.enqueue(row)
		writeProviderError(w, provider, http.StatusBadGateway, fmt.Sprintf("upstream request: %v", err))
		return
	}
	defer resp.Body.Close()

	upstreamStatus := resp.StatusCode
	row.UpstreamStatus = &upstreamStatus

	capture := newResponseCapture(provider, resp)
	ttfb, ttfbOK, copyErr := copyResponse(w, resp, capture)
	if ttfbOK {
		ms := ttfb.Milliseconds()
		row.TTFBMs = &ms
	}
	usage := capture.finalUsage()
	row.InputTokens = usage.Input
	row.OutputTokens = usage.Output
	row.CacheReadTokens = usage.CacheRead
	row.CacheWriteTokens = usage.CacheWrite
	row.ReasoningTokens = usage.Reasoning
	row.Status = resp.StatusCode
	row.DurationMs = time.Since(start).Milliseconds()

	if copyErr != nil {
		// Status and headers (and possibly part of the body) have already
		// reached the client by this point, so there's no clean JSON error
		// response left to fall back to: letting the handler return
		// normally here would finish the chunked response as if it were
		// complete, silently truncating whatever the client already
		// received -- for LLM streaming output, that reads as a normal,
		// finished reply with the tail quietly missing. Aborting resets the
		// connection (net/http turns http.ErrAbortHandler into a connection
		// reset for HTTP/1.1, RST_STREAM for HTTP/2) so the client sees an
		// error instead of a truncated success.
		if ctx.Err() != nil {
			row.Outcome = store.RequestLogOutcomeClientCancelled
		} else {
			row.Outcome = store.RequestLogOutcomeAborted
		}
		row.Error = truncatedError(copyErr)
		p.reqLog.enqueue(row)
		log.Printf("proxy: warn: upstream stream for %s account %d broke mid-response, aborting downstream: %v", provider, account.ID, copyErr)
		panic(http.ErrAbortHandler)
	}

	if resp.StatusCode >= 400 {
		row.Outcome = store.RequestLogOutcomeUpstreamError
	} else {
		row.Outcome = store.RequestLogOutcomeOK
	}
	p.reqLog.enqueue(row)
}

// readRequestBody buffers up to requestBodyParseCap bytes of r's body to
// extract model/stream (see requestMeta) and, for an OpenAI streaming
// request, to inject stream_options.include_usage when needed. It always
// returns a reader that reproduces r's body byte-for-byte (see this
// file's hard constraint: the client-request equivalent of "never buffer
// a whole stream before forwarding").
//
// If the body fits within the cap, the returned reader is the fully
// buffered (and possibly include_usage-rewritten) body, with an accurate
// length; meta is populated and ok is true. If the body exceeds the cap,
// the returned reader replays the already-read prefix followed by the
// live remainder of r.Body without ever buffering the rest, contentLength
// is r.ContentLength unchanged, and ok is false (model/stream unknown, no
// include_usage rewrite attempted).
func readRequestBody(r *http.Request, provider string) (forwardBody io.Reader, contentLength int64, meta requestMeta, ok bool) {
	buf, err := io.ReadAll(io.LimitReader(r.Body, requestBodyParseCap+1))
	if err != nil || len(buf) > requestBodyParseCap {
		return io.MultiReader(bytes.NewReader(buf), r.Body), r.ContentLength, requestMeta{}, false
	}

	meta, ok = parseRequestMeta(buf)
	body := buf
	if ok && provider == providerOpenAI && meta.Stream {
		body = injectIncludeUsage(buf)
	}
	return bytes.NewReader(body), int64(len(body)), meta, ok
}

// truncatedError renders err as a short string suitable for
// request_log.error -- never nil for a non-nil err, capped at
// maxLoggedErrorLen.
func truncatedError(err error) *string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if len(msg) > maxLoggedErrorLen {
		msg = msg[:maxLoggedErrorLen]
	}
	return &msg
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

// copyResponse copies resp's status, headers, and body to w, returning how
// long the first body byte took to arrive (ttfb, valid only if ttfbOK) and
// any error hit while streaming the body. The body is streamed via
// io.Copy through a flushing writer so both regular JSON responses and SSE
// streaming responses reach the client incrementally, without
// copyResponse needing to know which kind resp is; capture is fed the
// exact same bytes as they pass through, for usage extraction, and never
// affects what is written to w or when (see teeCaptureWriter).
func copyResponse(w http.ResponseWriter, resp *http.Response, capture *responseCapture) (ttfb time.Duration, ttfbOK bool, err error) {
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

	start := time.Now()
	tw := &teeCaptureWriter{fw: fw, capture: capture}
	_, copyErr := io.Copy(tw, resp.Body)
	if tw.sawFirstByte {
		ttfb, ttfbOK = tw.firstByteAt.Sub(start), true
	}
	return ttfb, ttfbOK, copyErr
}

// teeCaptureWriter writes every chunk io.Copy hands it to fw (the real
// client response) and, unconditionally, to capture (the usage-extraction
// side channel) -- capture never blocks or errors, so it can't affect
// what reaches the client or how fast. It also timestamps the first
// Write call, for copyResponse's ttfb measurement.
type teeCaptureWriter struct {
	fw           flushWriter
	capture      *responseCapture
	sawFirstByte bool
	firstByteAt  time.Time
}

func (t *teeCaptureWriter) Write(p []byte) (int, error) {
	if !t.sawFirstByte {
		t.sawFirstByte = true
		t.firstByteAt = time.Now()
	}
	n, err := t.fw.Write(p)
	if t.capture != nil {
		t.capture.write(p)
	}
	return n, err
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
