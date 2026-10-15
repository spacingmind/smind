// Package server exposes the smind HTTP API and embedded web UI.
package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/version"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// Server serves the smind API and UI.
type Server struct {
	cfg       config.Config
	proxy     *proxy
	ws        http.Handler
	api       *wsapi.API
	runs      *runs.Registry
	terminals *terminal.Registry
	token     string
	direct    *directAuth

	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// Option configures New.
type Option func(*options)

type options struct {
	observer quota.Observer
}

// WithQuotaObserver reports every Anthropic OAuth upstream response the proxy
// receives to o (ADR-0028 §7). Nil is a no-op.
func WithQuotaObserver(o quota.Observer) Option {
	return func(opts *options) { opts.observer = o }
}

// New builds a Server from config, backed by reg/router for the proxy
// endpoints and wm/runner/db for the /ws workspace/space/task/run/terminal
// API (db backs runs.Registry's persistence -- see wsapi.New). token gates
// the proxy endpoints and the /ws upgrade; see Handler.
func New(cfg config.Config, reg *accounts.Registry, router *routing.Router, wm *workspace.Manager, runner *taskrunner.Runner, db *store.Store, token string, opts ...Option) (*Server, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	direct, err := newDirectAuth(cfg.Server, token)
	if err != nil {
		return nil, err
	}
	api, err := wsapi.New(wm, reg, runner, db, token)
	if err != nil {
		return nil, fmt.Errorf("server: new: %w", err)
	}
	s := &Server{
		cfg:       cfg,
		proxy:     newProxy(reg, router, db, withObserver(o.observer)),
		ws:        api.Handler,
		api:       api,
		runs:      api.Runs,
		terminals: api.Terminals,
		token:     token,
		direct:    direct,

		shutdownCh: make(chan struct{}),
	}
	api.SetAuthorizer(s.authorizeWS)
	return s, nil
}

// API returns the underlying wsapi.API, for a caller that needs to bridge a
// non-WebSocket connection (a relay-carried mobile session, see
// internal/relay/bridge) into the exact same RPC dispatch table /ws serves
// -- see wsapi.API.ServeTransport.
func (s *Server) API() *wsapi.API {
	return s.api
}

// ShutdownSignal returns a channel that is closed exactly once, when
// POST /api/shutdown accepts a shutdown request (see handleShutdown).
// cmdServe selects on it next to its signal context, so the API and
// SIGINT/SIGTERM drive one shared shutdown code path (ADR-0029 D2).
func (s *Server) ShutdownSignal() <-chan struct{} {
	return s.shutdownCh
}

// Close releases daemon-owned resources that live outside the
// http.Server itself: killing every still-running agent Run (see
// internal/runs.Registry.CloseAll) and terminal session's shell/PTY (see
// internal/terminal.Registry.CloseAll), so a graceful daemon shutdown
// (cmdServe, in cmd/smind/serve.go) doesn't leave either kind of
// subprocess orphaned behind, and draining the proxy's async request_log
// writer (see proxy.Close) so a row enqueued just before shutdown isn't
// silently lost. Safe to call once, after http.Server.Shutdown has
// stopped accepting new /ws connections and new proxy requests.
func (s *Server) Close() {
	s.runs.CloseAll()
	s.terminals.CloseAll()
	s.proxy.Close()
}

// Handler returns the root http handler.
//
// GET /healthz and the web UI (GET /, static assets) stay unauthenticated:
// the UI's own HTML/JS has to load before a user could enter a token
// anywhere in it, so gating it behind the same bearer check would make it
// impossible to use without already having the token through some other
// channel. The proxy endpoints carry real provider credentials downstream,
// so those require the token. /ws requires the token too, but checks it as
// a query parameter rather than through auth.RequireToken's Authorization
// header check -- a browser WebSocket client can't set request headers on
// the upgrade -- so it isn't wrapped in that middleware; see wsapi.Handler.
// POST /api/shutdown is token-gated exactly like the proxy routes
// (ADR-0029 D2): shutting the daemon down is as destructive as anything
// the token already authorizes.
// GET /api/token is served only to a true loopback caller (handleToken) and
// POST /api/login trades the password for an expiring session token that /ws
// accepts (ADR-0031 D8); both, like every other route, sit behind the Host
// allowlist (directAuth.guard).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/token", s.handleToken)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.Handle("POST /v1/messages", auth.RequireToken(s.token, http.HandlerFunc(s.proxy.handleAnthropic)))
	mux.Handle("POST /v1/messages/count_tokens", auth.RequireToken(s.token, http.HandlerFunc(s.proxy.handleAnthropicCountTokens)))
	mux.Handle("POST /v1/chat/completions", auth.RequireToken(s.token, http.HandlerFunc(s.proxy.handleOpenAI)))
	mux.Handle("POST /api/shutdown", auth.RequireToken(s.token, http.HandlerFunc(s.handleShutdown)))
	mux.Handle("GET /ws", s.ws)
	mux.Handle("/", webUI())
	return s.direct.guard(mux)
}

// Addr returns the configured listen address (127.0.0.1:4648 by default).
func (s *Server) Addr() string {
	host, port, err := s.cfg.Server.ListenHostPort()
	if err != nil { // newDirectAuth rejected it already; keep Addr total
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(config.DefaultPort))
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Binds is every listener the daemon opens (see config.ServerConfig.Binds):
// the configured address plus, when needed, 127.0.0.1 so local clients always
// reach the daemon.
func (s *Server) Binds() []config.Bind {
	b, _ := s.cfg.Server.Binds() // validated by newDirectAuth
	return b
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "smind",
		"version": version.Version,
		// Whether remote clients must log in (a password is configured).
		// Loopback clients never need to.
		"authRequired": s.direct.hash != "",
	})
}

// handleShutdown serves POST /api/shutdown (ADR-0029 D2): 202 Accepted,
// then the shutdown signal cmdServe selects on -- the same path SIGINT/
// SIGTERM take. The sync.Once makes repeat calls during shutdown
// idempotent: they still get their 202, and closing the channel twice
// (which would panic) is impossible. Only POST reaches this handler; the
// mux pattern routes every other method away from it, and
// auth.RequireToken has already rejected unauthenticated callers.
func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting-down"})
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
