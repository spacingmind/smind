package server

import (
	"net/http"
	"runtime/debug"

	"github.com/spacingmind/smind/internal/crash"
)

// recoverPanics records a panic in any HTTP handler and then ends that one
// connection, which is what net/http itself does for a handler panic (the
// daemon stays up either way): it re-panics with http.ErrAbortHandler, the
// sentinel net/http treats as "abort quietly". The ResponseWriter is passed
// through untouched on purpose: wrapping it would hide the Hijacker and
// Flusher the WebSocket upgrade and the streaming proxy depend on.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v != http.ErrAbortHandler {
				crash.Default().Record("http", v, debug.Stack())
			}
			panic(http.ErrAbortHandler)
		}()
		next.ServeHTTP(w, r)
	})
}
