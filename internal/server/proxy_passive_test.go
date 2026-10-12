package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
)

// Passive observation through the proxy (ADR-0028 §7, AC59/AC62/AC64).

// recordingObserver keeps what the proxy handed it. It copies the status and
// the values it checks, as the real Observer never retains the header.
type recordingObserver struct {
	mu   sync.Mutex
	seen []quota.Response
	// onObserve, when set, runs inside Observe.
	onObserve func(quota.Response)
}

func (o *recordingObserver) Observe(r quota.Response) {
	if o.onObserve != nil {
		o.onObserve(r)
	}
	r.Header = r.Header.Clone()
	o.mu.Lock()
	o.seen = append(o.seen, r)
	o.mu.Unlock()
}

func (o *recordingObserver) responses() []quota.Response {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]quota.Response(nil), o.seen...)
}

func oauthCred() accounts.OAuthCredential {
	return accounts.OAuthCredential{AccessToken: "access-tok", RefreshToken: "refresh-tok", ExpiresAt: time.Now().Add(time.Hour)}
}

func doMessages(p *proxy, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)
	return w
}

func TestProxy_observer_called_once_per_anthropic_oauth_response_with_account_model_status_headers(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.42")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	obs := &recordingObserver{}
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(obs))
	id := addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())

	before := time.Now()
	w := doMessages(p, `{"model":"claude-opus-4-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := obs.responses()
	if len(got) != 1 {
		t.Fatalf("Observe calls = %d, want 1", len(got))
	}
	r := got[0]
	if r.AccountID != id || r.Provider != providerAnthropic || r.Model != "claude-opus-4-1" || r.Status != http.StatusOK {
		t.Errorf("Response = %+v", r)
	}
	if v := r.Header.Get("Anthropic-Ratelimit-Unified-5h-Utilization"); v != "0.42" {
		t.Errorf("observed header = %q, want 0.42", v)
	}
	if r.At.Before(before) || r.At.After(time.Now()) {
		t.Errorf("At = %v, want between request start and now", r.At)
	}
}

func TestProxy_observer_not_called_for_api_key_openai_and_route_errors(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	hc := testHTTPClient(t, upstream)

	obs := &recordingObserver{}
	p, reg := newTestProxy(t, withAnthropicHTTPClient(hc), withOpenAIHTTPClient(hc), withObserver(obs))

	// No account at all: a route error.
	if w := doMessages(p, `{"model":"claude"}`); w.Code < 400 {
		t.Fatalf("no-account status = %d, want an error", w.Code)
	}

	addAPIKeyAccount(t, reg, providerAnthropic, "key", "sk-ant")
	if w := doMessages(p, `{"model":"claude"}`); w.Code != http.StatusOK {
		t.Fatalf("api key status = %d", w.Code)
	}

	addOAuthAccount(t, reg, providerOpenAI, "codex", oauthCred())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt"}`))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("openai oauth status = %d; body = %s", w.Code, w.Body.String())
	}

	if got := obs.responses(); len(got) != 0 {
		t.Fatalf("Observe calls = %d, want 0: %+v", len(got), got)
	}
}

func TestProxy_observer_panic_does_not_break_response(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	obs := &recordingObserver{onObserve: func(quota.Response) { panic("secret-header-value") }}
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(obs))
	addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())

	w := doMessages(p, `{"model":"claude"}`)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("response = %d %q, want the upstream response despite the panic", w.Code, w.Body.String())
	}
}

func TestProxy_429_and_headers_passed_through_unchanged_when_observed(t *testing.T) {
	t.Parallel()
	const body = `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.Header().Set("Anthropic-Ratelimit-Unified-Status", "rejected")
		w.Header().Set("Anthropic-Ratelimit-Unified-Reset", "1760000000")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	obs := &recordingObserver{}
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(obs))
	addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())

	w := doMessages(p, `{"model":"claude"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 passed through", w.Code)
	}
	if got := w.Body.String(); got != body {
		t.Errorf("body = %q, want unchanged", got)
	}
	for k, want := range map[string]string{
		"Retry-After":                        "17",
		"Anthropic-Ratelimit-Unified-Status": "rejected",
		"Anthropic-Ratelimit-Unified-Reset":  "1760000000",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("client header %s = %q, want %q", k, got, want)
		}
	}
	got := obs.responses()
	if len(got) != 1 || got[0].Status != http.StatusTooManyRequests || got[0].Header.Get("Anthropic-Ratelimit-Unified-Status") != "rejected" {
		t.Errorf("observed = %+v, want the 429 with its headers", got)
	}
}

func TestProxy_observer_nil_is_noop(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(nil))
	addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())
	if w := doMessages(p, `{"model":"claude"}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// The public option accepts nil too.
	var o options
	WithQuotaObserver(nil)(&o)
	if o.observer != nil {
		t.Error("WithQuotaObserver(nil) set an observer")
	}
}

// httpUsageFetcher is the poller's fetcher in the zero-call test: it would
// call the (counting) usage endpoint, so any proxy-caused fetch is visible.
type httpUsageFetcher struct{ url string }

func (f httpUsageFetcher) Fetch(ctx context.Context, a accounts.Account) (quota.Report, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return quota.Report{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return quota.Report{}, err
	}
	_ = resp.Body.Close()
	return quota.Report{}, fmt.Errorf("usage status %d", resp.StatusCode)
}

func TestProxy_passive_zero_http_calls_proxy_path(t *testing.T) {
	t.Parallel()
	var usageCalls atomic.Int64
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer usage.Close()

	var n atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch n.Add(1) % 5 {
		case 0:
			w.Header().Set("Anthropic-Ratelimit-Unified-Status", "rejected")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		case 1:
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.9")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case 2:
			w.WriteHeader(http.StatusTooManyRequests) // bare 429
		case 3:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
		}
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router, poller := newEnabledRouting(t, s, httpUsageFetcher{url: usage.URL})
	obs := &recordingObserver{}
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(obs))
	t.Cleanup(p.Close)
	_ = poller
	a := addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())
	_ = addOAuthAccount(t, reg, providerAnthropic, "oauth2", oauthCred())

	before, hadBefore, err := s.GetQuotaPollState(a)
	if err != nil {
		t.Fatalf("GetQuotaPollState() error = %v", err)
	}
	for i := 0; i < 1000; i++ {
		doMessages(p, fmt.Sprintf(`{"model":"claude-m%d"}`, i%3))
	}
	if got := usageCalls.Load(); got != 0 {
		t.Fatalf("usage endpoint requests = %d, want 0", got)
	}
	if got := len(obs.responses()); got != 1000 {
		t.Errorf("Observe calls = %d, want 1000", got)
	}
	after, hadAfter, err := s.GetQuotaPollState(a)
	if err != nil {
		t.Fatalf("GetQuotaPollState() error = %v", err)
	}
	if hadBefore != hadAfter || !reflect.DeepEqual(before, after) {
		t.Errorf("quota_poll_state changed: %+v (%v) -> %+v (%v)", before, hadBefore, after, hadAfter)
	}
}

func TestProxy_count_tokens_not_observed(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	obs := &recordingObserver{}
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withObserver(obs))
	addOAuthAccount(t, reg, providerAnthropic, "oauth1", oauthCred())

	w := httptest.NewRecorder()
	p.handleAnthropicCountTokens(w, httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus-4-1"}`)))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want the upstream 429 passed through", w.Code)
	}
	if got := obs.responses(); len(got) != 0 {
		t.Fatalf("Observe calls for count_tokens = %d, want 0: %+v", len(got), got)
	}

	// The same 429 on /v1/messages is still observed.
	if w := doMessages(p, `{"model":"claude-opus-4-1"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("messages status = %d", w.Code)
	}
	if got := obs.responses(); len(got) != 1 {
		t.Fatalf("Observe calls after /v1/messages = %d, want 1", len(got))
	}
}
