package bridge

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/wsapi"
)

// DefaultSessionID and DefaultDeviceID name the one E2EE data session this
// milestone bridges per workspace.
//
// The relay's OpenControl already carries a ControlFrame_DeviceAttach
// message shape meant to let a device announce a fresh (sessionID,
// deviceID) pair to the daemon dynamically, but internal/relay/server's
// OpenControl doesn't forward or interpret it yet (it answers anything but
// Ping with Unimplemented) -- building that multi-device announcement
// protocol is real, separate scope (bidirectional control-frame routing
// between independently-authenticated connections, not touched by this
// plan's Item 1 or Item 2). Milestone 1's own scope is a single mobile
// device proving the pairing+E2EE+RPC chain end-to-end, so both the daemon
// bridge (here) and the Item 3 TypeScript client use this fixed,
// well-known pair instead. Milestone 2, which adds real multi-device
// pairing, is expected to replace this with the dynamic announcement path.
const (
	DefaultSessionID = "m1-default-session"
	DefaultDeviceID  = "m1-default-device"
)

// reconnectBackoff bounds how long Run waits between failed connect
// attempts, doubling from an initial 1s up to this cap.
const maxReconnectBackoff = 30 * time.Second

// DaemonKeyID derives a stable admission-time identifier for kp -- the
// same public key material a mobile pairing offer carries, base64'd,
// so a relay operator can recognize which daemon key an admission attempt
// claims without a separate registry.
func DaemonKeyID(kp *e2ee.KeyPair) string {
	return kp.PublicBase64()
}

// Run dials the relay named by cfg, admits, and bridges the resulting E2EE
// data session (DefaultSessionID/DefaultDeviceID, daemon role, kp as the
// daemon's long-lived identity) into api's RPC dispatch table via
// api.ServeTransport -- so a mobile-originated workspace.list call gets the
// same answer a browser tab's would.
//
// It runs until ctx is cancelled, reconnecting with backoff on any
// transport failure. A dropped data session is resumed in place
// (client.DataConn.Resume) whenever possible, keeping the established E2EE
// session (key + counters) rather than re-handshaking; only when Resume
// itself fails (relay restart, or no session yet established) does it fall
// back to a fresh OpenData + Handshake. Run never returns an error for a
// transient relay-side failure -- see cmd/smind/serve.go's caller, which
// treats this as a best-effort background service, not something that
// should crash the daemon.
func Run(ctx context.Context, cfg Config, kp *e2ee.KeyPair, api *wsapi.API) error {
	secret, err := hex.DecodeString(cfg.SecretHex)
	if err != nil {
		return fmt.Errorf("bridge: decode secret: %w", err)
	}
	daemonKeyID := DaemonKeyID(kp)

	var (
		grpcConn *grpc.ClientConn
		dc       *client.DataConn
		backoff  = time.Second
	)
	defer func() {
		if dc != nil {
			_ = dc.Close()
		}
		if grpcConn != nil {
			_ = grpcConn.Close()
		}
	}()

	for ctx.Err() == nil {
		if dc == nil {
			newConn, admissionID, err := dialAndAdmit(cfg, daemonKeyID, secret)
			if err != nil {
				log.Printf("relay bridge: connect: %v", err)
				if !sleepCtx(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff)
				continue
			}
			if grpcConn != nil {
				_ = grpcConn.Close()
			}
			grpcConn = newConn
			rc := relaypb.NewRelayClient(grpcConn)

			go runControl(ctx, rc, admissionID, cfg.WorkspaceID)

			dc, err = handshakeDataConn(ctx, rc, admissionID, cfg.WorkspaceID, kp)
			if err != nil {
				log.Printf("relay bridge: handshake: %v", err)
				dc = nil
				if !sleepCtx(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff)
				continue
			}
			backoff = time.Second
			log.Printf("relay bridge: connected to %s (workspace %s)", cfg.RelayAddress, cfg.WorkspaceID)
		}

		api.ServeTransport(ctx, dc)
		if ctx.Err() != nil {
			return nil
		}

		// The data session's transport dropped (network blip, relay
		// restart, mobile device disconnect). Re-admit on a fresh gRPC
		// connection and try to resume the existing E2EE session in place
		// before ever falling back to a brand new one.
		log.Printf("relay bridge: data session dropped, reconnecting")
		newConn, admissionID, err := dialAndAdmit(cfg, daemonKeyID, secret)
		if err != nil {
			log.Printf("relay bridge: reconnect: %v", err)
			if !sleepCtx(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if grpcConn != nil {
			_ = grpcConn.Close()
		}
		grpcConn = newConn
		rc := relaypb.NewRelayClient(grpcConn)
		go runControl(ctx, rc, admissionID, cfg.WorkspaceID)

		if err := dc.Resume(ctx, rc, admissionID); err != nil {
			log.Printf("relay bridge: resume failed, starting a fresh session: %v", err)
			_ = dc.Close()
			dc, err = handshakeDataConn(ctx, rc, admissionID, cfg.WorkspaceID, kp)
			if err != nil {
				log.Printf("relay bridge: fresh handshake: %v", err)
				dc = nil
				if !sleepCtx(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff)
				continue
			}
		}
		backoff = time.Second
	}
	return nil
}

// dialAndAdmit dials cfg's relay and completes admission, returning the raw
// *grpc.ClientConn (caller owns closing it) and the admission ID.
func dialAndAdmit(cfg Config, daemonKeyID string, secret []byte) (*grpc.ClientConn, string, error) {
	conn, err := client.Dial(cfg.RelayAddress, cfg.Fingerprint)
	if err != nil {
		return nil, "", fmt.Errorf("dial: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admissionID, err := client.Admit(ctx, relaypb.NewRelayClient(conn), cfg.WorkspaceID, daemonKeyID, secret)
	if err != nil {
		conn.Close()
		return nil, "", fmt.Errorf("admit: %w", err)
	}
	return conn, admissionID, nil
}

// handshakeDataConn opens a fresh OpenData stream for the default session
// and completes the E2EE handshake as the daemon role.
func handshakeDataConn(ctx context.Context, rc relaypb.RelayClient, admissionID, workspaceID string, kp *e2ee.KeyPair) (*client.DataConn, error) {
	dc, err := client.OpenData(ctx, rc, admissionID, workspaceID, DefaultSessionID, DefaultDeviceID, kp, e2ee.RoleDaemon)
	if err != nil {
		return nil, fmt.Errorf("open data: %w", err)
	}
	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := dc.Handshake(hctx); err != nil {
		_ = dc.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	return dc, nil
}

// runControl keeps the daemon's control stream open for the connection's
// lifetime -- registration plus liveness, per docs/plans/active/
// mobile-app-milestone-1.md's Item 1 acceptance criteria. It is best-effort:
// internal/relay/server's OpenControl only answers Ping today, so failures
// here never tear down the data session bridge.
func runControl(ctx context.Context, rc relaypb.RelayClient, admissionID, workspaceID string) {
	stream, err := client.OpenControl(ctx, rc, admissionID, workspaceID)
	if err != nil {
		log.Printf("relay bridge: control stream: %v", err)
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	var seq uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seq++
			if err := stream.Send(&relaypb.ControlFrame{
				WorkspaceId: workspaceID,
				Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: seq}},
			}); err != nil {
				log.Printf("relay bridge: control ping: %v", err)
				return
			}
		}
	}
}

// sleepCtx waits for d or ctx's cancellation, returning false in the latter
// case so callers can bail out of a retry loop immediately.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > maxReconnectBackoff {
		return maxReconnectBackoff
	}
	return next
}
