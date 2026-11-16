package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// The browser tools (ADR-0061) reach a run only through the reserved
// `smind-browser` MCP row. Whether a run gets it is decided here, once, when
// the run starts, and the daemon re-checks the same gate on every browser call.

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
	// The socket is NOT opened here: the runner opens it only when the row is
	// really handed to the agent, with the agent's process id (not for codex-native,
	// a plugin provider, a provider with no known process, or a row the agent
	// skipped), so no socket exists for a row nobody got.
	runID, taskID := r.id, r.taskID
	c.Mcp.Browser = &taskrunner.BrowserPolicy{
		Env: reg.mcpBaseEnv(r),
		Socket: func(rootPID int) (string, error) {
			reg.mu.Lock()
			bs := reg.browserSockets
			reg.mu.Unlock()
			if bs == nil {
				return "", errors.New("runs: no browser socket host is installed")
			}
			return bs.OpenBrowserSocket(runID, taskID, rootPID)
		},
		OpenPending: func() (string, func(int) error, error) {
			reg.mu.Lock()
			bs := reg.browserSockets
			reg.mu.Unlock()
			if bs == nil {
				return "", nil, errors.New("runs: no browser socket host is installed")
			}
			return bs.OpenBrowserSocketPending(runID, taskID)
		},
		Revoke: func() { reg.closeBrowserSocket(runID) },
	}
	return taskrunner.WithRunContract(ctx, c)
}

// BrowserSockets opens and closes the per-run browser sockets (the daemon's
// socket host implements it).
type BrowserSockets interface {
	// OpenBrowserSocket creates the run's socket, bound to the agent process
	// rooted at rootPID, and returns its path.
	OpenBrowserSocket(runID string, taskID int64, rootPID int) (string, error)
	// OpenBrowserSocketPending creates the socket before the agent process exists
	// (the Claude CLI path); bind attaches it to the process once it runs. A
	// connection before bind is refused.
	OpenBrowserSocketPending(runID string, taskID int64) (path string, bind func(rootPID int) error, err error)
	CloseBrowserSocket(runID string)
}

// SetBrowserSockets installs the socket host. Without it no run gets the row.
func (reg *Registry) SetBrowserSockets(bs BrowserSockets) {
	reg.mu.Lock()
	reg.browserSockets = bs
	reg.mu.Unlock()
}

// BrowserSocketHost returns the installed socket host (nil if none): the
// daemon wiring test checks it is the browser host.
func (reg *Registry) BrowserSocketHost() BrowserSockets {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.browserSockets
}

func (reg *Registry) closeBrowserSocket(runID string) {
	reg.mu.Lock()
	bs := reg.browserSockets
	reg.mu.Unlock()
	if bs != nil {
		bs.CloseBrowserSocket(runID)
	}
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
