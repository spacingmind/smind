package wsapi

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// readRunList decodes a run.list response into RunSummary, asserting on
// real run state rather than the RPC's echo.
func readRunList(t *testing.T, ws *websocket.Conn) []runs.RunSummary {
	t.Helper()
	sendRequest(t, ws, "list", "run.list", nil)
	var summaries []runs.RunSummary
	mustDecode(t, readEnvelopeFor(t, ws, "list", 5*time.Second).Result, &summaries)
	return summaries
}

// TestRunSetPermissionMode_OrchestratorCannotPickAutoApproving proves a
// non-human source cannot switch a run into an auto-approving mode
// (ADR-0019 decision 6): the fake agent advertises bypass_permissions,
// and source=orchestrator is rejected with the ADR cited, while the run
// stays in its current (asks-a-human) mode.
func TestRunSetPermissionMode_OrchestratorCannotPickAutoApproving(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})

	sendRequest(t, ws, "switch", "run.setPermissionMode", map[string]any{
		"runId": started.RunID, "modeId": "bypass_permissions", "source": "orchestrator",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "ADR-0019") {
		t.Fatalf("error = %+v, want ADR-0019 rejection", resp.Error)
	}
	if summaries := readRunList(t, ws); len(summaries) != 1 || summaries[0].PermissionMode == "bypass_permissions" {
		t.Fatalf("run.list = %+v, want mode unchanged", summaries)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetPermissionMode_OrchestratorCannotSetAutoAccept proves a
// non-human source cannot flip autoAccept=true on a live ACP run; the
// run's stored AutoAccept stays false.
func TestRunSetPermissionMode_OrchestratorCannotSetAutoAccept(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})

	sendRequest(t, ws, "switch", "run.setPermissionMode", map[string]any{
		"runId": started.RunID, "autoAccept": true, "source": "orchestrator",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "ADR-0019") {
		t.Fatalf("error = %+v, want ADR-0019 rejection", resp.Error)
	}
	if summaries := readRunList(t, ws); len(summaries) != 1 || summaries[0].AutoAccept {
		t.Fatalf("run.list = %+v, want AutoAccept still false", summaries)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetPermissionMode_HumanUnchanged proves source=human (and the
// absent-source default) keeps today's behavior: the human caller may
// pick bypass_permissions and set autoAccept=true. The mode switch
// retries briefly because the ACP session comes up shortly after
// run.start returns.
func TestRunSetPermissionMode_HumanUnchanged(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "modes:session"}))
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	started := startRun(t, ws, map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})

	deadline := time.Now().Add(5 * time.Second)
	for {
		sendRequest(t, ws, "switch", "run.setPermissionMode", map[string]any{
			"runId": started.RunID, "modeId": "bypass_permissions", "source": "human",
		})
		resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
		if resp.Error == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run.setPermissionMode(human, bypass) never succeeded: %v", resp.Error.Message)
		}
		time.Sleep(50 * time.Millisecond)
	}

	sendRequest(t, ws, "auto", "run.setPermissionMode", map[string]any{"runId": started.RunID, "autoAccept": true})
	if resp := readEnvelopeFor(t, ws, "auto", 5*time.Second); resp.Error != nil {
		t.Fatalf("run.setPermissionMode(autoAccept) error = %v", resp.Error.Message)
	}

	if summaries := readRunList(t, ws); len(summaries) != 1 || !summaries[0].AutoAccept || summaries[0].PermissionMode != "bypass_permissions" {
		t.Fatalf("run.list = %+v, want bypass_permissions with AutoAccept", summaries)
	}

	sendRequest(t, ws, "stop", "run.stop", map[string]any{"runId": started.RunID})
	readEnvelopeFor(t, ws, "stop", 5*time.Second)
}

// TestRunSetPermissionMode_OrchestratorFailsClosedWithoutCatalog proves
// the ADR-0019 decision-6 guard fails closed: for a run whose provider
// catalog cannot be resolved (here a rehydrated run with no live runner),
// a non-human caller is rejected with the catalog lookup error -- not
// waved through to the mode switch -- and the stored mode is unchanged.
func TestRunSetPermissionMode_OrchestratorFailsClosedWithoutCatalog(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)

	now := time.Now().UTC()
	runID := "rehydrated-run"
	if _, err := db.CreateRun(store.Run{
		ID: runID, TaskID: task.ID, Provider: "glm", Prompt: "hi",
		Status: "done", StartedAt: now, FinishedAt: &now, StopReason: "end_turn",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("store.CreateRun() error = %v", err)
	}
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "switch", "run.setPermissionMode", map[string]any{
		"runId": runID, "modeId": "accept_edits", "source": "orchestrator",
	})
	resp := readEnvelopeFor(t, ws, "switch", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "no live runner") {
		t.Fatalf("error = %+v, want the catalog lookup error (no live runner)", resp.Error)
	}
	var found bool
	for _, s := range readRunList(t, ws) {
		if s.ID == runID {
			found = true
			if s.PermissionMode != "default" {
				t.Errorf("permission mode = %q, want unchanged default", s.PermissionMode)
			}
		}
	}
	if !found {
		t.Fatalf("run %s not in run.list", runID)
	}
	if row, err := db.GetRun(runID); err != nil || row.PermissionMode != "default" {
		t.Fatalf("stored run = %+v, %v; want permission mode default", row, err)
	}
}
