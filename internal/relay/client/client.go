// Package client is the daemon-side (and, in tests, mobile-side) gRPC
// client for a smind relay: TLS with pairing-time fingerprint pinning
// (ADR-0011), the admission challenge-response, and data-stream wrappers
// that layer the E2EE session (internal/relay/e2ee) over OpenData so
// callers send/receive plaintext while the relay only ever sees
// ciphertext envelopes.
package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/spacingmind/smind/internal/relay/admission"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// The metadata key the relay's server package reads the admission ID from.
// Mirrored here (not imported) to keep the client package decoupled from
// the server implementation.
const metadataKeyAdmission = "admission-id"

// Dial connects to the relay at target (host:port), pinning the TLS
// certificate fingerprint (hex SHA-256 of the DER cert, as printed by
// `smind relay` at startup and carried in pairing.Offer.RelayFingerprint)
// instead of trusting any CA. An empty fingerprint is refused: an unpinned
// connection to a self-signed relay is exactly the MITM ADR-0011 exists
// to prevent.
func Dial(target, fingerprint string) (*grpc.ClientConn, error) {
	fp, err := hex.DecodeString(fingerprint)
	if err != nil || len(fp) != sha256.Size {
		return nil, fmt.Errorf("relay client: bad fingerprint %q", fingerprint)
	}
	tlsCfg := &tls.Config{
		// Chain verification is replaced by the fingerprint pin below.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			for _, raw := range rawCerts {
				sum := sha256.Sum256(raw)
				if string(sum[:]) == string(fp) {
					return nil
				}
			}
			return fmt.Errorf("relay client: certificate fingerprint does not match pin")
		},
		MinVersion: tls.VersionTLS13,
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
}

// Admit completes the admission exchange (ADR-0011) over c: obtains a
// server nonce and proves possession of the workspace secret via
// admission.ComputeHMAC's canonical transcript. Returns the admission ID
// (hex) to present on subsequent streams.
func Admit(ctx context.Context, c relaypb.RelayClient, workspaceID, daemonKeyID string, secret []byte) (string, error) {
	clientNonce := make([]byte, admission.NonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return "", fmt.Errorf("relay client: nonce: %w", err)
	}
	chal, err := c.AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
	})
	if err != nil {
		return "", fmt.Errorf("relay client: challenge: %w", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeHMAC(admission.HashSecret(secret), req)
	resp, err := c.Admit(ctx, req)
	if err != nil {
		return "", fmt.Errorf("relay client: admit: %w", err)
	}
	return hex.EncodeToString(resp.GetAdmissionId()), nil
}

// OpenControl opens the control stream under the given admission. It
// sends one registration frame and returns the raw stream for liveness
// use; device lifecycle announcements are a later concern.
func OpenControl(ctx context.Context, c relaypb.RelayClient, admissionID, workspaceID string) (relaypb.Relay_OpenControlClient, error) {
	sctx := metadata.AppendToOutgoingContext(ctx, metadataKeyAdmission, admissionID)
	stream, err := c.OpenControl(sctx)
	if err != nil {
		return nil, fmt.Errorf("relay client: open control: %w", err)
	}
	if err := stream.Send(&relaypb.ControlFrame{WorkspaceId: workspaceID}); err != nil {
		return nil, fmt.Errorf("relay client: control register: %w", err)
	}
	return stream, nil
}

// DataConn is one end of an E2EE session carried over an OpenData stream.
// It is a thin wrapper: plaintext in/out (Send/Receive from the underlying
// e2ee.Channel), with the relay seeing only framed ciphertext.
type DataConn struct {
	channel *e2ee.Channel
	frames  *frameConn
}

// Handshake runs the E2EE handshake over the relay (see e2ee.Channel).
func (d *DataConn) Handshake(ctx context.Context) error { return d.channel.Handshake(ctx) }

// Send encrypts and forwards one application message.
func (d *DataConn) Send(msg []byte) error { return d.channel.Send(msg) }

// Receive blocks for and decrypts one application message.
func (d *DataConn) Receive() ([]byte, error) { return d.channel.Receive() }

// Close tears down the stream and session.
func (d *DataConn) Close() error { return d.channel.Close() }

// OpenData opens a data stream for (workspace, session, device) under the
// admission, wraps it as an io.ReadWriteCloser of framed ciphertext, and
// layers an e2ee.Channel on top using the given keypair and role. The
// returned DataConn must complete Handshake before Send/Receive.
func OpenData(ctx context.Context, c relaypb.RelayClient, admissionID, workspaceID, sessionID, deviceID string, kp *e2ee.KeyPair, role e2ee.Role) (*DataConn, error) {
	sctx := metadata.AppendToOutgoingContext(ctx, metadataKeyAdmission, admissionID)
	stream, err := c.OpenData(sctx)
	if err != nil {
		return nil, fmt.Errorf("relay client: open data: %w", err)
	}
	fc := &frameConn{
		stream: stream,
		route: route{
			workspaceID: workspaceID,
			sessionID:   sessionID,
			deviceID:    deviceID,
		},
		direction: directionFor(role),
	}
	// Registration: the relay derives a stream's route from its first
	// frame, so an empty-payload registration frame is sent immediately.
	fc.register()
	channel, err := e2ee.NewChannel(fc, kp, role)
	if err != nil {
		fc.Close()
		return nil, fmt.Errorf("relay client: e2ee channel: %w", err)
	}
	return &DataConn{channel: channel, frames: fc}, nil
}

func directionFor(role e2ee.Role) relaypb.Direction {
	if role == e2ee.RoleDaemon {
		return relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE
	}
	return relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON
}

// frameConn adapts one OpenData stream to io.ReadWriteCloser: writes
// become outbound Frames (payload = the e2ee Channel's wire bytes, opaque
// here too), reads return inbound Frame payloads. Frame.sequence is this
// adapter's own monotonic counter, used by the relay for buffer ordering;
// E2EE replay protection stays inside the e2ee session, which keeps its
// own counters.
type frameConn struct {
	stream    relaypb.Relay_OpenDataClient
	route     route
	direction relaypb.Direction

	sendMu  sync.Mutex
	recvMu  sync.Mutex
	seq     uint64
	pending []byte
	closed  bool
}

type route struct {
	workspaceID string
	sessionID   string
	deviceID    string
}

// register sends the routing frame the relay requires as a stream's first
// frame (payload empty — the e2ee handshake follows as ordinary frames).
func (f *frameConn) register() {
	_, _ = f.Write(nil)
}

func (f *frameConn) Write(p []byte) (int, error) {
	f.sendMu.Lock()
	defer f.sendMu.Unlock()
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	frame := &relaypb.Frame{
		WorkspaceId: f.route.workspaceID,
		SessionId:   []byte(f.route.sessionID),
		DeviceId:    f.route.deviceID,
		Direction:   f.direction,
		Sequence:    f.seq,
		Payload:     append([]byte(nil), p...),
	}
	f.seq++
	if err := f.stream.Send(frame); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (f *frameConn) Read(p []byte) (int, error) {
	f.recvMu.Lock()
	defer f.recvMu.Unlock()
	if len(f.pending) > 0 {
		return f.drain(p)
	}
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	frame, err := f.stream.Recv()
	if err != nil {
		return 0, err
	}
	f.pending = frame.GetPayload()
	return f.drain(p)
}

func (f *frameConn) drain(p []byte) (int, error) {
	n := copy(p, f.pending)
	f.pending = f.pending[n:]
	return n, nil
}

func (f *frameConn) Close() error {
	f.sendMu.Lock()
	f.recvMu.Lock()
	f.closed = true
	f.recvMu.Unlock()
	f.sendMu.Unlock()
	return f.stream.CloseSend()
}
