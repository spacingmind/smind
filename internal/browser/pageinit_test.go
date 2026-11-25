package browser

import (
	"fmt"
	"testing"
	"time"
)

func TestHostPortOf(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"www.google.com:443":                     "www.google.com:443",
		"WWW.Google.COM:443":                     "www.google.com:443",
		"https://evil.example/path?q=1#f":        "evil.example:443",
		"http://evil.example/a":                  "evil.example:80",
		"http://evil.example:8080/a":             "evil.example:8080",
		"wss://evil.example/ws":                  "evil.example:443",
		"ws://evil.example/ws":                   "evil.example:80",
		"http://[2001:db8::1]:9000/":             "[2001:db8::1]:9000",
		"[2001:db8::1]:443":                      "[2001:db8::1]:443",
		"ftp://evil.example/":                    "",
		"":                                       "",
		"no-port":                                "",
		"host:notaport":                          "",
		"data:text/html,<h1>x</h1>":              "",
		"https://user:pw@evil.example:444/x?y=z": "evil.example:444",
	} {
		got, ok := hostPortOf(in)
		if (got != "") != ok || got != want {
			t.Errorf("hostPortOf(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func fastGrace(s *Session) { s.grace = 30 * time.Millisecond }

// The browser's own background request (nothing on a page asked for it) is counted, never reported.
func TestRefusalNobodyAskedForIsCountedNotReported(t *testing.T) {
	s, l := startTestSession(t, nil)
	fastGrace(s)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	s.refused("www.google.com:443", `host "www.google.com" is not allowed`)
	s.refused("accounts.google.com:443", `host "accounts.google.com" is not allowed`)
	waitUntil(t, "both counted", func() bool { return s.Unattributed() == 2 })
	if n := s.DrainNotices(); len(n) != 0 {
		t.Fatalf("the browser's own traffic must not reach the agent: %+v", n)
	}
}

// A target a page asked for (a fetch, a script, a WebSocket) is reported, once.
func TestRefusalOfATargetAPageRequestedIsReportedOnce(t *testing.T) {
	s, l := startTestSession(t, nil)
	fastGrace(s)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	f.event("S1", "Network.requestWillBeSent", map[string]any{"request": map[string]any{"url": "https://tracker.example/pixel.gif?id=9"}})
	f.event("S1", "Network.webSocketCreated", map[string]any{"url": "wss://socket.example:8443/live"})
	waitUntil(t, "the two targets named", func() bool {
		return s.pages.has("tracker.example:443") && s.pages.has("socket.example:8443")
	})
	s.refused("tracker.example:443", "not allowed")
	s.refused("tracker.example:443", "not allowed") // the same target again: once
	s.refused("socket.example:8443", "not allowed")
	waitUntil(t, "two notices", func() bool { return len(s.peekNotices()) == 2 })
	time.Sleep(100 * time.Millisecond) // sleep-ok: nothing more may arrive
	if n := len(s.peekNotices()); n != 2 {
		t.Fatalf("one notice per target, got %d", n)
	}
	if s.Unattributed() != 0 {
		t.Fatalf("every refusal was a page's: %d unattributed", s.Unattributed())
	}
}

// The CDP event that names the request can trail the proxy's refusal: a refusal waits a moment.
func TestRefusalWaitsForThePageRequestThatNamesIt(t *testing.T) {
	s, l := startTestSession(t, nil)
	s.grace = 300 * time.Millisecond
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	s.refused("late.example:443", "not allowed") // the refusal first
	time.Sleep(40 * time.Millisecond)            // sleep-ok: inside the grace
	if n := len(s.peekNotices()); n != 0 {
		t.Fatalf("nothing is reported before the grace is over: %d", n)
	}
	f.event("S1", "Network.requestWillBeSent", map[string]any{"request": map[string]any{"url": "https://late.example/x"}})
	waitUntil(t, "the notice after the event", func() bool { return len(s.peekNotices()) == 1 })
	if s.Unattributed() != 0 {
		t.Fatal("a refusal named within the grace is a page's")
	}
}

// The proxy's own notice about a wrong credential is not a request target: nothing to attribute,
// and the agent still hears it (once).
func TestProxyCredentialNoticeIsNeverFiltered(t *testing.T) {
	s, _ := startTestSession(t, nil)
	s.refused("proxy", "requests with a wrong proxy credential were refused and cut")
	s.refused("proxy", "requests with a wrong proxy credential were refused and cut")
	n := s.peekNotices()
	if len(n) != 1 || n[0].Kind != "blocked" || s.Unattributed() != 0 {
		t.Fatalf("notices %+v, unattributed %d", n, s.Unattributed())
	}
}

// A flood of the same refused target arms one timer, not one per refusal, and is counted once.
func TestRepeatedUnattributedRefusalsArmOneTimer(t *testing.T) {
	s, _ := startTestSession(t, nil)
	s.grace = 60 * time.Millisecond
	for i := 0; i < 50; i++ {
		s.refused("spam.example:443", "not allowed")
	}
	waitUntil(t, "counted once", func() bool { return s.Unattributed() == 1 })
	time.Sleep(120 * time.Millisecond) // sleep-ok: nothing more may arrive
	if s.Unattributed() != 1 {
		t.Fatalf("one target, one count: %d", s.Unattributed())
	}
	s.mu.Lock()
	left := len(s.armed)
	s.mu.Unlock()
	if left != 0 {
		t.Fatalf("a finished timer leaves nothing armed: %d", left)
	}
}

// A page cannot push its own target out by repeating it, and a flood of distinct hosts ages the OLDEST
// targets out one at a time instead of wiping the set.
func TestPageTargetsEvictOldestOneAtATime(t *testing.T) {
	t.Parallel()
	var p pageTargets
	p.add("keep.example:443")
	for i := 0; i < pageTargetsMax-1; i++ {
		p.add(fmt.Sprintf("h%d.example:443", i))
		p.add("keep.example:443") // repeating a known target changes nothing
	}
	if !p.has("keep.example:443") || !p.has("h0.example:443") {
		t.Fatal("a full set still holds everything added so far")
	}
	p.add("one-more.example:443") // the set is full: only the oldest goes
	if p.has("keep.example:443") {
		t.Fatal("the oldest target is evicted first")
	}
	if !p.has("h0.example:443") || !p.has("one-more.example:443") {
		t.Fatal("one eviction, not a wipe")
	}
}

// Network.enable runs on a new target before it is resumed, so no request of it precedes the domain.
func TestNetworkIsEnabledBeforeATargetRuns(t *testing.T) {
	_, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	if len(f.callsFor("Network.enable")) != 1 {
		t.Fatalf("Network.enable calls: %d", len(f.callsFor("Network.enable")))
	}
	assertOrder(t, f, "Network.enable", "Runtime.runIfWaitingForDebugger")
}

// A request the page's own Fetch interception sees names its target too (an allowed one included).
func TestFetchRequestNamesItsTarget(t *testing.T) {
	s, l := startTestSession(t, nil)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	f.event("S1", "Fetch.requestPaused", map[string]any{"requestId": "r1", "resourceType": "Script", "request": map[string]any{"url": "http://localhost:3000/app.js?token=secret"}})
	waitUntil(t, "the fetch's target", func() bool { return s.pages.has("localhost:3000") })
}
