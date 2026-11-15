package browser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// raw sends one request on its own connection (Connection: close added) and
// returns the whole response.
func (r *proxyRig) raw(t *testing.T, req string) string {
	t.Helper()
	c, err := net.Dial("tcp", r.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(c, strings.Replace(req, "\r\n\r\n", "\r\nConnection: close\r\n\r\n", 1))
	b, _ := io.ReadAll(c)
	return string(b)
}

func status(resp string) string {
	line := resp
	if i := strings.Index(resp, "\r\n"); i >= 0 {
		line = resp[:i]
	}
	f := strings.Fields(line)
	if len(f) >= 2 {
		return f[1]
	}
	return ""
}

func TestProxyMalformedRequestsAreRefusedWithAnExactStatus(t *testing.T) {
	live := newHitServer(t, nil)
	rig := newProxyRig(t, nil, proxyResolver, nil)
	dp := rig.daemon.port
	cases := []struct{ name, req, want string }{
		{"https absolute form", "GET https://localhost/ HTTP/1.1\r\nHost: localhost\r\n\r\n", "400"},
		{"ftp", "GET ftp://localhost/ HTTP/1.1\r\nHost: localhost\r\n\r\n", "400"},
		{"origin form", "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n", "400"},
		{"upgrade over plain", fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", live.port), "501"},
		{"backslash host", fmt.Sprintf("GET http://localhost\\@127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port), "400"},
		{"userinfo", fmt.Sprintf("GET http://user:pw@127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port), "403"},
		{"uppercase scheme to the daemon", fmt.Sprintf("GET HTTP://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", dp), "403"},
		{"mixed-case scheme to the daemon", fmt.Sprintf("GET hTtP://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", dp), "403"},
		{"connect without port", "CONNECT localhost HTTP/1.1\r\nHost: localhost\r\n\r\n", "400"},
		{"connect empty port", "CONNECT localhost: HTTP/1.1\r\nHost: localhost:\r\n\r\n", "400"},
		{"connect port 0", "CONNECT 127.0.0.1:0 HTTP/1.1\r\nHost: 127.0.0.1:0\r\n\r\n", "400"},
		{"connect port too big", "CONNECT 127.0.0.1:65536 HTTP/1.1\r\nHost: 127.0.0.1:65536\r\n\r\n", "400"},
		{"connect signed port", fmt.Sprintf("CONNECT 127.0.0.1:+%d HTTP/1.1\r\nHost: 127.0.0.1:+%d\r\n\r\n", live.port, live.port), "400"},
		{"connect userinfo", fmt.Sprintf("CONNECT user@127.0.0.1:%d HTTP/1.1\r\nHost: user@127.0.0.1:%d\r\n\r\n", live.port, live.port), "400"},
		{"connect path suffix", fmt.Sprintf("CONNECT 127.0.0.1/x:%d HTTP/1.1\r\nHost: 127.0.0.1/x:%d\r\n\r\n", live.port, live.port), "400"},
		{"connect query", fmt.Sprintf("CONNECT 127.0.0.1:%d?x HTTP/1.1\r\nHost: 127.0.0.1:%d?x\r\n\r\n", live.port, live.port), "400"},
		{"duplicate content-length", fmt.Sprintf("POST http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\nab", live.port), "400"},
		{"oversized headers", "GET http://localhost:1/ HTTP/1.1\r\nHost: x\r\nX: " + strings.Repeat("a", 64<<10) + "\r\n\r\n", "431"},
	}
	for _, c := range cases {
		if got := status(rig.raw(t, c.req)); got != c.want {
			t.Errorf("%s: status %q, want %s", c.name, got, c.want)
		}
	}
	if resp := rig.raw(t, "\x00\x01garbage\r\n\r\n"); strings.Contains(resp, " 200 ") {
		t.Errorf("garbage served: %q", resp)
	}
	if live.hits.Load() != 0 || rig.daemon.hits.Load() != 0 || len(rig.dials.list()) != 0 {
		t.Fatalf("a malformed request must never be forwarded: live=%d daemon=%d dials=%v", live.hits.Load(), rig.daemon.hits.Load(), rig.dials.list())
	}
}

func TestProxyValidConnectTarget(t *testing.T) {
	good := []string{"127.0.0.1:80", "localhost:3000", "[::1]:8080", "example.com:443", "a.b.localhost:65535", "127.0.0.1:0080"}
	bad := []string{"", "localhost", "localhost:", ":80", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:+80", "127.0.0.1:-1", "127.0.0.1:8x",
		"127.0.0.1/x:9", "127.0.0.1:9/x", "u@127.0.0.1:9", "127.0.0.1:9?x", "127.0.0.1:9#x", "a\\b:9", "a b:9", "[::1:80", "::1:80"}
	for _, g := range good {
		if !validConnectTarget(g) {
			t.Errorf("%q must be valid", g)
		}
	}
	for _, b := range bad {
		if validConnectTarget(b) {
			t.Errorf("%q must be invalid", b)
		}
	}
}

// The request target decides, never the Host header.
func TestProxyHostHeaderCannotRedirectTheTarget(t *testing.T) {
	var gotHost string
	live := newHitServer(t, func(w http.ResponseWriter, r *http.Request) { gotHost = r.Host; fmt.Fprint(w, "live") })
	rig := newProxyRig(t, nil, proxyResolver, nil)
	resp := rig.raw(t, fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", live.port, rig.daemon.port))
	if status(resp) != "200" || rig.daemon.hits.Load() != 0 || live.hits.Load() != 1 {
		t.Fatalf("status %s daemon=%d live=%d", status(resp), rig.daemon.hits.Load(), live.hits.Load())
	}
	if gotHost != fmt.Sprintf("127.0.0.1:%d", live.port) {
		t.Fatalf("the upstream must see the vetted host, not the client's Host header: %q", gotHost)
	}
	// And the other way round: a Host header naming an allowed server cannot
	// make a request for the daemon's port acceptable.
	resp = rig.raw(t, fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", rig.daemon.port, live.port))
	if status(resp) != "403" || rig.daemon.hits.Load() != 0 {
		t.Fatalf("daemon target with an allowed Host header: %s hits=%d", status(resp), rig.daemon.hits.Load())
	}
}

func TestProxyDeniedSpellingsAreRefusedForBothForms(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, nil)
	dp := rig.daemon.port
	for _, h := range []string{"LocalHost.", "LOCALHOST", "localhost.", "App.LocalHost", "127.1", "0x7f.1", "0177.0.0.1", "2130706433", "0x7f000001",
		"[::ffff:127.0.0.1]", "[::ffff:7f00:1]", "[0:0:0:0:0:0:0:1]", "[::1]", "127.0.0.1.", "127.000.000.001"} {
		if got := status(rig.raw(t, fmt.Sprintf("GET http://%s:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", h, dp))); got != "403" {
			t.Errorf("forward to %s: %q", h, got)
		}
		if got := status(rig.raw(t, fmt.Sprintf("CONNECT %s:%d HTTP/1.1\r\nHost: x\r\n\r\n", h, dp))); got != "403" {
			t.Errorf("CONNECT %s: %q", h, got)
		}
	}
	if rig.daemon.hits.Load() != 0 || len(rig.dials.list()) != 0 {
		t.Fatalf("daemon=%d dials=%v", rig.daemon.hits.Load(), rig.dials.list())
	}
}

// Requests on one connection are judged one by one: a refusal does not poison
// or free the next, and a body that looks like a request is just a body.
func TestProxyPipelinedAndSmuggledRequestsAreJudgedIndividually(t *testing.T) {
	var gotBody atomic.Value
	live := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(string(b))
		fmt.Fprint(w, "live")
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	fmt.Fprintf(c, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", rig.daemon.port)
	fmt.Fprintf(c, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port)
	br := bufio.NewReader(c)
	var got []string
	for i := 0; i < 2; i++ {
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatalf("response %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		got = append(got, fmt.Sprint(resp.StatusCode))
	}
	if got[0] != "403" || got[1] != "200" || live.hits.Load() != 1 || rig.daemon.hits.Load() != 0 {
		t.Fatalf("statuses %v live=%d daemon=%d", got, live.hits.Load(), rig.daemon.hits.Load())
	}

	// A body that is itself a request for the daemon is forwarded as a body.
	smuggled := fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", rig.daemon.port)
	rig.raw(t, fmt.Sprintf("POST http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n%s", live.port, len(smuggled), smuggled))
	if rig.daemon.hits.Load() != 0 {
		t.Fatalf("a request smuggled inside a body reached the daemon")
	}
	if gotBody.Load() != smuggled {
		t.Fatalf("the body must be forwarded verbatim as a body: %q", gotBody.Load())
	}
	// Transfer-Encoding with a Content-Length: whatever the server decides, nothing reaches the daemon.
	rig.raw(t, fmt.Sprintf("POST http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nContent-Length: 4\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n%s", live.port, smuggled))
	if rig.daemon.hits.Load() != 0 {
		t.Fatalf("a TE+CL smuggling attempt reached the daemon")
	}
}

func TestProxyOversizedUpstreamHeaderIs502(t *testing.T) {
	big := holdServerFunc(t, func(c net.Conn) {
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = bufio.NewReader(c).ReadString('\n')
		_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nX-Big: "+strings.Repeat("a", 70<<10)+"\r\nContent-Length: 0\r\n\r\n")
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	if got := status(rig.raw(t, "GET http://"+big.Addr().String()+"/ HTTP/1.1\r\nHost: x\r\n\r\n")); got != "502" {
		t.Fatalf("an upstream header over 64 KB must be a clean 502, got %q", got)
	}
}

// holdServerFunc serves each accepted connection with fn.
func holdServerFunc(t *testing.T, fn func(net.Conn)) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go fn(c)
		}
	}()
	return ln
}

func TestProxyConnectionRefusedIs502AndOneNotice(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens any more
	rig := newProxyRig(t, nil, proxyResolver, nil)
	if got := status(rig.raw(t, "GET http://"+addr+"/ HTTP/1.1\r\nHost: x\r\n\r\n")); got != "502" {
		t.Fatalf("forward: %q", got)
	}
	rig.mu.Lock()
	n := len(rig.refuse)
	rig.mu.Unlock()
	if n != 1 {
		t.Fatalf("one notice per failed connection, got %d", n)
	}
	if status, _, _ := rig.connect(t, addr); !strings.Contains(status, "502") {
		t.Fatalf("CONNECT: %q", status)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if len(rig.refuse) != 2 || !strings.Contains(rig.refuse[1], "could not connect") {
		t.Fatalf("notices: %v", rig.refuse)
	}
}

func failDial(rec *dialRecorder) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		rec.mu.Lock()
		rec.addrs = append(rec.addrs, addr)
		rec.mu.Unlock()
		return nil, fmt.Errorf("refused")
	}
}

// An allowlisted host does not make a reserved port reachable: a wildcard
// bind of the daemon reserves its port on every address.
func TestProxyAllowlistedHostCannotReachADeniedPort(t *testing.T) {
	hs := newHitServer(t, nil)
	rec := &dialRecorder{}
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) {
		pol, err := NewPolicy([]string{"93.184.216.34"}, []Endpoint{{Host: anyV4, Port: hs.port}})
		if err != nil {
			t.Fatal(err)
		}
		c.Policy = pol
		c.Dial = failDial(rec)
	})
	if got := status(rig.raw(t, fmt.Sprintf("GET http://93.184.216.34:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", hs.port))); got != "403" {
		t.Fatalf("allowlisted host, denied port: %q", got)
	}
	if status, _, _ := rig.connect(t, fmt.Sprintf("93.184.216.34:%d", hs.port)); !strings.Contains(status, "403") {
		t.Fatalf("CONNECT allowlisted host, denied port: %q", status)
	}
	if len(rec.list()) != 0 {
		t.Fatalf("nothing may be dialled for a denied port: %v", rec.list())
	}
	// The same host on another port is reachable (the dial is attempted).
	_ = status(rig.raw(t, "GET http://93.184.216.34:8080/ HTTP/1.1\r\nHost: x\r\n\r\n"))
	if got := rec.list(); len(got) != 1 || got[0] != "93.184.216.34:8080" {
		t.Fatalf("dials: %v", got)
	}
}

func TestProxyDialsIPv6OnlyAnswers(t *testing.T) {
	rec := &dialRecorder{}
	res := fakeResolver{"v6.test": {"2606:4700::1111"}}
	rig := newProxyRig(t, []string{"v6.test"}, res, func(c *ProxyConfig) { c.Dial = failDial(rec) })
	_ = rig.raw(t, "GET http://v6.test/ HTTP/1.1\r\nHost: x\r\n\r\n")
	if got := rec.list(); len(got) != 1 || got[0] != "[2606:4700::1111]:80" {
		t.Fatalf("an IPv6-only name must be dialled on its vetted IPv6 address: %v", got)
	}
}

func TestProxyHoldsSixtyFourConcurrentTunnels(t *testing.T) {
	sink := holdServer(t)
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.HalfCloseGrace = 200 * time.Millisecond }) // the default 64-connection limit
	var conns []net.Conn
	for i := 0; i < DefaultProxyMaxConns; i++ {
		st, c, _ := rig.connect(t, sink.Addr().String())
		if !strings.Contains(st, "200") {
			t.Fatalf("tunnel %d of %d: %q", i+1, DefaultProxyMaxConns, st)
		}
		conns = append(conns, c)
	}
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if line, _ := bufio.NewReader(c).ReadString('\n'); !strings.Contains(line, "503") {
		t.Fatalf("the 65th connection must be told 503, got %q", line)
	}
	_ = conns[0].Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, c2, _ := rig.connect(t, sink.Addr().String()); strings.Contains(st, "200") {
			_ = c2.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("closing a tunnel must free a slot")
}

// An idle keep-alive connection from the browser must not hold the cap.
func TestProxyIdleClientKeepAliveReleasesItsSlot(t *testing.T) {
	live := newHitServer(t, nil)
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.MaxConns = 1; c.ClientIdle = 200 * time.Millisecond })
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	fmt.Fprintf(c, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port) // keep-alive, then silence
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if st := status(rig.raw(t, fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port))); st != "503" {
		t.Fatalf("while the idle connection holds the only slot: %q", st)
	}
	time.Sleep(500 * time.Millisecond)
	if st := status(rig.raw(t, fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port))); st != "200" {
		t.Fatalf("after the client idle timeout the slot must be free: %q", st)
	}
}

// A long response (server-sent events, a long poll, a big download) must not
// be cut at the proxy's IO window just because the request body was read under
// a deadline.
func TestProxyLongResponseSurvivesTheIOWindow(t *testing.T) {
	stream := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 12; i++ {
			fmt.Fprintf(w, "data: %d\n\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
	}
	live := newHitServer(t, stream)
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.IOTimeout = 300 * time.Millisecond })
	for _, method := range []string{"GET", "POST"} {
		body := ""
		if method == "POST" {
			body = "hello"
		}
		start := time.Now()
		resp := rig.raw(t, fmt.Sprintf("%s http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n%s", method, live.port, len(body), body))
		if !strings.Contains(resp, "data: 11") || time.Since(start) < time.Second {
			t.Fatalf("%s: the stream (1.2 s, over the 300 ms IO window) was cut after %v: %q", method, time.Since(start), resp)
		}
	}
}

func TestProxyEmptyBodyIsNotSentChunked(t *testing.T) {
	type seen struct {
		method string
		cl     int64
		te     []string
	}
	got := make(chan seen, 4)
	live := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.Method, r.ContentLength, r.TransferEncoding}
	})
	rig := newProxyRig(t, nil, proxyResolver, nil)
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		rig.raw(t, fmt.Sprintf("%s http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n", m, live.port))
		s := <-got
		if s.method != m || s.cl != 0 || len(s.te) != 0 {
			t.Errorf("%s with Content-Length 0 reached the upstream as %+v (it must not become chunked)", m, s)
		}
	}
}

// The ADR's "no upstream connection reuse": a second request dials again and
// is vetted again (the keep-alive that remains is browser-to-proxy only).
func TestProxyReportsNoUpstreamReuseAcrossRequestsOnOneClientConnection(t *testing.T) {
	live := newHitServer(t, nil)
	rig := newProxyRig(t, nil, proxyResolver, nil)
	c, err := net.Dial("tcp", rig.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	br := bufio.NewReader(c)
	for i := 0; i < 3; i++ {
		fmt.Fprintf(c, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", live.port)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	if n := len(rig.dials.list()); n != 3 {
		t.Fatalf("three requests on one client connection must dial upstream three times, dialled %d", n)
	}
}

// A browser that is done closes its end; an upstream that never answers the
// close must not hold the slot for the whole idle timeout.
func TestProxyClientCloseFreesTheSlotWithinTheGrace(t *testing.T) {
	sink := holdServer(t) // never closes its side
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) {
		c.MaxConns = 1
		c.HalfCloseGrace = 200 * time.Millisecond
		c.TunnelIdle = time.Hour
	})
	_, c, _ := rig.connect(t, sink.Addr().String())
	_ = c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, c2, _ := rig.connect(t, sink.Addr().String()); strings.Contains(st, "200") {
			_ = c2.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("a closed client must not pin a slot for the whole idle timeout")
}
