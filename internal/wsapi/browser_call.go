package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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

// brokerAPI is the part of the broker the host drives (an interface so a test
// can see what the host asks of it).
type brokerAPI interface {
	Call(ctx context.Context, run browser.Run, tool string, args json.RawMessage) browser.Result
	EndRun(runID string)
}

type browserHost struct {
	db     *store.Store
	reg    *runs.Registry
	broker brokerAPI
	mgr    *browser.Manager

	// Hooks, defaulted by newBrowserHost and replaced by tests.
	settings     browserSettingsStore
	setMu        sync.Mutex // serialises browser.settings.set
	killSwitch   func() bool
	closeSession func(workspaceID int64)
	gate         func(workspaceID int64) bool
	tokenOK      func(runID, token string) bool
	live         func(runID string) bool
	forgetToken  func(runID string)
	imagesRead   func(runID string) bool
	allow        func(workspaceID int64) []string
	deny         func() ([]browser.Endpoint, error)
	launcher     func() (browser.Launcher, error)
	profileRoot  string
}

func newBrowserHost(db *store.Store, reg *runs.Registry) *browserHost {
	h := &browserHost{db: db, reg: reg, mgr: &browser.Manager{}}
	h.settings = db
	h.closeSession = func(ws int64) { h.mgr.Close(sessionKey(ws)) }
	h.killSwitch = config.BrowserDisabled
	h.gate = reg.BrowserAllowed
	h.tokenOK = reg.CheckBrowserToken
	h.live = reg.RunIsRunning
	h.forgetToken = reg.ForgetBrowserToken
	h.imagesRead = func(runID string) bool {
		info, err := reg.ProviderCatalogForRun(runID)
		return err == nil && imagesReadFrom(info.Images)
	}
	h.allow = func(ws int64) []string {
		s, err := h.settings.GetBrowserSettings(ws)
		if err != nil {
			return nil // an unreadable setting allows nothing beyond loopback
		}
		return s.AllowHosts
	}
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
		CloseSession: func(ws int64) { h.mgr.Close(sessionKey(ws)) },
		Record:       h.record,
	})
	return h
}

// imagesReadFrom says whether a provider catalogue entry reads images: only an
// explicit "yes". "no" and "unknown" (and anything else) get the text note.
func imagesReadFrom(images string) bool { return images == "yes" }

// sessionKey names a workspace's browser session in the manager; every place
// that starts or closes one uses it.
func sessionKey(ws int64) string { return strconv.FormatInt(ws, 10) }

// daemonDeny reserves the daemon's own listen address: a page must never reach
// its unauthenticated loopback endpoints. (The rest of the deny list, the
// connection list, arrives with the settings PR.)
func daemonDeny() ([]browser.Endpoint, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("browser: the daemon's configuration cannot be read, so its own address cannot be reserved: %w", err)
	}
	host, port, err := cfg.Server.ListenHostPort()
	if err != nil {
		return nil, fmt.Errorf("browser: the daemon's listen address cannot be read: %w", err)
	}
	if host == "" {
		host = "0.0.0.0" // ":4648" binds every address: the policy reserves the port everywhere
	}
	// The daemon also answers on loopback when it listens on a LAN address or a
	// name (the loopback companion listener), and loopback is always allowed to
	// a page: so the port is reserved on every loopback spelling, whatever the
	// configured host.
	out := []browser.Endpoint{
		{Host: host, Port: port},
		{Host: "127.0.0.1", Port: port}, {Host: "::1", Port: port}, {Host: "localhost", Port: port},
	}
	// The names the daemon is reached by (server.hostnames) are the same port.
	for _, name := range cfg.Server.Hostnames {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, browser.Endpoint{Host: name, Port: port})
		}
	}
	return out, nil
}

// workspaceSwitchOn is the registry's browser gate: the workspace's own switch
// is on and the daemon-wide kill switch is off. Both are read on every decision.
func (h *browserHost) workspaceSwitchOn(ws int64) bool {
	if h.killSwitch() {
		return false
	}
	s, err := h.settings.GetBrowserSettings(ws)
	return err == nil && s.Enabled
}

func (h *browserHost) session(ctx context.Context, run browser.Run) (*browser.Session, error) {
	l, err := h.launcher()
	if err != nil {
		return nil, err // an *UnavailableError: the broker turns it into the plain install text
	}
	allow := h.allow(run.WorkspaceID)
	deny, err := h.deny()
	if err != nil {
		return nil, err // fail closed: no session without the daemon's own address reserved
	}
	pol, err := browser.NewPolicy(allow, deny)
	if err != nil {
		return nil, err
	}
	return h.mgr.Get(ctx, browser.Config{
		Workspace: sessionKey(run.WorkspaceID),
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
func (h *browserHost) OnRunFinished(f runs.RunFinished) {
	h.forgetToken(f.RunID)
	h.broker.EndRun(f.RunID)
}

// close ends every browser session (daemon shutdown).
func (h *browserHost) close() { h.mgr.CloseAll() }

func (h *browserHost) call(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "browser.call"
	if rc == nil || !rc.runScoped {
		return nil, forbidden(m + " is only available to an agent run")
	}
	var p struct {
		Tool  string          `json:"tool"`
		Args  json.RawMessage `json:"args"`
		Token string          `json:"token"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // no runId or workspace beside tool and args
	if err := dec.Decode(&p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: m + ": invalid params"}
	}
	if rc.ident.runID == "" {
		return nil, forbidden(m + " is only available to an identified run")
	}
	// A run id alone proves nothing (session.identify takes any running id and
	// run.list shows them): the caller must hold the capability minted for that
	// run when its browser row was built, which only its own MCP child has.
	// A run that got no browser row has none, so system runs are refused here too.
	if !h.tokenOK(rc.ident.runID, p.Token) {
		return nil, forbidden(m + ": this run has no browser")
	}
	if !h.live(rc.ident.runID) {
		return nil, forbidden(m + ": the run has ended")
	}
	task, err := h.db.GetTask(rc.ident.taskID)
	if err != nil {
		return nil, forbidden(m + " is not available")
	}
	ws := task.WorkspaceID
	if h.killSwitch() {
		// The daemon-wide switch beats every workspace: end any browser now.
		h.mgr.CloseAll()
		return nil, forbidden(m + ": the browser is disabled by the daemon's configuration")
	}
	if !h.gate(ws) {
		return nil, forbidden(m + ": the browser is not enabled for this workspace")
	}
	run := browser.Run{ID: rc.ident.runID, WorkspaceID: ws, ImagesRead: h.imagesRead(rc.ident.runID)}
	return h.broker.Call(ctx, run, p.Tool, p.Args), nil
}
