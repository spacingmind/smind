package accounts

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// checkLoopbackAddr rejects any listen address that is not loopback-only:
// the callback listener carries an authorization code, so it must never be
// reachable from the network (ADR-0033 D1). Only "localhost" or a loopback IP
// literal passes; an empty host, 0.0.0.0, :: and every other address are
// refused.
func checkLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("oauth callback address %q is not host:port: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("oauth callback address %q is not loopback-only", addr)
}

// callbackResult is what the vendor's redirect to a callbackServer
// delivered: either code+state on success, or a non-empty errCode
// describing what went wrong (e.g. the user denied consent).
type callbackResult struct {
	code    string
	state   string
	errCode string
}

// checkBoundLoopback verifies the address a listener actually bound is
// loopback. checkLoopbackAddr only judges the requested string; a name such as
// "localhost" is resolved by the OS, and a hosts file or resolver could point
// it at a network-reachable address.
func checkBoundLoopback(l net.Listener) error {
	tcp, ok := l.Addr().(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		return fmt.Errorf("oauth callback listener bound to %s, which is not loopback-only", l.Addr())
	}
	return nil
}

// callbackServer is a temporary local HTTP listener that serves exactly one
// OAuth callback request on addr+path, then reports it on result. Bind
// happens synchronously in newCallbackServer, before the caller is told an
// authorize URL is safe to hand to a browser -- see that function's doc
// comment for why that ordering matters. The server is always torn down via
// close, on every exit path (success, timeout, or the caller giving up).
type callbackServer struct {
	httpServer    *http.Server
	listener      net.Listener
	result        chan callbackResult
	providerLabel string
}

// newCallbackServer binds addr synchronously and starts serving path on it
// in the background, returning once the bind itself has succeeded (as
// opposed to merely having been requested) so a caller can be sure the
// vendor's redirect has somewhere to land before it hands the authorize URL
// to a browser -- binding after that point would leave a window where the
// vendor could redirect before anything is listening. providerLabel is
// purely cosmetic: it's what the browser-facing success page names (see
// oauth_callback_page.go), not used for any routing/matching decision.
func newCallbackServer(addr, path, providerLabel string) (*callbackServer, error) {
	if err := checkLoopbackAddr(addr); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("bind oauth callback listener on %s: %w", addr, err)
	}
	if err := checkBoundLoopback(listener); err != nil {
		_ = listener.Close()
		return nil, err
	}

	s := &callbackServer{listener: listener, result: make(chan callbackResult, 1), providerLabel: providerLabel}

	mux := http.NewServeMux()
	mux.HandleFunc(path, s.handleCallback)
	s.httpServer = &http.Server{Handler: mux}

	go func() {
		// http.ErrServerClosed is the expected outcome of close's Shutdown
		// call -- every other error would mean the listener died from
		// under a still-in-flight login, which has nowhere useful to go
		// but is not this goroutine's job to report (the caller's own
		// wait times out instead).
		_ = s.httpServer.Serve(s.listener)
	}()

	return s, nil
}

// handleCallback captures the vendor's redirect query params and reports
// them on result, then tells the browser it's done. Only the first request
// is captured (result is buffered size 1); a comment isn't needed on the
// non-blocking send below since exactly one request is ever expected per
// flow.
func (s *callbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res := callbackResult{
		code:    q.Get("code"),
		state:   q.Get("state"),
		errCode: q.Get("error"),
	}

	select {
	case s.result <- res:
	default:
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if res.errCode != "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, renderCallbackErrorPage(res.errCode))
		return
	}
	if res.code == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, renderCallbackErrorPage("no authorization code was returned"))
		return
	}
	fmt.Fprint(w, renderCallbackSuccessPage(s.providerLabel))
}

// close gracefully shuts the server down, releasing its bound port. Safe to
// call exactly once per callbackServer; LoginCoordinator.Login calls it
// from a defer so every exit path (success, timeout, ctx cancellation)
// tears the listener down the same way.
func (s *callbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.httpServer.Shutdown(ctx)
}
