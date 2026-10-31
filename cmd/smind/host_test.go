package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// hostHome makes a temp SMIND_HOME whose config.yaml has the fleet section
// explicitly on or off, so runHost's config check reads a known state.
func hostHome(t *testing.T, fleetEnabled bool) string {
	t.Helper()
	home := t.TempDir()
	value := "false"
	if fleetEnabled {
		value = "true"
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("fleet:\n  enabled: "+value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMIND_HOME", home)
	return home
}

// useHostDaemon points the host commands at d for the test.
func useHostDaemon(t *testing.T, d *fakeDaemon) {
	t.Helper()
	old := hostDial
	hostDial = func(context.Context) (wsCaller, func(), error) { return d.call, func() {}, nil }
	t.Cleanup(func() { hostDial = old })
}

func runHostCmd(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = runHost(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHostCommandsSayFleetOffWhenDisabled(t *testing.T) {
	hostHome(t, false)
	d := &fakeDaemon{}
	useHostDaemon(t, d)

	for _, args := range [][]string{
		{"ls"},
		{"ls", "--json"},
		{"ls", "--help"},
		{"add", "smind-host://invite"},
		{"invite"},
		{"approve", "abcdef12"},
		{"rm", "abcdef12"},
		{"reject", "abcdef12"},
		{"enable-reverse", "abcdef12"},
		{"disable-reverse", "abcdef12"},
		{"rename", "abcdef12", "den"},
		{"test", "abcdef12"},
		{"grant"},
		{"bogus"},
	} {
		code, out, errOut := runHostCmd(args...)
		if code != 1 || !strings.Contains(errOut, "fleet is off") {
			t.Errorf("%v = %d, stdout %q, stderr %q; want exit 1 and the fleet-off message", args, code, out, errOut)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("the daemon was reached with the fleet off: %v", d.calls)
	}

	// Bare `smind host` and -h print usage and exit 0 without the check.
	for _, args := range [][]string{{}, {"-h"}, {"--help"}} {
		code, out, errOut := runHostCmd(args...)
		if code != 0 || !strings.Contains(out, "usage: smind host") || errOut != "" {
			t.Errorf("%v = %d, stdout %q, stderr %q; want usage on stdout and exit 0", args, code, out, errOut)
		}
	}
}

func TestHostLsNeverPrintsSecrets(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.list": map[string]any{
		"hosts": []map[string]any{{
			"hostId":      "abcdef1234567890abcdef",
			"alias":       "laptop",
			"label":       "Laptop",
			"state":       "approved",
			"outbound":    map[string]any{"enabled": true},
			"inbound":     map[string]any{"enabled": false},
			"link":        map[string]any{"state": "connected", "detail": "direct wss"},
			"createdAt":   "2026-10-31T00:00:00Z",
			"password":    "CANARY-PW",
			"relaySecret": "CANARY-RS",
			"pub":         "CANARY-PUB",
		}},
		"window": nil,
	}}}
	useHostDaemon(t, d)

	for _, jsonOut := range []bool{false, true} {
		args := []string{"ls"}
		if jsonOut {
			args = append(args, "--json")
		}
		code, out, errOut := runHostCmd(args...)
		if code != 0 || errOut != "" {
			t.Fatalf("ls --json=%v = %d, stderr %q", jsonOut, code, errOut)
		}
		for _, canary := range []string{"CANARY-PW", "CANARY-RS", "CANARY-PUB"} {
			if strings.Contains(out, canary) {
				t.Errorf("ls --json=%v printed %s:\n%s", jsonOut, canary, out)
			}
		}
		if jsonOut {
			var got struct {
				Hosts []map[string]any `json:"hosts"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("--json output is not JSON: %v\n%s", err, out)
			}
			if len(got.Hosts) != 1 {
				t.Fatalf("--json hosts = %v", got.Hosts)
			}
			h := got.Hosts[0]
			for _, k := range []string{"password", "relaySecret", "pub"} {
				if _, present := h[k]; present {
					t.Errorf("--json kept the extra field %q", k)
				}
			}
			for k, want := range map[string]any{"hostId": "abcdef1234567890abcdef", "alias": "laptop", "state": "approved"} {
				if h[k] != want {
					t.Errorf("--json %s = %v, want %v", k, h[k], want)
				}
			}
		} else {
			if !strings.Contains(out, "laptop") || !strings.Contains(out, "abcdef12") ||
				!strings.Contains(out, "approved") || !strings.Contains(out, "connected") ||
				!strings.Contains(out, "on") || !strings.Contains(out, "off") {
				t.Fatalf("table row missing fields:\n%s", out)
			}
		}
	}
}

func TestHostAddReadsPasswordFromEnvOnly(t *testing.T) {
	hostHome(t, true)
	t.Setenv("HOST_PW", "s3cret-pw")
	d := &fakeDaemon{results: map[string]any{"fleet.host.add": map[string]any{
		"hostId": "abcdef1234567890abcdef", "alias": "laptop", "state": "pending",
		"sas": "1234 5678", "warning": "cleartext route",
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("add", "smind-host://invite", "--alias", "laptop", "--password-env", "HOST_PW")
	if code != 0 {
		t.Fatalf("add = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "Added laptop (pending). Compare this code with the other host: 1234 5678") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "warning: cleartext route") {
		t.Fatalf("warning missing from stderr: %q", errOut)
	}
	want := map[string]any{"invite": "smind-host://invite", "alias": "laptop", "password": "s3cret-pw", "allowCleartext": false}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Fatalf("params = %v, want %v", d.calls[0].params, want)
	}

	// An unset variable sends an empty password, never a guess.
	d.calls = nil
	if code, _, _ := runHostCmd("add", "smind-host://invite", "--password-env", "HOST_PW_UNSET"); code != 0 {
		t.Fatal("add with an unset password env failed")
	}
	if got := d.calls[0].params.(map[string]any)["password"]; got != "" {
		t.Fatalf("password from an unset env var = %v, want empty", got)
	}

	// --password does not exist: the value must never ride on a flag.
	d.calls = nil
	code, _, errOut = runHostCmd("add", "smind-host://invite", "--password", "x")
	if code != 2 || len(d.calls) != 0 {
		t.Fatalf("--password = %d, stderr %q, calls %v; want a usage error and no call", code, errOut, d.calls)
	}
}

func TestHostApproveNeedsWorkspace(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.approve": map[string]any{
		"hostId": "abcdef1234567890abcdef", "alias": "laptop", "state": "approved",
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("approve", "abcdef12")
	if code != 2 || !strings.Contains(errOut, "approve needs at least one --write <workspaceId>: the host may only create tasks in workspaces you choose") {
		t.Fatalf("approve without --write = %d, stdout %q, stderr %q", code, out, errOut)
	}
	if len(d.calls) != 0 {
		t.Fatalf("approve without --write reached the daemon: %v", d.calls)
	}

	code, out, errOut = runHostCmd("approve", "abcdef12", "--write", "3", "--write", "5")
	if code != 0 || !strings.Contains(out, "approved laptop") {
		t.Fatalf("approve = %d %q %q", code, out, errOut)
	}
	want := map[string]any{"host": "abcdef12", "writeWorkspaces": []int{3, 5}}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Fatalf("params = %v, want %v", d.calls[0].params, want)
	}

	if code, _, _ = runHostCmd("approve", "abcdef12", "--write", "den"); code != 2 {
		t.Fatalf("approve with a non-numeric --write = %d, want 2", code)
	}
}

func TestHostInvitePrintsInviteAndHints(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.invite": map[string]any{
		"invite": "smind-host://abc", "expiresAt": "2026-10-31T01:00:00Z", "hostId": "abcdef1234567890abcdef",
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("invite")
	if code != 0 || errOut != "" {
		t.Fatalf("invite = %d, stderr %q", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "smind-host://abc" {
		t.Fatalf("stdout = %q", out)
	}
	if lines[1] != "Run on the other host: smind host add 'smind-host://abc'" {
		t.Fatalf("hint line 1 = %q", lines[1])
	}
	if lines[2] != "Then approve here with: smind host approve abcdef12" {
		t.Fatalf("hint line 2 = %q", lines[2])
	}
	want := map[string]any{"windowSeconds": 600, "via": "direct", "addresses": []string{}}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Fatalf("default params = %v, want %v", d.calls[0].params, want)
	}

	d.calls = nil
	if code, _, _ = runHostCmd("invite", "--window", "30m", "--via", "relay", "--addr", "wss://den.example:1", "--addr", "ws://127.0.0.1:4648"); code != 0 {
		t.Fatal("invite with flags failed")
	}
	want = map[string]any{"windowSeconds": 1800, "via": "relay", "addresses": []string{"wss://den.example:1", "ws://127.0.0.1:4648"}}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Fatalf("params = %v, want %v", d.calls[0].params, want)
	}
	for _, args := range [][]string{{"--window", "soon"}, {"--via", "carrier-pigeon"}} {
		d.calls = nil
		if code, _, _ := runHostCmd(args...); code != 2 || len(d.calls) != 0 {
			t.Errorf("invite %v = %d; want a usage error", args, code)
		}
	}
}

func TestHostRouteReadsPasswordFromEnvOnly(t *testing.T) {
	hostHome(t, true)
	t.Setenv("HOST_PW", "route-pw")
	d := &fakeDaemon{results: map[string]any{"fleet.host.route": map[string]any{"ok": true}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("route", "bee", "--addr", "ws://10.0.0.2:4648", "--password-env", "HOST_PW", "--allow-cleartext")
	if code != 0 {
		t.Fatalf("route = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "route set for bee") {
		t.Fatalf("stdout = %q", out)
	}
	want := map[string]any{"host": "bee", "address": "ws://10.0.0.2:4648", "password": "route-pw", "allowCleartext": true}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Fatalf("params = %v, want %v", d.calls[0].params, want)
	}

	// --password does not exist; --addr is required.
	d.calls = nil
	for _, args := range [][]string{
		{"route", "bee", "--addr", "ws://h:1", "--password", "x"},
		{"route", "bee"},
	} {
		if code, _, _ := runHostCmd(args...); code != 2 || len(d.calls) != 0 {
			t.Fatalf("%v = %d, calls %v; want a usage error and no call", args, code, d.calls)
		}
	}
}

func TestHostTestPrintsDetail(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.test": map[string]any{"ok": false, "latencyMs": 3, "state": "down", "code": "x", "detail": "dial refused"}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("test", "bee")
	if code != 0 {
		t.Fatalf("test = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "(down, 3 ms, x, dial refused)") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestHostLsShowsSASForPendingOnly(t *testing.T) {
	hostHome(t, true)
	host := func(id, alias, state, sas string) map[string]any {
		return map[string]any{"hostId": id, "alias": alias, "state": state, "sas": sas,
			"outbound": map[string]any{"enabled": false}, "inbound": map[string]any{"enabled": false}, "link": map[string]any{"state": "down"}}
	}
	d := &fakeDaemon{results: map[string]any{"fleet.host.list": map[string]any{
		"hosts": []map[string]any{
			host("aaaaaaaa11111111", "pend", "pending", "123456"),
			host("bbbbbbbb22222222", "appr", "approved", "654321"),
		},
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("ls")
	if code != 0 || errOut != "" {
		t.Fatalf("ls = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "SAS") || !strings.Contains(out, "123456") || strings.Contains(out, "654321") {
		t.Fatalf("table:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "appr") && !strings.HasSuffix(strings.TrimSpace(line), "-") {
			t.Errorf("approved row should end with '-': %q", line)
		}
	}

	_, out, _ = runHostCmd("ls", "--json")
	if !strings.Contains(out, `"sas":"123456"`) || strings.Contains(out, "654321") {
		t.Fatalf("json: %s", out)
	}
}
