// Package transport provides an http.RoundTripper that fingerprints outbound
// TLS handshakes via uTLS, so requests smind's proxy makes to LLM providers
// don't present Go's stdlib crypto/tls ClientHello on the wire.
// Outbound proxies from the environment are honoured: http:// proxies only,
// HTTPS through CONNECT with the uTLS handshake kept intact, and loopback
// targets are never proxied.
package transport

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/spacingmind/smind/internal/netenv"
	"golang.org/x/net/http/httpproxy"
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

	proxyCfg *httpproxy.Config
	proxyFn  func(*url.URL) (*url.URL, error)
	dialHook func(ctx context.Context, network, addr string) (net.Conn, error)

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

// WithProxyConfig overrides the proxy configuration (HTTPS_PROXY, HTTP_PROXY,
// NO_PROXY and their lowercase forms) that New would otherwise read from the
// environment. Intended for tests; production callers leave it unset.
func WithProxyConfig(c *httpproxy.Config) Option {
	return func(rt *RoundTripper) {
		rt.proxyCfg = c
	}
}

// WithDirectDialer is a TEST ONLY option: it replaces the dial of a target (a
// TLS target dialled directly, and every dial of a plain-HTTP request, which
// includes the dial to an http proxy: a hook that redirects everything must let
// the proxy's address through). The dial to a proxy for an HTTPS tunnel always
// uses the transport's own dialer.
func WithDirectDialer(d func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(rt *RoundTripper) {
		rt.dialHook = d
	}
}

// New builds a RoundTripper that fingerprints outbound TLS handshakes as
// helloID (e.g. utls.HelloChrome_Auto).
func New(helloID utls.ClientHelloID, opts ...Option) *RoundTripper {
	rt := &RoundTripper{helloID: helloID}
	for _, opt := range opts {
		opt(rt)
	}
	if rt.proxyCfg == nil {
		rt.proxyCfg = httpproxy.FromEnvironment()
	}
	rt.proxyFn = rt.proxyCfg.ProxyFunc()
	rt.inner = &http.Transport{
		DialTLSContext:  rt.dialTLS,
		IdleConnTimeout: 90 * time.Second,
		Proxy:           rt.plainProxy,
	}
	if rt.dialHook != nil {
		rt.inner.DialContext = rt.dialHook
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

// StreamClient builds an *http.Client like Client but with no overall timeout:
// http.Client.Timeout covers reading the response body, so it would cut a
// long streaming response (SSE) that is still healthy. The caller bounds each
// request itself (time to first byte, idle time between chunks, or a total
// for a non-streaming call) with a request context. Dialing and the TLS
// handshake keep their own timeouts.
func StreamClient(helloID utls.ClientHelloID, opts ...Option) *http.Client {
	return &http.Client{Transport: New(helloID, opts...)}
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

// neverProxyHost reports whether host must never be sent through a proxy:
// localhost and any *.localhost name, loopback IPs and the unspecified
// address. The rule is applied before the proxy configuration and cannot be
// configured away, so a misconfigured proxy can never intercept local
// traffic.
func neverProxyHost(host string) bool {
	host = strings.ToLower(host)
	host = strings.TrimSuffix(host, ".")
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// proxyFor decides which proxy a target URL goes through, or nil for direct.
// A loopback target is always direct; a non-http proxy scheme is an error
// rather than a silent direct connection, so a configured proxy is never
// bypassed without the caller knowing.
func (rt *RoundTripper) proxyFor(u *url.URL) (*url.URL, error) {
	if neverProxyHost(u.Hostname()) {
		return nil, nil
	}
	p, err := rt.proxyFn(u)
	if err != nil {
		// err can embed the proxy URL, credentials included; never surface it.
		return nil, errors.New("transport: invalid proxy configuration")
	}
	if p == nil {
		return nil, nil
	}
	if p.Scheme != "http" {
		return nil, fmt.Errorf("transport: proxy scheme %q is not supported for provider calls", p.Scheme)
	}
	return p, nil
}

// plainProxy is the http.Transport Proxy hook: plain HTTP requests are sent
// through the proxy as absolute-URI requests (net/http adds the proxy
// credentials itself); HTTPS returns nil because dialTLS owns the CONNECT
// tunnel.
func (rt *RoundTripper) plainProxy(req *http.Request) (*url.URL, error) {
	if req.URL.Scheme == "https" {
		return nil, nil
	}
	p, err := rt.proxyFor(req.URL)
	if err != nil {
		return nil, err
	}
	if p != nil {
		slog.Debug("transport: via proxy",
			"proxy", netenv.RedactProxy(p.String()),
			"target", req.URL.Hostname())
	}
	return p, nil
}

func (rt *RoundTripper) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("transport: split host/port %q: %w", addr, err)
	}

	p, err := rt.proxyFor(&url.URL{Scheme: "https", Host: addr})
	if err != nil {
		return nil, err
	}

	var rawConn net.Conn
	if p == nil {
		if rt.dialHook != nil {
			rawConn, err = rt.dialHook(ctx, network, addr)
		} else {
			dialer := net.Dialer{Timeout: dialTimeout}
			rawConn, err = dialer.DialContext(ctx, network, addr)
		}
		if err != nil {
			return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
		}
	} else {
		rawConn, err = rt.dialViaProxy(ctx, p, addr)
		if err != nil {
			return nil, err
		}
		slog.Debug("transport: via proxy",
			"proxy", netenv.RedactProxy(p.String()),
			"target", host)
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

// dialViaProxy opens a CONNECT tunnel through the http proxy p to target
// (host:port). The uTLS handshake then runs over the returned conn with the
// TARGET as ServerName, so the proxy only ever sees the tunnel. Proxy
// credentials go into the Proxy-Authorization header and nowhere else: never
// into an error or a log line.
func (rt *RoundTripper) dialViaProxy(ctx context.Context, p *url.URL, target string) (net.Conn, error) {
	for i := 0; i < len(target); i++ {
		if target[i] <= 0x20 || target[i] == 0x7f {
			return nil, errors.New("transport: invalid CONNECT target")
		}
	}
	thost, tport, err := net.SplitHostPort(target)
	if err != nil || thost == "" || tport == "" {
		return nil, errors.New("transport: invalid CONNECT target")
	}
	paddr := p.Host
	if _, _, err := net.SplitHostPort(paddr); err != nil {
		paddr += ":80"
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", paddr)
	if err != nil {
		return nil, fmt.Errorf("transport: dial proxy %s: %w", paddr, err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	fail := func(err error) (net.Conn, error) {
		stop()
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))

	var req strings.Builder
	req.WriteString("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n")
	if p.User != nil {
		pass, _ := p.User.Password()
		req.WriteString("Proxy-Authorization: Basic " +
			base64.StdEncoding.EncodeToString([]byte(p.User.Username()+":"+pass)) + "\r\n")
	}
	req.WriteString("\r\n")

	if _, err := conn.Write([]byte(req.String())); err != nil {
		return fail(errors.New("transport: write CONNECT to proxy failed"))
	}
	br := bufio.NewReader(io.LimitReader(conn, 16<<10))
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fail(errors.New("transport: read CONNECT response from proxy failed"))
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("transport: proxy refused CONNECT (status %d)", resp.StatusCode))
	}
	if br.Buffered() > 0 {
		return fail(errors.New("transport: proxy sent data before the tunnel opened"))
	}
	_ = conn.SetDeadline(time.Time{})
	stop()
	return conn, nil
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
