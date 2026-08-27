// Package server exposes the smind HTTP API and embedded web UI.
package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// Server serves the smind API and UI.
type Server struct {
	cfg    config.Config
	proxy  *proxy
	wm     *workspace.Manager
	runner *taskrunner.Runner
	token  string
}

// New builds a Server from config, backed by reg/router for the proxy
// endpoints and wm/runner for the /ws workspace/space/task API. token gates
// the proxy endpoints and the /ws upgrade; see Handler.
func New(cfg config.Config, reg *accounts.Registry, router *routing.Router, wm *workspace.Manager, runner *taskrunner.Runner, token string) *Server {
	return &Server{
		cfg:    cfg,
		proxy:  newProxy(reg, router),
		wm:     wm,
		runner: runner,
		token:  token,
	}
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
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("POST /v1/messages", auth.RequireToken(s.token, http.HandlerFunc(s.proxy.handleAnthropic)))
	mux.Handle("POST /v1/chat/completions", auth.RequireToken(s.token, http.HandlerFunc(s.proxy.handleOpenAI)))
	mux.Handle("GET /ws", wsapi.Handler(s.wm, s.runner, s.token))
	mux.Handle("/", webUI())
	return mux
}

// Addr returns the listen address.
func (s *Server) Addr() string {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 4648
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "smind",
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
