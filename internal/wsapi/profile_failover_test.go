package wsapi

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/store"
)

func profileCall(t *testing.T, ws *websocket.Conn, id, method string, params map[string]any) (store.AgentProfile, string) {
	t.Helper()
	sendRequest(t, ws, id, method, params)
	resp := readEnvelopeFor(t, ws, id, 5*time.Second)
	if resp.Error != nil {
		return store.AgentProfile{}, resp.Error.Message
	}
	var p store.AgentProfile
	mustDecode(t, resp.Result, &p)
	return p, ""
}

func newProfileFailoverWS(t *testing.T) *websocket.Conn {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func TestProfileCreate_FailoverFields(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	target, errMsg := profileCall(t, ws, "a", "profile.create", map[string]any{"name": "target", "provider": "glm"})
	if errMsg != "" {
		t.Fatalf("create target: %s", errMsg)
	}
	created, errMsg := profileCall(t, ws, "b", "profile.create", map[string]any{
		"name": "main", "provider": "claude-native", "onRateLimit": "fallback", "fallbackProfileId": target.ID,
	})
	if errMsg != "" {
		t.Fatalf("create main: %s", errMsg)
	}
	if created.OnRateLimit != "fallback" || created.FallbackProfileID == nil || *created.FallbackProfileID != target.ID {
		t.Fatalf("created = %q / %v", created.OnRateLimit, created.FallbackProfileID)
	}
	got, errMsg := profileCall(t, ws, "c", "profile.get", map[string]any{"id": created.ID})
	if errMsg != "" || got.OnRateLimit != "fallback" || got.FallbackProfileID == nil || *got.FallbackProfileID != target.ID {
		t.Fatalf("get = %+v, %q", got, errMsg)
	}
	plain, _ := profileCall(t, ws, "d", "profile.create", map[string]any{"name": "plain", "provider": "claude-native"})
	if plain.OnRateLimit != "none" || plain.FallbackProfileID != nil {
		t.Fatalf("default policy = %q / %v, want none / nil", plain.OnRateLimit, plain.FallbackProfileID)
	}
}

func TestProfileUpdate_ClearsFallback(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	target, _ := profileCall(t, ws, "a", "profile.create", map[string]any{"name": "target", "provider": "glm"})
	main, errMsg := profileCall(t, ws, "b", "profile.create", map[string]any{
		"name": "main", "provider": "claude-native", "onRateLimit": "wait", "fallbackProfileId": target.ID,
	})
	if errMsg != "" {
		t.Fatalf("create main: %s", errMsg)
	}
	updated, errMsg := profileCall(t, ws, "c", "profile.update", map[string]any{
		"id": main.ID, "name": "main", "provider": "claude-native", "onRateLimit": "wait", "fallbackProfileId": nil,
	})
	if errMsg != "" {
		t.Fatalf("update: %s", errMsg)
	}
	if updated.OnRateLimit != "wait" || updated.FallbackProfileID != nil {
		t.Fatalf("updated = %q / %v, want wait / nil", updated.OnRateLimit, updated.FallbackProfileID)
	}
	got, _ := profileCall(t, ws, "d", "profile.get", map[string]any{"id": main.ID})
	if got.FallbackProfileID != nil {
		t.Fatalf("stored fallback = %v, want nil", got.FallbackProfileID)
	}
}

func TestProfileCreate_RejectsBadOnRateLimit(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	if _, errMsg := profileCall(t, ws, "a", "profile.create", map[string]any{"name": "x", "provider": "glm", "onRateLimit": "retry"}); errMsg == "" {
		t.Fatalf("bad onRateLimit accepted")
	}
	if _, errMsg := profileCall(t, ws, "b", "profile.create", map[string]any{"name": "x", "provider": "glm", "onRateLimit": "fallback"}); errMsg == "" {
		t.Fatalf("fallback without fallbackProfileId accepted")
	}
}
