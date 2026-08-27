// Package server exposes the smind HTTP API and embedded web UI.
package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/store"
)

// Server serves the smind API and UI.
type Server struct {
	cfg      config.Config
	store    *store.Store
	registry *accounts.Registry
	router   *routing.Router
	proxy    *proxy
}

// New builds a Server from config, backed by s/reg/router for the proxy
// endpoints.
func New(cfg config.Config, s *store.Store, reg *accounts.Registry, router *routing.Router) *Server {
	return &Server{
		cfg:      cfg,
		store:    s,
		registry: reg,
		router:   router,
		proxy:    newProxy(reg, router),
	}
}

// Handler returns the root http handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/messages", s.proxy.handleAnthropic)
	mux.HandleFunc("POST /v1/chat/completions", s.proxy.handleOpenAI)
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
