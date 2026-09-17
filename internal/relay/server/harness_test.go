package server

// harness_test.go — in-process test doubles for the relay service: a
// bufconn gRPC server wired to Server, plus admitted daemon/device client
// wrappers that own their streams. The real `smind relay` subcommand step
// will reuse this shape over a real listener instead of bufconn.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

const testWorkspace = "ws-test"

// harness owns one in-memory relay instance.
type harness struct {
	t        *testing.T
	listener *bufconn.Listener
	gs       *grpc.Server
	srv      *Server
	conn     *grpc.ClientConn
	client   relaypb.RelayClient
	secret   []byte
}

func newHarness(t *testing.T, bufferCap int) *harness {
	t.Helper()
	ws, secret, err := admission.NewWorkspace(testWorkspace)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	verifier := admission.NewVerifier()
	verifier.Register(ws)

	srv := New(verifier, bufferCap)
	listener := bufconn.Listen(64 * 1024)
	gs := grpc.NewServer()
	srv.Register(gs)
	go gs.Serve(listener)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	h := &harness{
		t:        t,
		listener: listener,
		gs:       gs,
		srv:      srv,
		conn:     conn,
		client:   relaypb.NewRelayClient(conn),
		secret:   secret,
	}
	t.Cleanup(func() {
		conn.Close()
		gs.Stop()
		listener.Close()
	})
	return h
}

// admit performs the full daemon-side admission exchange on a fresh
// context (its own connection identity) and returns the admission ID.
func (h *harness) admit(ctx context.Context, workspaceID, daemonKeyID string) string {
	h.t.Helper()
	clientNonce := make([]byte, admission.NonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		h.t.Fatalf("client nonce: %v", err)
	}
	chal, err := h.client.AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
	})
	if err != nil {
		h.t.Fatalf("AdmitChallenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeHMAC(admission.HashSecret(h.secret), req)
	resp, err := h.client.Admit(ctx, req)
	if err != nil {
		h.t.Fatalf("Admit: %v", err)
	}
	return hex.EncodeToString(resp.GetAdmissionId())
}

// admittedContext returns a context carrying the admission metadata.
func admittedContext(ctx context.Context, admissionID string) context.Context {
	return metadataAppend(ctx, admissionID)
}

// endpoint is one admitted side (daemon or device) with an open OpenData
// stream.
type endpoint struct {
	t       *testing.T
	stream  relaypb.Relay_OpenDataClient
	route   frameRoute
	sendErr chan error
	recvCh  chan *relaypb.Frame
}

// frameRoute identifies the E2EE session the endpoint's frames belong to.
type frameRoute struct {
	workspaceID string
	sessionID   string
	deviceID    string
	direction   relaypb.Direction
}

func (h *harness) openData(ctx context.Context, r frameRoute) *endpoint {
	h.t.Helper()
	stream, err := h.client.OpenData(ctx)
	if err != nil {
		h.t.Fatalf("OpenData: %v", err)
	}
	e := &endpoint{
		t:       h.t,
		stream:  stream,
		route:   r,
		sendErr: make(chan error, 1),
		recvCh:  make(chan *relaypb.Frame, 64),
	}
	// Every frame on this stream must carry the endpoint's routing
	// metadata; Send only after the stream's first frame passed admission.
	go func() {
		for {
			f, err := stream.Recv()
			if err != nil {
				close(e.recvCh)
				return
			}
			e.recvCh <- f
		}
	}()
	return e
}

// sendFrame writes one frame with the endpoint's routing metadata.
func (e *endpoint) sendFrame(f *relaypb.Frame) {
	e.t.Helper()
	if err := e.stream.Send(f); err != nil {
		e.t.Fatalf("Send: %v", err)
	}
}

func (e *endpoint) frame(sequence uint64, payload []byte) *relaypb.Frame {
	return &relaypb.Frame{
		WorkspaceId: e.route.workspaceID,
		SessionId:   []byte(e.route.sessionID),
		DeviceId:    e.route.deviceID,
		Direction:   e.route.direction,
		Sequence:    sequence,
		Payload:     payload,
	}
}

// expectRecv waits for one frame from the stream with a timeout.
func (e *endpoint) expectRecv(timeout time.Duration) *relaypb.Frame {
	e.t.Helper()
	select {
	case f, ok := <-e.recvCh:
		if !ok {
			e.t.Fatalf("stream closed while waiting for frame")
		}
		return f
	case <-time.After(timeout):
		e.t.Fatalf("timed out waiting for frame after %v", timeout)
		return nil
	}
}

// expectQuiet asserts no frame arrives for d.
func (e *endpoint) expectQuiet(d time.Duration) {
	e.t.Helper()
	select {
	case f, ok := <-e.recvCh:
		if !ok {
			return // closed is fine — nothing leaked before close
		}
		e.t.Fatalf("unexpected frame while expecting quiet: %+v", f)
	case <-time.After(d):
	}
}

func metadataAppend(ctx context.Context, admissionID string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, MetadataKeyAdmission, admissionID)
}

// randBytes is a tiny helper for payload generation.
func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}
