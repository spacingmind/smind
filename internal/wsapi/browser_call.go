package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/browser/runsock"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// The browser call path (ADR-0061). The `smind-browser` MCP child does not use
// the daemon websocket: it dials ITS RUN'S OWN unix socket (internal/browser/
// runsock), whose accept path refuses any peer that is not the daemon's user and
// a process of that run's tree. There is no browser RPC on the websocket at all,
// so no identified connection can reach the browser, and no token exists.
// callRun below is the one place a call from a proven run is authorised: the run
// is still live, the daemon-wide kill switch is off, the workspace gate holds;
// the workspace is derived from the run's task, never from the request.

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
	settings browserSettingsStore
	setMu    sync.Mutex // serialises browser.settings.set
	// gateMu orders session creation against a settings change: a session is
	// created under the read lock after re-checking the gates, and a change
	// closes the workspace's session under the write lock, so a call that
	// passed the gates before the change can never leave a session behind.
	gateMu         sync.RWMutex
	endAllSessions func()
	startSession   func(ctx context.Context, cfg browser.Config) (*browser.Session, error)
	killSwitch     func() bool
	closeSession   func(workspaceID int64)
	gate           func(workspaceID int64) bool
	live           func(runID string) bool
	imagesRead     func(runID string) bool
	allow          func(workspaceID int64) []string
	deny           func() ([]browser.Endpoint, error)
	launcher       func() (browser.Launcher, error)
	profileRoot    string
	// The permission gate and its bookkeeping (hooks so a test can drive them).
	permit       func(ctx context.Context, call runs.BrowserCall) error
	revokeGrants func(workspaceID int64)
	revokeAll    func()
	forgetRun    func(runID string)
	liveDeny     func() []browser.Endpoint
	// socks hosts the per-run sockets; nil when the socket directory could not
	// be prepared (then no run gets a browser row).
	socks browserSockHub
}

// browserSockHub is the part of runsock.Hub the host uses (an interface so a
// test can see what the host asks of it).
type browserSockHub interface {
	Open(runID string, rootPID int, h runsock.Handler) (string, error)
	OpenPending(runID string, h runsock.Handler) (string, func(rootPID int) error, error)
	Close(runID string)
	CloseAll()
}

func newBrowserHost(db *store.Store, reg *runs.Registry) *browserHost {
	h := &browserHost{db: db, reg: reg, mgr: &browser.Manager{}}
	h.startSession = h.mgr.Get
	h.endAllSessions = h.mgr.CloseAll
	h.settings = db
	h.closeSession = func(ws int64) { h.mgr.Close(sessionKey(ws)) }
	h.killSwitch = config.BrowserDisabled
	h.gate = reg.BrowserAllowed
	h.live = reg.RunIsRunning
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
	h.permit = reg.BrowserGate
	h.revokeGrants = reg.RevokeBrowserGrants
	h.revokeAll = reg.RevokeAllBrowserGrants
	h.forgetRun = reg.ForgetBrowserRun
	h.liveDeny = newLiveDenySource(filepath.Join(config.Dir(), "listeners"), db).Endpoints
	h.launcher = func() (browser.Launcher, error) {
		p, err := browser.DetectChrome("")
		if err != nil {
			return nil, err
		}
		return browser.ChromeLauncher{Path: p}, nil
	}
	h.profileRoot = filepath.Join(config.Dir(), "browser")
	if hub, err := runsock.NewSystemHub(filepath.Join(config.Dir(), "browser", "sock")); err != nil {
		log.Printf("browser: no per-run socket host, so no run gets the browser: %v", err)
	} else {
		h.socks = hub
	}
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

// lookupListenHost resolves a hostname listen address (a variable so a test
// can answer without DNS).
var lookupListenHost = func(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

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
	// A listen address that is a NAME (not an IP literal) binds whatever it
	// resolves to: reserve those addresses too, or fail closed.
	if net.ParseIP(strings.Trim(host, "[]")) == nil && host != "0.0.0.0" {
		lctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		addrs, err := lookupListenHost(lctx, host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("browser: the daemon's listen host %q cannot be resolved, so its addresses cannot be reserved: %v", host, err)
		}
		for _, a := range addrs {
			if i := strings.IndexByte(a, '%'); i >= 0 {
				a = a[:i] // an IPv6 zone is not part of the address
			}
			out = append(out, browser.Endpoint{Host: a, Port: port})
		}
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
	return runs.StoreBrowserGate(h.settings, func() bool { return h.killSwitch() })(ws)
}

func (h *browserHost) session(ctx context.Context, run browser.Run) (*browser.Session, error) {
	h.gateMu.RLock()
	defer h.gateMu.RUnlock()
	// Re-check inside the lock: callRun checked the gates earlier, and a
	// settings change (disable plus close) may have landed since.
	if h.killSwitch() {
		return nil, errors.New("the browser is disabled by the daemon's configuration")
	}
	if !h.gate(run.WorkspaceID) {
		return nil, errors.New("the browser is not enabled for this workspace")
	}
	l, err := h.launcher()
	if err != nil {
		return nil, err // an *UnavailableError: the broker turns it into the plain install text
	}
	pol, allow, err := h.policyFor(run.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return h.startSession(ctx, browser.Config{
		Workspace: sessionKey(run.WorkspaceID),
		Launcher:  l,
		Launch:    browser.LaunchOpts{ProfileRoot: h.profileRoot, AllowHosts: allow},
		Policy:    pol,
	})
}

// policyFor builds the workspace's navigation policy: the stored allowlist, the
// daemon's own addresses (fail closed when they cannot be read), and the live
// deny source (other listeners, linked daemons). It returns the allowlist too.
func (h *browserHost) policyFor(ws int64) (browser.Policy, []string, error) {
	allow := h.allow(ws)
	deny, err := h.deny()
	if err != nil {
		return browser.Policy{}, nil, err // fail closed: no session without the daemon's own address reserved
	}
	pol, err := browser.NewPolicy(allow, deny)
	if err != nil {
		return browser.Policy{}, nil, err
	}
	if h.liveDeny != nil {
		pol = pol.WithLiveDeny(h.liveDeny)
	}
	return pol, allow, nil
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
	h.CloseBrowserSocket(f.RunID)
	if h.forgetRun != nil {
		h.forgetRun(f.RunID) // the run's grants and denial limiter die with it
	}
	h.broker.EndRun(f.RunID)
}

// close ends every browser session and socket (daemon shutdown).
func (h *browserHost) close() {
	if h.socks != nil {
		h.socks.CloseAll()
	}
	h.mgr.CloseAll()
}

// OpenBrowserSocket creates the run's browser socket, bound to the agent
// process rooted at rootPID (runs.BrowserSockets). The socket's handler is
// bound to this run and task: nothing in a request can name another.
func (h *browserHost) OpenBrowserSocket(runID string, taskID int64, rootPID int) (string, error) {
	if h.socks == nil {
		return "", errors.New("browser: no socket host")
	}
	return h.socks.Open(runID, rootPID, func(ctx context.Context, req runsock.Request) (any, error) {
		return h.callRun(ctx, runID, taskID, req.Tool, req.Args)
	})
}

// OpenBrowserSocketPending creates the run's socket before its agent process
// exists (the Claude CLI path); bind attaches it to the process later.
func (h *browserHost) OpenBrowserSocketPending(runID string, taskID int64) (string, func(rootPID int) error, error) {
	if h.socks == nil {
		return "", nil, errors.New("browser: no socket host")
	}
	return h.socks.OpenPending(runID, func(ctx context.Context, req runsock.Request) (any, error) {
		return h.callRun(ctx, runID, taskID, req.Tool, req.Args)
	})
}

// CloseBrowserSocket closes the run's socket and its connections.
func (h *browserHost) CloseBrowserSocket(runID string) {
	if h.socks != nil {
		h.socks.Close(runID)
	}
}

// callRun authorises and runs one browser call of a run the socket's peer
// check already proved. Every refusal is a plain error (the child shows it as a
// tool error).
func (h *browserHost) callRun(ctx context.Context, runID string, taskID int64, tool string, args json.RawMessage) (browser.Result, error) {
	if !h.live(runID) {
		return browser.Result{}, errors.New("browser: the run has ended")
	}
	task, err := h.db.GetTask(taskID)
	if err != nil {
		return browser.Result{}, errors.New("browser: not available")
	}
	ws := task.WorkspaceID
	if h.killSwitch() {
		// The daemon-wide switch beats every workspace: end any browser now,
		// and every grant with it.
		h.endAllSessions()
		if h.revokeAll != nil {
			h.revokeAll()
		}
		return browser.Result{}, errors.New("browser: the browser is disabled by the daemon's configuration")
	}
	if !h.gate(ws) {
		if h.revokeGrants != nil {
			h.revokeGrants(ws)
		}
		return browser.Result{}, errors.New("browser: the browser is not enabled for this workspace")
	}
	if err := h.permission(ctx, runID, ws, tool, args); err != nil {
		return browser.Result{}, err
	}
	run := browser.Run{ID: runID, WorkspaceID: ws, ImagesRead: h.imagesRead(runID)}
	return h.broker.Call(ctx, run, tool, args), nil
}

// permission is the daemon-side gate (ADR-0061 section 10): admitted now,
// refused, or held for a person. A browser_open whose URL the workspace policy
// would refuse is refused here with the policy's reason and raises no card.
func (h *browserHost) permission(ctx context.Context, runID string, ws int64, tool string, args json.RawMessage) error {
	if h.permit == nil {
		return errors.New("browser: no permission gate is installed")
	}
	call := runs.BrowserCall{RunID: runID, Tool: tool}
	pol, allow, err := h.policyFor(ws)
	if err != nil {
		return err
	}
	call.Allowlist = allow
	if tool == "browser_open" {
		var a struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(args, &a) != nil || a.URL == "" {
			return nil // the broker refuses the malformed call itself; nothing to ask about
		}
		d := pol.CheckURL(a.URL)
		if !d.Allowed {
			return errors.New("browser: " + d.Reason)
		}
		call.URL = a.URL
		call.HostStatus = "localhost"
		if h2, herr := browser.NormalizeAllowHost(d.Host); herr == nil {
			for _, a := range allow {
				if n, nerr := browser.NormalizeAllowHost(a); nerr == nil && n == h2 {
					call.HostStatus = "added"
				}
			}
		}
	}
	if err := h.permit(ctx, call); err != nil {
		return errors.New("browser: " + strings.TrimPrefix(err.Error(), "browser permission refused: "))
	}
	return nil
}
