package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
)

// TestServer_RunAttach_SecondConnectionMidRun proves a second connection
// (one that did not call task.prompt) can attach mid-run and receive the
// remaining live events plus the terminal result, driven entirely by
// run.attach rather than task.prompt.
func TestServer_RunAttach_SecondConnectionMidRun(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")

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

// TestServer_RunAttach_AlreadyFinished proves attaching to an
// already-finished run gets an immediate terminal response (full
// backfill, no live events, no hang) rather than blocking forever.
func TestServer_RunAttach_AlreadyFinished(t *testing.T) {
	t.Parallel()
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")

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
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")

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
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")

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
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
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
	if tail.Events[0] != full.Events[len(full.Events)-1] {
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
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
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
	wm := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, "tok")
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
