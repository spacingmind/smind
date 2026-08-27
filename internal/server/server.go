// Package server exposes the smind HTTP API and embedded web UI.
package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/spacingmind/smind/internal/config"
)

// Server serves the smind API and UI.
type Server struct {
	cfg config.Config
}

// New builds a Server from config.
func New(cfg config.Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns the root http handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
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
