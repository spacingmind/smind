package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/wsclient"
)

type recordedCall struct {
	method string
	params any
}

// fakeDaemon is a wsCaller answering relay.device.* from canned results, so
// no daemon (or home directory) is involved.
type fakeDaemon struct {
	calls   []recordedCall
	results map[string]any // method -> result, or error
}

func (f *fakeDaemon) call(_ context.Context, method string, params any, out any) error {
	f.calls = append(f.calls, recordedCall{method, params})
	res, ok := f.results[method]
	if !ok {
		return errors.New("method not found: " + method)
	}
	if err, isErr := res.(error); isErr {
		return err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// useDaemon points the CLI at d for the test, with home == config.Dir().
func useDaemon(t *testing.T, d *fakeDaemon) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	old := relayDeviceDial
	relayDeviceDial = func(context.Context) (wsCaller, func(), error) { return d.call, func() {}, nil }
	t.Cleanup(func() { relayDeviceDial = old })
	return home
}

// noDaemon makes every dial fail.
func noDaemon(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	old := relayDeviceDial
	relayDeviceDial = func(context.Context) (wsCaller, func(), error) {
		return nil, nil, errors.New("connect: refused")
	}
	t.Cleanup(func() { relayDeviceDial = old })
	return home
}

func runDevices(t *testing.T, home string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runRelayDevices(context.Background(), args, home, &out, &errOut)
	return code, out.String(), errOut.String()
}

func info(id, label string, st devices.State, connected bool) devices.Info {
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return devices.Info{Device: devices.Device{ID: id, Label: label, State: st, FirstSeen: t0, LastSeen: t0.Add(time.Hour)}, Connected: connected}
}

func TestDevicesLsAndPendingFlag(t *testing.T) {
	d := &fakeDaemon{results: map[string]any{"relay.device.list": map[string]any{
		"devices": []devices.Info{
			info("aaaa1111aaaa1111aaaa1111aa", "phone", devices.StatePending, false),
			info("bbbb2222bbbb2222bbbb2222bb", "tablet", devices.StateApproved, true),
			info("cccc3333cccc3333cccc3333cc", "", devices.StateRevoked, false),
		},
		"window": nil,
	}}}
	home := useDaemon(t, d)

	code, out, errOut := runDevices(t, home, "ls")
	if code != 0 || errOut != "" {
		t.Fatalf("ls = %d, stderr %q", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if got := strings.Fields(lines[0]); !reflect.DeepEqual(got, []string{"ID", "LABEL", "STATE", "CONNECTED", "FIRST", "SEEN", "LAST", "SEEN"}) {
		t.Fatalf("header = %q", lines[0])
	}
	wantFirst := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC).Local().Format("2006-01-02T15:04Z07:00")
	row := func(id string) []string {
		for _, l := range lines {
			if strings.HasPrefix(l, id) {
				return strings.Fields(l)
			}
		}
		t.Fatalf("no row for %s in:\n%s", id, out)
		return nil
	}
	if got := row("bbbb2222bbbb2222bbbb2222bb"); got[1] != "tablet" || got[2] != "approved" || got[3] != "yes" || got[4] != wantFirst {
		t.Fatalf("tablet row = %v", got)
	}
	if got := row("aaaa1111aaaa1111aaaa1111aa"); got[2] != "pending" || got[3] != "no" {
		t.Fatalf("phone row = %v", got)
	}
	if got := row("cccc3333cccc3333cccc3333cc"); got[1] != "-" || got[2] != "revoked" {
		t.Fatalf("empty-label row = %v", got)
	}
	if !strings.Contains(out, "1 device(s) awaiting approval: smind relay devices approve <id>") {
		t.Fatalf("no pending footer:\n%s", out)
	}

	code, out, _ = runDevices(t, home, "ls", "--pending")
	if code != 0 || !strings.Contains(out, "aaaa1111") || strings.Contains(out, "bbbb2222") || strings.Contains(out, "cccc3333") {
		t.Fatalf("ls --pending = %d:\n%s", code, out)
	}

	// Nothing pending: no footer, a plain note for --pending.
	d.results["relay.device.list"] = map[string]any{"devices": []devices.Info{info("bbbb2222bbbb2222bbbb2222bb", "tablet", devices.StateApproved, false)}, "window": nil}
	_, out, _ = runDevices(t, home, "ls")
	if strings.Contains(out, "awaiting approval") {
		t.Fatalf("footer without pending devices:\n%s", out)
	}
	_, out, _ = runDevices(t, home, "ls", "--pending")
	if !strings.Contains(out, "no devices awaiting approval") {
		t.Fatalf("empty --pending:\n%s", out)
	}

	// Usage errors exit 2.
	for _, args := range [][]string{{}, {"ls", "extra"}, {"ls", "--nope"}, {"bogus"}, {"approve"}, {"revoke", "a", "b"}, {"rename", "aaaa"}} {
		if code, _, _ := runDevices(t, home, args...); code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
	}
}

func TestDevicesApproveRevokeRename(t *testing.T) {
	dev := devices.Device{ID: "aaaa1111aaaa1111aaaa1111aa", Label: "tablet", State: devices.StateApproved}
	d := &fakeDaemon{results: map[string]any{
		"relay.device.approve": dev,
		"relay.device.revoke":  dev,
		"relay.device.rename":  dev,
	}}
	home := useDaemon(t, d)

	code, out, errOut := runDevices(t, home, "approve", "AAAA")
	if code != 0 || errOut != "" || !strings.Contains(out, "device aaaa1111aaaa1111aaaa1111aa approved") {
		t.Fatalf("approve = %d %q %q", code, out, errOut)
	}
	code, out, _ = runDevices(t, home, "revoke", "aaaa1")
	if code != 0 || !strings.Contains(out, "revoked") {
		t.Fatalf("revoke = %d %q", code, out)
	}
	code, out, _ = runDevices(t, home, "rename", "aaaa1", "kitchen ipad")
	if code != 0 || !strings.Contains(out, "renamed") {
		t.Fatalf("rename = %d %q", code, out)
	}
	want := []recordedCall{
		{"relay.device.approve", map[string]any{"id": "AAAA"}},
		{"relay.device.revoke", map[string]any{"id": "aaaa1"}},
		{"relay.device.rename", map[string]any{"id": "aaaa1", "label": "kitchen ipad"}},
	}
	if !reflect.DeepEqual(d.calls, want) {
		t.Fatalf("calls = %v, want %v", d.calls, want)
	}

	// A daemon refusal is a runtime error (exit 1) with its message.
	d.results["relay.device.revoke"] = &wsclient.RPCError{Message: "relay.device.revoke: no such device"}
	code, out, errOut = runDevices(t, home, "revoke", "zzzz")
	if code != 1 || out != "" || !strings.Contains(errOut, "no such device") {
		t.Fatalf("revoke unknown = %d %q %q", code, out, errOut)
	}
}

func seedDevice(t *testing.T, reg *devices.Registry, label string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := devices.DeviceID(pub)
	if _, err := reg.Admit(id, pub, label); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDevicesCLIEditsFileWhenNoDaemon(t *testing.T) {
	home := noDaemon(t)
	path := devices.RegistryPath(home)
	reg, err := devices.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id1 := seedDevice(t, reg, "phone")
	id2 := seedDevice(t, reg, "tablet")

	code, out, errOut := runDevices(t, home, "approve", strings.ToUpper(id1[:8]))
	if code != 0 || !strings.Contains(out, id1) {
		t.Fatalf("approve = %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "daemon is not running") || !strings.Contains(errOut, "applies when the daemon next starts") {
		t.Fatalf("stderr lacks the no-daemon note: %q", errOut)
	}
	if code, _, _ = runDevices(t, home, "rename", id2[:8], "den"); code != 0 {
		t.Fatalf("rename exit = %d", code)
	}
	if code, _, _ = runDevices(t, home, "revoke", id2[:8]); code != 0 {
		t.Fatalf("revoke exit = %d", code)
	}

	// The file (not just memory) carries the changes.
	check, err := devices.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := check.Get(id1); d.State != devices.StateApproved {
		t.Fatalf("device 1 = %+v, want approved", d)
	}
	if d, _ := check.Get(id2); d.State != devices.StateRevoked || d.Label != "den" {
		t.Fatalf("device 2 = %+v, want revoked and renamed", d)
	}

	// ls works on the file; connected is unknown without the daemon.
	code, out, _ = runDevices(t, home, "ls")
	if code != 0 || !strings.Contains(out, id1) || !strings.Contains(out, "revoked") {
		t.Fatalf("ls = %d:\n%s", code, out)
	}
	for _, l := range strings.Split(out, "\n")[1:] {
		if strings.HasPrefix(l, id1) && strings.Fields(l)[3] != "-" {
			t.Fatalf("connected column without a daemon: %q", l)
		}
	}

	// Unknown / ambiguous / too-short ids are runtime errors.
	for _, id := range []string{"zzzzzzzz", "abc"} {
		code, _, errOut = runDevices(t, home, "approve", id)
		if code != 1 || !strings.Contains(errOut, "no such device") {
			t.Fatalf("approve %q = %d %q", id, code, errOut)
		}
	}

	// The pairing window also opens in the file.
	var e bytes.Buffer
	openOfferWindow(home, 5*time.Minute, 2, &e)
	check, _ = devices.Open(path)
	if w, ok := check.Window(); !ok || w.Remaining != 2 {
		t.Fatalf("window = %+v, %v; want 2 slots open", w, ok)
	}
}

func TestRelayOfferWindowFlags(t *testing.T) {
	d := &fakeDaemon{results: map[string]any{"relay.device.window": map[string]any{"expires_at": time.Now().Add(time.Hour), "remaining": 3}}}
	useDaemon(t, d)
	// The offer's home must be the daemon's own (config.Dir()) for the daemon to be dialed.
	dir := offerDir(t, 12)
	t.Setenv("SMIND_HOME", dir)

	// Defaults: 10 minutes, one device.
	code, stdout, stderr := offer(t, dir, false)
	if code != 0 {
		t.Fatalf("offer = %d, stderr %q", code, stderr)
	}
	urlOnly(t, stdout)
	if len(d.calls) != 1 || d.calls[0].method != "relay.device.window" ||
		!reflect.DeepEqual(d.calls[0].params, map[string]any{"duration_seconds": 600, "max_devices": 1}) {
		t.Fatalf("default window call = %v", d.calls)
	}
	if want := "Pairing window open for 10m0s or until 1 new device connects; other new devices wait for `smind relay devices approve`.\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}

	// Explicit flags.
	d.calls = nil
	code, stdout, stderr = offer(t, dir, false, "--window", "90s", "--max-devices", "3")
	if code != 0 {
		t.Fatalf("offer = %d, stderr %q", code, stderr)
	}
	urlOnly(t, stdout)
	if !reflect.DeepEqual(d.calls[0].params, map[string]any{"duration_seconds": 90, "max_devices": 3}) {
		t.Fatalf("window call = %v", d.calls)
	}
	if !strings.Contains(stderr, "open for 1m30s or until 3 new devices connect") {
		t.Fatalf("stderr = %q", stderr)
	}

	// A daemon that refuses the window: warning on stderr, the offer is still
	// printed and the exit code is 0.
	d.results["relay.device.window"] = &wsclient.RPCError{Message: "relay.device.window: registry unavailable"}
	code, stdout, stderr = offer(t, dir, false)
	if code != 0 {
		t.Fatalf("refused window: exit = %d, want 0", code)
	}
	urlOnly(t, stdout)
	if !strings.Contains(stderr, "warning: could not open the pairing window (relay.device.window: registry unavailable)") {
		t.Fatalf("stderr = %q", stderr)
	}

	// Bad flag values are usage errors and print no offer.
	for _, args := range [][]string{{"--window", "0s"}, {"--window", "500ms"}, {"--max-devices", "0"}, {"--window", "soon"}} {
		d.calls = nil
		code, stdout, _ = offer(t, dir, false, args...)
		if code != 2 || stdout != "" || len(d.calls) != 0 {
			t.Errorf("%v = %d, stdout %q, calls %v; want usage error", args, code, stdout, d.calls)
		}
	}
}
