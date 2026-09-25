package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func TestProfile_CreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "profile.create", map[string]any{
		"name": "UI work", "provider": "claude-native", "approvalPolicy": "manual",
		"thinkingLevel": "standard", "notes": "For UI polish.",
	})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("profile.create error = %v", resp.Error.Message)
	}
	var created store.AgentProfile
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		t.Fatalf("decode profile.create result: %v", err)
	}
	if created.ID == 0 || created.Name != "UI work" || created.Provider != "claude-native" {
		t.Fatalf("profile.create result = %+v, want populated profile", created)
	}

	sendRequest(t, ws, "get", "profile.get", map[string]any{"id": created.ID})
	resp = readEnvelopeFor(t, ws, "get", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("profile.get error = %v", resp.Error.Message)
	}
	var got store.AgentProfile
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode profile.get result: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name {
		t.Fatalf("profile.get result = %+v, want %+v", got, created)
	}
}

func TestProfile_ListOrdering(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "a", "profile.create", map[string]any{"name": "first", "provider": "claude-native"})
	firstResp := readEnvelopeFor(t, ws, "a", 5*time.Second)
	sendRequest(t, ws, "b", "profile.create", map[string]any{"name": "second", "provider": "glm"})
	secondResp := readEnvelopeFor(t, ws, "b", 5*time.Second)

	var first, second store.AgentProfile
	mustDecode(t, firstResp.Result, &first)
	mustDecode(t, secondResp.Result, &second)

	sendRequest(t, ws, "list", "profile.list", nil)
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("profile.list error = %v", resp.Error.Message)
	}
	var list []store.AgentProfile
	mustDecode(t, resp.Result, &list)
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("profile.list = %+v, want [%+v %+v]", list, first, second)
	}
}

func TestProfile_UpdateVisibleFromSecondConnection(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	wsA := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsA.Close() })
	wsB := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = wsB.Close() })

	sendRequest(t, wsA, "create", "profile.create", map[string]any{"name": "old", "provider": "claude-native"})
	var created store.AgentProfile
	mustDecode(t, readEnvelopeFor(t, wsA, "create", 5*time.Second).Result, &created)

	sendRequest(t, wsA, "update", "profile.update", map[string]any{
		"id": created.ID, "name": "new", "provider": "glm", "approvalPolicy": "auto-safe",
	})
	if resp := readEnvelopeFor(t, wsA, "update", 5*time.Second); resp.Error != nil {
		t.Fatalf("profile.update error = %v", resp.Error.Message)
	}

	sendRequest(t, wsB, "get", "profile.get", map[string]any{"id": created.ID})
	resp := readEnvelopeFor(t, wsB, "get", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("profile.get error = %v", resp.Error.Message)
	}
	var got store.AgentProfile
	mustDecode(t, resp.Result, &got)
	if got.Name != "new" || got.Provider != "glm" || got.ApprovalPolicy != "auto-safe" {
		t.Fatalf("profile.get from second connection = %+v, want updated fields", got)
	}
}

func TestProfile_DeleteThenGetErrors(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "profile.create", map[string]any{"name": "throwaway", "provider": "claude-native"})
	var created store.AgentProfile
	mustDecode(t, readEnvelopeFor(t, ws, "create", 5*time.Second).Result, &created)

	sendRequest(t, ws, "delete", "profile.delete", map[string]any{"id": created.ID})
	if resp := readEnvelopeFor(t, ws, "delete", 5*time.Second); resp.Error != nil {
		t.Fatalf("profile.delete error = %v", resp.Error.Message)
	}

	sendRequest(t, ws, "get", "profile.get", map[string]any{"id": created.ID})
	resp := readEnvelopeFor(t, ws, "get", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("profile.get after delete error = nil, want a not-found error")
	}
}

func TestProfile_CreateMissingNameIsInvalidParamsError(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "profile.create", map[string]any{"provider": "claude-native"})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("profile.create with no name error = nil, want error")
	}
}

func TestProfile_CreateUnknownProviderErrors(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "create", "profile.create", map[string]any{"name": "x", "provider": "not-a-real-provider"})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error == nil {
		t.Fatalf("profile.create with unknown provider error = nil, want error")
	}
	if resp.Error.Message == "" {
		t.Fatalf("profile.create error message is empty, want it to name the bad provider")
	}
}

func mustDecode(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode result: %v", err)
	}
}
