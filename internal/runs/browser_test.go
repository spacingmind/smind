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
	if !c.Mcp.BrowserOnly {
		t.Fatal("a policy created for the browser alone says so")
	}
	if _, ok := env[BrowserTokenEnv]; ok || reg.CheckBrowserToken("run-9", "x") {
		t.Fatal("no capability exists until the runner issues it at a delivery point")
	}
	tok := c.Mcp.Browser.Issue()
	if c.Mcp.Orchestration {
		t.Fatal("the browser grant must not switch orchestration on")
	}
	if len(tok) != 32 {
		t.Fatalf("a 128-bit capability is minted into the row's env: %q", tok)
	}
	if !reg.CheckBrowserToken("run-9", tok) {
		t.Fatal("the minted capability must verify for its run")
	}
	for _, bad := range [][2]string{{"run-9", ""}, {"run-9", tok + "0"}, {"run-9", "x"}, {"another-run", tok}, {"", tok}} {
		if reg.CheckBrowserToken(bad[0], bad[1]) {
			t.Fatalf("%v must not verify", bad)
		}
	}
	// A second run gets its own capability, and a finished run's is gone.
	r2 := &run{id: "run-10", taskID: task.ID, chatID: 3}
	c2 := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r2))
	tok2 := c2.Mcp.Browser.Issue()
	if tok2 == tok || reg.CheckBrowserToken("run-9", tok2) || !reg.CheckBrowserToken("run-10", tok2) {
		t.Fatal("capabilities are per run")
	}
	c2.Mcp.Browser.Revoke() // a provider that skipped the row
	if reg.CheckBrowserToken("run-10", tok2) {
		t.Fatal("a revoked capability must stop verifying")
	}
	reg.ForgetBrowserToken("run-9")
	if reg.CheckBrowserToken("run-9", tok) {
		t.Fatal("a finished run's capability must stop verifying")
	}
	// A system run is never minted one.
	if reg.CheckBrowserToken("run-sys", "anything") {
		t.Fatal("no capability for a run that got no browser")
	}
}

// The REAL store-backed switch (the one the daemon installs), with the package
// gate opened for the test only: the row exists exactly when the workspace flag
// is on and the kill switch is off.
func TestBrowserPolicy_StoreBackedSwitch(t *testing.T) {
	reg, r, task := browserFixture(t)
	withGate(t, true)
	kill := false
	reg.SetBrowserGate(StoreBrowserGate(reg.st, func() bool { return kill }))
	hasRow := func() bool {
		c := taskrunner.RunContractFrom(reg.withBrowserPolicy(context.Background(), r))
		return c.Mcp != nil && c.Mcp.Browser != nil
	}
	save := func(on bool) {
		t.Helper()
		if err := reg.st.SaveBrowserSettings(store.BrowserSettings{WorkspaceID: task.WorkspaceID, Enabled: on, AllowHosts: []string{}, ProfileMode: "ephemeral", UpdatedBy: "local", UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if hasRow() {
		t.Fatal("no row in the store: no browser")
	}
	save(true)
	if !hasRow() {
		t.Fatal("flag on, kill switch off, an ordinary run: the row must be present")
	}
	kill = true
	if hasRow() {
		t.Fatal("the kill switch beats the workspace flag")
	}
	kill = false
	save(false)
	if hasRow() {
		t.Fatal("flag off: no browser")
	}
	save(true)
	if !hasRow() {
		t.Fatal("flag back on")
	}
	// A deleted workspace: the row cascades away and the run gets nothing.
	if err := reg.st.DeleteWorkspace(task.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if hasRow() || reg.BrowserSwitchOn(task.WorkspaceID) {
		t.Fatal("a deleted workspace has no browser")
	}
}

func TestRunIsRunning(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusRunning, 5*time.Second)
	if !reg.RunIsRunning(runID) {
		t.Fatal("a running run is running")
	}
	if reg.RunIsRunning("nope") {
		t.Fatal("an unknown run is not running")
	}
	if err := reg.Stop(runID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusStopped, 5*time.Second)
	if reg.RunIsRunning(runID) {
		t.Fatal("a stopped run is not running")
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
