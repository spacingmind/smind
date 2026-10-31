package wsapi

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/serve"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// The callee side of the fleet wire (ADR-0048 F1b): the host handshake
// (fleet.challenge / fleet.hello), the pairing request a candidate host
// makes while the window is open, the link-state notice an approved host
// sends, and the local-only fleet.host.* admin surface. Everything here is
// dark until SetFleet is called: without it none of these methods exist
// (testdata/methods.golden pins that map).
//
// A connection that completes fleet.hello becomes a host principal and is
// then confined to its kind's fleetHostAllowed methods, before the handler
// lookup, so a peer host never reaches a run, a task or any human surface
// whatever it asks for. The params a remote host may send are exactly
// hello.Hello, fleetPairRequestParams and fleetLinkStateParams -- none of
// which can carry a grant or a secret
// (TestFleetParamsHaveNoGrantOrSecretFields).

// codeAliasTaken is the wire code for a pairing whose alias is in use.
const codeAliasTaken = "alias_taken"

// The link-state values an approved peer may announce (fleet.link.state).
const (
	linkStateReverseEnabled  = "reverse_enabled"
	linkStateReverseDisabled = "reverse_disabled"
)

// FleetOptions wires the fleet wire methods to their dependencies: this
// host's identity, the paired-host registry, the human-in-the-loop pairing
// window, the dial-side link manager (FleetLinks, implemented by
// internal/fleet/link), the daemon version advertised in challenges and
// HelloOKs, and the clock (time.Now when nil).
type FleetOptions struct {
	Key      *identity.Key
	Registry *registry.Registry
	Window   *pairing.Window
	Links    FleetLinks
	Version  string
	Now      func() time.Time

	// Store, Workspaces and HomeDirs enable the fleet read methods; they
	// stay unregistered while Store or Workspaces is nil.
	Store      *store.Store
	Workspaces FleetWorkspaces
	HomeDirs   []string
}

// FleetLinks is the dial-side half of the fleet: the operations the
// fleet.host.* admin methods delegate to the link manager.
type FleetLinks interface {
	// Invite mints an invite deep link for a human to carry to the peer.
	Invite(ctx context.Context, p FleetInviteParams) (FleetInviteResult, error)
	// Add pairs this daemon to the inviter behind a pasted invite.
	Add(ctx context.Context, p FleetAddParams) (FleetAddResult, error)
	// Test probes the live link to hostID.
	Test(ctx context.Context, hostID string) (FleetTestResult, error)
	// NotifyReverse tells the peer over our outbound link that its
	// direction to us flipped; best effort.
	NotifyReverse(ctx context.Context, hostID string, on bool) error
	// PeerReverseChanged records that the peer enabled or disabled its
	// direction to us.
	PeerReverseChanged(hostID string, on bool)
	// LinkState reports the live outbound link's manager state ("connected",
	// "connecting", "awaiting_approval", "refused", "peer_disabled", "down",
	// or "none" when no link loop exists) with the refusal code and the
	// human-safe detail, for a host view.
	LinkState(hostID string) (state, code, detail string)
	// SetRoute stores the direct route (and optional password) this daemon
	// dials hostID on and restarts the link so the route takes effect.
	SetRoute(ctx context.Context, p FleetRouteParams) error
	// Forget stops dialing and drops the secrets and relay device of a
	// removed host.
	Forget(hostID string)
	// LinkIssues lists the outbound links a human should look at: refused
	// ones, and ones down for long enough.
	LinkIssues() []FleetLinkIssue
}

// FleetLinkIssue is one outbound link that needs attention. Kind is
// "refused" or "down"; Code and Guidance describe a refusal.
type FleetLinkIssue struct {
	HostID, Alias, Kind, Code, Guidance string
	Since                               time.Time
}

// FleetInviteParams is the ask behind fleet.host.invite: how long the
// invite stays usable and where the invitee may reach this host.
type FleetInviteParams struct {
	WindowSeconds int      `json:"windowSeconds"`
	Via           string   `json:"via"`
	Addresses     []string `json:"addresses"`
}

// FleetInviteResult is the minted invite.
type FleetInviteResult struct {
	Invite    string    `json:"invite"`
	ExpiresAt time.Time `json:"expiresAt"`
	HostID    string    `json:"hostId"`
}

// FleetAddParams is the ask behind fleet.host.add: the invite the human
// pasted, the alias to file the host under, the pairing password and
// whether a cleartext (non-TLS) direct link is acceptable.
type FleetAddParams struct {
	Invite         string `json:"invite"`
	Alias          string `json:"alias"`
	Password       string `json:"password"`
	AllowCleartext bool   `json:"allowCleartext"`
}

// FleetAddResult reports what pairing behind an invite produced.
type FleetAddResult struct {
	HostID  string `json:"hostId"`
	Alias   string `json:"alias"`
	State   string `json:"state"`
	SAS     string `json:"sas"`
	Warning string `json:"warning"`
}

// FleetRouteParams is the ask behind fleet.host.route: the direct address
// to reach an already paired host on.
type FleetRouteParams struct {
	HostID         string `json:"hostId"`
	Address        string `json:"address"`
	Password       string `json:"password"`
	AllowCleartext bool   `json:"allowCleartext"`
}

// FleetTestResult is one link probe.
type FleetTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	State     string `json:"state"`
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
}

// The link values of a host view (the F1c UI wire contract).
const (
	hostLinkActive       = "active"
	hostLinkConnecting   = "connecting"
	hostLinkDown         = "down"
	hostLinkRefused      = "refused"
	hostLinkPeerDisabled = "peer_disabled"
	hostLinkDisabled     = "disabled"
	hostLinkRevoked      = "revoked"
)

// Wire codes of the approve call's typed-SAS check.
const (
	codeSASMismatch    = "sas_mismatch"
	codeSASUnavailable = "sas_unavailable"
)

// FleetHostView is a host as the admin surface shows it (the UI's HostView).
// It names a host by id, alias and label only -- never the public key, a
// password, an address or any secret -- next to its lifecycle state, the
// live outbound link, the inbound direction with its grant and, for a
// pending pairing, the SAS the human must still compare.
type FleetHostView struct {
	HostID string `json:"hostId"`
	Alias  string `json:"alias"`
	Label  string `json:"label"`
	State  string `json:"state"`
	// Labels are the operator's routing labels, caller-local; they appear
	// only on this local admin view, never on the wire.
	Labels map[string]string `json:"labels"`
	// PendingApproval is true only for a responder-side pending host whose
	// SAS this daemon holds: the human here must approve it.
	PendingApproval bool   `json:"pendingApproval"`
	Link            string `json:"link"`
	LinkCode        string `json:"linkCode,omitempty"`
	LinkGuidance    string `json:"linkGuidance,omitempty"`
	OutboundEnabled bool   `json:"outboundEnabled"`
	Inbound         struct {
		Enabled bool           `json:"enabled"`
		Grant   registry.Grant `json:"grant"`
	} `json:"inbound"`
	SAS       string    `json:"sas,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// fleetPairRequestParams is what a candidate host sends with
// fleet.pair.request. Together with hello.Hello and fleetLinkStateParams it
// is the complete set of param shapes a remote host may send, so it must
// never grow a grant, secret or policy field.
type fleetPairRequestParams struct {
	Alias string `json:"alias"`
	Label string `json:"label"`
}

type fleetPairRequestResult struct {
	HostID string `json:"hostId"`
	Alias  string `json:"alias"`
	State  string `json:"state"`
	SAS    string `json:"sas"`
}

type fleetLinkStateParams struct {
	State string `json:"state"`
}

type fleetLinkStateResult struct {
	OK bool `json:"ok"`
}

// fleetHostRef is the host selector every admin method takes: hostId (the
// UI) or host (the CLI); either is an alias, a host id or a host-id prefix.
type fleetHostRef struct {
	HostID string `json:"hostId"`
	Host   string `json:"host"`
}

func (r fleetHostRef) ref() string {
	if r.HostID != "" {
		return r.HostID
	}
	return r.Host
}

type fleetHostParams = fleetHostRef

type fleetHostApproveParams struct {
	fleetHostRef
	SAS             string          `json:"sas"`
	Grant           *registry.Grant `json:"grant"`
	WriteWorkspaces []int64         `json:"writeWorkspaces"`
}

type fleetHostRenameParams struct {
	fleetHostRef
	Alias string `json:"alias"`
}

type fleetHostLabelsParams struct {
	fleetHostRef
	Set    map[string]string `json:"set"`
	Remove []string          `json:"remove"`
}

type fleetHostLabelsResult struct {
	Host   string            `json:"host"`
	HostID string            `json:"hostId"`
	Labels map[string]string `json:"labels"`
}

type fleetHostRouteParams struct {
	fleetHostRef
	Address        string `json:"address"`
	Password       string `json:"password"`
	AllowCleartext bool   `json:"allowCleartext"`
}

type fleetHostGrantParams struct {
	fleetHostRef
	Grant registry.Grant `json:"grant"`
}

// fleetInviteParams is the invite ask on the wire: the window as a Go
// duration string (the UI) or in seconds (the CLI), or neither.
type fleetInviteParams struct {
	Window        string   `json:"window"`
	WindowSeconds int      `json:"windowSeconds"`
	Via           string   `json:"via"`
	Addresses     []string `json:"addresses"`
}

// fleetAddResult keeps the flat pairing fields (the CLI) next to the host
// view (the UI).
type fleetAddResult struct {
	FleetAddResult
	Host *FleetHostView `json:"host,omitempty"`
}

type fleetHostResult struct {
	Host *FleetHostView `json:"host"`
}

type fleetWindowView struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Remaining int       `json:"remaining"`
}

type fleetHostListResult struct {
	Hosts  []FleetHostView  `json:"hosts"`
	Window *fleetWindowView `json:"window"`
}

type fleetHostActionResult struct {
	Host    *FleetHostView `json:"host"`
	Warning string         `json:"warning,omitempty"`
}

type fleetOKResult struct {
	OK bool `json:"ok"`
}

// fleetHost is the API's fleet state: the options SetFleet installed, the
// live host-principal connections by host id, and the SAS a human should
// still compare for a pending pairing.
type fleetHost struct {
	opts FleetOptions
	bus  *eventBus

	mu    sync.Mutex
	conns map[string]map[*conn]struct{}
	sas   map[string]string

	read *serve.Server
}

// hostPrincipal is what a connection proved about itself with a verified
// fleet.hello. kind is one of the fleetHostAllowed keys and confines every
// further request on that connection.
type hostPrincipal struct {
	hostID string
	pub    ed25519.PublicKey
	kind   string // "pairing" | "pending" | "approved"
	nonceC []byte
	nonceA []byte
	ts     int64
	seen   time.Time
	sas    string
}

// fleetHostAllowed is the exact method set a host principal may call, by
// kind. No kind may run the handshake again, so a captured challenge or
// hello cannot be retold on a connection that already spoke for a host.
var fleetHostAllowed = map[string][]string{
	"pairing":  {hello.MethodPairRequest},
	"pending":  {},
	"approved": approvedHostMethods(),
}

// approvedHostMethods is the method set of an approved host: the link-state
// notice, the fleet read methods and the fleet write methods.
func approvedHostMethods() []string {
	return append(append([]string{hello.MethodLinkState}, wire.ReadMethods...), wire.WriteMethods...)
}

// hostPrincipal returns the connection's fleet host principal, if any.
func (c *conn) hostPrincipal() *hostPrincipal {
	c.identMu.Lock()
	defer c.identMu.Unlock()
	return c.host
}

// SetFleet registers the fleet.* wire methods. Like SetRelayDevices it must
// be called at most once, at startup before the API serves any connection
// (it writes the shared handler table without locking). It panics when
// called twice or with a nil Key, Registry, Window or Links.
func (a *API) SetFleet(o FleetOptions) {
	if a.fleet != nil {
		panic("wsapi: SetFleet called twice")
	}
	if o.Key == nil || o.Registry == nil || o.Window == nil || o.Links == nil {
		panic("wsapi: SetFleet requires Key, Registry, Window and Links")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	f := &fleetHost{opts: o, bus: a.bus, conns: map[string]map[*conn]struct{}{}, sas: map[string]string{}}
	a.fleet = f
	if a.inbox != nil {
		a.inbox.setLinkIssues(fleetInboxLinkIssues(o.Links, func() bool { return configFleetEnabled() }))
	}
	a.hs[hello.MethodChallenge] = f.handleChallenge
	a.hs[hello.MethodHello] = f.handleHello
	a.hs[hello.MethodPairRequest] = f.handlePairRequest
	a.hs[hello.MethodLinkState] = f.handleLinkState
	a.hs["fleet.host.list"] = fleetLocalOnly("fleet.host.list", f.handleHostList)
	a.hs["fleet.host.invite"] = fleetLocalOnly("fleet.host.invite", f.handleHostInvite)
	a.hs["fleet.host.add"] = fleetLocalOnly("fleet.host.add", f.handleHostAdd)
	a.hs["fleet.host.approve"] = fleetLocalOnly("fleet.host.approve", f.handleHostApprove)
	a.hs["fleet.host.reject"] = fleetLocalOnly("fleet.host.reject", f.handleHostReject)
	a.hs["fleet.host.rename"] = fleetLocalOnly("fleet.host.rename", f.handleHostRename)
	a.hs["fleet.host.labels"] = fleetLocalOnly("fleet.host.labels", f.handleHostLabels)
	a.hs["fleet.host.grant"] = fleetLocalOnly("fleet.host.grant", f.handleHostGrant)
	a.hs["fleet.host.rm"] = fleetLocalOnly("fleet.host.rm", f.handleHostRm)
	a.hs["fleet.host.revoke"] = fleetLocalOnly("fleet.host.revoke", f.handleHostRevoke)
	a.hs["fleet.host.enable_reverse"] = fleetLocalOnly("fleet.host.enable_reverse", f.handleHostEnableReverse)
	a.hs["fleet.host.disable_reverse"] = fleetLocalOnly("fleet.host.disable_reverse", f.handleHostDisableReverse)
	a.hs["fleet.host.test"] = fleetLocalOnly("fleet.host.test", f.handleHostTest)
	a.hs["fleet.host.route"] = fleetLocalOnly("fleet.host.route", f.handleHostRoute)
	if o.Store != nil {
		a.hs["fleet.host.audit"] = fleetLocalOnly("fleet.host.audit", f.handleHostAudit)
	}
	if o.Store != nil && o.Workspaces != nil {
		// One source serves the Reader and the write path's TaskLookup, so
		// a send or a stop decides on the same task rows a read returns.
		src := &fleetSource{db: o.Store, ws: o.Workspaces}
		opts := serve.Options{
			HostID:     o.Key.HostID(),
			Authorizer: o.Registry,
			Audit:      o.Store,
			Reader: serve.NewReader(src, serve.ReaderOptions{
				HostID: o.Key.HostID(), Version: o.Version, Caps: hello.Caps(), HomeDirs: o.HomeDirs,
			}),
			Now: o.Now,
		}
		// The write methods need the effects only this API can perform;
		// serve leaves them "unknown method" until Backend, Ledger and
		// Tasks are all wired, which is always the case outside odd tests.
		var backend *fleetBackend
		if a.wm != nil && a.runner != nil {
			backend = &fleetBackend{a: a, db: o.Store}
			opts.Backend, opts.Ledger, opts.Tasks = backend, o.Store, src
		}
		f.read = serve.New(opts)
		for _, m := range wire.ReadMethods {
			a.hs[m] = f.readHandler(m)
		}
		for _, m := range wire.WriteMethods {
			a.hs[m] = f.readHandler(m)
		}
		a.fleetAuditSweep = serve.StartAuditSweeper(o.Store, store.FleetAuditRetention, time.Hour)
		if backend != nil {
			a.fleetWallSweep = serve.StartWallSweeper(o.Store, o.Registry, o.Store, backend.StopRun, 0, o.Now)
		}
		a.fleetIdemSweep = startFleetIdemSweeper(o.Store, o.Now)
	}
}

// FleetHostsChanged tells subscribers that the host list or a link changed
// (event topic fleet.hosts.changed, empty payload). It is a no-op until
// SetFleet ran, and safe to call from any goroutine afterwards.
func (a *API) FleetHostsChanged() {
	if a.fleet == nil {
		return
	}
	a.fleet.changed()
}

func (f *fleetHost) changed() {
	if f.bus != nil {
		f.bus.Publish(Event{Topic: TopicFleetHostsChanged})
	}
}

// FleetCloseHost closes every live host-principal connection of hostID, so
// the peer notices at once instead of learning it at its next refused call.
// Removing or darkening a host calls it; safe when the host has no live
// connection.
func (a *API) FleetCloseHost(hostID string) {
	if a.fleet == nil {
		return
	}
	a.fleet.closeHostConns(hostID)
}

// registerConn records c as hostID's live connection. One connection per
// peer host is kept (config.MaxConnectionsPerPeer is 1): a second
// connection replaces and closes the first.
func (f *fleetHost) registerConn(hostID string, c *conn) {
	f.mu.Lock()
	prev := f.conns[hostID]
	f.conns[hostID] = map[*conn]struct{}{c: {}}
	f.mu.Unlock()
	for pc := range prev {
		_ = pc.tr.Close()
	}
	f.changed()
}

// dropConn removes c from hostID's live connections (idempotent).
func (f *fleetHost) dropConn(hostID string, c *conn) {
	f.mu.Lock()
	if set := f.conns[hostID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(f.conns, hostID)
		}
	}
	f.mu.Unlock()
	f.changed()
}

// closeHostConns closes every live connection of hostID.
func (f *fleetHost) closeHostConns(hostID string) {
	f.mu.Lock()
	set := f.conns[hostID]
	delete(f.conns, hostID)
	f.mu.Unlock()
	for c := range set {
		_ = c.tr.Close()
	}
}

// setHostPrincipal establishes p on c and arranges the connection's
// deregistration when it ends.
func (f *fleetHost) setHostPrincipal(c *conn, p *hostPrincipal) {
	c.identMu.Lock()
	c.host = p
	c.onClose = func() { f.dropConn(p.hostID, c) }
	c.identMu.Unlock()
	f.registerConn(p.hostID, c)
}

// handleChallenge mints the handshake challenge for any admitted
// connection: a fresh nonce this connection's fleet.hello must answer. A
// second challenge replaces the first; each is single use.
func (f *fleetHost) handleChallenge(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
	nonceC, err := hello.NewNonce()
	if err != nil {
		return nil, fmt.Errorf("fleet.challenge: %w", err)
	}
	c := rc.conn
	c.identMu.Lock()
	c.fleetNonceC = nonceC
	c.identMu.Unlock()
	return hello.Challenge{
		NonceC:  nonceC,
		HostID:  f.opts.Key.HostID(),
		FleetV:  hello.FleetV,
		Caps:    hello.Caps(),
		Version: f.opts.Version,
	}, nil
}

// handleHello verifies the caller's Hello against this connection's
// challenge and decides what the caller may become: a pairing candidate
// while the window is open, an approved peer when its inbound link is
// live, or a refusal naming why. Success establishes the host principal
// and answers with a signed HelloOK binding the state.
func (f *fleetHost) handleHello(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	o := f.opts
	c := rc.conn
	c.identMu.Lock()
	nonceC := c.fleetNonceC
	c.fleetNonceC = nil
	c.identMu.Unlock()
	if nonceC == nil {
		return nil, &rpcCodeError{code: hello.CodeReplay, msg: "no challenge for this connection"}
	}
	var h hello.Hello
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, &rpcCodeError{code: hello.CodeBadHello, msg: "fleet.hello: invalid params: " + err.Error(), err: err}
	}
	now := o.Now()
	if err := hello.VerifyHello(h, o.Key.HostID(), nonceC, now); err != nil {
		var he *hello.Error
		if errors.As(err, &he) {
			return nil, &rpcCodeError{code: he.Code, msg: he.Msg, err: err}
		}
		return nil, fmt.Errorf("fleet.hello: %w", err)
	}
	if err := hello.CheckVersion(h.HostID[:8], h.FleetV, h.Caps, h.Version); err != nil {
		return nil, err // *hello.UnsupportedError carries its own code and data
	}
	if rc.deviceID != "" && rc.deviceID != h.HostID {
		return nil, forbidden("fleet.hello: this connection belongs to another device")
	}
	var kind, state string
	host, err := o.Registry.Get(h.HostID)
	if err != nil {
		if !errors.Is(err, registry.ErrNotFound) {
			return nil, fmt.Errorf("fleet.hello: %w", err)
		}
		if open, _, _ := o.Window.Status(); !open {
			return nil, &rpcCodeError{code: hello.CodeUnknownHost, msg: "fleet.hello: unknown host and the pairing window is closed", err: err}
		}
		kind, state = "pairing", "pairing"
	} else {
		switch host.State {
		case registry.StateRevoked:
			return nil, &rpcCodeError{code: hello.CodeRevoked, msg: "fleet.hello: host has been revoked"}
		case registry.StatePending:
			return nil, &rpcCodeError{code: hello.CodeAwaitingApproval, msg: "fleet.hello: pairing awaits approval"}
		case registry.StateApproved:
			_, inbound, err := o.Registry.Links(h.HostID)
			if err != nil {
				return nil, fmt.Errorf("fleet.hello: %w", err)
			}
			if !inbound.Enabled {
				// The reverse-direction refusal: the peer may not dial us.
				return nil, &rpcCodeError{code: hello.CodeNotGranted, msg: "fleet.hello: our inbound link to this host is not enabled"}
			}
			kind, state = "approved", "active"
		default:
			return nil, fmt.Errorf("fleet.hello: host %s is in unknown state %q", h.HostID, host.State)
		}
	}
	f.setHostPrincipal(c, &hostPrincipal{
		hostID: h.HostID,
		pub:    append(ed25519.PublicKey(nil), h.Pub...),
		kind:   kind,
		nonceC: append([]byte(nil), nonceC...),
		nonceA: append([]byte(nil), h.NonceA...),
		ts:     h.TS,
		seen:   now,
	})
	return hello.SignOK(o.Key, h.HostID, nonceC, h.NonceA, h.TS, state, o.Version, hello.Caps()), nil
}

// handlePairRequest is a pairing candidate's ask: it consumes one window
// slot and records the host as pending, pending the human's approval. The
// SAS both humans must compare is derived from the handshake's keys and
// nonces, so it is bound to exactly this pairing.
func (f *fleetHost) handlePairRequest(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.pair.request"
	var p fleetPairRequestParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	pr := rc.conn.hostPrincipal() // the gate admits only kind "pairing"
	if pr == nil {
		return nil, forbidden(method + ": no host principal")
	}
	if !f.opts.Window.Take(pr.seen) {
		return nil, &rpcCodeError{code: hello.CodeNotGranted, msg: "pairing window is closed"}
	}
	h, err := f.opts.Registry.AcceptPairRequest(pr.pub, p.Alias, p.Label)
	if err != nil {
		if errors.Is(err, registry.ErrAliasTaken) {
			return nil, &rpcCodeError{code: codeAliasTaken, msg: method + ": alias is already taken", err: err}
		}
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	sas := pairing.SAS(pr.pub, f.opts.Key.Public(), pr.nonceC, pr.nonceA)
	f.mu.Lock()
	f.sas[h.HostID] = sas
	f.mu.Unlock()
	rc.conn.identMu.Lock()
	pr.kind = "pending"
	pr.sas = sas
	rc.conn.identMu.Unlock()
	f.changed()
	return fleetPairRequestResult{HostID: h.HostID, Alias: h.Alias, State: registry.StatePending, SAS: sas}, nil
}

// handleLinkState records an approved peer's notice that its direction to
// us went live or dark. It only flips our outbound link's enabled flag --
// the grant governing what the peer may do here is never read or written.
func (f *fleetHost) handleLinkState(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.link.state"
	var p fleetLinkStateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	var on bool
	switch p.State {
	case linkStateReverseEnabled:
		on = true
	case linkStateReverseDisabled:
		on = false
	default:
		return nil, &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("%s: unknown state %q", method, p.State)}
	}
	pr := rc.conn.hostPrincipal() // the gate admits only kind "approved"
	if pr == nil {
		return nil, forbidden(method + ": no host principal")
	}
	if err := f.opts.Registry.SetOutboundEnabled(pr.hostID, on); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	f.opts.Links.PeerReverseChanged(pr.hostID, on)
	f.changed()
	return fleetLinkStateResult{OK: true}, nil
}

// fleetLocalOnly refuses h on any connection that is not the local /ws one:
// no paired device or peer host may administer the fleet.
func fleetLocalOnly(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if !rc.local {
			return nil, &rpcCodeError{code: codeForbidden, msg: method + " is not available over the relay"}
		}
		return h(ctx, rc, raw)
	}
}

// fleetHostError maps registry errors to wire codes: a missing host and a
// taken alias get their own codes, everything else (grant, alias and label
// validation included) stays a plain error naming the method.
func fleetHostError(method string, err error) error {
	switch {
	case errors.Is(err, registry.ErrNotFound):
		return &rpcCodeError{code: codeNotFound, msg: method + ": no such host", err: err}
	case errors.Is(err, registry.ErrAliasTaken):
		return &rpcCodeError{code: codeAliasTaken, msg: method + ": alias is already taken", err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

// resolveParams decodes the {host} admin param and resolves it to a
// registry host.
func (f *fleetHost) resolveParams(method string, raw json.RawMessage) (registry.Host, error) {
	var p fleetHostRef
	if err := json.Unmarshal(raw, &p); err != nil {
		return registry.Host{}, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	return f.resolveHost(method, p.ref())
}

// resolveHost maps the `host` admin param to a registry host: an exact
// alias wins, then an exact host id, then a unique host-id prefix of at
// least 4 characters.
func (f *fleetHost) resolveHost(method, host string) (registry.Host, error) {
	if host == "" {
		return registry.Host{}, &rpcCodeError{code: codeInvalid, msg: method + ": host is required"}
	}
	if h, err := f.opts.Registry.ByAlias(host); err == nil {
		return h, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return registry.Host{}, fmt.Errorf("%s: %w", method, err)
	}
	hosts, err := f.opts.Registry.List()
	if err != nil {
		return registry.Host{}, fmt.Errorf("%s: %w", method, err)
	}
	var matches []registry.Host
	for _, h := range hosts {
		if h.HostID == host || (len(host) >= 4 && strings.HasPrefix(h.HostID, host)) {
			matches = append(matches, h)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return registry.Host{}, &rpcCodeError{code: codeNotFound, msg: method + ": no such host"}
	default:
		return registry.Host{}, &rpcCodeError{code: codeAmbiguous, msg: method + ": host id prefix is ambiguous; use more characters"}
	}
}

// view renders a registry host for the admin surface; see FleetHostView
// for what deliberately is not in it.
func (f *fleetHost) view(h registry.Host) (FleetHostView, error) {
	v := FleetHostView{HostID: h.HostID, Alias: h.Alias, Label: h.Label, State: h.State, CreatedAt: h.CreatedAt, Labels: labelsOrEmpty(h.Labels)}
	v.Inbound.Grant = registry.DefaultForwardGrant()
	v.Inbound.Grant.WorkspacesWrite = []int64{}
	outbound, inbound, err := f.opts.Registry.Links(h.HostID)
	switch {
	case err == nil:
		v.OutboundEnabled = outbound.Enabled
		v.Inbound.Enabled = inbound.Enabled
		if g, err := registry.ParseGrant(inbound.GrantJSON); err == nil {
			v.Inbound.Grant = g
		}
	case errors.Is(err, registry.ErrNotFound):
		// A host without link rows reads as dark in both directions.
	default:
		return FleetHostView{}, fmt.Errorf("fleet.host: %w", err)
	}
	if h.State == registry.StatePending {
		f.mu.Lock()
		v.SAS = f.sas[h.HostID]
		f.mu.Unlock()
		v.PendingApproval = v.SAS != ""
	}
	v.Link, v.LinkCode, v.LinkGuidance = f.linkView(h, v.OutboundEnabled)
	return v, nil
}

// linkView maps the link manager's state to the contract's link value; the
// code and guidance accompany a refusal only.
func (f *fleetHost) linkView(h registry.Host, outboundEnabled bool) (link, code, guidance string) {
	if h.State == registry.StateRevoked {
		return hostLinkRevoked, "", ""
	}
	if !outboundEnabled {
		return hostLinkDisabled, "", ""
	}
	state, code, detail := f.opts.Links.LinkState(h.HostID)
	switch state {
	case "connected":
		return hostLinkActive, "", ""
	case "connecting", "awaiting_approval":
		return hostLinkConnecting, "", ""
	case "refused":
		return hostLinkRefused, code, detail
	case "peer_disabled":
		return hostLinkPeerDisabled, "", ""
	case "none":
		return hostLinkDisabled, "", ""
	}
	return hostLinkDown, "", ""
}

// hostViewByID renders the current registry row of hostID.
func (f *fleetHost) hostViewByID(hostID string) (FleetHostView, error) {
	h, err := f.opts.Registry.Get(hostID)
	if err != nil {
		return FleetHostView{}, fleetHostError("fleet.host", err)
	}
	return f.view(h)
}

func (f *fleetHost) handleHostList(context.Context, *requestContext, json.RawMessage) (any, error) {
	hosts, err := f.opts.Registry.List()
	if err != nil {
		return nil, fmt.Errorf("fleet.host.list: %w", err)
	}
	views := make([]FleetHostView, 0, len(hosts))
	for _, h := range hosts {
		v, err := f.view(h)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	var w *fleetWindowView
	if open, expires, remaining := f.opts.Window.Status(); open {
		w = &fleetWindowView{ExpiresAt: expires, Remaining: remaining}
	}
	return fleetHostListResult{Hosts: views, Window: w}, nil
}

// hostResult is the {host} answer of a mutation: the host's current view.
func (f *fleetHost) hostResult(hostID string) (any, error) {
	v, err := f.hostViewByID(hostID)
	if err != nil {
		return nil, err
	}
	return fleetHostResult{Host: &v}, nil
}

func (f *fleetHost) handleHostInvite(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.invite"
	var p fleetInviteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	seconds := p.WindowSeconds
	if p.Window != "" {
		d, err := time.ParseDuration(p.Window)
		if err != nil || d <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("%s: window %q is not a positive duration", method, p.Window)}
		}
		seconds = int(d / time.Second)
	}
	if seconds < 0 {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": window must not be negative"}
	}
	res, err := f.opts.Links.Invite(ctx, FleetInviteParams{WindowSeconds: seconds, Via: p.Via, Addresses: p.Addresses})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	f.changed()
	return res, nil
}

func (f *fleetHost) handleHostAdd(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	var p FleetAddParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("fleet.host.add: invalid params: %w", err)
	}
	res, err := f.opts.Links.Add(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("fleet.host.add: %w", err)
	}
	out := fleetAddResult{FleetAddResult: res}
	if v, err := f.hostViewByID(res.HostID); err == nil {
		// The initiator's SAS is not held for approval: the other human
		// approves, and this side only compares.
		v.SAS = res.SAS
		out.Host = &v
	}
	f.changed()
	return out, nil
}

// normalizeSAS strips the spaces and dashes a human or a client adds when
// grouping the six digits.
func normalizeSAS(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, s)
}

// handleHostApprove is the human act that makes a pending pairing live. The
// typed SAS is required and must equal the one this daemon holds for the
// host, so a client cannot confirm blindly; a daemon that no longer holds
// it (restarted) refuses and the pairing must be redone. The grant is the
// given one, else the default forward grant narrowed to the write
// workspaces; the registry validates either.
func (f *fleetHost) handleHostApprove(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.approve"
	var p fleetHostApproveParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.ref())
	if err != nil {
		return nil, err
	}
	if h.State != registry.StatePending {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": host is not pending approval"}
	}
	f.mu.Lock()
	held := f.sas[h.HostID]
	f.mu.Unlock()
	if held == "" {
		return nil, &rpcCodeError{code: codeSASUnavailable, msg: method + ": re-pair: the pairing code is no longer held"}
	}
	if subtle.ConstantTimeCompare([]byte(normalizeSAS(p.SAS)), []byte(held)) != 1 {
		return nil, &rpcCodeError{code: codeSASMismatch, msg: method + ": the code does not match the one shown for this host"}
	}
	var grant registry.Grant
	if p.Grant != nil {
		grant = *p.Grant
	} else {
		grant = registry.DefaultForwardGrant()
		grant.WorkspacesWrite = p.WriteWorkspaces
	}
	if err := f.opts.Registry.Approve(h.HostID, grant); err != nil {
		return nil, fleetHostError(method, err)
	}
	f.mu.Lock()
	delete(f.sas, h.HostID)
	f.mu.Unlock()
	res, err := f.hostResult(h.HostID)
	f.changed()
	return res, err
}

func (f *fleetHost) handleHostReject(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.reject"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.Reject(h.HostID); err != nil {
		return nil, fleetHostError(method, err)
	}
	f.closeHostConns(h.HostID)
	f.changed()
	return fleetOKResult{OK: true}, nil
}

func (f *fleetHost) handleHostRename(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.rename"
	var p fleetHostRenameParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.ref())
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.Rename(h.HostID, p.Alias); err != nil {
		return nil, fleetHostError(method, err)
	}
	res, err := f.hostResult(h.HostID)
	f.changed()
	return res, err
}

func labelsOrEmpty(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// handleHostLabels merges set pairs into and removes keys from the host's
// operator labels; with neither it only reads them.
func (f *fleetHost) handleHostLabels(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.labels"
	var p fleetHostLabelsParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.ref())
	if err != nil {
		return nil, err
	}
	labels := labelsOrEmpty(h.Labels)
	if len(p.Set) > 0 || len(p.Remove) > 0 {
		for k, v := range p.Set {
			labels[k] = v
		}
		for _, k := range p.Remove {
			delete(labels, k)
		}
		if err := registry.ValidateLabels(labels); err != nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
		}
		if err := f.opts.Registry.SetLabels(h.HostID, labels); err != nil {
			return nil, fleetHostError(method, err)
		}
		f.changed()
	}
	return fleetHostLabelsResult{Host: h.Alias, HostID: h.HostID, Labels: labels}, nil
}

func (f *fleetHost) handleHostGrant(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.grant"
	var p fleetHostGrantParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.ref())
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.SetGrant(h.HostID, p.Grant); err != nil {
		return nil, fleetHostError(method, err)
	}
	res, err := f.hostResult(h.HostID)
	f.changed()
	return res, err
}

// handleHostRm removes a host: an approved host is revoked first (the
// audit trail survives), a pending pairing is rejected outright; either
// way its live connections die, the link manager forgets its secrets and
// the registry row goes.
func (f *fleetHost) handleHostRm(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.rm"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	switch h.State {
	case registry.StateApproved:
		if err := f.opts.Registry.Revoke(h.HostID); err != nil {
			return nil, fleetHostError(method, err)
		}
	case registry.StatePending:
		if err := f.opts.Registry.Reject(h.HostID); err != nil {
			return nil, fleetHostError(method, err)
		}
	}
	f.closeHostConns(h.HostID)
	f.opts.Links.Forget(h.HostID)
	if err := f.opts.Registry.Remove(h.HostID); err != nil && !errors.Is(err, registry.ErrNotFound) {
		return nil, fleetHostError(method, err)
	}
	f.mu.Lock()
	delete(f.sas, h.HostID)
	f.mu.Unlock()
	f.changed()
	return fleetOKResult{OK: true}, nil
}

// handleHostRevoke ends an approved host's authority for good: both links go
// dark, its live connections die and the link manager forgets it. The row
// stays, marked revoked.
func (f *fleetHost) handleHostRevoke(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.revoke"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	if h.State != registry.StateApproved {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": only an approved host can be revoked"}
	}
	if err := f.opts.Registry.Revoke(h.HostID); err != nil {
		return nil, fleetHostError(method, err)
	}
	f.closeHostConns(h.HostID)
	f.opts.Links.Forget(h.HostID)
	res, err := f.hostResult(h.HostID)
	f.changed()
	return res, err
}

// handleHostEnableReverse lights the inbound link and tells the peer. The
// grant already flipped when NotifyReverse runs, so a failed notice is a
// warning in the result, not a failure of the call.
func (f *fleetHost) handleHostEnableReverse(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.enable_reverse"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.EnableReverse(h.HostID); err != nil {
		return nil, fleetHostError(method, err)
	}
	warning := ""
	if err := f.opts.Links.NotifyReverse(ctx, h.HostID, true); err != nil {
		warning = err.Error()
	}
	v, err := f.hostViewByID(h.HostID)
	if err != nil {
		return nil, err
	}
	f.changed()
	return fleetHostActionResult{Host: &v, Warning: warning}, nil
}

// handleHostDisableReverse darkens the inbound link; the peer's live
// connection dies with its authority and the notice it gets is best
// effort.
func (f *fleetHost) handleHostDisableReverse(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.disable_reverse"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.DisableReverse(h.HostID); err != nil {
		return nil, fleetHostError(method, err)
	}
	f.closeHostConns(h.HostID)
	_ = f.opts.Links.NotifyReverse(ctx, h.HostID, false)
	v, err := f.hostViewByID(h.HostID)
	if err != nil {
		return nil, err
	}
	f.changed()
	return fleetHostActionResult{Host: &v}, nil
}

func (f *fleetHost) handleHostTest(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.test"
	h, err := f.resolveParams(method, raw)
	if err != nil {
		return nil, err
	}
	res, err := f.opts.Links.Test(ctx, h.HostID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return res, nil
}

// handleHostRoute sets the direct route this daemon dials a paired host on;
// a reverse link enabled by the peer has no route until a human gives one.
func (f *fleetHost) handleHostRoute(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.route"
	var p fleetHostRouteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.ref())
	if err != nil {
		return nil, err
	}
	err = f.opts.Links.SetRoute(ctx, FleetRouteParams{HostID: h.HostID, Address: p.Address, Password: p.Password, AllowCleartext: p.AllowCleartext})
	if err != nil {
		return nil, fleetHostError(method, err)
	}
	f.changed()
	return fleetOKResult{OK: true}, nil
}
