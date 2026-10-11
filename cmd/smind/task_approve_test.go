package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// fakePermissionDaemon serves run.logs with two pending permission
// requests (req-1 then req-2, each with allow/always/deny options) and
// records every run.respondPermission call's params.
func fakePermissionDaemon(t *testing.T) (responds func() []map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	var mu sync.Mutex
	var got []map[string]any
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		opts := []map[string]string{
			{"id": "allow", "label": "Allow", "kind": "allow_once"},
			{"id": "always", "label": "Always", "kind": "allow_always"},
			{"id": "deny", "label": "Deny", "kind": "reject_once"},
		}
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var req wireEnvelope
			if json.Unmarshal(data, &req) != nil {
				return
			}
			var result any = map[string]any{}
			switch req.Method {
			case "run.logs":
				result = map[string]any{"status": "running", "events": []map[string]any{
					{"type": "permission_request", "requestId": "req-1", "summary": "one", "options": opts},
					{"type": "permission_request", "requestId": "req-2", "summary": "two", "options": opts},
				}}
			case "run.respondPermission":
				var p map[string]any
				_ = json.Unmarshal(req.Params, &p)
				mu.Lock()
				got = append(got, p)
				mu.Unlock()
			}
			raw, _ := json.Marshal(result)
			out, _ := json.Marshal(wireEnvelope{ID: req.ID, Result: raw})
			if ws.WriteMessage(websocket.TextMessage, out) != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	writeTestConfig(t, home, srv.URL)
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), got...)
	}
}

func TestTaskApproveExplicitOption(t *testing.T) {
	responds := fakePermissionDaemon(t)
	var code int
	out := captureStdout(t, func() { code = run([]string{"task", "approve", "run-1", "req-2", "always"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := responds()
	if len(got) != 1 || got[0]["optionId"] != "always" || got[0]["requestId"] != "req-2" || got[0]["runId"] != "run-1" {
		t.Fatalf("respondPermission calls = %+v, want one with req-2/always", got)
	}
	if !strings.Contains(out, "request req-2 -> always") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestTaskApproveUnknownOptionListsValid(t *testing.T) {
	responds := fakePermissionDaemon(t)
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"task", "approve", "run-1", "req-1", "bogus"}) })
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	want := `task approve: request "req-1" has no option "bogus"; valid: allow, always, deny`
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
	if got := responds(); len(got) != 0 {
		t.Fatalf("respondPermission called: %+v", got)
	}
}

func TestTaskApproveDefaults(t *testing.T) {
	responds := fakePermissionDaemon(t)
	if code := run([]string{"task", "approve", "run-1"}); code != 0 {
		t.Fatalf("1-arg exit = %d", code)
	}
	if code := run([]string{"task", "approve", "run-1", "req-2"}); code != 0 {
		t.Fatalf("2-arg exit = %d", code)
	}
	got := responds()
	if len(got) != 2 {
		t.Fatalf("calls = %+v, want 2", got)
	}
	if got[0]["requestId"] != "req-1" || got[0]["optionId"] != "allow" {
		t.Fatalf("1-arg call = %+v, want req-1/allow", got[0])
	}
	if got[1]["requestId"] != "req-2" || got[1]["optionId"] != "allow" {
		t.Fatalf("2-arg call = %+v, want req-2/allow", got[1])
	}
}

func TestTaskDenyRejectsThirdArg(t *testing.T) {
	responds := fakePermissionDaemon(t)
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"task", "deny", "run-1", "req-1", "deny"}) })
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, cmdTaskDenyUsage) {
		t.Fatalf("stderr = %q, want deny usage", stderr)
	}
	if got := responds(); len(got) != 0 {
		t.Fatalf("respondPermission called: %+v", got)
	}
}
