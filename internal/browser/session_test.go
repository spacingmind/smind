package browser

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInstance struct {
	url    string
	closed atomic.Bool
}

func (i *fakeInstance) WSURL() string { return i.url }
func (i *fakeInstance) Close() error  { i.closed.Store(true); return nil }

type fakeLauncher struct {
	mu    sync.Mutex
	insts []*fakeInstance
	cdps  []*fakeCDP
	t     *testing.T
	hook  func(f *fakeCDP, c fakeCall) (any, string)
}

func (l *fakeLauncher) Launch(_ context.Context, _ LaunchOpts) (Instance, error) {
	f := newFakeCDP(l.t, l.hook)
	inst := &fakeInstance{url: f.wsURL()}
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
	// Fetch.enable precedes the resume.
	if len(f.callsFor("Fetch.enable")) != 1 {
		t.Fatal("Fetch.enable must be called on the new target")
	}
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
