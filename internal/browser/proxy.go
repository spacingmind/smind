package browser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The enforcing proxy (ADR-0061 amendment 2026-11-15). Every network
// connection the browser makes goes through it, whatever the scheme: http as an
// absolute-form request, https, WebSocket and the rest as CONNECT host:port
// (measured on Chrome). Fetch interception cannot see WebSocket handshakes, so
// this is the one place that makes the policy true for all of them.
//
// One decision per connection: the target is parsed by the same code as a URL,
// checked against the Policy, its name is resolved HERE, every resolved
// address is vetted, and the connection is dialled to the vetted IP, never to
// the name (a name that rebinds afterwards cannot redirect an open connection).
// TLS is tunnelled, never intercepted.

// Proxy limits (ADR-0061 section 8 and the proxy design).
const (
	DefaultProxyMaxConns    = 64
	DefaultProxyDialTimeout = 10 * time.Second // per address
	proxyIOTimeout          = 60 * time.Second // forward path: no read or write may stall longer
	DefaultProxyTunnelIdle  = 5 * time.Minute
	proxyHeaderTimeout      = 10 * time.Second
	proxyResponseTimeout    = 30 * time.Second
	proxyMaxHeaderBytes     = 32 << 10
	proxyMaxRespHeader      = 64 << 10
)

// ProxyConfig configures a Proxy. Policy is required; the rest default.
type ProxyConfig struct {
	Policy      Policy
	Resolver    Resolver // nil: the system resolver
	MaxConns    int
	DialTimeout time.Duration
	TunnelIdle  time.Duration
	// HalfCloseGrace is how long a tunnel may stay open after one side closed its
	// end (default 10 s): a browser that is done closes fully, and an upstream that
	// never answers the close must not hold the slot for the whole idle timeout.
	HalfCloseGrace time.Duration
	// ClientIdle closes a browser-to-proxy keep-alive connection that has sent
	// no request for this long (default 5 s), so idle pooled sockets cannot hold
	// the connection cap.
	ClientIdle time.Duration
	// IOTimeout bounds any single read or write on the forward path (default 60 s).
	IOTimeout time.Duration
	// Dial opens the connection to a vetted IP:port (a test hook).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// OnRefuse is told about each refused target (cleaned text, no page data).
	OnRefuse func(target, reason string)
	// Secret is the per-session Proxy-Authorization secret. Empty: a random
	// 128-bit one is generated (the normal case; a test may pin it). The proxy
	// serves nobody who does not present it.
	Secret string
	// AuthFailuresPerMinute cuts a client that presents a WRONG credential more
	// often than this (default 20): the connection is closed without an answer. A
	// request with no credential at all is never cut (the browser's first request
	// has none) and always gets the 407.
	AuthFailuresPerMinute int
}

// Proxy is a loopback HTTP proxy that enforces a Policy.
type Proxy struct {
	cfg    ProxyConfig
	policy Policy
	ln     *limitListener
	srv    *http.Server
	tr     *http.Transport

	secretSum [sha256.Size]byte // sha256("smind:" + secret): what a valid credential hashes to
	secret    string

	mu     sync.Mutex
	tunnel map[net.Conn]struct{}
	closed bool

	authMu       sync.Mutex
	authWindow   time.Time
	authFailures int
	authNoted    bool
}

type pinKey struct{}

// StartProxy listens on 127.0.0.1:0. The proxy's own address is added to the
// policy's deny list, so a page cannot talk to the proxy as a URL.
func StartProxy(cfg ProxyConfig) (*Proxy, error) {
	if cfg.Resolver == nil {
		cfg.Resolver = net.DefaultResolver
	}
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = DefaultProxyMaxConns
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DefaultProxyDialTimeout
	}
	if cfg.TunnelIdle <= 0 {
		cfg.TunnelIdle = DefaultProxyTunnelIdle
	}
	if cfg.HalfCloseGrace <= 0 {
		cfg.HalfCloseGrace = 10 * time.Second
	}
	if cfg.ClientIdle <= 0 {
		cfg.ClientIdle = 5 * time.Second
	}
	if cfg.IOTimeout <= 0 {
		cfg.IOTimeout = proxyIOTimeout
	}
	if cfg.Dial == nil {
		d := &net.Dialer{}
		cfg.Dial = d.DialContext
	}
	if cfg.AuthFailuresPerMinute <= 0 {
		cfg.AuthFailuresPerMinute = 20
	}
	if cfg.Secret == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, fmt.Errorf("browser: proxy secret: %w", err)
		}
		cfg.Secret = hex.EncodeToString(b[:])
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("browser: proxy listen: %w", err)
	}
	port := raw.Addr().(*net.TCPAddr).Port
	p := &Proxy{cfg: cfg, tunnel: map[net.Conn]struct{}{}, secret: cfg.Secret}
	p.secretSum = sha256.Sum256([]byte(proxyAuthUser + ":" + cfg.Secret))
	p.cfg.Secret = "" // one copy, unexported: nothing that prints the config can show it
	p.policy = cfg.Policy
	p.policy.Deny = append(append([]Endpoint(nil), cfg.Policy.Deny...), Endpoint{Host: "127.0.0.1", Port: port})
	p.ln = &limitListener{Listener: raw, sem: make(chan struct{}, cfg.MaxConns)}
	// The transport dials the address the policy vetted (carried in the
	// context), never the name in the URL, and never reuses a connection.
	p.tr = &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		ResponseHeaderTimeout:  proxyResponseTimeout,
		MaxResponseHeaderBytes: proxyMaxRespHeader,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			addrs, _ := ctx.Value(pinKey{}).([]string)
			c, err := p.dialAny(ctx, addrs)
			if err != nil {
				return nil, err
			}
			return idleConn{c, p.cfg.IOTimeout}, nil // a stalled upstream cannot hold a slot
		},
	}
	p.srv = &http.Server{
		Handler:           http.HandlerFunc(p.handle),
		ReadHeaderTimeout: proxyHeaderTimeout,
		MaxHeaderBytes:    proxyMaxHeaderBytes,
		IdleTimeout:       cfg.ClientIdle,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = p.srv.Serve(p.ln) }()
	return p, nil
}

// proxyAuthUser is the fixed user name of the proxy credential; the secret is
// the password.
const proxyAuthUser = "smind"

// Secret is the per-session credential the session hands to the browser
// through CDP (Fetch.authRequired). It is never logged, never in an argument
// list, and not in the profile.
func (p *Proxy) Secret() string { return p.secret }

// String and GoString keep the secret out of any formatted output of a Proxy.
func (p *Proxy) String() string   { return "browser.Proxy(" + p.ln.Addr().String() + ")" }
func (p *Proxy) GoString() string { return p.String() }

// authorized reports whether the request carries the valid credential. Both
// sides are hashed first so the comparison is constant time whatever the
// length of what the client sent.
func (p *Proxy) authorized(r *http.Request) bool {
	h := r.Header.Get("Proxy-Authorization")
	scheme, rest, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
	if err != nil {
		return false
	}
	sum := sha256.Sum256(dec)
	return subtle.ConstantTimeCompare(sum[:], p.secretSum[:]) == 1
}

// authFailure counts a failed attempt and reports whether the client is over
// the limit (then it is cut, and the agent is told once).
func (p *Proxy) authFailure() (cut bool) {
	now := time.Now()
	p.authMu.Lock()
	if now.Sub(p.authWindow) > time.Minute {
		p.authWindow, p.authFailures = now, 0
	}
	p.authFailures++
	cut = p.authFailures > p.cfg.AuthFailuresPerMinute
	first := cut && !p.authNoted
	if first {
		p.authNoted = true
	}
	p.authMu.Unlock()
	if first && p.cfg.OnRefuse != nil {
		p.cfg.OnRefuse("proxy", "requests with a wrong proxy credential were refused and cut")
	}
	return cut
}

// Addr is the proxy's host:port, for --proxy-server.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Policy is the session policy plus the proxy's own endpoint in the deny list.
func (p *Proxy) Policy() Policy { return p.policy }

// Close stops the listener and closes every open connection and tunnel.
func (p *Proxy) Close() {
	p.mu.Lock()
	p.closed = true
	conns := make([]net.Conn, 0, len(p.tunnel))
	for c := range p.tunnel {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	_ = p.srv.Close()
	for _, c := range conns {
		_ = c.Close()
	}
	p.tr.CloseIdleConnections()
}

func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.tunnel[c] = struct{}{}
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.tunnel, c)
	p.mu.Unlock()
}

// vet is the single policy check, shared by every scheme: it returns the
// vetted addresses (as ip:port strings) the connection may be dialled to.
func (p *Proxy) vet(ctx context.Context, rawURL string) (addrs []string, d Decision) {
	d = p.policy.CheckURL(rawURL)
	if !d.Allowed {
		return nil, d
	}
	var ips []netip.Addr
	if d.loopbackName {
		// RFC 6761: localhost and *.localhost are loopback by definition. No DNS
		// query for a page-chosen name (it could leave the machine), and no
		// resolver to disagree.
		ips = []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}
	} else if d.NeedsResolve {
		lctx, cancel := context.WithTimeout(ctx, p.cfg.DialTimeout)
		defer cancel()
		found, err := p.cfg.Resolver.LookupNetIP(lctx, "ip", d.Host)
		if err != nil {
			return nil, refuse("host %q did not resolve", d.Host)
		}
		if d = p.policy.CheckResolved(d, found); !d.Allowed {
			return nil, d
		}
		ips = found
	} else {
		ip, err := netip.ParseAddr(d.Host)
		if err != nil {
			return nil, refuse("host %q is not an address", d.Host)
		}
		ips = []netip.Addr{ip}
	}
	for _, ip := range ips {
		addrs = append(addrs, net.JoinHostPort(ip.Unmap().String(), strconv.Itoa(d.Port)))
	}
	return addrs, d
}

func (p *Proxy) dialAny(ctx context.Context, addrs []string) (net.Conn, error) {
	if len(addrs) == 0 {
		return nil, errors.New("browser: no vetted address to dial")
	}
	var last error
	for _, a := range addrs {
		// Each address gets its own timeout, so a blackholed first address does
		// not use up the budget of the next.
		dctx, cancel := context.WithTimeout(ctx, p.cfg.DialTimeout)
		c, err := p.cfg.Dial(dctx, "tcp", a)
		cancel()
		if err == nil {
			return c, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

// reportFailure tells the agent about a connection that was allowed but failed
// (nothing listens, a timeout), once per target through OnRefuse's dedup.
func (p *Proxy) reportFailure(target, reason string) {
	if p.cfg.OnRefuse != nil {
		p.cfg.OnRefuse(redact(target), reason)
	}
}

func (p *Proxy) refuseWith(w http.ResponseWriter, code int, target, reason string) {
	reason = cleanText(reason, 200)
	if p.cfg.OnRefuse != nil {
		p.cfg.OnRefuse(redact(target), reason) // no query, no userinfo, sanitised
	}
	http.Error(w, "refused by the browser policy: "+reason, code)
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request) {
	// Authentication comes first, before the target is even parsed: nothing a
	// process without the session secret sends reaches the policy, the resolver
	// or a dial.
	if !p.authorized(r) {
		// Only a request that PRESENTS a wrong credential counts toward the cut.
		// The browser's first request on a fresh connection carries none (it has
		// no credential cached yet) and must always get the 407 that makes it ask,
		// so another local process can never lock the browser out by spamming.
		if r.Header.Get("Proxy-Authorization") != "" && p.authFailure() {
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					_ = c.Close()
					return
				}
			}
		}
		w.Header().Set("Proxy-Authenticate", `Basic realm="smind"`)
		w.Header().Set("Connection", "close")
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleForward(w, r)
}

// validConnectTarget accepts only a bare host and a numeric port in 1..65535:
// no path, query, fragment, userinfo, backslash or whitespace, and no empty
// port (SplitHostPort alone accepts "127.0.0.1/x:9" and "host:").
func validConnectTarget(t string) bool {
	if t == "" || strings.ContainsAny(t, "/?#@\\ \t\r\n") {
		return false
	}
	host, port, err := net.SplitHostPort(t)
	if err != nil || host == "" || port == "" {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.RequestURI
	if !validConnectTarget(target) {
		http.Error(w, "CONNECT needs host:port", http.StatusBadRequest)
		return
	}
	addrs, d := p.vet(r.Context(), "http://"+target+"/")
	if !d.Allowed {
		p.refuseWith(w, http.StatusForbidden, target, d.Reason)
		return
	}
	up, err := p.dialAny(r.Context(), addrs)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}
		p.reportFailure(target, "could not connect to that address")
		http.Error(w, "could not connect", code)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	client, rw, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		return
	}
	okClient, okUp := p.track(client), p.track(up)
	defer p.untrack(client)
	defer p.untrack(up)
	if !okClient || !okUp {
		_ = client.Close()
		_ = up.Close()
		return
	}
	_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	var early []byte // bytes the client already sent behind the CONNECT head
	if n := rw.Reader.Buffered(); n > 0 {
		early, _ = rw.Reader.Peek(n)
		early = append([]byte(nil), early...)
	}
	p.pipe(client, early, up)
}

// pipe copies both ways until both directions end or the tunnel has moved no
// data in either direction for TunnelIdle (one shared clock: an upload-only
// tunnel is alive while the client sends, a silent one is closed on both sides).
// An error, a timeout included, closes both connections; a clean EOF half-closes.
func (p *Proxy) pipe(client net.Conn, early []byte, up net.Conn) {
	idle := p.cfg.TunnelIdle
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	touch := func() { last.Store(time.Now().UnixNano()) }
	expired := func() bool { return time.Since(time.Unix(0, last.Load())) >= idle }

	var wg, wgDone sync.WaitGroup
	wg.Add(2)
	wgDone.Add(2)
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = client.Close(); _ = up.Close() }) }
	cp := func(dst, src net.Conn, pre []byte) {
		defer wg.Done()
		defer wgDone.Done()
		if len(pre) > 0 {
			if _, err := dst.Write(pre); err != nil {
				closeBoth()
				return
			}
			touch()
		}
		buf := make([]byte, 32<<10)
		for {
			_ = src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)
			if n > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(idle))
				if _, werr := dst.Write(buf[:n]); werr != nil {
					closeBoth()
					return
				}
				touch()
			}
			if err == nil {
				continue
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() && !expired() {
				continue // the other direction is moving: the tunnel is alive
			}
			if err == io.EOF {
				if cw, ok := dst.(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
					// The other direction may still finish, but not forever.
					grace := time.AfterFunc(p.cfg.HalfCloseGrace, closeBoth)
					go func() { wgDone.Wait(); grace.Stop() }()
					return
				}
			}
			closeBoth()
			return
		}
	}
	go cp(up, client, early)
	go cp(client, up, nil)
	wg.Wait()
	closeBoth()
}

// idleConn closes a connection that moves no data for idle.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c idleConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(b)
}

func (c idleConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

func (c idleConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.idle))
	return c.Conn.Write(b)
}

var hopByHop = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func stripHopByHop(h http.Header) {
	for _, tok := range strings.Split(h.Get("Connection"), ",") {
		if t := strings.TrimSpace(tok); t != "" {
			h.Del(t)
		}
	}
	for _, k := range hopByHop {
		h.Del(k)
	}
}

// handleForward serves an absolute-form http request (the only plain request
// a browser sends to a proxy).
func (p *Proxy) handleForward(w http.ResponseWriter, r *http.Request) {
	raw := r.RequestURI
	if !strings.HasPrefix(strings.ToLower(raw), "http://") {
		// https and ws(s) arrive as CONNECT; anything else is not proxied.
		p.refuseWith(w, http.StatusBadRequest, raw, "only http:// requests and CONNECT are proxied")
		return
	}
	if r.Header.Get("Upgrade") != "" {
		p.refuseWith(w, http.StatusNotImplemented, raw, "an upgrade must arrive as CONNECT")
		return
	}
	addrs, d := p.vet(r.Context(), raw)
	if !d.Allowed {
		p.refuseWith(w, http.StatusForbidden, raw, d.Reason)
		return
	}
	host := d.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if d.Port != 80 {
		host += ":" + strconv.Itoa(d.Port)
	}
	rc := http.NewResponseController(w)
	guard := &bodyGuard{rc: rc}
	defer guard.finish()
	var body io.ReadCloser = http.NoBody // an empty body stays empty: no chunked encoding, no deadline set
	var ib *idleBody
	if r.Body != nil && r.Body != http.NoBody {
		ib = &idleBody{r: r.Body, g: guard, idle: p.cfg.IOTimeout}
		body = ib
	}
	out, err := http.NewRequestWithContext(context.WithValue(r.Context(), pinKey{}, addrs), r.Method, "http://"+host+r.URL.RequestURI(), body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out.Header = r.Header.Clone()
	stripHopByHop(out.Header)
	out.Host = host
	out.ContentLength = r.ContentLength
	resp, err := p.tr.RoundTrip(out)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}
		p.reportFailure(raw, "could not connect to that address")
		http.Error(w, "upstream error", code)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 999 {
		http.Error(w, "upstream sent an unusable status", http.StatusBadGateway)
		return
	}
	stripHopByHop(resp.Header)
	if ib != nil && !ib.eof.Load() {
		// The upstream answered before the client finished its body. We will not
		// read the rest, so the connection cannot be reused: say so, which also
		// stops net/http from trying to drain the unread body (a stalled client
		// would hold the handler there).
		w.Header().Set("Connection", "close")
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(p.cfg.IOTimeout))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// deadliner is the part of http.ResponseController the body guard needs.
type deadliner interface{ SetReadDeadline(time.Time) error }

// bodyGuard serialises read-deadline changes on the client's connection and
// refuses them once the handler is finished: a late Read or Close from the
// transport (the upstream answered early, with the request body unfinished)
// must not clobber the deadline of the NEXT request on a keep-alive connection.
type bodyGuard struct {
	mu   sync.Mutex
	done bool
	rc   deadliner
}

func (g *bodyGuard) set(t time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.done {
		_ = g.rc.SetReadDeadline(t)
	}
}

// finish clears the deadline one last time and blocks every later change.
func (g *bodyGuard) finish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.done {
		_ = g.rc.SetReadDeadline(time.Time{})
		g.done = true
	}
}

// idleBody gives the client's request body a read deadline that is renewed on
// every read, so a client that stalls mid-body cannot hold a slot. (A client
// that drips one byte per window renews it forever: a known low, see the plan.)
type idleBody struct {
	r    io.ReadCloser
	g    *bodyGuard
	idle time.Duration
	eof  atomic.Bool // the client's body was read to its end
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.g.set(time.Now().Add(b.idle))
	n, err := b.r.Read(p)
	if err == io.EOF {
		b.eof.Store(true)
	}
	if err != nil {
		// The body is over (or broken): the deadline must not outlive it, or the
		// server's background read would time out and cancel the request context
		// of a long response (SSE, long poll, a big download).
		b.g.set(time.Time{})
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.g.set(time.Time{})
	return b.r.Close()
}

// limitListener caps concurrent connections: one over the limit is told 503
// and closed.
type limitListener struct {
	net.Listener
	sem chan struct{}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.sem <- struct{}{}:
			return &limitConn{Conn: c, release: func() { <-l.sem }}, nil
		default:
			go func() { // a slow client must not stall the accept loop
				_ = c.SetWriteDeadline(time.Now().Add(time.Second))
				_, _ = io.WriteString(c, "HTTP/1.1 503 Service Unavailable\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
				_ = c.Close()
			}()
		}
	}
}

type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
