// Package relaytest is the integration harness for the multi-device relay
// (ADR-0031 D9): it runs the REAL internal/relay/server.Run on ephemeral
// native-gRPC and grpc-web ports and provides the pieces a multi-device test
// needs -- a pinned native client, a grpc-web client that implements the full
// relaypb.RelayClient interface (so client.Admit / client.OpenData and their
// E2EE layer run unchanged over it), a Daemon stub standing in for the bridge
// device hub, and Device helpers that complete a real E2EE handshake with it.
//
// The Daemon stub is deliberately NOT the bridge: it only follows the
// control-stream contract (DeviceAttach / Sync -> open the daemon side of
// that route, handshake, answer). P12b replaces it with the real hub; P13a
// and P13b reuse the relay, the clients and the frame log.
//
// Nothing here logs keys, secrets or plaintext. The harness is test-only
// tooling, so it takes testing.TB and fails the test on setup errors.
package relaytest
