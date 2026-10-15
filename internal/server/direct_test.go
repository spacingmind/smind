package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
)

const (
	testPassword = "correct horse battery"
	daemonToken  = "daemon-token-0123456789"
)

var (
	hashOnce sync.Once
	testHash string
)

// passwordHash computes the test password's bcrypt hash once per test binary
// (cost 12 is deliberate in production, slow in a loop).
func passwordHash(t *testing.T) string {
	t.Helper()
	hashOnce.Do(func() {
		h, err := auth.HashPassword(testPassword)
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		testHash = h
	})
	return testHash
}

func serverWithPassword(t *testing.T, mutate func(*config.ServerConfig)) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Server.PasswordHash = passwordHash(t)
	if mutate != nil {
		mutate(&cfg.Server)
	}
	return newTestServerCfg(t, daemonToken, cfg)
}

// remoteReq is a request from another machine on the LAN, addressed by IP.
func remoteReq(method, target string, body []byte) *http.Request {
	r := httptest.NewRequest(method, "http://192.168.1.10:4648"+target, bytes.NewReader(body))
	r.RemoteAddr = "192.168.1.50:41000"
	return r
}

func login(h http.Handler, r *http.Request, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"password": password})
	r.Body = httptest.NewRequest("POST", "/", bytes.NewReader(body)).Body
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response %q is not JSON: %v", rec.Body.String(), err)
	}
	return m
}

// --- /api/token ---------------------------------------------------------

func TestAPITokenLoopbackOK(t *testing.T) {
	h := newTestServer(t, daemonToken).Handler()
	for _, host := range []string{"127.0.0.1:4648", "localhost:4648", "[::1]:4648", "app.localhost:4648"} {
		r := loopbackReq("GET", "/api/token", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 || decode(t, rec)["token"] != daemonToken {
			t.Errorf("Host %s: status %d body %s, want the token", host, rec.Code, rec.Body)
		}
	}
}

func TestAPITokenForbiddenForNonLoopbackRemoteAddr(t *testing.T) {
	h := newTestServer(t, daemonToken).Handler()
	r := loopbackReq("GET", "/api/token", nil)
	r.RemoteAddr = "192.168.1.50:41000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), daemonToken) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAPITokenForbiddenWhenHostIsExternal(t *testing.T) {
	// A same-host reverse proxy: the TCP peer is loopback, the browser's
	// external name is in Host. The name is allowed (server.hostnames), the
	// token is still not served.
	h := newTestServerCfg(t, daemonToken, func() config.Config {
		c := config.Default()
		c.Server.Hostnames = []string{"smind.example.com"}
		return c
	}()).Handler()
	for _, host := range []string{"smind.example.com", "192.168.1.10:4648"} {
		r := loopbackReq("GET", "/api/token", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), daemonToken) {
			t.Errorf("Host %s: status %d body %s, want 403 without the token", host, rec.Code, rec.Body)
		}
	}
}

func TestAPITokenForbiddenWithForwardedHeaders(t *testing.T) {
	h := newTestServer(t, daemonToken).Handler()
	for _, hdr := range [][2]string{
		{"X-Forwarded-For", "203.0.113.9"}, {"X-Forwarded-Host", "smind.example.com"}, {"X-Forwarded-Proto", "https"},
		{"Forwarded", "for=203.0.113.9"}, {"X-Real-IP", "203.0.113.9"}, {"Via", "1.1 nginx"},
	} {
		r := loopbackReq("GET", "/api/token", nil)
		r.Header.Set(hdr[0], hdr[1])
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), daemonToken) {
			t.Errorf("%s: status %d, want 403", hdr[0], rec.Code)
		}
	}
}

func TestAPITokenForbiddenForForeignOrigin(t *testing.T) {
	h := newTestServer(t, daemonToken).Handler()
	r := loopbackReq("GET", "/api/token", nil)
	r.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	r = loopbackReq("GET", "/api/token", nil)
	r.Header.Set("Origin", "http://127.0.0.1:4648")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("same-origin status %d", rec.Code)
	}
}

// --- Host allowlist -----------------------------------------------------

func TestHostAllowlistDefaults(t *testing.T) {
	h := newTestServer(t, daemonToken).Handler()
	for _, host := range []string{"localhost", "localhost:4648", "127.0.0.1:4648", "[::1]:4648", "foo.localhost", "192.168.1.10:4648", "10.0.0.1", "[2001:db8::1]:4648"} {
		r := loopbackReq("GET", "/healthz", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Errorf("Host %q: status %d, want allowed", host, rec.Code)
		}
	}
}

func TestHostNotAllowed403(t *testing.T) {
	srv := newTestServer(t, daemonToken)
	h := srv.Handler()
	for _, host := range []string{"evil.example.com", "evil.example.com:4648", "localhost.evil.com", "127.0.0.1.evil.com", "", "localhost:abc", "a b", "evil.com/x"} {
		for _, path := range []string{"/healthz", "/", "/api/token", "/api/login", "/ws"} {
			r := loopbackReq("GET", path, nil)
			r.Host = host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Host not allowed") {
				t.Errorf("Host %q %s: status %d body %q, want 403 Host not allowed", host, path, rec.Code, rec.Body)
			}
		}
	}
}

func TestHostAllowlistLeadingDotMatchesSubdomains(t *testing.T) {
	h := newTestServerCfg(t, daemonToken, func() config.Config {
		c := config.Default()
		c.Server.Hostnames = []string{".example.com", "nas.home.lan"}
		return c
	}()).Handler()
	allowed := []string{"example.com", "a.example.com", "a.b.example.com:4648", "EXAMPLE.com", "nas.home.lan", "nas.home.lan."}
	denied := []string{"evilexample.com", "example.com.evil.net", "other.home.lan", "home.lan", "x.nas.home.lan"}
	for host, want := range func() map[string]bool {
		m := map[string]bool{}
		for _, a := range allowed {
			m[a] = true
		}
		for _, d := range denied {
			m[d] = false
		}
		return m
	}() {
		r := loopbackReq("GET", "/healthz", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if got := rec.Code == 200; got != want {
			t.Errorf("Host %q: allowed=%v, want %v", host, got, want)
		}
	}
}

func TestHealthzAuthRequiredField(t *testing.T) {
	for name, tc := range map[string]struct {
		srv  *Server
		want bool
	}{
		"no password": {newTestServer(t, daemonToken), false},
		"password":    {serverWithPassword(t, nil), true},
	} {
		rec := httptest.NewRecorder()
		tc.srv.Handler().ServeHTTP(rec, loopbackReq("GET", "/healthz", nil))
		got, ok := decode(t, rec)["authRequired"].(bool)
		if !ok || got != tc.want {
			t.Errorf("%s: authRequired = %v (present=%v), want %v", name, got, ok, tc.want)
		}
	}
}

// --- startup policy -----------------------------------------------------

func TestServeRefusesNonLoopbackWithoutPassword(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:4648", ":4648", "192.168.1.10:4648", "[::]:4648", "nas.lan:4648"} {
		cfg := config.Default()
		cfg.Server.Listen = listen
		if _, err := newDirectAuth(cfg.Server, daemonToken); err == nil || !strings.Contains(err.Error(), "smind set-password") {
			t.Errorf("listen %q without password: err = %v, want a refusal naming smind set-password", listen, err)
		}
		cfg.Server.PasswordHash = passwordHash(t)
		if _, err := newDirectAuth(cfg.Server, daemonToken); err != nil {
			t.Errorf("listen %q with a password: %v", listen, err)
		}
	}
}

func TestServeLoopbackDefaultUnchanged(t *testing.T) {
	srv := newTestServer(t, daemonToken)
	if srv.Addr() != "127.0.0.1:4648" || len(srv.Binds()) != 1 || srv.Binds()[0].Network != "tcp4" {
		t.Fatalf("Addr = %s, Binds = %v", srv.Addr(), srv.Binds())
	}
	// No password: login is off, healthz says so, the token still reaches loopback.
	rec := login(srv.Handler(), loopbackReq("POST", "/api/login", nil), "anything at all")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("login without a password configured: status %d, want 403", rec.Code)
	}
}

func TestListenCompanionKeepsLoopbackReachable(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PasswordHash = passwordHash(t)
	cfg.Server.Listen = "192.168.1.10:5000"
	srv := newTestServerCfg(t, daemonToken, cfg)
	if got := srv.Binds(); len(got) != 2 || got[0].Addr != "192.168.1.10:5000" || got[0].Network != "tcp4" || got[1].Addr != "127.0.0.1:5000" {
		t.Fatalf("Binds = %v", got)
	}
	cfg.Server.Listen = "0.0.0.0:5000"
	// 0.0.0.0 means IPv4 only (not Go's dual-stack default) and already covers loopback.
	if got := newTestServerCfg(t, daemonToken, cfg).Binds(); len(got) != 1 || got[0].Network != "tcp4" {
		t.Fatalf("0.0.0.0 binds %v", got)
	}
	for listen, want := range map[string][]config.Bind{
		"[::]:5000":      {{Network: "tcp", Addr: "[::]:5000"}},
		":5000":          {{Network: "tcp", Addr: ":5000"}},
		"localhost:5000": {{Network: "tcp4", Addr: "127.0.0.1:5000"}},
		"[::1]:5000":     {{Network: "tcp6", Addr: "[::1]:5000"}, {Network: "tcp4", Addr: "127.0.0.1:5000"}},
		"127.0.0.2:5000": {{Network: "tcp4", Addr: "127.0.0.2:5000"}, {Network: "tcp4", Addr: "127.0.0.1:5000"}},
	} {
		cfg.Server.Listen = listen
		got, err := cfg.Server.Binds()
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("listen %s: binds %v (%v), want %v", listen, got, err, want)
		}
	}
}

func TestWeakOrBrokenPasswordMaterialRefused(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PasswordPlain = "short"
	if _, err := newDirectAuth(cfg.Server, daemonToken); err == nil || !strings.Contains(err.Error(), "SMIND_PASSWORD") {
		t.Errorf("short SMIND_PASSWORD: %v", err)
	}
	cfg = config.Default()
	cfg.Server.PasswordHash = "plaintext-in-the-hash-field"
	if _, err := newDirectAuth(cfg.Server, daemonToken); err == nil || !strings.Contains(err.Error(), "bcrypt") {
		t.Errorf("garbage hash: %v", err)
	}
}

func TestEnvPasswordIsHashedInMemory(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PasswordPlain = testPassword
	cfg.Server.Listen = "0.0.0.0:4648"
	d, err := newDirectAuth(cfg.Server, daemonToken)
	if err != nil {
		t.Fatal(err)
	}
	if d.hash == "" || d.hash == testPassword || !auth.CheckPassword(d.hash, testPassword) {
		t.Fatalf("hash = %q", d.hash)
	}
}

// --- login --------------------------------------------------------------

func TestLoginReturnsSessionNotDaemonToken(t *testing.T) {
	srv := serverWithPassword(t, nil)
	rec := login(srv.Handler(), remoteReq("POST", "/api/login", nil), testPassword)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	tok, _ := body["token"].(string)
	if tok == "" || tok == daemonToken || strings.Contains(rec.Body.String(), daemonToken) {
		t.Fatalf("login returned %v; it must be a fresh session token, not the daemon token", body)
	}
	if len(tok) < 43 { // 256 bits, base64url
		t.Fatalf("session token %q is shorter than 256 bits", tok)
	}
	exp, err := time.Parse(time.RFC3339, body["expiresAt"].(string))
	if err != nil || time.Until(exp) < 23*time.Hour || time.Until(exp) > 25*time.Hour {
		t.Fatalf("expiresAt = %v (%v)", body["expiresAt"], err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("login responses must not be cacheable")
	}
	// Two logins, two different tokens.
	if tok2 := decode(t, login(srv.Handler(), remoteReq("POST", "/api/login", nil), testPassword))["token"]; tok2 == tok {
		t.Error("two logins returned the same session token")
	}
}

func TestLoginWrongPasswordConstantTime401(t *testing.T) {
	srv := serverWithPassword(t, nil)
	// Wrong passwords of every shape get the same answer and body. bcrypt
	// compares in constant time with respect to the password; the one length
	// shortcut is that a guess over bcrypt's 72-byte limit cannot match and is
	// rejected without hashing, which reveals only that it was over-long.
	var bodies []string
	for _, pw := range []string{"", "x", testPassword + "!", testPassword[:len(testPassword)-1], strings.ToUpper(testPassword), strings.Repeat("a", 100)} {
		r := remoteReq("POST", "/api/login", nil)
		r.RemoteAddr = "192.168.1." + string(rune('0'+len(bodies)+1)) + ":1"
		rec := login(srv.Handler(), r, pw)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("password %.10q: status %d, want 401", pw, rec.Code)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("response bodies differ by password shape: %q vs %q", b, bodies[0])
		}
	}
}

func TestLoginRateLimitBackoff429(t *testing.T) {
	srv := serverWithPassword(t, nil)
	now := time.Unix(1_800_000_000, 0)
	srv.direct.limiter = auth.NewLoginLimiter(func() time.Time { return now })
	h := srv.Handler()

	// Five wrong passwords are free...
	for i := 0; i < 5; i++ {
		if rec := login(h, remoteReq("POST", "/api/login", nil), "wrong password "); rec.Code != 401 {
			t.Fatalf("attempt %d: status %d", i, rec.Code)
		}
	}
	// ...the sixth fails and the address is then locked out, even for the right password.
	if rec := login(h, remoteReq("POST", "/api/login", nil), "wrong password "); rec.Code != 401 {
		t.Fatalf("sixth attempt: status %d", rec.Code)
	}
	rec := login(h, remoteReq("POST", "/api/login", nil), testPassword)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked out: status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if body := decode(t, rec); body["retryAfterSeconds"] == nil {
		t.Errorf("429 body = %v, want retryAfterSeconds", body)
	}
	// Another address is unaffected.
	other := remoteReq("POST", "/api/login", nil)
	other.RemoteAddr = "192.168.1.77:9"
	if rec := login(h, other, testPassword); rec.Code != 200 {
		t.Fatalf("a different client was locked out: %d", rec.Code)
	}
	// After the delay the right password works and clears the history.
	now = now.Add(3 * time.Second)
	if rec := login(h, remoteReq("POST", "/api/login", nil), testPassword); rec.Code != 200 {
		t.Fatalf("after the delay: status %d", rec.Code)
	}
}

func TestLoginRejectsCrossSiteAndMalformedRequests(t *testing.T) {
	srv := serverWithPassword(t, nil)
	h := srv.Handler()

	r := remoteReq("POST", "/api/login", nil)
	r.Header.Set("Origin", "http://evil.example")
	if rec := login(h, r, testPassword); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin login: status %d, want 403", rec.Code)
	}

	r = remoteReq("POST", "/api/login", []byte(`{"password":"`+testPassword+`"}`))
	r.Header.Set("Content-Type", "text/plain") // what a cross-site form post can send
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-JSON login: status %d, want 403", rec.Code)
	}

	r = remoteReq("POST", "/api/login", []byte(`not json`))
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status %d, want 400", rec.Code)
	}

	// GET is not a login (it falls through to the web UI, never a token).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, remoteReq("GET", "/api/login", nil))
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Error("GET /api/login returned a token")
	}

	r = remoteReq("POST", "/api/login", bytes.Repeat([]byte("x"), maxLoginBody+10))
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: status %d, want 413", rec.Code)
	}
}

func TestSessionTokenExpires(t *testing.T) {
	srv := serverWithPassword(t, nil)
	now := time.Unix(1_800_000_000, 0)
	srv.direct.sessions = auth.NewSessionStore(time.Hour, func() time.Time { return now })
	tok := decode(t, login(srv.Handler(), remoteReq("POST", "/api/login", nil), testPassword))["token"].(string)

	if !srv.direct.sessions.Valid(tok) {
		t.Fatal("fresh session rejected")
	}
	now = now.Add(59 * time.Minute)
	if !srv.direct.sessions.Valid(tok) {
		t.Fatal("session expired early")
	}
	now = now.Add(2 * time.Minute)
	if srv.direct.sessions.Valid(tok) {
		t.Fatal("session still valid after its expiry")
	}
	if srv.direct.sessions.Len() != 0 {
		t.Error("an expired session should be dropped on lookup")
	}
}

// --- /ws ----------------------------------------------------------------

// wsServer serves srv's handler; fromLAN rewrites the TCP peer so the
// request looks like it came from another machine.
func wsServer(t *testing.T, srv *Server, fromLAN bool) *httptest.Server {
	t.Helper()
	h := srv.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fromLAN {
			r.RemoteAddr = "192.168.1.50:41000"
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func dialWS(ts *httptest.Server, query string, protocols []string, header http.Header) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{Subprotocols: protocols}
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws" + query
	return d.Dial(u, header)
}

func wsCall(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.WriteJSON(map[string]any{"id": "1", "method": "workspace.list"}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp struct {
		ID    string `json:"id"`
		Error *struct{ Message string }
	}
	if err := c.ReadJSON(&resp); err != nil || resp.ID != "1" || resp.Error != nil {
		t.Fatalf("workspace.list over the socket: %+v, %v", resp, err)
	}
}

func sessionToken(t *testing.T, srv *Server) string {
	t.Helper()
	rec := login(srv.Handler(), remoteReq("POST", "/api/login", nil), testPassword)
	tok, _ := decode(t, rec)["token"].(string)
	if tok == "" {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body)
	}
	return tok
}

func TestWSAcceptsSubprotocolBearer(t *testing.T) {
	srv := serverWithPassword(t, nil)
	tok := sessionToken(t, srv)
	ts := wsServer(t, srv, true) // from another machine
	c, resp, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, nil)
	if err != nil {
		t.Fatalf("dial: %v (status %v)", err, resp)
	}
	defer func() { _ = c.Close() }()
	if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != "smind" {
		t.Fatalf("negotiated subprotocol = %q; the server must answer \"smind\", never echo the token", got)
	}
	wsCall(t, c)
}

func TestWSAcceptsAuthorizationHeaderSession(t *testing.T) {
	srv := serverWithPassword(t, nil)
	tok := sessionToken(t, srv)
	ts := wsServer(t, srv, true)
	c, _, err := dialWS(ts, "", nil, http.Header{"Authorization": {"Bearer " + tok}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	wsCall(t, c)
}

func TestWSRejectsSessionTokenInQueryForNonLoopback(t *testing.T) {
	srv := serverWithPassword(t, nil)
	tok := sessionToken(t, srv)
	for _, lan := range []bool{true, false} {
		ts := wsServer(t, srv, lan)
		if _, resp, err := dialWS(ts, "?token="+tok, nil, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("lan=%v: a session token in the URL must be refused, got err=%v resp=%v", lan, err, resp)
		}
	}
}

func TestWSRejectsDaemonTokenFromNonLoopback(t *testing.T) {
	srv := serverWithPassword(t, nil)
	ts := wsServer(t, srv, true)
	if _, resp, err := dialWS(ts, "?token="+daemonToken, nil, nil); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the standing daemon token must not work from another machine: %v %v", err, resp)
	}
	// ...nor as a bearer.
	if _, _, err := dialWS(ts, "", []string{"smind", "smind.bearer." + daemonToken}, nil); err == nil {
		t.Fatal("the daemon token was accepted as a bearer from another machine")
	}
}

func TestLoopbackQueryTokenStillWorks(t *testing.T) {
	for name, srv := range map[string]*Server{"no password": newTestServer(t, daemonToken), "with password": serverWithPassword(t, nil)} {
		ts := wsServer(t, srv, false)
		c, _, err := dialWS(ts, "?token="+daemonToken, nil, nil)
		if err != nil {
			t.Fatalf("%s: loopback dial with the daemon token: %v", name, err)
		}
		wsCall(t, c)
		_ = c.Close()
		if _, resp, err := dialWS(ts, "?token=wrong", nil, nil); err == nil || resp.StatusCode != 401 {
			t.Errorf("%s: wrong token: %v %v", name, err, resp)
		}
	}
}

func TestWSRejectsExpiredAndUnknownSessions(t *testing.T) {
	srv := serverWithPassword(t, nil)
	now := time.Unix(1_800_000_000, 0)
	srv.direct.sessions = auth.NewSessionStore(time.Minute, func() time.Time { return now })
	tok := sessionToken(t, srv)
	ts := wsServer(t, srv, true)
	if c, _, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, nil); err != nil {
		t.Fatalf("live session: %v", err)
	} else {
		_ = c.Close()
	}
	now = now.Add(2 * time.Minute)
	if _, resp, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, nil); err == nil || resp.StatusCode != 401 {
		t.Fatalf("an expired session must be refused: %v %v", err, resp)
	}
	if _, _, err := dialWS(ts, "", []string{"smind", "smind.bearer.nope"}, nil); err == nil {
		t.Fatal("an unknown session token was accepted")
	}
}

func TestCheckOriginSameOriginOnly(t *testing.T) {
	srv := serverWithPassword(t, nil)
	tok := sessionToken(t, srv)
	ts := wsServer(t, srv, true)
	host := strings.TrimPrefix(ts.URL, "http://")
	// A page from another origin cannot open the socket even holding a valid token.
	if _, resp, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, http.Header{"Origin": {"http://evil.example"}}); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin upgrade: %v %v", err, resp)
	}
	if c, _, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, http.Header{"Origin": {"http://" + host}}); err != nil {
		t.Fatalf("same-origin upgrade: %v", err)
	} else {
		_ = c.Close()
	}
}

func TestSessionTokenOnlyOpensWebSocket(t *testing.T) {
	// A session is for the UI's socket. The proxy endpoints (provider
	// credentials downstream) and /api/shutdown stay behind the daemon token.
	srv := serverWithPassword(t, nil)
	tok := sessionToken(t, srv)
	h := srv.Handler()
	for _, path := range []string{"/api/shutdown", "/v1/messages", "/v1/chat/completions"} {
		r := remoteReq("POST", path, nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s with a session token: status %d, want 401", path, rec.Code)
		}
	}
}

// --- secrets in logs ------------------------------------------------------

func TestNoSecretInLogs(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	cfg := config.Default()
	cfg.Server.PasswordPlain = testPassword
	cfg.Server.Listen = "0.0.0.0:4648"
	srv := newTestServerCfg(t, daemonToken, cfg)
	h := srv.Handler()
	rec := login(h, remoteReq("POST", "/api/login", nil), testPassword)
	tok, _ := decode(t, rec)["token"].(string)
	login(h, remoteReq("POST", "/api/login", nil), "a wrong password!!")
	r := remoteReq("GET", "/healthz", nil)
	r.Host = "evil.example.com"
	h.ServeHTTP(httptest.NewRecorder(), r)
	ts := wsServer(t, srv, true)
	if c, _, err := dialWS(ts, "", []string{"smind", "smind.bearer." + tok}, nil); err == nil {
		_ = c.Close()
	}

	for _, secret := range []string{testPassword, "a wrong password!!", tok, daemonToken, srv.direct.hash} {
		if secret != "" && strings.Contains(buf.String(), secret) {
			t.Errorf("a secret leaked into the log: %q in %q", secret, buf.String())
		}
	}
}

// Clients that open login requests and never send the body must not hold the
// bcrypt slots real logins need (slots are taken only after the body is read).
func TestLoginSlowBodiesDoNotStarveRealLogins(t *testing.T) {
	srv := serverWithPassword(t, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	var hogs []net.Conn
	for i := 0; i < 8; i++ { // twice the slot count
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		hogs = append(hogs, c)
		fmt.Fprintf(c, "POST /api/login HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 4000\r\n\r\n", addr)
	}
	defer func() {
		for _, c := range hogs {
			_ = c.Close()
		}
	}()
	time.Sleep(200 * time.Millisecond)

	body, _ := json.Marshal(map[string]string{"password": testPassword})
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a real login behind 8 stalled bodies: status %d, want 200", resp.StatusCode)
	}
}
