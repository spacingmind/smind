package wsapi

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Tests for ADR-0018's mcp.* wsapi surface: CRUD round-trip, redaction on
// every read path (results and lifecycle events alike), setEnabled, and the
// name-conflict error -- the same coverage shape as profile_test.go.

func TestMcp_CreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "mcp.create", map[string]any{
		"name": "playwright", "transport": "stdio", "command": "npx",
		"args": []string{"-y", "@playwright/mcp@latest", "--headless"},
		"env":  map[string]string{"TOKEN": "top-secret"},
	})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.create error = %v", resp.Error.Message)
	}
	var created mcpServerResult
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode mcp.create result: %v", err)
	}
	if created.ID == 0 || created.Name != "playwright" || created.Transport != "stdio" || created.Command != "npx" {
		t.Fatalf("mcp.create result = %+v, want populated server", created)
	}
	if !created.Enabled {
		t.Fatalf("mcp.create result Enabled = false, want true (servers are created enabled)")
	}

	sendRequest(t, ws, "get", "mcp.get", map[string]any{"id": created.ID})
	resp = readEnvelopeFor(t, ws, "get", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.get error = %v", resp.Error.Message)
	}
	var got mcpServerResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode mcp.get result: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name {
		t.Fatalf("mcp.get result = %+v, want %+v", got, created)
	}
}

func TestMcp_CreateDuplicateNameIsConflictError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "a", "mcp.create", map[string]any{
		"name": "dup", "transport": "http", "url": "https://example.com/mcp",
	})
	if resp := readEnvelopeFor(t, ws, "a", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.create error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "b", "mcp.create", map[string]any{
		"name": "dup", "transport": "http", "url": "https://example.com/other",
	})
	resp := readEnvelopeFor(t, ws, "b", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("mcp.create with a duplicate name: error = nil, want a conflict error")
	}
}

func TestMcp_ListOrdering(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "a", "mcp.create", map[string]any{"name": "first", "transport": "http", "url": "https://a.example.com"})
	firstResp := readEnvelopeFor(t, ws, "a", 5*time.Second)
	sendRequest(t, ws, "b", "mcp.create", map[string]any{"name": "second", "transport": "http", "url": "https://b.example.com"})
	secondResp := readEnvelopeFor(t, ws, "b", 5*time.Second)

	var first, second mcpServerResult
	mustDecode(t, firstResp.Result, &first)
	mustDecode(t, secondResp.Result, &second)

	sendRequest(t, ws, "list", "mcp.list", nil)
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.list error = %v", resp.Error.Message)
	}
	var list []mcpServerResult
	mustDecode(t, resp.Result, &list)
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("mcp.list = %+v, want [%+v %+v]", list, first, second)
	}
}

func TestMcp_UpdateVisibleFromSecondConnection(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	wsA := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsA.Close() })
	wsB := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsB.Close() })

	sendRequest(t, wsA, "create", "mcp.create", map[string]any{
		"name": "old", "transport": "stdio", "command": "npx",
	})
	var created mcpServerResult
	mustDecode(t, readEnvelopeFor(t, wsA, "create", 5*time.Second).Result, &created)

	sendRequest(t, wsA, "update", "mcp.update", map[string]any{
		"id": created.ID, "name": "new", "transport": "http", "url": "https://example.com/mcp",
	})
	if resp := readEnvelopeFor(t, wsA, "update", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.update error = %v", resp.Error.Message)
	}

	sendRequest(t, wsB, "get", "mcp.get", map[string]any{"id": created.ID})
	resp := readEnvelopeFor(t, wsB, "get", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.get error = %v", resp.Error.Message)
	}
	var got mcpServerResult
	mustDecode(t, resp.Result, &got)
	if got.Name != "new" || got.Transport != "http" {
		t.Fatalf("mcp.get from second connection = %+v, want updated fields", got)
	}
}

func TestMcp_DeleteThenGetErrors(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "mcp.create", map[string]any{"name": "throwaway", "transport": "stdio", "command": "npx"})
	var created mcpServerResult
	mustDecode(t, readEnvelopeFor(t, ws, "create", 5*time.Second).Result, &created)

	sendRequest(t, ws, "delete", "mcp.delete", map[string]any{"id": created.ID})
	if resp := readEnvelopeFor(t, ws, "delete", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.delete error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "get", "mcp.get", map[string]any{"id": created.ID})
	resp := readEnvelopeFor(t, ws, "get", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("mcp.get after delete: error = nil, want a not-found error")
	}
}

func TestMcp_SetEnabledToggles(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "mcp.create", map[string]any{"name": "toggle", "transport": "stdio", "command": "npx"})
	var created mcpServerResult
	mustDecode(t, readEnvelopeFor(t, ws, "create", 5*time.Second).Result, &created)
	if !created.Enabled {
		t.Fatalf("mcp.create result Enabled = false, want true")
	}

	sendRequest(t, ws, "disable", "mcp.setEnabled", map[string]any{"id": created.ID, "enabled": false})
	resp := readEnvelopeFor(t, ws, "disable", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.setEnabled error = %v", resp.Error.Message)
	}
	var disabled mcpServerResult
	mustDecode(t, resp.Result, &disabled)
	if disabled.Enabled || disabled.Name != "toggle" || disabled.Command != "npx" {
		t.Fatalf("mcp.setEnabled(false) result = %+v, want Enabled false, other fields untouched", disabled)
	}

	sendRequest(t, ws, "enable", "mcp.setEnabled", map[string]any{"id": created.ID, "enabled": true})
	resp = readEnvelopeFor(t, ws, "enable", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("mcp.setEnabled error = %v", resp.Error.Message)
	}
	var enabled mcpServerResult
	mustDecode(t, resp.Result, &enabled)
	if !enabled.Enabled {
		t.Fatalf("mcp.setEnabled(true) result = %+v, want Enabled true", enabled)
	}
}

// TestMcp_RedactionOnEveryReadResult proves mcp.create/mcp.get/mcp.list/
// mcp.update never echo back a secret env/headers value: asserted against
// the raw response bytes (not the decoded struct), so a redaction bug that
// only fixes one field or one path can't hide behind a struct that happens
// to decode fine.
func TestMcp_RedactionOnEveryReadResult(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	const secret = "sk-super-secret-value"
	sendRequest(t, ws, "create", "mcp.create", map[string]any{
		"name": "secretive", "transport": "stdio", "command": "npx",
		"env": map[string]string{"TOKEN": secret},
	})
	createResp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if createResp.Error != nil {
		t.Fatalf("mcp.create error = %v", createResp.Error.Message)
	}
	if bytes.Contains(createResp.Result, []byte(secret)) {
		t.Fatalf("mcp.create result contains the raw secret: %s", createResp.Result)
	}
	if !bytes.Contains(createResp.Result, []byte(mcpSecretPlaceholder)) || !bytes.Contains(createResp.Result, []byte(`"TOKEN"`)) {
		t.Fatalf("mcp.create result = %s, want the TOKEN key preserved with a placeholder value", createResp.Result)
	}
	var created mcpServerResult
	mustDecode(t, createResp.Result, &created)

	sendRequest(t, ws, "get", "mcp.get", map[string]any{"id": created.ID})
	getResp := readEnvelopeFor(t, ws, "get", 5*time.Second)
	if bytes.Contains(getResp.Result, []byte(secret)) {
		t.Fatalf("mcp.get result contains the raw secret: %s", getResp.Result)
	}

	sendRequest(t, ws, "list", "mcp.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if bytes.Contains(listResp.Result, []byte(secret)) {
		t.Fatalf("mcp.list result contains the raw secret: %s", listResp.Result)
	}

	sendRequest(t, ws, "update", "mcp.update", map[string]any{
		"id": created.ID, "name": "secretive", "transport": "stdio", "command": "npx",
		"env": map[string]string{"TOKEN": secret, "OTHER": "another-secret"},
	})
	updateResp := readEnvelopeFor(t, ws, "update", 5*time.Second)
	if updateResp.Error != nil {
		t.Fatalf("mcp.update error = %v", updateResp.Error.Message)
	}
	if bytes.Contains(updateResp.Result, []byte(secret)) || bytes.Contains(updateResp.Result, []byte("another-secret")) {
		t.Fatalf("mcp.update result contains a raw secret: %s", updateResp.Result)
	}
}

// TestMcp_RedactionOnLifecycleEvents is the events counterpart to
// TestMcp_RedactionOnEveryReadResult: mcpServer.created/updated must be
// redacted on the wire too, not just on the direct RPC result -- asserted
// against the raw inbound WebSocket frame bytes so a bug that redacts the
// RPC result but forgets the notifier path (or vice versa) is caught.
//
// The mutation is issued on wsMutate; wsWatch only ever subscribes and then
// reads -- no further request is ever sent on it -- so every subsequent
// message on wsWatch is guaranteed to be an event notification, and
// nextRawMessage can read it directly without risking readEnvelopeFor's
// "discard anything that isn't my response id" behavior silently dropping
// the very event this test needs to inspect.
func TestMcp_RedactionOnLifecycleEvents(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	wsMutate := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsMutate.Close() })
	wsWatch := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsWatch.Close() })

	sendRequest(t, wsWatch, "sub", "events.subscribe", map[string]any{
		"topics": []string{TopicMcpServerCreated, TopicMcpServerUpdated},
	})
	if resp := readEnvelopeFor(t, wsWatch, "sub", 5*time.Second); resp.Error != nil {
		t.Fatalf("events.subscribe error = %v", resp.Error.Message)
	}

	const secret = "sk-super-secret-value"
	sendRequest(t, wsMutate, "create", "mcp.create", map[string]any{
		"name": "secretive", "transport": "stdio", "command": "npx",
		"env": map[string]string{"TOKEN": secret},
	})
	if resp := readEnvelopeFor(t, wsMutate, "create", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.create error = %v", resp.Error.Message)
	}

	raw := nextRawMessage(t, wsWatch, 5*time.Second)
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatalf("mcpServer.created event contains the raw secret: %s", raw)
	}
	if !bytes.Contains(raw, []byte(mcpSecretPlaceholder)) {
		t.Fatalf("mcpServer.created event = %s, want the redaction placeholder", raw)
	}
	var env struct {
		Event struct {
			Payload mcpServerCreatedPayload `json:"payload"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode mcpServer.created event: %v", err)
	}
	created := env.Event.Payload.Server

	sendRequest(t, wsMutate, "update", "mcp.update", map[string]any{
		"id": created.ID, "name": "secretive", "transport": "stdio", "command": "npx",
		"env": map[string]string{"TOKEN": "a-different-secret"},
	})
	if resp := readEnvelopeFor(t, wsMutate, "update", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.update error = %v", resp.Error.Message)
	}
	raw = nextRawMessage(t, wsWatch, 5*time.Second)
	if bytes.Contains(raw, []byte("a-different-secret")) {
		t.Fatalf("mcpServer.updated event contains the raw secret: %s", raw)
	}
}

func TestMcp_ListDisabledServersStillVisible(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "mcp.create", map[string]any{"name": "disable-me", "transport": "stdio", "command": "npx"})
	var created mcpServerResult
	mustDecode(t, readEnvelopeFor(t, ws, "create", 5*time.Second).Result, &created)

	sendRequest(t, ws, "off", "mcp.setEnabled", map[string]any{"id": created.ID, "enabled": false})
	if resp := readEnvelopeFor(t, ws, "off", 5*time.Second); resp.Error != nil {
		t.Fatalf("mcp.setEnabled error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "list", "mcp.list", nil)
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var list []mcpServerResult
	mustDecode(t, resp.Result, &list)
	if len(list) != 1 || list[0].ID != created.ID || list[0].Enabled {
		t.Fatalf("mcp.list after disable = %+v, want the disabled server still listed", list)
	}
}

// nextRawMessage reads the next raw WebSocket text frame off ws, regardless
// of whether it's an RPC response or an ADR-0005 event notification -- used
// where a test needs the literal bytes on the wire rather than a decoded
// envelope/eventNotification, so a redaction bug in one JSON encoding path
// can't hide behind a struct that happens to decode to the right value.
func nextRawMessage(t *testing.T, ws *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	if err := ws.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	return data
}
