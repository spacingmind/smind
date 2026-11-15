package browser

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRealChromeSmoke drives a real headless system Chromium against pages
// this test serves on loopback. It is opt-in (SMIND_BROWSER_SMOKE=1), skips
// when no browser is installed, never reaches a non-loopback URL and keeps its
// profile under SMIND_BROWSER_SMOKE_DIR (a temp dir by default).
func TestRealChromeSmoke(t *testing.T) {
	if os.Getenv("SMIND_BROWSER_SMOKE") != "1" {
		t.Skip("set SMIND_BROWSER_SMOKE=1 to run against a real headless Chromium")
	}
	chrome, err := DetectChrome(os.Getenv("SMIND_BROWSER_PATH"))
	if err != nil {
		t.Skipf("no browser installed: %v", err)
	}
	root := os.Getenv("SMIND_BROWSER_SMOKE_DIR")
	if root == "" {
		root = t.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	// The "daemon": a loopback server that must never be hit.
	var daemonHits atomic.Int64
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		daemonHits.Add(1)
		fmt.Fprint(w, "secret")
	}))
	defer daemon.Close()
	daemonPort := daemon.Listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>Smoke</title><body>
<img src="http://10.255.255.1/leak.png">
<script>
console.log(confirm("really?"));
alert("hi");
for (let i = 0; i < 6; i++) window.open("/popup" + i);
var a = document.createElement("a"); a.href = "/file.bin"; a.download = "../../evil.bin"; document.body.appendChild(a); a.click();
try { new WebSocket("ws://127.0.0.1:%d/ws"); new WebSocket("ws://localhost:%d/ws"); } catch (e) {}
console.log("loaded");
</script>`, daemonPort, daemonPort)
	})
	mux.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="evil.bin"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "BINARY")
	})
	mux.HandleFunc("/popup", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<title>p</title>popup") })
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%d/secret", daemonPort), http.StatusFound)
	})
	mux.HandleFunc("/redir-file", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	pol, err := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: daemonPort}})
	if err != nil {
		t.Fatal(err)
	}
	profiles := filepath.Join(root, "profiles")
	_ = os.MkdirAll(profiles, 0o700)
	var launched Instance
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := Start(ctx, Config{
		Workspace:   "smoke",
		Launcher:    recordingLauncher{inner: ChromeLauncher{Path: chrome}, out: &launched},
		Launch:      LaunchOpts{ProfileRoot: profiles},
		Policy:      pol,
		CallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tab, err := s.Navigate(ctx, "", base+"/")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if tab.Title != "Smoke" || !strings.HasPrefix(tab.URL, base) {
		t.Fatalf("tab: %+v", tab)
	}
	time.Sleep(1500 * time.Millisecond) // popups and the download settle
	notes := s.DrainNotices()
	kinds := map[string]int{}
	for _, n := range notes {
		kinds[n.Kind]++
		t.Logf("notice: %s: %s", n.Kind, n.Detail)
	}
	if kinds["blocked"] == 0 {
		t.Error("the image on a non-allowlisted address must be blocked")
	}
	if kinds["dialog"] < 2 {
		t.Errorf("confirm and alert must be handled and reported, got %d", kinds["dialog"])
	}
	if kinds["download"] == 0 {
		t.Error("the download must be denied and reported")
	}
	joined := strings.Join(s.Console(), "\n")
	if !strings.Contains(joined, "log: false") || !strings.Contains(joined, "log: loaded") {
		t.Errorf("confirm must return false and the page must keep running: %q", joined)
	}
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) > DefaultMaxTabs {
		t.Errorf("pool breached: %d tabs", len(tabs))
	}
	t.Logf("tabs open: %d, popups refused: %d", len(tabs), kinds["popup_refused"])

	// KNOWN GAP (see the plan, "Known gaps"): the Fetch domain does not see
	// WebSocket handshakes and Network.setBlockedURLs did not stop them in the
	// measured Chrome, so a page on an allowed host can open a WebSocket to a
	// denied loopback port. Logged, not asserted, until the enforcing proxy lands.
	wsHits := daemonHits.Load()
	t.Logf("KNOWN GAP: daemon hits from page WebSockets: %d", wsHits)
	// A redirect into the daemon, and a redirect to file:, never get through.
	for _, path := range []string{"/redir", "/redir-file"} {
		if _, err := s.Navigate(ctx, tabs[0].ID, base+path); err == nil {
			t.Errorf("%s must fail", path)
		}
	}
	if daemonHits.Load() != wsHits {
		t.Fatalf("the daemon was reached %d times through a redirect", daemonHits.Load()-wsHits)
	}
	if _, err := s.Navigate(ctx, tabs[0].ID, "https://example.com/"); err == nil {
		t.Error("a public host must be refused before it is sent")
	}

	s.Close()
	waitGone(t, launched, profiles)
}

type recordingLauncher struct {
	inner Launcher
	out   *Instance
}

func (r recordingLauncher) Launch(ctx context.Context, o LaunchOpts) (Instance, error) {
	i, err := r.inner.Launch(ctx, o)
	*r.out = i
	return i, err
}

func waitGone(t *testing.T, inst Instance, profiles string) {
	t.Helper()
	ci, ok := inst.(*chromeInstance)
	if !ok {
		return
	}
	select {
	case <-ci.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the browser process is still running after Close")
	}
	if left, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*")); len(left) != 0 {
		t.Fatalf("ephemeral profile left behind: %v", left)
	}
}
