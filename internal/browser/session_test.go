package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInstance struct {
	conn   io.ReadWriteCloser
	closed atomic.Bool
}

func (i *fakeInstance) Conn() io.ReadWriteCloser { return i.conn }
func (i *fakeInstance) Close() error             { i.closed.Store(true); _ = i.conn.Close(); return nil }

type fakeLauncher struct {
	mu    sync.Mutex
	insts []*fakeInstance
	cdps  []*fakeCDP
	t     *testing.T
	hook  func(f *fakeCDP, c fakeCall) (any, string)
	opts  LaunchOpts    // the options of the last Launch
	gate  chan struct{} // when set, Launch waits for it (or its context)
	// ignoreCtx makes Launch finish even if its context was cancelled, like a
	// launch that was already past the point of no return.
	ignoreCtx bool
}

func (l *fakeLauncher) Launch(ctx context.Context, o LaunchOpts) (Instance, error) {
	l.mu.Lock()
	l.opts = o
	l.mu.Unlock()
	if l.gate != nil {
		select {
		case <-l.gate:
		case <-ctx.Done():
			if !l.ignoreCtx {
				return nil, ctx.Err()
			}
			<-l.gate
		}
	}
	f := newFakeCDP(l.t, l.hook)
	inst := &fakeInstance{conn: f.client}
	l.mu.Lock()
	l.insts = append(l.insts, inst)
	l.cdps = append(l.cdps, f)
	l.mu.Unlock()
	return inst, nil
}

func (l *fakeLauncher) last() (*fakeCDP, *fakeInstance) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cdps[len(l.cdps)-1], l.insts[len(l.insts)-1]
}

var sessionResolver = fakeResolver{
	"localhost":     {"127.0.0.1"},
	"app.localhost": {"127.0.0.1"},
}

// defaultHook answers the calls a session makes.
func defaultHook(f *fakeCDP, c fakeCall) (any, string) {
	switch c.Method {
	case "Target.createTarget":
		return map[string]any{"targetId": "T-new"}, ""
	case "Page.navigate":
		go func(sid string) {
			time.Sleep(10 * time.Millisecond)
			f.event(sid, "Page.loadEventFired", map[string]any{"timestamp": 1})
		}(c.SessionID)
		return map[string]any{"loaderId": "L1", "frameId": "F1"}, ""
	case "Target.getTargetInfo":
		return map[string]any{"targetInfo": map[string]any{"targetId": c.Params["targetId"], "url": "http://localhost:3000/", "title": "Home"}}, ""
	case "DOM.getDocument":
		return map[string]any{"root": map[string]any{"nodeId": 1}}, ""
	case "DOM.querySelectorAll":
		return map[string]any{"nodeIds": []int{}}, ""
	case "Accessibility.getFullAXTree":
		return map[string]any{"nodes": []any{
			map[string]any{"nodeId": "1", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Home"}, "childIds": []string{"2"}},
			map[string]any{"nodeId": "2", "parentId": "1", "role": map[string]any{"value": "button"}, "name": map[string]any{"value": "Go"}, "backendDOMNodeId": 7},
		}}, ""
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1"}}}, ""
	case "Page.createIsolatedWorld":
		return map[string]any{"executionContextId": 5}, ""
	case "Runtime.evaluate":
		return map[string]any{"result": map[string]any{"value": true}}, ""
	case "Target.getTargets":
		return map[string]any{"targetInfos": []any{
			map[string]any{"targetId": "T1", "type": "page", "url": "http://localhost:3000/", "title": "Home"},
			map[string]any{"targetId": "W1", "type": "service_worker", "url": "http://localhost:3000/sw.js"},
			map[string]any{"targetId": "TX", "type": "page", "url": "chrome://x", "title": "not ours"},
		}}, ""
	}
	return nil, ""
}

func startTestSession(t *testing.T, mut func(*Config)) (*Session, *fakeLauncher) {
	t.Helper()
	l := &fakeLauncher{t: t, hook: defaultHook}
	pol, err := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: 4648}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Workspace: "ws1", Launcher: l, Policy: pol, Resolver: sessionResolver, IdleTimeout: time.Minute, CallTimeout: 2 * time.Second}
	if mut != nil {
		mut(&cfg)
	}
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, l
}

func attach(f *fakeCDP, session, target, typ string) {
	f.event("", "Target.attachedToTarget", map[string]any{
		"sessionId":          session,
		"waitingForDebugger": true,
		"targetInfo":         map[string]any{"targetId": target, "type": typ, "url": "about:blank"},
	})
}

func attachReady(t *testing.T, f *fakeCDP, session, target string, n int) {
	t.Helper()
	attach(f, session, target, "page")
	f.waitCall("Runtime.runIfWaitingForDebugger", n)
}

func TestSessionStartDeniesDownloadsAndAutoAttaches(t *testing.T) {
	_, l := startTestSession(t, nil)
	f, _ := l.last()
	dl := f.callsFor("Browser.setDownloadBehavior")
	if len(dl) != 1 || dl[0].Params["behavior"] != "deny" {
		t.Fatalf("downloads must be denied at start: %+v", dl)
	}
	aa := f.callsFor("Target.setAutoAttach")
	if len(aa) != 1 || aa[0].Params["waitForDebuggerOnStart"] != true || aa[0].Params["flatten"] != true {
		t.Fatalf("targets must be held until interception is on: %+v", aa)
	}
}

func TestSessionBlocksRefusedRequestAndContinuesAllowed(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	if len(f.callsFor("Fetch.enable")) != 1 {
		t.Fatal("Fetch.enable must be called on the new target")
	}
	assertOrder(t, f, "Fetch.enable", "Runtime.runIfWaitingForDebugger")
	paused := func(id, url, typ string) {
		f.event("S1", "Fetch.requestPaused", map[string]any{"requestId": id, "resourceType": typ, "request": map[string]any{"url": url}})
	}
	paused("r-ok", "http://localhost:3000/app.js?token=secret", "Script")
	paused("r-meta", "http://169.254.169.254/latest/meta-data/", "Document")
	paused("r-daemon", "http://127.0.0.1:4648/api/token", "XHR")
	paused("r-file", "file:///etc/passwd", "Document")
	paused("r-datadoc", "data:text/html,<h1>x</h1>", "Document")
	paused("r-dataimg", "data:image/png;base64,AAAA", "Image")
	paused("r-ext", "https://example.com/x.png", "Image")

	f.waitCall("Fetch.continueRequest", 2)
	f.waitCall("Fetch.failRequest", 5)
	ids := func(m string) map[string]bool {
		out := map[string]bool{}
		for _, c := range f.callsFor(m) {
			out[c.Params["requestId"].(string)] = true
			if c.SessionID != "S1" {
				t.Errorf("%s sent on the wrong session %q", m, c.SessionID)
			}
		}
		return out
	}
	cont, fail := ids("Fetch.continueRequest"), ids("Fetch.failRequest")
	if !cont["r-ok"] || !cont["r-dataimg"] || len(cont) != 2 {
		t.Fatalf("continued: %v", cont)
	}
	for _, id := range []string{"r-meta", "r-daemon", "r-file", "r-datadoc", "r-ext"} {
		if !fail[id] {
			t.Errorf("%s must be failed, failed set %v", id, fail)
		}
	}
	for _, c := range f.callsFor("Fetch.failRequest") {
		if c.Params["errorReason"] != "BlockedByClient" {
			t.Errorf("fail reason %v", c.Params["errorReason"])
		}
	}
	notes := s.DrainNotices()
	if len(notes) != 5 {
		t.Fatalf("one blocked notice per refusal, got %d: %+v", len(notes), notes)
	}
	for _, n := range notes {
		if n.Kind != "blocked" || strings.Contains(n.Detail, "secret") {
			t.Errorf("notice must be a blocked one without the query: %+v", n)
		}
	}
	if len(s.DrainNotices()) != 0 {
		t.Fatal("drain clears")
	}
}

func assertOrder(t *testing.T, f *fakeCDP, first, second string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	fi, si := -1, -1
	for i, c := range f.calls {
		if c.Method == first && fi < 0 {
			fi = i
		}
		if c.Method == second && si < 0 {
			si = i
		}
	}
	if fi < 0 || si < 0 || fi > si {
		t.Fatalf("%s must be sent before %s (indices %d, %d)", first, second, fi, si)
	}
}

// Each hop of a redirect chain is a separate paused request and is judged on
// its own URL; the real-Chrome smoke covers a server-sent redirect.
func TestSessionRedirectHopRefused(t *testing.T) {
	_, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	// The server redirects localhost:3000/login -> localhost:3000/app -> the daemon.
	for i, u := range []string{"http://localhost:3000/login", "http://localhost:3000/app", "http://localhost:4648/api/token"} {
		f.event("S1", "Fetch.requestPaused", map[string]any{"requestId": "hop" + string(rune('0'+i)), "resourceType": "Document", "request": map[string]any{"url": u}})
	}
	f.waitCall("Fetch.continueRequest", 2)
	got := f.waitCall("Fetch.failRequest", 1)
	if got[0].Params["requestId"] != "hop2" {
		t.Fatalf("only the hop to the daemon is failed: %+v", got)
	}
}

func TestSessionFailsClosedWhenInterceptionCannotBeEnabled(t *testing.T) {
	l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
		if c.Method == "Fetch.enable" {
			return nil, "boom"
		}
		return defaultHook(f, c)
	}}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, _ := l.last()
	attach(f, "S1", "T1", "page")
	got := f.waitCall("Target.closeTarget", 1)
	if got[0].Params["targetId"] != "T1" {
		t.Fatalf("an uninterceptable page must be closed: %+v", got)
	}
	if len(f.callsFor("Runtime.runIfWaitingForDebugger")) != 0 {
		t.Fatal("an uninterceptable page must never be resumed")
	}
}

func TestSessionPopupPoolCap(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	for i, id := range []string{"T1", "T2", "T3"} {
		attachReady(t, f, "S"+id, id, i+1)
	}
	attach(f, "S4", "T4", "page")
	got := f.waitCall("Target.closeTarget", 1)
	if got[0].Params["targetId"] != "T4" {
		t.Fatalf("the fourth tab is closed: %+v", got)
	}
	time.Sleep(50 * time.Millisecond)
	for _, c := range f.callsFor("Fetch.enable") {
		if c.SessionID == "S4" {
			t.Fatal("a refused popup must not be set up")
		}
	}
	notes := s.DrainNotices()
	if len(notes) != 1 || notes[0].Kind != "popup_refused" {
		t.Fatalf("notice: %+v", notes)
	}
	// Workers and frames are not tabs and never count against the pool.
	attach(f, "SW", "W1", "service_worker")
	f.waitCall("Fetch.enable", 4)
	if _, err := s.NewTab(context.Background()); !errors.Is(err, ErrTabLimit) {
		t.Fatalf("NewTab on a full pool: %v", err)
	}
	if len(f.callsFor("Target.createTarget")) != 0 {
		t.Fatal("a full pool must not even ask the browser for a tab")
	}
}

func TestSessionDialogsCancelled(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	for _, typ := range []string{"alert", "confirm", "prompt", "beforeunload"} {
		f.event("S1", "Page.javascriptDialogOpening", map[string]any{"type": typ, "message": "Delete everything?"})
	}
	got := f.waitCall("Page.handleJavaScriptDialog", 4)
	accepted := 0
	for _, c := range got {
		if c.Params["accept"] == true {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("only the alert may be accepted (dismissed), got %d accepts: %+v", accepted, got)
	}
	notes := s.DrainNotices()
	if len(notes) != 4 {
		t.Fatalf("each dialog is reported: %+v", notes)
	}
	for _, n := range notes {
		if n.Kind != "dialog" || strings.Contains(n.Detail, "Delete everything") {
			t.Errorf("dialog notice must not echo the page's message: %+v", n)
		}
	}
}

func TestSessionDownloadDeniedAndReported(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	f.event("", "Browser.downloadWillBegin", map[string]any{"guid": "g", "url": "http://localhost:3000/x", "suggestedFilename": "../../etc/ev\u0001il.sh"})
	deadline := time.Now().Add(2 * time.Second)
	var notes []Notice
	for len(notes) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		notes = s.DrainNotices()
	}
	if len(notes) != 1 || notes[0].Kind != "download" || !strings.HasSuffix(notes[0].Detail, "evil.sh") || strings.Contains(notes[0].Detail, "/") {
		t.Fatalf("download notice: %+v", notes)
	}
}

func TestSessionNavigateRefusedNeverSent(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	for _, u := range []string{"file:///etc/passwd", "http://127.0.0.1:4648/api/token", "http://example.com/", "javascript:alert(1)", "http://user@localhost/"} {
		if _, err := s.Navigate(context.Background(), "", u); !errors.Is(err, ErrNavigationDenied) {
			t.Errorf("Navigate(%q): %v", u, err)
		}
	}
	if n := len(f.callsFor("Page.navigate")); n != 0 {
		t.Fatalf("a refused URL must never reach the browser, %d navigate calls", n)
	}
	tab, err := s.Navigate(context.Background(), "T1", "http://localhost:3000/")
	if err != nil || tab.URL != "http://localhost:3000/" || tab.ID != "T1" {
		t.Fatalf("allowed navigate: %+v %v", tab, err)
	}
	nav := f.callsFor("Page.navigate")
	if len(nav) != 1 || nav[0].SessionID != "S1" {
		t.Fatalf("navigate goes to the tab's session: %+v", nav)
	}
}

func TestSessionNavigateCreatesTabAndReportsNavigationError(t *testing.T) {
	old := initialTabWait
	initialTabWait = 50 * time.Millisecond
	defer func() { initialTabWait = old }()
	l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
		if c.Method == "Target.createTarget" {
			go attach(f, "S9", "T-new", "page")
		}
		if c.Method == "Page.navigate" && strings.Contains(c.Params["url"].(string), "broken") {
			return map[string]any{"errorText": "net::ERR_BLOCKED_BY_CLIENT"}, ""
		}
		return defaultHook(f, c)
	}}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Navigate(context.Background(), "", "http://localhost:3000/"); err != nil {
		t.Fatal(err)
	}
	f, _ := l.last()
	if len(f.callsFor("Target.createTarget")) != 1 {
		t.Fatal("no tab yet: Navigate creates one")
	}
	if _, err := s.Navigate(context.Background(), "T-new", "http://localhost:3000/broken"); err == nil || !strings.Contains(err.Error(), "ERR_BLOCKED_BY_CLIENT") {
		t.Fatalf("navigation error text must surface: %v", err)
	}
	if _, err := s.Navigate(context.Background(), "nope", "http://localhost:3000/"); err == nil {
		t.Fatal("unknown tab")
	}
}

func TestSessionTabsOnlyListsOwnPages(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	tabs, err := s.Tabs(context.Background())
	if err != nil || len(tabs) != 1 || tabs[0].ID != "T1" || tabs[0].Title != "Home" {
		t.Fatalf("tabs: %+v %v", tabs, err)
	}
	if err := s.CloseTab(context.Background(), "T1"); err != nil {
		t.Fatal(err)
	}
	if got := f.callsFor("Target.closeTarget"); len(got) != 1 || got[0].Params["targetId"] != "T1" {
		t.Fatalf("close: %+v", got)
	}
	if err := s.CloseTab(context.Background(), "T1"); err == nil {
		t.Fatal("closing an unknown tab must fail")
	}
}

func TestSessionConsoleCollected(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	f.event("S1", "Runtime.consoleAPICalled", map[string]any{"type": "log", "args": []any{
		map[string]any{"type": "string", "value": "hello\nworld"},
		map[string]any{"type": "number", "value": 42},
		map[string]any{"type": "object", "description": "Object"},
	}})
	f.event("S1", "Runtime.consoleAPICalled", map[string]any{"type": "error", "args": []any{map[string]any{"type": "string", "value": strings.Repeat("x", 900)}}})
	f.event("S1", "Runtime.exceptionThrown", map[string]any{"exceptionDetails": map[string]any{"text": "Uncaught"}})
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Console()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	c := s.Console()
	if len(c) != 3 || c[0] != "log: hello world 42 Object" || c[2] != "exception: Uncaught" {
		t.Fatalf("console: %q", c)
	}
	if n := len([]rune(c[1])); n > maxConsoleLineLen+1 {
		t.Fatalf("a console line is capped at %d, got %d", maxConsoleLineLen, n)
	}
}

func TestSessionIdleClose(t *testing.T) {
	var closedHook atomic.Bool
	s, l := startTestSession(t, func(c *Config) {
		c.IdleTimeout = 80 * time.Millisecond
		c.OnClose = func() { closedHook.Store(true) }
	})
	_, inst := l.last()
	deadline := time.Now().Add(2 * time.Second)
	for !(s.Closed() && inst.closed.Load() && closedHook.Load()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Closed() || !inst.closed.Load() || !closedHook.Load() {
		t.Fatal("an idle session must close and kill the browser")
	}
}

func TestSessionActivityKeepsItOpen(t *testing.T) {
	s, _ := startTestSession(t, func(c *Config) { c.IdleTimeout = 150 * time.Millisecond })
	for i := 0; i < 5; i++ {
		time.Sleep(60 * time.Millisecond)
		if _, err := s.Tabs(context.Background()); err != nil {
			t.Fatalf("a used session must stay open: %v", err)
		}
	}
}

func TestSessionUseAfterClose(t *testing.T) {
	s, l := startTestSession(t, nil)
	_, inst := l.last()
	s.Close()
	s.Close() // idempotent
	if !inst.closed.Load() {
		t.Fatal("close kills the browser")
	}
	if _, err := s.Navigate(context.Background(), "", "http://localhost:3000/"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("navigate after close: %v", err)
	}
	if _, err := s.Tabs(context.Background()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("tabs after close: %v", err)
	}
	if _, err := s.NewTab(context.Background()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("new tab after close: %v", err)
	}
}

func TestSessionClosesWhenConnectionDrops(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, inst := l.last()
	f.closeConn()
	deadline := time.Now().Add(2 * time.Second)
	for !(s.Closed() && inst.closed.Load()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Closed() || !inst.closed.Load() {
		t.Fatal("a lost browser connection must end the session and kill the process")
	}
}

func TestManagerOneSessionPerWorkspaceAndCap(t *testing.T) {
	l := &fakeLauncher{t: t, hook: defaultHook}
	m := &Manager{}
	cfg := func(ws string) Config {
		return Config{Workspace: ws, Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second}
	}
	a, err := m.Get(context.Background(), cfg("a"))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := m.Get(context.Background(), cfg("a"))
	if err != nil || a2 != a {
		t.Fatalf("one session per workspace: %v", err)
	}
	if len(l.insts) != 1 {
		t.Fatalf("the second Get must not launch, launched %d", len(l.insts))
	}
	if _, err := m.Get(context.Background(), cfg("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(context.Background(), cfg("c")); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("third session must be refused: %v", err)
	}
	if m.Active() != 2 {
		t.Fatalf("active %d", m.Active())
	}
	// A run ended: the workspace's session is killed and the slot is free.
	m.Close("a")
	if !a.Closed() || !l.insts[0].closed.Load() {
		t.Fatal("Close(workspace) must kill the session")
	}
	if _, err := m.Get(context.Background(), cfg("c")); err != nil {
		t.Fatalf("slot freed: %v", err)
	}
	m.CloseAll()
	for i, inst := range l.insts {
		if !inst.closed.Load() {
			t.Errorf("instance %d left running", i)
		}
	}
	if m.Active() != 0 {
		t.Fatal("CloseAll leaves nothing")
	}
}

func TestManagerStartFailureFreesTheSlot(t *testing.T) {
	m := &Manager{Max: 1}
	bad := Config{Workspace: "a", Launcher: failingLauncher{}}
	if _, err := m.Get(context.Background(), bad); err == nil {
		t.Fatal("want launch error")
	}
	l := &fakeLauncher{t: t, hook: defaultHook}
	if _, err := m.Get(context.Background(), Config{Workspace: "b", Launcher: l, Resolver: sessionResolver}); err != nil {
		t.Fatalf("a failed start must not hold a slot: %v", err)
	}
	m.CloseAll()
}

type failingLauncher struct{}

func (failingLauncher) Launch(context.Context, LaunchOpts) (Instance, error) {
	return nil, &UnavailableError{InstallStep: "install"}
}

func TestSessionNavigateWaitsForInterception(t *testing.T) {
	l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
		switch c.Method {
		case "Target.createTarget":
			go attach(f, "S9", "T-new", "page")
		case "Fetch.enable":
			time.Sleep(150 * time.Millisecond) // a slow browser: the tab exists but is not armed yet
		}
		return defaultHook(f, c)
	}}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Navigate(context.Background(), "", "http://localhost:3000/"); err != nil {
		t.Fatal(err)
	}
	f, _ := l.last()
	assertOrder(t, f, "Fetch.enable", "Page.navigate")
}

func TestSessionNoticesFloodCannotEvictOtherKinds(t *testing.T) {
	s, _ := startTestSession(t, nil)
	s.note("dialog", "a confirm dialog was cancelled")
	for i := 0; i < 500; i++ {
		s.note("blocked", fmt.Sprintf("u%d", i))
	}
	var dialog, blocked int
	for _, n := range s.DrainNotices() {
		switch n.Kind {
		case "dialog":
			dialog++
		case "blocked":
			blocked++
		}
	}
	if dialog != 1 || blocked != maxNoticesPerKind {
		t.Fatalf("dialog=%d blocked=%d", dialog, blocked)
	}
}

func TestCleanTextStripsInvisibleAndControl(t *testing.T) {
	in := "a\u202eb\u200bc\U000e0041d\u2028e\x00f\x7fg\th"
	if got := cleanText(in, 100); got != "abcdefg h" {
		t.Fatalf("got %q", got)
	}
	if got := cleanText(strings.Repeat("x", 5000), 10); got != strings.Repeat("x", 10)+"…" {
		t.Fatalf("cap: %q", got)
	}
	if safeFileName("../../a\u202e/b\\evil.sh") != "evil.sh" || safeFileName("") != "(unnamed)" {
		t.Fatal("file name")
	}
}

func TestSessionDNSCacheIsBounded(t *testing.T) {
	s, _ := startTestSession(t, func(c *Config) { c.Resolver = manyResolver{} })
	r := cachedResolver{s}
	for i := 0; i < 3*maxDNSEntries; i++ {
		_, _ = r.LookupNetIP(context.Background(), "ip", fmt.Sprintf("h%d.localhost", i))
	}
	s.mu.Lock()
	n := len(s.dns)
	s.mu.Unlock()
	if n > maxDNSEntries {
		t.Fatalf("dns cache grew to %d", n)
	}
}

type manyResolver struct{}

func (manyResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestSessionChildSetupFailureIsFatalForTheTarget(t *testing.T) {
	for _, failing := range []string{"Target.setAutoAttach", "Page.enable", "Runtime.enable"} {
		failing := failing
		t.Run(failing, func(t *testing.T) {
			l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
				if c.SessionID == "S1" && c.Method == failing {
					return nil, "boom"
				}
				return defaultHook(f, c)
			}}
			s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			f, _ := l.last()
			attach(f, "S1", "T1", "page")
			got := f.waitCall("Target.closeTarget", 1)
			if got[0].Params["targetId"] != "T1" {
				t.Fatalf("a page that cannot be fully set up must be closed: %+v", got)
			}
			if len(f.callsFor("Runtime.runIfWaitingForDebugger")) != 0 {
				t.Fatal("it must never be resumed")
			}
			if _, ok := s.tabByID("T1"); ok {
				t.Fatal("and never handed out")
			}
		})
	}
}

func TestSessionFailureCloseUsesAFreshContext(t *testing.T) {
	// Fetch.enable hangs past the call timeout; the close afterwards still goes out.
	l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
		if c.Method == "Fetch.enable" {
			return hold, ""
		}
		return defaultHook(f, c)
	}}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, _ := l.last()
	attach(f, "S1", "T1", "page")
	f.waitCall("Target.closeTarget", 1)
}

func TestTabFieldsAreSanitised(t *testing.T) {
	l := &fakeLauncher{t: t, hook: func(f *fakeCDP, c fakeCall) (any, string) {
		switch c.Method {
		case "Target.getTargetInfo":
			return map[string]any{"targetInfo": map[string]any{"targetId": "T1", "url": "http://localhost:3000/\u202e" + strings.Repeat("a", 900), "title": "Ig\u200bnore\U000e0049 this\n" + strings.Repeat("x", 500)}}, ""
		case "Target.getTargets":
			return map[string]any{"targetInfos": []any{map[string]any{"targetId": "T1", "type": "page", "url": "http://localhost/\u202e", "title": "a\u0301\u0301\u0301\u0301\u0301\u0301b\ufe0f"}}}, ""
		}
		return defaultHook(f, c)
	}}
	s, err := Start(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	tab, err := s.Navigate(context.Background(), "T1", "http://localhost:3000/")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(tab.URL)) > 501 || len([]rune(tab.Title)) > 201 || strings.ContainsAny(tab.URL+tab.Title, "\u202e\u200b\n") || strings.Contains(tab.Title, "\U000e0049") {
		t.Fatalf("navigate result must be sanitised: %q %q", tab.URL, tab.Title)
	}
	tabs, _ := s.Tabs(context.Background())
	if len(tabs) != 1 || tabs[0].Title != "a\u0301\u0301b" || strings.ContainsAny(tabs[0].URL, "\u202e") {
		t.Fatalf("tabs must be sanitised: %+v", tabs)
	}
}

func TestRedactDropsUserinfoAndQuery(t *testing.T) {
	for in, want := range map[string]string{
		"http://user:pw@evil.test/a?token=1#f": "http://evil.test/a",
		"https://a@b@c.test/x":                 "https://c.test/x",
		"http://plain.test/p":                  "http://plain.test/p",
	} {
		if got := redact(in); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManagerCloseDuringStartLeavesNothingRunning(t *testing.T) {
	l := &fakeLauncher{t: t, hook: defaultHook, gate: make(chan struct{})}
	m := &Manager{}
	cfg := Config{Workspace: "a", Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second}
	errc := make(chan error, 1)
	go func() { _, err := m.Get(context.Background(), cfg); errc <- err }()
	time.Sleep(50 * time.Millisecond) // the launch is in flight
	m.Close("a")
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("a session closed while starting must not be returned")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Get did not return")
	}
	if m.Active() != 0 {
		t.Fatal("nothing may be left running")
	}

	// A launch that cannot be cancelled and finishes after CloseAll: the
	// session it produced is closed, never registered.
	l2 := &fakeLauncher{t: t, hook: defaultHook, gate: make(chan struct{}), ignoreCtx: true}
	cfg.Launcher = l2
	go func() { _, err := m.Get(context.Background(), cfg); errc <- err }()
	time.Sleep(50 * time.Millisecond)
	m.CloseAll()
	close(l2.gate)
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("CloseAll during start must not return a live session")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Get did not return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		l2.mu.Lock()
		ok := len(l2.insts) == 1 && l2.insts[0].closed.Load()
		l2.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	l2.mu.Lock()
	defer l2.mu.Unlock()
	if len(l2.insts) != 1 || !l2.insts[0].closed.Load() || m.Active() != 0 {
		t.Fatal("the instance started during CloseAll was left running")
	}
}

func TestManagerChangedConfigRestartsTheSession(t *testing.T) {
	l := &fakeLauncher{t: t, hook: defaultHook}
	m := &Manager{}
	defer m.CloseAll()
	pol1, _ := NewPolicy(nil, nil)
	pol2, _ := NewPolicy([]string{"intranet.test"}, nil)
	a, err := m.Get(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, Policy: pol1})
	if err != nil {
		t.Fatal(err)
	}
	same, _ := m.Get(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, Policy: pol1})
	if same != a {
		t.Fatal("same config reuses the session")
	}
	b, err := m.Get(context.Background(), Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, Policy: pol2})
	if err != nil || b == a {
		t.Fatalf("a changed allowlist must not be silently ignored: %v", err)
	}
	if !a.Closed() {
		t.Fatal("the old session (old policy) must be closed")
	}
}

func TestSessionRequestBacklogClosesTheSession(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, inst := l.last()
	attachReady(t, f, "S1", "T1", 1)
	s.maxQueued = 1
	f.event("S1", "Fetch.requestPaused", map[string]any{"requestId": "r", "request": map[string]any{"url": "http://localhost/"}})
	deadline := time.Now().Add(2 * time.Second)
	for !(s.Closed() && inst.closed.Load()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Closed() {
		t.Fatal("a backlog over the byte budget must close the session, not let requests through unchecked")
	}
}

func TestManagerConcurrentGetStartsOneBrowser(t *testing.T) {
	l := &fakeLauncher{t: t, hook: defaultHook}
	m := &Manager{Max: 4}
	defer m.CloseAll()
	cfg := Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Get(context.Background(), cfg)
		}()
	}
	wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.insts) != 1 {
		t.Fatalf("concurrent Gets for one workspace must launch one browser, launched %d", len(l.insts))
	}
}

func TestManagerRestartRacingCloseAllLeavesNothing(t *testing.T) {
	for round := 0; round < 20; round++ {
		l := &fakeLauncher{t: t, hook: defaultHook}
		m := &Manager{Max: 4}
		pol1, _ := NewPolicy(nil, nil)
		pol2, _ := NewPolicy([]string{"a.test"}, nil)
		base := Config{Workspace: "w", Launcher: l, Resolver: sessionResolver, CallTimeout: 2 * time.Second, Policy: pol1}
		if _, err := m.Get(context.Background(), base); err != nil {
			t.Fatal(err)
		}
		changed := base
		changed.Policy = pol2
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = m.Get(context.Background(), changed) }()
		go func() { defer wg.Done(); _, _ = m.Get(context.Background(), changed) }()
		go func() { defer wg.Done(); m.CloseAll() }()
		wg.Wait()
		m.CloseAll() // a Get that won the race registered its session after the first CloseAll
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && !allClosed(l) {
			time.Sleep(5 * time.Millisecond)
		}
		if !allClosed(l) || m.Active() != 0 {
			t.Fatalf("round %d: a browser was left running", round)
		}
	}
}

func allClosed(l *fakeLauncher) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, i := range l.insts {
		if !i.closed.Load() {
			return false
		}
	}
	return true
}

func TestCfgKeyCoversLauncherAndTimeouts(t *testing.T) {
	a := Config{Workspace: "w", Launcher: ChromeLauncher{Path: "/a"}}
	for name, b := range map[string]Config{
		"launcher":    {Workspace: "w", Launcher: ChromeLauncher{Path: "/b"}},
		"idle":        {Workspace: "w", Launcher: ChromeLauncher{Path: "/a"}, IdleTimeout: time.Hour},
		"call":        {Workspace: "w", Launcher: ChromeLauncher{Path: "/a"}, CallTimeout: time.Hour},
		"profileRoot": {Workspace: "w", Launcher: ChromeLauncher{Path: "/a"}, Launch: LaunchOpts{ProfileRoot: "/x"}},
	} {
		if cfgKey(a) == cfgKey(b) {
			t.Errorf("a changed %s must change the key", name)
		}
	}
	if cfgKey(a) != cfgKey(Config{Workspace: "other", Launcher: ChromeLauncher{Path: "/a"}}) {
		t.Error("the workspace name is not part of the key")
	}
}

// The browser's own blank page may be armed a moment after the first
// navigation is asked for: that navigation uses it (no second tab), and later
// calls keep working in the same tab.
func TestSessionFirstNavigationWaitsForTheInitialTab(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	go func() {
		time.Sleep(150 * time.Millisecond)
		attach(f, "S1", "T1", "page")
	}()
	tab, err := s.Navigate(context.Background(), "", "http://localhost:3000/")
	if err != nil {
		t.Fatal(err)
	}
	if tab.ID != "T1" || len(f.callsFor("Target.createTarget")) != 0 {
		t.Fatalf("the initial tab must be reused, not a second one opened: %+v createTarget=%d", tab, len(f.callsFor("Target.createTarget")))
	}
}

func TestSessionWorksInTheTabItNavigated(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	attachReady(t, f, "S2", "T2", 2)
	if _, err := s.Navigate(context.Background(), "T2", "http://localhost:3000/"); err != nil {
		t.Fatal(err)
	}
	tab, err := s.tabFor("")
	if err != nil || tab.targetID != "T2" {
		t.Fatalf("the tab the agent navigated is the current one, not the oldest: %v %v", tab, err)
	}
	tabs, err := s.Tabs(context.Background())
	if err != nil || len(tabs) == 0 {
		t.Fatal(err)
	}
}

func authRequired(f *fakeCDP, session, id, source, origin string) {
	f.event(session, "Fetch.authRequired", map[string]any{
		"requestId": id, "resourceType": "Document",
		"request":       map[string]any{"url": "http://localhost:3000/"},
		"authChallenge": map[string]any{"source": source, "origin": origin, "scheme": "basic", "realm": "smind"},
	})
}

func TestSessionFetchEnableHandlesAuthRequests(t *testing.T) {
	_, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	calls := f.callsFor("Fetch.enable")
	if len(calls) != 1 || calls[0].Params["handleAuthRequests"] != true {
		t.Fatalf("Fetch.enable must ask for auth requests (or the proxy challenge would stall every request): %+v", calls)
	}
}

func TestSessionProxyChallengeAnsweredWithTheSecretAndOthersCancelled(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	secret := s.proxy.Secret()
	origin := "http://" + s.proxy.Addr()

	authRequired(f, "S1", "a-proxy", "Proxy", origin)
	authRequired(f, "S1", "a-server", "Server", "http://localhost:3000")
	authRequired(f, "S1", "a-server-same-origin-as-proxy", "Server", origin) // a page cannot borrow the proxy's origin as a Server challenge
	authRequired(f, "S1", "a-other-proxy", "Proxy", "http://127.0.0.1:1")
	authRequired(f, "S1", "a-https-proxy-origin", "Proxy", "https://"+s.proxy.Addr())
	authRequired(f, "S1", "a-empty", "Proxy", "")
	got := f.waitCall("Fetch.continueWithAuth", 6)
	byID := map[string]fakeCall{}
	for _, c := range got {
		byID[c.Params["requestId"].(string)] = c
		if c.SessionID != "S1" {
			t.Errorf("answered on the wrong session: %+v", c)
		}
	}
	resp := func(id string) map[string]any {
		c, ok := byID[id]
		if !ok {
			t.Fatalf("no answer for %s", id)
		}
		return c.Params["authChallengeResponse"].(map[string]any)
	}
	pr := resp("a-proxy")
	if pr["response"] != "ProvideCredentials" || pr["username"] != "smind" || pr["password"] != secret {
		t.Fatalf("the proxy challenge gets the credential: %v", pr)
	}
	for _, id := range []string{"a-server", "a-server-same-origin-as-proxy", "a-other-proxy", "a-https-proxy-origin", "a-empty"} {
		r := resp(id)
		if r["response"] != "CancelAuth" || r["password"] != nil || r["username"] != nil {
			t.Errorf("%s must be cancelled with no credential: %v", id, r)
		}
	}
	for _, c := range f.callsFor("Fetch.continueWithAuth") {
		if c.Params["requestId"] != "a-proxy" && strings.Contains(fmt.Sprint(c.Params), secret) {
			t.Fatalf("the secret went to a non-proxy challenge: %+v", c)
		}
	}
	for _, n := range s.DrainNotices() {
		if strings.Contains(n.Detail, secret) {
			t.Fatalf("a notice shows the secret: %+v", n)
		}
	}
}

// A worker target is intercepted too, so its proxy challenge arrives on its own session.
func TestSessionProxyChallengeOnAWorkerTarget(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	attach(f, "W1", "W1T", "worker")
	f.waitCall("Runtime.runIfWaitingForDebugger", 2)
	authRequired(f, "W1", "w-proxy", "Proxy", "http://"+s.proxy.Addr())
	got := f.waitCall("Fetch.continueWithAuth", 1)
	if got[0].SessionID != "W1" || got[0].Params["authChallengeResponse"].(map[string]any)["password"] != s.proxy.Secret() {
		t.Fatalf("worker challenge: %+v", got[0])
	}
	if n := len(f.callsFor("Fetch.enable")); n != 2 {
		t.Fatalf("the worker target enables Fetch with auth handling too: %d", n)
	}
	for _, c := range f.callsFor("Fetch.enable") {
		if c.Params["handleAuthRequests"] != true {
			t.Fatalf("Fetch.enable without auth handling: %+v", c)
		}
	}
}

// The secret is not in the browser's command line (the launcher options carry no secret).
func TestSessionSecretNotInLaunchOptions(t *testing.T) {
	s, l := startTestSession(t, nil)
	if strings.Contains(fmt.Sprintf("%+v", l.opts), s.proxy.Secret()) {
		t.Fatal("the proxy secret reached the launch options")
	}
}

// A page can raise any number of credential challenges: one notice, and the agent's other notices survive.
func TestSessionRefusedChallengeNoticeIsOnce(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	for i := 0; i < 30; i++ {
		authRequired(f, "S1", fmt.Sprintf("a%d", i), "Server", "http://localhost:3000")
	}
	f.waitCall("Fetch.continueWithAuth", 30)
	n := 0
	for _, no := range s.DrainNotices() {
		if strings.Contains(no.Detail, "asked for credentials") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one notice for 30 refused challenges, got %d", n)
	}
}
