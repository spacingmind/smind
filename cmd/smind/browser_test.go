package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// stubBrowserDaemon is a stateful fake daemon: handle answers each method and
// every call is recorded (method plus params) in the returned slice.
func stubBrowserDaemon(t *testing.T, handle func(method string, params map[string]any) (any, error)) *[]map[string]any {
	t.Helper()
	var calls []map[string]any
	old := policyDial
	policyDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, result any) error {
			p, _ := params.(map[string]any)
			p["method"] = method
			calls = append(calls, p)
			reply, err := handle(method, p)
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(reply)
			return json.Unmarshal(raw, result)
		}, func() {}, nil
	}
	t.Cleanup(func() { policyDial = old })
	return &calls
}

func browserSettingsReply(enabled bool, hosts []string, extra ...func(map[string]any)) map[string]any {
	s := map[string]any{
		"workspaceId": int64(7), "enabled": enabled, "allowHosts": hosts, "profileMode": "ephemeral",
		"killSwitch": false, "available": true, "chromeInstalled": true, "installStep": "",
		"updatedAt": "2026-01-01T00:00:00Z", "updatedBy": "human",
	}
	for _, f := range extra {
		f(s)
	}
	return s
}

func TestBrowserCLI_RefusesWithRunID(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "run-1")
	dialled := false
	old := policyDial
	policyDial = func(context.Context) (wsCaller, func(), error) {
		dialled = true
		return nil, func() {}, fmt.Errorf("dialled")
	}
	t.Cleanup(func() { policyDial = old })
	var out, errb bytes.Buffer
	if code := runBrowser(context.Background(), []string{"enable", "--workspace", "7"}, &out, &errb); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if dialled {
		t.Error("the daemon was dialled inside an agent run")
	}
	if !strings.Contains(errb.String(), "is for the person, not for an agent run") || out.Len() != 0 {
		t.Errorf("stdout %q stderr %q", out.String(), errb.String())
	}
}

func TestBrowserCLI_Flow(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	settings := browserSettingsReply(false, []string{})
	calls := stubBrowserDaemon(t, func(method string, params map[string]any) (any, error) {
		switch method {
		case "browser.settings.get":
			return settings, nil
		case "browser.settings.set":
			if e, ok := params["enabled"]; ok {
				settings["enabled"] = e
			}
			hosts, _ := settings["allowHosts"].([]string)
			if h, ok := params["allowAdd"].([]string); ok {
				hosts = append(append([]string{}, hosts...), h...)
			}
			if h, ok := params["allowRemove"].([]string); ok {
				var keep []string
				for _, x := range hosts {
					if x != h[0] {
						keep = append(keep, x)
					}
				}
				hosts = keep
			}
			settings["allowHosts"] = hosts
			return settings, nil
		case "browser.data.clear":
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("unknown method %s", method)
	})
	var out, errb bytes.Buffer
	step := func(want int, args ...string) string {
		t.Helper()
		out.Reset()
		errb.Reset()
		if code := runBrowser(context.Background(), args, &out, &errb); code != want {
			t.Fatalf("%v: code %d (want %d), stderr %q", args, code, want, errb.String())
		}
		return out.String()
	}

	if s := step(0, "enable", "--workspace", "7"); !strings.Contains(s, "browser on for workspace 7") ||
		!strings.Contains(s, "an unidentified local connection counts as the human (ADR-0056)") {
		t.Errorf("enable stdout %q", s)
	}
	step(0, "allow", "add", "a.test", "--workspace", "7")
	step(0, "allow", "add", "b.test", "--workspace", "7")
	step(0, "allow", "rm", "a.test", "--workspace", "7")
	if s := step(0, "status", "--workspace", "7"); !strings.Contains(s, "browser: on") ||
		!strings.Contains(s, "allowed hosts: b.test") || !strings.Contains(s, "profile: ephemeral") {
		t.Errorf("status stdout %q", s)
	}
	if s := step(0, "disable", "--workspace", "7"); !strings.Contains(s, "browser off for workspace 7") ||
		strings.Contains(s, "ADR-0056") {
		t.Errorf("disable stdout %q", s)
	}
	if s := step(0, "clear", "--workspace", "7"); !strings.Contains(s, "browser session ended and its data removed for workspace 7") {
		t.Errorf("clear stdout %q", s)
	}

	// The whole session, call by call: allow add/rm send ONE host each and the daemon
	// applies it atomically (no client-side read-modify-write).
	want := []struct {
		method string
		params map[string]any
	}{
		{"browser.settings.set", map[string]any{"workspaceId": int64(7), "enabled": true}},
		{"browser.settings.set", map[string]any{"workspaceId": int64(7), "allowAdd": []string{"a.test"}}},
		{"browser.settings.set", map[string]any{"workspaceId": int64(7), "allowAdd": []string{"b.test"}}},
		{"browser.settings.set", map[string]any{"workspaceId": int64(7), "allowRemove": []string{"a.test"}}},
		{"browser.settings.get", map[string]any{"workspaceId": int64(7)}},
		{"browser.settings.set", map[string]any{"workspaceId": int64(7), "enabled": false}},
		{"browser.data.clear", map[string]any{"workspaceId": int64(7)}},
	}
	got := *calls
	if len(got) != len(want) {
		t.Fatalf("%d calls, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i]["method"] != w.method {
			t.Errorf("call %d: method %v, want %s", i, got[i]["method"], w.method)
		}
		for k, v := range w.params {
			if !reflect.DeepEqual(got[i][k], v) {
				t.Errorf("call %d (%s): %s = %v, want %v", i, w.method, k, got[i][k], v)
			}
		}
	}
}

func TestBrowserCLI_StatusText(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	reply := browserSettingsReply(true, []string{"a.test", "b.test"},
		func(s map[string]any) { s["killSwitch"] = true },
		func(s map[string]any) { s["available"] = false },
		func(s map[string]any) { s["chromeInstalled"] = false; s["installStep"] = "download-chrome" },
	)
	calls := stubBrowserDaemon(t, func(method string, _ map[string]any) (any, error) {
		if method != "browser.settings.get" {
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		return reply, nil
	})
	var out, errb bytes.Buffer
	if code := runBrowser(context.Background(), []string{"status", "--workspace", "7"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	for _, line := range []string{
		"browser: on",
		"allowed hosts: a.test, b.test",
		"profile: ephemeral",
		"kill switch: ON (the daemon's configuration disables the browser)",
		"not yet offered to agents in this version",
		"no browser installed: download-chrome",
	} {
		if !strings.Contains(out.String(), line+"\n") {
			t.Errorf("status stdout %q, want line %q", out.String(), line)
		}
	}
	if c := (*calls)[0]; c["method"] != "browser.settings.get" || c["workspaceId"] != int64(7) {
		t.Errorf("call = %v", c)
	}

	// off with an empty allowlist names loopback only; --json prints raw.
	reply = browserSettingsReply(false, []string{})
	out.Reset()
	if code := runBrowser(context.Background(), []string{"status", "--workspace", "7"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "browser: off") || !strings.Contains(out.String(), "allowed hosts: loopback only") {
		t.Errorf("status stdout %q", out.String())
	}
	out.Reset()
	if code := runBrowser(context.Background(), []string{"status", "--workspace", "7", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil || decoded["profileMode"] != "ephemeral" {
		t.Errorf("--json stdout %q (err %v)", out.String(), err)
	}
}

func TestBrowserCLI_Usage(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	calls := stubBrowserDaemon(t, func(string, map[string]any) (any, error) {
		return browserSettingsReply(false, []string{}), nil
	})
	var out, errb bytes.Buffer
	for _, args := range [][]string{
		{},
		{"status"},
		{"enable", "--workspace", "x"},
		{"enable", "--workspace", "0"},
		{"enable", "--workspace", "-3"},
		{"bogus", "--workspace", "1"},
		{"allow", "--workspace", "1"},
		{"allow", "add"},
		{"allow", "add", "--workspace", "1"},
		{"allow", "add", "a.test", "b.test", "--workspace", "1"},
		{"allow", "bogus", "h", "--workspace", "1"},
		{"status", "--workspace", "1", "extra"},
		{"status", "--workspace", "1", "--y"},
	} {
		out.Reset()
		errb.Reset()
		if code := runBrowser(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
		if !strings.Contains(errb.String(), cmdBrowserUsage) {
			t.Errorf("%v: stderr %q, want the usage", args, errb.String())
		}
	}
	if len(*calls) != 0 {
		t.Errorf("usage errors dialled the daemon: %+v", *calls)
	}
}

func TestBrowserCLI_AllowRmMissingHostShowsTheDaemonsRefusal(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	calls := stubBrowserDaemon(t, func(method string, _ map[string]any) (any, error) {
		return nil, fmt.Errorf("browser.settings.set: host not in the allowlist: b.test")
	})
	var out, errb bytes.Buffer
	if code := runBrowser(context.Background(), []string{"allow", "rm", "b.test", "--workspace", "7"}, &out, &errb); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "host not in the allowlist") {
		t.Errorf("stderr %q", errb.String())
	}
	if len(*calls) != 1 || (*calls)[0]["allowRemove"] == nil {
		t.Errorf("want one allowRemove call: %+v", *calls)
	}
}

func TestBrowserCLI_PassesTheDaemonsRefusalThrough(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	stubBrowserDaemon(t, func(method string, _ map[string]any) (any, error) {
		if method == "browser.settings.get" {
			return browserSettingsReply(true, []string{}), nil
		}
		return nil, fmt.Errorf(`allowHosts: "bad host" is not a valid host`)
	})
	var out, errb bytes.Buffer
	if code := runBrowser(context.Background(), []string{"allow", "add", "bad host", "--workspace", "7"}, &out, &errb); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(errb.String(), `"bad host" is not a valid host`) {
		t.Errorf("stderr %q, want the daemon's refusal text", errb.String())
	}
}
