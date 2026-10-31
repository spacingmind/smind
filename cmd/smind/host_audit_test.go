package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

func auditAnswer() map[string]any {
	return map[string]any{"rows": []map[string]any{
		{"id": 2, "time": "2026-11-01T10:05:00Z", "direction": "in", "hostId": "abcdef1234567890", "peer": "winbox",
			"method": "fleet.task.send", "decision": "refused", "code": "not_granted", "latencyMs": 3,
			"summary": "sha256=0123456789abcdef0123456789abcdef {\"prompt\":\"<12 chars>\",\"taskId\":7}"},
		{"id": 1, "time": "2026-11-01T10:00:00Z", "direction": "in", "hostId": "1111111122222222", "peer": "",
			"method": "fleet.info", "decision": "ok", "summary": "sha256=aa {}"},
	}}
}

func TestHostAuditPrintsTableAndSendsParams(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.audit": auditAnswer()}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("audit", "--host", "winbox", "--limit", "20")
	if code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if len(d.calls) != 1 || d.calls[0].method != "fleet.host.audit" {
		t.Fatalf("calls = %v", d.calls)
	}
	if want := map[string]any{"host": "winbox", "limit": 20}; !reflect.DeepEqual(d.calls[0].params, want) {
		t.Errorf("params = %v, want %v", d.calls[0].params, want)
	}
	for _, want := range []string{"TIME", "PEER", "DECISION", "winbox", "fleet.task.send", "refused (not_granted)", "sha256=0123456789ab ", "<12 chars>", "11111111"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0123456789abcdef0123") {
		t.Errorf("hash not shortened:\n%s", out)
	}
}

func TestHostAuditDefaultsAndJSON(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.audit": auditAnswer()}}
	useHostDaemon(t, d)

	code, out, _ := runHostCmd("audit", "--json")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if want := map[string]any{"limit": 50}; !reflect.DeepEqual(d.calls[0].params, want) {
		t.Errorf("params = %v, want %v", d.calls[0].params, want)
	}
	if !strings.Contains(out, `"rows"`) || !strings.Contains(out, "fleet.task.send") {
		t.Errorf("json output: %s", out)
	}
}

func TestHostAuditEmptyAndErrors(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"fleet.host.audit": map[string]any{"rows": []any{}}}}
	useHostDaemon(t, d)
	if code, out, _ := runHostCmd("audit"); code != 0 || !strings.Contains(out, "no fleet activity") {
		t.Errorf("empty: code %d out %q", code, out)
	}

	d2 := &fakeDaemon{results: map[string]any{"fleet.host.audit": &wsclient.RPCError{Code: "not_found", Message: "fleet.host.audit: no such host"}}}
	useHostDaemon(t, d2)
	code, _, errOut := runHostCmd("audit", "--host", "nobody")
	if code != 1 || !strings.Contains(errOut, "no such host") {
		t.Errorf("unknown host: code %d err %q", code, errOut)
	}
}

func TestHostAuditBadUsage(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{}
	useHostDaemon(t, d)
	for _, args := range [][]string{{"audit", "--limit", "0"}, {"audit", "--limit", "201"}, {"audit", "extra"}} {
		if code, _, _ := runHostCmd(args...); code != 2 {
			t.Errorf("%v = exit %d, want 2", args, code)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("daemon reached: %v", d.calls)
	}
}
