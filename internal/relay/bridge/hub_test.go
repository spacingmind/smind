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

	if _, err := f.hub.RevokeDevice(d.id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.rerr:
	case <-time.After(fixtureTimeout):
		t.Fatal("revoking did not close the device's stream")
	}

	// Even if the relay did not refuse it, the daemon does, with a reason.
	d2 := f.newDevice(false, "x")
	d2.priv, d2.pub, d2.id = d.priv, d.pub, d.id
	err := d2.open("")
	if err != nil {
		return // the relay refused the attach (Revoke reached it): also correct
	}
	r, err := d2.hello()
	if err == nil && (r.Err == nil || r.Err.Code != codeRevoked) {
		t.Fatalf("revoked device's hello = %+v, want code %q", r, codeRevoked)
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
	if err := d.open(DefaultSessionID); err != nil { // sends no hello
		t.Fatal(err)
	}
	d.mustList() // first message is an ordinary request
	if len(f.reg.List()) != 0 {
		t.Fatal("a legacy device must stay unlisted")
	}
}

func TestLegacyDeprecationLogged(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	d.mustList()
	if !f.logs.contains("deprecated") {
		t.Fatal("serving a legacy client must log that it is deprecated")
	}
}

func TestLegacyClientRefusedWhenLegacyDevicesFalse(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: false})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := d.request("workspace.list", nil)
	if err != nil || r.Err == nil || r.Err.Code != codeHelloRequired {
		t.Fatalf("legacy request answer = %+v, %v; want %q", r, err, codeHelloRequired)
	}
}

func TestLegacyClientSilentForHelloTimeoutIsStillServed(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true, HelloTimeout: 300 * time.Millisecond})
	f.start(nil)
	d := f.newDevice(false, "quiet-app")
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond) // longer than the hello timeout, no message sent
	d.mustList()
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
