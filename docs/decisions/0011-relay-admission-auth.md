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

## Cross-references

- `docs/decisions/0007-relay-architecture.md` — relay architecture and
  E2EE handshake this ADR's admission layer is explicitly independent of.
- `docs/research/relay-design-status.md` §7 — the research (pplx query
  comparing token/mTLS/capability-token approaches against Tailscale
  DERP/ntfy/rathole/chisel) this decision is based on.
- `docs/plans/active/relay-e2ee-mobile.md` — implementation plan; this
  ADR resolves that plan's previously-open "auth between daemon and
  relay" item.
