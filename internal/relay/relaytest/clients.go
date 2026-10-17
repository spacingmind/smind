package relaytest

import (
	"testing"

	"github.com/spacingmind/smind/internal/relay/client"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// DialNative dials a relay's native-gRPC listener with the pinned
// fingerprint (client.Dial). It needs no testing.TB, so non-test binaries
// (the interop harness) can use it; the returned func closes the connection.
func DialNative(addr, fingerprint string) (relaypb.RelayClient, func(), error) {
	conn, err := client.Dial(addr, fingerprint)
	if err != nil {
		return nil, nil, err
	}
	return relaypb.NewRelayClient(conn), func() { _ = conn.Close() }, nil
}

// DialWeb returns a relaypb.RelayClient speaking grpc-web to a relay's
// grpc-web listener (pinned TLS); the returned func releases its idle
// connections.
func DialWeb(addr, fingerprint string) (relaypb.RelayClient, func(), error) {
	cfg, err := pinnedTLS(fingerprint)
	if err != nil {
		return nil, nil, err
	}
	wc := newWebClient(addr, cfg)
	return wc, wc.close, nil
}

// NativeClient dials the relay's native-gRPC listener with the pinned
// fingerprint (client.Dial) and returns the generated client. Data frames
// through it are logged in relay.Frames. The connection closes when the test
// ends.
func NativeClient(t testing.TB, r *Relay) relaypb.RelayClient {
	t.Helper()
	rc, closeFn, err := DialNative(r.NativeAddr, r.Fingerprint)
	if err != nil {
		t.Fatalf("relaytest: dial relay: %v", err)
	}
	t.Cleanup(closeFn)
	return &recordingClient{RelayClient: rc, log: r.Frames}
}

// WebClient returns a relaypb.RelayClient that speaks grpc-web to the relay's
// grpc-web listener (pinned TLS), so client.Admit and client.OpenData work
// over it unchanged. Data frames through it are logged in relay.Frames.
func WebClient(t testing.TB, r *Relay) relaypb.RelayClient {
	t.Helper()
	rc, closeFn, err := DialWeb(r.WebAddr, r.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	return &recordingClient{RelayClient: rc, log: r.Frames}
}
