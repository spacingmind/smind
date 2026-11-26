package transport

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http/httpproxy"
)

// upstream pairs the TLS httptest server (body "ok-tls") with a plain one
// (body "ok-plain") and the RootCAs pool that trusts the TLS certificate.
// The TLS handler records the SNI the client sent, so tests can pin that the
// handshake (through a tunnel) uses the TARGET host as ServerName.
type upstream struct {
	tlsSrv   *httptest.Server
	plainSrv *httptest.Server
	pool     *x509.CertPool

	mu      sync.Mutex
	lastSNI string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()

	u := &upstream{}
	u.tlsSrv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		if r.TLS != nil {
			u.lastSNI = r.TLS.ServerName
		}
		u.mu.Unlock()
		_, _ = w.Write([]byte("ok-tls"))
	}))
	t.Cleanup(u.tlsSrv.Close)

	u.plainSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok-plain"))
	}))
	t.Cleanup(u.plainSrv.Close)

	u.pool = x509.NewCertPool()
	u.pool.AddCert(u.tlsSrv.Certificate())
	return u
}

func (u *upstream) tlsAddr() string   { return u.tlsSrv.Listener.Addr().String() }
func (u *upstream) plainAddr() string { return u.plainSrv.Listener.Addr().String() }

// tlsTarget/plainTarget rewrite the server URL with host as the target host,
// so a request resolves to host:port while the port stays the real server's.
func (u *upstream) tlsTarget(host string) string {
	return strings.Replace(u.tlsSrv.URL, "127.0.0.1", host, 1)
}
func (u *upstream) plainTarget(host string) string {
	return strings.Replace(u.plainSrv.URL, "127.0.0.1", host, 1)
}

func (u *upstream) sni() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastSNI
}

func srvPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", srv.Listener.Addr().String(), err)
	}
	return port
}

// recProxy is a recording CONNECT proxy: it answers CONNECT by tunneling to
// upstreamTLSAddr (whatever host the client asked for) and answers any other
// method with "via-proxy", recording what it saw.
type recProxy struct {
	mu       sync.Mutex
	connects []string
	auth     []string
	plain    []string
	status   int
	ln       net.Listener
}

func startProxy(t *testing.T, upstreamTLSAddr string, status int) *recProxy {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	p := &recProxy{status: status, ln: ln}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			p.record(r.Host, &p.connects)
			p.record(r.Header.Get("Proxy-Authorization"), &p.auth)
			if p.status != 0 && p.status != http.StatusOK {
				w.WriteHeader(p.status)
				return
			}
			up, err := net.Dial("tcp", upstreamTLSAddr)
			if err != nil {
				http.Error(w, "dial upstream failed", http.StatusBadGateway)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				up.Close()
				http.Error(w, "hijack unsupported", http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				up.Close()
				return
			}
			if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
				conn.Close()
				up.Close()
				return
			}
			go func() {
				defer conn.Close()
				defer up.Close()
				upDone := make(chan struct{})
				downDone := make(chan struct{})
				go func() { defer close(upDone); _, _ = io.Copy(up, conn) }()
				go func() { defer close(downDone); _, _ = io.Copy(conn, up) }()
				select {
				case <-upDone:
				case <-downDone:
				}
			}()
			return
		}
		p.record(r.RequestURI, &p.plain)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("via-proxy"))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
	return p
}

func (p *recProxy) record(v string, dst *[]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	*dst = append(*dst, v)
}

func (p *recProxy) connectsSeen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connects...)
}

func (p *recProxy) authSeen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.auth...)
}

func (p *recProxy) plainSeen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.plain...)
}

// url returns the proxy's URL, with userinfo when user is not empty.
func (p *recProxy) url(user, pass string) string {
	u := &url.URL{Scheme: "http", Host: p.ln.Addr().String()}
	if user != "" {
		u.User = url.UserPassword(user, pass)
	}
	return u.String()
}

// directTo dials addr whatever target it is given; used with
// WithDirectDialer so a fake target name reaches a real server. The hook also
// carries the dial of a plain-HTTP request to an http proxy, so a test with a
// proxy configured passes the proxy's address in passthrough: it is dialled
// for real, and a request that wrongly reaches the proxy is then seen by it.
func directTo(addr string, passthrough ...string) func(ctx context.Context, network, a string) (net.Conn, error) {
	return func(ctx context.Context, network, a string) (net.Conn, error) {
		var d net.Dialer
		for _, p := range passthrough {
			if a == p {
				return d.DialContext(ctx, network, a)
			}
		}
		return d.DialContext(ctx, "tcp", addr)
	}
}

// captureDebugLog redirects the default slog to a buffer at debug level;
// tests that use it (or t.Setenv) must not run in parallel.
func captureDebugLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func get(t *testing.T, client *http.Client, rawurl string) string {
	t.Helper()

	resp, err := client.Get(rawurl)
	if err != nil {
		t.Fatalf("GET %s: %v", rawurl, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, want %d", rawurl, resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", rawurl, err)
	}
	return string(body)
}

func TestProxy_HTTPSViaConnectKeepsUTLS(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	rt := New(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("", "")}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)
	client := &http.Client{Transport: rt}

	if body := get(t, client, u.tlsTarget("example.com")+"/x"); body != "ok-tls" {
		t.Errorf("body = %q, want %q", body, "ok-tls")
	}

	want := "example.com:" + srvPort(t, u.tlsSrv)
	if got := proxy.connectsSeen(); len(got) != 1 || got[0] != want {
		t.Errorf("proxy CONNECTs = %v, want [%s]", got, want)
	}

	state, ok := rt.ConnectionState()
	if !ok || !state.HandshakeComplete {
		t.Errorf("ConnectionState() = (%+v, %v), want a completed uTLS handshake", state, ok)
	}
	if got := u.sni(); got != "example.com" {
		t.Errorf("server saw SNI %q, want %q (the handshake must use the target host as ServerName)", got, "example.com")
	}
}

func TestProxy_ConnectSendsBasicAuth(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	client := Client(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("alice", "p@ss")}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)
	get(t, client, u.tlsTarget("example.com"))

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:p@ss"))
	if got := proxy.authSeen(); len(got) != 1 || got[0] != want {
		t.Errorf("Proxy-Authorization = %v, want [%s]", got, want)
	}
}

func TestProxy_PlainHTTPViaAbsoluteURI(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	client := Client(utls.HelloChrome_Auto,
		WithProxyConfig(&httpproxy.Config{HTTPProxy: proxy.url("", "")}),
	)

	target := u.plainTarget("example.com") + "/plain?q=1"
	if body := get(t, client, target); body != "via-proxy" {
		t.Errorf("body = %q, want %q", body, "via-proxy")
	}
	if got := proxy.plainSeen(); len(got) != 1 || got[0] != target {
		t.Errorf("proxy plain requests = %v, want [%s]", got, target)
	}
}

func TestProxy_StreamClientToo(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	client := StreamClient(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("", "")}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)

	if body := get(t, client, u.tlsTarget("example.com")); body != "ok-tls" {
		t.Errorf("body = %q, want %q", body, "ok-tls")
	}
	want := "example.com:" + srvPort(t, u.tlsSrv)
	if got := proxy.connectsSeen(); len(got) != 1 || got[0] != want {
		t.Errorf("proxy CONNECTs = %v, want [%s]", got, want)
	}
}

func TestProxy_NoProxyBypasses(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	t.Run("host", func(t *testing.T) {
		client := Client(utls.HelloChrome_Auto,
			WithRootCAs(u.pool),
			WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("", ""), NoProxy: "example.com"}),
			WithDirectDialer(directTo(u.tlsAddr())),
		)
		if body := get(t, client, u.tlsTarget("example.com")); body != "ok-tls" {
			t.Errorf("body = %q, want %q", body, "ok-tls")
		}
	})
	t.Run("suffix", func(t *testing.T) {
		client := Client(utls.HelloChrome_Auto,
			WithProxyConfig(&httpproxy.Config{HTTPProxy: proxy.url("", ""), NoProxy: ".internal.test"}),
			WithDirectDialer(directTo(u.plainAddr())),
		)
		if body := get(t, client, u.plainTarget("api.internal.test")+"/"); body != "ok-plain" {
			t.Errorf("body = %q, want %q", body, "ok-plain")
		}
	})
	t.Run("cidr", func(t *testing.T) {
		client := Client(utls.HelloChrome_Auto,
			WithProxyConfig(&httpproxy.Config{HTTPProxy: proxy.url("", ""), NoProxy: "10.0.0.0/8"}),
			WithDirectDialer(directTo(u.plainAddr())),
		)
		if body := get(t, client, u.plainTarget("10.1.2.3")+"/"); body != "ok-plain" {
			t.Errorf("body = %q, want %q", body, "ok-plain")
		}
	})

	if got := proxy.connectsSeen(); len(got) != 0 {
		t.Errorf("proxy CONNECTs = %v, want none", got)
	}
	if got := proxy.plainSeen(); len(got) != 0 {
		t.Errorf("proxy plain requests = %v, want none", got)
	}
}

func TestProxy_LoopbackNeverProxied(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)
	cfg := &httpproxy.Config{HTTPSProxy: proxy.url("", ""), HTTPProxy: proxy.url("", "")}

	// A real loopback target: no dial hook, the direct dial itself must
	// reach the server.
	direct := Client(utls.HelloChrome_Auto, WithRootCAs(u.pool), WithProxyConfig(cfg))
	if body := get(t, direct, u.tlsTarget("127.0.0.1")); body != "ok-tls" {
		t.Errorf("body = %q, want %q", body, "ok-tls")
	}

	// Loopback names that resolve nowhere: the hook carries them to a real
	// server, and the proxy must be bypassed all the same.
	hooked := Client(utls.HelloChrome_Auto,
		WithProxyConfig(cfg),
		WithDirectDialer(directTo(u.plainAddr(), proxy.ln.Addr().String())),
	)
	for _, host := range []string{"127.0.0.1", "localhost", "a.localhost", "[::1]"} {
		if body := get(t, hooked, u.plainTarget(host)+"/"); body != "ok-plain" {
			t.Errorf("GET http://%s: body = %q, want %q", host, body, "ok-plain")
		}
	}

	if got := proxy.connectsSeen(); len(got) != 0 {
		t.Errorf("proxy CONNECTs = %v, want none", got)
	}
	if got := proxy.plainSeen(); len(got) != 0 {
		t.Errorf("proxy plain requests = %v, want none", got)
	}

	for _, tt := range []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LOCALHOST.", true},
		{"a.b.localhost", true},
		{"127.0.0.5", true},
		{"::1", true},
		{"[::1]", true},
		{"::1%lo0", true},
		{"::ffff:127.0.0.1", true},
		{"127.1", true},
		{"0x7f.0.0.1", true},
		{"2130706433", true},
		{"0.0.0.0", true},
		{"example.com", false},
		{"10.0.0.1", false},
		{"10.1.2.3", false},
		{"1e9.example.com", false},
		{"fe80::1%en0", false},
		{"localhost.example.com", false},
		{"notlocalhost", false},
	} {
		if got := neverProxyHost(tt.host); got != tt.want {
			t.Errorf("neverProxyHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestProxy_UnsupportedSchemeFailsClosed(t *testing.T) {
	u := newUpstream(t)

	counting := func() (func(context.Context, string, string) (net.Conn, error), *atomic.Int64) {
		var dials atomic.Int64
		d := directTo(u.tlsAddr())
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials.Add(1)
			return d(ctx, network, addr)
		}, &dials
	}

	t.Run("https target socks5 proxy", func(t *testing.T) {
		hook, dials := counting()
		client := Client(utls.HelloChrome_Auto,
			WithRootCAs(u.pool),
			WithProxyConfig(&httpproxy.Config{HTTPSProxy: "socks5://127.0.0.1:1"}),
			WithDirectDialer(hook),
		)
		_, err := client.Get(u.tlsTarget("example.com"))
		if err == nil || !strings.Contains(err.Error(), `proxy scheme "socks5" is not supported`) {
			t.Errorf("Get() error = %v, want an unsupported proxy scheme error", err)
		}
		if n := dials.Load(); n != 0 {
			t.Errorf("direct dial called %d times, want 0 (no silent bypass)", n)
		}
	})
	t.Run("http target https proxy", func(t *testing.T) {
		hook, dials := counting()
		client := Client(utls.HelloChrome_Auto,
			WithProxyConfig(&httpproxy.Config{HTTPProxy: "https://127.0.0.1:1"}),
			WithDirectDialer(hook),
		)
		_, err := client.Get(u.plainTarget("example.com"))
		if err == nil || !strings.Contains(err.Error(), `proxy scheme "https" is not supported`) {
			t.Errorf("Get() error = %v, want an unsupported proxy scheme error", err)
		}
		if n := dials.Load(); n != 0 {
			t.Errorf("direct dial called %d times, want 0 (no silent bypass)", n)
		}
	})
}

func TestProxy_ConnectRefusedNamesStatusOnly(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), http.StatusProxyAuthRequired)

	client := Client(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("alice", "s3cret!")}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)
	_, err := client.Get(u.tlsTarget("example.com"))
	if err == nil {
		t.Fatal("Get() error = nil, want the refused CONNECT to fail the request")
	}
	msg := err.Error()
	if !strings.Contains(msg, "407") {
		t.Errorf("error %q does not name the status code", msg)
	}
	if strings.Contains(msg, "s3cret") || strings.Contains(msg, "alice") {
		t.Errorf("error %q leaks the proxy user or password", msg)
	}
}

func TestProxy_CredentialsNeverInErrorsOrLogs(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)
	buf := captureDebugLog(t)

	client := Client(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{HTTPSProxy: proxy.url("alice", "s3cret!")}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)
	if body := get(t, client, u.tlsTarget("example.com")); body != "ok-tls" {
		t.Fatalf("body = %q, want %q", body, "ok-tls")
	}

	logged := buf.String()
	for _, want := range []string{"via proxy", "127.0.0.1:"} {
		if !strings.Contains(logged, want) {
			t.Errorf("debug log does not contain %q:\n%s", want, logged)
		}
	}
	for _, bad := range []string{"s3cret", "alice", "Basic"} {
		if strings.Contains(logged, bad) {
			t.Errorf("debug log contains %q:\n%s", bad, logged)
		}
	}
}

func TestProxy_UnsetIsUnchanged(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	client := Client(utls.HelloChrome_Auto,
		WithRootCAs(u.pool),
		WithProxyConfig(&httpproxy.Config{}),
		WithDirectDialer(directTo(u.tlsAddr())),
	)
	if body := get(t, client, u.tlsTarget("example.com")); body != "ok-tls" {
		t.Errorf("body = %q, want %q", body, "ok-tls")
	}
	if n := len(proxy.connectsSeen()) + len(proxy.plainSeen()); n != 0 {
		t.Errorf("proxy saw %d requests, want 0", n)
	}
}

func TestProxy_EnvIsReadAtConstruction(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	t.Setenv("HTTPS_PROXY", proxy.url("", ""))
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	client := Client(utls.HelloChrome_Auto, WithRootCAs(u.pool))
	if body := get(t, client, u.tlsTarget("example.com")); body != "ok-tls" {
		t.Errorf("body = %q, want %q", body, "ok-tls")
	}
	want := "example.com:" + srvPort(t, u.tlsSrv)
	if got := proxy.connectsSeen(); len(got) != 1 || got[0] != want {
		t.Errorf("proxy CONNECTs = %v, want [%s]", got, want)
	}
}

func TestProxy_ConnectTargetWithControlBytesRefused(t *testing.T) {
	u := newUpstream(t)
	proxy := startProxy(t, u.tlsAddr(), 0)

	rt := New(utls.HelloChrome_Auto, WithProxyConfig(&httpproxy.Config{}))
	proxyURL, err := url.Parse(proxy.url("", ""))
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", proxy.url("", ""), err)
	}
	for _, target := range []string{
		"a.com\r\nX-y:443",
		"a b:443",
		"host",
		":443",
		"a.com:",
	} {
		_, err := rt.dialViaProxy(context.Background(), proxyURL, target)
		if err == nil || !strings.Contains(err.Error(), "invalid CONNECT target") {
			t.Errorf("dialViaProxy(%q) error = %v, want invalid CONNECT target", target, err)
		}
	}
	if got := proxy.connectsSeen(); len(got) != 0 {
		t.Errorf("proxy CONNECTs = %v, want none (the proxy must not be dialled at all)", got)
	}
}

func TestProxy_ConnectHonoursContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})

	rt := New(utls.HelloChrome_Auto, WithProxyConfig(&httpproxy.Config{}))
	proxyURL := &url.URL{Scheme: "http", Host: ln.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = rt.dialViaProxy(ctx, proxyURL, "example.com:443")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("dialViaProxy() error = nil, want the cancelled context to fail the exchange")
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("dialViaProxy() took %v, want < 5s", elapsed)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("dialViaProxy() took %v, want < 2s", elapsed)
	}
}

func TestProxy_ConnectHeaderIsCapped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				if _, err := conn.Write([]byte("HTTP/1.1 200 OK\r\n")); err != nil {
					return
				}
				for {
					if _, err := conn.Write([]byte("X-A: aaaa\r\n")); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	rt := New(utls.HelloChrome_Auto, WithProxyConfig(&httpproxy.Config{}))
	proxyURL := &url.URL{Scheme: "http", Host: ln.Addr().String()}

	start := time.Now()
	_, err = rt.dialViaProxy(context.Background(), proxyURL, "example.com:443")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("dialViaProxy() error = nil, want the endless header to fail the exchange")
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("dialViaProxy() took %v, want < 5s", elapsed)
	}
}
