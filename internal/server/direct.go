package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/wsapi"
)

// directAuth is the admission layer for clients that are not the local user
// (ADR-0031 D8): Host validation for every request, GET /api/token for true
// loopback callers only, and password login that mints expiring session
// tokens accepted by /ws. With no password configured (the default) there is
// no login and no sessions; Host validation and the loopback-only token still
// apply.
type directAuth struct {
	hash     string // bcrypt; "" means password login is not enabled
	sessions *auth.SessionStore
	limiter  *auth.LoginLimiter
	hosts    *auth.HostPolicy
	token    string // the daemon's standing token

	// bcryptSlots bounds concurrent password checks: each costs ~250 ms of
	// CPU and the limiter only learns about a failure after it happened.
	bcryptSlots chan struct{}
	lastHostLog atomic.Int64 // unix nanos
}

// errNonLoopbackNoPassword is the refusal `smind serve` prints.
func errNonLoopbackNoPassword(listen string) error {
	return fmt.Errorf("refusing to listen on %s: it is not a loopback address and no password is set. "+
		"Anyone who can reach it could run commands on this machine. Run `smind set-password` "+
		"(or set SMIND_PASSWORD, at least %d characters), or bind to 127.0.0.1", listen, auth.MinPasswordLength)
}

// newDirectAuth validates the server section and builds the admission layer.
// It fails (the daemon does not start) when the configuration would expose
// the daemon without a password, or the password material is unusable.
func newDirectAuth(sc config.ServerConfig, daemonToken string) (*directAuth, error) {
	host, port, err := sc.ListenHostPort()
	if err != nil {
		return nil, err
	}
	d := &directAuth{
		hosts:       auth.NewHostPolicy(sc.Hostnames),
		token:       daemonToken,
		bcryptSlots: make(chan struct{}, 4),
	}
	switch {
	case sc.PasswordPlain != "":
		// SMIND_PASSWORD: hashed in memory, never written anywhere.
		if err := auth.ValidatePassword(sc.PasswordPlain); err != nil {
			return nil, fmt.Errorf("%s: %w", config.EnvPassword, err)
		}
		if d.hash, err = auth.HashPassword(sc.PasswordPlain); err != nil {
			return nil, err
		}
	case sc.PasswordHash != "":
		if err := auth.ValidateHash(sc.PasswordHash); err != nil {
			return nil, err
		}
		d.hash = sc.PasswordHash
	}
	if !config.IsLoopbackHost(host) && d.hash == "" {
		return nil, errNonLoopbackNoPassword(net.JoinHostPort(host, fmt.Sprint(port)))
	}
	if d.hash != "" {
		d.sessions = auth.NewSessionStore(auth.SessionTTL, nil)
		d.limiter = auth.NewLoginLimiter(nil)
	}
	return d, nil
}

// guard rejects every request whose Host header is not allowed, before any
// handler (including /healthz, the UI and the /ws upgrade) runs. A DNS
// rebinding page keeps its own name in Host and therefore stops here.
func (d *directAuth) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !d.hosts.Allowed(r.Host) {
			d.logHostRejected(r.Host)
			http.Error(w, "Host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logHostRejected explains a rejection to the operator, at most once a
// minute (the Host value is attacker-controlled: quoted, never a log flood).
func (d *directAuth) logHostRejected(host string) {
	now := time.Now().UnixNano()
	last := d.lastHostLog.Load()
	if now-last < int64(time.Minute) || !d.lastHostLog.CompareAndSwap(last, now) {
		return
	}
	if len(host) > 120 {
		host = host[:120]
	}
	log.Printf("rejected a request with Host %q; to serve that name run `smind config set server.hostnames <name>` and restart", host)
}

// sameOrigin reports whether the request's Origin header, if any, names the
// request's own Host. Non-browser clients send no Origin and pass.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// handleToken serves the daemon's standing token to the bundled web UI.
//
// With a password configured it is never served, to anyone, loopback
// included (ADR-0031 D8, decided 2026-10-15): a request that looks local cannot
// be told from one relayed by a default same-host reverse proxy (nginx
// rewrites Host to the upstream and adds no header), so loopback is not proof
// of being the local user. The web UI signs in like every other client; the
// CLI reads the token file instead (wsclient sends it as a bearer).
//
// Without a password the daemon is loopback-only by default and the token is
// served to a true loopback caller: TCP peer loopback, a Host naming this
// machine, no proxy headers, no foreign Origin.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if s.direct.hash != "" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "a password is configured: the daemon token is not served; sign in with POST /api/login",
		})
		return
	}
	if !auth.IsTrueLoopback(r) || !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "the daemon token is only served to a local client; log in with POST /api/login",
		})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": s.token})
}

// maxLoginBody caps the login request: a password is at most 72 bytes.
const maxLoginBody = 4096

type loginRequest struct {
	Password string `json:"password"`
}

// handleLogin implements POST /api/login: the password is checked against the
// bcrypt hash and, on success, an expiring session token is returned. That
// token (never the daemon token) is what /ws then accepts.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	d := s.direct
	w.Header().Set("Cache-Control", "no-store")
	if d.hash == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "password login is not enabled on this daemon"})
		return
	}
	if !sameOrigin(r) || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		// application/json cannot be sent cross-site without a CORS preflight,
		// which this server never grants.
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "login must be a same-origin application/json request"})
		return
	}
	ip := auth.PeerIP(r)
	if ok, wait := d.limiter.Allow(ip); !ok {
		tooMany(w, wait)
		return
	}
	// Read the (tiny) body before taking a bcrypt slot, under a short deadline:
	// a client that opens connections and never sends the body must not hold
	// the slots that real logins need.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(5 * time.Second))
	var req loginRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLoginBody+1))
	if len(body) > maxLoginBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request too large"})
		return
	}
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected {\"password\": \"...\"}"})
		return
	}
	select {
	case d.bcryptSlots <- struct{}{}:
		defer func() { <-d.bcryptSlots }()
	default:
		tooMany(w, time.Second)
		return
	}
	if !auth.CheckPassword(d.hash, req.Password) {
		d.limiter.Fail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}
	d.limiter.Success(ip)
	token, expires := d.sessions.Issue()
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expiresAt": expires.UTC().Format(time.RFC3339)})
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", fmt.Sprint(secs))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts", "retryAfterSeconds": secs})
}

// authorizeWS is the /ws admission rule (wsapi.API.SetAuthorizer):
//   - a live session token, offered as the "smind.bearer.<token>" subprotocol
//     or an Authorization: Bearer header, from any client; never from the URL;
//   - otherwise the daemon token (which only someone with the token file has),
//     from a true loopback caller: in the Authorization header, and, only while
//     no password is configured, in the "token" query parameter that existing
//     loopback clients use. With a password set the query form is refused even
//     on loopback, so the credential a local client holds never rides in a URL
//     that a proxy or log could capture.
func (s *Server) authorizeWS(r *http.Request) bool {
	d := s.direct
	if d.sessions != nil {
		for _, tok := range bearerTokens(r) {
			if d.sessions.Valid(tok) {
				return true
			}
		}
	}
	if !auth.IsTrueLoopback(r) {
		return false
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") &&
		subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(d.token)) == 1 {
		return true
	}
	if d.hash != "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(d.token)) == 1
}

// RelayPasswordGate adapts the daemon password to the relay hub: relay devices
// must present it in their device_hello (ADR-0031 P13c). The password is fixed
// when the daemon starts (changing it needs a restart, which ends every relay
// session too), and Check is the same bcrypt check POST /api/login uses.
func (s *Server) RelayPasswordGate() relayPasswordGate { return relayPasswordGate{s.direct} }

type relayPasswordGate struct{ d *directAuth }

func (g relayPasswordGate) Required() bool { return g.d.hash != "" }

func (g relayPasswordGate) Check(password string) bool { return auth.CheckPassword(g.d.hash, password) }

// bearerTokens lists the tokens a request presents in headers: the
// Authorization bearer and any "smind.bearer.*" offered subprotocol.
func bearerTokens(r *http.Request) []string {
	var out []string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		out = append(out, strings.TrimPrefix(h, "Bearer "))
	}
	for _, v := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(v, ",") {
			if tok, ok := strings.CutPrefix(strings.TrimSpace(p), wsapi.BearerSubprotocolPrefix); ok {
				out = append(out, tok)
			}
		}
	}
	return out
}
