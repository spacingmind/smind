package bridge

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// DefaultSessionID and DefaultDeviceID are the fixed route ids of the
// single-device protocol (milestone 1). They are what a legacy client (one
// without device identity) uses, and what the hub opens proactively when it
// finds itself behind a relay that predates device announcements.
const (
	DefaultSessionID = "m1-default-session"
	DefaultDeviceID  = "m1-default-device"
)

// reconnectBackoff bounds how long Run waits between failed connect
// attempts, doubling from an initial 1s up to this cap.
const maxReconnectBackoff = 30 * time.Second

// minStableConnection is how long a data session must stay up before Run
// treats it as a real success and resets backoff to its minimum. Without
// this, a connection that keeps dropping within milliseconds of each
// reconnect (a flapping network path, a relay that admits but then
// immediately drops the stream) would reset backoff to 1s on every single
// cycle -- since success only ever came from api.ServeTransport RETURNING
// (which is exactly the failure signal), a rapid-flap connection can never
// escalate its backoff and instead tight-loops, spending its whole retry
// budget on back-to-back reconnect attempts with no pacing at all rather
// than ever giving a struggling relay or network path time to settle.
const minStableConnection = 2 * time.Second

// DaemonKeyID derives a stable admission-time identifier for kp -- the
// same public key material a mobile pairing offer carries, base64'd,
// so a relay operator can recognize which daemon key an admission attempt
// claims without a separate registry.
func DaemonKeyID(kp *e2ee.KeyPair) string {
	return kp.PublicBase64()
}

// Run is the one-call form of Hub.Run for callers that need no device
// registry (tests, and the daemon before its registry is wired): it serves
// devices through a fresh Hub, with legacy clients allowed. See Hub.
func Run(ctx context.Context, cfg Config, kp *e2ee.KeyPair, api Server) error {
	return NewHub(Options{LegacyDevices: true}).Run(ctx, cfg, kp, api)
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
