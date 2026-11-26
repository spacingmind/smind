package browser

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testProxy = "127.0.0.1:9"

func TestLauncherArgs(t *testing.T) {
	args, err := buildArgs("/p/profile", LaunchOpts{AllowHosts: []string{"intranet.test"}, HeapMB: 256, ProxyAddr: testProxy})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"--headless=new",
		"--remote-debugging-pipe",
		"--use-mock-keychain",
		"--password-store=basic",
		"--user-data-dir=/p/profile",
		"--js-flags=--max-old-space-size=256",
		"--proxy-server=http://" + testProxy,
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
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
	// The bypass list is exactly "<-loopback>": an empty list leaves loopback
	// direct (measured on Chrome), which would skip the policy.
	foundBypass := false
	for _, a := range args {
		if strings.HasPrefix(a, "--proxy-bypass-list") {
			foundBypass = true
			if a != "--proxy-bypass-list=<-loopback>" {
				t.Errorf("bypass list must be exactly <-loopback>, got %q", a)
			}
		}
		if a == "--no-proxy-server" {
			t.Error("a browser must never be launched without the proxy")
		}
	}
	if !foundBypass {
		t.Error("the bypass list argument is missing")
	}
	for _, bad := range []string{"--no-sandbox", "--remote-allow-origins", "--disable-web-security", "--remote-debugging-address", "--remote-debugging-port", "--allow-file-access"} {
		if strings.Contains(joined, bad) {
			t.Errorf("argument %s must never be used", bad)
		}
	}
	def, _ := buildArgs("/p", LaunchOpts{ProxyAddr: testProxy})
	if strings.Contains(strings.Join(def, " "), "max-old-space-size=0") {
		t.Fatal("a zero heap must fall back to the default cap")
	}
}

// fakeChrome writes a script that behaves like a browser for the launcher.
// echo: it copies fd 3 (what the browser reads) to fd 4 (what it writes),
// proving the pipe wiring; otherwise it ignores its pipe and sleeps. Either
// way it records its pid next to the profile dir.
func fakeChrome(t *testing.T, root string, echo bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake browser")
	}
	body := `#!/bin/sh
for a in "$@"; do case "$a" in --user-data-dir=*) d="${a#--user-data-dir=}";; esac; done
echo $$ > "$d.pid"
`
	if echo {
		body += "cat <&3 >&4\n"
	} else {
		body += "sleep 60\n"
	}
	p := filepath.Join(root, "fake-chrome")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func profileAndPid(t *testing.T, profiles string) (dir string, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*"))
		dir, pid = "", 0
		for _, e := range entries {
			if strings.HasSuffix(e, ".pid") {
				b, _ := os.ReadFile(e)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			} else {
				dir = e
			}
		}
		if dir != "" && pid != 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no profile dir and pid file in %s", profiles)
	return
}

func TestLauncherPipeRoundTrip(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "profiles")
	_ = os.Mkdir(profiles, 0o700)
	inst, err := ChromeLauncher{Path: fakeChrome(t, root, true)}.Launch(context.Background(), LaunchOpts{ProfileRoot: profiles, ProxyAddr: testProxy})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	dir, _ := profileAndPid(t, profiles)
	if _, err := os.Stat(filepath.Join(dir, "DevToolsActivePort")); !os.IsNotExist(err) {
		t.Fatal("there must be no DevToolsActivePort: nothing listens")
	}
	conn := inst.Conn()
	if _, err := conn.Write([]byte("{\"id\":1,\"result\":{}}\x00")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "{\"id\":1,\"result\":{}}\x00" {
		t.Fatalf("round trip over fd 3/4: %q %v", buf[:n], err)
	}
}

func TestLauncherCloseKillsTreeAndRemovesProfile(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "profiles")
	_ = os.Mkdir(profiles, 0o700)
	inst, err := ChromeLauncher{Path: fakeChrome(t, root, false)}.Launch(context.Background(), LaunchOpts{ProfileRoot: profiles, ProxyAddr: testProxy})
	if err != nil {
		t.Fatal(err)
	}
	dir, pid := profileAndPid(t, profiles)
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

func TestLauncherKillsTheGroupEvenAfterTheLeaderExited(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups")
	}
	root := t.TempDir()
	profiles := filepath.Join(root, "profiles")
	_ = os.Mkdir(profiles, 0o700)
	// The leader exits at once but leaves a grandchild (a renderer) behind.
	script := filepath.Join(root, "leaves-a-child")
	body := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --user-data-dir=*) d=\"${a#--user-data-dir=}\";; esac; done\nsleep 60 &\necho $! > \"$d.pid\"\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	inst, err := ChromeLauncher{Path: script}.Launch(context.Background(), LaunchOpts{ProfileRoot: profiles, ProxyAddr: testProxy})
	if err != nil {
		t.Fatal(err)
	}
	_, child := profileAndPid(t, profiles)
	select {
	case <-inst.(*chromeInstance).exited:
	case <-time.After(3 * time.Second):
		t.Fatal("the leader should have exited")
	}
	if !processAlive(child) {
		t.Fatal("test setup: the grandchild must be alive")
	}
	_ = inst.Close()
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(child) {
		t.Fatal("Close must kill the process group after the leader exited")
	}
}

// deadPid returns the pid of a process that has exited, portably: the test
// binary itself, started so it runs no tests.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestSweepStaleProfiles(t *testing.T) {
	root := t.TempDir()
	dead := strconv.Itoa(deadPid(t))
	mk := func(name, owner string, age time.Duration) string {
		d := filepath.Join(root, name)
		_ = os.Mkdir(d, 0o700)
		if owner != "" {
			_ = os.WriteFile(filepath.Join(d, ownerFile), []byte(owner), 0o600)
		}
		old := time.Now().Add(-age)
		_ = os.Chtimes(d, old, old)
		return d
	}
	stale := mk("smind-browser-stale", dead+" 123", 0) // dead owner: stale however new
	oldNoOwner := mk("smind-browser-oldnoowner", "", 2*time.Minute)
	oldGarbage := mk("smind-browser-oldgarbage", "nope", 2*time.Minute)
	newNoOwner := mk("smind-browser-newnoowner", "", 0) // being created right now by a launch
	newGarbage := mk("smind-browser-newgarbage", "nope", 10*time.Second)
	live := mk("smind-browser-live", strconv.Itoa(os.Getpid())+" 456", 2*time.Hour)
	other := mk("unrelated", dead, 2*time.Hour)
	SweepStaleProfiles(root)
	for _, gone := range []string{stale, oldNoOwner, oldGarbage} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s must be swept", gone)
		}
	}
	for _, kept := range []string{newNoOwner, newGarbage, live, other} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s must be kept (a live or brand-new profile is never swept)", kept)
		}
	}
}

func TestWriteOwnerIsAtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	if err := writeOwner(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ownerFile))
	f := strings.Fields(string(b))
	if err != nil || len(f) != 2 || f[0] != strconv.Itoa(os.Getpid()) {
		t.Fatalf("owner file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ownerFile+".tmp")); !os.IsNotExist(err) {
		t.Fatal("the temp file must be renamed away")
	}
	if err := writeOwner(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a failed owner write must be an error (the launch fails on it)")
	}
}

func TestLauncherArgsRejectBadAllowHost(t *testing.T) {
	for _, h := range []string{"a.test, MAP * 1.2.3.4", "*.test", "x y", anyV4} {
		if _, err := buildArgs("/p", LaunchOpts{AllowHosts: []string{h}, ProxyAddr: testProxy}); err == nil {
			t.Errorf("allow host %q must be rejected before it reaches a resolver rule", h)
		}
	}
}

func TestBrowserEnvDropsSecrets(t *testing.T) {
	got := strings.Join(browserEnv([]string{"PATH=/bin", "HOME=/h", "SMIND_TOKEN=secret", "ANTHROPIC_API_KEY=k", "LANG=C"}), " ")
	if strings.Contains(got, "secret") || strings.Contains(got, "ANTHROPIC") || !strings.Contains(got, "PATH=/bin") {
		t.Fatalf("env: %q", got)
	}
}

func TestBrowserEnvDropsProxyVariables(t *testing.T) {
	in := []string{"PATH=/bin"}
	for _, n := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		in = append(in, n+"=http://u:pw@p:1")
	}
	out := strings.Join(browserEnv(in), " ")
	if strings.Contains(out, "PROXY") || strings.Contains(out, "proxy") || strings.Contains(out, "pw@") {
		t.Fatalf("browser env carries a proxy variable: %q", out)
	}
	if !strings.Contains(out, "PATH=/bin") {
		t.Fatalf("browser env lost PATH: %q", out)
	}
	args, err := buildArgs(t.TempDir(), LaunchOpts{ProxyAddr: "127.0.0.1:4555"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, "\n"), "--proxy-server=http://127.0.0.1:4555") {
		t.Fatalf("launcher args miss the browser's own proxy: %v", args)
	}
}

func TestLaunchFailsWhenTheOwnerFileCannotBeWritten(t *testing.T) {
	old := ownerWriter
	ownerWriter = func(string) error { return errors.New("disk full") }
	defer func() { ownerWriter = old }()
	profiles := t.TempDir()
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true binary")
	}
	if inst, err := (ChromeLauncher{Path: truePath}).Launch(context.Background(), LaunchOpts{ProfileRoot: profiles, ProxyAddr: testProxy}); err == nil {
		_ = inst.Close()
		t.Fatal("a launch without an owner file could be swept while live: it must fail")
	}
	if left, _ := filepath.Glob(filepath.Join(profiles, "smind-browser-*")); len(left) != 0 {
		t.Fatalf("a failed launch leaves no profile dir: %v", left)
	}
}

func TestLauncherRequiresALoopbackProxy(t *testing.T) {
	for _, addr := range []string{"", net.JoinHostPort(anyV4, "80"), "example.com:3128", "10.0.0.1:3128", "127.0.0.1", "localhost:80x", "127.0.0.1:80x", "127.0.0.1:99999", "127.0.0.1:0", "127.0.0.1:-1"} {
		if _, err := buildArgs("/p", LaunchOpts{ProxyAddr: addr}); err == nil {
			t.Errorf("proxy address %q must be refused: no direct network path, no remote proxy", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:3128", "[::1]:3128"} {
		args, err := buildArgs("/p", LaunchOpts{ProxyAddr: addr})
		if err != nil {
			t.Errorf("%s: %v", addr, err)
			continue
		}
		if !strings.Contains(strings.Join(args, "\n"), "--proxy-server=http://"+addr+"\n") {
			t.Errorf("the proxy URL must bracket an IPv6 host: %q", args)
		}
	}
}

// The browser's own background networking is switched off as far as Chrome lets us: these flags
// are the measured ones (a start-up pre-connect to the search engine and the update, sync and
// metrics services); what remains is refused by the proxy and kept from the agent (pageinit.go).
func TestBuildArgsSilencesBackgroundNetworking(t *testing.T) {
	t.Parallel()
	args, err := buildArgs(t.TempDir(), LaunchOpts{ProxyAddr: "127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	var features string
	for _, a := range args {
		have[a] = true
		if strings.HasPrefix(a, "--disable-features=") {
			features = a
		}
	}
	for _, want := range []string{"--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-default-apps", "--no-first-run", "--disable-client-side-phishing-detection", "--disable-domain-reliability", "--no-pings"} {
		if !have[want] {
			t.Errorf("missing flag %s", want)
		}
	}
	for _, f := range []string{"PreconnectToSearch", "NetworkTimeServiceQuerying", "OptimizationHints", "AutofillServerCommunication"} {
		if !strings.Contains(features, f) {
			t.Errorf("--disable-features must name %s: %q", f, features)
		}
	}
	if strings.Count(strings.Join(args, " "), "--disable-features=") != 1 {
		t.Error("exactly one --disable-features flag (a second one replaces the first)")
	}
}
