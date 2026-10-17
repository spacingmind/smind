package relaytest

import (
	"testing"

	"github.com/spacingmind/smind/internal/relay/client"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// NativeClient dials the relay's native-gRPC listener with the pinned
// fingerprint (client.Dial) and returns the generated client. Data frames
// through it are logged in relay.Frames. The connection closes when the test
// ends.
func NativeClient(t testing.TB, r *Relay) relaypb.RelayClient {
	t.Helper()
	conn, err := client.Dial(r.NativeAddr, r.Fingerprint)
	if err != nil {
		t.Fatalf("relaytest: dial relay: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &recordingClient{RelayClient: relaypb.NewRelayClient(conn), log: r.Frames}
}

// WebClient returns a relaypb.RelayClient that speaks grpc-web to the relay's
// grpc-web listener (pinned TLS), so client.Admit and client.OpenData work
// over it unchanged. Data frames through it are logged in relay.Frames.
func WebClient(t testing.TB, r *Relay) relaypb.RelayClient {
	t.Helper()
	cfg, err := pinnedTLS(r.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	wc := newWebClient(r.WebAddr, cfg)
	t.Cleanup(wc.close)
	return &recordingClient{RelayClient: wc, log: r.Frames}
}
