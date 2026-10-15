package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
)

const goodPassword = "a perfectly fine password"

// testHTTP never waits forever on a socket that went nowhere.
var testHTTP = &http.Client{Timeout: 10 * time.Second}

func cliHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	for _, k := range []string{config.EnvListen, config.EnvHostnames, config.EnvPassword} {
		t.Setenv(k, "")
	}
	return home
}

func TestSetPasswordRejectsShort(t *testing.T) {
	home := cliHome(t)
	var out, errb bytes.Buffer
	code := runSetPassword([]string{"--password-stdin"}, strings.NewReader("short\n"), &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "at least 12 characters") {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("a rejected password must not touch config.yaml")
	}
	if strings.Contains(out.String()+errb.String(), "short") && !strings.Contains(errb.String(), "at least") {
		t.Fatal("the password was echoed")
	}
}

func TestSetPasswordWritesBcryptMode0600(t *testing.T) {
	home := cliHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("# my notes\nserver:\n  port: 4650 # custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := runSetPassword([]string{"--password-stdin"}, strings.NewReader(goodPassword+"\r\n"), &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	raw, _ := os.ReadFile(path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Server.PasswordHash, "$2") || !auth.CheckPassword(cfg.Server.PasswordHash, goodPassword) {
		t.Fatalf("stored hash %q does not verify the password", cfg.Server.PasswordHash)
	}
	for name, text := range map[string]string{"config.yaml": string(raw), "stdout": out.String(), "stderr": errb.String()} {
		if strings.Contains(text, goodPassword) {
			t.Errorf("%s contains the clear password", name)
		}
	}
	if strings.Contains(out.String()+errb.String(), cfg.Server.PasswordHash) {
		t.Error("the hash was printed")
	}
	if !strings.Contains(string(raw), "# my notes") || !strings.Contains(string(raw), "# custom") {
		t.Errorf("set-password dropped existing comments:\n%s", raw)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("config.yaml mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	if !strings.Contains(out.String(), "restart the daemon") {
		t.Errorf("stdout = %q, want the restart hint", out.String())
	}
}

func TestSetPasswordNonTTYNeedsFlag(t *testing.T) {
	cliHome(t)
	prev := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = prev })
	var out, errb bytes.Buffer
	if code := runSetPassword(nil, strings.NewReader(goodPassword+"\n"), &out, &errb); code != 2 || !strings.Contains(errb.String(), "--password-stdin") {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	// The password is never an argv value.
	errb.Reset()
	if code := runSetPassword([]string{goodPassword}, strings.NewReader(""), &out, &errb); code != 2 || strings.Contains(errb.String(), goodPassword) {
		t.Fatalf("code %d, stderr %q (the argument must not be echoed)", code, errb.String())
	}
}

func TestSetPasswordPromptsTwiceOnATTY(t *testing.T) {
	home := cliHome(t)
	prevT, prevR := stdinIsTerminal, readSecret
	t.Cleanup(func() { stdinIsTerminal, readSecret = prevT, prevR })
	stdinIsTerminal = func() bool { return true }

	answers := []string{goodPassword, goodPassword + "!"}
	readSecret = func(string) (string, error) { a := answers[0]; answers = answers[1:]; return a, nil }
	var out, errb bytes.Buffer
	if code := runSetPassword(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "differ") {
		t.Fatalf("mismatch: code %d, stderr %q", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("mismatching passwords wrote the config")
	}

	answers = []string{goodPassword, goodPassword}
	if code := runSetPassword(nil, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("match: code %d, stderr %q", code, errb.String())
	}
	if v, _ := config.Get("server.passwordHash"); v != "<set>" {
		t.Fatalf("password not stored: %q", v)
	}
}

func TestConfigCommandRoundTrip(t *testing.T) {
	cliHome(t)
	run := func(args ...string) (int, string, string) {
		var o, e bytes.Buffer
		return runConfig(args, &o, &e), o.String(), e.String()
	}
	if code, out, errs := run("set", "server.listen", "0.0.0.0:4650"); code != 0 || !strings.Contains(out, "restart the daemon") || !strings.Contains(errs, "smind set-password") {
		t.Fatalf("set: code %d out %q err %q (want a restart hint and the no-password warning)", code, out, errs)
	}
	if code, out, _ := run("get", "server.listen"); code != 0 || strings.TrimSpace(out) != "0.0.0.0:4650" {
		t.Fatalf("get: %d %q", code, out)
	}
	if code, out, _ := run("get"); code != 0 || !strings.Contains(out, "server.listen = 0.0.0.0:4650") || !strings.Contains(out, "server.passwordHash = (not set)") {
		t.Fatalf("get all: %d %q", code, out)
	}
	if code, _, errs := run("unset", "server.listen"); code != 0 || strings.Contains(errs, "warning") {
		t.Fatalf("unset: %d %q", code, errs)
	}
	if code, _, errs := run("set", "server.bogus", "1"); code != 1 || !strings.Contains(errs, "unknown config key") {
		t.Fatalf("unknown key: %d %q", code, errs)
	}
	for _, bad := range [][]string{nil, {"set", "server.port"}, {"unset"}, {"get", "a", "b"}, {"frobnicate"}} {
		if code, _, errs := run(bad...); code != 2 || !strings.Contains(errs, "usage") {
			t.Errorf("%v: code %d err %q, want a usage error", bad, code, errs)
		}
	}
}

func TestConfigGetNeverPrintsTheHash(t *testing.T) {
	cliHome(t)
	var o, e bytes.Buffer
	if runSetPassword([]string{"--password-stdin"}, strings.NewReader(goodPassword), &o, &e) != 0 {
		t.Fatal(e.String())
	}
	o.Reset()
	e.Reset()
	if code := runConfig([]string{"get"}, &o, &e); code != 0 || strings.Contains(o.String(), "$2") || !strings.Contains(o.String(), "<set>") {
		t.Fatalf("get all: %d %q", code, o.String())
	}
	o.Reset()
	if code := runConfig([]string{"get", "server.passwordHash"}, &o, &e); code != 0 || strings.Contains(o.String(), "$2") {
		t.Fatalf("get passwordHash: %d %q", code, o.String())
	}
	if code := runConfig([]string{"set", "server.passwordHash", "x"}, &o, &e); code != 1 {
		t.Fatalf("set passwordHash: %d", code)
	}
	o.Reset()
	if code := runConfig([]string{"unset", "server.passwordHash"}, &o, &e); code != 0 {
		t.Fatalf("unset passwordHash: %d", code)
	}
	if v, _ := config.Get("server.passwordHash"); v != "" {
		t.Fatalf("hash still set: %q", v)
	}
}

func TestServeFlagsStillRejected(t *testing.T) {
	for _, flag := range []string{"--port", "--port=4650", "--listen", "--listen=0.0.0.0:4648", "--hostnames=x.com", "--bind", "--password"} {
		var code int
		errOut := captureStderr(t, func() { code = run([]string{"serve", flag}) })
		if code != 2 || !strings.Contains(errOut, "smind config set") {
			t.Errorf("serve %s: code %d stderr %q, want exit 2 pointing at `smind config set`", flag, code, errOut)
		}
		if strings.Contains(errOut, "=") && strings.Contains(flag, "=") && strings.Contains(errOut, strings.SplitN(flag, "=", 2)[1]) {
			t.Errorf("serve %s: the flag's value was echoed: %q", flag, errOut)
		}
	}
	var code int
	errOut := captureStderr(t, func() { code = run([]string{"serve", "extra"}) })
	if code != 2 || !strings.Contains(errOut, "usage: smind serve") {
		t.Errorf("serve extra: %d %q", code, errOut)
	}
}

// --- real daemon processes -------------------------------------------------

type daemonProc struct {
	cmd  *exec.Cmd
	out  *stdBuffer
	done chan error
}

func startServe(t *testing.T, bin, home string, env ...string) *daemonProc {
	t.Helper()
	cmd := exec.Command(bin, "serve")
	cmd.Env = append(smindEnv(home), env...)
	out := &stdBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	d := &daemonProc{cmd: cmd, out: out, done: make(chan error, 1)}
	go func() { d.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-d.done:
		case <-time.After(10 * time.Second):
		}
	})
	return d
}

func TestServeRefusesNonLoopbackWithoutPassword(t *testing.T) {
	bin := buildSmind(t)
	home := cliHome(t)
	port := freePort(t)
	d := startServe(t, bin, home, fmt.Sprintf("%s=0.0.0.0:%d", config.EnvListen, port))
	select {
	case err := <-d.done:
		if err == nil {
			t.Fatal("the daemon started and exited 0; it must refuse")
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the daemon kept running with a non-loopback listen and no password:\n%s", d.out)
	}
	if out := d.out.String(); !strings.Contains(out, "smind set-password") || !strings.Contains(out, "not a loopback address") {
		t.Fatalf("refusal message = %q, want it to name smind set-password", out)
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("something is listening on the port after the refusal")
	}
	// A short SMIND_PASSWORD is refused too.
	d = startServe(t, bin, home, fmt.Sprintf("%s=0.0.0.0:%d", config.EnvListen, port), config.EnvPassword+"=short")
	if err := <-d.done; err == nil || !strings.Contains(d.out.String(), "at least 12") {
		t.Fatalf("weak SMIND_PASSWORD: err=%v out=%q", err, d.out)
	}
}

func TestServeLoopbackDefaultUnchanged(t *testing.T) {
	bin := buildSmind(t)
	home := cliHome(t)
	port := freePort(t)
	writePortConfig(t, home, port)
	startServe(t, bin, home)
	waitDaemonHealthy(t, port, 30*time.Second)

	var h map[string]any
	getJSON(t, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), http.StatusOK, &h)
	if h["authRequired"] != false {
		t.Errorf("healthz = %v, want authRequired=false", h)
	}
	var tok map[string]string
	getJSON(t, fmt.Sprintf("http://127.0.0.1:%d/api/token", port), http.StatusOK, &tok)
	want, _ := auth.LoadOrCreateToken(home)
	if tok["token"] != want {
		t.Errorf("/api/token = %v", tok)
	}
	// Existing clients keep working with no password: the CLI dials with the token file.
	cmd := exec.Command(bin, "workspace", "ls")
	cmd.Env = smindEnv(home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smind workspace ls against the default daemon: %v: %s", err, out)
	}
	// Only the loopback address is bound.
	if lan := lanIP(); lan != "" {
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", lan, port), time.Second); err == nil {
			_ = conn.Close()
			t.Errorf("the default daemon answers on %s", lan)
		}
	}
	// A foreign Host is refused even on loopback (DNS rebinding).
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil)
	req.Host = "evil.example.com"
	resp, err := testHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host: %v %v", resp, err)
	}
	_ = resp.Body.Close()
}

// TestServeDirectConnectionEndToEnd runs a real daemon beyond loopback and
// walks the whole remote flow against it: no standing token, wrong then right
// password, a session token that opens /ws only through the subprotocol, never
// the URL.
//
// Two ways to be "not the local user" without a second machine:
//   - through this machine's LAN address, so the TCP peer is a non-loopback
//     address (skipped when a host firewall swallows it);
//   - through 127.0.0.1 with an external Host name, exactly what a same-host
//     reverse proxy sends (the TCP peer is loopback, the Host is not).
func TestServeDirectConnectionEndToEnd(t *testing.T) {
	bin := buildSmind(t)
	t.Run("from the LAN address", func(t *testing.T) {
		lan := lanIP()
		if lan == "" {
			t.Skip("no non-loopback IPv4 address to play the remote client")
		}
		runDirectFlow(t, bin, func(port int) (string, string) { return fmt.Sprintf("%s:%d", lan, port), "" }, true)
	})
	t.Run("same-host reverse proxy (loopback peer, external Host)", func(t *testing.T) {
		runDirectFlow(t, bin, func(port int) (string, string) { return fmt.Sprintf("127.0.0.1:%d", port), "smind.example.com" }, false)
	})
}

// runDirectFlow starts a daemon on 0.0.0.0 with a password and drives it from
// the address target(port) returns, presenting hostHeader when non-empty.
func runDirectFlow(t *testing.T, bin string, target func(port int) (addr, hostHeader string), skipIfUnreachable bool) {
	home := cliHome(t)
	port := freePort(t)
	var o, e bytes.Buffer
	if code := runSetPassword([]string{"--password-stdin"}, strings.NewReader(goodPassword), &o, &e); code != 0 {
		t.Fatal(e.String())
	}
	d := startServe(t, bin, home, fmt.Sprintf("%s=0.0.0.0:%d", config.EnvListen, port), config.EnvHostnames+"=smind.example.com")
	waitDaemonHealthy(t, port, 30*time.Second)
	addr, hostHeader := target(port)
	base := "http://" + addr

	do := func(client *http.Client, method, path string, body []byte, hdr http.Header) (*http.Response, []byte) {
		req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if hostHeader != "" {
			req.Host = hostHeader
		}
		for k, v := range hdr {
			req.Header[k] = v
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}

	if skipIfUnreachable {
		// TCP can connect and still go nowhere (the macOS application firewall
		// holds unsigned binaries' LAN traffic), so probe with a real request.
		req, _ := http.NewRequest("GET", base+"/healthz", nil)
		if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req); err != nil {
			t.Skipf("this machine cannot reach its own daemon on %s (host firewall or VPN interface?): %v", addr, err)
		} else {
			_ = resp.Body.Close()
		}
	}

	_, hb := do(testHTTP, "GET", "/healthz", nil, nil)
	var h map[string]any
	_ = json.Unmarshal(hb, &h)
	if h["authRequired"] != true {
		t.Fatalf("healthz = %s", hb)
	}
	daemonTok, _ := auth.LoadOrCreateToken(home)
	resp, body := do(testHTTP, "GET", "/api/token", nil, nil)
	if resp.StatusCode != http.StatusForbidden || strings.Contains(string(body), daemonTok) {
		t.Fatalf("/api/token as a remote client: %d %s", resp.StatusCode, body)
	}
	// A name nobody configured is refused outright.
	if hostHeader != "" {
		req, _ := http.NewRequest("GET", base+"/healthz", nil)
		req.Host = "other.example.org"
		if r, err := testHTTP.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
			t.Fatalf("an unlisted Host: %v %v", r, err)
		}
	}

	jsonHdr := http.Header{"Content-Type": {"application/json"}}
	wrongBody, _ := json.Marshal(map[string]string{"password": "not the password!!"})
	if resp, _ := do(testHTTP, "POST", "/api/login", wrongBody, jsonHdr); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	rightBody, _ := json.Marshal(map[string]string{"password": goodPassword})
	resp, body = do(testHTTP, "POST", "/api/login", rightBody, jsonHdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("right password: %d %s", resp.StatusCode, body)
	}
	var login struct{ Token string }
	_ = json.Unmarshal(body, &login)
	if login.Token == "" || login.Token == daemonTok {
		t.Fatalf("login token %q", login.Token)
	}

	wsHdr := http.Header{}
	if hostHeader != "" {
		wsHdr.Set("Host", hostHeader)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Subprotocols: []string{"smind", "smind.bearer." + login.Token}}
	ws, wresp, err := dialer.Dial("ws://"+addr+"/ws", wsHdr)
	if err != nil {
		t.Fatalf("ws with a session token: %v", err)
	}
	if got := wresp.Header.Get("Sec-WebSocket-Protocol"); got != "smind" {
		t.Fatalf("negotiated subprotocol %q", got)
	}
	if err := ws.WriteJSON(map[string]any{"id": "1", "method": "workspace.list"}); err != nil {
		t.Fatal(err)
	}
	var reply struct{ ID string }
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := ws.ReadJSON(&reply); err != nil || reply.ID != "1" {
		t.Fatalf("workspace.list over the remote socket: %+v %v", reply, err)
	}
	_ = ws.Close()

	plain := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if _, r, err := plain.Dial("ws://"+addr+"/ws?token="+daemonTok, wsHdr); err == nil || r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the daemon token must be refused from a remote client: %v %v", err, r)
	}
	if _, _, err := plain.Dial("ws://"+addr+"/ws?token="+login.Token, wsHdr); err == nil {
		t.Fatal("a session token in the URL must be refused")
	}
	crossOrigin := http.Header{"Origin": {"http://evil.example"}}
	for k, v := range wsHdr {
		crossOrigin[k] = v
	}
	if _, _, err := dialer.Dial("ws://"+addr+"/ws", crossOrigin); err == nil {
		t.Fatal("a cross-origin upgrade must be refused even with a valid session")
	}

	// The daemon's own log carries no secret, and says it is beyond loopback.
	for _, secret := range []string{goodPassword, login.Token, daemonTok} {
		if strings.Contains(d.out.String(), secret) {
			t.Errorf("the daemon log contains a secret:\n%s", d.out)
		}
	}
	if !strings.Contains(d.out.String(), "WARNING") {
		t.Errorf("a daemon beyond loopback should say so in its log:\n%s", d.out)
	}
	// Local CLI keeps working beside it (it dials 127.0.0.1 with the token file).
	cmd := exec.Command(bin, "workspace", "ls")
	cmd.Env = append(smindEnv(home), fmt.Sprintf("%s=0.0.0.0:%d", config.EnvListen, port))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smind workspace ls beside a daemon on 0.0.0.0: %v: %s", err, out)
	}
}

func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			return ipn.IP.String()
		}
	}
	return ""
}

func getJSON(t *testing.T, url string, wantStatus int, into any) {
	t.Helper()
	resp, err := testHTTP.Get(url) //nolint:gosec // test URL
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d %s, want %d", url, resp.StatusCode, b, wantStatus)
	}
	_ = json.NewDecoder(resp.Body).Decode(into)
}

func postLogin(t *testing.T, base, password string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"password": password})
	resp, err := testHTTP.Post(base+"/api/login", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
