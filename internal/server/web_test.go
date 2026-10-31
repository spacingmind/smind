package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPDFViewerCSPHeader: the sandboxed PDF viewer's document carries its own
// Content-Security-Policy (ADR-0049 section 3.3) and no other path does.
func TestPDFViewerCSPHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	h := withViewerCSP(next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pdf-viewer.html", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if csp != pdfViewerCSP {
		t.Fatalf("CSP = %q, want %q", csp, pdfViewerCSP)
	}
	// The directives that make the viewer inert, by name.
	for _, want := range []string{
		"default-src 'none'", "script-src 'self'", "connect-src 'none'", "frame-ancestors 'self'",
		"worker-src blob: 'self'", "img-src blob: data:",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	// Nothing that widens it.
	for _, bad := range []string{"unsafe-eval", "script-src *", "connect-src *", "http:", "https:"} {
		if strings.Contains(csp, bad) {
			t.Errorf("CSP must not contain %q: %s", bad, csp)
		}
	}
	if scripts := cspDirective(csp, "script-src"); strings.Contains(scripts, "unsafe-inline") {
		t.Errorf("script-src allows inline script: %q", scripts)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("handler was not called")
	}

	// Every other path, including a look-alike, is untouched.
	for _, p := range []string{"/", "/index.html", "/assets/app.js", "/pdf-viewer.js", "/pdf-viewer.html/", "/x/pdf-viewer.html"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != "" {
			t.Errorf("%s: unexpected CSP %q", p, got)
		}
	}
}

func cspDirective(csp, name string) string {
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, name+" ") || d == name {
			return d
		}
	}
	return ""
}
