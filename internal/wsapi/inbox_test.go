package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/workspace"
)

func inboxEnv(t *testing.T) (*identEnv, string) {
	t.Helper()
	e := newIdentEnv(t, "hang")
	since := time.Unix(1_700_000_000, 0)
	e.api.SetStalledSource(func() map[int64]time.Time { return map[int64]time.Time{e.seatTask.ID: since} })
	return e, "stall:" + itoa64(e.seatTask.ID) + ":" + itoa64(since.Unix())
}

func itoa64(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{digits[n%10]}, b...)
	}
	return string(b)
}

func TestInboxReads_OpenOverRelay(t *testing.T) {
	t.Parallel()
	e, key := inboxEnv(t)
	relay := &requestContext{local: false}
	raw, err := budgetCall(t, e.api, relay, "inbox.list", map[string]any{})
	if err != nil || !strings.Contains(string(raw), key) {
		t.Fatalf("relay inbox.list = %s, %v", raw, err)
	}
	raw, err = budgetCall(t, e.api, relay, "inbox.count", map[string]any{})
	if err != nil || !strings.Contains(string(raw), `"total":1`) {
		t.Fatalf("relay inbox.count = %s, %v", raw, err)
	}
}

func TestInboxSetters_AllowedOverRelay(t *testing.T) {
	t.Parallel()
	e, key := inboxEnv(t)
	relay := &requestContext{local: false, deviceID: "dev1"}
	if _, err := budgetCall(t, e.api, relay, "inbox.seen", map[string]any{"keys": []string{key}}); err != nil {
		t.Fatalf("seen over relay: %v", err)
	}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := budgetCall(t, e.api, relay, "inbox.snooze", map[string]any{"keys": []string{key}, "until": until}); err != nil {
		t.Fatalf("snooze over relay: %v", err)
	}
	if _, err := budgetCall(t, e.api, relay, "inbox.resolve", map[string]any{"keys": []string{key}}); err != nil {
		t.Fatalf("resolve over relay: %v", err)
	}
	st, _ := e.db.ListInboxStates()
	if len(st) != 1 || st[0].State != "resolved" || st[0].Actor != "device:dev1" {
		t.Fatalf("states = %+v", st)
	}
}

func TestInboxSetters_RefusedForRunScoped(t *testing.T) {
	t.Parallel()
	e, key := inboxEnv(t)
	agent := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, m := range inboxSetters {
		_, err := budgetCall(t, e.api, agent, m, map[string]any{"keys": []string{key}, "until": until})
		wantErrCode(t, err, codeForbidden)
	}
	for _, m := range inboxMethods { // reads too: the inbox is the Human's console
		_, err := budgetCall(t, e.api, agent, m, map[string]any{})
		wantErrCode(t, err, codeForbidden)
	}
	// an identified connection never reaches a handler: not in RunScopedMethods
	ws := e.identified(t)
	for _, m := range inboxMethods {
		wantForbidden(t, callWS(t, ws, m, map[string]any{"keys": []string{key}}), "not available to agents")
	}
	if st, _ := e.db.ListInboxStates(); len(st) != 0 {
		t.Fatalf("an agent changed inbox state: %+v", st)
	}
}

func TestInboxSetters_RefusedForPermissionKeys(t *testing.T) {
	t.Parallel()
	e, _ := inboxEnv(t)
	human := &requestContext{local: true}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	_, err := budgetCall(t, e.api, human, "inbox.snooze", map[string]any{"keys": []string{"perm:abc"}, "until": until})
	wantErrCode(t, err, codeInvalid)
	_, err = budgetCall(t, e.api, human, "inbox.resolve", map[string]any{"keys": []string{"perm:abc"}})
	wantErrCode(t, err, codeInvalid)
	_, err = budgetCall(t, e.api, human, "inbox.snooze", map[string]any{"keys": []string{"stall:1:1"}, "until": "tomorrow"})
	wantErrCode(t, err, codeInvalid)
}

func TestNoInboxApproveAllMethod(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	allowed := map[string]bool{}
	for _, m := range inboxMethods {
		allowed[m] = true
		if runScopedSet[m] {
			t.Errorf("%s must not be run-scoped", m)
		}
		if e.api.hs[m] == nil {
			t.Errorf("%s is not registered", m)
		}
	}
	for name := range e.api.hs {
		if strings.HasPrefix(name, "inbox.") && !allowed[name] {
			t.Errorf("unexpected inbox RPC %q", name)
		}
		if strings.HasPrefix(name, "inbox.") {
			for _, bad := range []string{"approve", "merge", "all", "respond", "allow"} {
				if strings.Contains(name, bad) {
					t.Errorf("inbox RPC %q must not %s", name, bad)
				}
			}
		}
	}
}

func TestInboxChanged_CoalescedOnPermissionVerdictStall(t *testing.T) {
	t.Parallel()
	e, key := inboxEnv(t)
	var n atomic.Int32
	e.api.bus.setTap(func(ev Event) {
		if ev.Topic == TopicInboxChanged {
			n.Add(1)
		}
	})
	waitCount := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && n.Load() < want {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(inboxDebounce * 3) // anything extra would show up now
		if got := n.Load(); got != want {
			t.Fatalf("inbox.changed count = %d, want %d", got, want)
		}
	}

	// first look: a stalled seat exists -> one event
	e.api.inbox.Poke()
	waitCount(1)
	// nothing changed: a poke publishes nothing
	e.api.inbox.Poke()
	waitCount(1)
	// two setters back to back coalesce into one event
	human := &requestContext{local: true}
	if _, err := budgetCall(t, e.api, human, "inbox.seen", map[string]any{"keys": []string{key}}); err != nil {
		t.Fatal(err)
	}
	e.api.inbox.Poke()
	waitCount(2)
	// the stall ends (no event exists for that): the next check sees it
	e.api.SetStalledSource(func() map[int64]time.Time { return nil })
	e.api.inbox.Poke()
	waitCount(3)
}

// TestInboxList_LivePermission_AnsweredThroughExistingRPC: a real pending
// permission appears in the inbox (redacted), cannot be snoozed or resolved
// there, and leaves it when answered through the unchanged run.respondPermission.
func TestInboxList_LivePermission_AnsweredThroughExistingRPC(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	permEnv := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if permEnv.Event != "permission_request" {
		t.Fatalf("first message = %+v", permEnv)
	}
	var req permissionRequestParams
	if err := json.Unmarshal(permEnv.Params, &req); err != nil {
		t.Fatal(err)
	}

	human := &requestContext{local: true}
	raw, err := budgetCall(t, api, human, "inbox.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Items []struct {
			Key, Kind, Summary string
			Links              struct{ RunID, PermissionRequestID string }
		}
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Key != "perm:"+req.RequestID || listed.Items[0].Kind != "permission" {
		t.Fatalf("inbox = %s", raw)
	}
	if strings.Contains(listed.Items[0].Summary, "risky") {
		t.Fatalf("the request's own text must not reach the inbox summary: %q", listed.Items[0].Summary)
	}
	if c, _ := budgetCall(t, api, human, "inbox.count", map[string]any{}); !strings.Contains(string(c), `"unseen":1`) {
		t.Fatalf("count = %s", c)
	}
	_, err = budgetCall(t, api, human, "inbox.resolve", map[string]any{"keys": []string{"perm:" + req.RequestID}})
	wantErrCode(t, err, codeInvalid)

	// acting on it is the existing, guarded RPC
	if _, err := budgetCall(t, api, human, "run.respondPermission", map[string]any{"runId": listed.Items[0].Links.RunID, "requestId": req.RequestID, "optionId": "allow-1"}); err != nil {
		t.Fatalf("run.respondPermission: %v", err)
	}
	raw, _ = budgetCall(t, api, human, "inbox.list", map[string]any{})
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("an answered permission must leave the inbox: %s", raw)
	}
}

func TestInboxList_VerdictFromStoredValidation(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	svc := newValidationService(e.db, nil)
	rec, err := svc.SubmitContract(e.seatTask.ID, []byte(vContractDoc))
	if err != nil {
		t.Fatal(err)
	}
	head, err := workspace.GitHead(context.Background(), *e.seatTask.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	val, _, err := svc.CreateValidation(e.seatTask.ID, rec.ID, head)
	if err != nil {
		t.Fatal(err)
	}
	human := &requestContext{local: true}
	if raw, _ := budgetCall(t, e.api, human, "inbox.list", map[string]any{}); strings.Contains(string(raw), "verdict:") {
		t.Fatalf("a running validation has no verdict and is not an inbox item: %s", raw)
	}
	if err := svc.WriteBlocked(val.ID, "timeout", "ran out of time"); err != nil {
		t.Fatal(err)
	}
	raw, _ := budgetCall(t, e.api, human, "inbox.list", map[string]any{})
	if !strings.Contains(string(raw), "verdict:"+itoa64(val.ID)) || !strings.Contains(string(raw), "blocked: timeout") {
		t.Fatalf("inbox = %s", raw)
	}
	// a human resolves it; it stays resolved while it derives, and is hidden
	if _, err := budgetCall(t, e.api, human, "inbox.resolve", map[string]any{"keys": []string{"verdict:" + itoa64(val.ID)}}); err != nil {
		t.Fatal(err)
	}
	raw, _ = budgetCall(t, e.api, human, "inbox.list", map[string]any{})
	if strings.Contains(string(raw), "verdict:") {
		t.Fatalf("a resolved verdict must be hidden: %s", raw)
	}
}

// A verdict for a commit the branch has moved past is stale and drops out of the inbox.
func TestInboxList_StaleVerdictDropsOut_E2E(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	svc := newValidationService(e.db, nil)
	rec, _ := svc.SubmitContract(e.seatTask.ID, []byte(vContractDoc))
	val, _, err := svc.CreateValidation(e.seatTask.ID, rec.ID, strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteBlocked(val.ID, "crashed", "x"); err != nil {
		t.Fatal(err)
	}
	raw, _ := budgetCall(t, e.api, &requestContext{local: true}, "inbox.list", map[string]any{})
	if strings.Contains(string(raw), "verdict:") {
		t.Fatalf("a verdict for another commit is stale and must not be listed: %s", raw)
	}
}
