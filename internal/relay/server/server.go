// Package server implements the gRPC relay service defined in
// internal/relay/relaypb: admission delegated to internal/relay/admission,
// plus dumb-pipe forwarding of opaque Frame envelopes between a daemon
// connection and a paired device connection, with a bounded reconnect
// buffer (ADR-0007 (a)). The relay never sees an E2EE session key or
// plaintext: Frame payload bytes are copied through, never inspected.
//
// Fanout (ADR-0007 (b)): a workspace may have multiple paired devices,
// each with its own OpenData stream and its own E2EE session (and hence
// its own route, keyed (workspace, session, device), with one bounded
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
package server

import (
	"context"
	"encoding/hex"
	"errors"
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
// (workspace, session, device) adds a route). All are overridable with the
// Options below; the defaults are far above normal use (a workspace has a
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
	// workspace; past it the least-recently-used binding is evicted (its
	// holder re-admits, which is cheap).
	DefaultMaxBindingsPerWorkspace = 64

	// DefaultBindingTTL expires a binding that has gone this long without
	// being presented on a stream (and has no stream attached). It is
	// refreshed on every stream open and every stream end, so
	// DataConn.Resume after a normal drop always finds its binding.
	DefaultBindingTTL = 24 * time.Hour

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
// workspace binding it names.
const MetadataKeyAdmission = "admission-id"

// gRPC statuses for the two authz failures. Callers see codes, not causes,
// so a rejected stream reveals only "not admitted" or "wrong workspace".
var (
	errNotAdmitted  = status.Error(codes.Unauthenticated, "relay: connection not admitted")
	errWrongBinding = status.Error(codes.PermissionDenied, "relay: workspace binding mismatch")
)

type side uint8

const (
	sideDaemon side = iota + 1
	sideDevice
)

// Server implements relaypb.RelayServer.
type Server struct {
	relaypb.UnimplementedRelayServer

	verifier  *admission.Verifier
	bufferCap int

	now              func() time.Time
	routeGrace       time.Duration
	maxBindingsPerWS int
	bindingTTL       time.Duration

	mu struct {
		sync.Mutex
		bindings  map[string]*binding // admission ID -> binding
		routes    map[string]*route   // routeKey -> route
		lastSweep time.Time
	}
}

// binding is one admission ID's workspace binding plus the bookkeeping
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

// WithBindingLimits sets the per-workspace cap on admission bindings and
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
		streams map[side]relaypb.Relay_OpenDataServer
		// idleSince is when the last attached stream detached; zero while
		// any stream is attached. A route idle longer than the server's
		// route grace is garbage-collected.
		idleSince time.Time
		// pumpCancel stops the CURRENT send pump for a side (see
		// attachRoute's doc comment for why a side's pump needs an
		// independent, explicitly-cancellable context rather than reusing
		// stream.Context() directly).
		pumpCancel map[side]context.CancelFunc
	}
	daemonQ *frameQueue
	deviceQ *frameQueue
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
// workspace's least-recently-used binding(s) while it is at the cap.
// Bindings with no attached stream are preferred victims; a binding in use
// is only evicted when every one of the workspace's bindings is in use
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

// binding resolves the workspace binding a stream presents via its
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

// OpenControl keeps a per-daemon stream that enforces the admission
// binding and echoes/answers liveness. It carries no application payload;
// per-device lifecycle announcements are interpreted in a later step.
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

	recvErr := make(chan error, 1)
	handle := func(cf *relaypb.ControlFrame) error {
		if cf.GetPing() == nil {
			// Device attach/detach announcements are interpreted when
			// fanout lands; a dumb pipe forwards nothing it cannot route
			// yet.
			return status.Error(codes.Unimplemented, "relay: control frame not handled in this step")
		}
		return stream.Send(&relaypb.ControlFrame{
			WorkspaceId: ws,
			Body:        &relaypb.ControlFrame_Pong_{Pong: &relaypb.ControlFrame_Pong{Sequence: cf.GetPing().GetSequence()}},
		})
	}
	if err := handle(first); err != nil {
		return normalizeStreamErr(err)
	}
	go func() {
		for {
			cf, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if err := handle(cf); err != nil {
				recvErr <- err
				return
			}
		}
	}()

	select {
	case err := <-recvErr:
		return normalizeStreamErr(err)
	case <-stream.Context().Done():
		return normalizeStreamErr(stream.Context().Err())
	}
}

// --- Data socket ---

// OpenData is the forwarding loop for one side (daemon or device) of one
// E2EE session. Inbound frames are validated against the route's binding
// and pushed to the peer side's queue; the peer's pump (if attached)
// drains it onto its stream. Payload bytes are opaque throughout.
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
	r, pumpCtx := s.attachRoute(first, sd, stream)
	defer s.detach(sd, r, stream)

	if err := s.forward(r, sd, first); err != nil {
		return err
	}

	recvErr := make(chan error, 1)
	go func() {
		for {
			f, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
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
	// pumpCtx, not stream.Context() -- see attachRoute's doc comment for
	// why those must be different contexts.
	sendErr := make(chan error, 1)
	go func() {
		for {
			f, err := r.queueFor(sd).pop(pumpCtx)
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
			if pumpCtx.Err() != nil {
				r.queueFor(sd).pushFront(f)
				sendErr <- pumpCtx.Err()
				return
			}
			if err := stream.Send(f); err != nil {
				r.queueFor(sd).pushFront(f)
				sendErr <- err
				return
			}
		}
	}()

	select {
	case err := <-recvErr:
		return normalizeStreamErr(err)
	case err := <-sendErr:
		return normalizeStreamErr(err)
	case <-stream.Context().Done():
		return normalizeStreamErr(stream.Context().Err())
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
func (s *Server) attachRoute(first *relaypb.Frame, sd side, stream relaypb.Relay_OpenDataServer) (*route, context.Context) {
	key := routeKey(first.GetWorkspaceId(), string(first.GetSessionId()), first.GetDeviceId())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweepLocked(s.now())
	r, ok := s.mu.routes[key]
	if !ok {
		r = &route{
			workspaceID: first.GetWorkspaceId(),
			sessionID:   string(first.GetSessionId()),
			deviceID:    first.GetDeviceId(),
		}
		r.daemonQ = newFrameQueue(s.bufferCap)
		r.deviceQ = newFrameQueue(s.bufferCap)
		r.mu.streams = make(map[side]relaypb.Relay_OpenDataServer)
		r.mu.pumpCancel = make(map[side]context.CancelFunc)
		s.mu.routes[key] = r
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel, ok := r.mu.pumpCancel[sd]; ok {
		cancel()
	}
	pumpCtx, cancel := context.WithCancel(stream.Context())
	r.mu.pumpCancel[sd] = cancel
	r.mu.streams[sd] = stream
	r.mu.idleSince = time.Time{}
	return r, pumpCtx
}

// detach removes this stream from the route if a reconnect has not
// already replaced it. Queues persist in the route, which is what carries
// frames across the disconnect gap — until no stream has been attached to
// either side for routeGrace, when Sweep collects the route.
func (s *Server) detach(sd side, r *route, stream relaypb.Relay_OpenDataServer) {
	r.mu.Lock()
	if r.mu.streams[sd] == stream {
		delete(r.mu.streams, sd)
		if len(r.mu.streams) == 0 {
			// Start the grace clock: the route (and its reconnect buffers)
			// is kept for routeGrace waiting for a peer, then collected.
			r.mu.idleSince = s.now()
		}
	}
	r.mu.Unlock()
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

func routeKey(workspaceID, sessionID, deviceID string) string {
	return workspaceID + "\x00" + sessionID + "\x00" + deviceID
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
