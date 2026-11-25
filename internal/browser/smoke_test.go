package browser

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// A second reserved port, so wss:// can be told apart from ws:// by target.
	daemon2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		daemonHits.Add(1)
	}))
	defer daemon2.Close()
	daemon2Port := daemon2.Listener.Addr().(*net.TCPAddr).Port

	// A UDP listener on loopback: WebTransport/QUIC must send it nothing.
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udpPort := udp.LocalAddr().(*net.UDPAddr).Port
	var udpDatagrams atomic.Int64
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := udp.ReadFrom(buf); err != nil {
				return
			}
			udpDatagrams.Add(1)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>Smoke</title><body>
<img src="http://10.255.255.1/leak.png">
<script>
console.log(confirm("really?"));
alert("hi");
for (let i = 0; i < 6; i++) window.open("/popup" + i);
var a = document.createElement("a"); a.href = "/file.bin"; a.download = "../../evil.bin"; document.body.appendChild(a); a.click();
try { new WebSocket("ws://127.0.0.1:%d/ws"); new WebSocket("ws://localhost:%d/ws"); new WebSocket("ws://sock.localhost:%d/ws"); new WebSocket("wss://127.0.0.1:%d/ws"); } catch (e) {}
fetch("http://127.0.0.1:%d/f", {mode: "no-cors"}).catch(function () {});
fetch("http://localhost:%d/f", {mode: "no-cors"}).catch(function () {});
fetch("http://169.254.169.254:%d/f", {mode: "no-cors"}).catch(function () {});
try { new WebSocket("ws://169.254.169.254:%d/ws"); new WebSocket("ws://[fe80::1]:%d/ws"); } catch (e) {}
try { new WebTransport("https://127.0.0.1:%d/wt").ready.catch(function () {}); } catch (e) {}
console.log("loaded");
</script>`, daemonPort, daemonPort, daemonPort, daemon2Port, daemonPort, daemonPort, daemonPort, daemonPort, daemonPort, udpPort)
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

	pol, err := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: daemonPort}, {Host: "127.0.0.1", Port: daemon2Port}})
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

	// Another local process (a curl) talking to the proxy port without the
	// session secret is refused before anything else happens.
	{
		pc, err := net.Dial("tcp", s.proxy.Addr())
		if err != nil {
			t.Fatal(err)
		}
		_ = pc.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprintf(pc, "GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", daemonPort)
		reply, _ := io.ReadAll(pc)
		pc.Close()
		if !strings.HasPrefix(string(reply), "HTTP/1.1 407") || strings.Contains(string(reply), s.proxy.Secret()) {
			t.Errorf("a direct request to the proxy without the secret must get a bare 407: %q", reply)
		}
	}

	// Nothing may listen: no DevToolsActivePort, and no TCP listener in the browser's process group.
	if dirs, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*", "DevToolsActivePort")); len(dirs) != 0 {
		t.Errorf("DevToolsActivePort exists: %v", dirs)
	}
	if lsof, err := exec.LookPath("lsof"); err == nil {
		pid := launched.(*chromeInstance).cmd.Process.Pid
		out, _ := exec.Command(lsof, "-nP", "-a", "-g", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN").CombinedOutput()
		if strings.Contains(string(out), "LISTEN") {
			t.Errorf("the browser must not listen on TCP:\n%s", out)
		}
	}

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
	// The browser's own background requests (search pre-connect, Google sign-in cookie query)
	// are refused by the proxy but are not the page's: the agent hears nothing about them.
	for _, n := range notes {
		if strings.Contains(n.Detail, "google.com") {
			t.Errorf("a notice about the browser's own traffic reached the agent: %s: %s", n.Kind, n.Detail)
		}
	}
	t.Logf("proxy refusals no page asked for (kept from the agent): %d", s.Unattributed())
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

	// The page opened WebSockets (ws and wss, by IP literal and by name) and
	// fetches to the denied daemon port, to a link-local address, and a
	// WebTransport to a UDP port. The proxy sees all of them (Fetch
	// interception cannot see a WebSocket); positive evidence first: wait until
	// the proxy has refused each, then assert nothing reached its target.
	// Each target is matched by its exact notice form: a WebSocket reaches the
	// proxy as CONNECT, so its notice starts with host:port; a fetch arrives in
	// absolute form, so its notice starts with http://. One cannot stand in for
	// the other.
	wantRefused := []string{
		fmt.Sprintf("127.0.0.1:%d:", daemonPort),          // ws://127.0.0.1
		fmt.Sprintf("localhost:%d:", daemonPort),          // ws://localhost
		fmt.Sprintf("sock.localhost:%d:", daemonPort),     // ws://sock.localhost
		fmt.Sprintf("127.0.0.1:%d:", daemon2Port),         // wss://127.0.0.1
		fmt.Sprintf("http://127.0.0.1:%d/f:", daemonPort), // fetch by IP
		fmt.Sprintf("http://localhost:%d/f:", daemonPort), // fetch by name
		fmt.Sprintf("169.254.169.254:%d:", daemonPort),    // ws:// to link-local: <-loopback> also removed that implicit bypass
		fmt.Sprintf("http://169.254.169.254:%d/f:", daemonPort),
		fmt.Sprintf("[fe80::1]:%d:", daemonPort),
	}
	seen := map[string]bool{}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		for _, n := range notes {
			for _, w := range wantRefused {
				if strings.HasPrefix(n.Detail, w) {
					seen[w] = true
				}
			}
		}
		if len(seen) == len(wantRefused) {
			break
		}
		time.Sleep(200 * time.Millisecond)
		notes = append(notes, s.DrainNotices()...)
	}
	for _, w := range wantRefused {
		if !seen[w] {
			t.Errorf("the proxy never reported refusing %s: the page's connection did not reach it", w)
		}
	}
	if n := daemonHits.Load(); n != 0 {
		t.Errorf("the denied daemon port was reached %d times by page WebSockets or fetches", n)
	}
	if n := udpDatagrams.Load(); n != 0 {
		t.Errorf("WebTransport sent %d datagrams: it must be refused under the proxy", n)
	}
	// A redirect into the daemon, and a redirect to file:, never get through.
	for _, path := range []string{"/redir", "/redir-file"} {
		if _, err := s.Navigate(ctx, tabs[0].ID, base+path); err == nil {
			t.Errorf("%s must fail", path)
		}
	}
	if daemonHits.Load() != 0 {
		t.Fatalf("the daemon was reached %d times through a redirect", daemonHits.Load())
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
