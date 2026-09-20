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

	mu struct {
		sync.Mutex
		bindings map[string]string // admission ID -> workspace ID
		routes   map[string]*route // routeKey -> route
	}
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
func New(verifier *admission.Verifier, bufferCap int) *Server {
	if bufferCap <= 0 {
		bufferCap = DefaultBufferCap
	}
	s := &Server{verifier: verifier, bufferCap: bufferCap}
	s.mu.bindings = make(map[string]string)
	s.mu.routes = make(map[string]*route)
	return s
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
	s.mu.bindings[hex.EncodeToString(session.AdmissionID)] = session.WorkspaceID
	s.mu.Unlock()
	return &relaypb.AdmitResponse{AdmissionId: session.AdmissionID}, nil
}

// binding resolves the workspace binding a stream presents via its
// admission-id metadata, or fails with errNotAdmitted.
func (s *Server) binding(ctx context.Context) (string, error) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(MetadataKeyAdmission); len(v) == 1 {
			id = v[0]
		}
	}
	if id == "" {
		return "", errNotAdmitted
	}
	s.mu.Lock()
	ws, ok := s.mu.bindings[id]
	s.mu.Unlock()
	if !ok {
		return "", errNotAdmitted
	}
	return ws, nil
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
	ws, err := s.binding(stream.Context())
	if err != nil {
		return err
	}
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
	ws, err := s.binding(stream.Context())
	if err != nil {
		return err
	}
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
	return r, pumpCtx
}

// detach removes this stream from the route if a reconnect has not
// already replaced it. Queues persist in the route, which is what carries
// frames across the disconnect gap.
func (s *Server) detach(sd side, r *route, stream relaypb.Relay_OpenDataServer) {
	r.mu.Lock()
	if r.mu.streams[sd] == stream {
		delete(r.mu.streams, sd)
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
