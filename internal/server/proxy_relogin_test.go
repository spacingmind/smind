package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/spacingmind/smind/internal/accounts"
)

// reloginRefresher answers per refresh token: invalid_grant for a dead one, a
// fresh credential otherwise. It counts calls per refresh token.
type reloginRefresher struct {
	dead  map[string]bool
	mu    sync.Mutex
	calls map[string]int
}

func (f *reloginRefresher) Refresh(_ context.Context, cred accounts.OAuthCredential) (accounts.OAuthCredential, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[cred.RefreshToken]++
	f.mu.Unlock()
	if f.dead[cred.RefreshToken] {
		return accounts.OAuthCredential{}, &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	}
	return accounts.OAuthCredential{
		AccessToken:  "fresh-" + cred.RefreshToken,
		RefreshToken: cred.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil
}

func (f *reloginRefresher) count(refresh string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[refresh]
}

func expiredCred(refresh string) accounts.OAuthCredential {
	return accounts.OAuthCredential{AccessToken: "old-" + refresh, RefreshToken: refresh, ExpiresAt: time.Now().Add(-time.Minute)}
}

func reloginRequest(p *proxy) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m"}`))
	p.handleAnthropic(w, req)
	return w
}

type bearerUpstream struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
	hits  atomic.Int64
}

func newBearerUpstream(t *testing.T) *bearerUpstream {
	u := &bearerUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.mu.Lock()
		u.auths = append(u.auths, r.Header.Get("Authorization"))
		u.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(u.Close)
	return u
}

func Test_proxy_routes_around_a_needs_relogin_account(t *testing.T) {
	up := newBearerUpstream(t)
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, up.Server)))
	f := &reloginRefresher{dead: map[string]bool{"rt-a": true}}
	p.anthropicRefresher = f
	a := addOAuthAccount(t, reg, providerAnthropic, "a", expiredCred("rt-a"))
	addOAuthAccount(t, reg, providerAnthropic, "b", expiredCred("rt-b"))

	if _, err := reg.EnsureFresh(context.Background(), a, f); err == nil {
		t.Fatal("EnsureFresh(A) err = nil, want terminal error")
	}
	for i := 0; i < 3; i++ {
		if w := reloginRequest(p); w.Code != 200 {
			t.Fatalf("request %d status = %d: %s", i, w.Code, w.Body.String())
		}
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.auths) != 3 {
		t.Fatalf("upstream saw %d requests, want 3", len(up.auths))
	}
	for _, got := range up.auths {
		if got != "Bearer fresh-rt-b" {
			t.Errorf("upstream bearer = %q, want B's token", got)
		}
	}
	if n := f.count("rt-a"); n != 1 {
		t.Errorf("A refresher calls = %d, want 1", n)
	}
}

func assertReloginAnswer(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body.Error.Type != "authentication_error" {
		t.Errorf("error type = %q, want authentication_error", body.Error.Type)
	}
	if !strings.Contains(body.Error.Message, "Sign in to it again") {
		t.Errorf("message = %q", body.Error.Message)
	}
}

func Test_proxy_answers_401_sentence_when_every_account_needs_relogin(t *testing.T) {
	up := newBearerUpstream(t)
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, up.Server)))
	f := &reloginRefresher{dead: map[string]bool{"rt-a": true}}
	p.anthropicRefresher = f
	a := addOAuthAccount(t, reg, providerAnthropic, "a", expiredCred("rt-a"))
	_, _ = reg.EnsureFresh(context.Background(), a, f)

	assertReloginAnswer(t, reloginRequest(p))
	if n := up.hits.Load(); n != 0 {
		t.Errorf("upstream requests = %d, want 0", n)
	}
}

func Test_proxy_does_not_hit_the_token_endpoint_again(t *testing.T) {
	up := newBearerUpstream(t)
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, up.Server)))
	f := &reloginRefresher{dead: map[string]bool{"rt-a": true}}
	p.anthropicRefresher = f
	addOAuthAccount(t, reg, providerAnthropic, "a", expiredCred("rt-a"))

	for i := 0; i < 3; i++ {
		assertReloginAnswer(t, reloginRequest(p))
		if n := f.count("rt-a"); n != 1 {
			t.Fatalf("after request %d refresher calls = %d, want 1", i, n)
		}
	}
	if n := up.hits.Load(); n != 0 {
		t.Errorf("upstream requests = %d, want 0", n)
	}
}
