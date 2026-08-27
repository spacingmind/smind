package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
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
	return New(config.Default(), reg, router, token)
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
