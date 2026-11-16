package runs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browsercard"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// gateAsync runs BrowserGate on its own goroutine and returns its result channel
// plus the id of the card it raised (once pending).
func gateAsync(t *testing.T, reg *Registry, r *run, call BrowserCall) (<-chan error, string) {
	t.Helper()
	out := make(chan error, 1)
	go func() { out <- reg.BrowserGate(context.Background(), call) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for id := range r.pendingPermissions {
			r.mu.Unlock()
			return out, id
		}
		r.mu.Unlock()
		select {
		case err := <-out: // it did not raise a card at all
			out <- err
			return out, ""
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("no card was raised")
	return nil, ""
}

// answer picks an option of a pending card like a person would.
func answer(t *testing.T, reg *Registry, runID, reqID, optionID string) {
	t.Helper()
	if err := reg.RespondPermission(runID, reqID, optionID); err != nil {
		t.Fatalf("answer %s: %v", optionID, err)
	}
}

func wantErr(t *testing.T, ch <-chan error, substr string) {
	t.Helper()
	select {
	case err := <-ch:
		if err == nil || !errors.Is(err, ErrBrowserRefused) || !strings.Contains(err.Error(), substr) {
			t.Fatalf("got %v, want a refusal containing %q", err, substr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no result, want a refusal containing %q", substr)
	}
}

func wantNil(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("got %v, want the call admitted", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call was not admitted")
	}
}

// noCard asserts the call was decided with no card (immediately, nothing pending).
func noCard(t *testing.T, reg *Registry, r *run, call BrowserCall) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- reg.BrowserGate(context.Background(), call) }()
	select {
	case err := <-done:
		if pendingCount(r) != 0 {
			t.Fatalf("a card was raised for %s", call.Tool)
		}
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("%s raised a card (the call is held)", call.Tool)
		return nil
	}
}

func gateRun(t *testing.T, policy string) (*Registry, *run, string) {
	t.Helper()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, policy, true)
	return reg, r, runID
}

var allReadTools = []string{"browser_tabs", "browser_snapshot", "browser_screenshot", "browser_console", "browser_wait"}

func TestGate_ReadGrantCoversFiveReadToolsForThatRunOnly(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	answer(t, reg, runID, id, "allow_run")
	wantNil(t, ch)
	for _, tool := range allReadTools {
		if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: tool}); err != nil {
			t.Fatalf("%s after the read grant: %v", tool, err)
		}
		if !reg.BrowserGranted(runID, tool) {
			t.Fatalf("%s is covered by the read grant", tool)
		}
	}
	if reg.BrowserGranted("another-run", "browser_tabs") {
		t.Fatal("a grant belongs to one run")
	}
	// The read grant does not cover opening a page.
	ch2, id2 := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://localhost:3000/"})
	if id2 == "" {
		t.Fatal("browser_open must still ask")
	}
	answer(t, reg, runID, id2, "deny")
	wantErr(t, ch2, "the person said no")
}

func TestGate_AllowOnceAdmitsOneCall(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_snapshot"})
	answer(t, reg, runID, id, "allow_once")
	wantNil(t, ch)
	if reg.BrowserGranted(runID, "browser_snapshot") {
		t.Fatal("allow once grants nothing")
	}
	ch2, id2 := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_snapshot"})
	if id2 == "" {
		t.Fatal("the next read asks again")
	}
	answer(t, reg, runID, id2, "allow_once")
	wantNil(t, ch2)
}

func TestGate_OpenAsksEveryCallUnlessGrantedAndGrantCoversCloseOnly(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	open := BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://localhost:3000/a", HostStatus: "localhost"}
	for i := 0; i < 2; i++ { // asked every call
		ch, id := gateAsync(t, reg, r, open)
		if id == "" {
			t.Fatal("browser_open asks")
		}
		answer(t, reg, runID, id, "allow_once")
		wantNil(t, ch)
	}
	ch, id := gateAsync(t, reg, r, open)
	answer(t, reg, runID, id, "allow_run")
	wantNil(t, ch)
	if err := noCard(t, reg, r, open); err != nil {
		t.Fatalf("browser_open after its grant: %v", err)
	}
	if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_close"}); err != nil {
		t.Fatalf("the open grant covers browser_close: %v", err)
	}
	// It covers nothing else: a read still asks.
	ch2, id2 := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	if id2 == "" {
		t.Fatal("a read is not covered by the open grant")
	}
	answer(t, reg, runID, id2, "deny")
	wantErr(t, ch2, "the person said no")
}

func TestGate_UnknownToolRaisesNoCard(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	for _, tool := range []string{"browser_evaluate", "browser_click", "", "browser_open2", "smind"} {
		if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: tool}); err != nil {
			t.Fatalf("an unknown tool %q is left to the broker, no card: %v", tool, err)
		}
	}
}

const denyOpenAllowTabsPolicy = `version: 1
rules:
  - id: no-open
    action: deny
    why: no navigation in this project
    match:
      tool: "mcp:smind-browser.browser_open"
  - id: tabs-ok
    action: allow
    why: listing tabs is fine
    match:
      tool: "mcp:smind-browser.browser_tabs"
`

func TestGate_PolicyDenyAllowAskRules(t *testing.T) {
	reg, r, runID := gateRun(t, denyOpenAllowTabsPolicy)
	if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://localhost/"}); err == nil || !strings.Contains(err.Error(), "a policy rule refuses browser_open") {
		t.Fatalf("deny rule: %v", err)
	}
	if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"}); err != nil {
		t.Fatalf("allow rule admits the tool with no card: %v", err)
	}
	// No rule for a snapshot: ask (default), never allow.
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_snapshot"})
	if id == "" {
		t.Fatal("no rule means ask")
	}
	answer(t, reg, runID, id, "deny")
	wantErr(t, ch, "the person said no")
}

func TestGate_AutoAcceptAndTheProviderModeDoNotSatisfyIt(t *testing.T) {
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{AutoAccept: true}, "", true)
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	if id == "" {
		t.Fatal("an auto-accepting run still gets a card: the daemon's gate is independent of the provider's mode")
	}
	answer(t, reg, runID, id, "deny")
	wantErr(t, ch, "the person said no")
}

func TestGate_NobodyAnswersRefusesAfterTheCap(t *testing.T) {
	old := browserGateWait
	browserGateWait = 150 * time.Millisecond
	t.Cleanup(func() { browserGateWait = old })
	reg, r, runID := gateRun(t, "")
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	if id == "" {
		t.Fatal("a card was raised")
	}
	wantErr(t, ch, "no one answered")
	if reg.BrowserGranted(runID, "browser_tabs") {
		t.Fatal("a timeout grants nothing")
	}
	// A timeout is not a denial: it does not feed the limiter.
	for i := 0; i < 4; i++ {
		c, i2 := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
		if i2 == "" {
			t.Fatal("timeouts never block further cards")
		}
		wantErr(t, c, "no one answered")
	}
}

func TestGate_GrantsDieWithTheRunTheSwitchAndTheKillSwitch(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	task, err := reg.st.GetTask(r.taskID)
	if err != nil {
		t.Fatal(err)
	}
	grant := func() {
		ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
		answer(t, reg, runID, id, "allow_run")
		wantNil(t, ch)
		if !reg.BrowserGranted(runID, "browser_tabs") {
			t.Fatal("granted")
		}
	}
	grant()
	reg.RevokeBrowserGrants(task.WorkspaceID + 999) // another workspace: untouched
	if !reg.BrowserGranted(runID, "browser_tabs") {
		t.Fatal("a grant of another workspace's switch must not end this one")
	}
	reg.RevokeBrowserGrants(task.WorkspaceID)
	if reg.BrowserGranted(runID, "browser_tabs") {
		t.Fatal("the workspace switch turning off ends the grant")
	}
	grant()
	reg.RevokeAllBrowserGrants()
	if reg.BrowserGranted(runID, "browser_tabs") {
		t.Fatal("the kill switch ends every grant")
	}
	grant()
	reg.ForgetBrowserRun(runID)
	if reg.BrowserGranted(runID, "browser_tabs") {
		t.Fatal("the run's end drops its grants")
	}
	// A second run of the same task starts with none: grants are per run id and never persisted.
	if reg.BrowserGranted("the-next-run", "browser_tabs") {
		t.Fatal("a new run starts with no grant")
	}
}

func TestGate_ThreeDenialsStopCardsForFiveMinutes(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	for i := 0; i < 3; i++ {
		ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
		if id == "" {
			t.Fatalf("denial %d: a card is raised", i+1)
		}
		answer(t, reg, runID, id, "deny")
		wantErr(t, ch, "the person said no")
	}
	err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	if err == nil || !strings.Contains(err.Error(), "do not ask again for now") {
		t.Fatalf("after three denials the call is refused at once: %v", err)
	}
	// Even a different class is blocked for the run.
	if err := noCard(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://localhost/"}); err == nil {
		t.Fatal("the pause covers every tool of the run")
	}
	// A policy allow rule is not affected by the pause (it never asks).
}

func TestGate_OneCardAtATimePerRun(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	ch1, id1 := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	ch2 := make(chan error, 1)
	go func() {
		ch2 <- reg.BrowserGate(context.Background(), BrowserCall{RunID: runID, Tool: "browser_snapshot"})
	}()
	time.Sleep(150 * time.Millisecond)
	if pendingCount(r) != 1 {
		t.Fatalf("a second concurrent call must wait behind the first card: %d pending", pendingCount(r))
	}
	answer(t, reg, runID, id1, "allow_run") // the read grant also admits the waiting call
	wantNil(t, ch1)
	select {
	case err := <-ch2:
		if err != nil {
			t.Fatalf("the waiting call is admitted by the grant: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting call never finished")
	}
}

func TestGate_CardsCarryNoQueryOrUserinfoAndTheTimelineNoPageContent(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://user:PW@localhost:3000/app?token=SECRET#frag", HostStatus: "localhost"})
	answer(t, reg, runID, id, "allow_run")
	wantNil(t, ch)
	reqs, resolved := permEvents(r)
	if len(reqs) != 1 || len(resolved) != 1 {
		t.Fatalf("one card, one answer: %d %d", len(reqs), len(resolved))
	}
	b, _ := json.Marshal(reqs[0])
	for _, bad := range []string{"SECRET", "PW", "user:", "frag"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("the card carries %q: %s", bad, b)
		}
	}
	if reqs[0].PermissionDetail == nil || reqs[0].PermissionDetail.Kind != "browser" || reqs[0].PermissionDetail.Browser == nil ||
		reqs[0].PermissionDetail.Browser.URL != "http://localhost:3000/app" || reqs[0].PermissionDetail.Grant != nil {
		t.Fatalf("detail: %+v", reqs[0].PermissionDetail)
	}
	if !strings.Contains(reqs[0].PermissionSummary, "(daemon check)") {
		t.Fatalf("the card says it is the daemon's own check: %q", reqs[0].PermissionSummary)
	}
	// The grant is on the timeline, with no URL query and no page content.
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, e := range r.history {
		if e.Type == taskrunner.EventTypeSessionNote && strings.Contains(e.Text, "granted for this run") {
			found = true
			if strings.Contains(e.Text, "SECRET") || strings.Contains(e.Text, "http") {
				t.Fatalf("the note: %q", e.Text)
			}
		}
	}
	_ = found // NoteRunEvent needs a running run; the fixture's run is finished, so only the absence of leaks is asserted here
}

func TestGate_NoSavedAlwaysGrantIsOfferedOnABrowserCard(t *testing.T) {
	reg, r, runID := gateRun(t, "")
	_, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_tabs"})
	reqs, _ := permEvents(r)
	for _, o := range reqs[0].PermissionOptions {
		if o.Kind == "allow_always" || o.Kind == "reject_always" {
			t.Fatalf("no 'always' scope exists for the browser: %+v", o)
		}
	}
	if reqs[0].PermissionDetail.Grant != nil {
		t.Fatal("no saved-grant offer on a browser card")
	}
	answer(t, reg, runID, id, "deny")
}

// On a RUNNING run the grant is a line of its timeline, with no URL and no query.
func TestGate_TheGrantIsATimelineLine(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusRunning, 5*time.Second)
	t.Cleanup(func() { _ = reg.Stop(runID) })
	r, _ := reg.get(runID)
	ch, id := gateAsync(t, reg, r, BrowserCall{RunID: runID, Tool: "browser_open", URL: "http://localhost:3000/x?token=SECRET", HostStatus: "localhost"})
	answer(t, reg, runID, id, "allow_run")
	wantNil(t, ch)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.history {
		if e.Type == taskrunner.EventTypeSessionNote && strings.Contains(e.Text, "browser: opening pages granted for this run") {
			if strings.Contains(e.Text, "SECRET") || strings.Contains(e.Text, "http") {
				t.Fatalf("the note leaks: %q", e.Text)
			}
			return
		}
	}
	t.Fatal("the grant must be a line of the run's timeline")
}

// Grants never cover the act class (any future acting tool asks every call):
// even a run holding both grants has none for it.
func TestGate_NoGrantEverCoversTheActClass(t *testing.T) {
	st := &browserRunState{read: true, open: true}
	if st.grants(browsercard.ClassAct) {
		t.Fatal("an acting tool is never covered by a grant")
	}
	if st.grants("unknown-class") {
		t.Fatal("an unknown class is never covered")
	}
	if !st.grants(browsercard.ClassRead) || !st.grants(browsercard.ClassOpen) {
		t.Fatal("test setup: read and open are covered")
	}
}
