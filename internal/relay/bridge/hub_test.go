package bridge

import (
	"encoding/json"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/devices"
)

// approvedWindow lets the next n unknown devices in without a human.
func approvedWindow(t *testing.T, f *hubFixture, n int) {
	t.Helper()
	if err := f.reg.OpenWindow(time.Hour, n); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeServesThreeDevicesIndependently(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 3)
	f.start(nil)

	// Two desktop-like native clients and one mobile-like grpc-web client,
	// all attached at once.
	devs := []*device{f.newDevice(false, "desktop-a"), f.newDevice(false, "desktop-b"), f.newDevice(true, "phone")}
	var wg sync.WaitGroup
	for _, d := range devs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.connect()
		}()
	}
	wg.Wait()

	// Each is served on its own connection, concurrently and repeatedly.
	for round := 0; round < 3; round++ {
		for _, d := range devs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if got := d.mustList(); len(got) != 0 {
					t.Errorf("%s: workspace.list = %d entries, want 0", d.label, len(got))
				}
			}()
		}
		wg.Wait()
	}

	infos := f.hub.Devices()
	if len(infos) != 3 {
		t.Fatalf("registry lists %d devices, want 3", len(infos))
	}
	for _, in := range infos {
		if in.State != devices.StateApproved || !in.Connected {
			t.Errorf("device %s: state=%s connected=%v, want approved and connected", in.Label, in.State, in.Connected)
		}
	}
}

func TestEveryDeviceSeesAllWorkspaces(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 3)
	f.start(nil)
	a, b, c := f.newDevice(false, "a"), f.newDevice(false, "b"), f.newDevice(true, "c")
	for _, d := range []*device{a, b, c} {
		d.connect()
	}

	// A workspace created by one device's RPC is listed to the others: every
	// device sees ALL of the daemon's workspaces, not a per-device view.
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, out)
	}
	r, err := a.request("workspace.create", map[string]any{"path": repo, "title": "from-a"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Err != nil {
		t.Fatalf("workspace.create: %+v", r.Err)
	}
	for _, d := range []*device{a, b, c} {
		got := d.mustList()
		if len(got) != 1 {
			t.Fatalf("%s sees %d workspaces, want 1", d.label, len(got))
		}
		var w struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(got[0], &w); err != nil || w.Title != "from-a" {
			t.Fatalf("%s sees %s, want the workspace created by a", d.label, got[0])
		}
	}
}

func TestBridgeDeviceFailureIsolated(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 3)
	f.start(nil)
	a, b, c := f.newDevice(false, "a"), f.newDevice(false, "b"), f.newDevice(true, "c")
	for _, d := range []*device{a, b, c} {
		d.connect()
	}

	// A dies abruptly (its stream is cancelled mid-session) and a fourth
	// client fails its hello; B and C keep being served.
	a.cancel()
	bad := f.newDevice(false, "bad")
	if err := bad.open(""); err != nil {
		t.Fatal(err)
	}
	if err := bad.dc.Send([]byte(`{"id":"h","method":"relay.device_hello","params":{"v":1,"device_pub":"AA==","signature":"AA=="}}`)); err != nil {
		t.Fatal(err)
	}
	r, err := bad.await("h")
	if err != nil || r.Err == nil || r.Err.Code != codeBadHello {
		t.Fatalf("malformed hello answer = %+v, %v; want error code %q", r, err, codeBadHello)
	}
	for i := 0; i < 3; i++ {
		b.mustList()
		c.mustList()
	}
}

func TestBridgeDeviceDetachCancelsOnlyThatDevice(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 3)
	hub := f.start(nil)
	a, b, c := f.newDevice(false, "a"), f.newDevice(false, "b"), f.newDevice(true, "c")
	for _, d := range []*device{a, b, c} {
		d.connect()
	}

	hub.detach(routeRef{device: a.id, session: a.session})

	// The daemon side of A's route is gone: A is no longer answered (its own
	// stream stays attached at the relay, waiting for a daemon side).
	raw, _ := json.Marshal(map[string]any{"id": "after-detach", "method": "workspace.list"})
	if err := a.dc.Send(raw); err != nil {
		t.Fatal(err)
	}
	select {
	case m, ok := <-a.msgs:
		if !ok {
			t.Fatal("A's stream closed; the detach must only end the daemon side")
		}
		t.Fatalf("a detached device was still served: %s", m)
	case <-time.After(time.Second):
	}
	hub.mu.Lock()
	_, still := hub.sessions[routeRef{device: a.id, session: a.session}]
	hub.mu.Unlock()
	if still {
		t.Fatal("the detached session is still registered")
	}
	for i := 0; i < 3; i++ {
		b.mustList()
		c.mustList()
	}
	// Idempotent and harmless for an unknown route.
	hub.detach(routeRef{device: a.id, session: a.session})
	hub.detach(routeRef{device: "nobody", session: "nothing"})
	b.mustList()
}

// --- identity ---

func TestUnknownDeviceOutsideWindowIsPending(t *testing.T) {
	f := newHubFixture(t, Options{})
	f.start(nil)
	d := f.newDevice(false, "laptop")
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := d.hello()
	if err != nil {
		t.Fatal(err)
	}
	if r.Err == nil || r.Err.Code != string(devices.ReasonAwaitingApproval) {
		t.Fatalf("hello outside a window = %+v, want awaiting_approval", r)
	}
	got, ok := f.reg.Get(d.id)
	if !ok || got.State != devices.StatePending {
		t.Fatalf("registry = %+v, %v; want a pending record", got, ok)
	}

	// Approval by a human, then the same device reconnects and is served.
	if _, err := f.hub.ApproveDevice(d.id); err != nil {
		t.Fatal(err)
	}
	d2 := f.newDevice(false, "laptop")
	d2.priv, d2.pub, d2.id = d.priv, d.pub, d.id
	d2.connect()
	d2.mustList()
}

func TestRevokedDeviceRefusedWithReasonAtHello(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "x")
	d.connect()
	d.mustList()

	// Revoked in the registry only (the relay was never told), so the refusal
	// below can only come from the daemon's hello check.
	if _, err := f.reg.Revoke(d.id); err != nil {
		t.Fatal(err)
	}
	d2 := f.newDevice(false, "x")
	d2.priv, d2.pub, d2.id = d.priv, d.pub, d.id
	if err := d2.open(""); err != nil {
		t.Fatalf("the relay should still accept the attach: %v", err)
	}
	r, err := d2.hello()
	if err != nil {
		t.Fatal(err)
	}
	if r.Err == nil || r.Err.Code != codeRevoked {
		t.Fatalf("revoked device's hello = %+v, want error code %q", r, codeRevoked)
	}
}

func TestDeviceHelloReplayInAnotherSessionRefused(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 2)
	f.start(nil)
	victim := f.newDevice(false, "victim")
	victim.connect()

	// A eavesdropper (who cannot read the encrypted hello, but suppose it
	// obtained the signed bytes) replays them in its own session.
	good := devices.SignHello(victim.priv, "victim", f.kp.Public(), victim.eph.Public())
	attacker := f.newDevice(false, "attacker")
	attacker.id, attacker.pub = victim.id, victim.pub // claims the victim's id on its own route
	if err := attacker.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := attacker.request(devices.HelloMethod, good)
	if err != nil || r.Err == nil || r.Err.Code != codeBadHello {
		t.Fatalf("replayed hello answer = %+v, %v; want %q", r, err, codeBadHello)
	}
}

// --- legacy clients ---

func TestLegacyClientServedWhileLegacyDevicesTrue(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	d.id = DefaultDeviceID                           // a pre-identity client uses the fixed route
	if err := d.open(DefaultSessionID); err != nil { // sends no hello
		t.Fatal(err)
	}
	d.mustList() // first message is an ordinary request
	if len(f.reg.List()) != 0 {
		t.Fatal("a legacy device must stay unlisted")
	}
}

func TestLegacyWarningLogged(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	d.id = DefaultDeviceID
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	d.mustList()
	if !f.logs.contains("WARNING") {
		t.Fatal("serving a legacy client must log a warning that it is served without approval")
	}
}

func TestLegacyClientRefusedWhenLegacyDevicesFalse(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: false})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	d.id = DefaultDeviceID
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := d.request("workspace.list", nil)
	if err != nil || r.Err == nil || r.Err.Code != codeHelloRequired {
		t.Fatalf("legacy request answer = %+v, %v; want %q", r, err, codeHelloRequired)
	}
}

func TestLegacyClientSilentForHelloTimeoutIsStillServed(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true, HelloTimeout: 400 * time.Millisecond})
	f.start(nil)
	d := f.newDevice(false, "quiet-app")
	d.id = DefaultDeviceID
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if f.logs.contains("WARNING") {
		t.Fatal("the daemon decided the client was legacy before the hello timeout")
	}
	waitFor(t, "the silent client to be served as legacy after the hello timeout", func() bool { return f.logs.contains("WARNING") })
	d.mustList()
}

func TestSilentClientRefusedWhenLegacyDevicesFalse(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: false, HelloTimeout: 300 * time.Millisecond})
	f.start(nil)
	d := f.newDevice(false, "quiet-app")
	d.id = DefaultDeviceID
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if f.logs.contains("WARNING") {
		t.Fatal("a silent client was served as legacy although relay.legacyDevices is false")
	}
	r, err := d.request("workspace.list", nil)
	if err == nil && r.Err == nil {
		t.Fatal("a silent client was served although relay.legacyDevices is false")
	}
}

func TestLegacyFallbackOnlyOnTheFixedRoute(t *testing.T) {
	// Skipping the hello must not dodge identity: a client on any route id
	// other than the fixed legacy one is refused even while legacy is allowed.
	f := newHubFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	d := f.newDevice(false, "new-style-without-hello")
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := d.request("workspace.list", nil)
	if err != nil || r.Err == nil || r.Err.Code != codeHelloRequired {
		t.Fatalf("answer = %+v, %v; want %q", r, err, codeHelloRequired)
	}
}

func TestNewestLegacySessionReplacesTheOlder(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	first := f.newDevice(false, "old-1")
	first.id = DefaultDeviceID
	if err := first.open("legacy-session-1"); err != nil {
		t.Fatal(err)
	}
	first.mustList()
	second := f.newDevice(false, "old-2")
	second.id = DefaultDeviceID
	if err := second.open("legacy-session-2"); err != nil {
		t.Fatal(err)
	}
	second.mustList()
	f.hub.mu.Lock()
	legacy := 0
	for _, s := range f.hub.sessions {
		if s.legacy {
			legacy++
		}
	}
	f.hub.mu.Unlock()
	if legacy != 1 {
		t.Fatalf("%d legacy sessions, want exactly 1", legacy)
	}
}

func TestSameDeviceNewSessionReplacesItsOldSession(t *testing.T) {
	f := newHubFixture(t, Options{}, relaytestMaxDevices(40))
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "d")
	d.connect()
	d.mustList()
	old := d.session

	// Same persistent key, fresh session (a client that lost its daemon).
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	if r, err := d.hello(); err != nil || r.Err != nil {
		t.Fatalf("hello on the new session = %+v, %v", r, err)
	}
	d.mustList()
	f.hub.mu.Lock()
	_, oldStill := f.hub.sessions[routeRef{device: d.id, session: old}]
	n := len(f.hub.sessions)
	f.hub.mu.Unlock()
	if oldStill || n != 1 {
		t.Fatalf("old session still registered=%v, sessions=%d; want the old one replaced", oldStill, n)
	}
}

func TestBridgeDeviceCapRejectsSeventeenth(t *testing.T) {
	// The relay's own cap is raised so the daemon's is what answers.
	f := newHubFixture(t, Options{}, relaytestMaxDevices(40))
	approvedWindow(t, f, 17)
	f.start(nil)
	var served []*device
	for i := 0; i < DefaultMaxDevices; i++ {
		d := f.newDevice(i%2 == 1, "d")
		d.connect()
		served = append(served, d)
	}
	extra := f.newDevice(false, "seventeenth")
	if err := extra.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := extra.hello()
	if err != nil || r.Err == nil || r.Err.Code != codeTooManyDevices {
		t.Fatalf("17th device's hello = %+v, %v; want %q", r, err, codeTooManyDevices)
	}
	for _, d := range []*device{served[0], served[len(served)-1]} {
		d.mustList()
	}
}

// --- reconnect, restart, liveness ---

func TestBridgeResumesDeviceAfterTransportDrop(t *testing.T) {
	f := newHubFixture(t, Options{})
	proxy := f.viaProxy()
	approvedWindow(t, f, 2)
	hub := f.start(nil)
	a, b := f.newDevice(false, "a"), f.newDevice(true, "b")
	a.connect()
	b.connect()
	before := hub.snapshotSessions()
	if len(before) != 2 {
		t.Fatalf("%d sessions before the drop, want 2", len(before))
	}

	// Only the daemon's connection to the relay is cut; the devices' own
	// streams stay up.
	proxy.killAll()

	// Wait until the daemon is back and serving both sessions on the NEW
	// relay connection. A request handed to the relay while the daemon's old
	// stream is still dying can be lost in flight (the relay has no acks and a
	// gRPC write into a connection that is going away still "succeeds"), which
	// is a property of the protocol, not of resuming: what this test asserts is
	// that the SAME E2EE sessions are resumed in place and keep working.
	waitFor(t, "both sessions to be resumed on the new relay connection", func() bool {
		return hub.resumedSince(before)
	})
	a.mustList()
	b.mustList()

	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, d := range []*device{a, b} {
		s := hub.sessions[routeRef{device: d.id, session: d.session}]
		if s == nil || before[s] == nil {
			t.Fatalf("device %s was re-handshaken instead of resumed in place", d.label)
		}
	}
}

func TestBridgeReannouncesAfterDaemonRestartViaSync(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 3)
	f.start(nil)
	a := f.newDevice(false, "a")
	a.connect()
	a.mustList()

	// The daemon restarts: it loses every E2EE counter.
	f.stop()

	// A new device attaches while no daemon is there; the relay holds it and
	// announces it with Sync when the daemon's control stream returns.
	c := f.newDevice(true, "c")
	opened := make(chan error, 1)
	go func() { opened <- c.open("") }()
	f.start(nil)
	if err := <-opened; err != nil {
		t.Fatalf("device attached during the restart: %v", err)
	}
	r, err := c.hello()
	if err != nil || r.Err != nil {
		t.Fatalf("hello after restart = %+v, %v", r, err)
	}
	c.mustList()

	// The device that was already established cannot resume against a daemon
	// that lost its counters; it re-handshakes with a fresh session id and,
	// being approved already, is served without a window.
	if err := a.open(""); err != nil {
		t.Fatalf("fresh session after restart: %v", err)
	}
	r, err = a.hello()
	if err != nil || r.Err != nil {
		t.Fatalf("hello of a known device after restart = %+v, %v", r, err)
	}
	a.mustList()
}

func TestBridgeDeadStreamAfter75sReconnectsWithJitter(t *testing.T) {
	clock := newFakeClock()
	var mu sync.Mutex
	var jitterSeen []time.Duration
	f := newHubFixture(t, Options{Clock: clock, Jitter: func(d time.Duration) time.Duration {
		mu.Lock()
		jitterSeen = append(jitterSeen, d)
		mu.Unlock()
		return d
	}})
	proxy := f.viaProxy()
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "d")
	d.connect()
	d.mustList()
	before := f.hub.snapshotSessions()

	// The path to the relay goes silent (nothing is closed, nothing arrives).
	proxy.setBlackhole(true)
	clock.Advance(30 * time.Second) // pings and probes go into the void
	mu.Lock()
	n := len(jitterSeen)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("reconnect started after 30 s of silence (jitter calls %d); the rule is 75 s", n)
	}
	advanceUntil(t, clock, time.Second, 60, "the 75 s dead-stream rule to start a reconnect", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(jitterSeen) > 0
	})
	mu.Lock()
	first := jitterSeen[0]
	mu.Unlock()
	if first != time.Second {
		t.Fatalf("first reconnect backoff = %v, want 1s (jitter is applied to it)", first)
	}

	// The path heals; the daemon reconnects and resumes the device in place.
	proxy.setBlackhole(false)
	proxy.killAll()
	stopAdvancing := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopAdvancing:
				return
			default:
				clock.Advance(time.Second)
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
	defer close(stopAdvancing)
	// Only send once the daemon serves the session on its new connection: a
	// request handed to the relay while the old daemon stream is still dying
	// can be lost in flight (no acks in the relay protocol).
	waitFor(t, "the session to be resumed on the new relay connection", func() bool { return f.hub.resumedSince(before) })
	d.mustList()
}

func TestDevicesRevokeClosesStreamAndSendsRevoke(t *testing.T) {
	f := newHubFixture(t, Options{})
	proxy := f.viaProxy()
	approvedWindow(t, f, 2)
	hub := f.start(nil)
	a, b := f.newDevice(false, "a"), f.newDevice(false, "b")
	a.connect()
	b.connect()

	if _, err := hub.RevokeDevice(a.id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.rerr:
	case <-time.After(fixtureTimeout):
		t.Fatal("revoke did not end the device's stream")
	}
	b.mustList() // only that device

	// The RELAY refuses a new attach of the revoked device (the Revoke
	// reached it): a refusal at the relay is a PermissionDenied on the
	// stream, before any E2EE handshake — distinct from the daemon's refusal.
	a2 := f.newDevice(false, "a")
	a2.priv, a2.pub, a2.id = a.priv, a.pub, a.id
	err := a2.open("")
	if err == nil || relayStatus(err) != codePermissionDenied {
		t.Fatalf("attach of a revoked device = %v, want the relay's PermissionDenied", err)
	}

	// Revocations are per control stream at the relay: after the daemon
	// reconnects it must send them again.
	before := hub.snapshotSessions() // b's session only: a was revoked
	proxy.killAll()
	waitFor(t, "the daemon to be back and serving b on the new relay connection", func() bool { return hub.resumedSince(before) })
	waitFor(t, "the daemon to re-send the revocation", func() bool {
		a3 := f.newDevice(false, "a")
		a3.priv, a3.pub, a3.id = a.priv, a.pub, a.id
		err := a3.open("")
		return err != nil && relayStatus(err) == codePermissionDenied && b.canList()
	})
}

func (d *device) canList() bool {
	_, err := d.listWorkspaces()
	return err == nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(fixtureTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBridgeControlStreamSilentFor75sReconnects(t *testing.T) {
	// No device at all: only the control stream's own watchdog can notice.
	clock := newFakeClock()
	var mu sync.Mutex
	var jitterSeen []time.Duration
	f := newHubFixture(t, Options{Clock: clock, Jitter: func(d time.Duration) time.Duration {
		mu.Lock()
		jitterSeen = append(jitterSeen, d)
		mu.Unlock()
		return d
	}})
	proxy := f.viaProxy()
	f.start(nil)
	waitFor(t, "the hub to connect", func() bool { return f.logs.contains("connected to") })

	proxy.setBlackhole(true)
	clock.Advance(74 * time.Second)
	mu.Lock()
	n := len(jitterSeen)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("reconnect after 74 s of silence (%d backoffs); the rule is 75 s", n)
	}
	advanceUntil(t, clock, time.Second, 10, "reconnect after 75 s of silence", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(jitterSeen) > 0
	})
}
