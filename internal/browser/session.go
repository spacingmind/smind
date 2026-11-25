package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// maxQueuedBytes bounds the events waiting in the session queues.
const maxQueuedBytes int64 = 32 << 20

// seenSet remembers up to max keys and reports each once; when full it starts
// again, so a hostile page inventing targets cannot grow it or silence later
// notices for good (the per-kind notice cap bounds what reaches the agent).
type seenSet struct {
	mu  sync.Mutex
	m   map[string]struct{}
	max int
}

func (s *seenSet) first(k string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]struct{}{}
	}
	if _, ok := s.m[k]; ok {
		return false
	}
	if len(s.m) >= s.max {
		s.m = map[string]struct{}{} // a rolling window: later targets are still reported
	}
	s.m[k] = struct{}{}
	return true
}

// Session errors.
var (
	ErrSessionClosed    = errors.New("browser: the session is closed")
	ErrTooManySessions  = errors.New("browser: too many browser sessions are running (limit 2)")
	ErrTabLimit         = errors.New("browser: tab limit reached (3 tabs)")
	ErrNavigationDenied = errors.New("browser: navigation refused by policy")
)

// Limits (ADR-0061 section 8).
const (
	DefaultMaxTabs     = 3
	DefaultIdleTimeout = 5 * time.Minute
	DefaultCallTimeout = 20 * time.Second
	DefaultMaxSessions = 2
	maxConsoleLines    = 200
	maxConsoleLineLen  = 500
	maxNoticesPerKind  = 50
	maxDNSEntries      = 256
	requestWorkers     = 16
	requestQueue       = 4096
)

// Notice is something the session did on the page's behalf that the agent
// must be told about. Details never carry page content beyond a URL or a
// sanitised file name.
type Notice struct {
	Time   time.Time
	Kind   string // blocked, popup_refused, dialog, download
	Detail string
}

// Tab is one page of the session.
type Tab struct {
	ID    string
	URL   string
	Title string
}

// Config configures one workspace session.
type Config struct {
	Workspace   string
	Launcher    Launcher
	Launch      LaunchOpts
	Policy      Policy
	Resolver    Resolver // nil uses the system resolver
	MaxTabs     int
	IdleTimeout time.Duration
	CallTimeout time.Duration
	// OnClose runs once after the session ended (the manager uses it).
	OnClose func()
}

type tabState struct {
	targetID  string
	sessionID string
	seq       int
	// ready is closed once interception is on and the target was resumed; a
	// tab that is not ready counts against the pool but is never handed out.
	ready chan struct{}
}

func (t *tabState) isReady() bool {
	select {
	case <-t.ready:
		return true
	default:
		return false
	}
}

// Session is one headless browser for one workspace. It enforces the URL
// policy on every request, caps tabs, cancels dialogs and denies downloads.
type Session struct {
	cfg   Config
	proxy *Proxy
	inst  Instance
	cl    *Client

	mu       sync.Mutex
	closed   bool
	tabs     map[string]*tabState // by target id
	notices  []Notice
	console  []consoleLine
	idle     *time.Timer
	loads    map[string]chan struct{} // load waiters by cdp session id
	dns      map[string]dnsEntry
	tabSeq   int
	current  string                      // target id of the tab the agent works in ("" until the first navigation)
	snapRefs map[string]map[string]int64 // tab id -> ref ("e1") -> backend DOM node id, of the latest snapshot
	navMu    sync.Mutex                  // one navigation at a time: a load waiter is per tab

	lifecycle    chan Event   // attach, detach, dialog, download: handled in order
	authNoted    atomic.Bool  // the one notice about a refused credentials request
	authErrNoted atomic.Bool  // the one notice about an unanswered challenge
	requests     chan Event   // Fetch.requestPaused: handled by a bounded pool
	qbytes       atomic.Int64 // bytes waiting in the two queues
	maxQueued    int64        // the budget for qbytes (a field so a test can lower it)

	pages        pageTargets         // host:port of every request a page made (pageinit.go)
	refusedSeen  seenSet             // targets already reported to the agent
	armed        map[string]struct{} // refusals waiting out the grace (under mu)
	unattributed atomic.Int64        // proxy refusals no page asked for: counted, never reported
	grace        time.Duration       // how long a refusal waits for the page request that names it (a field so a test can shorten it)

	closeOnce sync.Once
}

type dnsEntry struct {
	addrs []netip.Addr
	err   error
	at    time.Time
}

// Start launches the browser and connects to it.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.Launcher == nil {
		return nil, errors.New("browser: no launcher")
	}
	if cfg.MaxTabs <= 0 {
		cfg.MaxTabs = DefaultMaxTabs
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = DefaultCallTimeout
	}
	if cfg.Resolver == nil {
		cfg.Resolver = net.DefaultResolver
	}
	var session atomic.Pointer[Session]
	px, err := StartProxy(ProxyConfig{
		Policy:   cfg.Policy,
		Resolver: cfg.Resolver,
		OnRefuse: func(target, reason string) {
			// The proxy also refuses the browser's own background traffic. The agent is
			// told once per target, and only about a target a page asked for
			// (pageinit.go): never a flood, never the browser's own noise.
			if sess := session.Load(); sess != nil {
				sess.refused(target, reason)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	cfg.Policy = px.Policy() // the proxy's own address is denied to the page too
	cfg.Launch.ProxyAddr = px.Addr()
	inst, err := cfg.Launcher.Launch(ctx, cfg.Launch)
	if err != nil {
		px.Close()
		return nil, err
	}
	cl := NewClient(inst.Conn())
	cl.SetWriteTimeout(cfg.CallTimeout)
	s := &Session{
		cfg:         cfg,
		proxy:       px,
		inst:        inst,
		cl:          cl,
		tabs:        map[string]*tabState{},
		loads:       map[string]chan struct{}{},
		dns:         map[string]dnsEntry{},
		snapRefs:    map[string]map[string]int64{},
		maxQueued:   maxQueuedBytes,
		refusedSeen: seenSet{max: 200},
		grace:       pageAttributionGrace,
		lifecycle:   make(chan Event, 256),
		requests:    make(chan Event, requestQueue),
	}
	session.Store(s)
	s.idle = time.AfterFunc(cfg.IdleTimeout, s.Close)
	go s.eventLoop()
	go s.lifecycleLoop()
	for i := 0; i < requestWorkers; i++ {
		go s.requestWorker()
	}
	go func() { <-cl.Done(); s.Close() }()

	cctx, cancel := context.WithTimeout(ctx, cfg.CallTimeout)
	defer cancel()
	if err := cl.Call(cctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "eventsEnabled": true}, nil); err != nil {
		s.Close()
		return nil, fmt.Errorf("browser: deny downloads: %w", err)
	}
	if err := cl.Call(cctx, "", "Target.setAutoAttach", autoAttachParams(), nil); err != nil {
		s.Close()
		return nil, fmt.Errorf("browser: attach: %w", err)
	}
	return s, nil
}

// cleanText makes page-controlled text safe to hand to the agent: control,
// format (bidi, zero width, tag characters), private-use and line-separator
// characters are dropped, and it is cut to max runes.
func cleanText(s string, max int) string {
	if len(s) > 4*max {
		s = s[:4*max] // bound the work before touching runes
	}
	out := make([]rune, 0, len(s))
	marks := 0
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			r = ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || r == 0x2028 || r == 0x2029 || r == 0xFFFD ||
			(r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF) {
			continue
		}
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) { // a flood of combining marks hides text
			marks++
			if marks > 2 {
				continue
			}
		} else {
			marks = 0
		}
		out = append(out, r)
		if len(out) >= max {
			out = append(out, '…')
			break
		}
	}
	return string(out)
}

func autoAttachParams() map[string]any {
	return map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}
}

// Close ends the session: the connection drops, the browser process tree is
// killed and an ephemeral profile is removed. It is idempotent.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.idle != nil {
			s.idle.Stop()
		}
		s.mu.Unlock()
		_ = s.inst.Close() // kill the browser first, then drop the connection
		_ = s.cl.Close()
		s.proxy.Close()
		if s.cfg.OnClose != nil {
			s.cfg.OnClose()
		}
	})
}

// Closed reports whether the session has ended.
func (s *Session) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// begin marks activity and rejects use after close.
func (s *Session) begin(ctx context.Context) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, ErrSessionClosed
	}
	s.idle.Reset(s.cfg.IdleTimeout)
	s.mu.Unlock()
	c, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	return c, cancel, nil
}

func (s *Session) note(kind, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// One kind can never push another kind out: a page that floods "blocked"
	// notices must not hide an earlier dialog, download or popup notice.
	n := 0
	first := -1
	for i, x := range s.notices {
		if x.Kind == kind {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	if n >= maxNoticesPerKind {
		s.notices = append(s.notices[:first], s.notices[first+1:]...)
	}
	s.notices = append(s.notices, Notice{Time: time.Now(), Kind: kind, Detail: detail})
}

// DrainNotices returns and clears the notices since the last call.
func (s *Session) DrainNotices() []Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.notices
	s.notices = nil
	return out
}

// consoleLine is one console line and the browser session (tab) it came from.
type consoleLine struct{ sess, text string }

// Console returns the recent console lines of every tab, oldest first.
func (s *Session) Console() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.console))
	for _, l := range s.console {
		out = append(out, l.text)
	}
	return out
}

func (s *Session) addConsole(sessID, line string) {
	line = cleanText(line, maxConsoleLineLen)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.console) >= maxConsoleLines {
		s.console = s.console[1:]
	}
	s.console = append(s.console, consoleLine{sess: sessID, text: line})
}

// ConsoleOf returns the recent console lines of one tab only (the tab's own
// CDP session), oldest first: a result labelled with that tab's origin must
// carry that tab's output and nothing else.
func (s *Session) ConsoleOf(tabID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tabs[tabID]
	if t == nil {
		return nil
	}
	var out []string
	for _, l := range s.console {
		if l.sess == t.sessionID {
			out = append(out, l.text)
		}
	}
	return out
}

// TabInfo is the sanitised URL and title of one tab.
func (s *Session) TabInfo(ctx context.Context, tabID string) (Tab, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return Tab{}, err
	}
	defer cancel()
	t, err := s.tabFor(tabID)
	if err != nil {
		return Tab{}, err
	}
	return s.tabInfo(ctx, t.targetID)
}

// OpenInNewTab opens a tab inside the pool and navigates it: for a second
// run sharing the workspace's browser, so it never takes over another run's tab.
func (s *Session) OpenInNewTab(ctx context.Context, rawURL string) (Tab, error) {
	// The policy first: a refused URL must not even cost a tab.
	if d := s.checkURL(ctx, rawURL); !d.Allowed {
		return Tab{}, fmt.Errorf("%w: %s", ErrNavigationDenied, d.Reason)
	}
	nt, err := s.NewTab(ctx)
	if err != nil {
		return Tab{}, err
	}
	t, err := s.Navigate(ctx, nt.ID, rawURL)
	if err != nil {
		_ = s.CloseTab(ctx, nt.ID)
		return Tab{}, err
	}
	return t, nil
}

func (s *Session) eventLoop() {
	for ev := range s.cl.Events() {
		s.cl.release(ev)
		switch ev.Method {
		case "Runtime.consoleAPICalled", "Runtime.exceptionThrown":
			s.onConsole(ev)
		case "Page.loadEventFired":
			s.mu.Lock()
			ch := s.loads[ev.SessionID]
			s.mu.Unlock()
			if ch != nil {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		case "Network.requestWillBeSent", "Network.webSocketCreated":
			s.notePageRequest(ev)
		case "Fetch.requestPaused", "Fetch.authRequired":
			if s.qbytes.Add(int64(ev.size)) > s.maxQueued {
				s.note("blocked", "request flood: the session was closed")
				go s.Close()
				continue
			}
			select {
			case s.requests <- ev:
			default:
				// A request flood the pool cannot keep up with: end the
				// session rather than let requests through unchecked.
				s.note("blocked", "request flood: the session was closed")
				go s.Close()
			}
		case "Target.attachedToTarget", "Target.detachedFromTarget", "Page.javascriptDialogOpening", "Browser.downloadWillBegin":
			if s.qbytes.Add(int64(ev.size)) > s.maxQueued {
				go s.Close()
				continue
			}
			select {
			case s.lifecycle <- ev:
			default:
				go s.Close() // cannot keep order any more: fail closed
			}
		}
	}
}

func (s *Session) requestWorker() {
	for {
		select {
		case ev := <-s.requests:
			s.qbytes.Add(-int64(ev.size))
			if ev.Method == "Fetch.authRequired" {
				s.onAuthRequired(ev)
				continue
			}
			s.onRequestPaused(ev)
		case <-s.cl.Done():
			return
		}
	}
}

// lifecycleLoop handles attach, detach, dialog and download events one at a
// time and in the order the browser sent them.
func (s *Session) lifecycleLoop() {
	for {
		select {
		case ev := <-s.lifecycle:
			s.qbytes.Add(-int64(ev.size))
			s.onEvent(ev)
		case <-s.cl.Done():
			return
		}
	}
}

func (s *Session) onEvent(ev Event) {
	switch ev.Method {
	case "Target.attachedToTarget":
		s.onAttached(ev)
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.mu.Lock()
			for id, t := range s.tabs {
				if t.sessionID == p.SessionID {
					delete(s.tabs, id)
				}
			}
			s.mu.Unlock()
		}
	case "Page.javascriptDialogOpening":
		s.onDialog(ev)
	case "Browser.downloadWillBegin":
		var p struct {
			Filename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.note("download", fmt.Sprintf("download blocked: %s", safeFileName(p.Filename)))
		}
	}
}

// safeFileName reduces a page-chosen file name to a short printable base name.
func safeFileName(n string) string {
	n = cleanText(path.Base(strings.ReplaceAll(cleanText(n, 400), "\\", "/")), 100)
	if n == "" || n == "." || n == "/" {
		return "(unnamed)"
	}
	return n
}

func (s *Session) bgCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.cfg.CallTimeout)
}

func (s *Session) onAttached(ev Event) {
	var p struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(ev.Params, &p) != nil || p.SessionID == "" {
		return
	}
	ctx, cancel := s.bgCtx()
	defer cancel()
	resume := func() {
		if p.WaitingForDebugger {
			_ = s.cl.Call(ctx, p.SessionID, "Runtime.runIfWaitingForDebugger", nil, nil)
		}
	}
	isPage := p.TargetInfo.Type == "page"
	var ts *tabState
	if isPage {
		s.mu.Lock()
		if _, known := s.tabs[p.TargetInfo.TargetID]; !known && len(s.tabs) >= s.cfg.MaxTabs {
			s.mu.Unlock()
			s.note("popup_refused", fmt.Sprintf("a new window was refused: at most %d tabs are allowed", s.cfg.MaxTabs))
			_ = s.cl.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
			return
		}
		s.tabSeq++
		ts = &tabState{targetID: p.TargetInfo.TargetID, sessionID: p.SessionID, seq: s.tabSeq, ready: make(chan struct{})}
		s.tabs[p.TargetInfo.TargetID] = ts
		s.mu.Unlock()
	}
	// Policy first: nothing of this target runs before Fetch is on, and a
	// target that cannot be fully set up is never resumed (and a page is
	// closed, on a fresh context: the one above may be spent).
	fail := func() {
		if isPage {
			s.mu.Lock()
			delete(s.tabs, p.TargetInfo.TargetID)
			s.mu.Unlock()
			fctx, fcancel := s.bgCtx()
			defer fcancel()
			_ = s.cl.Call(fctx, "", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
		}
	}
	steps := []struct {
		method string
		params any
		when   bool
	}{
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*"}}, "handleAuthRequests": true}, true},
		{"Target.setAutoAttach", autoAttachParams(), true}, // frames and workers of this target stay intercepted
		{"Page.enable", nil, isPage},
		{"Runtime.enable", nil, isPage},
	}
	for _, st := range steps {
		if !st.when {
			continue
		}
		if err := s.cl.Call(ctx, p.SessionID, st.method, st.params, nil); err != nil {
			s.note("blocked", fmt.Sprintf("a target could not be set up (%s) and was not started", st.method))
			fail()
			return
		}
	}
	// The Network domain only NAMES what a page requests (pageinit.go: a WebSocket reaches the
	// proxy and nothing else); a target that cannot enable it is still started, its refusals then
	// count as unattributed. Before the target runs: no request of it can precede the domain.
	// maxPostDataSize 0: request bodies are never carried in the events (only the URL is read).
	_ = s.cl.Call(ctx, p.SessionID, "Network.enable", map[string]any{"maxPostDataSize": 0}, nil)
	resume()
	if ts != nil {
		close(ts.ready)
	}
}

// onAuthRequired answers an authentication challenge of any attached target.
// The proxy's challenge (and only it: source Proxy AND the challenging origin is
// OUR proxy) gets the session secret. A challenge from anything else, a web
// site asking for credentials, a proxy at another address, is cancelled: the
// secret never goes to a page's origin.
func (s *Session) onAuthRequired(ev Event) {
	var p struct {
		RequestID     string `json:"requestId"`
		AuthChallenge struct {
			Source string `json:"source"`
			Origin string `json:"origin"`
		} `json:"authChallenge"`
	}
	if json.Unmarshal(ev.Params, &p) != nil || p.RequestID == "" {
		return
	}
	ctx, cancel := s.bgCtx()
	defer cancel()
	resp := map[string]any{"response": "CancelAuth"}
	if p.AuthChallenge.Source == "Proxy" && s.proxyOrigin(p.AuthChallenge.Origin) {
		resp = map[string]any{"response": "ProvideCredentials", "username": proxyAuthUser, "password": s.proxy.Secret()}
	} else if s.authNoted.CompareAndSwap(false, true) {
		// Once per session: a page can raise challenges without limit, and must
		// not push the agent's real refusal notices out.
		s.note("blocked", "a page asked for credentials and was refused (further requests are cancelled without a notice)")
	}
	if err := s.cl.Call(ctx, ev.SessionID, "Fetch.continueWithAuth", map[string]any{"requestId": p.RequestID, "authChallengeResponse": resp}, nil); err != nil &&
		!s.Closed() && s.authErrNoted.CompareAndSwap(false, true) {
		// Once per session and never for a closing session: a page that aborts
		// its 401 fetches must not push the agent's real notices out.
		s.note("blocked", "a proxy or site challenge could not be answered (further failures are not reported)")
	}
}

// proxyOrigin reports whether a challenge origin names this session's proxy.
func (s *Session) proxyOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" {
		return false
	}
	return u.Host == s.proxy.Addr()
}

func (s *Session) onRequestPaused(ev Event) {
	var p struct {
		RequestID    string `json:"requestId"`
		ResourceType string `json:"resourceType"`
		Request      struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(ev.Params, &p) != nil || p.RequestID == "" {
		return
	}
	s.pages.add(hostPortOrEmpty(p.Request.URL))
	ctx, cancel := s.bgCtx()
	defer cancel()
	d := s.checkRequest(ctx, p.Request.URL, p.ResourceType)
	if d.Allowed {
		_ = s.cl.Call(ctx, ev.SessionID, "Fetch.continueRequest", map[string]any{"requestId": p.RequestID}, nil)
		return
	}
	s.note("blocked", fmt.Sprintf("%s: %s", redact(p.Request.URL), cleanText(d.Reason, 200)))
	_ = s.cl.Call(ctx, ev.SessionID, "Fetch.failRequest", map[string]any{"requestId": p.RequestID, "errorReason": "BlockedByClient"}, nil)
}

// checkRequest applies the policy to one request of the page. data: and
// blob: are only ever allowed for subresources the page already holds the
// content of; as a document (top level or frame) they are refused like every
// other non-http(s) scheme.
func (s *Session) checkRequest(ctx context.Context, raw, resourceType string) Decision {
	lower := strings.ToLower(raw)
	if (strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:")) && resourceType != "Document" {
		return Decision{Allowed: true}
	}
	return s.checkURL(ctx, raw)
}

func (s *Session) checkURL(ctx context.Context, raw string) Decision {
	d := s.cfg.Policy.CheckURL(raw)
	if d.Allowed && d.loopbackName {
		return d // RFC 6761: no DNS query for a page-chosen *.localhost name
	}
	if !d.Allowed || !d.NeedsResolve {
		return d
	}
	return s.cfg.Policy.Check(ctx, raw, cachedResolver{s})
}

// cachedResolver resolves with a 2 second cache, bounded in size, so a page
// that loads hundreds of resources from one host costs one lookup and a page
// inventing host names cannot grow memory.
type cachedResolver struct{ s *Session }

func (c cachedResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	s := c.s
	s.mu.Lock()
	e, ok := s.dns[host]
	s.mu.Unlock()
	if ok && time.Since(e.at) < 2*time.Second {
		return e.addrs, e.err
	}
	addrs, err := s.cfg.Resolver.LookupNetIP(ctx, network, host)
	s.mu.Lock()
	if len(s.dns) >= maxDNSEntries {
		for k, v := range s.dns {
			if time.Since(v.at) >= 2*time.Second {
				delete(s.dns, k)
			}
		}
		if len(s.dns) >= maxDNSEntries {
			s.dns = map[string]dnsEntry{}
		}
	}
	s.dns[host] = dnsEntry{addrs: addrs, err: err, at: time.Now()}
	s.mu.Unlock()
	return addrs, err
}

// redact trims a URL for a notice: no query, no fragment, bounded length.
func redact(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			end = len(rest)
		}
		if at := strings.LastIndexByte(rest[:end], '@'); at >= 0 {
			u = u[:i+3] + rest[at+1:]
		}
	}
	return cleanText(u, 200)
}

func (s *Session) onDialog(ev Event) {
	var p struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(ev.Params, &p) != nil {
		return
	}
	accept := p.Type == "alert" // alert is dismissed; confirm, prompt and beforeunload are cancelled
	ctx, cancel := s.bgCtx()
	defer cancel()
	_ = s.cl.Call(ctx, ev.SessionID, "Page.handleJavaScriptDialog", map[string]any{"accept": accept}, nil)
	switch p.Type {
	case "alert":
		s.note("dialog", "an alert was dismissed")
	case "confirm", "prompt", "beforeunload":
		s.note("dialog", fmt.Sprintf("a %s dialog was cancelled", p.Type))
	default:
		s.note("dialog", "a dialog was cancelled")
	}
}

func (s *Session) onConsole(ev Event) {
	switch ev.Method {
	case "Runtime.consoleAPICalled":
		var p struct {
			Type string `json:"type"`
			Args []struct {
				Type        string          `json:"type"`
				Value       json.RawMessage `json:"value"`
				Description string          `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		parts := make([]string, 0, len(p.Args))
		for _, a := range p.Args {
			switch {
			case len(a.Value) > 0 && a.Type == "string":
				var v string
				_ = json.Unmarshal(a.Value, &v)
				parts = append(parts, v)
			case len(a.Value) > 0:
				parts = append(parts, string(a.Value))
			default:
				parts = append(parts, a.Description)
			}
		}
		s.addConsole(ev.SessionID, p.Type+": "+strings.Join(parts, " "))
	case "Runtime.exceptionThrown":
		var p struct {
			ExceptionDetails struct {
				Text string `json:"text"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.addConsole(ev.SessionID, "exception: "+p.ExceptionDetails.Text)
		}
	}
}

func (s *Session) firstTab() (*tabState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *tabState
	for _, t := range s.tabs {
		if t.isReady() && (best == nil || t.seq < best.seq) {
			best = t
		}
	}
	return best, best != nil
}

func (s *Session) waitTab(ctx context.Context, targetID string) (*tabState, error) {
	for {
		s.mu.Lock()
		t := s.tabs[targetID]
		s.mu.Unlock()
		if t != nil && t.isReady() {
			return t, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("browser: the new tab did not attach: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// NewTab opens a blank tab inside the pool.
func (s *Session) NewTab(ctx context.Context) (Tab, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return Tab{}, err
	}
	defer cancel()
	return s.newTab(ctx)
}

func (s *Session) newTab(ctx context.Context) (Tab, error) {
	s.mu.Lock()
	full := len(s.tabs) >= s.cfg.MaxTabs
	s.mu.Unlock()
	if full {
		return Tab{}, ErrTabLimit
	}
	var r struct {
		TargetID string `json:"targetId"`
	}
	if err := s.cl.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &r); err != nil {
		return Tab{}, err
	}
	if _, err := s.waitTab(ctx, r.TargetID); err != nil {
		return Tab{}, err
	}
	return Tab{ID: r.TargetID, URL: "about:blank"}, nil
}

// initialTabWait is how long the first navigation waits for the browser's own
// blank page to be armed before it opens a second tab (a var for tests).
var initialTabWait = 2 * time.Second

// waitInitialTab waits for the page the browser starts with, but only until no
// page has ever been seen: once the agent has had a tab, a missing one means
// it was closed and a new one is opened at once.
func (s *Session) waitInitialTab(ctx context.Context) (*tabState, bool) {
	s.mu.Lock()
	seen := s.tabSeq > 0
	s.mu.Unlock()
	if seen {
		return nil, false
	}
	deadline := time.Now().Add(initialTabWait)
	for time.Now().Before(deadline) {
		if t, ok := s.firstTab(); ok {
			return t, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(20 * time.Millisecond):
		}
	}
	return nil, false
}

// Navigate loads url in tab tabID (the first tab, created if none, when
// tabID is empty). The URL is checked before anything is sent; a refused URL
// never reaches the browser. Every request the page then makes, redirect hops
// included, is checked again by the Fetch interception.
func (s *Session) Navigate(ctx context.Context, tabID, rawURL string) (Tab, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return Tab{}, err
	}
	defer cancel()
	s.navMu.Lock()
	defer s.navMu.Unlock()
	if d := s.checkURL(ctx, rawURL); !d.Allowed {
		return Tab{}, fmt.Errorf("%w: %s", ErrNavigationDenied, d.Reason)
	}
	var t *tabState
	if tabID == "" {
		if ft, err := s.tabFor(""); err == nil {
			t = ft
		} else if ft, ok := s.waitInitialTab(ctx); ok {
			t = ft
		} else {
			nt, err := s.newTab(ctx)
			if err != nil {
				return Tab{}, err
			}
			var ok bool
			if t, ok = s.tabByID(nt.ID); !ok {
				return Tab{}, errors.New("browser: the new tab went away")
			}
		}
	} else if t, _ = s.tabByID(tabID); t == nil {
		return Tab{}, fmt.Errorf("browser: no such tab %q", tabID)
	}
	s.mu.Lock()
	s.current = t.targetID
	s.mu.Unlock()
	loaded := make(chan struct{}, 1)
	s.mu.Lock()
	s.loads[t.sessionID] = loaded
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.loads, t.sessionID)
		s.mu.Unlock()
	}()
	var nav struct {
		LoaderID  string `json:"loaderId"`
		ErrorText string `json:"errorText"`
	}
	if err := s.cl.Call(ctx, t.sessionID, "Page.navigate", map[string]any{"url": rawURL}, &nav); err != nil {
		return Tab{}, err
	}
	if nav.ErrorText != "" {
		return Tab{}, fmt.Errorf("browser: navigation failed: %s", nav.ErrorText)
	}
	if nav.LoaderID != "" { // empty for a same-document navigation: no load event follows
		select {
		case <-loaded:
		case <-ctx.Done():
			return Tab{}, fmt.Errorf("browser: the page did not finish loading: %w", ctx.Err())
		}
	}
	return s.tabInfo(ctx, t.targetID)
}

func (s *Session) tabByID(id string) (*tabState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tabs[id]
	if ok && !t.isReady() {
		return nil, false
	}
	return t, ok
}

func (s *Session) tabInfo(ctx context.Context, targetID string) (Tab, error) {
	var r struct {
		TargetInfo struct {
			TargetID string `json:"targetId"`
			URL      string `json:"url"`
			Title    string `json:"title"`
		} `json:"targetInfo"`
	}
	if err := s.cl.Call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": targetID}, &r); err != nil {
		return Tab{}, err
	}
	return cleanTab(Tab{ID: targetID, URL: r.TargetInfo.URL, Title: r.TargetInfo.Title}), nil
}

// cleanTab sanitises the page-controlled fields before they leave the package.
func cleanTab(t Tab) Tab {
	t.URL = cleanText(t.URL, 500)
	t.Title = cleanText(t.Title, 200)
	return t
}

// Tabs lists the pages of the session.
func (s *Session) Tabs(ctx context.Context) ([]Tab, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var r struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
			Title    string `json:"title"`
		} `json:"targetInfos"`
	}
	if err := s.cl.Call(ctx, "", "Target.getTargets", nil, &r); err != nil {
		return nil, err
	}
	var out []Tab
	for _, ti := range r.TargetInfos {
		if _, ok := s.tabByID(ti.TargetID); ok && ti.Type == "page" {
			out = append(out, cleanTab(Tab{ID: ti.TargetID, URL: ti.URL, Title: ti.Title}))
		}
	}
	// The tab the agent works in comes first: the broker marks it as current.
	s.mu.Lock()
	cur := s.current
	s.mu.Unlock()
	for i, t := range out {
		if t.ID == cur && i > 0 {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out, nil
}

// CloseTab closes one tab.
func (s *Session) CloseTab(ctx context.Context, tabID string) error {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if _, ok := s.tabByID(tabID); !ok {
		return fmt.Errorf("browser: no such tab %q", tabID)
	}
	if err := s.cl.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": tabID}, nil); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tabs, tabID)
	s.mu.Unlock()
	return nil
}

// Manager keeps at most one session per workspace and at most Max sessions
// daemon-wide.
type Manager struct {
	Max int // 0 means DefaultMaxSessions

	mu       sync.Mutex
	sessions map[string]*Session
	keys     map[string]string
	starting map[string]*startState
}

type startState struct {
	cancel context.CancelFunc
	closed bool
}

func (m *Manager) limit() int {
	if m.Max > 0 {
		return m.Max
	}
	return DefaultMaxSessions
}

// cfgKey identifies the parts of a Config a running session was built from; a
// changed policy, browser binary or limit must not be silently ignored.
func cfgKey(c Config) string {
	var allow []string
	for h := range c.Policy.AllowHosts {
		allow = append(allow, h)
	}
	sort.Strings(allow)
	var deny []string
	for _, d := range c.Policy.Deny {
		deny = append(deny, fmt.Sprintf("%s:%d", d.Host, d.Port))
	}
	sort.Strings(deny)
	la := append([]string(nil), c.Launch.AllowHosts...)
	sort.Strings(la)
	return fmt.Sprintf("%q|%q|%q|%d|%s|%d|%v|%v|%s", allow, deny, la, c.Launch.HeapMB, c.Launch.ProfileRoot, c.MaxTabs, c.IdleTimeout, c.CallTimeout, launcherKey(c.Launcher))
}

// launcherKey identifies a launcher: a value launcher (the browser path) by
// its contents, a pointer by identity (never read through, it may be in use).
func launcherKey(l Launcher) string {
	if l == nil {
		return "nil"
	}
	if reflect.TypeOf(l).Kind() == reflect.Pointer {
		return fmt.Sprintf("%T@%p", l, l)
	}
	return fmt.Sprintf("%T:%+v", l, l)
}

// ErrClosedWhileStarting is returned when a session was closed before Get
// could hand it out.
var ErrClosedWhileStarting = errors.New("browser: the session was closed while it was starting")

// Get returns the workspace's running session or starts one. If the running
// session was built from a different policy or launch configuration it is
// replaced: the new start is registered in the same lock hold that notices
// the staleness, so a concurrent Get or CloseAll always sees it.
func (m *Manager) Get(ctx context.Context, cfg Config) (*Session, error) {
	key := cfgKey(cfg)
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = map[string]*Session{}
		m.keys = map[string]string{}
		m.starting = map[string]*startState{}
	}
	if m.starting[cfg.Workspace] != nil {
		m.mu.Unlock()
		return nil, errors.New("browser: the session for this workspace is still starting")
	}
	var stale *Session
	if s := m.sessions[cfg.Workspace]; s != nil && !s.Closed() {
		if m.keys[cfg.Workspace] == key {
			m.mu.Unlock()
			return s, nil
		}
		stale = s
		delete(m.sessions, cfg.Workspace) // it no longer counts: it is being replaced
	}
	live := len(m.starting)
	for _, x := range m.sessions {
		if !x.Closed() {
			live++
		}
	}
	if live >= m.limit() {
		if stale != nil {
			m.sessions[cfg.Workspace] = stale // nothing changed
		}
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}
	sctx, cancel := context.WithCancel(ctx)
	st := &startState{cancel: cancel}
	m.starting[cfg.Workspace] = st
	m.mu.Unlock()
	defer cancel()

	if stale != nil {
		stale.Close()
	}

	var started *Session
	userClose := cfg.OnClose
	cfg.OnClose = func() {
		m.mu.Lock()
		if started != nil && m.sessions[cfg.Workspace] == started {
			delete(m.sessions, cfg.Workspace)
		}
		m.mu.Unlock()
		if userClose != nil {
			userClose()
		}
	}
	s, err := Start(sctx, cfg)
	m.mu.Lock()
	delete(m.starting, cfg.Workspace)
	closedMeanwhile := st.closed
	usable := err == nil && !closedMeanwhile && !s.Closed()
	if usable {
		started = s
		m.sessions[cfg.Workspace] = s
		m.keys[cfg.Workspace] = key
	}
	m.mu.Unlock()
	switch {
	case err != nil:
		return nil, err
	case !usable:
		s.Close() // idempotent; never hand out a closed session
		return nil, ErrClosedWhileStarting
	}
	return s, nil
}

// Close ends one workspace's session, or the one still starting (a run
// ended, the human stopped it, the switch was turned off).
func (m *Manager) Close(workspace string) {
	m.mu.Lock()
	s := m.sessions[workspace]
	if st := m.starting[workspace]; st != nil {
		st.closed = true
		st.cancel()
	}
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

// CloseAll ends every session, those still starting included (daemon
// shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	var all []*Session
	for _, s := range m.sessions {
		all = append(all, s)
	}
	for _, st := range m.starting {
		st.closed = true
		st.cancel()
	}
	m.mu.Unlock()
	for _, s := range all {
		s.Close()
	}
}

// Active reports how many sessions are running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, x := range m.sessions {
		if !x.Closed() {
			n++
		}
	}
	return n
}

func hostPortOrEmpty(raw string) string {
	hp, _ := hostPortOf(raw)
	return hp
}

// notePageRequest remembers the target a page named in a Network event.
func (s *Session) notePageRequest(ev Event) {
	var p struct {
		URL     string `json:"url"`
		Request struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(ev.Params, &p) != nil {
		return
	}
	if p.URL != "" {
		s.pages.add(hostPortOrEmpty(p.URL))
	}
	if p.Request.URL != "" {
		s.pages.add(hostPortOrEmpty(p.Request.URL))
	}
}

// refused handles one refusal by the proxy: it becomes a notice (once per target) only
// when a page asked for that target; otherwise it is counted as unattributed. The CDP
// event that names the page's request can trail the refusal, so a miss is checked again
// after a short grace.
func (s *Session) refused(target, reason string) {
	key, ok := hostPortOf(target)
	if !ok && !strings.Contains(target, "://") {
		// Not a request target at all (the proxy's own "wrong credential" notice): nothing a
		// page could have named, so nothing to attribute. Reported like before.
		if s.refusedSeen.first(target) {
			s.note("blocked", fmt.Sprintf("%s: %s", target, reason))
		}
		return
	}
	report := func() {
		if !s.refusedSeen.first(target) {
			return
		}
		s.note("blocked", fmt.Sprintf("%s: %s", target, reason))
	}
	if ok && s.pages.has(key) {
		report()
		return
	}
	// One timer per target at a time: a flood of the browser's own refusals arms 200 at most.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.armed == nil {
		s.armed = map[string]struct{}{}
	}
	if _, dup := s.armed[target]; dup {
		s.mu.Unlock()
		return
	}
	s.armed[target] = struct{}{}
	s.mu.Unlock()
	time.AfterFunc(s.grace, func() {
		s.mu.Lock()
		delete(s.armed, target)
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		if ok && s.pages.has(key) {
			report()
			return
		}
		s.unattributed.Add(1)
	})
}

// Unattributed is how many proxy refusals no page asked for (the browser's own
// background requests): they are never reported to the agent.
func (s *Session) Unattributed() int64 { return s.unattributed.Load() }

// peekNotices is the pending notices without clearing them (tests).
func (s *Session) peekNotices() []Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Notice(nil), s.notices...)
}
