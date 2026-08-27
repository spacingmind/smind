package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireToken(t *testing.T) {
	const token = "correct-token"

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantNext   bool
	}{
		{name: "missing header", authHeader: "", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "wrong token", authHeader: "Bearer wrong-token", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "malformed header", authHeader: token, wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "correct token", authHeader: "Bearer " + token, wantStatus: http.StatusOK, wantNext: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()

			RequireToken(token, next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if nextCalled != tt.wantNext {
				t.Errorf("next called = %v, want %v", nextCalled, tt.wantNext)
			}
		})
	}
}
