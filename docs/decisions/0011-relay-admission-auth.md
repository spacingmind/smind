# 0011: Relay admission — daemon↔relay auth, separate from the E2EE handshake

## Status

Accepted

## Decision

The daemon↔relay connection is authenticated and authorized as its own
state machine, independent of the E2EE handshake between daemon and
mobile (ADR-0007). A successful E2EE handshake is never treated as
proof that a daemon may use the relay, and a successful relay admission
is never treated as proof of E2EE authorization — the relay still
cannot decrypt application payloads either way.

Design, decided per `docs/research/relay-design-status.md` §7:

- **Transport**: TLS. The relay uses a self-signed certificate (no
  external CA); the daemon pins the relay's certificate fingerprint at
  pairing time (mirrors the `chisel`/`rathole` pattern of server-key
  pinning instead of a public CA chain).
- **Admission secret**: each workspace gets a random 256-bit secret,
  generated at pairing time. The relay persists only a hash of it
  (never the plaintext secret).
- **Challenge-response, not a raw bearer token per connection**: the
  daemon authenticates via a nonce-based HMAC over a transcript
  (protocol version, workspace ID, both nonces, daemon key ID) — the
  secret itself is never sent on the wire after pairing, closing off
  replay of a captured handshake.
- **Binding**: a successfully authenticated connection is bound to
  exactly one workspace ID; any subsequent frame whose workspace/session
  ID doesn't match the authenticated binding is rejected.
- **Optional upgrade, v1.x**: the daemon may register an Ed25519 public
  key at pairing time and sign the challenge transcript instead of
  proving knowledge of the workspace secret — the secret then becomes a
  bootstrap/recovery credential only, not the thing checked on every
  connection.

## Alternatives considered

- **Bearer/API token sent per connection, no challenge-response.**
  Simplest to implement, but a token in transit or in a log/backup can
  be replayed by anyone who obtains it — no proof-of-possession beyond
  "has the string."
- **mTLS with pinned self-signed client certificates as the only admission
  mechanism.** Stronger device identity than a shared secret, but forces
  the daemon-key lifecycle (rotation, recovery from a lost private key,
  mobile-platform client-cert handling) to be solved before v1 can ship
  anything; deferred to the optional Ed25519 upgrade path instead, which
  gets equivalent proof-of-possession without a certificate lifecycle.
- **Signed capability tokens (macaroons/biscuits).** Built for
  delegation, scoping, and independently-minted restricted credentials —
  useful when multiple parties mint tokens under a shared root key. Here
  the relay is the only authorization authority for its own workspaces,
  so a database-backed secret-hash plus HMAC challenge-response gets the
  same practical guarantees without the extra protocol/library surface.

## Rationale

`docs/plans/active/relay-e2ee-mobile.md`'s Acceptance Criteria explicitly
left "auth between daemon and relay beyond what the E2EE handshake and
connection identity require" open, pending a follow-up decision — ADR-0007
(f) scoped daemon↔relay transport (gRPC) but not this. Comparable
self-hosted relay/tunnel tools (Tailscale DERP, `ntfy`, `rathole`,
`chisel`) all keep "who may use this relay" as a layer distinct from
whatever the relay is blindly forwarding; conflating it with the E2EE
handshake would mean a bug or edge case in the mobile-pairing crypto
could also become a relay-admission bypass, which is a strictly larger
blast radius than keeping the two independent. A per-workspace secret
with challenge-response (rather than a bare token) is the smallest
change that removes the "anyone who captures one request can replay it"
weakness, without requiring a certificate-lifecycle story before v1 can
ship.

## Amendments

**Amendment 2026-10-08 — SCRAM-style proof; admission protocol v2.**
A security review found that v1 keyed the transcript HMAC with the very
value the relay persists (`HMAC(SHA-256(secret), transcript)`), so anyone
who could read the relay's `workspaces.json` could admit as any workspace —
the "relay persists only a hash" property bought nothing. The proof is now
SCRAM-style (RFC 5802 idea), with the stored value unchanged (existing
`workspaces.json` files keep working):

- `ClientKey` = the raw 32-byte workspace secret; `StoredKey` =
  `SHA-256(ClientKey)` (what the relay persists).
- `proof = ClientKey XOR HMAC-SHA256(StoredKey, transcript)`, sent in
  `AdmitRequest.hmac` (field number and name unchanged; the field now
  carries the proof). The transcript encoding is unchanged.
- The relay recovers `ClientKey' = proof XOR HMAC(StoredKey, transcript)`
  and accepts iff `len(proof) == 32` and `SHA-256(ClientKey') == StoredKey`
  (constant-time). Single-use nonce, 30s TTL, transcript-input matching and
  the uniform rejection are unchanged.
- `ProtocolVersion` is bumped to 2 in the Go, mobile (TS) and desktop
  (Rust) implementations. v1 is rejected (no dual-accept: v1 *is* the
  vulnerable scheme), so old mobile/desktop builds cannot admit to a new
  relay and vice versa; there is no data migration.

Residual (inherent to SCRAM): an attacker who has `workspaces.json` *and*
observes a full admission exchange can recover `ClientKey`; admission runs
over fingerprint-pinned TLS 1.3, so that requires a live relay-side
compromise, not merely a disk read. Daemon authentication of the mobile
key and per-device identity remain the deferred upgrade path above.

Related hardening shipped alongside (no decision change): the relay bounds
outstanding challenges, admission bindings and idle routes (see
`docs/plans/completed/relay-security-hardening.md`).

## Cross-references

- `docs/decisions/0007-relay-architecture.md` — relay architecture and
  E2EE handshake this ADR's admission layer is explicitly independent of.
- `docs/research/relay-design-status.md` §7 — the research (pplx query
  comparing token/mTLS/capability-token approaches against Tailscale
  DERP/ntfy/rathole/chisel) this decision is based on.
- `docs/plans/active/relay-e2ee-mobile.md` — implementation plan; this
  ADR resolves that plan's previously-open "auth between daemon and
  relay" item.
