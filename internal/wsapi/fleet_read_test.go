package wsapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeFleetWorkspaces serves the store's workspaces with default base main.
type fakeFleetWorkspaces struct{ st *store.Store }

func (f fakeFleetWorkspaces) ListWorkspaces() ([]store.Workspace, error) {
	return f.st.ListWorkspaces()
}

func (fakeFleetWorkspaces) DefaultBase(int64) (string, bool, error) { return "main", false, nil }

// fleetReq numbers the request ids so no two calls share one.
var fleetReq atomic.Int64

func readProv(origin string, hops []string, ts int64) wire.Prov {
	return wire.Prov{
		Origin: origin, Trace: "t1", Hops: hops,
		ReqID: fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), fleetReq.Add(1)),
		TS:    ts,
	}
}

func okProv(caller *identity.Key) wire.Prov {
	return readProv(caller.HostID(), []string{caller.HostID()}, time.Now().Unix())
}

// seedFleetData creates one workspace, two tasks and a run with two events.
func seedFleetData(t *testing.T, st *store.Store) (store.Workspace, []store.Task, store.Run) {
	t.Helper()
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/secret/local/path/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	var tasks []store.Task
	for _, title := range []string{"first task", "second task"} {
		tk, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: title, Status: "created"})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, tk)
	}
	run, err := st.CreateRun(store.Run{ID: "run-fleet-1", TaskID: tasks[0].ID, Provider: "glm", Prompt: "hi", Status: "done", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"hello", "world"} {
		data, _ := json.Marshal(map[string]any{"type": taskrunner.EventTypeText, "text": text})
		if _, err := st.AppendRunEvent(run.ID, int64(i+1), string(data)); err != nil {
			t.Fatal(err)
		}
	}
	return ws, tasks, run
}

// fleetStore returns the store behind e (the registry shares it).
func (e *fleetEnv) fleetStore() *store.Store { return e.api.fleet.opts.Store }

func approvedConn(t *testing.T, e *fleetEnv) (*wsclient.Client, *identity.Key) {
	t.Helper()
	caller := newCallerKey(t)
	e.pairApproved(t, caller, "caller")
	cl := e.dial(t)
	if _, err := e.doHello(t, cl, caller); err != nil {
		t.Fatal(err)
	}
	return cl, caller
}

func TestFleetReadServedOverWsapi(t *testing.T) {
	e := newFleetEnv(t)
	st := e.fleetStore()
	ws, tasks, run := seedFleetData(t, st)
	cl, caller := approvedConn(t, e)

	var raws []string
	call := func(method string, params any, out any) {
		t.Helper()
		var raw json.RawMessage
		if err := cl.Call(callCtx(), method, params, &raw); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		raws = append(raws, string(raw))
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode %s: %v", method, raw, err)
		}
	}

	var info wire.Info
	call(wire.MethodInfo, wire.InfoParams{Prov: okProv(caller)}, &info)
	if info.HostID != e.key.HostID() {
		t.Fatalf("info host = %q, want %q", info.HostID, e.key.HostID())
	}
	var wl wire.WorkspaceList
	call(wire.MethodWorkspaceList, wire.WorkspaceListParams{Prov: okProv(caller)}, &wl)
	if len(wl.Workspaces) != 1 || wl.Workspaces[0].Title != "repo" || wl.Workspaces[0].DefaultBranch != "main" {
		t.Fatalf("workspaces = %+v", wl.Workspaces)
	}
	var tl wire.TaskList
	call(wire.MethodTaskList, wire.TaskListParams{Prov: okProv(caller), WorkspaceID: ws.ID}, &tl)
	if len(tl.Tasks) != 2 || tl.Tasks[0].Title != "first task" || tl.Tasks[1].Title != "second task" {
		t.Fatalf("tasks = %+v", tl.Tasks)
	}
	var tg wire.Task
	call(wire.MethodTaskGet, wire.TaskGetParams{Prov: okProv(caller), ID: tasks[1].ID}, &tg)
	if tg.Title != "second task" {
		t.Fatalf("task = %+v", tg)
	}
	var rs wire.RunStatus
	call(wire.MethodRunStatus, wire.RunStatusParams{Prov: okProv(caller), TaskID: tasks[0].ID}, &rs)
	if rs.RunID != run.ID || rs.State == "" || rs.CostUSD != nil {
		t.Fatalf("run status = %+v", rs)
	}
	var act wire.Activity
	call(wire.MethodTaskActivity, wire.TaskActivityParams{Prov: okProv(caller), TaskID: tasks[0].ID}, &act)
	if len(act.Events) != 2 || act.Events[0].Text != "hello" || act.Events[1].Text != "world" {
		t.Fatalf("activity = %+v", act)
	}

	for i, raw := range raws {
		if strings.Contains(raw, ws.Path) || strings.Contains(raw, "/secret") {
			t.Errorf("result %d leaks the local path: %s", i, raw)
		}
	}
	rows, err := st.ListFleetAudit("", time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(wire.ReadMethods) {
		t.Fatalf("audit rows = %d, want %d: %+v", len(rows), len(wire.ReadMethods), rows)
	}
	for _, r := range rows {
		if r.Direction != "in" || r.HostID != caller.HostID() || r.Outcome != "ok" {
			t.Errorf("audit row = %+v", r)
		}
	}
}

func TestFleetReadRefusalsOverWsapi(t *testing.T) {
	e := newFleetEnv(t)
	st := e.fleetStore()
	seedFleetData(t, st)
	cl, caller := approvedConn(t, e)
	callee := e.key.HostID()
	me := caller.HostID()
	now := time.Now().Unix()

	var wantCodes []string
	refuse := func(p wire.Prov, code string) {
		t.Helper()
		err := cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: p}, nil)
		wantFleetCode(t, err, code)
		wantCodes = append(wantCodes, code)
	}
	refuse(readProv(me, []string{me, callee}, now), wire.CodeCycle)
	refuse(readProv(me, []string{me, "other"}, now), wire.CodeHopLimit)
	refuse(readProv("someoneelse", []string{me}, now), wire.CodePrincipalMismatch)

	dup := okProv(caller)
	if err := cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: dup}, nil); err != nil {
		t.Fatalf("first use of reqId: %v", err)
	}
	wantCodes = append(wantCodes, "")
	err := cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: dup}, nil)
	wantFleetCode(t, err, wire.CodeReplay)
	wantCodes = append(wantCodes, wire.CodeReplay)

	refuse(readProv(me, []string{me}, now-600), wire.CodeStale)

	if err := cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: okProv(caller)}, nil); err != nil {
		t.Fatalf("valid call after refusals: %v", err)
	}
	wantCodes = append(wantCodes, "")

	rows, err := st.ListFleetAudit("", time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(wantCodes) {
		t.Fatalf("audit rows = %d, want %d: %+v", len(rows), len(wantCodes), rows)
	}
	// rows are newest first.
	for i, r := range rows {
		want := wantCodes[len(wantCodes)-1-i]
		if want == "" {
			if r.Outcome != "ok" {
				t.Errorf("row %d = %+v, want ok", i, r)
			}
			continue
		}
		if r.Outcome != "refused" || r.Code != want {
			t.Errorf("row %d = %+v, want refused %s", i, r, want)
		}
	}
}

func TestFleetReadNotGrantedWithoutReadOp(t *testing.T) {
	e := newFleetEnv(t)
	st := e.fleetStore()
	ws, tasks, _ := seedFleetData(t, st)
	cl, caller := approvedConn(t, e)

	g := registry.DefaultForwardGrant()
	g.WorkspacesWrite = []int64{1}
	g.WorkspacesRead = registry.WorkspaceScope{IDs: []int64{999}}
	if err := e.reg.SetGrant(caller.HostID(), g); err != nil {
		t.Fatal(err)
	}

	err := cl.Call(callCtx(), wire.MethodTaskList, wire.TaskListParams{Prov: okProv(caller), WorkspaceID: ws.ID}, nil)
	wantFleetCode(t, err, wire.CodeNotGranted)
	err = cl.Call(callCtx(), wire.MethodTaskGet, wire.TaskGetParams{Prov: okProv(caller), ID: tasks[0].ID}, nil)
	wantFleetCode(t, err, wire.CodeNotFound)
}

func TestFleetReadMethodsNotRunScopedNotPlugin(t *testing.T) {
	e := newFleetEnv(t)
	for _, m := range wire.ReadMethods {
		if runScopedSet[m] {
			t.Errorf("%s is run-scoped", m)
		}
		if _, ok := pluginMethodCap[m]; ok {
			t.Errorf("%s is in the plugin capability table", m)
		}
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("handler map lacks %s", m)
		}
		// A plugin session refuses it: either never-listed or not part of the API.
		s := &pluginSession{}
		if _, rerr := s.Request(context.Background(), m, nil); rerr == nil {
			t.Errorf("plugin session served %s", m)
		}
		// A non-host caller never reaches the data: no principal, forbidden.
		_, err := e.api.hs[m](context.Background(), &requestContext{conn: &conn{}, local: true}, []byte(`{}`))
		var ce *rpcCodeError
		if !errors.As(err, &ce) || ce.code != codeForbidden {
			t.Errorf("%s without a host principal: %v, want forbidden", m, err)
		}
	}
}

// signHelloV signs a hello with an arbitrary fleet version and caps, using
// the canonical layout of hello.signedBytes.
func signHelloV(k *identity.Key, calleeID string, nonceC, nonceA []byte, ts int64, fleetV int, caps []string) hello.Hello {
	field := func(out, b []byte) []byte {
		out = binary.BigEndian.AppendUint16(out, uint16(len(b)))
		return append(out, b...)
	}
	h := hello.Hello{V: 1, HostID: k.HostID(), Pub: k.Public(), NonceA: nonceA, TS: ts, Version: "caller", FleetV: fleetV, Caps: caps}
	msg := []byte(hello.Domain)
	msg = field(msg, []byte(calleeID))
	msg = field(msg, []byte(h.HostID))
	msg = field(msg, nonceC)
	msg = field(msg, nonceA)
	msg = field(msg, nil)
	msg = binary.BigEndian.AppendUint64(msg, uint64(ts))
	msg = binary.BigEndian.AppendUint16(msg, uint16(fleetV))
	h.Sig = k.Sign(msg)
	return h
}

func TestFleetVersionSkewRefused(t *testing.T) {
	e := newFleetEnv(t)
	seedFleetData(t, e.fleetStore())
	caller := newCallerKey(t)
	e.pairApproved(t, caller, "caller")

	for name, tc := range map[string]struct {
		fleetV int
		caps   []string
	}{
		"newer fleet version": {hello.FleetV + 1, hello.Caps()},
		"missing fleet.link":  {hello.FleetV, []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			cl := e.dial(t)
			var ch hello.Challenge
			if err := cl.Call(callCtx(), hello.MethodChallenge, nil, &ch); err != nil {
				t.Fatal(err)
			}
			nonceA, err := hello.NewNonce()
			if err != nil {
				t.Fatal(err)
			}
			h := signHelloV(caller, ch.HostID, ch.NonceC, nonceA, time.Now().Unix(), tc.fleetV, tc.caps)
			wantFleetCode(t, cl.Call(callCtx(), hello.MethodHello, h, nil), hello.CodeVersionUnsupported)
			err = cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: okProv(caller)}, nil)
			wantFleetCode(t, err, codeForbidden)
		})
	}

	// A normal hello on a new connection works.
	cl := e.dial(t)
	if _, err := e.doHello(t, cl, caller); err != nil {
		t.Fatalf("normal hello after skew: %v", err)
	}
	if err := cl.Call(callCtx(), wire.MethodInfo, wire.InfoParams{Prov: okProv(caller)}, nil); err != nil {
		t.Fatalf("fleet.info after normal hello: %v", err)
	}
}
