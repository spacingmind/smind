package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

func readResult(op string, data any) map[string]any {
	return map[string]any{"host": "box", "hostId": "abcdef1234567890", "op": op, "data": data}
}

func TestHostReadVerbsSendRemoteReadParams(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"remote.read": readResult("x", map[string]any{
		"tasks": []any{}, "workspaces": []any{}, "events": []any{}, "nextCursor": 0, "hasMore": false,
		"id": 1, "state": "running", "runId": "r1", "taskId": 4, "provider": "p", "costUsd": nil,
		"hostId": "h", "limits": map[string]any{},
	})}}
	useHostDaemon(t, d)

	cases := []struct {
		args []string
		want map[string]any
	}{
		{[]string{"info", "box"}, map[string]any{"host": "box", "op": "info"}},
		{[]string{"workspaces", "box"}, map[string]any{"host": "box", "op": "workspaces"}},
		{[]string{"tasks", "box", "--workspace", "3", "--cursor", "7", "--limit", "20"},
			map[string]any{"host": "box", "op": "tasks", "workspaceId": 3, "cursor": 7, "limit": 20}},
		{[]string{"task", "box:12"}, map[string]any{"host": "box", "op": "task", "id": 12}},
		{[]string{"status", "box", "run-9"}, map[string]any{"host": "box", "op": "status", "runId": "run-9"}},
		{[]string{"status", "box:4"}, map[string]any{"host": "box", "op": "status", "taskId": 4}},
		{[]string{"activity", "box:4", "--run", "r2", "--cursor", "5", "--limit", "10"},
			map[string]any{"host": "box", "op": "activity", "taskId": 4, "runId": "r2", "cursor": 5, "limit": 10}},
	}
	for _, c := range cases {
		d.calls = nil
		code, out, errOut := runHostCmd(c.args...)
		if code != 0 {
			t.Fatalf("%v = %d, stdout %q, stderr %q", c.args, code, out, errOut)
		}
		if len(d.calls) != 1 || d.calls[0].method != "remote.read" {
			t.Fatalf("%v calls = %v", c.args, d.calls)
		}
		if !reflect.DeepEqual(d.calls[0].params, c.want) {
			t.Errorf("%v params = %v, want %v", c.args, d.calls[0].params, c.want)
		}
	}
}

func TestHostReadGlobalIDParsing(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"remote.read": readResult("task", map[string]any{"id": 12, "globalId": "box:12"})}}
	useHostDaemon(t, d)

	code, _, errOut := runHostCmd("task", "box:12")
	if code != 0 {
		t.Fatalf("box:12 = %d, stderr %q", code, errOut)
	}
	if got := d.calls[0].params.(map[string]any); got["host"] != "box" || got["id"] != 12 {
		t.Fatalf("params = %v", got)
	}
	d.calls = nil
	for _, in := range []string{"box", "box:x", ":3", "a:b:3"} {
		if code, _, _ := runHostCmd("task", in); code != 2 {
			t.Errorf("task %q = %d, want 2", in, code)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("a bad id reached the daemon: %v", d.calls)
	}
}

func TestHostReadStatusUnknownNeverFinished(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"remote.read": map[string]any{
		"host": "box", "hostId": "h", "op": "status", "data": nil, "statusUnknown": true, "failures": 3,
	}}}
	useHostDaemon(t, d)

	for _, args := range [][]string{{"status", "box", "r1"}, {"status", "box", "r1", "--json"}} {
		code, out, errOut := runHostCmd(args...)
		if code != 1 || !strings.Contains(errOut, "status unknown after 3 failed observations") {
			t.Fatalf("%v = %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		all := strings.ToLower(out + errOut)
		if strings.Contains(all, "finished") || strings.Contains(all, "done") {
			t.Errorf("%v printed a terminal word: %q %q", args, out, errOut)
		}
	}
}

func TestHostReadTypedErrorsPrinted(t *testing.T) {
	hostHome(t, true)
	for _, code := range []string{"hop_limit", "not_granted", "fleet_version_unsupported"} {
		d := &fakeDaemon{results: map[string]any{"remote.read": &wsclient.RPCError{Code: code, Message: "m"}}}
		useHostDaemon(t, d)
		rc, out, errOut := runHostCmd("info", "box")
		if rc != 1 || !strings.Contains(errOut, "smind host info: "+code+": m") || out != "" {
			t.Errorf("%s = %d, stdout %q, stderr %q", code, rc, out, errOut)
		}
	}
}

func TestHostReadFleetOffSaysSo(t *testing.T) {
	hostHome(t, false)
	d := &fakeDaemon{}
	useHostDaemon(t, d)
	for _, args := range [][]string{
		{"info", "box"}, {"workspaces", "box"}, {"tasks", "box"}, {"task", "box:1"},
		{"status", "box", "r1"}, {"activity", "box:1"},
	} {
		code, _, errOut := runHostCmd(args...)
		if code != 1 || !strings.Contains(errOut, "fleet is off") {
			t.Errorf("%v = %d, stderr %q", args, code, errOut)
		}
	}
	if len(d.calls) != 0 {
		t.Fatalf("the daemon was dialed with the fleet off: %v", d.calls)
	}
}

func TestHostReadJSONPrintsResultObject(t *testing.T) {
	hostHome(t, true)
	stub := readResult("workspaces", map[string]any{"workspaces": []any{
		map[string]any{"id": 1, "title": "w", "repoUrl": "u", "defaultBranch": "main"},
	}})
	d := &fakeDaemon{results: map[string]any{"remote.read": stub}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("workspaces", "box", "--json")
	if code != 0 {
		t.Fatalf("= %d, stderr %q", code, errOut)
	}
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Fatalf("output is not one line: %q", out)
	}
	var got any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	var want any
	raw, _ := json.Marshal(stub)
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHostReadTasksTableAndCursor(t *testing.T) {
	hostHome(t, true)
	d := &fakeDaemon{results: map[string]any{"remote.read": readResult("tasks", map[string]any{
		"tasks":      []any{map[string]any{"id": 12, "workspaceId": 3, "title": "Fix bug", "branch": "smind/fix", "state": "running", "host": "box", "globalId": "box:12"}},
		"nextCursor": 13,
	})}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("tasks", "box")
	if code != 0 {
		t.Fatalf("= %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"GLOBAL ID", "STATE", "BRANCH", "TITLE", "box:12", "running", "smind/fix", "Fix bug"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "more: --cursor 13\n") {
		t.Fatalf("missing cursor line:\n%s", out)
	}
}

func TestHostReadSanitizesRemoteText(t *testing.T) {
	hostHome(t, true)
	title := "ok\x1b[31mred\x1b]0;pwn\x07"
	d := &fakeDaemon{results: map[string]any{"remote.read": readResult("tasks", map[string]any{
		"tasks": []any{map[string]any{"id": 1, "title": title, "branch": "b‮x", "state": "running", "host": "box", "globalId": "box:1"}},
	})}}
	useHostDaemon(t, d)

	code, out, errOut := runHostCmd("tasks", "box")
	if code != 0 {
		t.Fatalf("tasks = %d, stderr %q", code, errOut)
	}
	for _, bad := range []string{"\x1b", "\x07", "\r", "‮"} {
		if strings.Contains(out, bad) {
			t.Errorf("tasks output contains %q: %q", bad, out)
		}
	}

	d.results["remote.read"] = readResult("activity", map[string]any{
		"events": []any{
			map[string]any{"seq": 1, "type": "text", "text": "a\r\nfake line"},
			map[string]any{"seq": 2, "type": "text", "text": "\x1b[2Jb"},
		},
		"nextCursor": 0, "hasMore": false,
	})
	code, out, errOut = runHostCmd("activity", "box:1")
	if code != 0 {
		t.Fatalf("activity = %d, stderr %q", code, errOut)
	}
	if strings.ContainsAny(out, "\x1b\x07\r") || strings.Count(out, "\n") != 2 {
		t.Fatalf("activity output is not one clean line per event: %q", out)
	}

	// --json is the raw object and keeps the string as sent.
	d.results["remote.read"] = readResult("tasks", map[string]any{
		"tasks": []any{map[string]any{"id": 1, "title": title}},
	})
	code, out, _ = runHostCmd("tasks", "box", "--json")
	var got struct {
		Data struct {
			Tasks []struct {
				Title string `json:"title"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil || len(got.Data.Tasks) != 1 || got.Data.Tasks[0].Title != title {
		t.Fatalf("--json = %d, %v, %q", code, err, out)
	}
}
