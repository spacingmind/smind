package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

const revealBody = "plain SECRET_TOKEN_123 line\nuse ghp_abcdefghijklmnopqrstuvwxyz0123456789 here\n"

// fakeHeredocs serves one pending permission "perm-1" and one tool call
// run-1/call-1, the way *runs.Registry does (pending only while pending).
type fakeHeredocs struct{ pending bool }

func (f *fakeHeredocs) PendingHeredoc(id string, i int) (runs.HeredocReveal, error) {
	if !f.pending || id != "perm-1" || i != 0 {
		return runs.HeredocReveal{}, runs.ErrNoHeredoc
	}
	return runs.HeredocReveal{RunID: "run-1", TaskID: 7, Text: revealBody}, nil
}

func (f *fakeHeredocs) ToolCallHeredoc(run, call string, i int) (runs.HeredocReveal, error) {
	if run != "run-1" || call != "call-1" || i != 0 {
		return runs.HeredocReveal{}, runs.ErrNoHeredoc
	}
	return runs.HeredocReveal{RunID: run, Text: revealBody}, nil
}

func revealCall(t *testing.T, h handlerFunc, rc *requestContext, params any) (heredocRevealResult, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := h(context.Background(), rc, raw)
	if err != nil {
		return heredocRevealResult{}, err
	}
	return out.(heredocRevealResult), nil
}

func TestPermissionRevealHeredoc_RedactsAndRefusesWhenNotPending(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	src := &fakeHeredocs{pending: true}
	h := heredocRevealHandlers(src, db)["permission.revealHeredoc"]
	human := &requestContext{local: true}

	res, err := revealCall(t, h, human, map[string]any{"permissionId": "perm-1", "index": 0})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") || !res.Redacted || !strings.Contains(res.Text, "SECRET_TOKEN_123") || res.Bytes != len(revealBody) {
		t.Errorf("result = %+v", res)
	}
	_, err = revealCall(t, h, human, map[string]any{"permissionId": "perm-1", "index": 5})
	wantErrCode(t, err, codeNotFound)
	src.pending = false
	_, err = revealCall(t, h, human, map[string]any{"permissionId": "perm-1", "index": 0})
	wantErrCode(t, err, codeNotFound)
}

func TestRunRevealToolHeredoc_RedactsAndRefusesUnknown(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	h := heredocRevealHandlers(&fakeHeredocs{}, db)["run.revealToolHeredoc"]
	human := &requestContext{local: true}
	res, err := revealCall(t, h, human, map[string]any{"runId": "run-1", "toolCallId": "call-1", "index": 0})
	if err != nil || strings.Contains(res.Text, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") || !res.Redacted {
		t.Fatalf("result = %+v err %v", res, err)
	}
	for _, bad := range []map[string]any{
		{"runId": "run-1", "toolCallId": "nope", "index": 0},
		{"runId": "run-1", "toolCallId": "call-1", "index": 3},
		{"runId": "run-9", "toolCallId": "call-1", "index": 0},
	} {
		_, err := revealCall(t, h, human, bad)
		wantErrCode(t, err, codeNotFound)
	}
	_, err = revealCall(t, h, human, map[string]any{"runId": "run-1"})
	wantErrCode(t, err, codeInvalid)
}

func TestHeredocReveal_RefusesRunScopedCallers(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	hs := heredocRevealHandlers(&fakeHeredocs{pending: true}, db)
	for name, params := range map[string]map[string]any{
		"permission.revealHeredoc": {"permissionId": "perm-1", "index": 0},
		"run.revealToolHeredoc":    {"runId": "run-1", "toolCallId": "call-1", "index": 0},
	} {
		_, err := revealCall(t, hs[name], &requestContext{runScoped: true}, params)
		wantErrCode(t, err, codeForbidden)
		_, err = revealCall(t, hs[name], nil, params)
		wantErrCode(t, err, codeForbidden)
	}
}

// Neither reveal may be reachable from an agent connection: not on
// RunScopedMethods, both registered, and the walk test keeps it that way.
func TestHeredocReveal_NotRunScopedAndRegistered(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	for _, m := range heredocRevealMethods {
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("%s is not registered", m)
		}
		for _, rs := range RunScopedMethods {
			if rs == m {
				t.Errorf("%s must not be run-scoped", m)
			}
		}
	}
	ws := e.identified(t)
	resp := callWS(t, ws, "permission.revealHeredoc", map[string]any{"permissionId": "x", "index": 0})
	if resp.Error == nil {
		t.Fatal("an identified agent connection reached permission.revealHeredoc")
	}
}

func TestHeredocReveal_AuditRowIsContentFree(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	hs := heredocRevealHandlers(&fakeHeredocs{pending: true}, db)
	human := &requestContext{local: true}
	if _, err := revealCall(t, hs["permission.revealHeredoc"], human, map[string]any{"permissionId": "perm-1", "index": 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := revealCall(t, hs["run.revealToolHeredoc"], human, map[string]any{"runId": "run-1", "toolCallId": "call-1", "index": 0}); err != nil {
		t.Fatal(err)
	}
	// A refused reveal writes nothing.
	_, _ = revealCall(t, hs["permission.revealHeredoc"], human, map[string]any{"permissionId": "nope", "index": 0})
	rows, err := db.ListPolicyAudit(store.PolicyAuditFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		if r.Decision != "heredoc_revealed" || !strings.HasSuffix(r.Tool, ".heredoc_revealed") ||
			strings.Contains(string(raw), "SECRET_TOKEN_123") || strings.Contains(string(raw), "ghp_") {
			t.Errorf("audit row = %s", raw)
		}
	}
}

// Fail closed: a reveal whose audit row cannot be written is refused, and
// neither the result nor the error carries the body.
func TestHeredocReveal_RefusedWhenAuditFails(t *testing.T) {
	t.Parallel()
	_, db := newTestWorkspaceManager(t)
	hs := heredocRevealHandlers(&fakeHeredocs{pending: true}, db)
	if err := db.Close(); err != nil { // every later AppendPolicyAudit fails
		t.Fatal(err)
	}
	human := &requestContext{local: true}
	for name, params := range map[string]map[string]any{
		"permission.revealHeredoc": {"permissionId": "perm-1", "index": 0},
		"run.revealToolHeredoc":    {"runId": "run-1", "toolCallId": "call-1", "index": 0},
	} {
		res, err := revealCall(t, hs[name], human, params)
		if err == nil {
			t.Fatalf("%s: reveal succeeded without an audit row: %+v", name, res)
		}
		raw, _ := json.Marshal(res)
		for _, leak := range []string{"SECRET_TOKEN_123", "ghp_", "plain "} {
			if strings.Contains(string(raw), leak) || strings.Contains(err.Error(), leak) {
				t.Errorf("%s: body leaked through %q: result %s, error %v", name, leak, raw, err)
			}
		}
		if res.Text != "" {
			t.Errorf("%s: result text = %q, want empty", name, res.Text)
		}
	}
}
