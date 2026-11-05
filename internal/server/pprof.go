package server

import (
	"net/http"
	"net/http/pprof"

	"github.com/spacingmind/smind/internal/auth"
)

// pprofHandler serves Go's /debug/pprof/ for memory diagnosis. It is mounted
// only when debug.pprof / SMIND_PPROF is on, and every request must come from a
// true loopback peer (auth.IsTrueLoopback: TCP peer, Host header, no proxy
// headers) AND carry the daemon token. The peer is checked first, so a remote
// caller learns nothing about the token. A handler profile can show goroutine
// stacks and allocation sites, which is why it is off by default and never on
// /ws (so no run-scoped agent connection or relay session can reach it).
func (s *Server) pprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	gated := auth.RequireToken(s.token, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsTrueLoopback(r) {
			http.Error(w, "pprof is only served to a local client", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		gated.ServeHTTP(w, r)
	})
}
