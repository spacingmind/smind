// Package transport provides an http.RoundTripper that fingerprints outbound
// TLS handshakes via uTLS, so requests smind's proxy makes to LLM providers
// don't present Go's stdlib crypto/tls ClientHello on the wire.
package transport

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	clientTimeout    = 60 * time.Second
)

// RoundTripper is an http.RoundTripper that dials with a uTLS ClientHello
// fingerprint instead of Go's stdlib crypto/tls, then proceeds with a normal
// HTTP/1.1 request over that connection.
//
// HTTP/2 is not supported. Chrome/Firefox ClientHello fingerprints normally
// advertise ALPN "h2" ahead of "http/1.1", and a server that supports h2
// will pick it — but this RoundTripper has no HTTP/2 codec (wiring one up
// needs golang.org/x/net/http2.ConfigureTransport against the uTLS conn,
// which this package does not do). Rather than silently breaking against
// any h2-capable server, dialTLS rewrites the fingerprint's ALPN extension
// to offer only "http/1.1" before the handshake, so the connection always
// negotiates HTTP/1.1.
type RoundTripper struct {
	helloID utls.ClientHelloID
	rootCAs *x509.CertPool
	inner   *http.Transport

	mu        sync.Mutex
	state     utls.ConnectionState
	haveState bool
}

// Option configures a RoundTripper.
type Option func(*RoundTripper)

// WithRootCAs overrides the trust store used to verify server certificates.
// Intended for tests that need to trust a self-signed httptest server cert;
// production callers should leave this unset to use the system trust store.
func WithRootCAs(pool *x509.CertPool) Option {
	return func(rt *RoundTripper) {
		rt.rootCAs = pool
	}
}

// New builds a RoundTripper that fingerprints outbound TLS handshakes as
// helloID (e.g. utls.HelloChrome_Auto).
func New(helloID utls.ClientHelloID, opts ...Option) *RoundTripper {
	rt := &RoundTripper{helloID: helloID}
	for _, opt := range opts {
		opt(rt)
	}
	rt.inner = &http.Transport{
		DialTLSContext:  rt.dialTLS,
		IdleConnTimeout: 90 * time.Second,
	}
	return rt
}

// Client builds an *http.Client backed by a uTLS RoundTripper, with a
// reasonable overall timeout for outbound provider requests.
func Client(helloID utls.ClientHelloID, opts ...Option) *http.Client {
	return &http.Client{
		Transport: New(helloID, opts...),
		Timeout:   clientTimeout,
	}
}

// RoundTrip implements http.RoundTripper.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.inner.RoundTrip(req)
}

// ConnectionState returns the uTLS connection state from the most recently
// completed handshake. It exists mainly so tests/diagnostics can confirm
// uTLS (not stdlib crypto/tls) negotiated the connection.
func (rt *RoundTripper) ConnectionState() (utls.ConnectionState, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.state, rt.haveState
}

func (rt *RoundTripper) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("transport: split host/port %q: %w", addr, err)
	}

	dialer := net.Dialer{Timeout: dialTimeout}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	uConn := utls.UClient(rawConn, &utls.Config{
		ServerName: host,
		RootCAs:    rt.rootCAs,
	}, rt.helloID)

	if err := restrictToHTTP1(uConn); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("transport: build uTLS ClientHello for %s: %w", addr, err)
	}

	if err := uConn.HandshakeContext(handshakeCtx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("transport: uTLS handshake with %s: %w", addr, err)
	}

	rt.mu.Lock()
	rt.state = uConn.ConnectionState()
	rt.haveState = true
	rt.mu.Unlock()

	return uConn, nil
}

// restrictToHTTP1 builds the ClientHello for uConn's fingerprint, then
// narrows its ALPN extension (if present) to offer only "http/1.1". Preset
// fingerprints (e.g. HelloChrome_Auto) otherwise hardcode ALPN to
// ["h2", "http/1.1"], which a real server would honor by switching to an
// HTTP/2 wire format this RoundTripper can't speak.
func restrictToHTTP1(uConn *utls.UConn) error {
	if err := uConn.BuildHandshakeState(); err != nil {
		return err
	}
	for _, ext := range uConn.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
	return nil
}
