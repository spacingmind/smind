package serve

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// storeTasks adapts *store.Store to TaskLookup the way the wsapi source
// does, so the write tests decide on real task and run rows.
type storeTasks struct{ st *store.Store }

func (s storeTasks) GetTask(id int64) (store.Task, error) {
	t, err := s.st.GetTask(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Task{}, fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	return t, err
}

func (s storeTasks) TaskOrigin(id int64) (string, string, error) { return s.st.TaskOrigin(id) }

func (s storeTasks) LatestRun(id int64) (store.Run, bool, error) {
	runs, err := s.st.ListRunsByTask(id, 1)
	if err != nil || len(runs) == 0 {
		return store.Run{}, false, err
	}
	return runs[0], true, nil
}

// grantAuthorizer enforces ops and write workspaces like registry.Authorize,
// so only the grant shape decides a call's fate.
type grantAuthorizer struct{ g registry.Grant }

func (a grantAuthorizer) Authorize(_ string, op string, wsID int64) (registry.Grant, error) {
	notGranted := errors.New("not granted")
	if !a.g.Allows(op) {
		return registry.Grant{}, notGranted
	}
	if op == registry.OpRead {
		if wsID != 0 && !a.g.CanRead(wsID) {
			return registry.Grant{}, notGranted
		}
		return a.g, nil
	}
	if wsID == 0 || !a.g.CanWrite(wsID) {
		return registry.Grant{}, notGranted
	}
	return a.g, nil
}

// fakeBackend performs the write effects against the real store: StartRun
// leaves a running, origin-stamped run row and StopRun finishes it, like the
// wsapi adapter will.
type fakeBackend struct {
	mu           sync.Mutex
	st           *store.Store
	unconfigured map[string]bool
	waiting      map[string]bool
	createCalls  int
	startCalls   int
	stopCalls    []string
	lastPrompt   string
}

func (b *fakeBackend) ProviderConfigured(provider string) bool {
	return b.unconfigured == nil || !b.unconfigured[provider]
}

func (b *fakeBackend) CreateTask(ws int64, title, branch, baseRef string) (TaskInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createCalls++
	t, err := b.st.CreateTask(store.Task{WorkspaceID: ws, Title: title, Status: "created"})
	if err != nil {
		return TaskInfo{}, err
	}
	return TaskInfo{ID: t.ID, Branch: branch}, nil
}

func (b *fakeBackend) SetTaskOrigin(taskID int64, host, trace string) error {
	return b.st.SetTaskOrigin(taskID, host, trace)
}

func (b *fakeBackend) StartRun(taskID int64, provider, model, prompt, originHost string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.startCalls++
	runID := fmt.Sprintf("run-%d", b.startCalls)
	if _, err := b.st.CreateRun(store.Run{ID: runID, TaskID: taskID, Provider: provider, Model: model, Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		return "", err
	}
	if err := b.st.SetRunOrigin(runID, originHost); err != nil {
		return "", err
	}
	b.lastPrompt = prompt
	return runID, nil
}

func (b *fakeBackend) StopRun(runID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopCalls = append(b.stopCalls, runID)
	fin := time.Now().UTC()
	_, err := b.st.UpdateRunStatus(runID, "done", &fin, "", "")
	return err
}

func (b *fakeBackend) WaitingPermission(runID string) bool { return b.waiting[runID] }

func (b *fakeBackend) creates() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.createCalls
}

func (b *fakeBackend) starts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startCalls
}

func (b *fakeBackend) stops() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.stopCalls...)
}

// writeGrant is a human-approved write grant: every op, one write workspace,
// read-all and the ADR budget defaults.
func writeGrant(ws int64, live int) registry.Grant {
	return registry.Grant{
		Ops:             []string{registry.OpRead, registry.OpCreate, registry.OpSend, registry.OpStop},
		WorkspacesRead:  registry.WorkspaceScope{All: true},
		WorkspacesWrite: []int64{ws},
		MaxLiveAgents:   live,
		MaxWallSeconds:  7200,
		MaxCostUSD:      5,
	}
}

// writeServer wires the whole write path: real store ledger, store-backed
// task lookup, fake backend, and (optionally) a reader to answer run.wait.
func writeServer(clk *fakeClock, st *store.Store, g registry.Grant, be *fakeBackend, rd Reader) (*Server, *memAudit) {
	au := &memAudit{}
	if rd == nil {
		rd = &fakeReader{}
	}
	s := New(Options{
		HostID: "A", Authorizer: grantAuthorizer{g: g}, Audit: au, Reader: rd,
		Now: clk.now, Backend: be, Ledger: st, Tasks: storeTasks{st},
		PollInterval: time.Millisecond,
	})
	return s, au
}

// writeReqSeq keeps every test call on a fresh reqId, as the replay cache
// demands.
var writeReqSeq int

func callWrite(t *testing.T, s *Server, clk *fakeClock, principal, method string, extra map[string]any) (any, error) {
	t.Helper()
	writeReqSeq++
	return s.Handle(context.Background(), principal, method,
		rawReq(t, clk, principal, fmt.Sprintf("wreq-%d", writeReqSeq), extra))
}

// callResult is callWrite with the result cast, for the happy paths.
func callResult[T any](t *testing.T, s *Server, clk *fakeClock, principal, method string, extra map[string]any) T {
	t.Helper()
	v, err := callWrite(t, s, clk, principal, method, extra)
	return resultOf[T](t, v, err)
}

func createParams(key string, ws int64) map[string]any {
	return map[string]any{
		"idempotencyKey": key, "workspaceId": ws,
		"provider": "glm", "model": "glm-4.7", "prompt": "do the thing",
	}
}

func wantRefusal(t *testing.T, err error, code, msg string) {
	t.Helper()
	var rf *Refusal
	if !errors.As(err, &rf) {
		t.Fatalf("err = %v, want *Refusal", err)
	}
	if rf.Code != code || (msg != "" && rf.Msg != msg) {
		t.Fatalf("refusal = %q: %q, want %q: %q", rf.Code, rf.Msg, code, msg)
	}
}

func wantRow(t *testing.T, au *memAudit, i int, outcome, code, method, taskID string) store.FleetAuditRow {
	t.Helper()
	if len(au.rows) <= i {
		t.Fatalf("audit rows %d, want at least %d", len(au.rows), i+1)
	}
	r := au.rows[i]
	if r.Outcome != outcome || r.Code != code || r.Method != method || r.TaskID != taskID {
		t.Fatalf("row %d = %+v, want outcome=%s code=%s method=%s taskId=%s", i, r, outcome, code, method, taskID)
	}
	return r
}

func resultOf[T any](t *testing.T, v any, err error) T {
	t.Helper()
	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	res, ok := v.(T)
	if !ok {
		t.Fatalf("result %T, want %T", v, *new(T))
	}
	return res
}

func TestFleetCreateRequiresExplicitProviderModel(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 2), be, nil)
	for name, drop := range map[string]string{"model": "model", "provider": "provider"} {
		p := createParams("k-"+name, ws)
		p[drop] = ""
		_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, p)
		wantRefusal(t, err, wire.CodeInvalid, "")
	}
	if be.creates() != 0 || be.starts() != 0 {
		t.Fatalf("backend touched: creates %d starts %d", be.creates(), be.starts())
	}
	wantRow(t, au, 0, "refused", wire.CodeInvalid, wire.MethodTaskCreate, "")
	wantRow(t, au, 1, "refused", wire.CodeInvalid, wire.MethodTaskCreate, "")
}

func TestFleetCreateProviderNotConfigured(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st, unconfigured: map[string]bool{"glm": true}}
	s, au := writeServer(clk, st, writeGrant(ws, 2), be, nil)
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	wantRefusal(t, err, wire.CodeProviderNotConfigured, "provider is not configured on this host")
	if be.creates() != 0 {
		t.Fatalf("task created %d times", be.creates())
	}
	if _, ok, err := st.GetFleetIdem("B", "k1"); err != nil || ok {
		t.Fatalf("idem row present: ok=%v err=%v", ok, err)
	}
	wantRow(t, au, 0, "refused", wire.CodeProviderNotConfigured, wire.MethodTaskCreate, "")
}

func TestFleetCreateNeedsWriteWorkspace(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	other := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 2), be, nil)
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k-other", other))
	wantRefusal(t, err, wire.CodeNotGranted, "not granted")

	readOnly := writeGrant(ws, 2)
	readOnly.Ops = []string{registry.OpRead}
	s2, au2 := writeServer(clk, st, readOnly, be, nil)
	_, err = callWrite(t, s2, clk, "B", wire.MethodTaskCreate, createParams("k-noop", ws))
	wantRefusal(t, err, wire.CodeNotGranted, "not granted")
	if be.creates() != 0 {
		t.Fatalf("task created %d times", be.creates())
	}
	wantRow(t, au, 0, "refused", wire.CodeNotGranted, wire.MethodTaskCreate, "")
	wantRow(t, au2, 0, "refused", wire.CodeNotGranted, wire.MethodTaskCreate, "")
}

func TestFleetSendOnlyOwnTasks(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 3), be, nil)

	foreign := mkOriginTask(t, st, ws, "C", "tr-c")
	mkOriginRun(t, st, "run-c", foreign, "running", clk.now().UTC(), "C")
	local, err := st.CreateTask(store.Task{WorkspaceID: ws, Title: "local", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		id   int64
	}{{"another host", foreign}, {"local task", local.ID}} {
		_, err := callWrite(t, s, clk, "B", wire.MethodTaskSend, map[string]any{
			"idempotencyKey": "k-" + tc.name, "taskId": tc.id, "prompt": "more",
		})
		wantRefusal(t, err, wire.CodeNotOwner, "task was not created by this host")
	}
	if be.starts() != 0 {
		t.Fatalf("StartRun called %d times", be.starts())
	}
	wantRow(t, au, 0, "refused", wire.CodeNotOwner, wire.MethodTaskSend, fmt.Sprint(foreign))
	wantRow(t, au, 1, "refused", wire.CodeNotOwner, wire.MethodTaskSend, fmt.Sprint(local.ID))

	cr := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k-own", ws))
	sr := callResult[wire.TaskSendResult](t, s, clk, "B", wire.MethodTaskSend, map[string]any{
		"idempotencyKey": "k-send", "taskId": cr.TaskID, "prompt": "go on",
	})
	if sr.TaskID != cr.TaskID || sr.RunID == "" || sr.RunID == cr.RunID || sr.Replayed {
		t.Fatalf("send result %+v after create %+v", sr, cr)
	}
	if be.starts() != 2 {
		t.Fatalf("StartRun called %d times, want 2", be.starts())
	}
	wantRow(t, au, 2, "ok", "", wire.MethodTaskCreate, fmt.Sprint(cr.TaskID))
	wantRow(t, au, 3, "ok", "", wire.MethodTaskSend, fmt.Sprint(cr.TaskID))
}

func TestFleetStopOnlyOwnTasks(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 3), be, nil)

	foreign := mkOriginTask(t, st, ws, "C", "tr-c")
	mkOriginRun(t, st, "run-c", foreign, "running", clk.now().UTC(), "C")
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskStop, map[string]any{"taskId": foreign})
	wantRefusal(t, err, wire.CodeNotOwner, "task was not created by this host")
	if got := be.stops(); len(got) != 0 {
		t.Fatalf("StopRun called: %v", got)
	}
	wantRow(t, au, 0, "refused", wire.CodeNotOwner, wire.MethodTaskStop, fmt.Sprint(foreign))

	cr := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k-stop", ws))
	stop := func(extra map[string]any) wire.TaskStopResult {
		p := map[string]any{"taskId": cr.TaskID}
		for k, v := range extra {
			p[k] = v
		}
		return callResult[wire.TaskStopResult](t, s, clk, "B", wire.MethodTaskStop, p)
	}
	if r := stop(nil); !r.Stopped {
		t.Fatalf("stop live run: %+v", r)
	}
	if got := be.stops(); len(got) != 1 || got[0] != cr.RunID {
		t.Fatalf("StopRun calls %v, want [%s]", got, cr.RunID)
	}
	// No live run left: nothing to stop, and that is not a refusal.
	if r := stop(nil); r.Stopped {
		t.Fatalf("stop without live run: %+v", r)
	}
	// A runId that is not the task's live run is refused.
	_, err = callWrite(t, s, clk, "B", wire.MethodTaskStop, map[string]any{"taskId": cr.TaskID, "runId": "run-c"})
	wantRefusal(t, err, wire.CodeInvalid, "runId does not belong to the task's live run")

	// An explicit runId naming the live run stops it. Only two StopRun
	// calls ever happened: the no-live-run stop made none.
	cr2 := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k-stop2", ws))
	if r := stop(map[string]any{"taskId": cr2.TaskID, "runId": cr2.RunID}); !r.Stopped {
		t.Fatalf("stop by runId: %+v", r)
	}
	if got := be.stops(); len(got) != 2 || got[1] != cr2.RunID {
		t.Fatalf("StopRun calls %v", got)
	}
	wantRow(t, au, 1, "ok", "", wire.MethodTaskCreate, fmt.Sprint(cr.TaskID))
	wantRow(t, au, 2, "ok", "", wire.MethodTaskStop, fmt.Sprint(cr.TaskID))
	wantRow(t, au, 3, "ok", "", wire.MethodTaskStop, fmt.Sprint(cr.TaskID))
	wantRow(t, au, 4, "refused", wire.CodeInvalid, wire.MethodTaskStop, fmt.Sprint(cr.TaskID))
	wantRow(t, au, 5, "ok", "", wire.MethodTaskCreate, fmt.Sprint(cr2.TaskID))
	wantRow(t, au, 6, "ok", "", wire.MethodTaskStop, fmt.Sprint(cr2.TaskID))
}

func TestFleetIdempotencyKeyRetryReturnsSame(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 4), be, nil)

	first := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	if first.Replayed || first.TaskID == 0 || first.RunID == "" {
		t.Fatalf("first create %+v", first)
	}
	// Same key, new reqId: the stored answer returns with replayed set and
	// no second effect.
	again := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	if again.TaskID != first.TaskID || again.RunID != first.RunID || !again.Replayed {
		t.Fatalf("retry %+v, want replay of %+v", again, first)
	}
	if be.creates() != 1 {
		t.Fatalf("CreateTask called %d times, want 1", be.creates())
	}
	// A different key is a different request: a second task.
	other := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k2", ws))
	if other.TaskID == first.TaskID || other.Replayed || be.creates() != 2 {
		t.Fatalf("second key %+v, creates %d", other, be.creates())
	}
	// A key may not change method mid-window.
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskSend, map[string]any{
		"idempotencyKey": "k1", "taskId": first.TaskID, "prompt": "go on",
	})
	wantRefusal(t, err, wire.CodeInvalid, "idempotencyKey was used for another method")
	// Past the 24 h window the row no longer answers: the request re-runs.
	clk.advance(25 * time.Hour)
	late := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	if late.TaskID == first.TaskID || late.Replayed || be.creates() != 3 {
		t.Fatalf("stale key re-ran as %+v, creates %d", late, be.creates())
	}
	id := fmt.Sprint(first.TaskID)
	wantRow(t, au, 0, "ok", "", wire.MethodTaskCreate, id)
	wantRow(t, au, 1, "ok", "", wire.MethodTaskCreate, id)
	wantRow(t, au, 2, "ok", "", wire.MethodTaskCreate, fmt.Sprint(other.TaskID))
	wantRow(t, au, 3, "refused", wire.CodeInvalid, wire.MethodTaskSend, id)
	wantRow(t, au, 4, "ok", "", wire.MethodTaskCreate, fmt.Sprint(late.TaskID))
}

func TestFleetCreateBudgetRefusedBeforeEffect(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	s, au := writeServer(clk, st, writeGrant(ws, 1), be, nil)
	first := callResult[wire.TaskCreateResult](t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k2", ws))
	var rf *Refusal
	if !errors.As(err, &rf) || rf.Code != wire.CodeBudgetExceeded {
		t.Fatalf("second create: %v", err)
	}
	if be.creates() != 1 {
		t.Fatalf("CreateTask called %d times, want 1", be.creates())
	}
	if _, ok, err := st.GetFleetIdem("B", "k2"); err != nil || ok {
		t.Fatalf("refused call stored an idem row: ok=%v err=%v", ok, err)
	}
	wantRow(t, au, 0, "ok", "", wire.MethodTaskCreate, fmt.Sprint(first.TaskID))
	wantRow(t, au, 1, "refused", wire.CodeBudgetExceeded, wire.MethodTaskCreate, "")
}

// waitFixture is a reader over one task and run, for the run.wait and
// run.status tests.
func waitFixture(t *testing.T, status string) (Reader, *memSource) {
	t.Helper()
	m := newMemSource()
	m.workspaces = []WorkspaceInfo{{ID: 1, Title: "w", DefaultBranch: "main"}}
	at := time.Unix(1_800_000_000, 0)
	m.tasks[1] = store.Task{ID: 1, WorkspaceID: 1, Title: "t", Status: "idle", CreatedAt: at, UpdatedAt: at}
	m.runs["r1"] = store.Run{ID: "r1", TaskID: 1, Provider: "glm", Model: "glm-4.7", Status: status, StartedAt: at, StopReason: "stop_requested"}
	for _, text := range []string{"one", "two", "three"} {
		m.addEvent("r1", map[string]any{"type": int(taskrunner.EventTypeText), "text": text})
	}
	return NewReader(m, ReaderOptions{}), m
}

func TestFleetRunWaitCursor(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	rd, m := waitFixture(t, "running")
	be := &fakeBackend{st: st, waiting: map[string]bool{"r1": true}}
	s, au := writeServer(clk, st, writeGrant(1, 2), be, rd)

	wait := func(extra map[string]any) wire.RunWaitResult {
		return callResult[wire.RunWaitResult](t, s, clk, "B", wire.MethodRunWait, extra)
	}
	// A live run with events answers at once, permission waiting included.
	w := wait(map[string]any{"runId": "r1"})
	if len(w.Events) != 3 || w.NextCursor != 3 || w.Done || w.State != "running" || !w.WaitingPermission {
		t.Fatalf("first page %+v", w)
	}
	// Nothing new and still running: the poll times out with the caller's
	// own cursor back and empty (never nil) events.
	w = wait(map[string]any{"runId": "r1", "cursor": 3})
	if w.Events == nil || len(w.Events) != 0 || w.NextCursor != 3 || w.Done || !w.WaitingPermission {
		t.Fatalf("timeout page %+v", w)
	}
	// A done run drains the remaining page and only then reports done.
	doneRun := m.runs["r1"]
	doneRun.Status = "done"
	m.runs["r1"] = doneRun
	w = wait(map[string]any{"runId": "r1", "cursor": 3})
	if len(w.Events) != 0 || !w.Done || w.State != "done" || w.StopReason != "stop_requested" || w.WaitingPermission {
		t.Fatalf("done tail %+v", w)
	}
	w = wait(map[string]any{"runId": "r1"})
	if len(w.Events) != 3 || !w.Done || w.NextCursor != 3 {
		t.Fatalf("full drain %+v", w)
	}
	for i := 0; i < 4; i++ {
		if r := wantRow(t, au, i, "ok", "", wire.MethodRunWait, "1"); r.RunID != "r1" {
			t.Fatalf("row %d runId %q", i, r.RunID)
		}
	}
}

func TestFleetRunStatusReportsWaitingPermission(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	rd, m := waitFixture(t, "running")
	m.runs["r2"] = store.Run{ID: "r2", TaskID: 1, Provider: "glm", Status: "done", StartedAt: time.Unix(1_800_000_000, 0)}
	be := &fakeBackend{st: st, waiting: map[string]bool{"r1": true}}
	s, au := writeServer(clk, st, writeGrant(1, 2), be, rd)
	live := callResult[wire.RunStatus](t, s, clk, "B", wire.MethodRunStatus, map[string]any{"runId": "r1"})
	if !live.WaitingPermission || live.State != "running" {
		t.Fatalf("live run %+v", live)
	}
	done := callResult[wire.RunStatus](t, s, clk, "B", wire.MethodRunStatus, map[string]any{"runId": "r2"})
	if done.WaitingPermission {
		t.Fatalf("done run %+v", done)
	}
	// Without a backend there is nobody to ask, so the flag stays false.
	au2 := &memAudit{}
	s2 := New(Options{HostID: "A", Authorizer: grantAuthorizer{g: writeGrant(1, 2)}, Audit: au2, Reader: rd, Now: clk.now})
	bare := callResult[wire.RunStatus](t, s2, clk, "B", wire.MethodRunStatus, map[string]any{"runId": "r1"})
	if bare.WaitingPermission {
		t.Fatalf("backend-less server reports waiting: %+v", bare)
	}
	// A read row records only the ids the request named, so a run.status
	// by runId has no taskId on the row.
	wantRow(t, au, 0, "ok", "", wire.MethodRunStatus, "")
	wantRow(t, au, 1, "ok", "", wire.MethodRunStatus, "")
	wantRow(t, au2, 0, "ok", "", wire.MethodRunStatus, "")
}

func TestFleetWriteDisabledWithoutBackend(t *testing.T) {
	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	be := &fakeBackend{st: st}
	for name, o := range map[string]Options{
		"no backend": {HostID: "A", Authorizer: grantAuthorizer{g: writeGrant(ws, 2)}, Audit: &memAudit{}, Reader: &fakeReader{}, Now: clk.now, Ledger: st, Tasks: storeTasks{st}},
		"no tasks":   {HostID: "A", Authorizer: grantAuthorizer{g: writeGrant(ws, 2)}, Audit: &memAudit{}, Reader: &fakeReader{}, Now: clk.now, Backend: be, Ledger: st},
	} {
		s := New(o)
		_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k-"+name, ws))
		wantRefusal(t, err, wire.CodeInvalid, "unknown method")
		if be.creates() != 0 {
			t.Fatalf("%s: task created", name)
		}
		rows := o.Audit.(*memAudit).rows
		if len(rows) != 1 || rows[0].Outcome != "refused" || rows[0].Code != wire.CodeInvalid || rows[0].Method != wire.MethodTaskCreate {
			t.Fatalf("%s: rows %+v", name, rows)
		}
	}
}

// failingCreateBackend makes CreateTask fail like a git worktree add that
// names the callee's local path.
type failingCreateBackend struct {
	*fakeBackend
	err error
}

func (b failingCreateBackend) CreateTask(int64, string, string, string) (TaskInfo, error) {
	return TaskInfo{}, b.err
}

func TestFleetBackendErrorNeverLeaksPaths(t *testing.T) {
	logs := &strings.Builder{}
	prev := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	st := openStore(t)
	clk := newClock()
	ws := fleetWorkspace(t, st)
	au := &memAudit{}
	build := func(err error) *Server {
		return New(Options{
			HostID: "A", Authorizer: grantAuthorizer{g: writeGrant(ws, 2)}, Audit: au, Reader: &fakeReader{},
			Now: clk.now, Backend: failingCreateBackend{&fakeBackend{st: st}, err}, Ledger: st, Tasks: storeTasks{st},
			PollInterval: time.Millisecond,
		})
	}

	s := build(errors.New("git worktree add /Users/secret/home/worktrees/1/x: exit status 128"))
	_, err := callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k1", ws))
	wantRefusal(t, err, wire.CodeInternal, "")
	if strings.Contains(err.Error(), "/Users/secret") {
		t.Fatalf("peer-facing error leaks a path: %v", err)
	}
	row := wantRow(t, au, 0, "error", "", wire.MethodTaskCreate, "")
	if row.Trace == "" {
		t.Fatal("audit row has no trace")
	}
	if !strings.Contains(logs.String(), "/Users/secret") {
		t.Fatalf("callee log lacks the detail: %q", logs.String())
	}

	s = build(refuse(wire.CodeBranchNotFound, "branch not found on origin"))
	_, err = callWrite(t, s, clk, "B", wire.MethodTaskCreate, createParams("k2", ws))
	wantRefusal(t, err, wire.CodeBranchNotFound, "branch not found on origin")
}
