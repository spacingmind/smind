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
	return withViewerCSP(http.FileServer(http.FS(sub)))
}

// pdfViewerPath is the sandboxed PDF viewer's document (office mode, ADR-0049
// section 3.3). The app embeds it in an <iframe sandbox="allow-scripts">, so it
// runs in an opaque origin; its own policy forbids every connection and every
// script that is not its own file, and only the app itself may frame it.
const pdfViewerPath = "/pdf-viewer.html"

// pdfViewerCSP is the Content-Security-Policy header of pdfViewerPath.
const pdfViewerCSP = "default-src 'none'; script-src 'self'; worker-src blob: 'self'; img-src blob: data:; " +
	"style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'none'; frame-ancestors 'self'"

// withViewerCSP adds the viewer's policy to the response for pdfViewerPath and
// leaves every other path untouched.
func withViewerCSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pdfViewerPath {
			h := w.Header()
			h.Set("Content-Security-Policy", pdfViewerCSP)
			h.Set("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(w, r)
	})
}

func placeholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><title>sMind</title></head>
<body style="font-family:system-ui;padding:3rem;color:#eee;background:#111">
<h1>sMind</h1>
<p>Web UI not built yet. Run <code>task build:web</code> and rebuild the binary.</p>
</body></html>`))
}
