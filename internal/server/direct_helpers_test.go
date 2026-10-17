package server

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// loopbackReq builds a request as a local client would send it: a loopback
// TCP peer and a loopback Host. httptest.NewRequest's defaults (Host
// example.com, peer 192.0.2.1) are what the Host allowlist and the
// loopback-only /api/token are meant to refuse.
func loopbackReq(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:4648"+target, body)
	r.RemoteAddr = "127.0.0.1:50000"
	return r
}
