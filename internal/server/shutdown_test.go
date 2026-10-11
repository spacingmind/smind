package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// shutdownFired reports whether the server's shutdown signal has been
// closed within a short grace window -- long enough that a genuinely
// signalled shutdown (a synchronous close inside the handler) is always
// observed, short enough that the test still fails fast when nothing ever
// fires.
func shutdownFired(srv *Server) bool {
	select {
	case <-srv.ShutdownSignal():
		return true
	case <-time.After(250 * time.Millisecond):
		return false
	}
}

// TestShutdown_NoToken_Unauthorized: POST /api/shutdown without a token,
// or with a wrong one, is 401 and never fires the shutdown signal
// (ADR-0028 D2: the endpoint is guarded by auth.RequireToken exactly like
// the /v1/* proxy routes).
func TestShutdown_NoToken_Unauthorized(t *testing.T) {
	for name, header := range map[string]string{
		"missing token": "",
		"wrong token":   "Bearer not-the-token",
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, "test-token")
			handler := srv.Handler()

			req := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if shutdownFired(srv) {
				t.Error("shutdown signal fired after a rejected request; want nothing to happen")
			}
		})
	}
}

// TestShutdown_GetDoesNotTrigger: only POST may trigger the shutdown. A
// GET with a valid token must not return 202 and must not fire the
// signal.
func TestShutdown_GetDoesNotTrigger(t *testing.T) {
	srv := newTestServer(t, "test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/shutdown", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusAccepted {
		t.Fatalf("GET /api/shutdown status = 202; want anything but (only POST may shut down)")
	}
	if shutdownFired(srv) {
		t.Error("shutdown signal fired on GET; want nothing to happen")
	}
}

// TestShutdown_PostWithToken_AcceptsOnce: a valid POST returns 202 and
// fires the shutdown signal exactly once -- a second POST during shutdown
// still returns 202 and must neither fire the signal again (closing the
// already-closed channel would panic) nor block.
func TestShutdown_PostWithToken_AcceptsOnce(t *testing.T) {
	srv := newTestServer(t, "test-token")
	handler := srv.Handler()

	doPost := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := doPost(); rec.Code != http.StatusAccepted {
		t.Fatalf("first POST status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if !shutdownFired(srv) {
		t.Fatal("shutdown signal did not fire after an accepted POST")
	}

	// Idempotent: second POST during shutdown is still 202; the
	// sync.Once guard is what keeps this from panicking on double close.
	if rec := doPost(); rec.Code != http.StatusAccepted {
		t.Fatalf("second POST status = %d, want %d (idempotent)", rec.Code, http.StatusAccepted)
	}
}
