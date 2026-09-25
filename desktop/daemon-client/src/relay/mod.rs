//! Rust-side E2EE relay client (ADR-0013 part C): pairing, crypto/framing,
//! admission, pairing persistence, and the gRPC transport + reconnect
//! loop. See `docs/plans/active/desktop-relay-transport.md`.

pub mod admission;
pub mod channel;
pub mod client;
pub mod crypto;
pub mod pairing;
pub mod pairing_store;

/// Generated from `internal/relay/relaypb/relay.proto` (see `build.rs`) —
/// the same wire contract the Go daemon/relay/mobile all share.
pub mod relaypb {
    tonic::include_proto!("relay.v1");
}
