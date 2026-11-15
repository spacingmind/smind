package runs

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// The browser tools (ADR-0061) reach a run only through the reserved
// `smind-browser` MCP row. Whether a run gets it is decided here, once, when
// the run starts, and the daemon re-checks the same gate on every browser.call.

// browserGateReady mirrors browser.GateReady (false while the browser is dark);
// a variable so a test can open it.
var browserGateReady = browser.GateReady

// SetBrowserGate installs the hook that says whether a workspace has the
// browser switched on (human-only, a later PR). Without it no run gets the row.
func (reg *Registry) SetBrowserGate(fn func(workspaceID int64) bool) {
	reg.mu.Lock()
	reg.browserGate = fn
	reg.mu.Unlock()
}

// BrowserSettingsReader is the part of the store the workspace switch reads.
type BrowserSettingsReader interface {
	GetBrowserSettings(workspaceID int64) (store.BrowserSettings, error)
}

// StoreBrowserGate is the real workspace switch: the workspace's stored flag is
// on and the daemon-wide kill switch is off, both read on every decision, an
// unreadable setting meaning off.
func StoreBrowserGate(settings BrowserSettingsReader, killSwitch func() bool) func(int64) bool {
	return func(ws int64) bool {
		if killSwitch() {
			return false
		}
		s, err := settings.GetBrowserSettings(ws)
		return err == nil && s.Enabled
	}
}

// BrowserSwitchOn runs the installed workspace switch hook alone (without the
// package dark gate): what the wiring decides for a workspace.
func (reg *Registry) BrowserSwitchOn(workspaceID int64) bool {
	reg.mu.Lock()
	fn := reg.browserGate
	reg.mu.Unlock()
	return fn != nil && fn(workspaceID)
}

// BrowserAllowed reports whether runs of the workspace may use the browser:
// the package gate is open AND the workspace switch hook says yes.
func (reg *Registry) BrowserAllowed(workspaceID int64) bool {
	if !browserGateReady {
		return false
	}
	reg.mu.Lock()
	fn := reg.browserGate
	reg.mu.Unlock()
	return fn != nil && fn(workspaceID)
}

// withBrowserPolicy adds the browser MCP row to the run contract of an
// ordinary user run whose workspace may use the browser. It adds nothing
// otherwise: not for system runs (validators, seats), not while the gate is
// dark, not for a workspace that did not switch it on.
func (reg *Registry) withBrowserPolicy(ctx context.Context, r *run) context.Context {
	if r.purpose != "" || !browserGateReady {
		return ctx
	}
	task, err := reg.st.GetTask(r.taskID)
	if err != nil || !reg.BrowserAllowed(task.WorkspaceID) {
		return ctx
	}
	c := taskrunner.RunContractFrom(ctx)
	if c.Mcp == nil {
		c.Mcp = &taskrunner.McpPolicy{BrowserOnly: true}
	} else {
		cp := *c.Mcp
		c.Mcp = &cp
	}
	// The capability is NOT minted here: the runner issues it only when the row
	// is really handed to the agent (not for codex-native, a plugin provider, or
	// a row the agent skipped), so a token never exists for a row nobody got.
	runID := r.id
	c.Mcp.Browser = &taskrunner.BrowserPolicy{
		Env:    reg.mcpBaseEnv(r),
		Issue:  func() string { return reg.issueBrowserToken(runID) },
		Revoke: func() { reg.ForgetBrowserToken(runID) },
	}
	return taskrunner.WithRunContract(ctx, c)
}

// issueBrowserToken mints and stores the capability of one run.
func (reg *Registry) issueBrowserToken(runID string) string {
	tok := newBrowserToken()
	reg.mu.Lock()
	if reg.browserTokens == nil {
		reg.browserTokens = map[string]string{}
	}
	reg.browserTokens[runID] = tok
	reg.mu.Unlock()
	return tok
}

// BrowserTokenEnv is the environment variable that carries a run's browser
// capability to its `smind-browser` MCP child (and nowhere else: it is in the
// row's env, not in the agent's own environment).
const BrowserTokenEnv = "SMIND_BROWSER_TOKEN"

func newBrowserToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("runs: no randomness for the browser token: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// CheckBrowserToken reports whether tok is the capability minted for runID
// when its browser row was built. Run ids are guessable (run.list is an agent
// method) and session.identify proves nothing, so browser.call needs this
// proof as well: only the run's own MCP child holds it. Constant time.
func (reg *Registry) CheckBrowserToken(runID, tok string) bool {
	reg.mu.Lock()
	want, ok := reg.browserTokens[runID]
	reg.mu.Unlock()
	return ok && tok != "" && subtle.ConstantTimeCompare([]byte(want), []byte(tok)) == 1
}

// ForgetBrowserToken drops a finished run's capability.
func (reg *Registry) ForgetBrowserToken(runID string) {
	reg.mu.Lock()
	delete(reg.browserTokens, runID)
	reg.mu.Unlock()
}

// RunIsRunning reports whether the run exists and is still running.
func (reg *Registry) RunIsRunning(runID string) bool {
	r, err := reg.get(runID)
	return err == nil && r.requireRunning("browser") == nil
}

// NoteRunEvent appends a one-line note to a live run's timeline (the browser
// broker records each call this way). It is an error for a run that is not
// running: a finished run's history is closed.
func (reg *Registry) NoteRunEvent(runID, text string) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	if err := r.requireRunning("note"); err != nil {
		return fmt.Errorf("run %s: %w", runID, err)
	}
	reg.record(r, Event{Type: taskrunner.EventTypeSessionNote, Text: text})
	return nil
}
