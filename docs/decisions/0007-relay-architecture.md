# 0007: Relay architecture (E2EE relay + mobile pairing, Phase 3)

## Status

**DRAFT — pending user approval.** Per `AGENTS.md` rule (d) ("Material
ambiguity"), every choice below affects architecture (state model,
protocol, public API shape, crypto) and none is yet decided in
`docs/decisions/`. This document presents each choice for the user to
approve, reject, or amend — it is not itself a decision, and no
implementation should start against it until Status changes to
`Accepted`. Item (c) in particular has no safe default and must be
explicitly resolved by the user before any handshake code is written.

## Context

`docs/ROADMAP.md` Phase 3 ("Relay E2EE + Mobile") is the only place
relay is designed as more than a passing mention (confirmed by
`docs/research/relay-design-status.md`, a read-only audit of every
"relay" reference in this repo plus `refs/paseo` and `refs/cliproxyapi`).
Verbatim from the roadmap:

- Relay server (Go, dumb pipe), self-hostable as `smind relay`
- E2EE handshake: X25519 + ChaCha20-Poly1305, QR pairing
- Reconnect grace, correct key rotation
- Mobile app (Expo + `@expo/ui`): pairing + workspace/task list, realtime
  agent timeline + follow-up, push notifications, mobile permission
  approval
- Deploy relay at `relay.spacingmind.sh` (Cloudflare TLS)

Transport note already pre-decided in the roadmap (Phase 2, restated in
Phase 3): daemon↔relay uses gRPC, "a real service boundary," distinct
from the browser-facing `internal/wsapi` RPC-over-WebSocket API from
Phase 2 (gRPC has no native browser support).

As of this audit: **zero relay code exists** — no `cmd/relay`, no
`smind relay` subcommand, no `.proto` files, no QR/X25519/ChaCha20-
Poly1305 dependency in `go.mod`. Everything below is greenfield.

The closest available precedent is `refs/paseo` (read-only reference,
per `AGENTS.md`'s `refs/` map), which has shipped, tested, and
documented (`SECURITY.md`) an E2EE relay for the same daemon↔mobile
problem shape. The user has approved the general principle for this
draft: **default to following paseo's direction unless there's a clear
best-practice reason to diverge**, called out per decision below.

## Decisions

### (a) Relay state model — store-and-forward vs. pure forward

**Recommendation: store-and-forward, bounded per-connection buffer**
(follow paseo).

Paseo's Cloudflare Durable Object adapter
(`refs/paseo/packages/relay/src/cloudflare-adapter.ts:256-257`) buffers
up to 200 frames per side when the peer isn't currently connected, and
flushes them on reconnect, rather than dropping traffic the instant one
side is briefly offline.

**Alternative considered:** pure forward-only relay — no relay-side
state, forward bytes only when both sockets are open simultaneously,
drop otherwise. Simpler (no buffer, no memory bound to reason about),
but it directly conflicts with the roadmap's own explicit "Reconnect
grace" requirement: a phone moving from WiFi to cellular, or a daemon
process restart, would silently lose any message sent during that gap
with no relay-side recourse.

**Rationale:** "Reconnect grace" as written in the roadmap only makes
sense if the relay holds something across the gap; a pure forwarder has
nothing to make "graceful" about a reconnect — the client would simply
resync from scratch every time, which is a different (and already
roadmap-named "chỉ-ý-tưởng") feature. Bound size (paseo uses 200) is a
starting point to validate under load, not treated as load-bearing here.

### (b) Multi-device fanout — control + data socket split

**Recommendation: adopt paseo's protocol v2 shape** — one persistent
control socket per daemon↔relay session, plus one data socket per
paired mobile device, rather than a single shared pipe.

**Alternative considered:** paseo's own protocol v1 (single
server-client socket pair, no fanout) — rejected because
`docs/research/codmon-chat-server-notes.md` already flagged multi-device
push fan-out as a known, deliberately deferred question ("Reject for
now, revisit at Phase 3"); Phase 3 is now. A single pair can't serve
more than one paired phone without ad hoc multiplexing bolted on later.

**Rationale:** paseo already solved this exact problem (v1 → v2
migration) and the v2 shape (control socket owns session/key
lifecycle, data sockets carry per-device application traffic) maps
cleanly onto smind's daemon↔relay gRPC transport as separate streams
rather than requiring a new multiplexing scheme designed from scratch.

### (c) Crypto primitive — X25519 + ChaCha20-Poly1305 vs. X25519 + XSalsa20-Poly1305 (NaCl `box`)

**This is the one item in this ADR with no recommended default — it
requires an explicit user decision, not a rubber-stamp.** The roadmap
already names ChaCha20-Poly1305; paseo's shipped implementation uses
XSalsa20-Poly1305 via NaCl `box`. These are two different, non-
interoperable AEAD choices for the same handshake role, and picking
one silently (in either direction) would either overrule an existing
written roadmap commitment or ignore the repo's own "follow paseo
unless there's a reason" default — so it's surfaced here instead.

**Option 1 — ChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305`),
as already written in `docs/ROADMAP.md` Phase 3:**
- IETF-standard AEAD construction (RFC 8439), the same primitive used
  by TLS 1.3 and WireGuard — broadly reviewed, familiar to anyone
  auditing the code later.
- 12-byte nonce. Safe only if nonces are never reused under the same
  key — in practice this means a counter-based nonce scheme (not
  random generation, which risks collision at scale with a 96-bit
  space), which also gives replay-detection a natural home (see
  decision (d)) since a counter is already being tracked.
- Ships in `golang.org/x/crypto`, already present in `go.sum` (pulled
  in transitively via `internal/accounts`'s use of `utls` for OAuth
  TLS fingerprinting) — but only as an *indirect* dependency today, and
  the `chacha20poly1305` subpackage itself is not imported anywhere
  yet. Adopting it makes the dependency direct and adds the first real
  use of it, but doesn't add a new module to `go.mod`.
- Matches the roadmap's existing, already-written pre-decision — no
  need to reopen or justify a change from prior planning.

**Option 2 — XSalsa20-Poly1305 / NaCl `box` (`golang.org/x/crypto/nacl/box`),
matching `refs/paseo`:**
- Same module (`golang.org/x/crypto`), equally available today with no
  new dependency either.
- 24-byte random nonce — large enough that random generation alone
  (no counter needed) is safe against collision, which is exactly how
  paseo uses it (`refs/paseo/packages/relay/src/crypto.ts:156-165`:
  fresh `nacl.randomBytes(24)` per message, bundle format
  `[nonce][ciphertext]`).
- `box.Seal`/`box.Open`-style API bundles key agreement and AEAD into
  one call, slightly less surface for a handshake implementer to get
  wrong than composing X25519 + a separate AEAD by hand.
- Directly mirrors paseo's shipped, threat-modeled implementation
  (`refs/paseo/SECURITY.md`) — matches the "follow paseo unless a clear
  reason to diverge" default this ADR otherwise applies everywhere
  else, and paseo's wire framing (`[24-byte nonce][ciphertext]`) could
  be adopted verbatim rather than redesigned.
- Diverges from the roadmap's own already-written text, which would
  need to be edited and the divergence justified beyond "matches
  paseo" alone — random nonce reuse is not a live problem being
  avoided (24-byte nonces are already collision-safe at random), so
  there's no correctness argument forcing the change, only a
  consistency-with-precedent one.

**Ask:** confirm ChaCha20-Poly1305 (keep the roadmap as written) or
switch to XSalsa20-Poly1305/NaCl `box` (match paseo) — and if switching,
approve updating `docs/ROADMAP.md` Phase 3's wording to match.

### (d) Replay protection — add nonce/counter tracking from the start

**Recommendation: yes, build in replay protection (monotonic
per-direction message counter, rejecting/closing on reuse or
out-of-window sequence numbers) from the first handshake implementation** —
diverge from paseo here.

**Alternative considered:** ship without it, matching paseo exactly.
Paseo's own `SECURITY.md` states this outright: "Within a live session,
replay protection is not yet implemented; the protocol uses random
nonces and does not track nonce reuse or message counters"
(`refs/paseo/SECURITY.md:39`). Simpler v1, and it's a documented,
accepted gap in a shipped product — not a hypothetical risk.

**Rationale:** this is exactly the kind of known gap the research audit
flagged as something smind should decide about deliberately rather than
inherit by default (`docs/research/relay-design-status.md` §4). It's
also cheaper to build in now than to retrofit: a dumb-pipe relay with a
store-and-forward buffer (decision (a)) means a captured/replayed frame
has a wider window to be re-injected than in paseo's live-only-buffer
case, which raises the cost of the gap relative to paseo's version of
the same design. A per-direction counter is a small addition once a
handshake/session-key exchange already exists, regardless of which AEAD
(c) resolves to.

### (e) Key rotation — new session = new key, no live rekey (v1)

**Recommendation: match paseo — a "rotated" key means starting a new
session (fresh ephemeral keypair, fresh handshake), not rekeying an
open connection.**

Paseo's encrypted-channel state machine treats a second `e2ee_hello`
with a *different* key arriving on an already-established channel as an
attack and closes the socket with code 1008
(`refs/paseo/packages/relay/src/encrypted-channel.ts:120`, comment at
lines 45-50). It does allow a duplicate `e2ee_hello` with the *same*
key to be a harmless retry (peer didn't observe the `e2ee_ready` yet).
There is no in-session rekey mechanism.

**Alternative considered:** a live rekey protocol (mid-session key
update, e.g. TLS 1.3 `KeyUpdate` or a ratchet). Rejected for v1: no
concrete threat in this repo's threat model currently demands it, it's
meaningfully more state machine complexity, and the roadmap's phrase
"correct key rotation" is satisfiable by "no session ever reuses a
previous session's key material" without inventing in-session rotation.

**Rationale — limitation to document explicitly, not paper over:** this
means rotating credentials (e.g. suspected compromise) requires
tearing down and re-pairing, not a hot swap. That's an acceptable v1
limitation given no current requirement forces otherwise, but it should
be written down as a known limitation in the relay's docs when built,
not discovered later.

### (f) Transport — gRPC for daemon↔relay (carrying forward the roadmap's pre-decision)

**Recommendation: confirm gRPC daemon↔relay as already written in
`docs/ROADMAP.md`** (Phase 2 note, restated Phase 3) — this one is not
newly opened by this ADR, it's an existing written pre-decision being
carried forward, not re-litigated, per the roadmap's own framing ("Not
implemented yet; noted here so Phase 3 design starts from this rather
than re-litigating it").

What's still genuinely undecided and *is* in scope here: there is no
`.proto`/service definition yet, and control-vs-data-socket split (b)
needs to map onto concrete gRPC streams (e.g. one bidi-streaming RPC
for the control channel, one per data channel, or one multiplexed
bidi-stream carrying a tagged envelope for both). That message/service
design is left to the implementation plan
(`docs/plans/active/relay-e2ee-mobile.md`), not fixed here.

Out of scope for this item: the mobile↔relay wire protocol is *not*
specified anywhere in the roadmap (only daemon↔relay is called out as
gRPC). Paseo uses plain WebSocket end-to-end, including phone↔relay,
specifically because gRPC has no native browser/React-Native-friendly
support without a grpc-web proxy layer — the same reasoning the roadmap
already uses to justify WS-RPC for the Phase 2 browser-facing API. This
ADR does not resolve mobile↔relay transport; flagging it so the
implementation plan doesn't silently assume gRPC applies there too.

### (g) QR pairing — persistent daemon keypair + URL-fragment offer (follow paseo)

**Recommendation: adopt paseo's pairing pattern.**

- Daemon generates and persists an X25519 keypair once, on disk under
  smind's home directory (mode 0600), reused across restarts — mirrors
  `refs/paseo/packages/server/src/server/daemon-keypair.ts` (`daemon-keypair.json`,
  `{v, publicKeyB64, secretKeyB64}`, regenerated only if the file is
  missing or fails to parse).
- Pairing offer is a small object — server/daemon ID, daemon public
  key, relay endpoint — JSON-encoded and base64url'd into a URL
  **fragment** (`#offer=...`), never a query parameter and never sent
  to any server (`refs/paseo/packages/server/src/server/connection-offer.ts:43-50`,
  `pairing-offer.ts`). The QR code encodes that URL; scanning it opens
  smind's mobile app directly to the pairing screen with the offer
  already in hand.
- Mobile side generates a fresh ephemeral X25519 keypair per pairing
  session (not persisted long-term the way the daemon's is).

**Alternative considered:** encode the raw public key/relay address
directly in the QR with no URL/app-scheme wrapper. Simpler payload, but
loses the deep-link entry point that lets a scan open straight into the
mobile app's pairing screen, and paseo's specific choice to put the
offer in a URL *fragment* rather than a query string is a deliberate
privacy property (fragments never leave the client — no server, CDN,
or access log ever sees them) that a bare-QR approach would need to
reinvent some other way.

**Rationale:** this is a fully-baked, working pattern with no
identified downside for smind's equivalent use case (daemon and mobile
app, same pairing problem) — a clean case for "follow paseo" with no
best-practice reason to diverge.

### (h) Repo split + license — cross-reference to ADR-0003, not re-decided here

**Recommendation: do not decide this here.** `docs/decisions/0003-agpl-license-no-repo-split.md`
already addresses this directly and explicitly defers it:

> A future `smind-relay` (Phase 3, not yet built) is expected to land on
> the permissive side of that same split when it exists [...] Noted
> here as the expected default for that future repo, not decided as a
> live thing today.

This ADR does not override or formalize that expectation. When relay
implementation is far enough along that a repo split becomes a live,
concrete choice (not a forward-looking note), that should be resolved
by either an update to 0003 or a new ADR at that time — not folded
into this one as a side effect of building the relay's protocol.

## Cross-references

- `docs/ROADMAP.md` Phase 3 — the source requirements this ADR responds
  to.
- `docs/decisions/0003-agpl-license-no-repo-split.md` — repo
  split/license expectation for a future relay repo (see (h)).
- `docs/research/relay-design-status.md` — the audit this ADR is based
  on (docs/code inventory, refs/paseo and refs/cliproxyapi comparison).
- `refs/paseo` (read-only reference) — `packages/relay/src/crypto.ts`,
  `packages/relay/src/encrypted-channel.ts`,
  `packages/relay/src/cloudflare-adapter.ts`,
  `packages/server/src/server/{daemon-keypair,connection-offer,pairing-offer,relay-transport}.ts`,
  `SECURITY.md`.
- `docs/plans/active/relay-e2ee-mobile.md` — implementation plan,
  currently blocked on this ADR's approval.

## Decisions requiring explicit user approval before implementation starts

1. **(c) Crypto primitive — ChaCha20-Poly1305 vs. XSalsa20-Poly1305/NaCl
   `box`.** No default; must be explicitly chosen.
2. All other items (a, b, d, e, f, g, h) carry a recommendation each,
   but per AGENTS.md rule (d) this entire document is a DRAFT — the
   user should confirm or amend each before Status moves to `Accepted`
   and implementation begins.
