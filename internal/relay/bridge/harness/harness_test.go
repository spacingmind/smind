package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/relaytest"
)

const testTimeout = 20 * time.Second

// running is one in-process harness with its stdin/stdout wired up.
type running struct {
	t       *testing.T
	ready   Ready
	stdin   io.WriteCloser
	lines   chan string
	backlog []string
	cancel  context.CancelFunc
	done    chan error
}

func startHarness(t *testing.T, cfg Config) *running {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cfg.Stdin, cfg.Stdout, cfg.Stderr = inR, outW, io.Discard
	readyCh := make(chan Ready, 1)
	cfg.OnReady = func(r Ready) { readyCh <- r }

	ctx, cancel := context.WithCancel(context.Background())
	r := &running{t: t, stdin: inW, lines: make(chan string, 1024), cancel: cancel, done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			r.lines <- sc.Text()
		}
	}()
	go func() {
		err := Run(ctx, cfg)
		_ = outW.Close()
		r.done <- err
	}()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		select {
		case <-r.done:
		case <-time.After(testTimeout):
			t.Error("harness did not stop")
		}
	})
	select {
	case r.ready = <-readyCh:
	case err := <-r.done:
		t.Fatalf("harness exited early: %v", err)
	case <-time.After(testTimeout):
		t.Fatal("harness never became ready")
	}
	return r
}

// next returns (and removes) the oldest unread stdout line satisfying match;
// lines it skips stay available to later calls.
func (r *running) next(match func(string) bool) string {
	r.t.Helper()
	deadline := time.After(testTimeout)
	for {
		for i, l := range r.backlog {
			if match(l) {
				r.backlog = append(r.backlog[:i:i], r.backlog[i+1:]...)
				return l
			}
		}
		select {
		case l := <-r.lines:
			r.backlog = append(r.backlog, l)
		case <-deadline:
			r.t.Fatalf("timed out waiting for a stdout line (unread: %q)", r.backlog)
		}
	}
}

func hasPrefix(p ...string) func(string) bool {
	return func(l string) bool {
		for _, x := range p {
			if strings.HasPrefix(l, x) {
				return true
			}
		}
		return false
	}
}

// cmd sends one stdin command and returns its single reply line.
func (r *running) cmd(line string) string {
	r.t.Helper()
	if _, err := fmt.Fprintln(r.stdin, line); err != nil {
		r.t.Fatalf("write stdin: %v", err)
	}
	return r.next(hasPrefix("OK", "ERR", "DEVICES"))
}

func (r *running) mustOK(line string) string {
	r.t.Helper()
	reply := r.cmd(line)
	if !strings.HasPrefix(reply, "OK") {
		r.t.Fatalf("%q -> %q", line, reply)
	}
	return reply
}

func (r *running) devices() []devices.Info {
	r.t.Helper()
	reply := r.cmd("list")
	if !strings.HasPrefix(reply, "DEVICES ") {
		r.t.Fatalf("list -> %q", reply)
	}
	var out []devices.Info
	if err := json.Unmarshal([]byte(strings.TrimPrefix(reply, "DEVICES ")), &out); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// goDevice is a native-gRPC device built on the mobile stub's rpcConn.
type goDevice struct {
	r    *running
	pub  []byte
	priv []byte
	id   string
	dc   *client.DataConn
	c    *rpcConn
	stop context.CancelFunc
}

func newGoDevice(r *running) *goDevice {
	pub, priv, err := devices.GenerateKey()
	if err != nil {
		r.t.Fatal(err)
	}
	return &goDevice{r: r, pub: pub, priv: priv, id: devices.DeviceID(pub)}
}

// connect opens a fresh session (new session id) and sends the hello; it
// returns the hello answer.
func (d *goDevice) connect(label string) rpcResponse {
	d.r.t.Helper()
	resp, err := d.tryConnect(label)
	if err != nil {
		d.r.t.Fatal(err)
	}
	return resp
}

// tryConnect is connect that reports a setup failure (the relay itself
// refuses a revoked device at the handshake) instead of failing the test.
func (d *goDevice) tryConnect(label string) (rpcResponse, error) {
	d.r.t.Helper()
	if d.stop != nil {
		d.stop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.stop = cancel
	d.r.t.Cleanup(cancel)
	rc, closeRC, err := relaytest.DialNative(d.r.ready.NativeAddr, d.r.ready.Fingerprint)
	if err != nil {
		d.r.t.Fatal(err)
	}
	d.r.t.Cleanup(closeRC)
	actx, acancel := context.WithTimeout(ctx, testTimeout)
	defer acancel()
	adm, err := client.Admit(actx, rc, d.r.ready.WorkspaceID, mobileAdmitTag, mustHex(d.r.t, d.r.ready.SecretHex))
	if err != nil {
		return rpcResponse{}, fmt.Errorf("admit: %w", err)
	}
	eph, err := e2ee.GenerateKeyPair()
	if err != nil {
		d.r.t.Fatal(err)
	}
	session := fmt.Sprintf("go-%s-%d", d.id[:6], time.Now().UnixNano())
	d.dc, err = client.OpenData(ctx, rc, adm, d.r.ready.WorkspaceID, session, d.id, eph, e2ee.RoleMobile,
		client.WithExpectedPeerKey(d.r.ready.DaemonPub))
	if err != nil {
		return rpcResponse{}, fmt.Errorf("open data: %w", err)
	}
	hctx, hcancel := context.WithTimeout(ctx, testTimeout)
	defer hcancel()
	if err := d.dc.Handshake(hctx); err != nil {
		return rpcResponse{}, fmt.Errorf("handshake: %w", err)
	}
	c, dc := &rpcConn{dc: d.dc, msgs: make(chan []byte, 64), done: make(chan struct{})}, d.dc
	d.c = c
	go func() {
		defer close(c.done)
		for {
			m, err := dc.Receive()
			if err != nil {
				c.err = err
				return
			}
			c.msgs <- m
		}
	}()
	h := devices.SignHello(d.priv, label, d.r.ready.DaemonPub, eph.Public())
	resp, err := d.c.request(ctx, devices.HelloMethod, h)
	if err != nil {
		return rpcResponse{}, fmt.Errorf("hello: %w", err)
	}
	return resp, nil
}

func (d *goDevice) list() error {
	resp, err := d.c.request(context.Background(), "workspace.list", nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s", resp.Error.Message)
	}
	return nil
}

func (d *goDevice) mustServe(label string) {
	d.r.t.Helper()
	if resp := d.connect(label); resp.Error != nil {
		d.r.t.Fatalf("hello refused: %+v", resp.Error)
	}
	if err := d.list(); err != nil {
		d.r.t.Fatalf("workspace.list: %v", err)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		var v int
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &v); err != nil {
			t.Fatal(err)
		}
		b[i] = byte(v)
	}
	return b
}

func TestLegacyDefaultPrintsReadyAndNative(t *testing.T) {
	r := startHarness(t, Config{})
	got := map[string]string{}
	for len(got) < 3 {
		l := r.next(hasPrefix("READY ", "NATIVE ", "HOME "))
		k, v, _ := strings.Cut(l, " ")
		got[k] = v
	}
	if got["READY"] == "" || got["NATIVE"] != r.ready.NativeAddr || got["HOME"] == "" {
		t.Fatalf("lines: %v", got)
	}
	time.Sleep(200 * time.Millisecond)
	for len(r.lines) > 0 {
		r.backlog = append(r.backlog, <-r.lines)
	}
	for _, l := range r.backlog {
		if strings.HasPrefix(l, "DAEMONPUB") {
			t.Fatalf("legacy mode printed %q", l)
		}
	}
}

func TestHubDeviceLifecycle(t *testing.T) {
	r := startHarness(t, Config{Hub: true})
	l := r.next(hasPrefix("DAEMONPUB "))
	if l == "DAEMONPUB " {
		t.Fatal("empty daemon key")
	}

	a := newGoDevice(r)
	resp := a.connect("laptop")
	if resp.Error == nil || resp.Error.Code != "awaiting_approval" {
		t.Fatalf("first hello = %+v, want awaiting_approval", resp.Error)
	}
	if reply := r.cmd("approve nosuchdevice"); !strings.HasPrefix(reply, "ERR") {
		t.Fatalf("approve unknown -> %q", reply)
	}
	r.mustOK("approve " + a.id[:8])
	a.mustServe("laptop")
	r.mustOK("rename " + a.id + " Long's laptop")

	var found bool
	for _, d := range r.devices() {
		if d.ID == a.id {
			found = true
			if d.Label != "Long's laptop" || !d.Connected {
				t.Fatalf("device info = %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("device not listed")
	}

	// revoke ends the live session and refuses a reconnect.
	r.mustOK("revoke " + a.id)
	select {
	case <-a.c.done:
	case <-time.After(testTimeout):
		t.Fatal("revoked device's stream did not end")
	}
	// The relay refuses a revoked device before the handshake finishes; a
	// daemon that missed the relay-side revoke would answer at hello instead.
	if resp, err := a.tryConnect("laptop"); err == nil && (resp.Error == nil || resp.Error.Code != "revoked") {
		t.Fatalf("hello after revoke = %+v, want revoked", resp.Error)
	}

	// a restarted daemon has no session state: the device re-handshakes on a
	// fresh session and is served again.
	r.mustOK("window 1 60")
	b := newGoDevice(r)
	b.mustServe("phone")
	if reply := r.mustOK("restart-daemon"); reply != "OK restarted" {
		t.Fatalf("restart -> %q", reply)
	}
	// The old session is dead (the new hub never completed its handshake); a
	// client notices on its next request and reconnects like this.
	b.mustServe("phone")
	r.mustOK("quit")
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		r.done <- nil
	case <-time.After(testTimeout):
		t.Fatal("quit did not stop the harness")
	}
}

func TestHubMobileStubGetsServedAndSurvivesRestart(t *testing.T) {
	r := startHarness(t, Config{Hub: true, MobileStub: true})
	r.next(hasPrefix("MOBILE ok 2"))
	r.mustOK("restart-daemon")
	// served again after the restart: counts keep rising past a reconnect.
	r.next(hasPrefix("MOBILE reconnecting"))
	r.next(hasPrefix("MOBILE ok"))
	var mobile string
	for _, d := range r.devices() {
		if d.Label == mobileLabel {
			mobile = d.ID
		}
	}
	if mobile == "" {
		t.Fatal("mobile stub not registered")
	}
	r.mustOK("revoke " + mobile)
	r.next(hasPrefix("MOBILE refused revoked"))
}

func TestMobileStubRequiresHub(t *testing.T) {
	if err := Run(context.Background(), Config{MobileStub: true}); err == nil {
		t.Fatal("expected error")
	}
}
