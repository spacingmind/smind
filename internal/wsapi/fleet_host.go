package wsapi

import (
	"context"
	"crypto/ed25519"
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
	// LinkState reports the live link's state and detail for a host view.
	LinkState(hostID string) (state, detail string)
	// SetRoute stores the direct route (and optional password) this daemon
	// dials hostID on and restarts the link so the route takes effect.
	SetRoute(ctx context.Context, p FleetRouteParams) error
	// Forget stops dialing and drops the secrets and relay device of a
	// removed host.
	Forget(hostID string)
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

// FleetHostView is a host as the admin surface shows it. It names a host by
// id, alias and label only -- never the public key, a password or any
// secret -- next to its lifecycle state, both link directions, the live
// link and, for a pending pairing, the SAS the human must still compare.
type FleetHostView struct {
	HostID   string `json:"hostId"`
	Alias    string `json:"alias"`
	Label    string `json:"label"`
	State    string `json:"state"`
	Outbound struct {
		Enabled bool `json:"enabled"`
	} `json:"outbound"`
	Inbound struct {
		Enabled bool            `json:"enabled"`
		Grant   *registry.Grant `json:"grant,omitempty"`
	} `json:"inbound"`
	Link struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
	} `json:"link"`
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

type fleetHostParams struct {
	Host string `json:"host"`
}

type fleetHostApproveParams struct {
	Host            string  `json:"host"`
	WriteWorkspaces []int64 `json:"writeWorkspaces"`
}

type fleetHostRenameParams struct {
	Host  string `json:"host"`
	Alias string `json:"alias"`
}

type fleetHostRouteParams struct {
	Host           string `json:"host"`
	Address        string `json:"address"`
	Password       string `json:"password"`
	AllowCleartext bool   `json:"allowCleartext"`
}

type fleetHostGrantParams struct {
	Host  string         `json:"host"`
	Grant registry.Grant `json:"grant"`
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

	mu    sync.Mutex
	conns map[string]map[*conn]struct{}
	sas   map[string]string
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
	"approved": {hello.MethodLinkState},
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
	f := &fleetHost{opts: o, conns: map[string]map[*conn]struct{}{}, sas: map[string]string{}}
	a.fleet = f
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
	a.hs["fleet.host.grant"] = fleetLocalOnly("fleet.host.grant", f.handleHostGrant)
	a.hs["fleet.host.rm"] = fleetLocalOnly("fleet.host.rm", f.handleHostRm)
	a.hs["fleet.host.enable_reverse"] = fleetLocalOnly("fleet.host.enable_reverse", f.handleHostEnableReverse)
	a.hs["fleet.host.disable_reverse"] = fleetLocalOnly("fleet.host.disable_reverse", f.handleHostDisableReverse)
	a.hs["fleet.host.test"] = fleetLocalOnly("fleet.host.test", f.handleHostTest)
	a.hs["fleet.host.route"] = fleetLocalOnly("fleet.host.route", f.handleHostRoute)
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
}

// dropConn removes c from hostID's live connections (idempotent).
func (f *fleetHost) dropConn(hostID string, c *conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if set := f.conns[hostID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(f.conns, hostID)
		}
	}
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
	var p fleetHostParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return registry.Host{}, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	return f.resolveHost(method, p.Host)
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
	v := FleetHostView{HostID: h.HostID, Alias: h.Alias, Label: h.Label, State: h.State, CreatedAt: h.CreatedAt}
	outbound, inbound, err := f.opts.Registry.Links(h.HostID)
	switch {
	case err == nil:
		v.Outbound.Enabled = outbound.Enabled
		v.Inbound.Enabled = inbound.Enabled
		if g, err := registry.ParseGrant(inbound.GrantJSON); err == nil {
			v.Inbound.Grant = &g
		}
	case errors.Is(err, registry.ErrNotFound):
		// A host without link rows reads as dark in both directions.
	default:
		return FleetHostView{}, fmt.Errorf("fleet.host: %w", err)
	}
	v.Link.State, v.Link.Detail = f.opts.Links.LinkState(h.HostID)
	if h.State == registry.StatePending {
		f.mu.Lock()
		v.SAS = f.sas[h.HostID]
		f.mu.Unlock()
	}
	return v, nil
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

func (f *fleetHost) handleHostInvite(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	var p FleetInviteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("fleet.host.invite: invalid params: %w", err)
	}
	res, err := f.opts.Links.Invite(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("fleet.host.invite: %w", err)
	}
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
	return res, nil
}

// handleHostApprove is the human act that makes a pending pairing live:
// the default forward grant narrowed to the chosen write workspaces. The
// SAS the human compared stops being shown.
func (f *fleetHost) handleHostApprove(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.approve"
	var p fleetHostApproveParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.Host)
	if err != nil {
		return nil, err
	}
	grant := registry.DefaultForwardGrant()
	grant.WorkspacesWrite = p.WriteWorkspaces
	if err := f.opts.Registry.Approve(h.HostID, grant); err != nil {
		return nil, fleetHostError(method, err)
	}
	f.mu.Lock()
	delete(f.sas, h.HostID)
	f.mu.Unlock()
	return f.hostViewByID(h.HostID)
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
	return fleetOKResult{OK: true}, nil
}

func (f *fleetHost) handleHostRename(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.rename"
	var p fleetHostRenameParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.Host)
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.Rename(h.HostID, p.Alias); err != nil {
		return nil, fleetHostError(method, err)
	}
	return f.hostViewByID(h.HostID)
}

func (f *fleetHost) handleHostGrant(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.grant"
	var p fleetHostGrantParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	h, err := f.resolveHost(method, p.Host)
	if err != nil {
		return nil, err
	}
	if err := f.opts.Registry.SetGrant(h.HostID, p.Grant); err != nil {
		return nil, fleetHostError(method, err)
	}
	return f.hostViewByID(h.HostID)
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
	return fleetOKResult{OK: true}, nil
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
	h, err := f.resolveHost(method, p.Host)
	if err != nil {
		return nil, err
	}
	err = f.opts.Links.SetRoute(ctx, FleetRouteParams{HostID: h.HostID, Address: p.Address, Password: p.Password, AllowCleartext: p.AllowCleartext})
	if err != nil {
		return nil, fleetHostError(method, err)
	}
	return fleetOKResult{OK: true}, nil
}
