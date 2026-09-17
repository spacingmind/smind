// Package server implements the gRPC relay service defined in
// internal/relay/relaypb: admission delegated to internal/relay/admission,
// plus dumb-pipe forwarding of opaque Frame envelopes between a daemon
// connection and a paired device connection, with a bounded reconnect
// buffer (ADR-0007 (a)). The relay never sees an E2EE session key or
// plaintext: Frame payload bytes are copied through, never inspected.
//
// Scope for this step: one daemon connection and one paired device per
// workspace/session. Routing state is keyed by (workspace, session,
// device) with one bounded queue per side, so multi-device fanout later
// means "more routes share the daemon side", not a redesign.
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
	r := s.attachRoute(first, sd, stream)
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
	// never blocks the relay — its queue absorbs and evicts.
	sendErr := make(chan error, 1)
	go func() {
		for {
			f, err := r.queueFor(sd).pop(stream.Context())
			if err != nil {
				sendErr <- err
				return
			}
			if err := stream.Send(f); err != nil {
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
// this stream as its side (replacing any dead predecessor — reconnect).
func (s *Server) attachRoute(first *relaypb.Frame, sd side, stream relaypb.Relay_OpenDataServer) *route {
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
		s.mu.routes[key] = r
	}
	r.mu.Lock()
	r.mu.streams[sd] = stream
	r.mu.Unlock()
	return r
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
