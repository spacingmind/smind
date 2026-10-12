package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
)

// End to end through the real quota.Passive (ADR-0028 §7, AC59/AC62/AC64): a
// fake upstream answers per account, the proxy reports every response, and
// routing reads the resulting cooldown with zero usage-endpoint calls.

type passiveE2E struct {
	p        *proxy
	a, b     int64
	hits     map[string]*atomic.Int64 // by bearer token
	respond  func(token string, n int64, w http.ResponseWriter)
	usage    *atomic.Int64
	passive  *quota.Passive
	upstream *httptest.Server
}

func newPassiveE2E(t *testing.T, respond func(token string, n int64, w http.ResponseWriter)) *passiveE2E {
	t.Helper()
	e := &passiveE2E{hits: map[string]*atomic.Int64{"tok-a": {}, "tok-b": {}}, respond: respond, usage: &atomic.Int64{}}
	usageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.usage.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(usageSrv.Close)
	e.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, ok := e.hits[tok]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		e.respond(tok, c.Add(1), w)
	}))
	t.Cleanup(e.upstream.Close)

	s := newTestStoreForProxy(t)
	reg := accounts.New(s)
	e.passive = quota.NewPassive(s)
	t.Cleanup(e.passive.Close)
	poller := quota.New(s, reg, httpUsageFetcher{url: usageSrv.URL}, quota.WithEnabled(true), quota.WithCooldowns(e.passive))
	t.Cleanup(poller.Close)
	router := routing.New(s, reg, poller)
	e.p = newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, e.upstream)), withObserver(e.passive))
	t.Cleanup(e.p.Close)
	cred := func(tok string) accounts.OAuthCredential {
		return accounts.OAuthCredential{AccessToken: tok, RefreshToken: "r-" + tok, ExpiresAt: time.Now().Add(time.Hour)}
	}
	e.a = addOAuthAccount(t, reg, providerAnthropic, "a", cred("tok-a"))
	e.b = addOAuthAccount(t, reg, providerAnthropic, "b", cred("tok-b"))
	return e
}

func (e *passiveE2E) do(model string) *httptest.ResponseRecorder {
	return doMessages(e.p, fmt.Sprintf(`{"model":%q}`, model))
}

func (e *passiveE2E) count(tok string) int64 { return e.hits[tok].Load() }

func TestProxy_passive_pool_skips_cooled_account_next_request_end_to_end(t *testing.T) {
	t.Parallel()
	reset := strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10)
	e := newPassiveE2E(t, func(tok string, n int64, w http.ResponseWriter) {
		if tok == "tok-a" {
			w.Header().Set("Anthropic-Ratelimit-Unified-Status", "rejected")
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Status", "rejected")
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Reset", reset)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	first := e.do("claude-opus-4-1")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first status = %d, want the upstream 429 passed through", first.Code)
	}
	if got := first.Header().Get("Anthropic-Ratelimit-Unified-5h-Status"); got != "rejected" {
		t.Errorf("429 headers not passed through: 5h-status = %q", got)
	}
	// The very next request, any model, goes to the other account.
	for _, m := range []string{"claude-opus-4-1", "claude-sonnet-4-5"} {
		if w := e.do(m); w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 from the second account", m, w.Code)
		}
	}
	if a, b := e.count("tok-a"), e.count("tok-b"); a != 1 || b != 2 {
		t.Fatalf("upstream hits a=%d b=%d, want a=1 (the rejection) b=2", a, b)
	}
	if got := e.usage.Load(); got != 0 {
		t.Fatalf("usage endpoint requests = %d, want 0", got)
	}
}

func TestProxy_passive_bare_429_and_success_headers_do_not_cool_the_account(t *testing.T) {
	t.Parallel()
	e := newPassiveE2E(t, func(tok string, n int64, w http.ResponseWriter) {
		if tok != "tok-a" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		switch n {
		case 1: // bare 429, shared windows allowed, no Retry-After
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Status", "allowed")
			w.Header().Set("Anthropic-Ratelimit-Unified-7d-Status", "allowed")
			w.WriteHeader(http.StatusTooManyRequests)
		default: // success carrying rejected-looking headers: display only
			w.Header().Set("Anthropic-Ratelimit-Unified-Status", "rejected")
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Status", "rejected")
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "1.0")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	})
	if w := e.do("claude-opus-4-1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("first status = %d, want 429", w.Code)
	}
	for i := 0; i < 3; i++ {
		if w := e.do("claude-opus-4-1"); w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i, w.Code)
		}
	}
	if a, b := e.count("tok-a"), e.count("tok-b"); a != 4 || b != 0 {
		t.Fatalf("upstream hits a=%d b=%d, want all 4 on the first account (no cooldown)", a, b)
	}
	if _, ok := e.passive.Cooldown(e.a, "claude-opus-4-1", time.Now()); ok {
		t.Fatal("a bare 429 or success headers started a cooldown")
	}
	if got := e.usage.Load(); got != 0 {
		t.Fatalf("usage endpoint requests = %d, want 0", got)
	}
}

func TestProxy_passive_model_scoped_rejection_only_moves_that_model(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	rejected := false
	e := newPassiveE2E(t, func(tok string, n int64, w http.ResponseWriter) {
		mu.Lock()
		defer mu.Unlock()
		if tok == "tok-a" && !rejected {
			rejected = true
			// 7d_oi-only shape with a Retry-After deadline.
			w.Header().Set("Anthropic-Ratelimit-Unified-Status", "rejected")
			w.Header().Set("Anthropic-Ratelimit-Unified-5h-Status", "allowed")
			w.Header().Set("Anthropic-Ratelimit-Unified-7d-Status", "allowed")
			w.Header().Set("Anthropic-Ratelimit-Unified-7d_oi-Status", "rejected")
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if w := e.do("claude-fable-1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("first status = %d, want 429", w.Code)
	}
	// Another session (own credential header) so affinity does not mask the
	// per-model decision.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-fable-1"}`))
	req.Header.Set("x-api-key", "other-client")
	w := httptest.NewRecorder()
	e.p.handleAnthropic(w, req)
	if w.Code != http.StatusOK || e.count("tok-b") != 1 {
		t.Fatalf("the rejected model did not move to account b: status %d, b hits %d", w.Code, e.count("tok-b"))
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5"}`))
	req.Header.Set("x-api-key", "third-client")
	w = httptest.NewRecorder()
	e.p.handleAnthropic(w, req)
	if w.Code != http.StatusOK || e.count("tok-a") != 2 {
		t.Fatalf("a sibling model should still use account a: status %d, a hits %d", w.Code, e.count("tok-a"))
	}
}
