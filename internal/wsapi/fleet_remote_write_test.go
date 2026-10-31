package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeWriteCaller answers each fleet write method from its own canned
// result, so one env can serve a create, send, stop and wait in a row.
type fakeWriteCaller struct {
	mu      sync.Mutex
	methods []string
	params  []json.RawMessage
	results map[string]string
	err     error // when set, every call fails
}

func (f *fakeWriteCaller) Call(_ context.Context, method string, params, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(params)
	f.methods = append(f.methods, method)
	f.params = append(f.params, raw)
	if f.err != nil {
		return f.err
	}
	if out != nil {
		if res, ok := f.results[method]; ok {
			return json.Unmarshal([]byte(res), out)
		}
	}
	return nil
}

func (f *fakeWriteCaller) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.methods)
}

func (f *fakeWriteCaller) lastParams(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.params) == 0 {
		t.Fatal("fake write caller saw no call")
	}
	var m map[string]any
	if err := json.Unmarshal(f.params[len(f.params)-1], &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeWatcher records the watch ids handed to the watcher.
type fakeWatcher struct {
	mu   sync.Mutex
	adds []int64
}

func (w *fakeWatcher) Add(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adds = append(w.adds, id)
}

func (w *fakeWatcher) added() []int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]int64(nil), w.adds...)
}

// writeEnv is a fleet env whose remote bridge answers the write methods
// and whose watch, seat and put seams are observable.
type writeEnv struct {
	*fleetEnv
	peerID  string
	caller  *fakeWriteCaller
	watcher *fakeWatcher

	disconnected bool  // Conn reports no live link
	putErr       error // the local mirror/watch write fails
	seatFn       func(runID string) (bool, error)
	originFn     func(runID string, taskID int64) (string, error)
}

func newWriteEnv(t *testing.T, mods ...func(*FleetRemoteOptions)) *writeEnv {
	t.Helper()
	e := &writeEnv{
		fleetEnv: newFleetEnv(t),
		caller:   &fakeWriteCaller{results: map[string]string{}},
		watcher:  &fakeWatcher{},
	}
	peer := newCallerKey(t)
	e.pairApproved(t, peer, "peer")
	e.peerID = peer.HostID()
	o := FleetRemoteOptions{
		Conn: func(string) (client.Caller, client.LinkState) {
			if e.disconnected {
				return nil, client.LinkOther
			}
			return e.caller, client.LinkConnected
		},
		RunOrigin: func(runID string, taskID int64) (string, error) {
			if e.originFn != nil {
				return e.originFn(runID, taskID)
			}
			return "", nil
		},
		SeatMayDelegate: func(runID string) (bool, error) {
			if e.seatFn != nil {
				return e.seatFn(runID)
			}
			return true, nil
		},
		PutWatch: func(mt store.FleetRemoteTask, w *store.FleetWatch) (int64, error) {
			if e.putErr != nil {
				return 0, e.putErr
			}
			return e.store().PutFleetRemoteTaskAndWatch(mt, w)
		},
		Watch: e.watcher,
	}
	for _, m := range mods {
		m(&o)
	}
	e.fleetEnv.api.SetFleetRemote(o)
	return e
}

func (e *writeEnv) store() *store.Store { return e.api.fleet.opts.Store }

func (e *writeEnv) call(t *testing.T, rc *requestContext, method string, params any) (json.RawMessage, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.api.hs[method](context.Background(), rc, raw)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, nil
}

func (e *writeEnv) mirrors(t *testing.T) []store.FleetRemoteTask {
	t.Helper()
	ms, err := e.store().ListFleetRemoteTasks(e.peerID)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func (e *writeEnv) openWatches(t *testing.T) []store.FleetWatch {
	t.Helper()
	ws, err := e.store().ListOpenFleetWatches()
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// writeRunRC is a local, run-scoped caller: task 7 in chat 9.
func writeRunRC() *requestContext {
	return &requestContext{local: true, runScoped: true, ident: runIdentity{runID: "r1", taskID: 7, chatID: 9}}
}

func decodeWrite(t *testing.T, raw json.RawMessage) RemoteWriteResult {
	t.Helper()
	var res RemoteWriteResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func createParams(key string) RemoteWriteParams {
	return RemoteWriteParams{
		Host: "peer", Op: "create", WorkspaceID: 1, Provider: "p", Model: "m",
		Title: "Peer work", Prompt: "go", IdempotencyKey: key,
	}
}

func TestRemoteWriteCreateWritesMirrorAndWatchTogether(t *testing.T) {
	e := newWriteEnv(t)
	e.caller.results[wire.MethodTaskCreate] = `{"taskId":4,"runId":"run-a","branch":"topic/b"}`

	raw, err := e.call(t, writeRunRC(), "remote.write", createParams(""))
	if err != nil {
		t.Fatal(err)
	}
	res := decodeWrite(t, raw)
	if res.Host != "peer" || res.HostID != e.peerID || res.Op != "create" ||
		res.GlobalID != "peer:4" || res.TaskID != 4 || res.RunID != "run-a" || res.Branch != "topic/b" {
		t.Fatalf("result = %s", raw)
	}
	if res.IdempotencyKey == "" || res.WatchID == 0 {
		t.Fatalf("result = %s: want a minted key and a watch", raw)
	}
	if !strings.Contains(string(raw), `"events":[`) {
		t.Fatalf("result = %s: events must be an array", raw)
	}
	if sent := e.caller.lastParams(t); sent["idempotencyKey"] != res.IdempotencyKey {
		t.Fatalf("sent key = %v, want the minted %q", sent["idempotencyKey"], res.IdempotencyKey)
	}

	if ms := e.mirrors(t); len(ms) != 1 || ms[0].RemoteTaskID != "4" || ms[0].LocalTaskID != 7 ||
		ms[0].Title != "Peer work" || ms[0].Branch != "topic/b" || ms[0].State != "running" {
		t.Fatalf("mirrors = %+v", ms)
	}
	ws := e.openWatches(t)
	if len(ws) != 1 || ws[0].RemoteTaskID != "4" || ws[0].RemoteRunID != "run-a" ||
		ws[0].LocalTaskID != 7 || ws[0].ChatID != 9 || !ws[0].Notify {
		t.Fatalf("watches = %+v", ws)
	}
	if ws[0].ID != res.WatchID {
		t.Fatalf("watchId = %d, watch row = %d", res.WatchID, ws[0].ID)
	}
	if adds := e.watcher.added(); len(adds) != 1 || adds[0] != res.WatchID {
		t.Fatalf("watcher adds = %v, want [%d]", adds, res.WatchID)
	}

	// Without the seam the pair is written straight through the store.
	e2 := newWriteEnv(t, func(o *FleetRemoteOptions) { o.PutWatch = nil })
	e2.caller.results[wire.MethodTaskCreate] = `{"taskId":5,"runId":"run-b"}`
	if _, err := e2.call(t, writeRunRC(), "remote.write", createParams("k2")); err != nil {
		t.Fatal(err)
	}
	if ws := e2.openWatches(t); len(ws) != 1 || ws[0].RemoteTaskID != "5" {
		t.Fatalf("store-path watches = %+v", ws)
	}
}

func TestFleetWatchUnsentPromptNoWatch(t *testing.T) {
	e := newWriteEnv(t)
	e.disconnected = true
	_, err := e.call(t, writeRunRC(), "remote.write", createParams(""))
	wantRemoteCode(t, err, client.CodeHostUnavailable)
	if n := e.caller.calls(); n != 0 {
		t.Fatalf("caller called %d times while disconnected", n)
	}
	if ms, ws := e.mirrors(t), e.openWatches(t); len(ms) != 0 || len(ws) != 0 {
		t.Fatalf("mirrors = %+v watches = %+v", ms, ws)
	}
	if adds := e.watcher.added(); len(adds) != 0 {
		t.Fatalf("watcher adds = %v", adds)
	}

	e2 := newWriteEnv(t)
	e2.caller.err = &wsclient.RPCError{Code: wire.CodeNotGranted, Message: "no"}
	_, err = e2.call(t, writeRunRC(), "remote.write", createParams(""))
	wantRemoteCode(t, err, wire.CodeNotGranted)
	if ms, ws := e2.mirrors(t), e2.openWatches(t); len(ms) != 0 || len(ws) != 0 {
		t.Fatalf("mirrors = %+v watches = %+v", ms, ws)
	}
	if adds := e2.watcher.added(); len(adds) != 0 {
		t.Fatalf("watcher adds = %v", adds)
	}
}

func TestFleetLocalWriteFailureReportsAndRetrySafe(t *testing.T) {
	e := newWriteEnv(t)
	e.caller.results[wire.MethodTaskCreate] = `{"taskId":4,"runId":"run-a"}`
	e.putErr = errors.New("disk full")

	_, err := e.call(t, writeRunRC(), "remote.write", createParams("k1"))
	wantRemoteCode(t, err, codeLocalWriteFailed)
	for _, want := range []string{"peer:4", "run-a", "k1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q misses %q", err, want)
		}
	}
	if ms, ws := e.mirrors(t), e.openWatches(t); len(ms) != 0 || len(ws) != 0 {
		t.Fatalf("mirrors = %+v watches = %+v", ms, ws)
	}

	e.putErr = nil
	raw, err := e.call(t, writeRunRC(), "remote.write", createParams("k1"))
	if err != nil {
		t.Fatal(err)
	}
	res := decodeWrite(t, raw)
	if sent := e.caller.lastParams(t); sent["idempotencyKey"] != "k1" {
		t.Fatalf("sent key = %v, want the retried k1", sent["idempotencyKey"])
	}
	if ws := e.openWatches(t); len(ws) != 1 || ws[0].ID != res.WatchID {
		t.Fatalf("watches = %+v, result watchId = %d", ws, res.WatchID)
	}
}

func TestRemoteWriteAmbiguousKeepsKeyAndRetryReturnsSame(t *testing.T) {
	e := newWriteEnv(t)
	e.caller.err = errors.New("connection reset mid-call")

	_, err := e.call(t, writeRunRC(), "remote.write", createParams("k1"))
	wantRemoteCode(t, err, codeAmbiguous)
	if !strings.Contains(err.Error(), "k1") {
		t.Fatalf("message %q should name the key", err)
	}
	if ms, ws := e.mirrors(t), e.openWatches(t); len(ms) != 0 || len(ws) != 0 {
		t.Fatalf("mirrors = %+v watches = %+v", ms, ws)
	}

	e.caller.err = nil
	e.caller.results[wire.MethodTaskCreate] = `{"taskId":4,"runId":"run-a","replayed":true}`
	raw, err := e.call(t, writeRunRC(), "remote.write", createParams("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if res := decodeWrite(t, raw); !res.Replayed {
		t.Fatalf("result = %s: want the replayed create", raw)
	}
	if sent := e.caller.lastParams(t); sent["idempotencyKey"] != "k1" {
		t.Fatalf("sent key = %v, want k1", sent["idempotencyKey"])
	}
	if ws := e.openWatches(t); len(ws) != 1 {
		t.Fatalf("watches = %+v", ws)
	}
}

func TestRemoteWriteRefusedForPeerSeatAndRemoteOrigin(t *testing.T) {
	e := newWriteEnv(t)
	rc := writeRunRC()
	e.caller.results[wire.MethodTaskCreate] = `{"taskId":4,"runId":"run-a"}`
	e.seatFn = func(string) (bool, error) { return false, nil }
	for _, m := range []string{"remote.write", "remote.read", "remote.hosts"} {
		p := map[string]any{"host": "peer"}
		switch m {
		case "remote.write":
			p["op"] = "create"
			p["workspaceId"] = 1
			p["provider"] = "p"
			p["model"] = "m"
			p["prompt"] = "go"
		case "remote.read":
			p["op"] = "info"
		}
		_, err := e.call(t, rc, m, p)
		wantRemoteCode(t, err, wire.CodeHopLimit)
	}
	if n := e.caller.calls(); n != 0 {
		t.Fatalf("caller called %d times for a seat that cannot delegate", n)
	}

	e.seatFn = func(string) (bool, error) { return false, errors.New("seat db closed") }
	_, err := e.call(t, rc, "remote.write", map[string]any{"host": "peer", "op": "wait", "runId": "r"})
	wantRemoteCode(t, err, wire.CodeHopLimit)

	e.seatFn = nil
	e.originFn = func(string, int64) (string, error) { return "otherhost", nil }
	_, err = e.call(t, rc, "remote.write", map[string]any{"host": "peer", "op": "wait", "runId": "r"})
	wantRemoteCode(t, err, wire.CodeHopLimit)
	if ms, ws := e.mirrors(t), e.openWatches(t); len(ms) != 0 || len(ws) != 0 {
		t.Fatalf("mirrors = %+v watches = %+v", ms, ws)
	}
}

func TestRemoteWriteSendStopWait(t *testing.T) {
	e := newWriteEnv(t)
	// The mirror of the task being continued keeps its title and branch.
	if _, err := e.store().PutFleetRemoteTaskAndWatch(store.FleetRemoteTask{
		HostID: e.peerID, RemoteTaskID: "4", Title: "Old title", Branch: "old/b", State: "done",
	}, nil); err != nil {
		t.Fatal(err)
	}
	e.caller.results[wire.MethodTaskSend] = `{"taskId":4,"runId":"run-2"}`
	e.caller.results[wire.MethodTaskStop] = `{"stopped":true}`
	e.caller.results[wire.MethodRunWait] = `{"runId":"run-2","state":"waiting_permission","waitingPermission":true,"events":[{"seq":3,"type":"log","text":"hi"}],"nextCursor":3}`
	rc := writeRunRC()

	raw, err := e.call(t, rc, "remote.write", RemoteWriteParams{Host: "peer", Op: "send", TaskID: 4, Prompt: "again", IdempotencyKey: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if res := decodeWrite(t, raw); res.GlobalID != "peer:4" || res.TaskID != 4 || res.RunID != "run-2" || res.WatchID == 0 {
		t.Fatalf("send result = %s", raw)
	}
	if sent := e.caller.lastParams(t); sent["taskId"] != float64(4) || sent["prompt"] != "again" || sent["idempotencyKey"] != "s1" {
		t.Fatalf("sent = %v", sent)
	}
	if ms := e.mirrors(t); len(ms) != 1 || ms[0].Title != "Old title" || ms[0].Branch != "old/b" ||
		ms[0].State != "running" || ms[0].LocalTaskID != 7 {
		t.Fatalf("mirrors = %+v", ms)
	}
	if ws := e.openWatches(t); len(ws) != 1 || ws[0].RemoteRunID != "run-2" || ws[0].ChatID != 9 {
		t.Fatalf("watches = %+v", ws)
	}

	raw, err = e.call(t, rc, "remote.write", RemoteWriteParams{Host: "peer", Op: "stop", TaskID: 4, RunID: "run-2"})
	if err != nil {
		t.Fatal(err)
	}
	if res := decodeWrite(t, raw); !res.Stopped {
		t.Fatalf("stop result = %s", raw)
	}
	if sent := e.caller.lastParams(t); sent["taskId"] != float64(4) || sent["runId"] != "run-2" {
		t.Fatalf("sent = %v", sent)
	}
	if ws := e.openWatches(t); len(ws) != 1 {
		t.Fatalf("stop must not touch the watch: %+v", ws)
	}

	raw, err = e.call(t, rc, "remote.write", RemoteWriteParams{Host: "peer", Op: "wait", RunID: "run-2", TimeoutSeconds: 999})
	if err != nil {
		t.Fatal(err)
	}
	if res := decodeWrite(t, raw); res.State != "waiting_permission" || !res.WaitingPermission ||
		len(res.Events) != 1 || res.Events[0].Seq != 3 || res.NextCursor != 3 || res.Done {
		t.Fatalf("wait result = %s", raw)
	}
	if sent := e.caller.lastParams(t); sent["runId"] != "run-2" || sent["timeoutSeconds"] != float64(25) {
		t.Fatalf("sent = %v, want the wait capped to 25s", sent)
	}
}

func TestRemoteWriteNotOverRelayNotHostPrincipal(t *testing.T) {
	e := newWriteEnv(t)
	rc := &requestContext{local: false}
	for _, p := range []RemoteWriteParams{
		{Host: "peer", Op: "create", WorkspaceID: 1, Provider: "p", Model: "m", Prompt: "go"},
		{Host: "peer", Op: "send", TaskID: 4, Prompt: "go"},
		{Host: "peer", Op: "stop", TaskID: 4},
		{Host: "peer", Op: "wait", RunID: "r"},
	} {
		_, err := e.call(t, rc, "remote.write", p)
		wantRemoteCode(t, err, codeForbidden)
	}
	if n := e.caller.calls(); n != 0 {
		t.Fatalf("caller called %d times over the relay", n)
	}

	for _, kind := range []string{"pairing", "pending", "approved"} {
		c := &conn{host: &hostPrincipal{hostID: "h", kind: kind}}
		wantRemoteCode(t, c.gate("remote.write"), codeForbidden)
	}
}

func TestRemoteWriteAbsentWhenFleetOff(t *testing.T) {
	api, _ := newCloseTestAPI(t)
	if _, ok := api.hs["remote.write"]; ok {
		t.Fatal("remote.write registered with fleet off")
	}
}
