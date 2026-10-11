package wsapi

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// waitForRunEvents polls run.logs until the run has recorded at least one
// event -- the signal that its ACP session is live, so config-option calls
// actually reach it.
func waitForRunEvents(t *testing.T, ws *websocket.Conn, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sendRequest(t, ws, "poll", "run.logs", map[string]any{"runId": runID})
		env := readEnvelopeFor(t, ws, "poll", time.Until(deadline))
		if env.Error != nil {
			t.Fatalf("run.logs error = %v", env.Error.Message)
		}
		var logs runLogsResult
		mustDecode(t, env.Result, &logs)
		if len(logs.Events) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %s first event", runID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// currentValueOf returns the raw currentValue run.listConfigOptions reports
// for configID, failing the test if the option is missing.
func currentValueOf(t *testing.T, ws *websocket.Conn, runID, configID string) string {
	t.Helper()
	sendRequest(t, ws, "list-"+configID, "run.listConfigOptions", map[string]any{"runId": runID})
	env := readEnvelopeFor(t, ws, "list-"+configID, 5*time.Second)
	if env.Error != nil {
		t.Fatalf("run.listConfigOptions error = %v", env.Error.Message)
	}
	var list runConfigOptionsResult
	mustDecode(t, env.Result, &list)
	for _, o := range list.Options {
		if o.ConfigID == configID {
			return string(o.CurrentValue)
		}
	}
	t.Fatalf("option %q not listed: %+v", configID, list.Options)
	return ""
}

// TestRunSetConfigOption_OrchestratorRejectsModeCategory proves a
// non-human source cannot set a category-"mode" option even when the
// value names a non-auto-approving choice (fail-closed, ADR-0019 #6), and
// that the live option is left untouched.
func TestRunSetConfigOption_OrchestratorRejectsModeCategory(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "live-opts", "modes:config"}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	waitForRunEvents(t, ws, started.RunID)

	sendRequest(t, ws, "set", "run.setConfigOption", map[string]any{
		"runId": started.RunID, "configId": "mode", "value": "accept_edits", "source": "orchestrator",
	})
	resp := readEnvelopeFor(t, ws, "set", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "thought_level or model") {
		t.Fatalf("error = %+v, want category allowlist rejection", resp.Error)
	}
	if got := currentValueOf(t, ws, started.RunID, "mode"); got != `"default"` {
		t.Errorf("mode currentValue = %s after rejected set, want unchanged \"default\"", got)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetConfigOption_OrchestratorRejectsUncategorized proves a
// non-human source cannot set an option whose category is empty or whose
// configId is unknown -- fail-closed, not fail-open.
func TestRunSetConfigOption_OrchestratorRejectsUncategorized(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "live-opts", "modes:config"}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	waitForRunEvents(t, ws, started.RunID)

	for name, tc := range map[string]struct {
		configId string
		wantErr  string
	}{
		// web-search has no category at all.
		"empty category": {"web-search", "thought_level or model"},
		"unknown id":     {"no-such-option", "unknown configId"},
	} {
		sendRequest(t, ws, name, "run.setConfigOption", map[string]any{
			"runId": started.RunID, "configId": tc.configId, "value": "high", "source": "orchestrator",
		})
		resp := readEnvelopeFor(t, ws, name, 5*time.Second)
		if resp.Error == nil || !strings.Contains(resp.Error.Message, tc.wantErr) {
			t.Errorf("%s: error = %+v, want containing %q", name, resp.Error, tc.wantErr)
		}
	}
	if got := currentValueOf(t, ws, started.RunID, "web-search"); got != "true" {
		t.Errorf("web-search currentValue = %s after rejected set, want unchanged true", got)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetConfigOption_OrchestratorAllowsThoughtLevel proves a
// non-human source may set a thought_level option, and the change lands
// on the live session.
func TestRunSetConfigOption_OrchestratorAllowsThoughtLevel(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	waitForRunEvents(t, ws, started.RunID)

	sendRequest(t, ws, "set", "run.setConfigOption", map[string]any{
		"runId": started.RunID, "configId": "thinking-level", "value": "high", "source": "orchestrator",
	})
	resp := readEnvelopeFor(t, ws, "set", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.setConfigOption error = %v", resp.Error.Message)
	}
	var set runConfigOptionsResult
	mustDecode(t, resp.Result, &set)
	if len(set.Options) != 1 || string(set.Options[0].CurrentValue) != `{"type":"id","value":"high"}` {
		t.Fatalf("options = %+v, want thinking-level set to high", set.Options)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetConfigOption_HumanUnchanged proves a human source (explicit
// or absent) keeps today's behavior: the uncategorized web-search option
// is still settable.
func TestRunSetConfigOption_HumanUnchanged(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "live-opts", "modes:config"}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	waitForRunEvents(t, ws, started.RunID)

	// Ordered, alternating values so each case proves its own change.
	for _, tc := range []struct{ name, source, value string }{
		{"explicit human", "human", "false"},
		{"absent", "", "true"},
	} {
		name := tc.name
		params := map[string]any{
			"runId": started.RunID, "configId": "web-search", "value": tc.value,
		}
		if tc.source != "" {
			params["source"] = tc.source
		}
		sendRequest(t, ws, name, "run.setConfigOption", params)
		if resp := readEnvelopeFor(t, ws, name, 5*time.Second); resp.Error != nil {
			t.Errorf("%s: run.setConfigOption error = %v", name, resp.Error.Message)
		}
		if got := currentValueOf(t, ws, started.RunID, "web-search"); !strings.Contains(got, tc.value) {
			t.Errorf("%s: web-search currentValue = %s, want %s", name, got, tc.value)
		}
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}
