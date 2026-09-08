package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

func newTestServer(t *testing.T, token string) *Server {
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
	poller := quota.New(s, newFakeQuotaFetcher())
	router := routing.New(s, reg, poller)
	wm := workspace.New(s)
	runner := taskrunner.New(wm)
	srv, err := New(config.Default(), reg, router, wm, runner, s, token)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}

func TestHandlerAuth(t *testing.T) {
	const token = "test-token"
	handler := newTestServer(t, token).Handler()

	tests := []struct {
		name       string
		method     string
		path       string
		authHeader string
		wantStatus int
	}{
		{name: "healthz bypasses auth entirely", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "v1/messages without token", method: http.MethodPost, path: "/v1/messages", wantStatus: http.StatusUnauthorized},
		{name: "v1/messages with wrong token", method: http.MethodPost, path: "/v1/messages", authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
		{name: "v1/chat/completions without token", method: http.MethodPost, path: "/v1/chat/completions", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestHandlerAuth_CorrectTokenReachesProxy checks the middleware lets a
// correctly authenticated request through to the proxy handler, rather than
// asserting an exact status code (which depends on proxy routing behavior,
// covered separately in proxy_test.go).
func TestHandlerAuth_CorrectTokenReachesProxy(t *testing.T) {
	const token = "test-token"
	handler := newTestServer(t, token).Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("status = %d, want request to pass auth and reach the proxy", rec.Code)
	}
}

func TestHandlerAuth_WebUIUnauthenticated(t *testing.T) {
	const token = "test-token"
	handler := newTestServer(t, token).Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHandleToken_Unauthenticated checks that GET /api/token itself carries
// no auth check (see handleToken's doc comment for why) -- it must succeed
// with no Authorization header, same as the web UI's own HTML/JS.
func TestHandleToken_Unauthenticated(t *testing.T) {
	const token = "the-real-token"
	handler := newTestServer(t, token).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/token response: %v", err)
	}
	if body.Token != token {
		t.Errorf("token = %q, want %q", body.Token, token)
	}
}

// TestHandleToken_RoundTripsIntoWorkingWSConnection proves GET /api/token
// serves the daemon's *real* current token, not just some string, by doing
// exactly what the web UI's page JS does: fetch /api/token, then dial /ws
// with the fetched token and successfully call a real RPC method. It also
// checks that a token which merely differs from the fetched one -- rather
// than being a byte-for-byte match -- is rejected by /ws, so this isn't a
// vacuous "any token works" pass.
func TestHandleToken_RoundTripsIntoWorkingWSConnection(t *testing.T) {
	const token = "test-token-for-ws-roundtrip"
	handler := newTestServer(t, token).Handler()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/token")
	if err != nil {
		t.Fatalf("GET /api/token: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/token response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("fetched token is empty")
	}

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// The fetched token must actually open a working connection.
	ws, _, err := websocket.DefaultDialer.Dial(wsBase+"?token="+url.QueryEscape(body.Token), nil)
	if err != nil {
		t.Fatalf("dial /ws with fetched token: %v", err)
	}
	defer ws.Close()

	if err := ws.WriteJSON(map[string]any{"id": "1", "method": "workspace.list"}); err != nil {
		t.Fatalf("write workspace.list request: %v", err)
	}
	var env map[string]any
	if err := ws.ReadJSON(&env); err != nil {
		t.Fatalf("read workspace.list response: %v", err)
	}
	if errVal, ok := env["error"]; ok {
		t.Fatalf("workspace.list returned error: %v", errVal)
	}
	if env["id"] != "1" {
		t.Errorf("response id = %v, want %q", env["id"], "1")
	}
	if _, ok := env["result"]; !ok {
		t.Errorf("response missing result: %v", env)
	}

	// A token that isn't the real one must still be rejected.
	_, resp2, err := websocket.DefaultDialer.Dial(wsBase+"?token=not-"+url.QueryEscape(body.Token), nil)
	if err == nil {
		t.Fatalf("dial /ws with wrong token unexpectedly succeeded")
	}
	if resp2 == nil || resp2.StatusCode != http.StatusUnauthorized {
		status := "<nil response>"
		if resp2 != nil {
			status = resp2.Status
		}
		t.Errorf("dial /ws with wrong token: status = %v, want %d", status, http.StatusUnauthorized)
	}
}
