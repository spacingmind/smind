package wsapi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/store"
)

// The three saved-grant methods are human-only: handlers exist, none is
// reachable from a plugin, none is on RunScopedMethods, an identified (agent)
// connection is refused before any handler runs, a run-scoped request context
// is refused inside each handler, and each name is on the floor's table so a
// shell command naming one next to a transport is denied.
func TestWsapi_GrantMethodsAreHumanOnly(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range permissionGrantMethods {
		h, ok := e.api.hs[m]
		if !ok {
			t.Fatalf("%s is not a wsapi method", m)
		}
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is on RunScopedMethods", m)
		}
		if !pluginNever(m) {
			t.Errorf("%s is reachable from a plugin session", m)
		}
		if !slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s is not on permpolicy.HumanOnlyRPCs", m)
		}
		for capName, ms := range PluginCapabilityTable {
			for _, pm := range ms {
				if pm.Name == m {
					t.Errorf("%s is granted by plugin capability %q", m, capName)
				}
			}
		}
		// A run-scoped request context (a task an agent started) is refused inside the handler.
		_, err := h(context.Background(), &requestContext{runScoped: true}, json.RawMessage(`{"runId":"r","requestId":"q","optionId":"o","scope":"project","grantId":1}`))
		if err == nil {
			t.Errorf("%s accepted a run-scoped caller", m)
		}
		// So is a nil context.
		if _, err := h(context.Background(), nil, json.RawMessage(`{}`)); err == nil {
			t.Errorf("%s accepted a nil context", m)
		}
	}
}

func TestWsapi_GrantMethodsRefusedForRunScoped(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	for _, m := range permissionGrantMethods {
		wantForbidden(t, callWS(t, ws, m, map[string]any{"runId": e.runID, "requestId": "x", "optionId": "allow", "scope": "project", "grantId": 1}), "not available to agents")
	}
}

func TestWsapi_GrantListAndRevoke(t *testing.T) {
	e := newProcEnv(t)
	now := time.Now()
	g, err := e.db.SaveGrant(store.PermissionGrant{WorkspaceID: e.task.WorkspaceID, CommandSHA: "aa", CommandLen: 5, CommandDisplay: "make build", Folder: "/repo", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), CreatedBy: "local"})
	if err != nil {
		t.Fatal(err)
	}
	// The expired one lists as expired, the live one as void here (its stored fingerprint "" is not the project's current one).
	expired, _ := e.db.SaveGrant(store.PermissionGrant{WorkspaceID: e.task.WorkspaceID, CommandSHA: "bb", CommandLen: 5, CommandDisplay: "make old", CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), CreatedBy: "local"})
	rc := &requestContext{local: true}
	res, err := e.api.hs["permission.grants.list"](context.Background(), rc, json.RawMessage(`null`))
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Grants []struct {
			ID      int64  `json:"id"`
			Status  string `json:"status"`
			Command string `json:"command"`
			Sha     string `json:"commandSha"`
		} `json:"grants"`
	}
	b, _ := json.Marshal(res)
	_ = json.Unmarshal(b, &list)
	if len(list.Grants) != 2 {
		t.Fatalf("grants = %s", b)
	}
	if strings.Contains(string(b), "commandSha") || strings.Contains(string(b), "commandLen") || strings.Contains(string(b), "mcpFingerprint") {
		t.Errorf("the list leaks the match key: %s", b)
	}
	for _, v := range list.Grants {
		if v.ID == expired.ID && v.Status != "expired" {
			t.Errorf("expired grant status = %s", v.Status)
		}
		// Its stored MCP fingerprint is not the project's current one, so it is void, not active.
		if v.ID == g.ID && v.Status != "void" {
			t.Errorf("a grant whose MCP fingerprint differs reads %q, want void", v.Status)
		}
	}
	out, err := e.api.hs["permission.grants.revoke"](context.Background(), rc, json.RawMessage(`{"grantId":`+jsonInt(g.ID)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if ob, _ := json.Marshal(out); string(ob) != `{"revoked":true}` {
		t.Fatalf("revoke = %s", ob)
	}
	if got, _ := e.db.GetGrant(g.ID); got.RevokedAt == nil || got.RevokedBy != "local" {
		t.Fatalf("grant after revoke = %+v", got)
	}
	if _, err := e.api.hs["permission.grants.revoke"](context.Background(), rc, json.RawMessage(`{"grantId":9999}`)); err == nil {
		t.Fatal("revoking a missing grant succeeded")
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }
