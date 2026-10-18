package bridge

import (
	"crypto/subtle"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/devices"
)

const daemonPassword = "correct horse battery staple"

// testGate is a PasswordGate with a plain constant-time comparison (the real
// gate, server.RelayPasswordGate, is bcrypt and is tested in internal/server).
type testGate struct {
	required bool
	want     string
}

func (g testGate) Required() bool { return g.required }
func (g testGate) Check(pw string) bool {
	return subtle.ConstantTimeCompare([]byte(pw), []byte(g.want)) == 1
}

func passwordFixture(t *testing.T, opts Options) *hubFixture {
	t.Helper()
	opts.Password = testGate{required: true, want: daemonPassword}
	return newHubFixture(t, opts)
}

// helloOutcome opens a fresh session for d and sends a hello carrying pw.
func helloOutcome(t *testing.T, d *device, pw string) rpcResp {
	t.Helper()
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	r, err := d.helloWithPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func wantCode(t *testing.T, r rpcResp, code string) {
	t.Helper()
	if r.Err == nil || r.Err.Code != code {
		t.Fatalf("hello answer = %+v (err %+v), want error code %q", r, r.Err, code)
	}
}

func TestDeviceHelloPasswordRequiredWhenSet(t *testing.T) {
	f := passwordFixture(t, Options{})
	approvedWindow(t, f, 3)
	f.start(nil)

	d := f.newDevice(false, "laptop")
	wantCode(t, helloOutcome(t, d, ""), codePasswordRequired)
	wantCode(t, helloOutcome(t, d, "not the password"), codeIncorrectPassword)
	r := helloOutcome(t, d, daemonPassword)
	if r.Err != nil {
		t.Fatalf("hello with the right password refused: %+v", r.Err)
	}
	d.mustList() // and the session is served

	// Approved devices need it too, on every hello (a reconnect without it is refused).
	d2 := f.newDevice(false, "laptop")
	d2.priv, d2.pub, d2.id = d.priv, d.pub, d.id
	wantCode(t, helloOutcome(t, d2, ""), codePasswordRequired)
	d3 := f.newDevice(false, "laptop")
	d3.priv, d3.pub, d3.id = d.priv, d.pub, d.id
	d3.connectWithPassword(daemonPassword)
	d3.mustList()
}

func TestWrongPasswordRejectedAndNotRecordedPending(t *testing.T) {
	f := passwordFixture(t, Options{})
	f.start(nil) // no pairing window: an unknown device would be recorded pending
	d := f.newDevice(false, "intruder")
	wantCode(t, helloOutcome(t, d, ""), codePasswordRequired)
	wantCode(t, helloOutcome(t, d, "guess"), codeIncorrectPassword)
	if got, ok := f.reg.Get(d.id); ok {
		t.Fatalf("a device that failed the password was recorded: %+v", got)
	}
	if len(f.reg.List()) != 0 {
		t.Fatalf("registry = %+v, want empty", f.reg.List())
	}
	// With the password it is an ordinary unknown device: pending, awaiting approval.
	wantCode(t, helloOutcome(t, d, daemonPassword), string(devices.ReasonAwaitingApproval))
	if got, ok := f.reg.Get(d.id); !ok || got.State != devices.StatePending {
		t.Fatalf("registry = %+v, %v; want a pending record once the password is right", got, ok)
	}
}

func TestPasswordRefusalRevealsNothingAboutTheRegistry(t *testing.T) {
	f := passwordFixture(t, Options{})
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "x")
	d.connectWithPassword(daemonPassword)
	if _, err := f.reg.Revoke(d.id); err != nil {
		t.Fatal(err)
	}
	// A revoked device without the password is told to supply it, not "revoked":
	// nothing about its state leaks to someone who cannot prove the password.
	d2 := f.newDevice(false, "x")
	d2.priv, d2.pub, d2.id = d.priv, d.pub, d.id
	wantCode(t, helloOutcome(t, d2, ""), codePasswordRequired)
	wantCode(t, helloOutcome(t, d2, "wrong"), codeIncorrectPassword)
	wantCode(t, helloOutcome(t, d2, daemonPassword), codeRevoked)
}

func TestWrongPasswordsAreRateLimitedPerDevice(t *testing.T) {
	clock := newFakeClock()
	// 41 rotating device ids: lift the relay's own per-host attachment cap (16) out of the way.
	f := newHubFixture(t, Options{Clock: clock, Password: testGate{required: true, want: daemonPassword}}, relaytestMaxDevices(100))
	approvedWindow(t, f, 2)
	f.start(nil)
	d := f.newDevice(false, "guesser")
	for i := 0; i < 5; i++ { // five free wrong attempts
		wantCode(t, helloOutcome(t, d, "wrong"), codeIncorrectPassword)
	}
	wantCode(t, helloOutcome(t, d, "wrong"), codeIncorrectPassword) // the sixth is counted and locks
	r := helloOutcome(t, d, daemonPassword)                         // even the right password waits
	wantCode(t, r, codeTooManyAttempts)
	if r.Err.RetryAfter < 1 {
		t.Fatalf("too_many_attempts without retry_after: %+v", r.Err)
	}
	// Another device id is unaffected.
	other := f.newDevice(false, "other")
	other.connectWithPassword(daemonPassword)
	// After the wait the right password is accepted and clears the history.
	clock.Advance(time.Duration(r.Err.RetryAfter+1) * time.Second)
	if r := helloOutcome(t, d, daemonPassword); r.Err != nil {
		t.Fatalf("after the wait: %+v", r.Err)
	}
}

func TestOverlongPasswordIsIncorrect(t *testing.T) {
	f := passwordFixture(t, Options{})
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "x")
	long := make([]byte, 129)
	for i := range long {
		long[i] = 'a'
	}
	wantCode(t, helloOutcome(t, d, string(long)), codeIncorrectPassword)
}

func TestNoPasswordConfiguredWarnsOnce(t *testing.T) {
	f := newHubFixture(t, Options{Password: testGate{required: false}})
	approvedWindow(t, f, 3)
	f.start(nil)
	for _, label := range []string{"a", "b", "c"} {
		f.newDevice(false, label).connect()
	}
	if n := f.logs.count("no daemon password is set"); n != 1 {
		t.Fatalf("the no-password warning was logged %d times, want exactly once per start", n)
	}
	// A password-protected daemon does not warn.
	g := passwordFixture(t, Options{})
	g.start(nil)
	if g.logs.contains("no daemon password is set") {
		t.Fatal("a daemon with a password must not warn that it has none")
	}
}

// Without a daemon password a client may still send one (it cannot know): it is
// ignored, and never appears in the log.
func TestNoPasswordSetIgnoresAHelloPasswordAndNeverLogsIt(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "x")
	d.connectWithPassword("hunter2-sent-but-unneeded")
	d.mustList()
	if f.logs.contains("hunter2") {
		t.Fatal("a hello password reached the log")
	}
}

func TestPasswordNeverLoggedOnRefusal(t *testing.T) {
	f := passwordFixture(t, Options{})
	f.start(nil)
	d := f.newDevice(false, "x")
	wantCode(t, helloOutcome(t, d, "guess-guess-guess-123"), codeIncorrectPassword)
	if f.logs.contains("guess-guess") || f.logs.contains(daemonPassword) {
		f.logs.dump(t)
		t.Fatal("a password reached the log")
	}
}

func TestPasswordProtectedDaemonRefusesLegacyClients(t *testing.T) {
	f := passwordFixture(t, Options{LegacyDevices: true})
	f.start(nil)
	d := f.newDevice(false, "old-app")
	d.id = DefaultDeviceID
	if err := d.open(DefaultSessionID); err != nil {
		t.Fatal(err)
	}
	r, err := d.request("workspace.list", nil)
	if err != nil || r.Err == nil || r.Err.Code != codeHelloRequired {
		t.Fatalf("legacy request on a password daemon = %+v, %v; want %q", r, err, codeHelloRequired)
	}
}

// A device that fails the password must not be able to disturb a live, approved
// session of the same device id (the attacker needs only the key, not the password).
func TestWrongPasswordHelloDoesNotSupersedeALiveSession(t *testing.T) {
	f := passwordFixture(t, Options{})
	approvedWindow(t, f, 1)
	f.start(nil)
	d := f.newDevice(false, "laptop")
	d.connectWithPassword(daemonPassword)
	d.mustList()

	// The same device key, a new session, no/wrong password: refused, and the first session keeps serving.
	again := f.newDevice(false, "laptop")
	again.priv, again.pub, again.id = d.priv, d.pub, d.id
	wantCode(t, helloOutcome(t, again, ""), codePasswordRequired)
	wantCode(t, helloOutcome(t, again, "wrong"), codeIncorrectPassword)
	d.mustList()
}

func TestSilentClientOnAPasswordDaemonIsRefusedEvenWithLegacyOn(t *testing.T) {
	f := passwordFixture(t, Options{LegacyDevices: true, HelloTimeout: 300 * time.Millisecond})
	f.start(nil)
	d := f.newDevice(false, "quiet-app")
	d.id = DefaultDeviceID
	if err := d.open(""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if f.logs.contains("as a legacy device") {
		t.Fatal("a silent client was served as legacy on a password-protected daemon")
	}
}

func TestHelloOnTheFixedRouteIsRefusedOnAPasswordDaemon(t *testing.T) {
	f := passwordFixture(t, Options{})
	f.start(nil)
	d := f.newDevice(false, "x")
	d.id = DefaultDeviceID // the fixed single-device route: no challenge, no identity hello
	if err := d.open(DefaultSessionID); err != nil {
		t.Fatal(err)
	}
	r, err := d.request(devices.HelloMethod, map[string]any{"v": 2, "password": daemonPassword})
	if err != nil || r.Err == nil {
		t.Fatalf("a hello on the fixed route = %+v, %v; want a refusal", r, err)
	}
}

// More than 40 wrong attempts a minute across ALL devices (rotating device keys is free for an
// attacker) lock every password check for a minute, correct password included; it then lifts.
// Exercised on the hub's check directly: 41 rotating routes would hit the relay's own per-host cap.
func TestGlobalWrongPasswordWindowLocksEveryDeviceThenLifts(t *testing.T) {
	clock := newFakeClock()
	h := NewHub(Options{Clock: clock, Password: testGate{required: true, want: daemonPassword}, Logf: func(string, ...any) {}})
	for i := 0; i < 41; i++ {
		ok, code, _, _ := h.checkPassword("rotating-"+string(rune('A'+i%26))+string(rune('a'+i/26)), "guess")
		if ok || code != codeIncorrectPassword {
			t.Fatalf("attempt %d: ok=%v code=%q", i, ok, code)
		}
	}
	ok, code, _, retry := h.checkPassword("a-legitimate-device", daemonPassword)
	if ok || code != codeTooManyAttempts || retry < 1 {
		t.Fatalf("during the global lock: ok=%v code=%q retry=%d; want too_many_attempts with a retry_after", ok, code, retry)
	}
	clock.Advance(time.Duration(retry+1) * time.Second)
	if ok, code, _, _ := h.checkPassword("a-legitimate-device", daemonPassword); !ok {
		t.Fatalf("after the lock lifted: %q", code)
	}
}
