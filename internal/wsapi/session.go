package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/spacingmind/smind/internal/runs"
)

// runIdentity is what a connection learned about itself through
// session.identify (ADR-0041 S2b): it is an agent run, with that run's task
// and chat taken from the registry (never from params).
//
// This is a drift guard, not a sandbox. Identifying can only REDUCE a
// connection's powers (a fail-closed method allowlist, own-task parent and
// sender defaults), so a human gains nothing by doing it. An agent with a
// shell can still read the daemon token and speak /ws unidentified; the
// real boundary is the provider's permission mode (ADR-0019).
type runIdentity struct {
	runID  string
	taskID int64
	chatID int64
}

// RunScopedMethods is the exact set of wsapi methods an identified (run
// scoped) connection may call: those the `smind mcp serve` tools use, plus
// session.identify. Anything else is refused before its handler runs. A test
// in cmd/smind keeps it in step with the MCP tool sources.
var RunScopedMethods = []string{
	"account.usage",
	"chat.create", "chat.list", "chat.queueCancel", "chat.queueList",
	"profile.get", "profile.list",
	"provider.list",
	"remote.hosts", "remote.read", "remote.select", "remote.write",
	"run.list", "run.listConfigOptions", "run.logs", "run.setConfigOption",
	"run.setPermissionMode", "run.start", "run.stop",
	"session.identify",
	"steward.digest.get", "steward.verdict.submit",
	"task.archive", "task.create", "task.diff", "task.get", "task.list", "task.tree",
	"usage.summary",
	"validation.contract.get", "validation.contract.submit", "validation.get", "validation.list", "validation.request", "validation.verdict.submit",
	"workspace.list",
}

var runScopedSet = func() map[string]bool {
	m := make(map[string]bool, len(RunScopedMethods))
	for _, k := range RunScopedMethods {
		m[k] = true
	}
	return m
}()

func forbidden(msg string) error { return &rpcCodeError{code: codeForbidden, msg: msg} }

// identity returns the connection's run identity, if it has identified.
func (c *conn) identity() (runIdentity, bool) {
	c.identMu.Lock()
	defer c.identMu.Unlock()
	return c.ident, c.runScoped
}

// gate enforces a run-scoped connection's constraints on one request: the
// run must still be running, and the method must be allowlisted. It returns
// nil for an unidentified connection.
func (c *conn) gate(method string) error {
	if err := c.gateRun(method); err != nil {
		return err
	}
	if p := c.hostPrincipal(); p != nil {
		if !slices.Contains(fleetHostAllowed[p.kind], method) {
			return forbidden("not available to hosts")
		}
	}
	return nil
}

// gateRun is the run-scoped half of gate: nil for an unscoped connection.
func (c *conn) gateRun(method string) error {
	id, scoped := c.identity()
	if !scoped {
		return nil
	}
	if c.runs != nil {
		st, err := c.runs.State(id.runID)
		if err != nil || st.Status != runs.StatusRunning {
			return forbidden("your run has ended")
		}
	}
	if !runScopedSet[method] {
		return forbidden("not available to agents")
	}
	return nil
}

// handleSessionIdentify binds the (local, token) connection to a running run.
func handleSessionIdentify(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if !rc.local {
			return nil, forbidden("session.identify is not available over the relay")
		}
		var p struct {
			RunID string `json:"runId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.RunID == "" {
			return nil, &rpcCodeError{code: codeInvalid, msg: "session.identify: runId is required"}
		}
		st, err := reg.State(p.RunID)
		if errors.Is(err, runs.ErrNotFound) {
			return nil, fmt.Errorf("session.identify: unknown run")
		}
		if err != nil {
			return nil, fmt.Errorf("session.identify: %w", err)
		}
		if st.Status != runs.StatusRunning {
			return nil, fmt.Errorf("session.identify: run is not running")
		}
		c := rc.conn
		c.identMu.Lock()
		defer c.identMu.Unlock()
		if c.runScoped && c.ident.runID != p.RunID {
			return nil, fmt.Errorf("session.identify: connection is already identified as another run")
		}
		c.runScoped = true
		c.ident = runIdentity{runID: p.RunID, taskID: st.TaskID, chatID: st.ChatID}
		return map[string]any{"runId": p.RunID, "taskId": st.TaskID, "chatId": st.ChatID}, nil
	}
}
