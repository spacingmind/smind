# Desktop relay transport — Rust-side E2EE relay client (ADR-0013, part C)

## Context

ADR-0013 (`docs/decisions/0013-desktop-bundled-ui.md`) moved the desktop
app to a bundled UI behind a Rust-side loopback proxy
(`desktop/daemon-client/src/proxy`, shipped in
`docs/plans/completed/desktop-bundled-ui.md`, part 1). That plan explicitly
deferred the relay transport. Its sub-decision (user, 2026-09-25) is the
starting point here:

> **Relay transport: Rust-side.** Rust holds the E2EE keys and bridges
> `/ws` JSON-RPC frames onto the relay channel, answering `/api/token`
> itself. The bundled UI stays transport-agnostic, and key material never
> enters the webview.

This plan (part C) adds a `relay` `ConnectionKind` to
`desktop/daemon-client/src/proxy`: pairing by pasting a pairing URL (the
same format `mobile/src/relay/*` accepts), an E2EE relay client in Rust
byte-compatible with `internal/relay/*` (Go) and `mobile/src/relay/*`
(TypeScript), and wiring so the proxy's `/ws` and the daemon-client
watcher (tray/notifications, `desktop/src-tauri/src/client_watch.rs`) both
work over it.

**Transport decision (user, 2026-09-25 — see ADR-0007 amendment below):**
the Rust relay client speaks **native gRPC** to the relay's plain gRPC
listener (`internal/relay/server.DefaultListenAddr`, `:7400` by default),
generated from `internal/relay/relaypb/relay.proto` via `tonic-build`/
`prost` — the same transport `internal/relay/client` (Go, daemon-side)
already uses. This is *not* what `mobile/src/relay/*` does (grpc-web over
WebSocket, `internal/relay/server.DefaultGRPCWebListenAddr`, `:7401`) —
mobile's choice was forced by browser/React-Native's lack of native
HTTP/2/gRPC support, a constraint that doesn't apply to a native desktop
process. Recorded as an amendment to ADR-0007(f), which had explicitly
left mobile/device↔relay transport unresolved. Cross-language
byte-compatibility is unaffected either way: the E2EE handshake/framing
(hello/ready/data frames, ChaCha20-Poly1305 ciphertext, HKDF-derived keys)
rides inside the protobuf `Frame.payload` regardless of which gRPC
transport carries it.

**What exists today (`desktop/daemon-client/src/proxy`, after #196):**
- `ConnectionKind::{Local, Url}` (`connections.rs`) — no `Relay` variant.
- `Connection { id, kind, label, base_url }`, persisted as plaintext JSON
  (`connections.json` in the app data dir, **no permission hardening**).
- `proxy_http`/`proxy_ws` (`server.rs`) are pure, kind-agnostic reverse
  proxies: `/api/*` via `reqwest`, `/ws` via `tokio_tungstenite`, both
  built from `current().base_url`. There is no per-kind branching
  anywhere in the proxy today.
- `connections_add(label, url)`, `connections_select`, etc.
  (`desktop/src-tauri/src/commands.rs`) assume a bare dialable URL.
- `client_watch::spawn`/`restart` (`desktop/src-tauri/src/client_watch.rs`)
  independently dial the daemon for the tray/notification watcher, via
  `Config { daemon_url }` — a second connection path, separate from the
  proxy, that also assumes a plain dialable URL.

**Reference material read for this plan:**
- ADR-0007 (`docs/decisions/0007-relay-architecture.md`): store-and-forward
  (200-frame bounded buffer per side, `internal/relay/server/framequeue.go`),
  control/data stream split, X25519 + HKDF-SHA256 + ChaCha20-Poly1305
  (IETF, 12-byte counter nonces — 4 zero bytes + 8-byte BE counter),
  strict in-order replay protection (no sliding window), rotation = new
  session only, transport-level reconnect = resume same session/keys/
  counters (2026-09-17 amendment), pairing = persistent daemon keypair +
  URL-fragment offer.
- ADR-0011 (`docs/decisions/0011-relay-admission-auth.md`): daemon↔relay
  admission is independent of the E2EE handshake — per-workspace 256-bit
  secret, nonce-based HMAC-SHA256 challenge-response transcript, TLS with
  a pinned self-signed relay cert fingerprint (no CA).
- `internal/relay/{relaypb,e2ee,pairing,admission,client,bridge,server}`
  (Go) — wire format, handshake state machine, admission RPCs, the daemon
  bridge's reconnect loop, and the `bridge/harness` test harness.
- `mobile/src/relay/*` (TypeScript) — confirms the same crypto/framing at
  the byte level (`@noble/{curves,ciphers,hashes}` 2.4.0) and that it has
  **no** reconnect/backoff/resume logic of its own (that's server-side
  store-and-forward plus the Go daemon bridge's client-side policy, not a
  device-side concern mobile implements).
- `desktop/daemon-client/src/proxy/*`, `tests/proxy_integration.rs`,
  `desktop/src-tauri/src/{commands,state,client_watch,tray,notify}.rs`,
  and `docs/plans/completed/desktop-bundled-ui.md` — current proxy/IPC/
  watcher shape and its own AC/Decisions/Progress/Validation template,
  reused here.

**Hard constraints (task):**
- No Go daemon/relay protocol change. (One existing Go-side milestone-1
  simplification is unavoidable input, not something this plan changes —
  see the "Known limitation" below.)
- No IPC command may expose key material (device private key, workspace
  admission secret) to the webview.
- Remove/unpair must delete persisted key material.

**Known limitation carried over from Go, not fixed here:** the daemon's
own relay bridge (`internal/relay/bridge.go`) hardcodes a single
`(session_id, device_id)` pair (`DefaultSessionID = "m1-default-session"`,
`DefaultDeviceID = "m1-default-device"`) — "milestone-1 simplification:
exactly one hardcoded (session,device) pair per workspace; real per-device
announcement via `ControlFrame_DeviceAttach` is unbuilt" (`bridge.go`'s own
comment). The relay server's route model supports fan-out to multiple
devices in principle (ADR-0007 (b)), but today only one device stream can
be attached to a given route at a time — a second device attaching with
the same IDs replaces the first (`internal/relay/server/server.go`
`attachRoute`). The desktop relay client **must** use these same fixed IDs
to reach the daemon's bridge at all, which means **a mobile device and a
desktop relay connection to the same workspace cannot both be attached at
once** — connecting one will disconnect the other. This is an accepted,
pre-existing gap (not a regression this plan introduces), out of scope to
fix (it needs the unbuilt `ControlFrame_DeviceAttach` work, which is a Go
change and its own plan).

## Acceptance Criteria

- **AC1: `relay` `ConnectionKind`, created from a pairing URL.**
  - `ConnectionKind::Relay` added alongside `Local`/`Url`.
  - A relay connection is created by pasting the same pairing URL format
    mobile accepts (`<base>#offer=<base64url(JSON)>`,
    `internal/relay/pairing/offer.go`), parsed and validated entirely in
    Rust (version check, required fields, 32-byte X25519 public key
    shape) — no network round trip needed just to validate the paste.
  - The relay's **native gRPC** address is derived from the offer's
    `Relay` field (which encodes the grpc-web address — mobile-only
    convention, see `cmd/smind/relay.go`'s `cmdRelayOffer`) by the same
    "native port = grpc-web port − 1" convention that Go's own
    `deriveGRPCWebAddress` already documents and relies on for the
    inverse direction. Document this as a known, narrow limitation
    (breaks for a non-default, non-adjacent port deployment) — matching
    the existing accepted milestone-1 scope, not a new one.
- **AC2: pairing + device-key persistence, restrictive permissions.**
  - Per-connection pairing material (daemon id, daemon public key, relay
    native address, relay TLS fingerprint, workspace id, admission
    secret) persists in the app data dir, one file per relay connection,
    **0600 on unix**. Document the Windows posture (no POSIX mode bits;
    relies on the app data directory's own per-user ACL, same posture as
    Go's own `bridge/config.go`/`e2ee/keypair.go`, which write the same
    way via `os.WriteFile(path, data, 0o600)` — a no-op permission bit on
    Windows there too).
  - The device's own X25519 keypair is generated **fresh per connect**,
    not persisted — matching mobile's own choice
    (`mobile/src/relay/e2ee.ts`) and explicitly allowed by ADR-0007(e)'s
    2026-09-17 amendment, which names "process restart with nothing
    persisted" as exactly the case that falls back to a fresh handshake
    rather than resuming a session. Document this as a deliberate
    decision, not an oversight: persisting the ephemeral session key
    *and* both AEAD counters would be needed to safely resume across a
    process restart (reusing a key at a previously-used counter value is
    a nonce-reuse violation), and nothing in the task requires
    surviving a full app restart without a fresh handshake.
  - `connections_remove` for a `relay` connection deletes the pairing
    file. No IPC command returns the admission secret or any private key
    material.
- **AC3: handshake + framing, byte-compatible with Go/TypeScript.**
  - X25519 key agreement, HKDF-SHA256 (salt = `SHA256("smind relay e2ee
    v1" || daemonPub || mobilePub)`, directional info strings), ChaCha20-
    Poly1305 (IETF, 12-byte nonce = 4 zero bytes + 8-byte BE counter),
    hello/ready/data frame types and the `[4B BE length][1B type][payload]`
    envelope, strict in-order counter replay rejection — all matching
    `internal/relay/e2ee/{handshake,session}.go` exactly.
  - Admission challenge-response (HMAC-SHA256 over the exact
    length-prefixed transcript in `internal/relay/admission/admission.go`)
    implemented in Rust, driving the `AdmitChallenge`/`Admit` unary RPCs.
  - The Rust device plays the "mobile" role (`Role::Mobile = 2`) in the
    handshake and pins the peer public key to the offer's `PublicKey`,
    exactly like `mobile/src/relay/e2ee.ts`'s `handshake()`.
- **AC4: reconnect with backoff + store-and-forward resume.**
  - On a transport-level drop (the gRPC stream/connection only), the
    client resumes the **same** E2EE session (same key, same counters) by
    reopening `OpenData` and re-presenting the same `admission-id` —
    mirroring `internal/relay/client.DataConn.Resume` and
    `internal/relay/bridge.Run`'s Resume-first-then-fresh-handshake
    policy, with exponential backoff (mirroring `bridge.go`'s 1s→30s
    doubling and its "reset backoff only after `minStableConnection`"
    anti-flap rule).
  - A brand new connect (first pairing, or the process never established
    a session this run) always does a fresh handshake.
  - Frames buffered relay-side during a disconnect (up to the relay's
    200-frame bound, `internal/relay/server/framequeue.go`) are delivered
    in order once the client reattaches — no client-side cursor/sequence
    is presented; resume is "reattach to the same route key," exactly as
    the Go relay server implements it.
- **AC5: proxy bridges `/ws` onto the relay, synthesizes `/api/token`.**
  - For a `relay`-kind selected connection, `proxy_http`'s handling of
    `GET /api/token` is answered locally by Rust (no `reqwest` call) —
    the bundled UI's `fetchToken()`/`{token: string}` contract is
    satisfied with a synthesized value; the real security boundary is the
    E2EE tunnel plus the existing per-launch loopback-proxy secret
    cookie, not this token (documented as a Decision).
  - `proxy_ws`'s `/ws` upgrade, for a `relay`-kind connection, pipes each
    inbound/outbound WebSocket message directly as the plaintext JSON-RPC
    envelope (`internal/wsapi`'s `envelope`) into/out of the relay E2EE
    channel — no `connect_async` to any `base_url`.
  - `Local`/`Url` connections are completely unaffected — same
    `reqwest`/`tokio_tungstenite` code path as today.
- **AC6: IPC — extend `connections_add` for relay, no key-exposing command.**
  - A new command (e.g. `connections_add_relay(label, pairing_url) ->
    Result<Connection, String>`) parses and validates the pairing URL in
    Rust, persists the pairing file (AC2), and returns a `Connection`
    with only non-secret display fields (workspace id, relay host, daemon
    id) — never the admission secret or any key.
  - `connections_list`/`connections_get_current`/`connections_select`/
    `connections_remove` all handle the `Relay` variant correctly.
  - Every command validates its own input in Rust (matching AC5 of the
    completed bundled-UI plan's precedent).
- **AC7: daemon-client watcher (tray, notifications) works over relay.**
  - `client_watch::spawn`/`restart` — currently `Config { daemon_url }`,
    a plain dialable URL for `dclient::client::run` — is extended so that
    when the selected connection is `relay`-kind, the same underlying
    relay transport the proxy uses is what the watcher's WS-equivalent
    JSON-RPC transport rides on, so `permission.pending`/task-run/
    reconnect tray and notification behavior all keep working.
  - `connections_select` onto/away from a relay connection correctly
    starts/stops the relay transport and restarts the watcher, exactly as
    it already does for `Local`/`Url` via `client_watch::restart`.
- **AC8: no regressions, no Go changes.**
  - `cargo test` (both `desktop/daemon-client` and `desktop/src-tauri`)
    passes.
  - `task test` and `task lint` pass.
  - `git diff --stat -- internal/ cmd/` against this plan's changes is
    empty (aside from the ADR-0007 amendment, which is docs-only).
  - The `desktop-windows` CI workflow is green.

## Test Scenarios

- **Unit tests (Rust, `desktop/daemon-client`):**
  - Pairing URL parse/validate: the exact fixture from
    `internal/relay/pairing/fixture_test.go` (also mirrored in
    `mobile/src/relay/__tests__/pairing.test.ts`) round-trips to the same
    fields; version mismatch, missing fields, and a malformed/short
    public key are all rejected.
  - Crypto/framing cross-checked against `internal/relay/e2ee/fixture_test.go`'s
    pinned vectors: deterministic seeded X25519 keypairs (`0x11`/`0x22`
    repeated-byte seeds) reproduce the exact pinned public keys; sealing
    the fixture plaintexts at counter 0 reproduces the exact pinned
    ciphertexts in both directions; the pinned hello-frame hex round-trips
    through the frame encoder; a tampered ciphertext byte fails to open; a
    repeated counter is rejected (replay).
  - Admission HMAC transcript: reproduce
    `internal/relay/admission/admission_test.go`'s
    `TestComputeHMACCanonicalForm` inputs and confirm the transcript
    construction doesn't collide across a nonce/string-boundary shuffle
    (mirrors that test's own assertions, since no numeric HMAC vector is
    hardcoded in Go to copy directly).
  - Handshake state machine edge cases mirroring
    `internal/relay/e2ee/rotation_test.go`: duplicate hello with the same
    key before ready is tolerated; a hello with a *different* key
    (before or after ready) is rejected/closes the channel.
  - Pairing/device-key file persistence: 0600 permission on unix, missing/
    malformed file handled without crashing, `connections_remove` deletes
    the file.
  - Native-gRPC address derivation from a grpc-web offer address (the
    port−1 convention) — normal case and a documented failure case
    (non-numeric port).
- **Integration test — mandatory, against the real Go relay harness:**
  run `go run ./internal/relay/bridge/harness`, capture the printed
  `READY <pairingUrl>` line, and drive the real Rust client against it:
  1. Parse the printed pairing URL.
  2. Admit + handshake succeeds (native gRPC to the harness's relay
     listener).
  3. A JSON-RPC round trip through the bridged channel (e.g.
     `workspace.list`, which the harness's `wsapi.API` answers) returns a
     valid response.
  4. Disconnect the transport (simulate a network drop without tearing
     down the session, mirroring `DataConn.DropTransport` in Go) while
     the harness keeps buffering; reconnect and confirm buffered/in-flight
     frames are still delivered and a further JSON-RPC call still works.
- **Rust integration test — proxy bridging:** extend
  `desktop/daemon-client/tests/proxy_integration.rs` (or a sibling test
  file) with a `relay`-kind connection backed by the real harness:
  `/api/token` returns the synthesized `{token}` JSON with no outbound
  HTTP call; a `/ws` round trip through the proxy reaches the harness
  daemon and gets a real response.
- **`cargo test` full run** for both crates; **live WSLg run** (Progress/
  Validation) against the harness, never touching the user's real daemon
  on port 4648.

## Decisions

*(Filled in as implementation proceeds; the transport choice and the
milestone-1 fixed-IDs limitation above are already decided.)*

- **Crate layout:** a new `desktop/daemon-client/src/relay/` module
  (pairing, crypto/framing, admission, client, pairing_store), alongside
  `proxy/`, not in `src-tauri` — same testability rationale as the
  existing `proxy` module (runs and tests without webkit2gtk).
- **gRPC codegen:** `tonic-build` + `prost`, compiling
  `internal/relay/relaypb/relay.proto` directly (relative path from the
  crate) — no protobuf schema is duplicated or hand-maintained in Rust.
  Uses vendored `protoc` (`protoc-bin-vendored` or equivalent) so the
  Windows CI build doesn't need a system `protoc` install.
- **TLS cert pinning:** the relay's self-signed cert (no CA) is verified
  by a custom `rustls` certificate verifier that checks the DER cert's
  SHA-256 against the pairing offer's `RelayFingerprint`, mirroring
  `internal/relay/client.Dial`'s `VerifyPeerCertificate` — not hostname/CA
  validation.
- **Crypto crates:** `x25519-dalek` (or RustCrypto's `x25519`), RustCrypto
  `chacha20poly1305`, `hkdf`+`sha2`, `hmac`+`sha2` — chosen for
  interoperability with Go's `crypto/ecdh`+`golang.org/x/crypto/chacha20poly1305`
  and TypeScript's `@noble/*` 2.4.0, all implementing the same standard
  primitives (X25519 RFC 7748, ChaCha20-Poly1305 RFC 8439, HKDF RFC 5869).
- **`/api/token` synthesis (AC5):** a random opaque value is returned in
  `{token}` for a `relay`-kind connection; it is not checked against
  anything when the UI later opens `/ws?token=...` through the proxy,
  since the E2EE tunnel and the loopback proxy's own per-launch secret
  cookie (ADR-0013 AC3) are what actually gate access — the token exists
  only to satisfy the bundled UI's existing same-origin contract
  unmodified.
- **Single shared tunnel:** the relay's `OpenData` bidi-stream is treated
  as one shared, multiplexed pipe per relay connection (matching how the
  daemon's own `wsapi.ServeTransport` treats one relay session as one
  logical JSON-RPC transport) — the proxy's `/ws` handler forwards the
  (in practice singular) webview WebSocket's frames onto it directly,
  rather than opening a new relay data session per browser tab. This
  matches the bundled UI's single-window architecture; multi-tab fanout
  is out of scope.

## Progress

- [ ] AC1 pairing URL parsing/validation + `ConnectionKind::Relay` +
      native-address derivation
- [ ] AC2 pairing/device-key persistence (0600, delete-on-remove)
- [ ] AC3 handshake + framing (crypto module, cross-checked against Go/TS
      fixtures)
- [ ] AC4 gRPC client (tonic, pinned TLS), admission, reconnect/backoff,
      resume
- [ ] AC5 proxy `/ws` + `/api/token` bridging for `relay` kind
- [ ] AC6 IPC: `connections_add_relay` + existing commands handle `Relay`
- [ ] AC7 `client_watch` works over relay
- [ ] AC8 regressions + Windows CI

## Validation

*(Filled in once each AC lands: test counts, interop run output, live
WSLg walkthrough, Windows CI run link.)*
