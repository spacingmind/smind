package runs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/browsercard"
	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// The daemon-side permission gate of the agent browser (ADR-0061 section 10,
// decisions D-B and D-D). It answers, for one browser tool call of a run:
// admitted now, refused, or held for a person. The provider's permission mode
// never answers it (a provider in an auto-accept mode never asks): only a
// human policy rule, a per-run grant the person gave on a card, or the person
// answering the card does.

// browserGateWait caps how long a held call waits for a person (then it is
// refused with "no one answered"); the child's per-call deadline is longer. A
// variable so a test can shorten it.
var browserGateWait = 120 * time.Second

// BrowserGateWait is the cap, for the child's deadline.
func BrowserGateWait() time.Duration { return browserGateWait }

const (
	browserDenyWindow = time.Minute
	browserDenyLimit  = 3
	browserDenyPause  = 5 * time.Minute
	// browserTimeoutPause: after a card nobody answered, no new card for this long.
	browserTimeoutPause = 30 * time.Second
	// browserMaxWaiters bounds the calls of one run waiting on its card (the running
	// one included).
	browserMaxWaiters = 4
)

// readTools are admitted together by one per-run read grant.
var readTools = map[string]bool{
	"browser_tabs": true, "browser_snapshot": true, "browser_screenshot": true,
	"browser_console": true, "browser_wait": true,
}

// browserClass classifies a tool: read, open (browser_open and browser_close,
// covered by one per-run open grant) or act (anything else the catalogue may one
// day hold: asked every call, never grantable). ok is false for a tool name the
// gate does not know: it raises no card (the broker refuses it).
func browserClass(tool string) (c browsercard.Class, ok bool) {
	switch {
	case readTools[tool]:
		return browsercard.ClassRead, true
	case tool == "browser_open" || tool == "browser_close":
		return browsercard.ClassOpen, true
	}
	return "", false
}

// BrowserCall describes one call the gate judges.
type BrowserCall struct {
	RunID      string
	Tool       string
	URL        string   // the target of a browser_open (raw), "" otherwise
	HostStatus string   // the workspace policy's verdict on the URL's host: "localhost" | "added"
	Allowlist  []string // the workspace's added hosts, for the card text
}

// browserRunState is the per-run gate state: the grants the person gave on cards
// and the denial limiter. In memory only: it dies with the run, the workspace
// switch and the kill switch, and is never persisted.
type browserRunState struct {
	workspaceID int64
	// ask is a one-slot semaphore: one card at a time per run, and a queued call
	// leaves when its context ends (a mutex would hold it for a whole card). waiters
	// bounds how many calls may queue behind it.
	ask     chan struct{}
	waiters atomic.Int32

	mu           sync.Mutex
	epoch        uint64 // bumped by every revoke: an answer taken before one is void
	read, open   bool
	denials      []time.Time
	blockedUntil time.Time
	blockedWhy   string // what the agent is told while blockedUntil is in the future
	uses         int
}

func (reg *Registry) browserRun(runID string, workspaceID int64) *browserRunState {
	reg.bgMu.Lock()
	defer reg.bgMu.Unlock()
	if reg.bgRuns == nil {
		reg.bgRuns = map[string]*browserRunState{}
	}
	s := reg.bgRuns[runID]
	if s == nil {
		s = &browserRunState{workspaceID: workspaceID, ask: make(chan struct{}, 1)}
		reg.bgRuns[runID] = s
	}
	return s
}

// ForgetBrowserRun drops a finished run's grants and limiter.
func (reg *Registry) ForgetBrowserRun(runID string) {
	reg.bgMu.Lock()
	delete(reg.bgRuns, runID)
	reg.bgMu.Unlock()
}

// RevokeBrowserGrants drops the grants of every run of one workspace (the
// workspace switch was turned off).
func (reg *Registry) RevokeBrowserGrants(workspaceID int64) {
	reg.bgMu.Lock()
	defer reg.bgMu.Unlock()
	for _, s := range reg.bgRuns {
		if s.workspaceID == workspaceID {
			s.mu.Lock()
			s.read, s.open = false, false
			s.epoch++
			s.mu.Unlock()
		}
	}
}

// RevokeAllBrowserGrants drops every run's grants (the kill switch is on).
func (reg *Registry) RevokeAllBrowserGrants() {
	reg.bgMu.Lock()
	defer reg.bgMu.Unlock()
	for _, s := range reg.bgRuns {
		s.mu.Lock()
		s.read, s.open = false, false
		s.epoch++
		s.mu.Unlock()
	}
}

// BrowserGranted reports whether the run holds the grant that admits tool (for
// tests and the settings view).
func (reg *Registry) BrowserGranted(runID, tool string) bool {
	class, ok := browserClass(tool)
	if !ok {
		return false
	}
	reg.bgMu.Lock()
	s := reg.bgRuns[runID]
	reg.bgMu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grants(class)
}

func (s *browserRunState) grants(class browsercard.Class) bool {
	switch class {
	case browsercard.ClassRead:
		return s.read
	case browsercard.ClassOpen:
		return s.open
	}
	return false
}

// ErrBrowserRefused marks a refusal the gate itself made (the text is safe to
// show the agent: it names no secret and no page content).
var ErrBrowserRefused = errors.New("browser permission refused")

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBrowserRefused, fmt.Sprintf(format, a...))
}

// BrowserGate judges one call. A nil error admits it. Decision order: the
// tool class (an unknown tool raises no card), the human's policy rules
// (deny refuses, allow admits), the denial limiter, the run's grants, and
// otherwise a held call and a card.
func (reg *Registry) BrowserGate(ctx context.Context, call BrowserCall) error {
	class, known := browserClass(call.Tool)
	if !known {
		return nil // the broker refuses an unknown tool; no card is raised for it
	}
	r, err := reg.get(call.RunID)
	if err != nil || r.requireRunning("browser") != nil {
		return refused("the run is not known or has ended")
	}
	task, err := reg.st.GetTask(r.taskID)
	if err != nil {
		return refused("the run's task is not known")
	}
	d := runPermissionDecider{reg: reg, r: r, browser: true}

	// 1. The human's own policy rules (default: ask, never allow).
	if engine := reg.getPolicyEngine(); engine != nil {
		probe := &taskrunner.PermissionDetail{Kind: "browser", Browser: &taskrunner.BrowserCardInfo{Tool: call.Tool, Class: string(class)}}
		if dec, on := engine.Evaluate(d.policyRequest("", "", probe)); on {
			switch dec.Action {
			case permpolicy.ActionDeny:
				return refused("a policy rule refuses %s", call.Tool)
			case permpolicy.ActionAllow:
				_ = reg.NoteRunEvent(call.RunID, fmt.Sprintf("browser %s admitted by a policy rule", call.Tool))
				return nil
			}
		}
	}

	st := reg.browserRun(call.RunID, task.WorkspaceID)

	// 2. The denial limiter and the run's grants.
	st.mu.Lock()
	if time.Now().Before(st.blockedUntil) {
		st.mu.Unlock()
		return refused("%s", st.blockedWhy)
	}
	if st.grants(class) {
		st.uses++
		st.mu.Unlock()
		return nil
	}
	st.mu.Unlock()

	// 3. Hold the call and raise a card; one card at a time per run, a bounded
	// queue behind it, and a call whose context ends leaves the queue.
	if st.waiters.Add(1) > browserMaxWaiters {
		st.waiters.Add(-1)
		return refused("too many browser calls are waiting for an answer")
	}
	defer st.waiters.Add(-1)
	select {
	case st.ask <- struct{}{}:
	case <-ctx.Done():
		return refused("the call ended while it waited for an earlier question")
	}
	defer func() { <-st.ask }()
	st.mu.Lock()
	if time.Now().Before(st.blockedUntil) {
		st.mu.Unlock()
		return refused("%s", st.blockedWhy)
	}
	if st.grants(class) { // another call's card granted it while this one waited
		st.uses++
		st.mu.Unlock()
		return nil
	}
	uses := st.uses
	epoch := st.epoch
	st.mu.Unlock()

	ws, _ := reg.st.GetWorkspace(task.WorkspaceID)
	card := browsercard.Build(browsercard.Input{
		Class: class, Tool: call.Tool, TaskTitle: task.Title, Workspace: ws.Title,
		Allowlist: call.Allowlist, URL: call.URL, HostStatus: call.HostStatus, Uses: uses,
	})
	options := make([]taskrunner.PermissionOption, 0, len(card.Options))
	for _, o := range card.Options {
		options = append(options, taskrunner.PermissionOption{ID: o.ID, Label: o.Name, Kind: o.Kind})
	}
	detail := &taskrunner.PermissionDetail{Kind: "browser", Browser: &taskrunner.BrowserCardInfo{
		Tool: card.Info.Tool, Class: card.Info.Class, URL: card.Info.URL, Host: card.Info.Host,
		HostStatus: card.Info.HostStatus, TaskTitle: card.Info.TaskTitle, Workspace: card.Info.Workspace,
		Allowlist: card.Info.Allowlist, Uses: card.Info.Uses,
	}}

	var timedOut bool
	d.timeout, d.timedOut = browserGateWait, &timedOut
	optionID, _, derr := d.decide(ctx, card.Summary, detail, options, false)
	if derr != nil {
		return refused("the permission request ended")
	}
	if timedOut {
		// Nobody is there: do not raise another card at once (an agent could
		// otherwise spam an empty room); the next call after the pause asks again.
		st.mu.Lock()
		st.blockedUntil = time.Now().Add(browserTimeoutPause)
		st.blockedWhy = "no one answered a moment ago; try again later"
		st.mu.Unlock()
		return refused("no one answered the permission request; ask again later if it is still needed")
	}

	st.mu.Lock()
	if st.epoch != epoch {
		// A switch (the workspace's or the kill switch) was turned off while the
		// card was open: whatever was answered is void.
		st.mu.Unlock()
		return refused("the browser was switched off while the question was open")
	}
	st.mu.Unlock()

	switch optionID {
	case "allow_once":
		st.mu.Lock()
		st.uses++
		st.mu.Unlock()
		_ = reg.NoteRunEvent(call.RunID, fmt.Sprintf("browser: %s allowed once", call.Tool))
		return nil
	case "allow_run":
		st.mu.Lock()
		if class == browsercard.ClassRead {
			st.read = true
		} else if class == browsercard.ClassOpen {
			st.open = true
		}
		st.uses++
		st.mu.Unlock()
		what := "read access"
		if class == browsercard.ClassOpen {
			what = "opening pages"
		}
		_ = reg.NoteRunEvent(call.RunID, fmt.Sprintf("browser: %s granted for this run", what))
		return nil
	}

	// Anything else is a refusal by the person.
	now := time.Now()
	st.mu.Lock()
	keep := st.denials[:0]
	for _, t := range st.denials {
		if now.Sub(t) < browserDenyWindow {
			keep = append(keep, t)
		}
	}
	st.denials = append(keep, now)
	if len(st.denials) >= browserDenyLimit {
		st.blockedUntil = now.Add(browserDenyPause)
		st.blockedWhy = "the person declined; do not ask again for now"
		st.denials = nil
	}
	st.mu.Unlock()
	_ = reg.NoteRunEvent(call.RunID, fmt.Sprintf("browser: %s refused by the person", call.Tool))
	return refused("the person said no")
}
