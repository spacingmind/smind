package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
)

// rewriteTransport redirects every request to target's host, regardless of
// the URL the caller built the request against. Mirrors the pattern in
// internal/accounts's refresher tests, so proxy tests can exercise the real
// (constant) provider URLs while actually talking to a local httptest
// server.
type rewriteTransport struct {
	target *url.URL
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testHTTPClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", srv.URL, err)
	}
	return &http.Client{Transport: &rewriteTransport{target: target}}
}

type fakeQuotaFetcher struct {
	mu    sync.Mutex
	usage map[int64]quota.Usage
}

func newFakeQuotaFetcher() *fakeQuotaFetcher {
	return &fakeQuotaFetcher{usage: map[int64]quota.Usage{}}
}

func (f *fakeQuotaFetcher) Fetch(ctx context.Context, account store.Account) (quota.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage[account.ID], nil
}

func (f *fakeQuotaFetcher) setExhausted(accountID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage[accountID] = quota.Usage{TokensUsed: 100, TokensLimit: 100}
}

func newTestProxy(t *testing.T, opts ...proxyOption) (*proxy, *accounts.Registry, *fakeQuotaFetcher) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	reg := accounts.New(s)
	fetcher := newFakeQuotaFetcher()
	poller := quota.New(s, fetcher)
	router := routing.New(s, reg, poller)
	return newProxy(reg, router, opts...), reg, fetcher
}

func addAPIKeyAccount(t *testing.T, reg *accounts.Registry, provider, label, key string) int64 {
	t.Helper()
	a, err := reg.AddAPIKey(provider, label, key)
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	return a.ID
}

func addOAuthAccount(t *testing.T, reg *accounts.Registry, provider, label string, cred accounts.OAuthCredential) int64 {
	t.Helper()
	a, err := reg.AddOAuth(provider, label, cred)
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	return a.ID
}

func TestProxy_Anthropic_APIKeySuccess(t *testing.T) {
	t.Parallel()

	var gotHeader http.Header
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("X-Upstream", "anthropic")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, reg, _ := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("x-api-key", "incoming-client-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q", got)
	}
	if got := w.Header().Get("X-Upstream"); got != "anthropic" {
		t.Errorf("X-Upstream header = %q, want passthrough", got)
	}
	if got := gotHeader.Get("x-api-key"); got != "sk-ant-real" {
		t.Errorf("upstream x-api-key = %q, want account key", got)
	}
	if got := gotHeader.Get("Authorization"); got != "" {
		t.Errorf("upstream Authorization = %q, want empty for api_key account", got)
	}
	if gotBody != `{"model":"claude"}` {
		t.Errorf("upstream body = %q", gotBody)
	}
}

func TestProxy_OpenAI_APIKeySuccess(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	p, reg, _ := newTestProxy(t, withOpenAIHTTPClient(testHTTPClient(t, upstream)))
	addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer incoming-client-key")
	w := httptest.NewRecorder()

	p.handleOpenAI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer sk-openai-real" {
		t.Errorf("upstream Authorization = %q, want Bearer sk-openai-real", gotAuth)
	}
}

func TestProxy_Anthropic_OAuthCredentialAndBetaMerge(t *testing.T) {
	t.Parallel()

	var gotAuth, gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, reg, _ := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	addOAuthAccount(t, reg, providerAnthropic, "oauth1", accounts.OAuthCredential{
		AccessToken:  "access-tok",
		RefreshToken: "refresh-tok",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("anthropic-beta", "client-beta")
	w := httptest.NewRecorder()

	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer access-tok" {
		t.Errorf("upstream Authorization = %q, want Bearer access-tok", gotAuth)
	}
	wantBeta := "claude-code-20250219,oauth-2025-04-20,client-beta"
	if gotBeta != wantBeta {
		t.Errorf("upstream anthropic-beta = %q, want %q", gotBeta, wantBeta)
	}
}

func TestProxy_NoAccountsConfigured(t *testing.T) {
	t.Parallel()

	p, _, _ := newTestProxy(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, w.Body.String())
	}
	if body.Type != "error" || body.Error.Message == "" {
		t.Errorf("body = %+v, want anthropic error shape", body)
	}
}

func TestProxy_AllAccountsExhausted(t *testing.T) {
	t.Parallel()

	p, reg, fetcher := newTestProxy(t)
	accID := addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")
	fetcher.setExhausted(accID)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Code    *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, w.Body.String())
	}
	if body.Error.Message == "" || body.Error.Code != nil {
		t.Errorf("body = %+v, want openai error shape with null code", body)
	}
}

func TestProxy_SessionAffinity(t *testing.T) {
	t.Parallel()

	var gotKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("x-api-key"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, reg, _ := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-account-1")
	addAPIKeyAccount(t, reg, providerAnthropic, "a2", "sk-account-2")

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
		req.Header.Set("x-api-key", "same-incoming-client-key")
		w := httptest.NewRecorder()
		p.handleAnthropic(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200; body = %s", i, w.Code, w.Body.String())
		}
	}

	if len(gotKeys) != 2 || gotKeys[0] != gotKeys[1] {
		t.Errorf("upstream keys = %v, want same account both times", gotKeys)
	}
}

func TestProxy_StreamingPassthrough(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("chunk1\n"))
		flusher.Flush()
		<-release
		_, _ = w.Write([]byte("chunk2\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	p, reg, _ := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	proxySrv := httptest.NewServer(http.HandlerFunc(p.handleAnthropic))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/messages", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	type readResult struct {
		line string
		err  error
	}
	readLine := func() readResult {
		line, err := reader.ReadString('\n')
		return readResult{line, err}
	}

	resultCh := make(chan readResult, 1)
	go func() { resultCh <- readLine() }()

	select {
	case r := <-resultCh:
		if r.err != nil || r.line != "chunk1\n" {
			t.Fatalf("first chunk = %q, err = %v, want chunk1", r.line, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	// chunk2 is gated behind release: prove the client hasn't received it yet
	// by confirming a read attempt doesn't resolve before we let it through.
	go func() { resultCh <- readLine() }()
	select {
	case r := <-resultCh:
		t.Fatalf("second chunk arrived before release: %q, err = %v", r.line, r.err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case r := <-resultCh:
		if r.err != nil || r.line != "chunk2\n" {
			t.Fatalf("second chunk = %q, err = %v, want chunk2", r.line, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second chunk")
	}
}
