package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// browser.call (ADR-0061). The one RPC behind the `smind-browser` MCP child:
// RUN-SCOPED ONLY (it is on RunScopedMethods and refuses a connection that did
// not identify as a run), bound to the identified run. The workspace is derived
// from the run's task, never read from the params, and the daemon re-checks the
// browser gate on every call (not only when the MCP row was built), so a child
// that somehow runs against a workspace without the browser gets a refusal.
// It is not human-only and not on permpolicy.HumanOnlyRPCs: it grants nothing,
// it only runs the read tools the gate allows.

// browserCallMethods lists the browser RPCs; a test walks it.
var browserCallMethods = []string{"browser.call"}

type browserHost struct {
	db     *store.Store
	reg    *runs.Registry
	broker *browser.Broker
	mgr    *browser.Manager

	// Hooks, defaulted by newBrowserHost and replaced by tests.
	gate        func(workspaceID int64) bool
	imagesRead  func(runID string) bool
	allow       func(workspaceID int64) []string
	deny        func() []browser.Endpoint
	launcher    func() (browser.Launcher, error)
	profileRoot string
}

func newBrowserHost(db *store.Store, reg *runs.Registry) *browserHost {
	h := &browserHost{db: db, reg: reg, mgr: &browser.Manager{}}
	h.gate = reg.BrowserAllowed
	h.imagesRead = func(runID string) bool {
		info, err := reg.ProviderCatalogForRun(runID)
		return err == nil && info.Images == "yes" // "no" and "unknown" cannot read images
	}
	h.allow = func(int64) []string { return nil } // the per-workspace allowlist is a later PR: loopback only
	h.deny = daemonDeny
	h.launcher = func() (browser.Launcher, error) {
		p, err := browser.DetectChrome("")
		if err != nil {
			return nil, err
		}
		return browser.ChromeLauncher{Path: p}, nil
	}
	h.profileRoot = filepath.Join(config.Dir(), "browser")
	h.broker = browser.NewBroker(browser.BrokerConfig{
		Session:      h.session,
		CloseSession: func(ws int64) { h.mgr.Close(strconv.FormatInt(ws, 10)) },
		Record:       h.record,
	})
	return h
}

// daemonDeny reserves the daemon's own listen address: a page must never reach
// its unauthenticated loopback endpoints. (The rest of the deny list, the
// connection list, arrives with the settings PR.)
func daemonDeny() []browser.Endpoint {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	host, port, err := cfg.Server.ListenHostPort()
	if err != nil {
		return nil
	}
	return []browser.Endpoint{{Host: host, Port: port}}
}

func (h *browserHost) session(ctx context.Context, run browser.Run) (*browser.Session, error) {
	l, err := h.launcher()
	if err != nil {
		return nil, err // an *UnavailableError: the broker turns it into the plain install text
	}
	allow := h.allow(run.WorkspaceID)
	pol, err := browser.NewPolicy(allow, h.deny())
	if err != nil {
		return nil, err
	}
	return h.mgr.Get(ctx, browser.Config{
		Workspace: strconv.FormatInt(run.WorkspaceID, 10),
		Launcher:  l,
		Launch:    browser.LaunchOpts{ProfileRoot: h.profileRoot, AllowHosts: allow},
		Policy:    pol,
	})
}

// record puts one line per call in the run's timeline: the tool, the redacted
// URL and the outcome, never page content.
func (h *browserHost) record(run browser.Run, rec browser.Record) {
	line := fmt.Sprintf("browser %s", rec.Tool)
	if rec.URL != "" {
		line += " " + rec.URL
	}
	line += fmt.Sprintf(" -> %s (%d ms)", rec.Outcome, rec.Duration.Milliseconds())
	_ = h.reg.NoteRunEvent(run.ID, line)
}

// OnRunFinished ends the browser session a finished run was using.
func (h *browserHost) OnRunFinished(f runs.RunFinished) { h.broker.EndRun(f.RunID) }

// close ends every browser session (daemon shutdown).
func (h *browserHost) close() { h.mgr.CloseAll() }

func (h *browserHost) call(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "browser.call"
	if rc == nil || !rc.runScoped {
		return nil, forbidden(m + " is only available to an agent run")
	}
	var p struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // no runId or workspace beside tool and args
	if err := dec.Decode(&p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: m + ": invalid params"}
	}
	task, err := h.db.GetTask(rc.ident.taskID)
	if err != nil {
		return nil, forbidden(m + " is not available")
	}
	ws := task.WorkspaceID
	if !h.gate(ws) {
		return nil, forbidden(m + ": the browser is not enabled for this workspace")
	}
	run := browser.Run{ID: rc.ident.runID, WorkspaceID: ws, ImagesRead: h.imagesRead(rc.ident.runID)}
	return h.broker.Call(ctx, run, p.Tool, p.Args), nil
}
