package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spacingmind/smind/internal/version"
)

// TestHandleHealth_ReportsVersion asserts GET /healthz returns all three
// fields, with version bound to the package var the build stamps (AC4);
// the injected value is restored afterwards so other tests keep seeing
// "dev".
func TestHandleHealth_ReportsVersion(t *testing.T) {
	handler := newTestServer(t, "test-token").Handler()

	prev := version.Version
	version.Version = "0.7.0-dev+abc1234"
	t.Cleanup(func() { version.Version = prev })

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz response: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
	if body.Service != "smind" {
		t.Errorf("service = %q, want %q", body.Service, "smind")
	}
	if body.Version != "0.7.0-dev+abc1234" {
		t.Errorf("version = %q, want the injected %q", body.Version, "0.7.0-dev+abc1234")
	}
}
