package accounts

import (
	"context"
	"crypto/subtle"
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

const (
	// callbackReadHeaderTimeout bounds how long a client may dribble request
	// headers at the listener (slowloris from another local process).
	callbackReadHeaderTimeout = 10 * time.Second
	// callbackShutdownTimeout bounds close's graceful shutdown.
	callbackShutdownTimeout = 2 * time.Second
)

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
	// state is the login's expected state; only a request carrying it is
	// delivered on result.
	state string
}

// newCallbackServer binds addr synchronously and starts serving path on it
// in the background, returning once the bind itself has succeeded (as
// opposed to merely having been requested) so a caller can be sure the
// vendor's redirect has somewhere to land before it hands the authorize URL
// to a browser -- binding after that point would leave a window where the
// vendor could redirect before anything is listening. providerLabel is
// purely cosmetic: it's what the browser-facing success page names (see
// oauth_callback_page.go), not used for any routing/matching decision.
//
// state is the login's expected state: the listener is reachable by every
// local process, so only a request carrying that state is delivered; any other
// request (a scanner, a favicon fetch) gets an error page and is ignored, and
// the login stays pending.
func newCallbackServer(addr, path, providerLabel, state string) (*callbackServer, error) {
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

	s := &callbackServer{listener: listener, result: make(chan callbackResult, 1), providerLabel: providerLabel, state: state}

	mux := http.NewServeMux()
	mux.HandleFunc(path, s.handleCallback)
	s.httpServer = &http.Server{Handler: mux, ReadHeaderTimeout: callbackReadHeaderTimeout}

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

// handleCallback delivers the vendor's redirect on result, then tells the
// browser it's done -- but only when the request carries the login's state
// (constant-time compared), for the success form and the error= form alike. A
// request with a missing or wrong state is answered with a 400 and ignored.
// Only the first matching request is captured (result is buffered size 1).
func (s *callbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res := callbackResult{
		code:    q.Get("code"),
		state:   q.Get("state"),
		errCode: q.Get("error"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.state == "" || subtle.ConstantTimeCompare([]byte(res.state), []byte(s.state)) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, renderCallbackErrorPage("this request does not belong to the login in progress"))
		return
	}

	select {
	case s.result <- res:
	default:
	}

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

// close shuts the server down, releasing its bound port: a short graceful
// Shutdown, then a hard Close so a stuck connection cannot hold the port (or
// this call) open. Safe to call exactly once per callbackServer;
// LoginCoordinator.Login calls it from a defer so every exit path (success,
// timeout, ctx cancellation) tears the listener down the same way.
func (s *callbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
	defer cancel()
	_ = s.httpServer.Shutdown(ctx)
	_ = s.httpServer.Close()
}
