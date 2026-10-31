package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

var e2eReq atomic.Int64

func e2eProv(origin string, hops ...string) wire.Prov {
	return wire.Prov{
		Origin: origin, Trace: "e2e", Hops: hops,
		ReqID: fmt.Sprintf("e2e-req-%d", e2eReq.Add(1)),
		TS:    time.Now().Unix(),
	}
}

func wantRPCCode(t *testing.T, err error, code string) {
	t.Helper()
	var re *wsclient.RPCError
	if !errors.As(err, &re) || re.Code != code {
		t.Fatalf("err = %v, want RPC code %s", err, code)
	}
}

func TestFleetReadTwoDaemonsE2E(t *testing.T) {
	a, b := pairE2E(t)
	aID, bID := a.key.HostID(), b.key.HostID()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// B's data.
	bPath := t.TempDir()
	ws, err := b.db.CreateWorkspace(store.Workspace{Path: bPath, Title: "b-ws"})
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != 1 {
		t.Fatalf("workspace id = %d, want 1", ws.ID)
	}
	task, err := b.db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "remote readable", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := b.db.CreateRun(store.Run{ID: "run-e2e-1", TaskID: task.ID, Provider: "glm", Prompt: "hi", Status: "done", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i, ev := range []string{`{"type":0,"text":"hello from b"}`, `{"type":1,"stopReason":"end_turn"}`} {
		if _, err := b.db.AppendRunEvent(run.ID, int64(i+1), ev); err != nil {
			t.Fatal(err)
		}
	}

	// A reads B over the paired link.
	var c Conn
	poll(t, "A's live conn to B", func() bool {
		var live bool
		c, live = a.mgr.Conn(bID)
		return live
	})
	var raws []string
	call := func(method string, params, out any) {
		t.Helper()
		var raw json.RawMessage
		if err := c.Call(ctx, method, params, &raw); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		raws = append(raws, string(raw))
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode %s: %v", method, raw, err)
		}
	}
	okProv := func() wire.Prov { return e2eProv(aID, aID) }

	var info wire.Info
	call(wire.MethodInfo, wire.InfoParams{Prov: okProv()}, &info)
	hasRead := false
	for _, op := range info.Limits.Ops {
		hasRead = hasRead || op == "read"
	}
	if info.HostID != bID || !hasRead {
		t.Fatalf("info = %+v, want host %s with read op", info, bID)
	}
	var wl wire.WorkspaceList
	call(wire.MethodWorkspaceList, wire.WorkspaceListParams{Prov: okProv()}, &wl)
	if len(wl.Workspaces) != 1 || wl.Workspaces[0].Title != "b-ws" || wl.Workspaces[0].RepoURL != "" {
		t.Fatalf("workspaces = %+v", wl.Workspaces)
	}
	var tl wire.TaskList
	call(wire.MethodTaskList, wire.TaskListParams{Prov: okProv()}, &tl)
	if len(tl.Tasks) != 1 || tl.Tasks[0].Title != "remote readable" || tl.Tasks[0].State != "done" {
		t.Fatalf("tasks = %+v", tl.Tasks)
	}
	var tg wire.Task
	call(wire.MethodTaskGet, wire.TaskGetParams{Prov: okProv(), ID: task.ID}, &tg)
	if tg.ID != task.ID || tg.Title != "remote readable" {
		t.Fatalf("task = %+v", tg)
	}
	var rs wire.RunStatus
	call(wire.MethodRunStatus, wire.RunStatusParams{Prov: okProv(), TaskID: task.ID}, &rs)
	if rs.RunID != run.ID || rs.TaskID != task.ID {
		t.Fatalf("run status = %+v", rs)
	}
	var act wire.Activity
	call(wire.MethodTaskActivity, wire.TaskActivityParams{Prov: okProv(), TaskID: task.ID}, &act)
	if len(act.Events) != 2 || act.Events[0].Type != "text" || act.Events[0].Text != "hello from b" || act.Events[1].Type != "done" {
		t.Fatalf("activity = %+v", act)
	}
	for i, raw := range raws {
		if strings.Contains(raw, bPath) {
			t.Errorf("result %d leaks B's workspace path: %s", i, raw)
		}
	}

	// Provenance over the real link.
	refuse := func(p wire.Prov, code string) {
		t.Helper()
		wantRPCCode(t, c.Call(ctx, wire.MethodInfo, wire.InfoParams{Prov: p}, nil), code)
	}
	refuse(e2eProv(aID, aID, bID), wire.CodeCycle)
	refuse(e2eProv(aID, aID, "x"), wire.CodeHopLimit)
	refuse(e2eProv(bID, aID), wire.CodePrincipalMismatch)
	dup := okProv()
	if err := c.Call(ctx, wire.MethodInfo, wire.InfoParams{Prov: dup}, nil); err != nil {
		t.Fatalf("first use of reqId: %v", err)
	}
	refuse(dup, wire.CodeReplay)
	if err := c.Call(ctx, wire.MethodInfo, wire.InfoParams{Prov: okProv()}, nil); err != nil {
		t.Fatalf("valid call after refusals: %v", err)
	}

	// Callee audit: 6 reads + the first use of dup + the final call are ok,
	// four are refused.
	rows, err := b.db.ListFleetAudit("", time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 12 {
		t.Fatalf("audit rows = %d, want 12: %+v", len(rows), rows)
	}
	codes := map[string]int{}
	oks := 0
	for _, r := range rows {
		if r.Direction != "in" || r.HostID != aID {
			t.Errorf("audit row = %+v", r)
		}
		if strings.Contains(r.Summary, bPath) {
			t.Errorf("audit summary leaks path: %q", r.Summary)
		}
		switch r.Outcome {
		case "ok":
			oks++
		case "refused":
			codes[r.Code]++
		default:
			t.Errorf("audit outcome = %q: %+v", r.Outcome, r)
		}
	}
	if oks != 8 {
		t.Errorf("ok rows = %d, want 8", oks)
	}
	for _, code := range []string{wire.CodeCycle, wire.CodeHopLimit, wire.CodePrincipalMismatch, wire.CodeReplay} {
		if codes[code] != 1 {
			t.Errorf("refused rows with code %s = %d, want 1 (%v)", code, codes[code], codes)
		}
	}

	// Reverse is dark: B cannot read A.
	var ok struct {
		OK bool `json:"ok"`
	}
	b.call(t, "fleet.host.route", map[string]any{"host": aID, "address": a.wsURL, "password": "pw-alpha-0123456", "allowCleartext": true}, &ok)
	if err := b.reg.SetOutboundEnabled(aID, true); err != nil {
		t.Fatal(err)
	}
	res, err := b.svc.Test(ctx, aID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Code != wire.CodeNotGranted {
		t.Fatalf("B->A test before enable_reverse = %+v, want not_granted", res)
	}
	if _, live := b.mgr.Conn(aID); live {
		t.Fatal("B has a live conn to A before enable_reverse")
	}
	if err := b.reg.SetOutboundEnabled(aID, false); err != nil {
		t.Fatal(err)
	}

	a.call(t, "fleet.host.enable_reverse", map[string]any{"host": "bee"}, &json.RawMessage{})
	poll(t, "B's link to A connected", func() bool {
		return b.mgr.Status(aID).State == StateConnected
	})
	bc, live := b.mgr.Conn(aID)
	if !live {
		t.Fatal("no live conn B->A")
	}
	var rev wire.WorkspaceList
	if err := bc.Call(ctx, wire.MethodWorkspaceList, wire.WorkspaceListParams{Prov: e2eProv(bID, bID)}, &rev); err != nil {
		t.Fatalf("B reads A after enable_reverse: %v", err)
	}
	if len(rev.Workspaces) != 0 {
		t.Fatalf("A's workspaces = %+v, want none", rev.Workspaces)
	}

	a.call(t, "fleet.host.disable_reverse", map[string]any{"host": "bee"}, &json.RawMessage{})
	poll(t, "B's link to A to leave connected", func() bool {
		return b.mgr.Status(aID).State != StateConnected
	})
}
