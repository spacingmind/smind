package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// These tests flip the package gate variable, so none of them is parallel.

func browserFixture(t *testing.T) (*Registry, *run, store.Task) {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	reg.SetSmindHome("/daemon/home")
	return reg, &run{id: "run-9", taskID: task.ID, chatID: 3}, task
}

func withGate(t *testing.T, open bool) {
	t.Helper()
	old := browserGateReady
	browserGateReady = open
	t.Cleanup(func() { browserGateReady = old })
}

func TestBrowserPolicy_DarkWhileTheGateIsClosed(t *testing.T) {
	reg, r, task := browserFixture(t)
	reg.SetBrowserGate(func(int64) bool { return true }) // everything else says yes
	withGate(t, false)
	if reg.BrowserAllowed(task.WorkspaceID) {
		t.Fatal("the browser must be dark while the package gate is closed")
	}
	if c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r)); c.Mcp != nil {
		t.Fatalf("no run may get the browser row while the gate is dark: %+v", c.Mcp)
	}
}

func TestBrowserPolicy_RowOnlyWhenAllGatesHold(t *testing.T) {
	reg, r, task := browserFixture(t)
	withGate(t, true)

	// No hook: no.
	if c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r)); c.Mcp != nil {
		t.Fatal("no workspace switch hook means no browser")
	}
	// The hook says no for this workspace.
	reg.SetBrowserGate(func(ws int64) bool { return ws != task.WorkspaceID })
	if c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r)); c.Mcp != nil {
		t.Fatal("a workspace without the switch gets no browser")
	}
	// A system run never gets it.
	reg.SetBrowserGate(func(int64) bool { return true })
	sys := &run{id: "run-sys", taskID: task.ID, purpose: "reflect"}
	if c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), sys)); c.Mcp != nil {
		t.Fatal("a system run never gets the browser")
	}
	// All hold: the row, carrying the run identity.
	c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r))
	if c.Mcp == nil || c.Mcp.Browser == nil {
		t.Fatalf("all gates hold, the run must get the browser: %+v", c.Mcp)
	}
	env := c.Mcp.Browser.Env
	if env["SMIND_RUN_ID"] != "run-9" || env["SMIND_HOME"] != "/daemon/home" {
		t.Fatalf("env: %v", env)
	}
	if c.Mcp.Orchestration {
		t.Fatal("the browser grant must not switch orchestration on")
	}
}

func TestBrowserPolicy_KeepsAndDoesNotMutateAnExistingContract(t *testing.T) {
	reg, r, task := browserFixture(t)
	withGate(t, true)
	reg.SetBrowserGate(func(int64) bool { return true })
	orig := &taskrunner.McpPolicy{Orchestration: true, Env: map[string]string{"SMIND_RUN_ID": "run-9"}}
	ctx := taskrunner.WithRunContract(context.Background(), taskrunner.RunContract{Instructions: "ROLE", Mcp: orig})
	c := taskrunner.RunContractFrom(reg.withBrowserPolicy(ctx, r))
	if c.Instructions != "ROLE" || c.Mcp == nil || !c.Mcp.Orchestration || c.Mcp.Browser == nil {
		t.Fatalf("the existing contract must be kept and extended: %+v", c)
	}
	if orig.Browser != nil {
		t.Fatal("the original policy must not be mutated")
	}
	_ = task
}

func TestNoteRunEvent_AppendsToALiveRunOnly(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusRunning, 5*time.Second)
	if err := reg.NoteRunEvent(runID, "browser browser_open http://localhost:3000/ -> ok (12 ms)"); err != nil {
		t.Fatal(err)
	}
	h, err := reg.get(runID)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	found := false
	for _, e := range h.history {
		if e.Type == taskrunner.EventTypeSessionNote && strings.Contains(e.Text, "browser_open") {
			found = true
		}
	}
	h.mu.Unlock()
	if !found {
		t.Fatal("the note must be in the run's history")
	}
	if err := reg.Stop(runID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if err := reg.NoteRunEvent(runID, "late"); err == nil {
		t.Fatal("a finished run's history is closed")
	}
	if err := reg.NoteRunEvent("no-such-run", "x"); err == nil {
		t.Fatal("an unknown run is an error")
	}
}
