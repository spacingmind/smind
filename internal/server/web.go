// Package server serves the embedded web UI when a build is present.
package server

import (
	"embed"
	"io/fs"
	"net/http"
)

// webDist holds the built web UI. The directory is populated by
// `task build:web`; an empty tree keeps `go build` working before
// the first web build.
//
//go:embed all:dist
var webDist embed.FS

// webUI returns a handler serving the embedded web build, falling back
// to a placeholder when dist is empty.
func webUI() http.Handler {
	sub, err := fs.Sub(webDist, "dist")
	if err != nil {
		return http.HandlerFunc(placeholder)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return http.HandlerFunc(placeholder)
	}
	return http.FileServer(http.FS(sub))
}

func placeholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><title>smind</title></head>
<body style="font-family:system-ui;padding:3rem;color:#eee;background:#111">
<h1>smind</h1>
<p>Web UI not built yet. Run <code>task build:web</code> and rebuild the binary.</p>
</body></html>`))
}
