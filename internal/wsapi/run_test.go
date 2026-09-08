package wsapi

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestServer_RunAttach_SecondConnectionMidRun proves a second connection
// (one that did not call task.prompt) can attach mid-run and receive the
// remaining live events plus the terminal result, driven entirely by
// run.attach rather than task.prompt.
func TestServer_RunAttach_SecondConnectionMidRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	first := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if first.Event != "chunk" {
		t.Fatalf("first message = %+v, want a chunk event", first)
	}

	sendRequest(t, starter, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, starter, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("run.list error = %v", listResp.Error.Message)
	}
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d runs, want 1: %+v", len(summaries), summaries)
	}
	runID := summaries[0].ID

	attacher := dialWS(t, srv, "tok")
	sendRequest(t, attacher, "attach", "run.attach", map[string]any{"runId": runID})

	var texts []string
	var term envelope
	deadline := time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, attacher, "attach", time.Until(deadline))
		if env.Event == "chunk" {
			var p taskChunkParams
			if err := json.Unmarshal(env.Params, &p); err != nil {
				t.Fatalf("decode chunk params: %v", err)
			}
			texts = append(texts, p.Text)
			continue
		}
		term = env
		break
	}

	if term.Error != nil {
		t.Fatalf("run.attach terminal error = %v, want success", term.Error.Message)
	}
	var result taskPromptResult
	if err := json.Unmarshal(term.Result, &result); err != nil {
		t.Fatalf("decode run.attach result: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if len(texts) == 0 || texts[len(texts)-1] != "world!" {
		t.Fatalf("got chunks %v, want the stream to end with %q", texts, "world!")
	}
}

// TestServer_RunStart_NotStoppedByOwnRequestCompletion proves run.start
// decouples "start a run" from "watch a run": the request that started the
// run has already completed (its terminal {runId} response arrived) and
// its connection is then closed entirely, yet the run keeps going and
// remains reachable -- via run.attach and run.list -- from a second
// connection. This is the property task.prompt deliberately does not have
// (its own request context going Done stops the run; see
// handleTaskPrompt's doc comment), which is exactly why the CLI's
// foreground `task send` needs run.start instead.
func TestServer_RunStart_NotStoppedByOwnRequestCompletion(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "run.start", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	term := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if term.Event != "" {
		t.Fatalf("run.start emitted an event %+v, want no streaming, just a terminal result", term)
	}
	if term.Error != nil {
		t.Fatalf("run.start error = %v", term.Error.Message)
	}
	var result runStartResult
	if err := json.Unmarshal(term.Result, &result); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}
	if result.RunID == "" {
		t.Fatal("run.start result did not include a runId")
	}

	// The starting request already completed above; closing its connection
	// entirely must not stop the run.
	if err := starter.Close(); err != nil {
		t.Fatalf("starter.Close() error = %v", err)
	}

	watcher := dialWS(t, srv, "tok")
	sendRequest(t, watcher, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, watcher, "list", 5*time.Second)
	if listResp.Error != nil {
		t.Fatalf("run.list error = %v", listResp.Error.Message)
	}
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != result.RunID {
		t.Fatalf("run.list = %+v, want exactly the run started by run.start (%s)", summaries, result.RunID)
	}
	if summaries[0].Status != runs.StatusRunning {
		t.Fatalf("run.list status = %q, want %q (starter closing must not have stopped it)", summaries[0].Status, runs.StatusRunning)
	}

	sendRequest(t, watcher, "attach", "run.attach", map[string]any{"runId": result.RunID})
	chunk := readEnvelopeFor(t, watcher, "attach", 5*time.Second)
	if chunk.Event != "chunk" {
		t.Fatalf("run.attach first message = %+v, want a chunk event", chunk)
	}

	sendRequest(t, watcher, "logs", "run.logs", map[string]any{"runId": result.RunID})
	logsResp := readEnvelopeFor(t, watcher, "logs", 5*time.Second)
	if logsResp.Error != nil {
		t.Fatalf("run.logs error = %v", logsResp.Error.Message)
	}
	var logs runLogsResult
	if err := json.Unmarshal(logsResp.Result, &logs); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if logs.Status != string(runs.StatusRunning) {
		t.Fatalf("run.logs status = %q, want still %q", logs.Status, runs.StatusRunning)
	}

	// Clean up: stop the run so the fake agent subprocess doesn't outlive
	// this test. The still-open run.attach observes the stop as its own
	// terminal (error) response, same as any other subscriber.
	sendRequest(t, watcher, "stop", "run.stop", map[string]any{"runId": result.RunID})
	if resp := readEnvelopeFor(t, watcher, "stop", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.stop error = %v", resp.Error.Message)
	}
	readEnvelopeFor(t, watcher, "attach", 5*time.Second)
}

// TestServer_RunAttach_AlreadyFinished proves attaching to an
// already-finished run gets an immediate terminal response (full
// backfill, no live events, no hang) rather than blocking forever.
func TestServer_RunAttach_AlreadyFinished(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	var runID string
	deadline := time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, starter, "1", time.Until(deadline))
		if env.Event != "" {
			continue
		}
		if env.Error != nil {
			t.Fatalf("task.prompt error = %v", env.Error.Message)
		}
		var result taskPromptResult
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("decode task.prompt result: %v", err)
		}
		runID = result.RunID
		break
	}
	if runID == "" {
		t.Fatal("task.prompt result did not include a runId")
	}

	attacher := dialWS(t, srv, "tok")
	sendRequest(t, attacher, "attach", "run.attach", map[string]any{"runId": runID})
	var term envelope
	deadline = time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, attacher, "attach", time.Until(deadline))
		if env.Event != "" {
			continue
		}
		term = env
		break
	}
	if term.Error != nil {
		t.Fatalf("run.attach on a finished run: error = %v, want success", term.Error.Message)
	}
	var result taskPromptResult
	if err := json.Unmarshal(term.Result, &result); err != nil {
		t.Fatalf("decode run.attach result: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
}

// TestServer_RunStop_CrossConnection proves run.stop from a connection
// that did not start the run actually cancels it -- the run.stop-specific
// path through Registry.Stop, distinct from task.cancel's same-connection
// mechanism.
func TestServer_RunStop_CrossConnection(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	chunk := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if chunk.Event != "chunk" {
		t.Fatalf("first message = %+v, want a chunk event", chunk)
	}

	sendRequest(t, starter, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, starter, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d runs, want 1", len(summaries))
	}
	runID := summaries[0].ID

	stopper := dialWS(t, srv, "tok")
	start := time.Now()
	sendRequest(t, stopper, "stop", "run.stop", map[string]any{"runId": runID})
	stopResp := readEnvelopeFor(t, stopper, "stop", 5*time.Second)
	if stopResp.Error != nil {
		t.Fatalf("run.stop error = %v", stopResp.Error.Message)
	}

	// task.prompt's own connection must observe the run end (as an error
	// terminal response, matching task.prompt's cancel semantics) promptly
	// -- proving run.stop actually reached the subprocess rather than just
	// flipping a flag nobody acted on.
	term := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("run.stop took %s to take effect, want well under the fake agent's 1h hang", elapsed)
	}
	if term.Error == nil {
		t.Fatal("task.prompt after run.stop from another connection: error = nil, want an error")
	}

	sendRequest(t, stopper, "logs", "run.logs", map[string]any{"runId": runID})
	logsResp := readEnvelopeFor(t, stopper, "logs", 5*time.Second)
	if logsResp.Error != nil {
		t.Fatalf("run.logs error = %v", logsResp.Error.Message)
	}
	var logs runLogsResult
	if err := json.Unmarshal(logsResp.Result, &logs); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if logs.Status != string(runs.StatusStopped) {
		t.Fatalf("run.logs status = %q, want %q", logs.Status, runs.StatusStopped)
	}
}

// TestServer_RunAttach_DetachDoesNotStopRun proves a run.attach subscriber
// disconnecting (here: its own request cancelled via task.cancel) leaves
// the run running -- only run.stop does that.
func TestServer_RunAttach_DetachDoesNotStopRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	readEnvelopeFor(t, starter, "1", 5*time.Second) // first chunk

	sendRequest(t, starter, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, starter, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	watcher := dialWS(t, srv, "tok")
	sendRequest(t, watcher, "attach", "run.attach", map[string]any{"runId": runID})
	attachChunk := readEnvelopeFor(t, watcher, "attach", 5*time.Second)
	if attachChunk.Event != "chunk" {
		t.Fatalf("run.attach first message = %+v, want a chunk event", attachChunk)
	}

	sendCancel(t, watcher, "attach")
	term := readEnvelopeFor(t, watcher, "attach", 5*time.Second)
	if term.Error == nil {
		t.Fatal("run.attach after task.cancel: error = nil, want an error (the attach request itself was cancelled)")
	}

	// The run itself must still be running: a fresh run.logs from a third
	// connection proves detaching the attach didn't stop it.
	checker := dialWS(t, srv, "tok")
	sendRequest(t, checker, "logs", "run.logs", map[string]any{"runId": runID})
	logsResp := readEnvelopeFor(t, checker, "logs", 5*time.Second)
	if logsResp.Error != nil {
		t.Fatalf("run.logs error = %v", logsResp.Error.Message)
	}
	var logs runLogsResult
	if err := json.Unmarshal(logsResp.Result, &logs); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if logs.Status != string(runs.StatusRunning) {
		t.Fatalf("run.logs status = %q after detaching a run.attach, want still %q", logs.Status, runs.StatusRunning)
	}

	// Clean up: actually stop the run so the test doesn't leak the
	// fake agent subprocess past this test's lifetime.
	sendRequest(t, checker, "stop", "run.stop", map[string]any{"runId": runID})
	readEnvelopeFor(t, checker, "stop", 5*time.Second)
}

// TestServer_RunLogs_Tail proves run.logs honors tail, returning only the
// last N events instead of the full history.
func TestServer_RunLogs_Tail(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	var runID string
	deadline := time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, ws, "1", time.Until(deadline))
		if env.Event != "" {
			continue
		}
		var result taskPromptResult
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("decode task.prompt result: %v", err)
		}
		runID = result.RunID
		break
	}

	sendRequest(t, ws, "full", "run.logs", map[string]any{"runId": runID})
	fullResp := readEnvelopeFor(t, ws, "full", 5*time.Second)
	var full runLogsResult
	if err := json.Unmarshal(fullResp.Result, &full); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if len(full.Events) != 3 {
		t.Fatalf("full run.logs events = %d, want 3", len(full.Events))
	}

	sendRequest(t, ws, "tail", "run.logs", map[string]any{"runId": runID, "tail": 1})
	tailResp := readEnvelopeFor(t, ws, "tail", 5*time.Second)
	var tail runLogsResult
	if err := json.Unmarshal(tailResp.Result, &tail); err != nil {
		t.Fatalf("decode run.logs (tail) result: %v", err)
	}
	if len(tail.Events) != 1 {
		t.Fatalf("tailed run.logs events = %d, want 1", len(tail.Events))
	}
	if !reflect.DeepEqual(tail.Events[0], full.Events[len(full.Events)-1]) {
		t.Fatalf("tailed event = %+v, want the last full event %+v", tail.Events[0], full.Events[len(full.Events)-1])
	}
	if tail.Status != string(runs.StatusDone) {
		t.Fatalf("run.logs (tail) status = %q, want %q", tail.Status, runs.StatusDone)
	}
}

// TestServer_TaskCancel_OnlyAffectsItsOwnRequest proves task.cancel
// targeting a run.attach request doesn't touch a task.prompt-started run
// it's merely watching -- task.cancel is scoped to a request id on one
// connection, not to a run.
func TestServer_TaskCancel_OnlyAffectsItsOwnRequest(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	readEnvelopeFor(t, ws, "1", 5*time.Second) // first chunk

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	sendRequest(t, ws, "attach", "run.attach", map[string]any{"runId": runID})
	readEnvelopeFor(t, ws, "attach", 5*time.Second) // backfilled chunk

	// Cancel the *attach* request, not the original task.prompt one.
	sendCancel(t, ws, "attach")
	term := readEnvelopeFor(t, ws, "attach", 5*time.Second)
	if term.Error == nil {
		t.Fatal("run.attach after its own task.cancel: error = nil, want an error")
	}

	// task.prompt's own request must still be in flight -- unaffected by
	// cancelling the unrelated attach request id.
	sendRequest(t, ws, "logs", "run.logs", map[string]any{"runId": runID})
	logsResp := readEnvelopeFor(t, ws, "logs", 5*time.Second)
	var logs runLogsResult
	if err := json.Unmarshal(logsResp.Result, &logs); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	if logs.Status != string(runs.StatusRunning) {
		t.Fatalf("run.logs status = %q, want still %q", logs.Status, runs.StatusRunning)
	}

	sendCancel(t, ws, "1")
	promptTerm := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if promptTerm.Error == nil {
		t.Fatal("task.prompt after its own task.cancel: error = nil, want an error")
	}
}

// TestServer_ConnectionClose_StopsTaskPromptRun proves a task.prompt
// run's underlying subprocess doesn't outlive the connection that started
// it closing -- task.prompt's implicit attach stops the run on context
// cancellation (see handleTaskPrompt), and a closed WebSocket connection
// cancels its in-flight requests' contexts (conn.serve) the same way
// task.cancel does.
func TestServer_ConnectionClose_StopsTaskPromptRun(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	readEnvelopeFor(t, ws, "1", 5*time.Second) // first chunk

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	if err := ws.Close(); err != nil {
		t.Fatalf("ws.Close() error = %v", err)
	}

	checker := dialWS(t, srv, "tok")
	deadline := time.Now().Add(5 * time.Second)
	for {
		sendRequest(t, checker, "logs", "run.logs", map[string]any{"runId": runID})
		logsResp := readEnvelopeFor(t, checker, "logs", time.Until(deadline))
		if logsResp.Error != nil {
			t.Fatalf("run.logs error = %v", logsResp.Error.Message)
		}
		var logs runLogsResult
		if err := json.Unmarshal(logsResp.Result, &logs); err != nil {
			t.Fatalf("decode run.logs result: %v", err)
		}
		if logs.Status != string(runs.StatusRunning) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run's status is still %q 5s after its starting connection closed, want it stopped", logs.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestServer_RunRespondPermission_CrossConnection proves run.respondPermission
// from a connection that did not start (or attach to) the run answers its
// pending permission request and the blocked turn continues, reflecting
// that exact choice -- mirroring run.stop's existing cross-connection test
// pattern (TestServer_RunStop_CrossConnection above). It also proves the
// request/resolution round trip is visible on the watching connection as
// its own named events ("permission_request"/"permission_resolved"),
// distinct from a plain "chunk".
func TestServer_RunRespondPermission_CrossConnection(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")

	starter := dialWS(t, srv, "tok")
	sendRequest(t, starter, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})

	permEnv := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if permEnv.Event != "permission_request" {
		t.Fatalf("first message = %+v, want a permission_request event", permEnv)
	}
	var req permissionRequestParams
	if err := json.Unmarshal(permEnv.Params, &req); err != nil {
		t.Fatalf("decode permission_request params: %v", err)
	}
	if req.RequestID == "" {
		t.Fatal("permission_request event's requestId is empty")
	}
	if req.Summary != "Run a risky command" {
		t.Fatalf("permission_request summary = %q, want %q", req.Summary, "Run a risky command")
	}
	if len(req.Options) != 2 || req.Options[0].ID != "allow-1" || req.Options[1].ID != "deny-1" {
		t.Fatalf("permission_request options = %+v, want allow-1 then deny-1", req.Options)
	}

	sendRequest(t, starter, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, starter, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	// A different connection than the one watching the run answers it.
	responder := dialWS(t, srv, "tok")
	sendRequest(t, responder, "respond", "run.respondPermission", map[string]any{
		"runId": runID, "requestId": req.RequestID, "optionId": "allow-1",
	})
	respondResp := readEnvelopeFor(t, responder, "respond", 5*time.Second)
	if respondResp.Error != nil {
		t.Fatalf("run.respondPermission error = %v", respondResp.Error.Message)
	}

	resolvedEnv := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if resolvedEnv.Event != "permission_resolved" {
		t.Fatalf("message after respondPermission = %+v, want a permission_resolved event", resolvedEnv)
	}
	var resolved permissionResolvedParams
	if err := json.Unmarshal(resolvedEnv.Params, &resolved); err != nil {
		t.Fatalf("decode permission_resolved params: %v", err)
	}
	if resolved.RequestID != req.RequestID || resolved.OptionID != "allow-1" {
		t.Fatalf("permission_resolved = %+v, want requestId=%q optionId=%q", resolved, req.RequestID, "allow-1")
	}

	finalChunk := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if finalChunk.Event != "chunk" {
		t.Fatalf("message after permission_resolved = %+v, want the agent's post-decision chunk", finalChunk)
	}
	var chunkParams taskChunkParams
	if err := json.Unmarshal(finalChunk.Params, &chunkParams); err != nil {
		t.Fatalf("decode chunk params: %v", err)
	}
	if chunkParams.Text != "chose:allow-1" {
		t.Fatalf("final chunk text = %q, want %q (the fake agent's echo of the chosen option)", chunkParams.Text, "chose:allow-1")
	}

	term := readEnvelopeFor(t, starter, "1", 5*time.Second)
	if term.Error != nil {
		t.Fatalf("task.prompt terminal error = %v, want success", term.Error.Message)
	}
	var result taskPromptResult
	if err := json.Unmarshal(term.Result, &result); err != nil {
		t.Fatalf("decode task.prompt result: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
}

// TestServer_RunLogs_ShowsPermissionRequestAndResolved proves run.logs
// renders both new event types with their real data (requestId/summary/
// options, and requestId/optionId respectively) rather than falling
// through toRunLogEvent's default branch, which would otherwise silently
// mis-render either one as an empty/wrong "chunk" entry -- a real, specific
// risk this test exists to rule out, not just exercise the happy path.
func TestServer_RunLogs_ShowsPermissionRequestAndResolved(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "task.prompt", map[string]any{
		"taskId": task.ID, "provider": "glm", "prompt": "hi",
	})
	permEnv := readEnvelopeFor(t, ws, "1", 5*time.Second)
	var req permissionRequestParams
	if err := json.Unmarshal(permEnv.Params, &req); err != nil {
		t.Fatalf("decode permission_request params: %v", err)
	}

	sendRequest(t, ws, "list", "run.list", nil)
	listResp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var summaries []runs.RunSummary
	if err := json.Unmarshal(listResp.Result, &summaries); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	runID := summaries[0].ID

	// run.logs while the request is still pending (unanswered): must show
	// the permission_request entry correctly, with no resolution yet.
	sendRequest(t, ws, "logs1", "run.logs", map[string]any{"runId": runID})
	logsResp1 := readEnvelopeFor(t, ws, "logs1", 5*time.Second)
	var logs1 runLogsResult
	if err := json.Unmarshal(logsResp1.Result, &logs1); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	var sawPending bool
	for _, e := range logs1.Events {
		if e.Type == "permission_resolved" {
			t.Fatalf("run.logs shows a permission_resolved entry before the request was answered: %+v", logs1.Events)
		}
		if e.Type == "permission_request" {
			sawPending = true
			if e.RequestID != req.RequestID {
				t.Fatalf("run.logs permission_request RequestID = %q, want %q", e.RequestID, req.RequestID)
			}
			if e.Summary != "Run a risky command" {
				t.Fatalf("run.logs permission_request Summary = %q, want %q", e.Summary, "Run a risky command")
			}
			if len(e.Options) != 2 {
				t.Fatalf("run.logs permission_request Options = %+v, want 2", e.Options)
			}
			if e.Text != "" {
				t.Fatalf("run.logs permission_request Text = %q, want empty (not mis-rendered as a chunk)", e.Text)
			}
		}
	}
	if !sawPending {
		t.Fatalf("run.logs does not show the pending permission_request: %+v", logs1.Events)
	}

	sendRequest(t, ws, "respond", "run.respondPermission", map[string]any{
		"runId": runID, "requestId": req.RequestID, "optionId": "deny-1",
	})
	if resp := readEnvelopeFor(t, ws, "respond", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.respondPermission error = %v", resp.Error.Message)
	}

	// Drain the rest of the streamed task.prompt turn.
	deadline := time.Now().Add(5 * time.Second)
	for {
		env := readEnvelopeFor(t, ws, "1", time.Until(deadline))
		if env.Event == "" {
			break
		}
	}

	// run.logs after the run finished: the permission_resolved entry must
	// carry the exact requestId/optionId, not an empty/wrong chunk.
	sendRequest(t, ws, "logs2", "run.logs", map[string]any{"runId": runID})
	logsResp2 := readEnvelopeFor(t, ws, "logs2", 5*time.Second)
	var logs2 runLogsResult
	if err := json.Unmarshal(logsResp2.Result, &logs2); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	var sawResolved bool
	for _, e := range logs2.Events {
		if e.Type == "permission_resolved" {
			sawResolved = true
			if e.RequestID != req.RequestID || e.OptionID != "deny-1" {
				t.Fatalf("run.logs permission_resolved = %+v, want RequestID=%q OptionID=%q", e, req.RequestID, "deny-1")
			}
			if e.Text != "" {
				t.Fatalf("run.logs permission_resolved Text = %q, want empty (not mis-rendered as a chunk)", e.Text)
			}
		}
	}
	if !sawResolved {
		t.Fatalf("run.logs does not show the permission_resolved entry after answering: %+v", logs2.Events)
	}
}

// TestServer_RunStart_KimiProvider proves the wire path recognizes
// provider "kimi" end to end: wsapi decodes p.Provider as a bare
// taskrunner.Provider string with no allowlist of its own (see
// handleRunStart), so this exercises the real dependency -- that
// taskrunner.ProviderKimi is wired all the way through runs.Registry.Start
// -> taskrunner.Runner.RunPrompt -- not just taskrunner in isolation.
func TestServer_RunStart_KimiProvider(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderKimi, []string{fakeACPAgentPath}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "run.start", map[string]any{
		"taskId": task.ID, "provider": "kimi", "prompt": "hi",
	})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	sendRequest(t, ws, "2", "run.logs", map[string]any{"runId": started.RunID})
	deadline := time.Now().Add(5 * time.Second)
	var logs runLogsResult
	for {
		env := readEnvelopeFor(t, ws, "2", time.Until(deadline))
		if err := json.Unmarshal(env.Result, &logs); err != nil {
			t.Fatalf("decode run.logs result: %v", err)
		}
		if logs.Status == string(runs.StatusDone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for kimi run to finish, last status %q", logs.Status)
		}
		sendRequest(t, ws, "2", "run.logs", map[string]any{"runId": started.RunID})
	}
	if len(logs.Events) != 3 {
		t.Fatalf("run.logs events = %d, want 3: %+v", len(logs.Events), logs.Events)
	}
}

// TestServer_RunStart_CodexNativeProvider proves the wire path recognizes
// provider "codex-native" end to end, mirroring
// TestServer_RunStart_KimiProvider but through Runner's non-ACP
// runCodexNative path.
func TestServer_RunStart_CodexNativeProvider(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, taskrunner.WithCodexCommand([]string{fakeCodexAgentPath}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "run.start", map[string]any{
		"taskId": task.ID, "provider": "codex-native", "prompt": "hi",
	})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start error = %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode run.start result: %v", err)
	}

	sendRequest(t, ws, "2", "run.logs", map[string]any{"runId": started.RunID})
	deadline := time.Now().Add(5 * time.Second)
	var logs runLogsResult
	for {
		env := readEnvelopeFor(t, ws, "2", time.Until(deadline))
		if err := json.Unmarshal(env.Result, &logs); err != nil {
			t.Fatalf("decode run.logs result: %v", err)
		}
		if logs.Status == string(runs.StatusDone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for codex-native run to finish, last status %q", logs.Status)
		}
		sendRequest(t, ws, "2", "run.logs", map[string]any{"runId": started.RunID})
	}
	if len(logs.Events) != 3 {
		t.Fatalf("run.logs events = %d, want 3: %+v", len(logs.Events), logs.Events)
	}
}
