package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// identEnv is a daemon with one running "hang" run (the seat) on seatTask.
type identEnv struct {
	api      *API
	srv      *httptest.Server
	wm       *workspace.Manager
	db       *store.Store
	seatTask store.Task
	runID    string
	chatID   int64
	admin    *websocket.Conn // plain, unidentified
}

func newIdentEnv(t *testing.T, scenario string) *identEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	admin := dialWS(t, srv, "tok")
	started := startRun(t, admin, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "seat"})
	row, err := db.GetRun(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return &identEnv{api: api, srv: srv, wm: wm, db: db, seatTask: task, runID: started.RunID, chatID: row.ChatID, admin: admin}
}

func (e *identEnv) identified(t *testing.T) *websocket.Conn {
	t.Helper()
	ws := dialWS(t, e.srv, "tok")
	resp := callWS(t, ws, "session.identify", map[string]any{"runId": e.runID})
	if resp.Error != nil {
		t.Fatalf("session.identify error = %v", resp.Error)
	}
	return ws
}

func wantForbidden(t *testing.T, resp envelope, substr string) {
	t.Helper()
	if resp.Error == nil || resp.Error.Code != "forbidden" || !strings.Contains(resp.Error.Message, substr) {
		t.Fatalf("response = %+v / %+v, want forbidden %q", resp.Result, resp.Error, substr)
	}
}

func wantOK(t *testing.T, resp envelope, what string) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("%s error = %+v", what, resp.Error)
	}
}

func TestSessionIdentify_BindsConnToRun(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := dialWS(t, e.srv, "tok")
	// Spoofed ids in params are ignored: the registry's win.
	resp := callWS(t, ws, "session.identify", map[string]any{"runId": e.runID, "taskId": 999, "chatId": 998})
	wantOK(t, resp, "session.identify")
	var got struct {
		RunID  string `json:"runId"`
		TaskID int64  `json:"taskId"`
		ChatID int64  `json:"chatId"`
	}
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.RunID != e.runID || got.TaskID != e.seatTask.ID || got.ChatID != e.chatID {
		t.Fatalf("identify = %+v, want run %s task %d chat %d", got, e.runID, e.seatTask.ID, e.chatID)
	}
	// Same id again is fine; another run is an error.
	wantOK(t, callWS(t, ws, "session.identify", map[string]any{"runId": e.runID}), "second identify")
}

func TestSessionIdentify_UnknownOrFinishedRunRejected(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "reply")
	waitRunDone(t, e.admin, e.runID)
	ws := dialWS(t, e.srv, "tok")
	resp := callWS(t, ws, "session.identify", map[string]any{"runId": e.runID})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "run is not running") {
		t.Fatalf("finished run: %+v", resp.Error)
	}
	resp = callWS(t, ws, "session.identify", map[string]any{"runId": "no-such-run"})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "unknown run") {
		t.Fatalf("unknown run: %+v", resp.Error)
	}
	// The failed attempts left the conn unidentified: profile.create works.
	wantOK(t, callWS(t, ws, "profile.create", map[string]any{"name": "p", "provider": "glm"}), "profile.create")
}

func TestSessionIdentify_SecondRunIdRejected(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	task2 := newTestTask(t, e.wm, "hang")
	r2 := startRun(t, e.admin, map[string]any{"taskId": task2.ID, "provider": "glm", "prompt": "two"})
	resp := callWS(t, ws, "session.identify", map[string]any{"runId": r2.RunID})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "already identified") {
		t.Fatalf("second identify = %+v", resp.Error)
	}
}

func TestSessionIdentify_ForbiddenOnRelaySession(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.api.ServeTransport(ctx, server)
	defer client.Close()
	data, _ := json.Marshal(envelope{ID: "i", Method: "session.identify", Params: marshalOrNull(map[string]any{"runId": e.runID})})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	raw, err := client.Receive()
	if err != nil {
		t.Fatal(err)
	}
	var resp envelope
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	wantForbidden(t, resp, "relay")
}

func TestRunScoped_MethodAllowlistFailsClosed(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	for _, m := range []string{"profile.create", "mcp.create", "workspace.setScripts", "account.add", "relay.device.list", "terminal.create"} {
		wantForbidden(t, callWS(t, ws, m, map[string]any{}), "not available to agents")
	}
	for _, m := range []string{"task.list", "run.list", "provider.list"} {
		wantOK(t, callWS(t, ws, m, map[string]any{}), m)
	}
}

func TestRunScoped_AllowlistedMethodsStillWork(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	resp := callWS(t, ws, "task.create", map[string]any{"workspaceId": e.seatTask.WorkspaceID, "title": "child"})
	wantOK(t, resp, "task.create")
	var child store.Task
	if err := json.Unmarshal(resp.Result, &child); err != nil {
		t.Fatal(err)
	}
	startRun(t, ws, map[string]any{"taskId": child.ID, "provider": "glm", "prompt": "work", "source": "orchestrator"})
}

func TestRunScoped_DiesWithRun(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	wantOK(t, callWS(t, ws, "task.list", map[string]any{}), "task.list while running")
	wantOK(t, callWS(t, e.admin, "run.stop", map[string]any{"runId": e.runID}), "run.stop")
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := callWS(t, ws, "task.list", map[string]any{})
		if resp.Error != nil {
			wantForbidden(t, resp, "your run has ended")
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("identified conn still served after its run was stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func createChild(t *testing.T, ws *websocket.Conn, e *identEnv, extra map[string]any) envelope {
	t.Helper()
	params := map[string]any{"workspaceId": e.seatTask.WorkspaceID, "title": "child"}
	for k, v := range extra {
		params[k] = v
	}
	return callWS(t, ws, "task.create", params)
}

func parentOf(t *testing.T, resp envelope) int64 {
	t.Helper()
	var out struct {
		ParentTaskID *int64 `json:"ParentTaskID"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.ParentTaskID == nil {
		return 0
	}
	return *out.ParentTaskID
}

func TestTaskCreate_ParentDefaultsToIdentity(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	resp := createChild(t, ws, e, nil)
	wantOK(t, resp, "task.create")
	if got := parentOf(t, resp); got != e.seatTask.ID {
		t.Fatalf("parent = %d, want %d (raw %s)", got, e.seatTask.ID, resp.Result)
	}
	// Explicit own id is fine.
	wantOK(t, createChild(t, ws, e, map[string]any{"parentTaskId": e.seatTask.ID}), "task.create(own parent)")
	// Unidentified: no parent, as before.
	if got := parentOf(t, createChild(t, e.admin, e, nil)); got != 0 {
		t.Fatalf("unidentified parent = %d, want none", got)
	}
}

func TestTaskCreate_ForeignParentRejectedWhenIdentified(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	other := newTestTask(t, e.wm, "")
	resp := createChild(t, ws, e, map[string]any{"parentTaskId": other.ID})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "parentTaskId must be your own task #") {
		t.Fatalf("error = %+v", resp.Error)
	}
	// An unidentified caller may still pick any parent.
	wantOK(t, createChild(t, e.admin, e, map[string]any{"parentTaskId": e.seatTask.ID}), "unidentified task.create")
}

func runRow(t *testing.T, e *identEnv, resp envelope) store.Run {
	t.Helper()
	wantOK(t, resp, "run.start")
	var out runStartResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatal(err)
	}
	row, err := e.db.GetRun(out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func childOfSeat(t *testing.T, e *identEnv) store.Task {
	t.Helper()
	c, err := e.wm.CreateTask(e.seatTask.WorkspaceID, nil, "Child", workspace.WithParentTask(e.seatTask.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*c.WorktreePath, "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatal(err)
	}
	return c
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func TestRunStart_FromIdsFilledFromIdentity(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	child := childOfSeat(t, e)
	// No from ids and the orchestrator source: upgraded to agent, ids filled.
	row := runRow(t, e, callWS(t, ws, "run.start", map[string]any{
		"taskId": child.ID, "provider": "glm", "prompt": "work", "source": "orchestrator",
	}))
	if row.ReplyChatID != e.chatID {
		t.Fatalf("ReplyChatID = %d, want the caller's chat %d", row.ReplyChatID, e.chatID)
	}
	// The queued delivery path records the provenance: queue behind the same chat.
	resp := callWS(t, ws, "run.start", map[string]any{
		"taskId": child.ID, "chatId": row.ChatID, "provider": "glm", "prompt": "more", "source": "orchestrator", "whenBusy": "queue",
	})
	wantOK(t, resp, "queued run.start")
	items, err := e.db.ListChatQueue(row.ChatID)
	if err != nil || len(items) != 1 {
		t.Fatalf("queue = %+v, %v", items, err)
	}
	if it := items[0]; it.Source != store.ChatQueueSourceAgent || ptrVal(it.FromTaskID) != e.seatTask.ID || ptrVal(it.FromChatID) != e.chatID {
		t.Fatalf("queue item = %+v, want agent from task %d chat %d", it, e.seatTask.ID, e.chatID)
	}
}

func TestRunStart_ForeignFromIdsRejectedWhenIdentified(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	child := childOfSeat(t, e)
	for name, extra := range map[string]map[string]any{
		"task": {"fromTaskId": e.seatTask.ID + 100},
		"chat": {"fromChatId": e.chatID + 100},
	} {
		params := map[string]any{"taskId": child.ID, "provider": "glm", "prompt": "w", "source": "agent"}
		for k, v := range extra {
			params[k] = v
		}
		resp := callWS(t, ws, "run.start", params)
		if resp.Error == nil || !strings.Contains(resp.Error.Message, "fromTaskId/fromChatId must be your own") {
			t.Fatalf("%s: error = %+v", name, resp.Error)
		}
	}
}

func TestRunStart_ProfileSendKeepsHumanSourceWithIdentityIds(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	child := childOfSeat(t, e)
	first := runRow(t, e, callWS(t, ws, "run.start", map[string]any{"taskId": child.ID, "provider": "glm", "prompt": "a", "source": "human"}))
	wantOK(t, callWS(t, ws, "run.start", map[string]any{
		"taskId": child.ID, "chatId": first.ChatID, "provider": "glm", "prompt": "b", "source": "human", "whenBusy": "queue",
	}), "queued human run.start")
	items, err := e.db.ListChatQueue(first.ChatID)
	if err != nil || len(items) != 1 {
		t.Fatalf("queue = %+v, %v", items, err)
	}
	if it := items[0]; it.Source != store.ChatQueueSourceHuman || ptrVal(it.FromTaskID) != e.seatTask.ID || ptrVal(it.FromChatID) != e.chatID {
		t.Fatalf("queue item = %+v, want human with identity ids", it)
	}
}

func TestUnidentifiedConn_BehaviourUnchanged(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	wantOK(t, callWS(t, e.admin, "profile.create", map[string]any{"name": "p1", "provider": "glm"}), "profile.create")
	wantOK(t, callWS(t, e.admin, "mcp.list", nil), "mcp.list")
	child := childOfSeat(t, e)
	// A foreign from id is accepted without an identity.
	resp := callWS(t, e.admin, "run.start", map[string]any{
		"taskId": child.ID, "provider": "glm", "prompt": "w", "source": "agent", "fromTaskId": 12345, "fromChatId": 54321,
	})
	wantOK(t, resp, "unidentified run.start")
}
