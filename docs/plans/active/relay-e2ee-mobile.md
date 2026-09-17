# Relay: E2EE relay server + mobile pairing (Phase 3)

Builds `docs/ROADMAP.md` Phase 3's relay half: a self-hostable,
dumb-pipe relay server (`smind relay`) that lets a paired mobile device
reach a daemon it can't otherwise route to directly, without the relay
ever seeing plaintext. Based on `docs/research/relay-design-status.md`
(read-only audit: zero relay code exists today) and
`docs/decisions/0007-relay-architecture.md` — **Accepted 2026-09-11**:
crypto is X25519 + ChaCha20-Poly1305 (counter-based nonces); all other
choices per the ADR's recommendations. Daemon↔relay admission (who may
use the relay at all, separate from the E2EE handshake) is
`docs/decisions/0011-relay-admission-auth.md` — **Accepted
2026-09-17**: per-workspace secret + HMAC challenge-response over
pinned TLS, optional Ed25519 upgrade.

**This plan does not cover** the Expo mobile app UI (pairing screen,
workspace/task list, timeline, push notifications) — that is a
separate, later plan once the relay/daemon side here is real. This
plan's scope is the relay server, the daemon-side E2EE/pairing/
reconnect/rotation machinery, and the wire protocol between them.

## Acceptance Criteria

- **`smind relay` subcommand**: a new self-hostable relay server,
  runnable standalone (`smind relay --listen :PORT` or equivalent),
  independent of the daemon binary's other subcommands. Dumb pipe: the
  relay forwards ciphertext frames between a daemon connection and its
  paired mobile connection(s) and never has access to the E2EE session
  key. It authenticates connections at the transport/session-identity
  layer (which side is which daemon/device) but cannot decrypt or
  inspect application payloads.
- **E2EE handshake (daemon ↔ mobile, relay as blind forwarder)**:
  X25519 key agreement + ChaCha20-Poly1305 AEAD with counter-based
  (12-byte) nonces, per ADR-0007 (c) as accepted. Handshake completes
  before either side accepts
  application-level messages; a mismatched or malformed handshake frame
  is rejected, not silently ignored.
- **QR pairing**: daemon persists an X25519 keypair on disk (mode
  0600) across restarts (ADR-0007 (g)); generates a pairing offer
  (daemon ID, public key, relay endpoint) encoded into a URL fragment,
  never sent to any server; renders that URL as a QR code. Mobile scans
  it, generates its own ephemeral keypair for the session, and
  completes the handshake through the relay.
- **Reconnect grace**: relay buffers a bounded number of frames
  (starting point: paseo's 200-frame cap, ADR-0007 (a)) per connection
  when the peer side is briefly disconnected, and flushes them on
  reconnect instead of dropping traffic outright. Buffer is bounded —
  a permanently-offline peer does not grow relay memory unboundedly;
  oldest frames are evicted once the cap is hit.
- **Key rotation**: rotating keys means starting a new session (fresh
  handshake, fresh ephemeral keys) — no live/in-session rekey required
  for v1 (ADR-0007 (e)). A second handshake attempt with a different
  key on an already-established session is rejected as a protocol
  violation, not silently accepted.
- **Replay protection**: per-direction monotonic message counter
  (or equivalent) rejects replayed/out-of-order-beyond-window frames
  within a live session (ADR-0007 (d) — an explicit divergence from
  paseo, which ships without this).
- **Relay admission (daemon↔relay auth, ADR-0011)**: independent of the
  E2EE handshake — a workspace has a random 256-bit secret; the relay
  persists only its hash. The daemon authenticates via a nonce-based
  HMAC challenge-response over the transcript (protocol version,
  workspace ID, both nonces, daemon key ID); the raw secret is never
  sent after pairing. TLS uses a relay-generated self-signed
  certificate; the daemon pins its fingerprint at pairing time. A
  connection is bound to exactly one workspace ID; a frame whose
  workspace/session ID doesn't match the authenticated binding is
  rejected. E2EE handshake success is never treated as relay
  authorization, and vice versa.
- Out of scope this pass: the Expo mobile app itself; multi-tenant
  relay deployment/scaling; the actual `relay.spacingmind.sh` deploy
  (Cloudflare TLS) — self-hostability is the acceptance bar, not a live
  deployment; the optional Ed25519 daemon-identity upgrade path from
  ADR-0011 (v1 ships the HMAC challenge-response only); mobile↔relay
  transport framing beyond what's needed to prove the handshake and
  forwarding work (ADR-0007 (f) leaves this open).

## Test Scenarios

- Go: X25519 keypair generation, persistence, and reload — daemon
  keypair file survives a restart, round-trips correctly, and is
  created with 0600 permissions.
- Go: pairing offer encoding — offer fields serialize into the URL
  fragment form and decode back to the same struct; fragment (not
  query string) is asserted so the offer never appears in anything a
  server could log.
- Go: E2EE handshake happy path (in-process, two ends of a fake relay
  pipe) — both sides derive the same shared key, application messages
  encrypt/decrypt correctly end-to-end.
- Go: handshake rejects a malformed/truncated hello frame without
  crashing or hanging.
- Go: a second handshake attempt with a different key on an
  already-established session is rejected (mirrors paseo's re-hello
  key-mismatch close behavior) — connection is closed, not silently
  re-keyed.
- Go: replay protection — a captured/replayed ciphertext frame (same
  session, reused counter/nonce) is rejected; a live, in-order frame
  with the next expected counter is accepted.
- Go: relay forward — with both daemon and mobile connections open,
  a frame sent by one side arrives byte-identical (still ciphertext,
  relay never decrypts) at the other within the test's timeout.
- Go: reconnect buffer — mobile side disconnects, daemon sends N
  frames (N < cap), mobile reconnects and receives all N in order;
  a test past the cap confirms oldest frames are evicted, not that the
  relay grows unbounded or crashes.
- Go: relay never has access to the shared session key or plaintext —
  test asserts the relay-side connection object holds no derivable
  plaintext and that a relay-side decrypt attempt (using only what the
  relay legitimately has) fails.
- Go: `smind relay` subcommand starts, binds its listen address, and
  shuts down cleanly (context cancellation / signal), matching the
  daemon's existing subcommand lifecycle conventions.
- Go: relay admission — a daemon connection presenting a valid
  workspace-secret HMAC over the challenge transcript is admitted and
  bound to that workspace; a wrong secret, a replayed transcript
  (reused nonce), or a workspace-ID mismatch on a subsequent frame is
  rejected. A connection admitted for workspace A cannot forward or
  receive frames tagged for workspace B.
- Go: relay admission is independent of the E2EE handshake — a test
  double that completes relay admission but sends a malformed/no E2EE
  hello is still rejected at the E2EE layer (and vice versa: a valid
  E2EE hello over a connection that never completed relay admission is
  never forwarded).
- Integration: real daemon process + real relay process (both
  in-process test harnesses or subprocesses) — full pairing flow from
  QR-offer generation through handshake completion through a forwarded
  application message, then a simulated mobile disconnect/reconnect
  proving the buffered frames are delivered.
- Integration: two mobile devices paired to the same daemon (fanout,
  ADR-0007 (b)) both receive a daemon-originated broadcast/event
  correctly, and a message from one device does not leak to the other
  unless that's the intended semantic (define and assert whichever
  the control/data-socket design in ADR-0007 (b) actually specifies).

## Decisions

**Unblocked.** ADR-0007 (Accepted 2026-09-11) and ADR-0011 (Accepted
2026-09-17) together cover every crypto, state-model, fanout, replay,
rotation, transport, QR-pairing, and admission-auth choice this plan's
Acceptance Criteria depend on. Implementation may proceed.

- Repo split/license (ADR-0007 (h)) is explicitly out of scope for this
  plan (see `docs/decisions/0003-agpl-license-no-repo-split.md`) — this
  plan builds relay code in this repo regardless of any future split
  decision.
- gRPC service/message definition (ADR-0007 (f)) and the control/data
  socket mapping (ADR-0007 (b)) have no `.proto` yet — that concrete
  design is this plan's own work, not a blocked-on-ADR item.

## Progress

- [x] ADR-0007 approved (2026-09-11, user: ChaCha20-Poly1305; rest as
      recommended)
- [x] ADR-0011 approved (2026-09-17, relay admission auth)
- [x] Daemon X25519 keypair generation + persistence
      (`internal/relay/e2ee/keypair.go`; 0600, survives restart, corrupt
      file regenerates)
- [x] Pairing offer encoding (URL fragment) + QR rendering
      (`internal/relay/pairing/`; `Offer.URL` puts the payload in the
      fragment only, `Offer.QRCode`/`QRPNG`/`QRText` render it with an
      in-tree pure-stdlib QR encoder)
- [x] E2EE handshake (daemon + mobile sides)
      (`internal/relay/e2ee/handshake.go`; X25519 + HKDF, malformed and
      truncated hellos rejected, no hang on a silent peer)
- [x] Replay protection (per-direction counter)
      (`internal/relay/e2ee/session.go`; replayed and
      out-of-order-beyond-window frames rejected, counter exhaustion
      handled)
- [x] Key rotation behavior (new-session-only; reject re-hello with
      different key on live session)
- [x] daemon↔relay gRPC service/message definition (ADR-0007 (f))
      (`internal/relay/relaypb/relay.proto`; control/data split mapped as
      `OpenControl` (one bidi stream per daemon, lifecycle only) +
      `OpenData` (one bidi stream per device, Frame envelopes with opaque
      ciphertext), admission as unary `AdmitChallenge`/`Admit`;
      generated Go committed alongside; reasoning documented in the
      .proto header comment)
- [x] Relay: admission auth — workspace secret + HMAC challenge-response
      (`internal/relay/admission/`; SHA-256 of the 256-bit secret is both
      the stored form and the HMAC key so the relay verifies possession
      without ever holding the raw secret; single-use expiring server
      nonces; constant-time compare; one generic `ErrRejected` for every
      cause so unknown-workspace/wrong-secret/reused-nonce are
      indistinguishable. TLS cert pinning deferred to the transport step,
      per ADR-0011's layering)
- [x] Relay: dumb-pipe forwarding (daemon ↔ mobile, ciphertext only)
      (`internal/relay/server/`; bufconn-tested: byte-identical both
      directions, admission gating on data+control, wrong-workspace and
      direction violations rejected, never-has-plaintext assertions)
- [x] Relay: reconnect-grace buffer (bounded per side, default cap 200;
      in-order flush on reconnect, oldest evicted past cap; per-route
      queues persist across disconnects)
- [x] Relay: multi-device fanout (control + data socket shape per
      ADR-0007 (b)) — per-(workspace, session, device) routes, each
      device with its own OpenData stream and reconnect buffer; daemon
      "broadcast" = separately-encrypted per-device sends; no
      device-to-device forwarding (conservative reading; also the only
      coherent one given per-session keys). Documented in server.go.
- [x] `smind relay` subcommand (self-hostable, standalone lifecycle)
      (`cmd/smind/relay.go` + `internal/relay/server/run.go`: real TLS
      gRPC listener with serve-style signal/cancel lifecycle; self-signed
      cert generated once and persisted under $SMIND_HOME/relay with
      fingerprint printed at startup; workspace enrollment via
      `smind relay workspace new/ls`, raw secret printed exactly once)
- [x] Tests (unit: keypair/offer/handshake/replay/rotation/buffer/
      admission; integration: daemon+relay end-to-end, multi-device
      fanout) — unit coverage accumulated with each step above;
      integration scenarios now in `internal/relay/client` against a
      real TLS gRPC relay (server.Run): full pairing flow (offer URL
      round-trip incl. fingerprint pin -> admission -> E2EE handshake
      -> forwarded message both directions), mobile disconnect/
      reconnect (relay buffers, new-session reconnect flows), and
      two-device fanout (same event, separately encrypted per session)
- [ ] ROADMAP update
- [ ] Verification

## Validation

- 2026-09-17 — gRPC wire contract step: `go build ./...`, `go vet ./...`,
  `go test ./internal/relay/...` (incl. new `relaypb` marshal/unmarshal
  round-trip tests), and `gofmt -l $(git ls-files '*.go')` all clean after
  committing the generated files (verified gofmt-clean directly too, since
  the lint step only sees tracked files).
- 2026-09-17 — admission auth step: `internal/relay/admission` tests cover
  valid-admission-binds-workspace, wrong-secret, replayed
  transcript/reused nonce, expired nonce, cross-workspace transcript
  mismatch, unknown workspace, uniform rejection shape (no oracle), HMAC
  canonical-form ambiguity checks, and challenge input validation.
  `go build ./...`, `go vet ./...`, `go test ./internal/relay/...`,
  gofmt all clean.
- 2026-09-17 — fanout step: two devices per workspace both receive
  daemon-originated sends; a device's frames never reach the other device
  or the other route's daemon stream; reconnect buffers are independent
  per (workspace, session, device). All relay tests green under -race.
- 2026-09-17 — subcommand step: `smind relay` binds a real TLS gRPC
  listener, serves an enrolled workspace's admission exchange to a
  cert-pinning client, and exits cleanly on cancellation (lifecycle test,
  3 consecutive green runs); cert persistence + enrollment CLI verified.
  Full `go test ./...` green.
- 2026-09-17 — resume amendment (ADR-0007 (e), 2026-09-17): transport
  reconnect now resumes the same session (key + counters kept,
  `DataConn.Resume`) so relay-buffered frames decrypt — verified by the
  reconnect integration test (3 buffered frames decrypt in order, live
  traffic continues both directions). New-session Handshake remains for
  pairing/re-pair/compromise/lost state. Relay server needed no change:
  re-presenting the same session id already re-attaches to the existing
  route and its buffers.
- 2026-09-17 — client step: `internal/relay/client` (Dial with
  fingerprint pinning, Admit, OpenControl, OpenData wrapping e2ee.Channel
  over framed ciphertext) + the two integration scenarios over a real
  in-process TLS relay. server.Config gained an optional pre-bound
  Listener for race-free ephemeral-port tests. Full suite green
  (client tests x2 runs).
