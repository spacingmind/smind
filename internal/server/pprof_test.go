package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

func pprofReq(remote, token string, hdr map[string]string) *http.Request {
	r := loopbackReq(http.MethodGet, "/debug/pprof/heap?debug=1", nil)
	r.RemoteAddr = remote
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestPprofOffByDefault(t *testing.T) {
	h := newTestServer(t, "tok").Handler()
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/goroutine", "/debug/pprof/profile", "/debug/pprof/cmdline"} {
		r := pprofReq("127.0.0.1:50000", "tok", nil)
		r.URL.Path = path
		rec := serve(h, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s with debug.pprof off: status %d, want 404 (never the web UI's 200 HTML)", path, rec.Code)
		}
		if strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s answered HTML with debug.pprof off", path)
		}
	}
}

func TestPprofRequiresTokenAndLoopback(t *testing.T) {
	cfg := config.Default()
	cfg.Debug.Pprof = true
	h := newTestServerCfg(t, "tok", cfg).Handler()

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"no token", pprofReq("127.0.0.1:50000", "", nil), http.StatusUnauthorized},
		{"wrong token", pprofReq("127.0.0.1:50000", "nope", nil), http.StatusUnauthorized},
		{"non-loopback peer with token", pprofReq("203.0.113.9:4444", "tok", nil), http.StatusForbidden},
		{"non-loopback peer spoofing X-Forwarded-For", pprofReq("203.0.113.9:4444", "tok", map[string]string{"X-Forwarded-For": "127.0.0.1"}), http.StatusForbidden},
		{"loopback peer behind a proxy", pprofReq("127.0.0.1:50000", "tok", map[string]string{"X-Forwarded-For": "203.0.113.9"}), http.StatusForbidden},
		{"loopback with token", pprofReq("127.0.0.1:50000", "tok", nil), http.StatusOK},
	}
	for _, c := range cases {
		if rec := serve(h, c.req); rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%.80s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	// A non-loopback peer never learns whether its token was right.
	a := serve(h, pprofReq("203.0.113.9:4444", "tok", nil))
	b := serve(h, pprofReq("203.0.113.9:4444", "wrong", nil))
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Fatalf("remote responses differ by token validity: %d/%d", a.Code, b.Code)
	}
}
