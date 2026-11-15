package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path"
	"strings"
	"sync"
	"time"
)

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
	maxNotices         = 200
	maxConsoleLines    = 200
	maxConsoleLineLen  = 500
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
}

// Session is one headless browser for one workspace. It enforces the URL
// policy on every request, caps tabs, cancels dialogs and denies downloads.
type Session struct {
	cfg  Config
	inst Instance
	cl   *Client

	mu      sync.Mutex
	closed  bool
	tabs    map[string]*tabState // by target id
	notices []Notice
	console []string
	idle    *time.Timer
	loads   map[string]chan struct{} // load waiters by cdp session id
	dns     map[string]dnsEntry

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
	inst, err := cfg.Launcher.Launch(ctx, cfg.Launch)
	if err != nil {
		return nil, err
	}
	cl, err := DialCDP(ctx, inst.WSURL())
	if err != nil {
		_ = inst.Close()
		return nil, err
	}
	s := &Session{
		cfg:   cfg,
		inst:  inst,
		cl:    cl,
		tabs:  map[string]*tabState{},
		loads: map[string]chan struct{}{},
		dns:   map[string]dnsEntry{},
	}
	go s.eventLoop()
	go func() { <-cl.Done(); s.Close() }()
	s.idle = time.AfterFunc(cfg.IdleTimeout, s.Close)

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
		_ = s.cl.Close()
		_ = s.inst.Close()
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
	if len(s.notices) >= maxNotices {
		s.notices = s.notices[1:]
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

// Console returns the recent console lines, oldest first.
func (s *Session) Console() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.console...)
}

func (s *Session) addConsole(line string) {
	line = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return ' '
		}
		return r
	}, line)
	if r := []rune(line); len(r) > maxConsoleLineLen {
		line = string(r[:maxConsoleLineLen]) + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.console) >= maxConsoleLines {
		s.console = s.console[1:]
	}
	s.console = append(s.console, line)
}

func (s *Session) eventLoop() {
	for ev := range s.cl.Events() {
		ev := ev
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
		default:
			go s.onEvent(ev)
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
	case "Fetch.requestPaused":
		s.onRequestPaused(ev)
	case "Page.javascriptDialogOpening":
		s.onDialog(ev)
	case "Browser.downloadWillBegin":
		var p struct {
			URL      string `json:"url"`
			Filename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.note("download", fmt.Sprintf("download blocked: %s", safeFileName(p.Filename)))
		}
	}
}

// safeFileName reduces a page-chosen file name to a short printable base name.
func safeFileName(n string) string {
	n = path.Base(strings.ReplaceAll(n, "\\", "/"))
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, n)
	if r := []rune(n); len(r) > 100 {
		n = string(r[:100])
	}
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
	if isPage {
		s.mu.Lock()
		if _, known := s.tabs[p.TargetInfo.TargetID]; !known && len(s.tabs) >= s.cfg.MaxTabs {
			s.mu.Unlock()
			s.note("popup_refused", fmt.Sprintf("a new window was refused: at most %d tabs are allowed", s.cfg.MaxTabs))
			_ = s.cl.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
			return
		}
		s.tabs[p.TargetInfo.TargetID] = &tabState{targetID: p.TargetInfo.TargetID, sessionID: p.SessionID}
		s.mu.Unlock()
	}
	// Policy first: nothing of this target runs before Fetch is on.
	if err := s.cl.Call(ctx, p.SessionID, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*"}}}, nil); err != nil {
		// Without interception the target must not run at all.
		if isPage {
			_ = s.cl.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
		}
		return
	}
	_ = s.cl.Call(ctx, p.SessionID, "Target.setAutoAttach", autoAttachParams(), nil)
	if isPage {
		_ = s.cl.Call(ctx, p.SessionID, "Page.enable", nil, nil)
		_ = s.cl.Call(ctx, p.SessionID, "Runtime.enable", nil, nil)
	}
	resume()
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
	ctx, cancel := s.bgCtx()
	defer cancel()
	d := s.checkRequest(ctx, p.Request.URL, p.ResourceType)
	if d.Allowed {
		_ = s.cl.Call(ctx, ev.SessionID, "Fetch.continueRequest", map[string]any{"requestId": p.RequestID}, nil)
		return
	}
	s.note("blocked", fmt.Sprintf("%s: %s", redact(p.Request.URL), d.Reason))
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
	if !d.Allowed || !d.NeedsResolve {
		return d
	}
	addrs, err := s.resolve(ctx, d.Host)
	if err != nil {
		return refuse("host %q did not resolve", d.Host)
	}
	return s.cfg.Policy.CheckResolved(d, addrs)
}

// resolve looks a name up with a 2 second cache, so a page that loads
// hundreds of resources from one host costs one lookup.
func (s *Session) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	s.mu.Lock()
	e, ok := s.dns[host]
	s.mu.Unlock()
	if ok && time.Since(e.at) < 2*time.Second {
		return e.addrs, e.err
	}
	addrs, err := s.cfg.Resolver.LookupNetIP(ctx, "ip", host)
	s.mu.Lock()
	s.dns[host] = dnsEntry{addrs: addrs, err: err, at: time.Now()}
	s.mu.Unlock()
	return addrs, err
}

// redact trims a URL for a notice: no query, no fragment, bounded length.
func redact(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if r := []rune(u); len(r) > 200 {
		u = string(r[:200]) + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, u)
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
		s.addConsole(p.Type + ": " + strings.Join(parts, " "))
	case "Runtime.exceptionThrown":
		var p struct {
			ExceptionDetails struct {
				Text string `json:"text"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			s.addConsole("exception: " + p.ExceptionDetails.Text)
		}
	}
}

func (s *Session) firstTab() (*tabState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *tabState
	for _, t := range s.tabs {
		if best == nil || t.targetID < best.targetID {
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
		if t != nil {
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
	if d := s.checkURL(ctx, rawURL); !d.Allowed {
		return Tab{}, fmt.Errorf("%w: %s", ErrNavigationDenied, d.Reason)
	}
	var t *tabState
	if tabID == "" {
		if ft, ok := s.firstTab(); ok {
			t = ft
		} else {
			nt, err := s.newTab(ctx)
			if err != nil {
				return Tab{}, err
			}
			t, _ = s.tabByID(nt.ID)
		}
	} else if t, _ = s.tabByID(tabID); t == nil {
		return Tab{}, fmt.Errorf("browser: no such tab %q", tabID)
	}
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
	return Tab{ID: targetID, URL: r.TargetInfo.URL, Title: r.TargetInfo.Title}, nil
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
			out = append(out, Tab{ID: ti.TargetID, URL: ti.URL, Title: ti.Title})
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
	starting map[string]bool
}

func (m *Manager) limit() int {
	if m.Max > 0 {
		return m.Max
	}
	return DefaultMaxSessions
}

// Get returns the workspace's running session or starts one.
func (m *Manager) Get(ctx context.Context, cfg Config) (*Session, error) {
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = map[string]*Session{}
		m.starting = map[string]bool{}
	}
	if s := m.sessions[cfg.Workspace]; s != nil && !s.Closed() {
		m.mu.Unlock()
		return s, nil
	}
	if m.starting[cfg.Workspace] {
		m.mu.Unlock()
		return nil, errors.New("browser: the session for this workspace is still starting")
	}
	if len(m.sessions)+len(m.starting) >= m.limit() {
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}
	m.starting[cfg.Workspace] = true
	m.mu.Unlock()

	var started *Session
	userClose := cfg.OnClose
	cfg.OnClose = func() {
		m.mu.Lock()
		if m.sessions[cfg.Workspace] == started {
			delete(m.sessions, cfg.Workspace)
		}
		m.mu.Unlock()
		if userClose != nil {
			userClose()
		}
	}
	s, err := Start(ctx, cfg)
	m.mu.Lock()
	delete(m.starting, cfg.Workspace)
	if err == nil {
		started = s
		m.sessions[cfg.Workspace] = s
	}
	m.mu.Unlock()
	return s, err
}

// Close ends one workspace's session (a run ended, the human stopped it, the
// switch was turned off).
func (m *Manager) Close(workspace string) {
	m.mu.Lock()
	s := m.sessions[workspace]
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

// CloseAll ends every session (daemon shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	var all []*Session
	for _, s := range m.sessions {
		all = append(all, s)
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
	return len(m.sessions)
}
