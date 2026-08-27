package wsapi

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/terminal"
)

func decodeDataEvent(t *testing.T, env envelope) string {
	t.Helper()
	if env.Event != "data" {
		t.Fatalf("event = %+v, want a %q event", env, "data")
	}
	var p terminalDataParams
	if err := json.Unmarshal(env.Params, &p); err != nil {
		t.Fatalf("decode data params: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		t.Fatalf("base64-decode data params: %v", err)
	}
	return string(raw)
}

// collectDataUntil reads "data" events for id off ws, base64-decoding and
// accumulating them, until the accumulated text contains want or timeout
// elapses.
func collectDataUntil(t *testing.T, ws *websocket.Conn, id, want string, timeout time.Duration) string {
	t.Helper()
	var acc strings.Builder
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(acc.String(), want) {
			return acc.String()
		}
		env := readEnvelopeFor(t, ws, id, time.Until(deadline))
		acc.WriteString(decodeDataEvent(t, env))
	}
}

// readTerminalResponses reads messages off ws until every id in ids has
// produced its own terminal (non-streaming, i.e. Event == "") response,
// returning them keyed by id. Unlike calling readEnvelopeFor once per id
// in sequence, this doesn't assume any particular arrival order between
// two different in-flight requests' terminal responses -- e.g.
// terminal.close and a still-open terminal.attach it ends both become
// ready to send at roughly the same time (both are unblocked by the same
// underlying session finishing), so which one's response actually hits
// the wire first is a goroutine-scheduling detail, not a guarantee.
// readEnvelopeFor alone would silently discard the "other" one while
// waiting for the first, since it drops any message whose id doesn't
// match what it's currently looking for.
func readTerminalResponses(t *testing.T, ws *websocket.Conn, ids []string, timeout time.Duration) map[string]envelope {
	t.Helper()
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make(map[string]envelope, len(ids))
	deadline := time.Now().Add(timeout)
	for len(out) < len(want) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for terminal responses for %v; got so far: %v", ids, out)
		}
		if err := ws.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline() error = %v", err)
		}
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() error = %v (waiting for terminal responses for %v)", err, ids)
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v: %s", err, data)
		}
		if env.Event != "" || !want[env.ID] {
			continue
		}
		out[env.ID] = env
	}
	return out
}

func TestServer_TerminalCreateAttachWrite_RealShell(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "terminal.create", map[string]any{"taskId": task.ID})
	createResp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if createResp.Error != nil {
		t.Fatalf("terminal.create error = %v", createResp.Error.Message)
	}
	var created terminalCreateResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatalf("decode terminal.create result: %v", err)
	}
	if created.TerminalID == "" {
		t.Fatal("terminal.create result did not include a terminalId")
	}

	sendRequest(t, ws, "attach", "terminal.attach", map[string]any{"terminalId": created.TerminalID})

	sendRequest(t, ws, "write", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "echo hello-from-wsapi-test\n",
	})
	writeResp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if writeResp.Error != nil {
		t.Fatalf("terminal.write error = %v", writeResp.Error.Message)
	}

	collectDataUntil(t, ws, "attach", "hello-from-wsapi-test", 5*time.Second)

	sendRequest(t, ws, "close", "terminal.close", map[string]any{"terminalId": created.TerminalID})

	// terminal.close's own response and the still-open attach's terminal
	// response (closing ends it) both become ready at roughly the same
	// time -- see readTerminalResponses's doc comment for why these can't
	// be read in an assumed order.
	responses := readTerminalResponses(t, ws, []string{"close", "attach"}, 5*time.Second)

	closeResp := responses["close"]
	if closeResp.Error != nil {
		t.Fatalf("terminal.close error = %v", closeResp.Error.Message)
	}

	// Closing must end the still-open attach with its terminal result, not
	// leave it hanging.
	term := responses["attach"]
	if term.Error != nil {
		t.Fatalf("terminal.attach after close: error = %v, want a clean terminal result", term.Error.Message)
	}
	var attachResult terminalAttachResult
	if err := json.Unmarshal(term.Result, &attachResult); err != nil {
		t.Fatalf("decode terminal.attach result: %v", err)
	}
	if attachResult.TerminalID != created.TerminalID {
		t.Fatalf("terminal.attach result terminalId = %q, want %q", attachResult.TerminalID, created.TerminalID)
	}
}

// TestServer_TerminalAttach_SecondConnectionSeesBackfill proves a second
// connection (which did not create the session) can attach mid-session and
// see the output produced so far (backfill) followed by new output live --
// the terminal analog of run_test.go's
// TestServer_RunAttach_SecondConnectionMidRun.
func TestServer_TerminalAttach_SecondConnectionSeesBackfill(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")

	creator := dialWS(t, srv, "tok")
	sendRequest(t, creator, "create", "terminal.create", map[string]any{"taskId": task.ID})
	createResp := readEnvelopeFor(t, creator, "create", 5*time.Second)
	var created terminalCreateResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatalf("decode terminal.create result: %v", err)
	}

	sendRequest(t, creator, "attach", "terminal.attach", map[string]any{"terminalId": created.TerminalID})
	sendRequest(t, creator, "write", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "echo before-second-conn\n",
	})
	readEnvelopeFor(t, creator, "write", 5*time.Second)
	collectDataUntil(t, creator, "attach", "before-second-conn", 5*time.Second)

	attacher := dialWS(t, srv, "tok")
	sendRequest(t, attacher, "attach2", "terminal.attach", map[string]any{"terminalId": created.TerminalID})
	backfill := collectDataUntil(t, attacher, "attach2", "before-second-conn", 5*time.Second)
	if !strings.Contains(backfill, "before-second-conn") {
		t.Fatalf("second connection's backfill = %q, want it to contain the earlier output", backfill)
	}

	sendRequest(t, creator, "write2", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "echo after-second-conn\n",
	})
	readEnvelopeFor(t, creator, "write2", 5*time.Second)
	collectDataUntil(t, attacher, "attach2", "after-second-conn", 5*time.Second)

	sendRequest(t, attacher, "close", "terminal.close", map[string]any{"terminalId": created.TerminalID})
	readEnvelopeFor(t, attacher, "close", 5*time.Second)
}

// TestServer_TerminalResize_ReachesPTY proves terminal.resize's cols/rows
// actually reach the PTY, by asking the shell itself (`stty size`, which
// queries the terminal driver directly) rather than trusting a
// no-op/fixed-default implementation.
func TestServer_TerminalResize_ReachesPTY(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "terminal.create", map[string]any{"taskId": task.ID})
	createResp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	var created terminalCreateResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatalf("decode terminal.create result: %v", err)
	}

	sendRequest(t, ws, "attach", "terminal.attach", map[string]any{"terminalId": created.TerminalID})

	const rows, cols = 52, 111
	sendRequest(t, ws, "resize", "terminal.resize", map[string]any{
		"terminalId": created.TerminalID, "cols": cols, "rows": rows,
	})
	resizeResp := readEnvelopeFor(t, ws, "resize", 5*time.Second)
	if resizeResp.Error != nil {
		t.Fatalf("terminal.resize error = %v", resizeResp.Error.Message)
	}

	sendRequest(t, ws, "write", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "stty size\n",
	})
	readEnvelopeFor(t, ws, "write", 5*time.Second)

	want := strconv.Itoa(rows) + " " + strconv.Itoa(cols)
	collectDataUntil(t, ws, "attach", want, 5*time.Second)
}

// TestServer_TerminalAttach_DetachDoesNotCloseSession proves a
// terminal.attach subscriber detaching (here: task.cancel on its own
// request id) leaves the session running -- only terminal.close does
// that. Mirrors run_test.go's TestServer_RunAttach_DetachDoesNotStopRun.
func TestServer_TerminalAttach_DetachDoesNotCloseSession(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "terminal.create", map[string]any{"taskId": task.ID})
	createResp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	var created terminalCreateResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatalf("decode terminal.create result: %v", err)
	}

	sendRequest(t, ws, "attach", "terminal.attach", map[string]any{"terminalId": created.TerminalID})

	// Synchronize with the server actually having dispatched and
	// subscribed the attach request before cancelling it: sending
	// task.cancel immediately after the request, with no read in
	// between, races the dispatch goroutine's own inflight registration
	// (see conn.go's dispatch) -- the same reason run_test.go's analogous
	// detach tests always read at least one event first.
	sendRequest(t, ws, "write", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "echo sync-marker\n",
	})
	readEnvelopeFor(t, ws, "write", 5*time.Second)
	collectDataUntil(t, ws, "attach", "sync-marker", 5*time.Second)

	sendCancel(t, ws, "attach")
	term := readEnvelopeFor(t, ws, "attach", 5*time.Second)
	if term.Error == nil {
		t.Fatal("terminal.attach after task.cancel: error = nil, want an error (the attach request itself was cancelled)")
	}

	// The session itself must still be running: terminal.list from a
	// second connection proves detaching the attach didn't close it.
	checker := dialWS(t, srv, "tok")
	sendRequest(t, checker, "list", "terminal.list", map[string]any{"taskId": task.ID})
	listResp := readEnvelopeFor(t, checker, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("terminal.list error = %v", listResp.Error.Message)
	}
	var sessions []terminal.SessionStatus
	if err := json.Unmarshal(listResp.Result, &sessions); err != nil {
		t.Fatalf("decode terminal.list result: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != created.TerminalID {
		t.Fatalf("terminal.list = %+v, want exactly session %s", sessions, created.TerminalID)
	}
	if sessions[0].Status != terminal.StatusRunning {
		t.Fatalf("terminal.list status = %q after detaching an attach, want still %q", sessions[0].Status, terminal.StatusRunning)
	}

	// Clean up: actually close it so the test doesn't leak the shell
	// process past this test's lifetime.
	sendRequest(t, checker, "close", "terminal.close", map[string]any{"terminalId": created.TerminalID})
	readEnvelopeFor(t, checker, "close", 5*time.Second)
}

// TestServer_TerminalClose_ActuallyClosesSession proves terminal.close's
// effect is real and visible cross-connection: the session's status flips
// to closed via terminal.list, and a write to it afterward is rejected --
// not just "the close call itself returned no error". The deeper OS-level
// "the process is actually gone" check lives in
// internal/terminal/registry_test.go, which has direct access to the
// spawned pid; this test proves the wire-level contract on top of it.
func TestServer_TerminalClose_ActuallyClosesSession(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "terminal.create", map[string]any{"taskId": task.ID})
	createResp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	var created terminalCreateResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatalf("decode terminal.create result: %v", err)
	}

	sendRequest(t, ws, "close", "terminal.close", map[string]any{"terminalId": created.TerminalID})
	closeResp := readEnvelopeFor(t, ws, "close", 5*time.Second)
	if closeResp.Error != nil {
		t.Fatalf("terminal.close error = %v", closeResp.Error.Message)
	}

	sendRequest(t, ws, "list", "terminal.list", map[string]any{"taskId": task.ID})
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var sessions []terminal.SessionStatus
	if err := json.Unmarshal(listResp.Result, &sessions); err != nil {
		t.Fatalf("decode terminal.list result: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Status != terminal.StatusClosed {
		t.Fatalf("terminal.list = %+v, want exactly one closed session", sessions)
	}

	sendRequest(t, ws, "write", "terminal.write", map[string]any{
		"terminalId": created.TerminalID, "data": "echo x\n",
	})
	writeResp := readEnvelopeFor(t, ws, "write", 5*time.Second)
	if writeResp.Error == nil {
		t.Fatal("terminal.write after terminal.close: error = nil, want an error")
	}

	// terminal.close on an already-closed session is a no-op, not an
	// error (mirrors run.stop's already-terminal contract).
	sendRequest(t, ws, "close2", "terminal.close", map[string]any{"terminalId": created.TerminalID})
	closeResp2 := readEnvelopeFor(t, ws, "close2", 5*time.Second)
	if closeResp2.Error != nil {
		t.Fatalf("terminal.close on an already-closed session: error = %v, want nil", closeResp2.Error.Message)
	}
}

// TestServer_TerminalCreate_UnknownTask proves terminal.create fails
// synchronously (and doesn't leak a spawned shell) for a taskId the
// workspace manager doesn't know about.
func TestServer_TerminalCreate_UnknownTask(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "create", "terminal.create", map[string]any{"taskId": 99999})
	resp := readEnvelopeFor(t, ws, "create", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("terminal.create for an unknown taskId: error = nil, want an error")
	}
}

// TestServer_TerminalList_FiltersByTask proves terminal.list only returns
// sessions belonging to the requested task, not every session the
// Registry knows about.
func TestServer_TerminalList_FiltersByTask(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	taskA := newTestTask(t, wm, "")
	taskB := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "createA", "terminal.create", map[string]any{"taskId": taskA.ID})
	createAResp := readEnvelopeFor(t, ws, "createA", 5*time.Second)
	var createdA terminalCreateResult
	if err := json.Unmarshal(createAResp.Result, &createdA); err != nil {
		t.Fatalf("decode terminal.create (A) result: %v", err)
	}

	sendRequest(t, ws, "createB", "terminal.create", map[string]any{"taskId": taskB.ID})
	createBResp := readEnvelopeFor(t, ws, "createB", 5*time.Second)
	var createdB terminalCreateResult
	if err := json.Unmarshal(createBResp.Result, &createdB); err != nil {
		t.Fatalf("decode terminal.create (B) result: %v", err)
	}

	sendRequest(t, ws, "listA", "terminal.list", map[string]any{"taskId": taskA.ID})
	listAResp := readEnvelopeFor(t, ws, "listA", 5*time.Second)
	var sessionsA []terminal.SessionStatus
	if err := json.Unmarshal(listAResp.Result, &sessionsA); err != nil {
		t.Fatalf("decode terminal.list (A) result: %v", err)
	}
	if len(sessionsA) != 1 || sessionsA[0].ID != createdA.TerminalID {
		t.Fatalf("terminal.list for task A = %+v, want exactly its own session %s", sessionsA, createdA.TerminalID)
	}

	sendRequest(t, ws, "closeA", "terminal.close", map[string]any{"terminalId": createdA.TerminalID})
	readEnvelopeFor(t, ws, "closeA", 5*time.Second)
	sendRequest(t, ws, "closeB", "terminal.close", map[string]any{"terminalId": createdB.TerminalID})
	readEnvelopeFor(t, ws, "closeB", 5*time.Second)
}
