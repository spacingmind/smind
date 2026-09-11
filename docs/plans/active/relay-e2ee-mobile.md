# Relay: E2EE relay server + mobile pairing (Phase 3)

Builds `docs/ROADMAP.md` Phase 3's relay half: a self-hostable,
dumb-pipe relay server (`smind relay`) that lets a paired mobile device
reach a daemon it can't otherwise route to directly, without the relay
ever seeing plaintext. Based on `docs/research/relay-design-status.md`
(read-only audit: zero relay code exists today) and
`docs/decisions/0007-relay-architecture.md` (draft ADR covering the
architectural choices this plan depends on).

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
  X25519 key agreement plus an AEAD per ADR-0007 decision (c) — pending
  that decision, implementation must not start on the handshake's
  crypto step. Handshake completes before either side accepts
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
- Out of scope this pass: the Expo mobile app itself; multi-tenant
  relay deployment/scaling; the actual `relay.spacingmind.sh` deploy
  (Cloudflare TLS) — self-hostability is the acceptance bar, not a live
  deployment; auth between daemon and relay beyond what the E2EE
  handshake and connection identity require (no separate API-key layer
  unless ADR-0007 or a follow-up decision adds one); mobile↔relay
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

**Draft — chờ user duyệt ADR-0007, đặc biệt mục crypto (X25519 +
ChaCha20-Poly1305 vs XSalsa20-Poly1305/NaCl `box`).** No implementation
in this plan should begin until `docs/decisions/0007-relay-architecture.md`
moves from `DRAFT` to `Accepted`. In particular:

- The handshake's AEAD choice (ADR-0007 (c)) has no default and blocks
  any crypto code from being written at all, including tests that
  assert specific ciphertext framing.
- The relay state model (a), fanout shape (b), replay protection (d),
  key rotation model (e), gRPC service/message design (f), and QR
  pairing format (g) all carry a recommendation in ADR-0007 but are
  still formally unapproved — this plan's Acceptance Criteria already
  assume those recommendations will be accepted as written; if the
  user amends any of them, this plan's Acceptance Criteria and Test
  Scenarios need a corresponding update before implementation starts
  against the changed decision.
- Repo split/license (h) is explicitly out of scope for this plan (see
  ADR-0007 (h) and `docs/decisions/0003-agpl-license-no-repo-split.md`)
  — this plan builds relay code in this repo regardless of any future
  split decision.

## Progress

- [ ] ADR-0007 approved (blocking — nothing below starts until this is
      checked)
- [ ] Daemon X25519 keypair generation + persistence
- [ ] Pairing offer encoding (URL fragment) + QR rendering
- [ ] E2EE handshake (daemon + mobile sides)
- [ ] Replay protection (per-direction counter)
- [ ] Key rotation behavior (new-session-only; reject re-hello with
      different key on live session)
- [ ] Relay: dumb-pipe forwarding (daemon ↔ mobile, ciphertext only)
- [ ] Relay: reconnect-grace buffer (bounded, per-connection)
- [ ] Relay: multi-device fanout (control + data socket shape per
      ADR-0007 (b))
- [ ] `smind relay` subcommand (self-hostable, standalone lifecycle)
- [ ] daemon↔relay gRPC service/message definition (ADR-0007 (f))
- [ ] Tests (unit: keypair/offer/handshake/replay/rotation/buffer;
      integration: daemon+relay end-to-end, multi-device fanout)
- [ ] ROADMAP update
- [ ] Verification

## Validation

(empty — fill in as work completes)
