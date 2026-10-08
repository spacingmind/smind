# Relay security hardening (SCRAM-style admission proof, bounded relay state, Go handshake key pin)

Three issues from a security review of the self-hosted relay
(`internal/relay/*`, ADR-0007, ADR-0011). One PR into `develop`.

## Acceptance Criteria

### Fix A — admission proof no longer keyed by the stored hash

- A1. The relay-stored value is unchanged: `StoredKey = SHA256(ClientKey)`,
  where `ClientKey` is the raw 32-byte workspace secret. Existing
  `workspaces.json` files keep working with no migration.
- A2. The client sends `proof = ClientKey XOR HMAC-SHA256(StoredKey,
  transcript)` in the existing `AdmitRequest.hmac` bytes field (field number
  6 unchanged; the wire/generated field name stays `hmac` — see Decisions).
  The transcript is the existing canonical length-prefixed encoding
  (version, workspace_id, client_nonce, server_nonce, daemon_key_id).
- A3. The relay accepts iff `len(proof) == 32` and
  `ConstantTimeCompare(SHA256(proof XOR HMAC(StoredKey, transcript)),
  StoredKey) == 1`, plus all existing checks: single-use nonce, 30s TTL,
  transcript inputs match the challenge, uniform `ErrRejected` (no cause
  oracle, no wrapping).
- A4. Admission `ProtocolVersion` is 2 in Go, mobile TS and desktop Rust.
  Version 1 requests are rejected at both `AdmitChallenge` and `Admit`
  (no dual-accept).
- A5. Go client (`internal/relay/client`), mobile TS
  (`mobile/src/relay/admission.ts`, `RelayConnection.ts`) and desktop Rust
  (`desktop/daemon-client/src/relay/{admission,client}.rs`) all produce the
  new proof. All three implementations agree on a shared fixed test vector.
- A6. A test proves that possession of only `StoredKey` (what an attacker
  gets from reading `workspaces.json`) cannot produce an accepted `Admit`.
- A7. `docs/decisions/0011-relay-admission-auth.md` gets a short dated
  amendment (SCRAM-style proof + version bump); the ADR body is not
  rewritten. Proto/doc/code comments say "proof", not "HMAC tag".

### Fix B — bounded relay memory

- B1. A route (`s.mu.routes`) with no attached stream on either side for
  longer than the reconnect-grace period is garbage-collected, including its
  buffered frames. Default grace 5 minutes, configurable via a Server
  option. Within the grace window the existing reconnect-buffer behavior is
  unchanged (frames buffered while detached are delivered on reattach). A
  route with any attached stream is never collected.
- B2. Admission bindings (`s.mu.bindings`) are bounded per workspace
  (default 64, least-recently-used evicted) and expire when unused for longer
  than a TTL (default 24h). A binding with an attached stream is not
  expired, and `DataConn.Resume` after a normal drop still works (the
  binding's idle clock restarts when its last stream ends).
- B3. Outstanding (issued, unconsumed, unexpired) challenges are capped
  globally (default 4096) and per workspace ID (default 256). When full,
  `Challenge` returns the same uniform `ErrRejected`. Expired challenges are
  evicted before the cap is evaluated, so the cap frees up as TTLs lapse.
- B4. Time is injectable (server option / unexported field) so none of the
  new tests sleep.

### Fix C — Go E2EE Channel peer-key pin (parity with TS/Rust)

- C1. `e2ee.Channel.Handshake` accepts an optional pin for the peer's
  X25519 public key (variadic `HandshakeOption`, `WithExpectedPeerKey`). With
  a pin, a peer hello carrying a different key fails the handshake with the
  sentinel `e2ee.ErrPeerKeyMismatch`, the channel is closed, and no ready
  frame is sent / session derived. No pin = existing behavior.
- C2. The Go relay client's `OpenData` takes an option to set the expected
  peer key (`client.WithExpectedPeerKey`) applied at `DataConn.Handshake`;
  the bridge test (mobile-role device with the offer's daemon key) uses it.
- C3. A test shows a substituted daemon key fails the handshake with
  `ErrPeerKeyMismatch`, and a matching pin succeeds.

### Global

- G1. `task test` and `task lint` pass; mobile relay vitest suite and
  `cargo test` in `desktop/daemon-client` pass (or any environmental
  inability to run one is reported honestly in Validation).
- G2. Out of scope and untouched: daemon authenticating the mobile key,
  per-device Ed25519 identity, WebSocket transport, removing the relay-side
  reconnect buffer.

## Test Scenarios

### Fix A
- `TestAdmitValidBindsWorkspace` (existing, updated to the new proof).
- `TestAdmitStoredKeyAloneCannotAdmit`: attacker holding only `StoredKey`
  fails with `ErrRejected` for: (a) v1-style `proof = HMAC(StoredKey, t)`;
  (b) `proof = StoredKey XOR HMAC(StoredKey, t)`; (c) all-zero proof;
  (d) wrong-length proof (31/33/0 bytes).
- `TestAdmitV1Rejected`: `ProtocolVersion` 1 rejected at Challenge and at
  Admit (even with a correct v2-style proof for the v1 transcript).
- `TestAdmitProofBoundToTranscript`: a valid proof for one server nonce
  replayed against another challenge fails; replay of a used proof fails
  (existing `TestAdmitReplayRejected`, updated).
- `TestAdmitWrongSecretRejected`, `TestAdmitUnknownWorkspaceRejected`,
  `TestAdmitExpiredNonceRejected`, `TestAdmitTranscriptMismatchRejected`,
  `TestRejectionShapeIsUniform` (existing, updated).
- `TestProofKnownVector`: fixed secret/nonces → fixed expected proof hex.
  The identical vector is asserted in mobile `admission` test and Rust
  `admission.rs` unit test (cross-language agreement).
- Rust `relay_harness_interop.rs` and mobile `integration.node.test.ts`
  run the real v2 clients against the real Go relay (already exist; they
  must keep passing, which proves wire compatibility end to end).
- Existing server/grpcweb/client/bridge integration tests pass on v2.
- `workspaces.json` written by the old code (hash only) still admits a v2
  client (`TestEnrolledWorkspaceFromOldStoreAdmitsV2` in `server`).

### Fix B
- `TestRouteGCAfterGrace`: route with buffered frames, both sides detached;
  clock advanced past grace → route removed, `BufferedFrames` 0.
- `TestRouteKeptWithinGrace`: advanced to just under grace → still buffered,
  reattach delivers the frames in order.
- `TestRouteNotGCedWhileAttached`: one side attached for longer than grace
  → route survives; grace timer starts only once both sides are gone.
- `TestRouteReattachResetsGraceClock`.
- `TestBindingCapPerWorkspace`: admit cap+N times → count == cap, oldest
  (least recently used) id fails with unauthenticated, newest works, other
  workspaces' bindings untouched.
- `TestBindingTTLExpiry`: unused binding past TTL → rejected & removed;
  a binding touched inside the TTL survives; a binding with an attached
  stream survives past TTL and is usable for Resume after the stream drops.
- `TestChallengeCapGlobal` / `TestChallengePerWorkspaceCap`: cap+1th
  Challenge → `ErrRejected` (same error value, unwrapped); after TTL
  expiry the slots free up; a consumed challenge frees its slot.

### Fix C
- `TestHandshakePinMismatchFails`: mobile-role channel pinned to key A,
  daemon presents key B → `errors.Is(err, ErrPeerKeyMismatch)`, channel
  closed, daemon never completes.
- `TestHandshakePinMatchSucceeds`.
- `TestHandshakeNoPinUnchanged` (existing handshake tests).
- `client` integration test: `OpenData(..., WithExpectedPeerKey(wrong))`
  → `DataConn.Handshake` fails with `ErrPeerKeyMismatch`.

## Decisions

- **SCRAM-style proof, stored value unchanged.** Exactly as specified in the
  task: the relay keeps `SHA256(secret)`; clients prove knowledge of
  `ClientKey` by XOR-masking it with an HMAC keyed by `StoredKey`. A reader
  of `workspaces.json` learns `StoredKey` but cannot produce `ClientKey`
  (preimage of SHA-256 over 256 random bits). Note the residual (inherent to
  SCRAM): an attacker who both reads `workspaces.json` AND observes one full
  admission exchange can recover `ClientKey`; admission runs over
  fingerprint-pinned TLS 1.3, so this needs relay-side compromise at
  runtime, not just a disk read. That is the same boundary SCRAM accepts.
- **Wire field name stays `hmac`.** `protoc`/`protoc-gen-go` are not
  available in this environment and the rename is optional per the task;
  field number 6 and bytes type are unchanged. Proto/Go/TS/Rust comments are
  rewritten to say the field carries the *proof* (a follow-up can rename the
  field when the proto toolchain is available).
- **No `ComputeHMAC` export in Go anymore.** Replaced by
  `ComputeProof(secret, req)` (client) and an unexported verify path
  (relay); `SessionSignature(storedKey, req)` is exported for tests/clients
  needing the HMAC step. Leaving a function that HMACs with the stored hash
  exported invites the old misuse.
- **No v1 dual-accept.** v1 is the vulnerable scheme; old mobile/desktop
  builds cannot admit to a new relay and vice versa (called out in the PR).
- **Route GC** is lazy (on route attach, rate-limited) plus an exported
  `Sweep()` that `Run` calls from a ticker goroutine (`RunJanitor`) tied to
  its ctx, so an idle relay also frees memory. Clock injected via
  `server.WithClock` / `admission.WithClock`. Options: `WithRouteGrace`,
  `WithBindingLimits`.
- **Bindings** are LRU-capped per workspace and TTL'd by last use, where
  "use" = stream open *and* stream end, and bindings with live streams are
  never expired. This keeps `Resume` working for arbitrarily long-lived
  streams while still bounding abandoned admissions (every `Admit` creates
  one).
- **Challenge caps** are global + per-workspace-ID. When full we reject
  (per task) rather than evict, so an attacker flooding `AdmitChallenge`
  can make new admissions fail for up to one 30s TTL; this is a bounded
  availability trade for bounded memory and is documented in the code. Rate
  limiting remains a transport concern (unchanged).
- **Pin fails closed.** `WithExpectedPeerKey(nil/empty/short)` can never
  match (→ `ErrPeerKeyMismatch`) instead of silently meaning "no pin".
  Omitting the option is the only way to run unpinned.
- **Go pin API** is a variadic `HandshakeOption` on `Handshake` (mirrors
  Rust's `pin_peer_public_key` / TS's `daemonPublicKeyIfMobile` being a
  handshake-time argument) with sentinel `ErrPeerKeyMismatch` (Rust:
  `ChannelError::PinMismatch`).
- No new ADR needed: nothing here changes the architecture decided in
  ADR-0007/0011 (admission amendment only), and the out-of-scope items are
  untouched.

## Progress

- [x] Plan committed
- [x] Fix A: Go admission (`ComputeProof`/`ProofMask`/`verifyProof`, v2) + tests
- [x] Fix A: proto comments (field kept as `hmac`), Go client, server tests, ADR-0011 amendment
- [x] Fix A: mobile TS (`computeProof`, v2, `admission.test.ts` vector)
- [x] Fix A: desktop Rust (`compute_proof`, v2, vector test)
- [x] Fix B: challenge caps (global 4096 / per-workspace-ID 256)
- [x] Fix B: route GC (grace 5m) + bindings bound (64/ws LRU, 24h idle TTL, in-use never expires) + janitor in `Run`
- [x] Fix C: Go pin (`e2ee.WithExpectedPeerKey`, `ErrPeerKeyMismatch`) + `client.WithExpectedPeerKey` + tests
- [x] Verification

## Validation

Environment notes: `task` is not installed here, so its commands were run
directly (`go vet ./...`; `gofmt -l`; `go test ./...`; web:
`bun run --filter '@smind/ui' test`). `cargo` was not installed either; a
throwaway rustup toolchain (stable 1.99) was installed under `/tmp` (not in
the user's home) to run the Rust tests, with `CARGO_TARGET_DIR` in `/tmp`.

Commands (all green):

- `go vet ./...` — clean. `gofmt -l $(git ls-files '*.go')` — empty.
- `go test ./...` — all packages ok; `go test -race -count=2 ./internal/relay/...` — ok.
- `cd web && bun run --filter '@smind/ui' test` — 106 files / 1346 tests pass.
- `cd mobile && npx vitest run --exclude "**/*.node.test.ts"` — 15 files / 69 tests pass; `npx tsc --noEmit` clean; `npm run test:integration` (real TS client ↔ real Go relay + bridge harness, v2) — 2/2 pass.
- `cd desktop/daemon-client && cargo test --lib` — 182 pass (incl. proof vector + recover tests); `cargo test --test relay_harness_interop` (real Rust client ↔ real Go relay harness, v2, pin, resume) — pass.

Per acceptance criterion:

- A1 (stored value unchanged): `server.TestEnrolledWorkspaceFromOldStoreAdmitsV2` — a hash-only `workspaces.json` admits a v2 client.
- A2/A3 (proof + verification, uniform rejection): `admission.TestProofKnownVector`, `TestAdmitValidBindsWorkspace`, `TestAdmitWrongSecretRejected`, `TestAdmitUnknownWorkspaceRejected`, `TestAdmitReplayRejected`, `TestAdmitExpiredNonceRejected`, `TestAdmitTranscriptMismatchRejected`, `TestAdmitProofBoundToChallenge`, `TestRejectionShapeIsUniform`, `TestProofMaskCanonicalForm`.
- A4 (v2, no v1): `admission.TestAdmitV1Rejected`, `TestChallengeValidation` (v1 → ErrRejected); `ProtocolVersion` is 2 in Go, `mobile/src/relay/admission.ts` (`admission.test.ts` "speaks protocol v2"), and Rust (`protocol_version_is_2`).
- A5 (all clients + cross-language agreement): same fixed vector `b007e940…7cfb` asserted in Go `TestProofKnownVector`, mobile `admission.test.ts`, Rust `proof_matches_cross_language_known_vector`; the vector was also independently reproduced with a Python HMAC/XOR. End-to-end: mobile `integration.node.test.ts` + Rust `relay_harness_interop.rs` + Go `client`/`bridge`/`server` integration tests all admit against the v2 Go relay.
- A6 (stored hash alone cannot admit): `admission.TestAdmitStoredKeyAloneCannotAdmit` — eight forgeries from `StoredKey` (v1-style `HMAC(StoredKey,t)`, StoredKey-as-ClientKey, mask⊕StoredKey, zero, wrong lengths, StoredKey itself) all `ErrRejected`; genuine secret still admits.
- A7 (docs): ADR-0011 "Amendment 2026-10-08"; proto + generated-Go comments + code comments say "proof".
- B1: `server.TestRouteGCAfterGrace`, `TestRouteKeptWithinGrace` (buffered frames flushed in order inside grace), `TestRouteNotGCedWhileAttached`, `TestRouteReattachResetsGraceClock`.
- B2: `server.TestBindingCapPerWorkspaceEvictsLRU`, `TestBindingCapPrefersEvictingIdleOverInUse`, `TestBindingTTLExpiry`, `TestBindingInUseNeverExpiresAndResumeWorks`, `TestDataStreamResumeAfterLongLivedStream` (real OpenData streams); existing `client` reconnect/Resume integration tests still pass.
- B3: `admission.TestChallengeGlobalCap`, `TestChallengePerWorkspaceCap`, `TestChallengeBookkeepingDoesNotLeak` (cap → exactly `ErrRejected`; expiry/consumption frees slots).
- B4: all of the above use `fakeClock`/`WithClock`; no test sleeps for a grace/TTL.
- C1/C3: `e2ee.TestHandshakePinMismatchFails` (error class `ErrPeerKeyMismatch`, channel closed, no ready frame, no session), `TestHandshakePinMatchSucceeds`, `TestHandshakePinFailsClosed`, `TestHandshakeNoPinUnchanged`.
- C2: `client.WithExpectedPeerKey`; used by the bridge tests' mobile-role device and `client` integration `pairWithClients`; `client.TestIntegrationSubstitutedDaemonKeyFailsHandshake` over a real relay.
- G1: see commands above. G2: nothing in the out-of-scope list was touched.

Known gaps / residuals (also in the PR description): the wire field is still
named `hmac` (no protoc here; generated-code comments were hand-synced and
the descriptor is unchanged); an unauthenticated `AdmitChallenge` flood can
still fill the (bounded) challenge budget for up to one 30s TTL and make new
admissions fail until it lapses — rate limiting stays a transport concern.
