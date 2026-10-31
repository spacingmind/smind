package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

func TestHostLabelSetSendsPairs(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.labels": map[string]any{
		"host": "winbox", "hostId": "abcdef1234567890", "labels": map[string]any{"os": "windows", "gpu": "cuda"},
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("label", "set", "winbox", "os:windows", "gpu:cuda")
	if code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if len(d.calls) != 1 || d.calls[0].method != "fleet.host.labels" {
		t.Fatalf("calls = %v", d.calls)
	}
	want := map[string]any{"host": "winbox", "set": map[string]string{"os": "windows", "gpu": "cuda"}}
	if !reflect.DeepEqual(d.calls[0].params, want) {
		t.Errorf("params = %v, want %v", d.calls[0].params, want)
	}
}

func TestHostLabelSetBadPair(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{}
	useHostDaemon(t, d)
	for _, bad := range []string{"os", ":x", "x:"} {
		code, _, _ := runHostCmd("label", "set", "winbox", bad)
		if code != 2 {
			t.Errorf("%q = exit %d, want 2", bad, code)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("daemon reached: %v", d.calls)
	}
}

func TestHostLabelRm(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.labels": map[string]any{
		"host": "winbox", "hostId": "h", "labels": map[string]any{},
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("label", "rm", "winbox", "os", "gpu")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	want := map[string]any{"host": "winbox", "remove": []string{"os", "gpu"}}
	if len(d.calls) != 1 || !reflect.DeepEqual(d.calls[0].params, want) {
		t.Errorf("calls = %v, want params %v", d.calls, want)
	}
	if !strings.Contains(out, "(no labels)") {
		t.Errorf("stdout %q", out)
	}
}

func TestHostLabelLsOne(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.labels": map[string]any{
		"host": "winbox", "hostId": "h", "labels": map[string]any{"os": "windows", "arch": "arm64", "gpu": "cuda"},
	}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("label", "ls", "winbox")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if out != "arch:arm64\ngpu:cuda\nos:windows\n" {
		t.Errorf("stdout %q", out)
	}
	if want := (map[string]any{"host": "winbox"}); !reflect.DeepEqual(d.calls[0].params, want) {
		t.Errorf("params = %v", d.calls[0].params)
	}
}

func TestHostLabelLsAll(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.list": map[string]any{"hosts": []any{
		map[string]any{"alias": "winbox", "hostId": "abcdef1234567890", "labels": map[string]any{"os": "windows", "gpu": "cuda"}},
		map[string]any{"alias": "plain", "hostId": "1234567890abcdef"},
	}}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("label", "ls")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"ALIAS", "LABELS", "winbox", "gpu:cuda,os:windows", "plain"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q lacks %q", out, want)
		}
	}
	if !strings.Contains(out, "plain   -") {
		t.Errorf("stdout %q: want - for no labels", out)
	}
}

func TestHostLabelDaemonRefusalPrinted(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.labels": &wsclient.RPCError{Code: "invalid", Message: "label key \"token\" looks like a secret"}}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("label", "set", "winbox", "token:x")
	if code != 1 || out != "" || !strings.Contains(errOut, "looks like a secret") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}
