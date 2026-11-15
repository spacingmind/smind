package browser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hitServer is a loopback HTTP server that counts every request it receives.
type hitServer struct {
	*httptest.Server
	hits atomic.Int64
	port int
}

func newHitServer(t *testing.T, h http.HandlerFunc) *hitServer {
	t.Helper()
	hs := &hitServer{}
	hs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hs.hits.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(hs.Close)
	hs.port = hs.Listener.Addr().(*net.TCPAddr).Port
	return hs
}

// dialRecorder records every address the proxy dials.
type dialRecorder struct {
	mu    sync.Mutex
	addrs []string
}

func (d *dialRecorder) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, addr)
	d.mu.Unlock()
	var nd net.Dialer
	return nd.DialContext(ctx, network, addr)
}

func (d *dialRecorder) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

type proxyRig struct {
	px     *Proxy
	dials  *dialRecorder
	daemon *hitServer
	refuse []string
	mu     sync.Mutex
}

func newProxyRig(t *testing.T, allow []string, res Resolver, mut func(*ProxyConfig)) *proxyRig {
	t.Helper()
	r := &proxyRig{dials: &dialRecorder{}, daemon: newHitServer(t, nil)}
	pol, err := NewPolicy(allow, []Endpoint{{Host: "127.0.0.1", Port: r.daemon.port}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ProxyConfig{Policy: pol, Resolver: res, Dial: r.dials.dial, OnRefuse: func(target, reason string) {
		r.mu.Lock()
		r.refuse = append(r.refuse, target+": "+reason)
		r.mu.Unlock()
	}}
	if mut != nil {
		mut(&cfg)
	}
	r.px, err = StartProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.px.Close)
	return r
}

// get does a proxied GET the way a browser sends it (absolute form).
func (r *proxyRig) get(t *testing.T, target string) (int, string) {
	t.Helper()
	pu, _ := url.Parse("http://" + r.px.Addr())
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(target)
	if err != nil {
		t.Fatalf("GET %s via proxy: %v", target, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// connect opens a CONNECT tunnel; it returns the status line and the conn.
func (r *proxyRig) connect(t *testing.T, target string) (string, net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.Dial("tcp", r.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := br.ReadString('\n')
	if err != nil {
		return "", c, br
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(l) == "" {
			break
		}
	}
	_ = c.SetReadDeadline(time.Time{})
	return strings.TrimSpace(status), c, br
}

var proxyResolver = fakeResolver{
	"localhost":      {"127.0.0.1", "::1"},
	"app.localhost":  {"127.0.0.1"},
	"evil.localhost": {"10.0.0.1"},
	"meta.test":      {"169.254.169.254"},
	"mixed.test":     {"127.0.0.1", "169.254.169.254"},
	"nat64.test":     {"64:ff9b::a9fe:a9fe"},
}

func TestProxyForwardsAllowedHTTP(t *testing.T) {
	var gotHdr http.Header
	var gotHost string
	srv := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotHdr, gotHost = r.Header.Clone(), r.Host
		fmt.Fprint(w, "hello from the app")
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	code, body := rig.get(t, fmt.Sprintf("http://127.0.0.1:%d/a?b=1", srv.port))
	if code != 200 || body != "hello from the app" || srv.hits.Load() != 1 {
		t.Fatalf("allowed target: %d %q hits=%d", code, body, srv.hits.Load())
	}
	if gotHdr.Get("Proxy-Connection") != "" || gotHdr.Get("Proxy-Authorization") != "" {
		t.Fatalf("hop-by-hop headers must not be forwarded: %v", gotHdr)
	}
	if gotHost != fmt.Sprintf("127.0.0.1:%d", srv.port) {
		t.Fatalf("host header %q", gotHost)
	}
	// No connection is reused: a second request dials again, through the policy.
	if code, _ := rig.get(t, fmt.Sprintf("http://127.0.0.1:%d/again", srv.port)); code != 200 || len(rig.dials.list()) != 2 {
		t.Fatalf("every request must be vetted and dialled afresh: dials %v", rig.dials.list())
	}
	// By name: resolved by the proxy, dialled to the vetted IP.
	code, _ = rig.get(t, fmt.Sprintf("http://localhost:%d/", srv.port))
	if code != 200 {
		t.Fatalf("localhost: %d", code)
	}
	for _, a := range rig.dials.list() {
		host, _, _ := net.SplitHostPort(a)
		if _, err := netip.ParseAddr(host); err != nil {
			t.Fatalf("the proxy must dial an IP, never a name: %q", a)
		}
	}
}

func TestProxyRefusesDeniedAndUnlistedTargets(t *testing.T) {
	rig := newProxyRig(t, []string{"intranet.test"}, proxyResolver, nil)
	cases := map[string]string{
		"daemon port":                     fmt.Sprintf("http://127.0.0.1:%d/api/token", rig.daemon.port),
		"daemon by name":                  fmt.Sprintf("http://localhost:%d/api/token", rig.daemon.port),
		"daemon decimal ip":               fmt.Sprintf("http://2130706433:%d/", rig.daemon.port),
		"proxy itself":                    "http://" + rig.px.Addr() + "/",
		"public host":                     "http://example.com/",
		"public host with a secret query": "http://example.com/a?token=SECRETVALUE#frag",
		"public ip":                       "http://93.184.216.34/",
		"metadata":                        "http://169.254.169.254/latest/meta-data",
		"private range":                   "http://10.0.0.5:8080/",
	}
	for name, target := range cases {
		code, _ := rig.get(t, target)
		if code != http.StatusForbidden {
			t.Errorf("%s (%s): got %d, want 403", name, target, code)
		}
	}
	if rig.daemon.hits.Load() != 0 {
		t.Fatalf("the daemon was reached %d times through the proxy", rig.daemon.hits.Load())
	}
	if n := len(rig.dials.list()); n != 0 {
		t.Fatalf("a refused target must never be dialled, dials: %v", rig.dials.list())
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	for _, r := range rig.refuse {
		if strings.Contains(r, "SECRETVALUE") || strings.Contains(r, "frag") {
			t.Fatalf("a refusal notice must not carry the query or fragment: %q", r)
		}
	}
	if len(rig.refuse) != len(cases) {
		t.Fatalf("every refusal is reported once: %d of %d", len(rig.refuse), len(cases))
	}
}

func TestProxyConnectTunnelsAllowedTargets(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, nil)
	status, c, br := rig.connect(t, echo.Addr().String())
	if !strings.Contains(status, "200") {
		t.Fatalf("CONNECT to an allowed loopback port: %q", status)
	}
	fmt.Fprint(c, "ping")
	buf := make([]byte, 4)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("tunnel echo: %q %v", buf, err)
	}
}

func TestProxyConnectToDeniedIPLiteralIsRefused(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, nil)
	for _, target := range []string{
		fmt.Sprintf("127.0.0.1:%d", rig.daemon.port), // the daemon
		fmt.Sprintf("[::1]:%d", rig.daemon.port),
		fmt.Sprintf("2130706433:%d", rig.daemon.port), // decimal form of 127.0.0.1
		rig.px.Addr(), // the proxy itself
		"93.184.216.34:443",
		"169.254.169.254:80",
		"[64:ff9b::a9fe:a9fe]:80",
	} {
		status, _, _ := rig.connect(t, target)
		if !strings.Contains(status, "403") {
			t.Errorf("CONNECT %s: %q, want 403", target, status)
		}
	}
	if rig.daemon.hits.Load() != 0 || len(rig.dials.list()) != 0 {
		t.Fatalf("nothing may be dialled: hits=%d dials=%v", rig.daemon.hits.Load(), rig.dials.list())
	}
}

// A page WebSocket to a denied loopback port arrives as CONNECT and is refused
// before anything is dialled (the case Fetch interception cannot see).
func TestProxyPageWebSocketToDeniedPortIsRefused(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, nil)
	for _, target := range []string{fmt.Sprintf("127.0.0.1:%d", rig.daemon.port), fmt.Sprintf("localhost:%d", rig.daemon.port), fmt.Sprintf("sock.localhost:%d", rig.daemon.port)} {
		status, _, _ := rig.connect(t, target)
		if !strings.Contains(status, "403") {
			t.Errorf("CONNECT %s (a WebSocket to the daemon): %q", target, status)
		}
	}
	if rig.daemon.hits.Load() != 0 {
		t.Fatalf("the daemon saw %d connections", rig.daemon.hits.Load())
	}
}

// seqResolver answers each lookup of a name from a list, in order.
type seqResolver struct {
	mu      sync.Mutex
	answers [][]string
	calls   int
}

func (s *seqResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	s.calls++
	var out []netip.Addr
	for _, a := range s.answers[i] {
		out = append(out, netip.MustParseAddr(a))
	}
	return out, nil
}

func TestProxyRebindingIsPinned(t *testing.T) {
	srv := newHitServer(t, nil)
	// Lookup 1 is loopback (allowed); every later lookup would be the daemon's
	// address on the daemon's port space or a metadata address.
	res := &seqResolver{answers: [][]string{{"127.0.0.1"}, {"169.254.169.254"}}}
	rig := newProxyRig(t, []string{"app.test"}, res, nil)
	code, _ := rig.get(t, fmt.Sprintf("http://app.test:%d/", srv.port))
	if code != 200 {
		t.Fatalf("first request: %d", code)
	}
	if got := rig.dials.list(); len(got) != 1 || got[0] != fmt.Sprintf("127.0.0.1:%d", srv.port) {
		t.Fatalf("the connection must be dialled to the vetted IP, not the name: %v", got)
	}
	// The name now rebinds: the very next connection re-resolves and is refused.
	code, _ = rig.get(t, fmt.Sprintf("http://app.test:%d/", srv.port))
	if code != http.StatusForbidden || srv.hits.Load() != 1 || len(rig.dials.list()) != 1 {
		t.Fatalf("after rebinding: %d hits=%d dials=%v", code, srv.hits.Load(), rig.dials.list())
	}
	status, _, _ := rig.connect(t, fmt.Sprintf("app.test:%d", srv.port))
	if !strings.Contains(status, "403") || len(rig.dials.list()) != 1 {
		t.Fatalf("CONNECT after rebinding: %q dials=%v", status, rig.dials.list())
	}
}

func TestProxyMixedAndSensitiveResolutionsAreRefused(t *testing.T) {
	rig := newProxyRig(t, []string{"mixed.test", "meta.test", "nat64.test"}, proxyResolver, nil)
	for _, host := range []string{"mixed.test", "meta.test", "nat64.test"} {
		code, _ := rig.get(t, "http://"+host+"/")
		if code != http.StatusForbidden {
			t.Errorf("%s: %d", host, code)
		}
		status, _, _ := rig.connect(t, host+":443")
		if !strings.Contains(status, "403") {
			t.Errorf("CONNECT %s: %q", host, status)
		}
	}
	if len(rig.dials.list()) != 0 {
		t.Fatalf("dialled: %v", rig.dials.list())
	}
}

func TestProxyMalformedRequestsAreRefused(t *testing.T) {
	live := newHitServer(t, nil)
	rig := newProxyRig(t, nil, proxyResolver, nil)
	defer func() {
		if live.hits.Load() != 0 || len(rig.dials.list()) != 0 {
			t.Errorf("a malformed request must never be forwarded: hits=%d dials=%v", live.hits.Load(), rig.dials.list())
		}
	}()
	raw := func(req string) string {
		c, err := net.Dial("tcp", rig.px.Addr())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(c, strings.Replace(req, "\r\n\r\n", "\r\nConnection: close\r\n\r\n", 1))
		b, _ := io.ReadAll(c)
		return string(b)
	}
	for name, req := range map[string]string{
		"https absolute form":  "GET https://localhost/ HTTP/1.1\r\nHost: localhost\r\n\r\n",
		"ftp":                  "GET ftp://localhost/ HTTP/1.1\r\nHost: localhost\r\n\r\n",
		"origin form":          "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n",
		"upgrade over plain":   fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", live.port),
		"connect without port": "CONNECT localhost HTTP/1.1\r\nHost: localhost\r\n\r\n",
		"backslash host":       fmt.Sprintf("GET http://localhost\\@127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port),
		"userinfo":             fmt.Sprintf("GET http://user:pw@127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port),
	} {
		resp := raw(req)
		if !strings.HasPrefix(resp, "HTTP/1.1 4") && !strings.HasPrefix(resp, "HTTP/1.1 501") {
			t.Errorf("%s: want a 4xx/501, got %q", name, firstLine(resp))
		}
	}
	// Garbage and an oversized header block are closed, not served.
	if resp := raw("\x00\x01garbage\r\n\r\n"); strings.Contains(resp, "200") {
		t.Errorf("garbage served: %q", resp)
	}
	big := "GET http://localhost:1/ HTTP/1.1\r\nHost: x\r\nX: " + strings.Repeat("a", 64<<10) + "\r\n\r\n"
	if resp := raw(big); !strings.HasPrefix(resp, "HTTP/1.1 431") && resp != "" {
		t.Errorf("oversized headers: %q", firstLine(resp))
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func TestProxyConnectionLimit(t *testing.T) {
	echo, _ := net.Listen("tcp", "127.0.0.1:0")
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.MaxConns = 3 })
	for i := 0; i < 3; i++ {
		if status, _, _ := rig.connect(t, echo.Addr().String()); !strings.Contains(status, "200") {
			t.Fatalf("tunnel %d: %q", i, status)
		}
	}
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.Contains(line, "503") {
		t.Fatalf("a connection over the limit must be told 503, got %q", line)
	}
}

func TestProxyDialTimeout(t *testing.T) {
	blocked := func(ctx context.Context, network, addr string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.Dial = blocked; c.DialTimeout = 100 * time.Millisecond })
	start := time.Now()
	code, _ := rig.get(t, "http://127.0.0.1:9/")
	if code != http.StatusGatewayTimeout || time.Since(start) > 3*time.Second {
		t.Fatalf("a dial that hangs must time out as 504 quickly: %d after %v", code, time.Since(start))
	}
	status, _, _ := rig.connect(t, "127.0.0.1:9")
	if !strings.Contains(status, "504") {
		t.Fatalf("CONNECT dial timeout: %q", status)
	}
}

func TestProxyTunnelIdleTimeout(t *testing.T) {
	quiet, _ := net.Listen("tcp", "127.0.0.1:0")
	defer quiet.Close()
	go func() {
		for {
			c, err := quiet.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.TunnelIdle = 150 * time.Millisecond })
	status, c, br := rig.connect(t, quiet.Addr().String())
	if !strings.Contains(status, "200") {
		t.Fatalf("%q", status)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := br.ReadByte(); err == nil {
		t.Fatal("expected the idle tunnel to be closed")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("the proxy did not close an idle tunnel")
	}
}

func TestProxyCloseEndsListenerAndTunnels(t *testing.T) {
	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, nil)
	_, c, br := rig.connect(t, sink.Addr().String())
	addr := rig.px.Addr()
	rig.px.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := br.ReadByte(); err == nil {
		t.Fatal("Close must end open tunnels")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("Close left the tunnel open")
	}
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("Close must stop the listener")
	}
}

func TestProxyStreamsAndStripsResponseHopByHop(t *testing.T) {
	srv := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-Keep", "yes")
		w.WriteHeader(201)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "chunk%d", i)
			w.(http.Flusher).Flush()
		}
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	pu, _ := url.Parse("http://" + rig.px.Addr())
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}, Timeout: 5 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/", srv.port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 || string(b) != "chunk0chunk1chunk2" || resp.Header.Get("X-Keep") != "yes" || resp.Header.Get("Keep-Alive") != "" {
		t.Fatalf("response: %d %q %v", resp.StatusCode, b, resp.Header)
	}
}

func TestProxyDoesNotFollowRedirects(t *testing.T) {
	target := newHitServer(t, nil)
	srv := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%d/", target.port), http.StatusFound)
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	code, _ := rig.get(t, fmt.Sprintf("http://127.0.0.1:%d/", srv.port))
	if code != http.StatusFound || target.hits.Load() != 0 {
		t.Fatalf("the redirect is the browser's to follow (and to be vetted again): %d, target hits %d", code, target.hits.Load())
	}
}

func TestSessionStartsAProxyAndDeniesItToThePage(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	_ = f
	if s.proxy == nil {
		t.Fatal("a session must own a proxy")
	}
	l.mu.Lock()
	opts := l.opts
	l.mu.Unlock()
	if opts.ProxyAddr != s.proxy.Addr() {
		t.Fatalf("the launcher must be handed the proxy address: %q vs %q", opts.ProxyAddr, s.proxy.Addr())
	}
	port := s.proxy.ln.Addr().(*net.TCPAddr).Port
	if d := s.cfg.Policy.CheckURL(fmt.Sprintf("http://localhost:%d/", port)); d.Allowed {
		t.Fatal("the proxy's own port must be denied to the page")
	}
	addr := s.proxy.Addr()
	s.Close()
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("closing the session must stop its proxy")
	}
}

func TestSessionStartFailureStopsTheProxy(t *testing.T) {
	var addr string
	fl := launcherFunc(func(ctx context.Context, o LaunchOpts) (Instance, error) {
		addr = o.ProxyAddr
		return nil, fmt.Errorf("no browser")
	})
	if _, err := Start(context.Background(), Config{Workspace: "w", Launcher: fl}); err == nil {
		t.Fatal("want error")
	}
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("a failed start must not leave its proxy listening")
	}
}

type launcherFunc func(ctx context.Context, o LaunchOpts) (Instance, error)

func (f launcherFunc) Launch(ctx context.Context, o LaunchOpts) (Instance, error) { return f(ctx, o) }

func TestProxyRefusesAnUpgradeOverAPlainRequest(t *testing.T) {
	srv := newHitServer(t, nil)
	rig := newProxyRig(t, nil, proxyResolver, nil)
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: close, Upgrade\r\n\r\n", srv.port)
	line, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.Contains(line, "501") || srv.hits.Load() != 0 {
		t.Fatalf("an upgrade must arrive as CONNECT (and be vetted there): %q hits=%d", strings.TrimSpace(line), srv.hits.Load())
	}
}

func TestProxyLoopbackNamesNeedNoDNS(t *testing.T) {
	srv := newHitServer(t, nil)
	res := &countingResolver{}
	rig := newProxyRig(t, nil, res, nil)
	for _, host := range []string{"localhost", "app.localhost", "a.b.localhost"} {
		if code, _ := rig.get(t, fmt.Sprintf("http://%s:%d/", host, srv.port)); code != 200 {
			t.Errorf("%s: %d", host, code)
		}
		if status, _, _ := rig.connect(t, fmt.Sprintf("%s:%d", host, srv.port)); !strings.Contains(status, "200") {
			t.Errorf("CONNECT %s: %q", host, status)
		}
	}
	if res.calls.Load() != 0 {
		t.Fatalf("a page-chosen *.localhost name must never be sent to DNS (it could leave the machine): %d lookups", res.calls.Load())
	}
	for _, a := range rig.dials.list() {
		if !strings.HasPrefix(a, "127.0.0.1:") && !strings.HasPrefix(a, "[::1]:") {
			t.Errorf("loopback names are dialled on loopback only: %s", a)
		}
	}
}

type countingResolver struct{ calls atomic.Int64 }

func (c *countingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	c.calls.Add(1)
	return nil, fmt.Errorf("must not be called")
}

// A silent client behind a silent upstream: the tunnel is closed on BOTH sides
// at the idle timeout and its slot is released.
func TestProxyIdleTunnelReleasesItsSlot(t *testing.T) {
	quiet, _ := net.Listen("tcp", "127.0.0.1:0")
	defer quiet.Close()
	go func() {
		for {
			c, err := quiet.Accept()
			if err != nil {
				return
			}
			_ = c // held open, never speaks
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.MaxConns = 1; c.TunnelIdle = 150 * time.Millisecond })
	status, c, _ := rig.connect(t, quiet.Addr().String())
	if !strings.Contains(status, "200") {
		t.Fatal(status)
	}
	// The client stays silent too. Both sides must be closed by the proxy, and
	// the only slot freed: a new tunnel works afterwards.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if status2, c2, _ := rig.connect(t, quiet.Addr().String()); strings.Contains(status2, "200") {
			_ = c2.Close()
			_ = c.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := c.Write([]byte("x")); err == nil {
				// a write may succeed once into a closing socket; the read must now see EOF
				_ = c.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := c.Read(make([]byte, 1)); err == nil {
					t.Fatal("the idle tunnel's client side must be closed")
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("an idle tunnel must release its connection slot")
}

// A tunnel the client only writes to stays alive while the client sends, even
// though the upstream is silent (one shared activity clock).
func TestProxyUploadKeepsTheTunnelAlive(t *testing.T) {
	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()
	var got atomic.Int64
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			go func() { n, _ := io.Copy(io.Discard, c); got.Add(n) }()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.TunnelIdle = 300 * time.Millisecond })
	status, c, _ := rig.connect(t, sink.Addr().String())
	if !strings.Contains(status, "200") {
		t.Fatal(status)
	}
	for i := 0; i < 8; i++ { // 8 x 100 ms = more than twice the idle timeout
		if _, err := c.Write([]byte("data")); err != nil {
			t.Fatalf("the tunnel was closed while the client was sending (write %d): %v", i, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	deadline := time.Now().Add(2 * time.Second)
	for got.Load() < 32 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 32 {
		t.Fatalf("upstream received %d bytes, want 32", got.Load())
	}
}

func TestProxyClosedTunnelReleasesItsSlot(t *testing.T) {
	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.MaxConns = 2 })
	for round := 0; round < 6; round++ {
		status, c, _ := rig.connect(t, sink.Addr().String())
		if !strings.Contains(status, "200") {
			t.Fatalf("round %d: %q (slots must be released when a tunnel closes)", round, status)
		}
		_ = c.Close()
		time.Sleep(50 * time.Millisecond)
	}
}

func TestProxyUpstreamWithABadStatusIs502(t *testing.T) {
	bad, _ := net.Listen("tcp", "127.0.0.1:0")
	defer bad.Close()
	go func() {
		for {
			c, err := bad.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = bufio.NewReader(c).ReadString('\n')
				_, _ = io.WriteString(c, "HTTP/1.1 099 Nope\r\nContent-Length: 0\r\n\r\n")
			}()
		}
	}()
	rig := newProxyRig(t, nil, proxyResolver, nil)
	code, _ := rig.get(t, "http://"+bad.Addr().String()+"/")
	if code != http.StatusBadGateway {
		t.Fatalf("an unusable upstream status must be a clean 502, got %d", code)
	}
}

func TestProxyEarlyBytesAfterConnectReachUpstream(t *testing.T) {
	var got atomic.Value
	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()
	go func() {
		c, err := sink.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		b := make([]byte, 5)
		_, _ = io.ReadFull(c, b)
		got.Store(string(b))
	}()
	rig := newProxyRig(t, nil, proxyResolver, nil)
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The client sends data in the same packet as the CONNECT head.
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\nHELLO", sink.Addr(), sink.Addr())
	deadline := time.Now().Add(3 * time.Second)
	for got.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != "HELLO" {
		t.Fatalf("bytes sent behind the CONNECT head must reach upstream: %v", got.Load())
	}
}
