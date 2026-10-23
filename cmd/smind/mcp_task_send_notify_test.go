package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// newTaskSendRecorder returns a task_send caller backed by a fake daemon; it
// reports the params of the run.start the call produced.
func newTaskSendRecorder(t *testing.T) func(map[string]any) map[string]json.RawMessage {
	t.Helper()
	send, _ := newTaskSendRecorderMethods(t)
	return send
}

// newTaskSendRecorderMethods is newTaskSendRecorder plus the list of every
// daemon method the fake saw, in order.
func newTaskSendRecorderMethods(t *testing.T) (func(map[string]any) map[string]json.RawMessage, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	var starts []map[string]json.RawMessage
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var env struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := ws.ReadJSON(&env); err != nil {
				return
			}
			mu.Lock()
			methods = append(methods, env.Method)
			mu.Unlock()
			var result any = map[string]any{"ok": true}
			switch env.Method {
			case "provider.list":
				result = map[string]any{"providers": []map[string]any{{"id": "glm"}}}
			case "profile.get":
				result = map[string]any{"Provider": "glm"}
			case "run.start":
				var p map[string]json.RawMessage
				_ = json.Unmarshal(env.Params, &p)
				mu.Lock()
				starts = append(starts, p)
				mu.Unlock()
				result = map[string]any{"runId": "r1"}
			}
			b, _ := json.Marshal(result)
			_ = ws.WriteJSON(map[string]any{"id": env.ID, "result": json.RawMessage(b)})
		}
	}))
	t.Cleanup(srv.Close)
	client, err := wsclient.Dial(context.Background(), srv.URL[len("http://"):], "test-token")
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	server := newMCPServer(client)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	send := func(args map[string]any) map[string]json.RawMessage {
		t.Helper()
		var out taskSendOutput
		if isErr := callMCPTool(t, cs, "task_send", args, &out); isErr {
			t.Fatalf("task_send(%v) failed", args)
		}
		mu.Lock()
		defer mu.Unlock()
		return starts[len(starts)-1]
	}

	return send, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), methods...)
	}
}

// TestMCPTaskSend_NotifyOnFinishForwarded pins ADR-0041 N1's MCP seam: an
// explicit notifyOnFinish=false reaches run.start's params, and an omitted
// value leaves the key absent (the daemon defaults to true).
func TestMCPTaskSend_NotifyOnFinishForwarded(t *testing.T) {
	send := newTaskSendRecorder(t)
	got := send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi", "notifyOnFinish": false})
	if string(got["notifyOnFinish"]) != "false" {
		t.Fatalf("run.start notifyOnFinish = %q, want false", got["notifyOnFinish"])
	}
	got = send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi", "notifyOnFinish": true})
	if string(got["notifyOnFinish"]) != "true" {
		t.Fatalf("run.start notifyOnFinish = %q, want true", got["notifyOnFinish"])
	}
	got = send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi"})
	if _, present := got["notifyOnFinish"]; present {
		t.Fatalf("run.start has notifyOnFinish = %q when omitted, want the key absent", got["notifyOnFinish"])
	}
}

// TestMCPTaskSend_ProfileSendForwardsFromIds: a profileId send stays
// source=human but still carries the sender ids (ADR-0041 N1c).
func TestMCPTaskSend_ProfileSendForwardsFromIds(t *testing.T) {
	send := newTaskSendRecorder(t)
	got := send(map[string]any{"taskId": 1, "prompt": "hi", "profileId": 5, "fromTaskId": 7, "fromChatId": 9})
	if string(got["source"]) != `"human"` {
		t.Fatalf("source = %s, want \"human\"", got["source"])
	}
	if string(got["fromTaskId"]) != "7" || string(got["fromChatId"]) != "9" {
		t.Fatalf("fromTaskId/fromChatId = %s/%s, want 7/9", got["fromTaskId"], got["fromChatId"])
	}
}

// TestMCPTaskSend_ForwardsProfileID: profileId reaches run.start's params
// so the daemon can record it on the run (ADR-0041 F2); omitted stays absent.
func TestMCPTaskSend_ForwardsProfileID(t *testing.T) {
	send := newTaskSendRecorder(t)
	got := send(map[string]any{"taskId": 1, "prompt": "hi", "profileId": 5})
	if string(got["profileId"]) != "5" {
		t.Fatalf("run.start profileId = %s, want 5", got["profileId"])
	}
	got = send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi"})
	if _, present := got["profileId"]; present {
		t.Fatalf("run.start has profileId = %s when omitted, want the key absent", got["profileId"])
	}
}

func TestMCPTaskSend_ProfileZeroForwardedWithoutProfileGet(t *testing.T) {
	send, methods := newTaskSendRecorderMethods(t)
	got := send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi", "profileId": 0})
	if string(got["profileId"]) != "0" {
		t.Fatalf("run.start profileId = %q, want 0", got["profileId"])
	}
	for _, m := range methods() {
		if m == "profile.get" {
			t.Fatalf("profile.get was called for profileId 0: %v", methods())
		}
	}
}

func TestMCPTaskSend_OmittedProfileIdOmittedOnTheWire(t *testing.T) {
	send := newTaskSendRecorder(t)
	got := send(map[string]any{"taskId": 1, "provider": "glm", "prompt": "hi"})
	if v, ok := got["profileId"]; ok {
		t.Fatalf("run.start carries profileId = %s, want the key absent (inherit)", v)
	}
}
