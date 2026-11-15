package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealChromeToolsSmoke drives the broker's read tools against a real
// headless Chromium and a page this test serves on loopback (opt-in, like the
// other smoke: SMIND_BROWSER_SMOKE=1). It checks what the fakes cannot: that the
// accessibility snapshot hides a password value, that the screenshot decodes,
// stays within its caps and has the password field painted over at the right
// place, and that console and wait work, all through the proxy.
func TestRealChromeToolsSmoke(t *testing.T) {
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
	profiles := filepath.Join(root, "profiles")
	_ = os.MkdirAll(profiles, 0o700)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Tools page</title>
<body style="margin:0;background:#2040ff">
<h1 style="color:white">Welcome back</h1>
<form><label style="color:white">User <input name="u" value="alice"></label><br>
<label style="color:white">Password <input id="pw" type="password" value="hunter2-SECRET" style="position:absolute;left:100px;top:120px;width:240px;height:36px"></label></form>
<a href="http://evil.test/secret-destination">a link</a>
<script>console.log("page console says hi"); setTimeout(function(){ var p=document.createElement("p"); p.textContent="late arrival"; document.body.appendChild(p); }, 700);</script>
</body>`)
	}))
	defer srv.Close()
	base := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	pol, err := NewPolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := Start(ctx, Config{Workspace: "tools", Launcher: ChromeLauncher{Path: chrome}, Launch: LaunchOpts{ProfileRoot: profiles}, Policy: pol, CallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := NewBroker(BrokerConfig{Session: func(context.Context, Run) (*Session, error) { return s, nil }})
	run := Run{ID: "smoke", WorkspaceID: 1, ImagesRead: true}
	call := func(tool string, args any) Result {
		raw, _ := json.Marshal(args)
		return b.Call(ctx, run, tool, raw)
	}

	res := call("browser_open", map[string]any{"url": base + "/"})
	if res.IsError || !strings.Contains(text(res), "title: Tools page") {
		t.Fatalf("open: %s", text(res))
	}

	snap := text(call("browser_snapshot", map[string]any{}))
	if strings.Contains(snap, "hunter2") || strings.Contains(snap, "SECRET") {
		t.Fatalf("a password value must never appear in a snapshot:\n%s", snap)
	}
	if !strings.Contains(snap, "(password)") || !strings.Contains(snap, "Welcome back") || !strings.Contains(snap, `value="alice"`) {
		t.Fatalf("the snapshot must show the page, the ordinary field and mark the password field:\n%s", snap)
	}
	if strings.Contains(snap, "evil.test") || strings.Contains(snap, "secret-destination") {
		t.Fatalf("a link destination must not appear:\n%s", snap)
	}
	checkEnvelope(t, "real snapshot", snap)

	shotRes := call("browser_screenshot", map[string]any{})
	var img *Content
	for i := range shotRes.Content {
		if shotRes.Content[i].Type == "image" {
			img = &shotRes.Content[i]
		}
	}
	if shotRes.IsError || img == nil {
		t.Fatalf("screenshot: %s", text(shotRes))
	}
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil || len(raw) > MaxShotBytes {
		t.Fatalf("the image must decode and fit the cap: err=%v len=%d", err, len(raw))
	}
	im, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if long := max(im.Bounds().Dx(), im.Bounds().Dy()); long > MaxShotLongSide {
		t.Fatalf("long side %d", long)
	}
	// The password input sits at css (100,120) 240x36 on a blue page: that
	// region must be the redaction gray, the page background elsewhere stays blue.
	if !near(im.At(220, 138), 128, 128, 128, 40) {
		t.Errorf("the password field must be painted over at the right place, pixel at its centre is %v", im.At(220, 138))
	}
	if !near(im.At(700, 400), 0x20, 0x40, 0xff, 50) {
		t.Errorf("the rest of the page must be untouched, pixel is %v", im.At(700, 400))
	}

	if out := text(call("browser_console", map[string]any{})); !strings.Contains(out, "page console says hi") {
		t.Fatalf("console: %s", out)
	}
	if out := text(call("browser_wait", map[string]any{"text": "late arrival", "timeoutSeconds": 5})); !strings.Contains(out, "found: true") {
		t.Fatalf("wait for late text: %s", out)
	}
	if out := text(call("browser_wait", map[string]any{"text": "never appears", "timeoutSeconds": 1})); !strings.Contains(out, "not found within the timeout") {
		t.Fatalf("a wait must time out softly: %s", out)
	}
	if res := call("browser_open", map[string]any{"url": "http://example.com/"}); !res.IsError {
		t.Fatalf("a public host must be refused")
	}
}
