package devices

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/securefile"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type dev struct {
	id    string
	pub   ed25519.PublicKey
	label string
}

func newDev(t *testing.T, label string) dev {
	t.Helper()
	pub, _ := testKey(t)
	return dev{DeviceID(pub), pub, label}
}

func openReg(t *testing.T) (*Registry, *fakeClock, string) {
	t.Helper()
	clk := newClock()
	path := filepath.Join(t.TempDir(), "relay-devices.json")
	r, err := Open(path, WithClock(clk.Now))
	if err != nil {
		t.Fatal(err)
	}
	return r, clk, path
}

func admit(t *testing.T, r *Registry, d dev) Decision {
	t.Helper()
	dec, err := r.Admit(d.id, d.pub, d.label)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	return dec
}

func readFileDoc(t *testing.T, path string) fileFormat {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRegistryFileMode0600AndAtomicWrite(t *testing.T) {
	r, _, path := openReg(t)
	d := newDev(t, "phone")
	admit(t, r, d)

	if ok, err := privateFile(path); err != nil || !ok {
		t.Fatalf("registry file private = %v, %v", ok, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}

	t.Run("failed temp write keeps previous file", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a read-only directory that is enforced")
		}
		before, _ := os.ReadFile(path)
		dir := filepath.Dir(path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o700)

		d2 := newDev(t, "tablet")
		if _, err := r.Admit(d2.id, d2.pub, d2.label); err == nil {
			t.Fatal("Admit succeeded with an unwritable directory")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("registry file changed after a failed write")
		}
		if _, ok := r.Get(d2.id); ok {
			t.Fatal("in-memory state advanced despite the write error")
		}
		if _, err := Open(path); err != nil {
			t.Fatalf("file no longer valid: %v", err)
		}
	})
}

func privateFile(path string) (bool, error) {
	// Thin wrapper so the test reads the same on every OS.
	return securefile.IsPrivate(path)
}

func TestRevokedDeviceRefusedWithReason(t *testing.T) {
	r, _, _ := openReg(t)
	d := newDev(t, "old phone")
	if err := r.OpenWindow(0, 0); err != nil {
		t.Fatal(err)
	}
	if dec := admit(t, r, d); !dec.Approved {
		t.Fatalf("setup: %+v", dec)
	}
	if _, err := r.Revoke(d.id); err != nil {
		t.Fatal(err)
	}
	// Even with a fresh window open, a revoked device stays out.
	if err := r.OpenWindow(0, 5); err != nil {
		t.Fatal(err)
	}
	dec := admit(t, r, d)
	if dec.Approved || dec.Reason != ReasonRevoked || dec.New {
		t.Fatalf("decision = %+v", dec)
	}
	if w, _ := r.Window(); w.Remaining != 5 {
		t.Fatalf("revoked device consumed a slot: %+v", w)
	}
}

func TestUnknownDeviceOutsideWindowIsPending(t *testing.T) {
	r, _, path := openReg(t)
	d := newDev(t, "new")
	dec := admit(t, r, d)
	if dec.Approved || dec.Reason != ReasonAwaitingApproval || !dec.New || dec.Device.State != StatePending {
		t.Fatalf("decision = %+v", dec)
	}
	again := admit(t, r, d)
	if again.New || again.Approved || again.Reason != ReasonAwaitingApproval {
		t.Fatalf("second admit = %+v", again)
	}
	if f := readFileDoc(t, path); len(f.Devices) != 1 || f.Devices[0].State != StatePending {
		t.Fatalf("file = %+v", f)
	}
}

func TestOfferOpensWindowAndApprovesExactlyOneDevice(t *testing.T) {
	r, _, path := openReg(t)
	if err := r.OpenWindow(time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	a, b := newDev(t, "a"), newDev(t, "b")
	da := admit(t, r, a)
	if !da.Approved || !da.New || da.Device.State != StateApproved {
		t.Fatalf("a = %+v", da)
	}
	if _, ok := r.Window(); ok {
		t.Fatal("window still open after its last slot")
	}
	if f := readFileDoc(t, path); f.Window != nil {
		t.Fatalf("file still lists window: %+v", f.Window)
	}
	db := admit(t, r, b)
	if db.Approved || db.Reason != ReasonAwaitingApproval {
		t.Fatalf("b = %+v", db)
	}
	// An already-approved device reconnecting is fine without a window.
	if !admit(t, r, a).Approved {
		t.Fatal("a refused on reconnect")
	}
}

func TestPairingWindowExpires(t *testing.T) {
	r, clk, path := openReg(t)
	if err := r.OpenWindow(5*time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5*time.Minute - time.Second)
	if _, ok := r.Window(); !ok {
		t.Fatal("window closed early")
	}
	clk.Advance(time.Second) // exactly at ExpiresAt counts as expired
	d := newDev(t, "late")
	dec := admit(t, r, d)
	if dec.Approved || dec.Reason != ReasonAwaitingApproval {
		t.Fatalf("decision = %+v", dec)
	}
	if _, ok := r.Window(); ok {
		t.Fatal("expired window reported open")
	}
	if f := readFileDoc(t, path); f.Window != nil {
		t.Fatalf("file still lists expired window: %+v", f.Window)
	}
}

func TestPairingWindowFlags(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		r, clk, _ := openReg(t)
		if err := r.OpenWindow(0, 0); err != nil {
			t.Fatal(err)
		}
		w, ok := r.Window()
		if !ok || w.Remaining != 1 || !w.ExpiresAt.Equal(clk.Now().Add(10*time.Minute)) {
			t.Fatalf("window = %+v, %v", w, ok)
		}
	})
	t.Run("custom admits exactly three", func(t *testing.T) {
		r, _, _ := openReg(t)
		if err := r.OpenWindow(time.Hour, 3); err != nil {
			t.Fatal(err)
		}
		var approved int
		for i := 0; i < 5; i++ {
			if admit(t, r, newDev(t, "d")).Approved {
				approved++
			}
		}
		if approved != 3 {
			t.Fatalf("approved %d, want 3", approved)
		}
	})
	t.Run("pending device takes a slot when a window opens", func(t *testing.T) {
		r, _, _ := openReg(t)
		d := newDev(t, "p")
		admit(t, r, d)
		if err := r.OpenWindow(0, 1); err != nil {
			t.Fatal(err)
		}
		dec := admit(t, r, d)
		if !dec.Approved || dec.New {
			t.Fatalf("decision = %+v", dec)
		}
		if _, ok := r.Window(); ok {
			t.Fatal("window should be spent")
		}
	})
	t.Run("close", func(t *testing.T) {
		r, _, path := openReg(t)
		r.OpenWindow(0, 1)
		if err := r.CloseWindow(); err != nil {
			t.Fatal(err)
		}
		if _, ok := r.Window(); ok || readFileDoc(t, path).Window != nil {
			t.Fatal("window not closed")
		}
	})
}

func TestRegistryApproveRenameRevokeResolve(t *testing.T) {
	r, clk, _ := openReg(t)
	a := newDev(t, "alpha")
	admit(t, r, a)
	clk.Advance(time.Second)
	b := newDev(t, "beta")
	admit(t, r, b)

	if got, err := r.Approve(a.id); err != nil || got.State != StateApproved {
		t.Fatalf("Approve = %+v, %v", got, err)
	}
	if got, err := r.Rename(a.id, "Alpha Phone"); err != nil || got.Label != "Alpha Phone" {
		t.Fatalf("Rename = %+v, %v", got, err)
	}
	if _, err := r.Rename(a.id, strings.Repeat("x", 65)); !errors.Is(err, ErrBadLabel) {
		t.Fatalf("Rename long: %v", err)
	}
	got, err := r.Revoke(a.id)
	if err != nil || got.State != StateRevoked || got.Label != "Alpha Phone" || len(got.Pub) != 32 {
		t.Fatalf("Revoke = %+v, %v", got, err)
	}
	if got, err := r.Approve(a.id); err != nil || got.State != StateApproved {
		t.Fatalf("re-Approve revoked = %+v, %v", got, err)
	}
	for _, f := range []func(string) (Device, error){r.Approve, r.Revoke} {
		if _, err := f("nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown id: %v", err)
		}
	}
	if _, err := r.Rename("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rename unknown: %v", err)
	}

	list := r.List()
	if len(list) != 2 || list[0].ID != a.id || list[1].ID != b.id {
		t.Fatalf("List order = %+v", list)
	}
	list[0].Pub[0] ^= 0xff // List returns copies
	if g, _ := r.Get(a.id); g.Pub[0] != a.pub[0] {
		t.Fatal("List leaked internal key slice")
	}

	// Resolve: exact, case-insensitive prefix, short, ambiguous, missing.
	if id, err := r.Resolve(strings.ToUpper(a.id[:6])); err != nil || id != a.id {
		t.Fatalf("Resolve prefix = %q, %v", id, err)
	}
	if id, err := r.Resolve(b.id); err != nil || id != b.id {
		t.Fatalf("Resolve exact = %q, %v", id, err)
	}
	if _, err := r.Resolve(a.id[:3]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve 3 chars: %v", err)
	}
	if _, err := r.Resolve("zzzzzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve missing: %v", err)
	}
	// Force an ambiguity with a crafted registry that shares a prefix.
	r.mu.Lock()
	r.cur.devices["abcd1"] = Device{ID: "abcd1"}
	r.cur.devices["abcd2"] = Device{ID: "abcd2"}
	r.mu.Unlock()
	if _, err := r.Resolve("ABCD"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Resolve ambiguous: %v", err)
	}
	if id, err := r.Resolve("abcd1"); err != nil || id != "abcd1" {
		t.Fatalf("Resolve exact short-prefix id = %q, %v", id, err)
	}
}

func TestRegistryReloadSeesExternalEdit(t *testing.T) {
	r, clk, path := openReg(t)
	d := newDev(t, "phone")
	admit(t, r, d)

	// A CLI editing the file while no daemon runs: a second handle revokes it.
	cli, err := Open(path, WithClock(clk.Now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Revoke(d.id); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.Get(d.id); g.State != StatePending {
		t.Fatalf("state before reload = %v", g.State)
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.Get(d.id); g.State != StateRevoked {
		t.Fatalf("state after reload = %v", g.State)
	}
}

func TestRegistryCorruptFileIsAnError(t *testing.T) {
	good := newDev(t, "x")
	goodDoc := func(mut func(*fileFormat)) string {
		f := fileFormat{V: 1, Devices: []Device{{ID: good.id, Pub: good.pub, State: StateApproved}}}
		mut(&f)
		b, _ := json.Marshal(f)
		return string(b)
	}
	tests := map[string]string{
		"garbage":         "{not json",
		"empty":           "",
		"unknown version": `{"v":2,"devices":[]}`,
		"missing version": `{"devices":[]}`,
		"bad state":       goodDoc(func(f *fileFormat) { f.Devices[0].State = "weird" }),
		"id/key mismatch": goodDoc(func(f *fileFormat) { f.Devices[0].ID = strings.Repeat("a", 26) }),
		"duplicate":       goodDoc(func(f *fileFormat) { f.Devices = append(f.Devices, f.Devices[0]) }),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "relay-devices.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("Open succeeded on a bad file")
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Fatal("Open modified the corrupt file")
			}
		})
	}
	// And a valid one opens.
	path := filepath.Join(t.TempDir(), "ok.json")
	os.WriteFile(path, []byte(goodDoc(func(*fileFormat) {})), 0o600)
	r, err := Open(path)
	if err != nil || len(r.List()) != 1 {
		t.Fatalf("valid file: %v", err)
	}
}

func TestRegistryConcurrentAdmit(t *testing.T) {
	r, _, path := openReg(t)
	if err := r.OpenWindow(time.Hour, 7); err != nil {
		t.Fatal(err)
	}
	const n = 40
	devs := make([]dev, n)
	for i := range devs {
		devs[i] = newDev(t, "d")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	approved := 0
	for i := 0; i < n; i++ {
		for rep := 0; rep < 2; rep++ {
			wg.Add(1)
			go func(d dev) {
				defer wg.Done()
				dec, err := r.Admit(d.id, d.pub, d.label)
				if err != nil {
					t.Error(err)
					return
				}
				_ = r.List()
				if dec.Approved && dec.New {
					mu.Lock()
					approved++
					mu.Unlock()
				}
			}(devs[i])
		}
	}
	wg.Wait()
	if approved != 7 {
		t.Fatalf("%d devices approved on first sight, want exactly 7", approved)
	}
	if got := len(r.List()); got != n {
		t.Fatalf("%d devices, want %d", got, n)
	}
	re, err := Open(path)
	if err != nil || len(re.List()) != n {
		t.Fatalf("reopen: %v", err)
	}
	var onDisk int
	for _, d := range re.List() {
		if d.State == StateApproved {
			onDisk++
		}
	}
	if onDisk != 7 {
		t.Fatalf("%d approved on disk, want 7", onDisk)
	}
}

func TestRegistryDoesNotOverwriteRenamedLabel(t *testing.T) {
	r, clk, _ := openReg(t)
	d := newDev(t, "Pixel")
	admit(t, r, d)
	r.Approve(d.id)
	if _, err := r.Rename(d.id, "Kitchen tablet"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Minute)
	d.label = "Pixel 9"
	if dec := admit(t, r, d); dec.Device.Label != "Kitchen tablet" {
		t.Fatalf("label = %q after hello, rename must stick", dec.Device.Label)
	}

	// An empty stored label does get filled in.
	e := newDev(t, "")
	admit(t, r, e)
	e.label = "Late label"
	if dec := admit(t, r, e); dec.Device.Label != "Late label" {
		t.Fatalf("empty label not filled: %q", dec.Device.Label)
	}
}

func TestRegistryLastSeenPersistThrottled(t *testing.T) {
	r, clk, path := openReg(t)
	d := newDev(t, "p")
	r.OpenWindow(0, 1)
	admit(t, r, d)
	first := readFileDoc(t, path).Devices[0].LastSeen

	clk.Advance(30 * time.Second)
	admit(t, r, d)
	if got := readFileDoc(t, path).Devices[0].LastSeen; !got.Equal(first) {
		t.Fatal("LastSeen persisted within 60s")
	}
	if g, _ := r.Get(d.id); !g.LastSeen.Equal(first.Add(30 * time.Second)) {
		t.Fatalf("in-memory LastSeen = %v", g.LastSeen)
	}
	clk.Advance(31 * time.Second)
	admit(t, r, d)
	if got := readFileDoc(t, path).Devices[0].LastSeen; !got.Equal(first.Add(61 * time.Second)) {
		t.Fatalf("LastSeen not persisted after 60s: %v", got)
	}
}

func TestAdmitRejectsMismatchedKey(t *testing.T) {
	r, _, _ := openReg(t)
	a, b := newDev(t, "a"), newDev(t, "b")
	if _, err := r.Admit(a.id, b.pub, "x"); !errors.Is(err, ErrIDMismatch) {
		t.Fatalf("err = %v", err)
	}
}
