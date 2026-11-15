package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLauncherArgs(t *testing.T) {
	args := buildArgs("/p/profile", LaunchOpts{AllowHosts: []string{"intranet.test"}, HeapMB: 256})
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir=/p/profile",
		"--js-flags=--max-old-space-size=256",
		"--no-proxy-server",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	var rules string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--host-resolver-rules="); ok {
			rules = v
		}
	}
	if !strings.HasPrefix(rules, "MAP * ~NOTFOUND") {
		t.Fatalf("everything must be mapped to nothing first: %q", rules)
	}
	for _, ex := range []string{"EXCLUDE localhost", "EXCLUDE *.localhost", "EXCLUDE intranet.test"} {
		if !strings.Contains(rules, ex) {
			t.Errorf("rules %q miss %q", rules, ex)
		}
	}
	for _, bad := range []string{"--no-sandbox", "--remote-allow-origins", "--disable-web-security", "--remote-debugging-address", "--allow-file-access"} {
		if strings.Contains(joined, bad) {
			t.Errorf("argument %s must never be used", bad)
		}
	}
	if strings.Contains(strings.Join(buildArgs("/p", LaunchOpts{}), " "), "max-old-space-size=0") {
		t.Fatal("a zero heap must fall back to the default cap")
	}
}

// fakeChrome writes a script that behaves like a browser for the launcher: it
// publishes DevToolsActivePort in its user-data-dir and records its pid.
func fakeChrome(t *testing.T, root string, ready bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake browser")
	}
	body := `#!/bin/sh
for a in "$@"; do case "$a" in --user-data-dir=*) d="${a#--user-data-dir=}";; esac; done
echo $$ > "$d.pid"
`
	if ready {
		body += "printf '12345\\n/devtools/browser/abc\\n' > \"$d/DevToolsActivePort\"\n"
	}
	body += "sleep 60\n"
	p := filepath.Join(root, "fake-chrome")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLauncherReadsDevToolsActivePort(t *testing.T) {
	root := t.TempDir()
	script := fakeChrome(t, root, true)
	profiles := filepath.Join(root, "profiles")
	_ = os.Mkdir(profiles, 0o700)
	inst, err := ChromeLauncher{Path: script}.Launch(context.Background(), LaunchOpts{ProfileRoot: profiles})
	if err != nil {
		t.Fatal(err)
	}
	if inst.WSURL() != "ws://127.0.0.1:12345/devtools/browser/abc" {
		t.Fatalf("ws url %q", inst.WSURL())
	}
	entries, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*"))
	var dir, pidFile string
	for _, e := range entries {
		if strings.HasSuffix(e, ".pid") {
			pidFile = e
		} else {
			dir = e
		}
	}
	if dir == "" || pidFile == "" {
		t.Fatalf("expected a profile dir and a pid file, got %v", entries)
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if err := inst.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("an ephemeral profile must be removed on close")
	}
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatal("the browser process must be dead after Close")
	}
	_ = inst.Close() // idempotent
}

func TestLauncherFailsWhenBrowserNeverReady(t *testing.T) {
	root := t.TempDir()
	script := fakeChrome(t, root, false)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	profiles := filepath.Join(root, "profiles")
	_ = os.Mkdir(profiles, 0o700)
	if _, err := (ChromeLauncher{Path: script}).Launch(ctx, LaunchOpts{ProfileRoot: profiles}); err == nil {
		t.Fatal("want an error when the endpoint never appears")
	}
	left, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*"))
	for _, l := range left {
		if !strings.HasSuffix(l, ".pid") {
			t.Fatalf("a failed launch must not leave a profile dir: %v", left)
		}
	}
}

func TestLauncherWithoutPathIsUnavailable(t *testing.T) {
	if _, err := (ChromeLauncher{}).Launch(context.Background(), LaunchOpts{}); err == nil {
		t.Fatal("no path must fail")
	}
}
