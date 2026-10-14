// Package server implements the gRPC relay service defined in
// internal/relay/relaypb: admission delegated to internal/relay/admission,
// plus dumb-pipe forwarding of opaque Frame envelopes between a daemon
// connection and a paired device connection, with a bounded reconnect
// buffer (ADR-0007 (a)). The relay never sees an E2EE session key or
// plaintext: Frame payload bytes are copied through, never inspected.
//
// Fanout (ADR-0007 (b)): a relay host may have multiple paired devices,
// each with its own OpenData stream and its own E2EE session (and hence
// its own route, keyed (relay host, session, device), with one bounded
// queue per side). There is deliberately NO device-to-device forwarding:
// the daemon reaches each device over its own route, and a device's
// frames go only to the daemon. That is the conservative reading of the
// ADR/plan (which specify daemon↔device pipes and never name
// device-to-device traffic), and it is also the only cryptographically
// coherent one — each session has its own key (ADR-0007 (e)), so a
// ciphertext sealed for device 1 could not be opened by device 2 anyway;
// a daemon "broadcast" is the same event separately encrypted and sent
// on each per-device stream, which the relay sees as ordinary
// independent routes.
//
// Multi-device (ADR-0031 D9): the relay keeps ONE control stream per relay
// host and uses it to tell the daemon which device routes are waiting for a
// daemon side (DeviceAttach / DeviceDetach / Sync), so the daemon can open
// one data stream per device. Everything is keyed by the authenticated relay
// host binding, so a route, a control stream, a Revoke or a device count of
// one relay host can never reach another's. Routing is by opaque ids only;
// payloads are never inspected.
package server

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// DefaultBufferCap is the reconnect-grace buffer size per (side, session):
// frames held for a briefly disconnected peer, flushed in order on
// reconnect; past the cap the oldest is evicted (ADR-0007 (a), paseo's
// 200-frame precedent).
const DefaultBufferCap = 200

// Bounds on relay state that would otherwise grow for as long as the
// process runs (every Admit adds a binding, every distinct
// (relay host, session, device) adds a route). All are overridable with the
// Options below; the defaults are far above normal use (a relay host has a
// handful of devices, and a daemon re-admits only after a restart or
// reconnect) and low enough that a misbehaving client cannot make the relay
// hold unbounded memory.
const (
	// DefaultRouteGrace is how long a route with NO attached stream on
	// either side is kept (with its reconnect buffers) before it is
	// garbage-collected. Within the window the reconnect-buffer behavior of
	// ADR-0007 (a) is unchanged; past it the peer must start a new session.
	DefaultRouteGrace = 5 * time.Minute

	// DefaultMaxBindingsPerWorkspace caps live admission bindings per
	// relay host; past it the least-recently-used binding is evicted (its
	// holder re-admits, which is cheap).
	DefaultMaxBindingsPerWorkspace = 64

	// DefaultBindingTTL expires a binding that has gone this long without
	// being presented on a stream (and has no stream attached). It is
	// refreshed on every stream open and every stream end, so
	// DataConn.Resume after a normal drop always finds its binding.
	DefaultBindingTTL = 24 * time.Hour

	// DefaultMaxDevicesPerHost caps the devices (distinct device ids with a
	// live device-side stream) one relay host may have attached at once; the
	// next one is refused with ResourceExhausted.
	DefaultMaxDevicesPerHost = 16

	// DefaultBufferBytes bounds the payload bytes one side of a route may
	// buffer for a disconnected peer, in addition to the frame-count cap.
	DefaultBufferBytes = 4 << 20

	// routesPerDevice scales DefaultMaxDevicesPerHost into the cap on routes
	// (sessions) one relay host may hold, whatever their side: a device that
	// re-handshakes leaves its old route to the grace period, so a host
	// legitimately has a few per device, but not unboundedly many.
	routesPerDevice = 8

	// maxIDLen bounds device and session ids in frame metadata; they key
	// relay state and are echoed on the control stream.
	maxIDLen = 128

	// controlOutboxCap bounds the control frames queued for a daemon that is
	// slow to read. Past it frames are dropped and a fresh Sync snapshot is
	// sent instead: the daemon must treat Sync as the authoritative list of
	// waiting routes and reconcile (a dropped DeviceDetach is recovered by the
	// route's absence from it).
	controlOutboxCap = 64

	// sweepInterval rate-limits the opportunistic sweep that rides on
	// Admit / attachRoute; Run also drives Sweep from a ticker so an idle
	// relay releases memory too.
	sweepInterval = 30 * time.Second
)

// MetadataKeyAdmission is the metadata key a client uses to present the
// admission ID returned by a successful Admit on subsequent streams, as
// its lowercase-hex encoding (metadata values must be printable ASCII). The
// ID is random, delivered only over the authenticated connection, and is
// not itself a credential — every frame is still checked against the
// relay host binding it names.
const MetadataKeyAdmission = "admission-id"

// gRPC statuses for the two authz failures. Callers see codes, not causes,
// so a rejected stream reveals only "not admitted" or "wrong relay host".
var (
	errNotAdmitted  = status.Error(codes.Unauthenticated, "relay: connection not admitted")
	errWrongBinding = status.Error(codes.PermissionDenied, "relay: relay host binding mismatch")
	errRevoked      = status.Error(codes.PermissionDenied, "relay: device revoked")
	errSuperseded   = status.Error(codes.Aborted, "superseded")
	errTooManyDev   = status.Error(codes.ResourceExhausted, "relay: too many attached devices for this relay host")
	errTooManyRoute = status.Error(codes.ResourceExhausted, "relay: too many sessions for this relay host")
)

type side uint8

const (
	sideDaemon side = iota + 1
	sideDevice
)

// Server implements relaypb.RelayServer.
type Server struct {
	relaypb.UnimplementedRelayServer

	verifier   *admission.Verifier
	bufferCap  int
	bufferByte int
	maxDevices int

	now              func() time.Time
	routeGrace       time.Duration
	maxBindingsPerWS int
	bindingTTL       time.Duration

	mu struct {
		sync.Mutex
		bindings  map[string]*binding     // admission ID -> binding
		routes    map[string]*route       // routeKey -> route
		controls  map[string]*controlConn // relay host id -> its one control stream
		lastSweep time.Time
	}
}

// binding is one admission ID's relay host binding plus the bookkeeping
// that bounds it. Guarded by Server.mu.
type binding struct {
	workspaceID string
	// lastUsed is refreshed when a stream presents the binding and again
	// when that stream ends, so the TTL measures idleness, not age.
	lastUsed time.Time
	// active counts streams currently holding the binding; a binding in
	// use is never expired, however long its streams live.
	active int
}

// Option configures a Server.
type Option func(*Server)

// WithRouteGrace sets how long a route with no attached stream on either
// side is kept before garbage collection (<=0 keeps DefaultRouteGrace).
func WithRouteGrace(d time.Duration) Option {
	return func(s *Server) {
		if d > 0 {
			s.routeGrace = d
		}
	}
}

// WithBindingLimits sets the per-host cap on admission bindings and
// the idle TTL after which an unused binding expires (<=0 keeps the
// default for that bound).
func WithBindingLimits(maxPerWorkspace int, ttl time.Duration) Option {
	return func(s *Server) {
		if maxPerWorkspace > 0 {
			s.maxBindingsPerWS = maxPerWorkspace
		}
		if ttl > 0 {
			s.bindingTTL = ttl
		}
	}
}

// WithMaxDevicesPerHost sets how many devices one relay host may have
// attached at once (<=0 keeps DefaultMaxDevicesPerHost).
func WithMaxDevicesPerHost(n int) Option {
	return func(s *Server) {
		if n > 0 {
			s.maxDevices = n
		}
	}
}

// WithBufferBytes sets the per-side reconnect buffer byte bound (<=0 keeps
// DefaultBufferBytes).
func WithBufferBytes(n int) Option {
	return func(s *Server) {
		if n > 0 {
			s.bufferByte = n
		}
	}
}

// WithClock injects the time source used for route grace and binding TTLs,
// so tests advance a fake clock instead of sleeping.
func WithClock(now func() time.Time) Option {
	return func(s *Server) { s.now = now }
}

// route is one E2EE session's forwarding state between the daemon side
// and the device side. Each side has a live stream pump while attached
// and a bounded queue that carries frames across a disconnect gap.
type route struct {
	workspaceID string
	sessionID   string
	deviceID    string

	mu struct {
		sync.Mutex
		// streams is the live attachment per side. A side's pump has an
		// independent, explicitly-cancellable context rather than
		// stream.Context() directly (see attachRoute's doc comment).
		streams map[side]*attachment
		// idleSince is when the last attached stream detached; zero while
		// any stream is attached. A route idle longer than the server's
		// route grace is garbage-collected.
		idleSince time.Time
	}
	daemonQ *frameQueue
	deviceQ *frameQueue
}

// attachment is one live OpenData stream on one side of a route.
type attachment struct {
	stream relaypb.Relay_OpenDataServer
	// sendMu serializes stream.Send between the queue pump and keepalive
	// replies (grpc streams allow one concurrent sender).
	sendMu sync.Mutex
	// pumpCtx/cancel drive this attachment's send pump.
	pumpCtx context.Context
	cancel  context.CancelFunc
	// abort carries the status the handler must end the stream with when the
	// relay closes it on purpose (Revoke); set BEFORE cancel is called.
	abort chan error
}

func (a *attachment) send(f *relaypb.Frame) error {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	return a.stream.Send(f)
}

// close ends the attachment with err (best effort, never blocks).
func (a *attachment) close(err error) {
	select {
	case a.abort <- err:
	default:
	}
	a.cancel()
}

// New creates a Server using the admission Verifier and a per-side
// reconnect buffer cap (<=0 means DefaultBufferCap).
func New(verifier *admission.Verifier, bufferCap int, opts ...Option) *Server {
	if bufferCap <= 0 {
		bufferCap = DefaultBufferCap
	}
	s := &Server{
		verifier:         verifier,
		bufferCap:        bufferCap,
		bufferByte:       DefaultBufferBytes,
		maxDevices:       DefaultMaxDevicesPerHost,
		now:              time.Now,
		routeGrace:       DefaultRouteGrace,
		maxBindingsPerWS: DefaultMaxBindingsPerWorkspace,
		bindingTTL:       DefaultBindingTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.mu.bindings = make(map[string]*binding)
	s.mu.routes = make(map[string]*route)
	s.mu.controls = make(map[string]*controlConn)
	return s
}

// Sweep garbage-collects expired state now: routes idle past the grace
// period (with their buffers) and bindings idle past the TTL. It is cheap,
// idempotent and safe to call at any time; Run drives it from a ticker and
// Admit/attachRoute also trigger it opportunistically, so state is
// bounded even on a relay that sees no new traffic.
func (s *Server) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(s.now())
}

// RunJanitor calls Sweep every sweepInterval until ctx is done.
func (s *Server) RunJanitor(ctx context.Context) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep()
		}
	}
}

func (s *Server) maybeSweepLocked(now time.Time) {
	if now.Sub(s.mu.lastSweep) >= sweepInterval {
		s.sweepLocked(now)
	}
}

func (s *Server) sweepLocked(now time.Time) {
	s.mu.lastSweep = now
	for key, r := range s.mu.routes {
		r.mu.Lock()
		gone := len(r.mu.streams) == 0 && !r.mu.idleSince.IsZero() && now.Sub(r.mu.idleSince) > s.routeGrace
		r.mu.Unlock()
		if gone {
			delete(s.mu.routes, key)
			// Tell the daemon the route is gone (idempotent: a device side
			// that ended earlier already announced it).
			s.announceLocked(r.workspaceID, detachFrame(r))
		}
	}
	for id, b := range s.mu.bindings {
		if s.bindingExpired(b, now) {
			delete(s.mu.bindings, id)
		}
	}
}

func (s *Server) bindingExpired(b *binding, now time.Time) bool {
	return b.active == 0 && now.Sub(b.lastUsed) > s.bindingTTL
}

// addBindingLocked records a new admission binding, first evicting the
// relay host's least-recently-used binding(s) while it is at the cap.
// Bindings with no attached stream are preferred victims; a binding in use
// is only evicted when every one of the relay host's bindings is in use
// (its stream keeps running — only a later Resume would need a fresh
// admission).
func (s *Server) addBindingLocked(id, workspaceID string, now time.Time) {
	for {
		count := 0
		var victimID string
		var victim *binding
		for bid, b := range s.mu.bindings {
			if b.workspaceID != workspaceID {
				continue
			}
			count++
			if victim == nil || olderVictim(b, victim) {
				victimID, victim = bid, b
			}
		}
		if count < s.maxBindingsPerWS {
			break
		}
		delete(s.mu.bindings, victimID)
	}
	s.mu.bindings[id] = &binding{workspaceID: workspaceID, lastUsed: now}
}

// olderVictim reports whether a is a better eviction victim than b: idle
// before in-use, then least recently used.
func olderVictim(a, b *binding) bool {
	if (a.active == 0) != (b.active == 0) {
		return a.active == 0
	}
	return a.lastUsed.Before(b.lastUsed)
}

// Register attaches the service to a grpc.Server (in-process tests now;
// the `smind relay` subcommand later).
func (s *Server) Register(gs *grpc.Server) {
	relaypb.RegisterRelayServer(gs, s)
}

// --- Admission ---

func (s *Server) AdmitChallenge(ctx context.Context, req *relaypb.AdmitChallengeRequest) (*relaypb.AdmitChallengeResponse, error) {
	return s.verifier.Challenge(req)
}

func (s *Server) Admit(ctx context.Context, req *relaypb.AdmitRequest) (*relaypb.AdmitResponse, error) {
	session, err := s.verifier.Admit(req)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	now := s.now()
	s.maybeSweepLocked(now)
	s.addBindingLocked(hex.EncodeToString(session.AdmissionID), session.WorkspaceID, now)
	s.mu.Unlock()
	return &relaypb.AdmitResponse{AdmissionId: session.AdmissionID}, nil
}

// binding resolves the relay host binding a stream presents via its
// admission-id metadata, or fails with errNotAdmitted. On success the
// binding is marked in use until the returned release func is called (once,
// when the stream ends): a binding with a live stream never expires, and
// its idle clock restarts when the stream ends so a reconnect/Resume within
// the TTL still finds it.
func (s *Server) binding(ctx context.Context) (workspaceID string, release func(), err error) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(MetadataKeyAdmission); len(v) == 1 {
			id = v[0]
		}
	}
	if id == "" {
		return "", nil, errNotAdmitted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	b, ok := s.mu.bindings[id]
	if !ok {
		return "", nil, errNotAdmitted
	}
	if s.bindingExpired(b, now) {
		delete(s.mu.bindings, id)
		return "", nil, errNotAdmitted
	}
	b.active++
	b.lastUsed = now
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			// b may have been evicted meanwhile; releasing a dropped
			// binding is a harmless no-op on an orphaned struct.
			b.active--
			b.lastUsed = s.now()
		})
	}
	return b.workspaceID, release, nil
}

// --- Control socket ---

// controlConn is the relay-side state of a relay host's one control stream.
// Every frame bound for the daemon goes through out and is written by
// OpenControl's single writer loop (a grpc stream allows one sender).
type controlConn struct {
	host string

	outMu  sync.Mutex
	out    []*relaypb.ControlFrame
	resync bool
	notify chan struct{}

	// abort ends the stream with a status (superseded by a newer control
	// stream). Buffered so the sender never blocks.
	abort chan error

	// revoked holds device ids the daemon revoked on this stream; it lives and
	// dies with the stream. Guarded by Server.mu.
	revoked map[string]struct{}
}

func newControlConn(host string) *controlConn {
	return &controlConn{
		host:    host,
		notify:  make(chan struct{}, 1),
		abort:   make(chan error, 1),
		revoked: make(map[string]struct{}),
	}
}

// enqueue queues cf for the daemon. It never blocks; when the outbox is full
// the frame is dropped and the writer sends a fresh Sync snapshot instead.
func (c *controlConn) enqueue(cf *relaypb.ControlFrame) {
	c.outMu.Lock()
	if len(c.out) >= controlOutboxCap {
		c.resync = true
	} else {
		c.out = append(c.out, cf)
	}
	c.outMu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// drain returns the queued frames and whether a resync is owed.
func (c *controlConn) drain() ([]*relaypb.ControlFrame, bool) {
	c.outMu.Lock()
	defer c.outMu.Unlock()
	out, resync := c.out, c.resync
	c.out, c.resync = nil, false
	return out, resync
}

func controlBody(host string, body func(*relaypb.ControlFrame)) *relaypb.ControlFrame {
	cf := &relaypb.ControlFrame{WorkspaceId: host}
	body(cf)
	return cf
}

func attachFrame(r *route) *relaypb.ControlFrame {
	return controlBody(r.workspaceID, func(cf *relaypb.ControlFrame) {
		cf.Body = &relaypb.ControlFrame_DeviceAttach_{DeviceAttach: &relaypb.ControlFrame_DeviceAttach{
			DeviceId: r.deviceID, SessionId: []byte(r.sessionID),
		}}
	})
}

func detachFrame(r *route) *relaypb.ControlFrame {
	return controlBody(r.workspaceID, func(cf *relaypb.ControlFrame) {
		cf.Body = &relaypb.ControlFrame_DeviceDetach_{DeviceDetach: &relaypb.ControlFrame_DeviceDetach{
			DeviceId: r.deviceID, SessionId: []byte(r.sessionID),
		}}
	})
}

// announceLocked queues cf on host's control stream, if it has one (a daemon
// that is not connected learns the same state from Sync when it is). Caller
// holds s.mu.
func (s *Server) announceLocked(host string, cf *relaypb.ControlFrame) {
	if c := s.mu.controls[host]; c != nil {
		c.enqueue(cf)
	}
}

// syncFrameLocked lists the host's routes that have a live device side and no
// daemon side. Caller holds s.mu.
func (s *Server) syncFrameLocked(host string) *relaypb.ControlFrame {
	sync := &relaypb.ControlFrame_Sync{}
	for _, r := range s.mu.routes {
		if r.workspaceID != host {
			continue
		}
		r.mu.Lock()
		waiting := r.mu.streams[sideDevice] != nil && r.mu.streams[sideDaemon] == nil
		r.mu.Unlock()
		if waiting {
			sync.Devices = append(sync.Devices, &relaypb.ControlFrame_DeviceRef{
				DeviceId: r.deviceID, SessionId: []byte(r.sessionID),
			})
		}
	}
	return controlBody(host, func(cf *relaypb.ControlFrame) {
		cf.Body = &relaypb.ControlFrame_Sync_{Sync: sync}
	})
}

// registerControl makes c the host's control stream, ending any predecessor
// with Aborted "superseded", and queues the initial Sync — both under s.mu,
// so a device that attaches concurrently is either in the Sync or announced
// after it, never missed.
func (s *Server) registerControl(c *controlConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.mu.controls[c.host]; old != nil {
		select {
		case old.abort <- errSuperseded:
		default:
		}
	}
	s.mu.controls[c.host] = c
	c.enqueue(s.syncFrameLocked(c.host))
}

// unregisterControl drops c if it is still the host's control stream (a
// superseded one is already replaced). Its revocations end with it.
func (s *Server) unregisterControl(c *controlConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mu.controls[c.host] == c {
		delete(s.mu.controls, c.host)
	}
}

// revoke closes every stream of the host's routes for deviceID and refuses
// new attaches for it until c's stream ends. The daemon stays authoritative:
// this only saves relay resources and stops a reconnect loop early.
func (s *Server) revoke(c *controlConn, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.revoked[deviceID] = struct{}{}
	for key, r := range s.mu.routes {
		if r.workspaceID != c.host || r.deviceID != deviceID {
			continue
		}
		delete(s.mu.routes, key)
		r.mu.Lock()
		for _, a := range r.mu.streams {
			a.close(errRevoked)
		}
		r.mu.Unlock()
	}
}

// OpenControl keeps the host's one control stream: it enforces the admission
// binding, answers Ping with Pong, sends Sync on accept and
// DeviceAttach/DeviceDetach as device routes appear and disappear, and
// executes Revoke. A newer control stream for the same host supersedes this
// one. It carries no application payload.
func (s *Server) OpenControl(stream relaypb.Relay_OpenControlServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	ws, release, err := s.binding(stream.Context())
	if err != nil {
		return err
	}
	defer release()
	if first.GetWorkspaceId() != ws {
		return errWrongBinding
	}
	// The first frame must be a Ping (the client's registration); anything
	// else is not a valid control registration.
	if first.GetPing() == nil {
		return status.Error(codes.Unimplemented, "relay: control stream must start with a ping")
	}

	c := newControlConn(ws)
	c.enqueue(pongFrame(ws, first.GetPing().GetSequence()))
	s.registerControl(c)
	defer s.unregisterControl(c)

	recvErr := make(chan error, 1)
	go func() {
		for {
			cf, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if cf.GetWorkspaceId() != ws {
				recvErr <- errWrongBinding
				return
			}
			switch body := cf.GetBody().(type) {
			case *relaypb.ControlFrame_Ping_:
				c.enqueue(pongFrame(ws, body.Ping.GetSequence()))
			case *relaypb.ControlFrame_Revoke_:
				s.revoke(c, body.Revoke.GetDeviceId())
			default:
				// Pong and relay→daemon bodies from a daemon, and bodies this
				// revision does not know: ignored, so newer peers can add
				// messages without killing the stream.
			}
		}
	}()

	for {
		select {
		case <-c.notify:
			frames, resync := c.drain()
			if resync {
				s.mu.Lock()
				frames = append(frames, s.syncFrameLocked(ws))
				s.mu.Unlock()
			}
			for _, f := range frames {
				if err := stream.Send(f); err != nil {
					return normalizeStreamErr(err)
				}
			}
		case err := <-c.abort:
			return err
		case err := <-recvErr:
			return normalizeStreamErr(err)
		case <-stream.Context().Done():
			return normalizeStreamErr(stream.Context().Err())
		}
	}
}

func pongFrame(host string, seq uint64) *relaypb.ControlFrame {
	return &relaypb.ControlFrame{
		WorkspaceId: host,
		Body:        &relaypb.ControlFrame_Pong_{Pong: &relaypb.ControlFrame_Pong{Sequence: seq}},
	}
}

// --- Data socket ---

// OpenData is the forwarding loop for one side (daemon or device) of one
// E2EE session. Inbound frames are validated against the route's binding
// and pushed to the peer side's queue; the peer's pump (if attached)
// drains it onto its stream. Payload bytes are opaque throughout. A frame
// with Keepalive set is answered on the same stream and goes no further.
func (s *Server) OpenData(stream relaypb.Relay_OpenDataServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if err := s.checkFrame(first); err != nil {
		return err
	}
	ws, release, err := s.binding(stream.Context())
	if err != nil {
		return err
	}
	defer release()
	if first.GetWorkspaceId() != ws {
		return errWrongBinding
	}

	sd := dataSide(first.GetDirection())
	r, att, err := s.attachRoute(first, sd, stream)
	if err != nil {
		return err
	}
	defer s.detach(sd, r, att)

	if first.GetKeepalive() {
		if err := att.send(keepaliveEcho(r, sd, first)); err != nil {
			return normalizeStreamErr(err)
		}
	} else if err := s.forward(r, sd, first); err != nil {
		return err
	}

	// finish maps a loop exit to the stream's status: a deliberate close by
	// the relay (Revoke) wins over the cancellation it caused.
	finish := func(err error) error {
		select {
		case a := <-att.abort:
			return a
		default:
			return normalizeStreamErr(err)
		}
	}

	recvErr := make(chan error, 1)
	go func() {
		for {
			f, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if f.GetKeepalive() {
				// Liveness probes need only the stream's own binding: they
				// are consumed here, never forwarded or buffered.
				if f.GetWorkspaceId() != "" && f.GetWorkspaceId() != r.workspaceID {
					recvErr <- errWrongBinding
					return
				}
				if err := att.send(keepaliveEcho(r, sd, f)); err != nil {
					recvErr <- err
					return
				}
				continue
			}
			if err := s.checkFrame(f); err != nil {
				recvErr <- err
				return
			}
			if f.GetWorkspaceId() != r.workspaceID ||
				string(f.GetSessionId()) != r.sessionID ||
				f.GetDeviceId() != r.deviceID {
				recvErr <- errWrongBinding
				return
			}
			if dataSide(f.GetDirection()) != sd {
				recvErr <- status.Error(codes.InvalidArgument, "relay: direction contradicts stream side")
				return
			}
			if err := s.forward(r, sd, f); err != nil {
				recvErr <- err
				return
			}
		}
	}()

	// Send pump: drain this side's queue onto this stream. A stalled peer
	// never blocks the relay — its queue absorbs and evicts. It pops using
	// the attachment's pumpCtx, not stream.Context() -- see attachRoute's doc
	// comment for why those must be different contexts.
	sendErr := make(chan error, 1)
	go func() {
		for {
			f, err := r.queueFor(sd).pop(att.pumpCtx)
			if err != nil {
				sendErr <- err
				return
			}
			// Re-check right before handing the frame to the network:
			// pop() already re-checks pumpCtx itself, but a cancellation
			// landing in the gap between pop() returning and this line
			// is still possible, and grpc's Send can return success into
			// a connection that's already dead (the actual failure can
			// surface later, or never, since sends are buffered
			// asynchronously beneath the call) -- silently handing a
			// frame to a stream that's already been superseded is
			// exactly how it gets lost. Either way out of this pump
			// re-queues the frame instead of dropping it.
			if att.pumpCtx.Err() != nil {
				r.queueFor(sd).pushFront(f)
				sendErr <- att.pumpCtx.Err()
				return
			}
			if err := att.send(f); err != nil {
				r.queueFor(sd).pushFront(f)
				sendErr <- err
				return
			}
		}
	}()

	select {
	case err := <-recvErr:
		return finish(err)
	case err := <-sendErr:
		return finish(err)
	case <-stream.Context().Done():
		return finish(stream.Context().Err())
	}
}

// keepaliveEcho is the answer to a keepalive probe received from side sd: a
// keepalive frame travelling the other way, with the probe's sequence so the
// sender can match it.
func keepaliveEcho(r *route, sd side, probe *relaypb.Frame) *relaypb.Frame {
	// A device is answered "from the daemon" and vice versa, like the real
	// traffic arriving on that stream.
	dir := relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON
	if sd == sideDevice {
		dir = relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE
	}
	return &relaypb.Frame{
		WorkspaceId: r.workspaceID,
		SessionId:   []byte(r.sessionID),
		DeviceId:    r.deviceID,
		Direction:   dir,
		Sequence:    probe.GetSequence(),
		Keepalive:   true,
	}
}

// dataSide maps a frame's direction to the side that sends it.
func dataSide(d relaypb.Direction) side {
	if d == relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE {
		return sideDaemon
	}
	return sideDevice
}

func other(sd side) side {
	if sd == sideDaemon {
		return sideDevice
	}
	return sideDaemon
}

func (s *Server) checkFrame(f *relaypb.Frame) error {
	if f.GetWorkspaceId() == "" || len(f.GetSessionId()) == 0 || f.GetDeviceId() == "" {
		return status.Error(codes.InvalidArgument, "relay: frame missing routing metadata")
	}
	if f.GetDirection() == relaypb.Direction_DIRECTION_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "relay: frame missing direction")
	}
	if len(f.GetSessionId()) > maxIDLen || len(f.GetDeviceId()) > maxIDLen {
		return status.Error(codes.InvalidArgument, "relay: session or device id too long")
	}
	return nil
}

// attachRoute finds or creates the route for the frame's key and attaches
// this stream as its side (replacing any dead predecessor — reconnect),
// returning the context this stream's own send pump must use.
//
// A superseded stream's send pump must stop pulling from the side's queue
// the instant it's replaced, not whenever it eventually notices its own
// transport died. Reusing stream.Context() for the pump's pop() call was
// the original approach, but a stream's context is only cancelled once its
// underlying transport is confirmed broken -- there is a real, observed
// window (a reconnect racing a not-yet-detected dead connection) where the
// OLD stream's pump can still win frameQueue.pop() against the NEW
// stream's pump. Because pop() removes the frame from the queue
// unconditionally, that frame is then lost the moment the old pump's
// stream.Send() fails on the dead connection, instead of ever reaching the
// new one — this is exactly the daemon-transport-drop reconnect scenario
// internal/relay/bridge's own integration test exercises, and was
// reproduced directly by adding diagnostic logging around a real failure.
// Deriving each pump's context from the route itself (cancelled here, the
// moment a new stream attaches for the same side) closes that window
// deterministically instead of relying on transport-failure detection
// timing.
func (s *Server) attachRoute(first *relaypb.Frame, sd side, stream relaypb.Relay_OpenDataServer) (*route, *attachment, error) {
	ws, deviceID := first.GetWorkspaceId(), first.GetDeviceId()
	key := routeKey(ws, string(first.GetSessionId()), deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweepLocked(s.now())
	if c := s.mu.controls[ws]; c != nil {
		if _, revoked := c.revoked[deviceID]; revoked {
			return nil, nil, errRevoked
		}
	}
	r, ok := s.mu.routes[key]
	if sd == sideDevice && s.deviceLimitReachedLocked(ws, deviceID) {
		return nil, nil, errTooManyDev
	}
	if !ok {
		if s.routeCountLocked(ws) >= s.maxDevices*routesPerDevice {
			return nil, nil, errTooManyRoute
		}
		r = &route{
			workspaceID: ws,
			sessionID:   string(first.GetSessionId()),
			deviceID:    deviceID,
		}
		r.daemonQ = newByteBoundedFrameQueue(s.bufferCap, s.bufferByte)
		r.deviceQ = newByteBoundedFrameQueue(s.bufferCap, s.bufferByte)
		r.mu.streams = make(map[side]*attachment)
		s.mu.routes[key] = r
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.mu.streams[sd]; old != nil {
		old.cancel()
	}
	pumpCtx, cancel := context.WithCancel(stream.Context())
	att := &attachment{stream: stream, pumpCtx: pumpCtx, cancel: cancel, abort: make(chan error, 1)}
	r.mu.streams[sd] = att
	r.mu.idleSince = time.Time{}
	if sd == sideDevice && r.mu.streams[sideDaemon] == nil {
		// A device is waiting for a daemon side: tell the daemon.
		s.announceLocked(ws, attachFrame(r))
	}
	return r, att, nil
}

// routeCountLocked is the number of routes the host holds. Caller holds s.mu.
func (s *Server) routeCountLocked(ws string) int {
	n := 0
	for _, r := range s.mu.routes {
		if r.workspaceID == ws {
			n++
		}
	}
	return n
}

// deviceLimitReachedLocked reports whether attaching a device-side stream for
// deviceID would exceed the host's device limit: the distinct device ids with
// a live device side, not counting deviceID itself (a reconnect or a fresh
// session of an attached device is not a new device). Caller holds s.mu.
func (s *Server) deviceLimitReachedLocked(ws, deviceID string) bool {
	live := make(map[string]struct{})
	for _, r := range s.mu.routes {
		if r.workspaceID != ws || r.deviceID == deviceID {
			continue
		}
		r.mu.Lock()
		if r.mu.streams[sideDevice] != nil {
			live[r.deviceID] = struct{}{}
		}
		r.mu.Unlock()
	}
	return len(live) >= s.maxDevices
}

// detach removes this attachment from the route if a reconnect has not
// already replaced it. Queues persist in the route, which is what carries
// frames across the disconnect gap — until no stream has been attached to
// either side for routeGrace, when Sweep collects the route. It tells the
// daemon what changed: a device side that ends with no daemon side is a
// DeviceDetach. A daemon side that ends is NOT re-announced: the daemon
// closed it, or lost it together with its control stream (which resyncs), and
// re-announcing would make a daemon that deliberately rejects a device
// reconnect to it in a loop.
func (s *Server) detach(sd side, r *route, att *attachment) {
	att.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mu.streams[sd] != att {
		return
	}
	delete(r.mu.streams, sd)
	if len(r.mu.streams) == 0 {
		// Start the grace clock: the route (and its reconnect buffers)
		// is kept for routeGrace waiting for a peer, then collected.
		r.mu.idleSince = s.now()
	}
	if s.mu.routes[routeKey(r.workspaceID, r.sessionID, r.deviceID)] != r {
		return // revoked or collected: nothing to announce
	}
	if sd == sideDevice && r.mu.streams[sideDaemon] == nil {
		s.announceLocked(r.workspaceID, detachFrame(r))
	}
}

// forward pushes one frame to the opposite side's queue. The peer's pump
// drains it onto its live stream; without one it buffers (reconnect
// grace) — bounded, oldest-evicted, in order.
func (s *Server) forward(r *route, from side, f *relaypb.Frame) error {
	r.queueFor(other(from)).push(f)
	return nil
}

func (r *route) queueFor(sd side) *frameQueue {
	if sd == sideDaemon {
		return r.daemonQ
	}
	return r.deviceQ
}

// routeKey is length-prefixed so ids containing any byte cannot collide
// ("a\x00b","c" vs "a","b\x00c").
func routeKey(workspaceID, sessionID, deviceID string) string {
	return fmt.Sprintf("%d:%s|%d:%s|%d:%s", len(workspaceID), workspaceID, len(sessionID), sessionID, len(deviceID), deviceID)
}

// normalizeStreamErr maps clean client closes and context cancellation to
// nil so they surface as a normal RPC end, not a relay error.
func normalizeStreamErr(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Streams returns the number of currently attached data streams (both
// sides, all routes) — a test introspection hook, not part of the wire
// contract.
func (s *Server) Streams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.mu.routes {
		r.mu.Lock()
		n += len(r.mu.streams)
		r.mu.Unlock()
	}
	return n
}

// BufferedFrames returns the number of frames currently held in
// reconnect buffers for the given route — test introspection only.
func (s *Server) BufferedFrames(workspaceID, sessionID, deviceID string) int {
	s.mu.Lock()
	r, ok := s.mu.routes[routeKey(workspaceID, sessionID, deviceID)]
	s.mu.Unlock()
	if !ok {
		return 0
	}
	return r.daemonQ.len() + r.deviceQ.len()
}
