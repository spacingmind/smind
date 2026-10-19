# 0035: Relay E2EE v2: a fresh daemon ephemeral key in every session

## Status

**Accepted — 2026-10-20.** The Human decided to remove the nonce-reuse residual recorded in ADR-0031 D9 by
changing the E2EE core, and accepted all four [open choices](#open-choices) as recommended. The Human also stated that
there are no users yet, so breaking changes are fine: no compatibility shims anywhere. Plan: [`docs/plans/active/relay-e2ee-v2.md`](../plans/active/relay-e2ee-v2.md).
Amends ADR-0007 (c)/(e) (key schedule, handshake) and ADR-0031 D9 (device_hello signed bytes, "E2EE core unchanged").

## Context

Today (E2EE v1, `internal/relay/e2ee`) both ends send one `hello` carrying their X25519 public key, derive keys, then
exchange an **empty** `ready`. The daemon's key is its persisted *static* key; the client's is a per-session *ephemeral*.
`keys = HKDF(X25519(c_eph, d_static), salt = SHA-256(label ‖ d_static ‖ c_eph))` (`session.go`). The daemon contributes nothing
fresh, which has three consequences:

1. **Replay re-derives the same keys (ADR-0031 D9 residual).** Anyone who can open a relay route (the relay operator, a
   host-secret holder) can replay a captured client hello into a new route. The daemon derives the identical keys and
   encrypts its first frames (the challenge, a refusal) with the same (key, nonce) pairs as the original session.
   ChaCha20-Poly1305 nonce reuse leaks the XOR of the two plaintexts and the Poly1305 one-time key at those counters.
2. **No forward secrecy against the daemon key.** A later theft of the daemon's static key (`$SMIND_HOME`, a backup)
   decrypts every recorded session, past ones included.
3. **No key confirmation.** `ready` is empty, so a handshake "succeeds" with a wrong or tampered key and only fails at
   the first data frame. A client that reuses its ephemeral key across sessions (the pre-P13a Rust loop did) repeats keys
   and nonces on *its* side too.

`device_hello` v2 (ADR-0031 D9, #354, **unreleased**: it is on `develop`, and release PR #352 for 0.13.0 is open) works
around (1) at the application layer with a daemon challenge. It leaves the nonce reuse in place and the E2EE core
deliberately untouched (`TestE2EECoreUnchanged`). 0.12.0 clients cannot connect to a hello-v2 daemon anyway.

Reference points: **Noise `NK`** (`← s` pre-message; `→ e, es`; `← e, ee`) is exactly the shape we need: the initiator is
anonymous, the responder is authenticated by a key the initiator already knows (the pairing offer), both contribute an
ephemeral. libsignal's X3DH/PQXDH likewise mixes `DH(ephemeral, signed prekey)` with further ephemeral DHs, never a single
static-only DH. `refs/paseo` (`SECURITY.md`, `packages/relay/src/crypto.ts`) uses NaCl `box` with an ephemeral phone key and
the daemon's static key — the same construction as our v1, with random 24-byte nonces so it does not share the nonce-reuse
property, but it has no daemon ephemeral and no replay protection within a session; there is nothing there to port. We
follow Noise NK's structure.

## Decision

### 1. Handshake: Noise-NK-shaped, hand-rolled on the primitives we already ship

No Noise library: the three clients (Go, Rust, TypeScript under Hermes) already carry X25519, HKDF-SHA-256, HMAC-SHA-256
and ChaCha20-Poly1305, and a library per language would add three new dependencies, a different wire shape (handshake
payloads, Noise nonces) and nothing the construction below lacks. The structure is NK: two ephemeral–static/ephemeral
DHs, a running transcript, key confirmation.

Roles are unchanged: **daemon = responder** (static key `d`, role byte `1`), **every client = initiator** (desktop app,
mobile app, test clients; role byte `2`). The frame layer is unchanged: `u32BE(1+len(payload)) ‖ type ‖ payload`,
types `hello 0x01`, `ready 0x02`, `data 0x03`, at most 1 MiB. Version byte: **`0x02`** (`ProtocolVersion = 2`).

```
client                                                         daemon
  c_eph ← fresh X25519 key
  M1 hello  ──────────────────────────────────────────────────►  validate M1
                                                                 d_eph ← fresh X25519 key
                                                                 derive keys
  ◄──────────────────────────────────────────  M2 hello, R_d ready
  pin check, derive keys, verify R_d
  R_c ready ──────────────────────────────────────────────────►  verify R_c → established
  (established; may send data after R_c)                         (first data frame only now)
```

Wire layouts (all lengths are exact; anything else is `ErrProtocol`):

| Message | Frame type | Payload |
|---|---|---|
| **M1** client hello | `0x01` | `0x02 ‖ 0x02 ‖ client_eph_pub[32]` = 34 bytes |
| **M2** daemon hello | `0x01` | `0x02 ‖ 0x01 ‖ daemon_pub[32] ‖ daemon_eph_pub[32]` = 66 bytes |
| **R_d** daemon ready | `0x02` | `HMAC-SHA256(confirm_d, th)` = 32 bytes |
| **R_c** client ready | `0x02` | `HMAC-SHA256(confirm_m, th)` = 32 bytes |
| data | `0x03` | `u64BE(counter) ‖ ChaCha20-Poly1305(k_dir, nonce(counter), plaintext)` — **unchanged** |

M1 has the v1 layout apart from the version byte. An empty `ready` (the v1 shape) is a protocol error.

### 2. Key schedule

```
LABEL = "smind relay e2ee v2"                      (ASCII, 19 bytes)
th    = SHA-256( LABEL ‖ daemon_pub ‖ daemon_eph_pub ‖ client_eph_pub )      (daemon static, daemon eph, client eph)
es    = X25519(client_eph_priv, daemon_pub)        = X25519(daemon_priv, client_eph_pub)
ee    = X25519(client_eph_priv, daemon_eph_pub)    = X25519(daemon_eph_priv, client_eph_pub)
ikm   = es ‖ ee                                     (64 bytes)
k_d2m     = HKDF-SHA256(ikm, salt = th, info = LABEL ‖ " daemon->mobile", 32)
k_m2d     = HKDF-SHA256(ikm, salt = th, info = LABEL ‖ " mobile->daemon", 32)
confirm_d = HKDF-SHA256(ikm, salt = th, info = LABEL ‖ " confirm daemon", 32)
confirm_m = HKDF-SHA256(ikm, salt = th, info = LABEL ‖ " confirm mobile", 32)
```

`HKDF-SHA256(ikm, salt, info, 32)` is RFC 5869 extract-then-expand (Go `hkdf.Key`, as in v1). Nonces, counters, replay checks
and `Seal`/`Open` are unchanged (4 zero bytes ‖ `u64BE(counter)`; one key per direction).

- **Both** DH outputs must be checked: an all-zero output (a low-order peer point) aborts the handshake (RFC 7748 §6.1).
  Go's `crypto/ecdh` already errors; Rust and TypeScript must check explicitly and have a negative vector.
- `th` covers the label (so the version), the daemon's identity and both ephemerals. The version byte is not hashed
  separately: it is fixed by the label, and a flipped version byte changes the schedule so `R_d`/`R_c` fail.
- The transcript is **public**; the Go/Rust/TS `Channel` expose it once established (`TranscriptHash()`), and
  `device_hello` signs exactly that value (§5). Nothing else recomputes it.
- Zeroise `c_eph_priv`, `d_eph_priv`, `es`, `ee` and the confirmation keys once the session is derived (best effort in Go
  and TypeScript, `zeroize` in Rust).

### 3. Why this removes the residual

Each session's keys depend on `ee`, which needs a private ephemeral that only exists for that one handshake on each side.

- **Replayed client handshake.** The daemon draws a fresh `d_eph` per `Handshake` call, so `th`, `ee` and all four keys differ
  from the original session (vector: same client ephemeral, two daemon ephemerals, two disjoint key sets). Better still, the
  replaying party does not know `c_eph_priv`, cannot compute `ee`, cannot produce `R_c`, and the daemon never sends a
  `data` frame: it only ever emitted M2 and `R_d` (a MAC under a key the attacker cannot derive).
- **Daemon freshness protects the daemon's sends; client freshness protects the client's.** A client that reused its
  ephemeral and met a replayed old M2/R_d would reuse keys on its own sends. The API therefore **never lets a caller pass the
  initiator's key**: the client `Channel` generates `c_eph` inside `Handshake`, and a retry after a failed handshake gets
  a new one (a test-only seed hook exists for vectors, in-package/`cfg(test)` only).
- **Forward secrecy.** A stolen daemon static key no longer decrypts recorded sessions (they also need an ephemeral).
- **Key confirmation.** A tampered `daemon_eph_pub`, a wrong static key or a wrong pin fails inside `Handshake`
  (`ErrHandshakeConfirm` / `ErrPeerKeyMismatch`), not at the first data frame.
- **Daemon authentication** is unchanged in kind: the client pins `daemon_pub` from the pairing offer
  (`WithExpectedPeerKey`, `ErrPeerKeyMismatch`, checked on M2 *before* any DH) and only a holder of the daemon's static
  private key can produce a valid `R_d`. An unpinned client (tests only) authenticates nobody, as today.

Not claimed: the construction does not hide the client's identity from the relay (device ids are plaintext frame
metadata, ADR-0031), and a party holding **both** the daemon static key and a live client ephemeral is the session.

### 4. Retries, duplicates, resume

Behaviour stays as ADR-0007 (e): rotation means a new session, never an in-place rekey.

- Daemon, after M1 and at any later time: a byte-identical repeat of M1 re-sends the **stored** M2 and R_d (same `d_eph`;
  no new keys, no AEAD frame involved). A different M1 → `ErrKeyRotation`, channel closed.
- Client: a byte-identical repeat of M2 or R_d is ignored. A different M2 → `ErrKeyRotation`.
- Before the handshake completes, any `data` frame is `ErrProtocol`. Transport resume (`DataConn.Resume`) keeps the
  established session and its counters exactly as today; a client that lost its daemon opens a fresh route/session and
  runs a new handshake (new `c_eph`, new `d_eph`).
- The handshake stays bounded by the caller's context/deadline; a replayed M1 costs the daemon one key generation, two DHs
  and a few HKDFs and holds a route slot until the deadline: the same order of cost as a v1 hello, and the hub's
  existing per-host session cap and handshake timeout apply unchanged.

### 5. device_hello: what changes (nothing in the flow)

- `TranscriptHash` gains an argument: `TranscriptHash(daemonPub, daemonEphPub, mobilePub)` = `th` above. The hello signed
  bytes keep their layout and domain string:
  `"smind-device-hello-v2" ‖ th ‖ daemon_pub ‖ uint16BE(len(session_id)) ‖ session_id ‖ challenge`. Only the definition of
  `th` changes (it now includes `daemon_eph_pub`), so a signature is bound to *this* handshake's fresh daemon value.
  Implementations take `th` from the channel instead of recomputing it.
- Hello **version stays 2**: v2 has never been released with the old transcript, so there is no deployed client to
  disambiguate, and `th` is domain-separated by the new label so an old-transcript signature cannot verify.
- **The challenge stays** (daemon speaks first, `relay.device_challenge`). It is now redundant for replay (the E2EE layer
  already refuses a replayed handshake) but removing it changes the application flow and three client state machines for
  one saved round trip. Defense in depth; listed as an [open choice](#open-choices).
- `internal/relay/devices/testdata/device_hello_vector.json` is regenerated (adds `daemon_eph_pub_hex`, `transcript_hash_hex`
  and the negative "other daemon eph"); `device_hello_vector_v1.json` stays as the record of the retired format.
- The desktop/mobile "no challenge within 5 s → daemon too old" rule keeps its timer, but an E2EE v1 daemon is now
  detected earlier, at the handshake (§6), and with a precise message.

### 6. Version handling and downgrade protection

**There is no negotiation and no v1 code in v2 builds.**

- Both hellos are parsed **version byte first**, before length and role. `payload[0] != 2` → `ErrUnsupportedVersion`
  carrying the peer's version, channel closed. The client shows "this daemon speaks an older E2EE version: update the
  daemon" (peer 1) or "update this app" (peer > 2); the daemon logs it and closes without replying, so an unauthenticated
  peer learns nothing.
- The label (`… e2ee v2`) separates v2 keys from v1 keys, so no handshake can derive keys that both a v1 and a v2
  implementation accept.
- **A v2 client never accepts a v1 session from anything**, including a v2-capable daemon being attacked: the only way
  to talk v1 would be a v1 implementation, and none exists in a v2 client. A relay that rewrites the daemon's M2 into a v1-shaped hello
  makes the client refuse it (`ErrUnsupportedVersion`); one that flips the version byte inside a genuine M2 changes the
  schedule and `R_d` fails. Clients **never** retry the same daemon with an older route, protocol or "single-device"
  fallback in response to an E2EE version error; they stop the auto-retry loop and show the cause.
- Future versions: each gets its own label and version byte; a daemon that ever supports two would pick the schedule from
  M1's byte, and a tampered byte is caught by key confirmation, as above.
- The pairing offer does not gain an E2EE-version hint: it is unauthenticated, so a hint would add nothing.

### 7. Test vectors

`internal/relay/e2ee/testdata/e2ee_v2_vectors.json`, generated by the Go implementation (`go test -run TestWriteVectors
-update`) and read by the Go, Rust and TypeScript tests. All keys come from fixed seeds. Fields:
`label`, `seeds` (`client_eph`, `daemon_static`, `daemon_eph`, plus `daemon_eph_replay`), `public_keys`, `es`, `ee`, `th`, `k_d2m`,
`k_m2d`, `confirm_d`, `confirm_m`, `m1_frame_hex`, `m2_frame_hex`, `r_d_frame_hex`, `r_c_frame_hex` (full wire frames with the 4-byte length and the type),
`data` (one sealed frame per direction at counters 0 and 1, plaintexts, full `data` frames), `replay` (the same client ephemeral
with `daemon_eph_replay`: a different `th`, keys and tags), and `negative`:

| Name | Input | Expected |
|---|---|---|
| `tampered_daemon_eph` | M2 with one bit flipped in `daemon_eph_pub`, original R_d | client: `ErrHandshakeConfirm` |
| `wrong_static_key` | M2 carrying a different `daemon_pub`, pin = the original | client: `ErrPeerKeyMismatch`, nothing sent after M1 |
| `forged_daemon` | an attacker's M2/R_d built with its own static key, with the client's pin set to the real daemon's key; and, in a second case, the real `daemon_pub` in M2 but R_d made without the static private key | `ErrPeerKeyMismatch` / `ErrHandshakeConfirm` |
| `v1_hello_from_daemon` | v1 daemon hello (`01 02 ‖ pub`, 34 bytes) | client: `ErrUnsupportedVersion{peer:1}` |
| `v1_hello_from_client` | v1 client hello | daemon: `ErrUnsupportedVersion{peer:1}`, nothing written |
| `empty_ready` | R_d / R_c with an empty payload | `ErrProtocol` |
| `low_order_client_eph` | M1 with the all-zero point | daemon: key agreement error, no M2 |
| `wrong_r_c` | R_c computed from the replay session | daemon: `ErrHandshakeConfirm`, no data sent |
| `device_hello_*` | the `device_hello` negatives (other daemon key, other client key, other daemon ephemeral, other session id, other challenge), now over the v2 `th` | `bad_signature` |

Reference values computed from this spec (seeds: client `a0a1…bf` (byte `0xa0+i`), daemon static `0102…20`, daemon eph
`4041…5f` (session A) and `6061…7f` (session B, a replay of A's client handshake)). The Go implementation must reproduce them:

```
client_eph_pub  605a725d2a4adfeeb1a29e17edd621c1b7593ee8cdbc44ac6c4ab6e2f805d23c
daemon_pub      07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c
-- session A: daemon_eph_pub 79a631eede1bf9c98f12032cdeadd0e7a079398fc786b88cc846ec89af85a51a
th        2d1486c943e0176e9d685177f5b8713ca0aaf78da9f26d3ea6fd301c492a18df
k_d2m     abfe27aeb9a17b57b24c49f36cbd0172825436e677d7f31c64de264cd3871d27
k_m2d     9473838513956c5e92f603e2c7b93c9f2156e67055bd556bc004f3090f728ac1
R_d       a5686ea0af98582a9193c55f4cfa612c63f26d2a4c070bbe5a2befc367196ba3
R_c       231d5e288861ca6336511c5e6f1742bd888c48ef5910aa5f821aa315ae987dc1
-- session B: daemon_eph_pub 675dd574ed7789310b3d2e7681f3790b466c773b1521fecf36577958371ea52f
th        450564e3ccbd05cf0e6cc4a8aceb4ff83314c49ea69028607df2d784c4e24106
k_d2m     0f61785329080dbc6d6e2a3d9b8372c61c2644d90d72df1e472942c885301097
k_m2d     d574b5f6ca341625895b4f219650e45575acbea93e023e265ec15982d04c62cc
```

(`es` is the same in both sessions — `caa14fed…1759` — which is the point: only `ee` changes the keys.)

### 8. Invariant tests that replace `TestE2EECoreUnchanged`

`TestE2EECoreUnchanged` pinned the v1 schedule so P12b could not disturb it. It is deleted with v1. In its place:

- `TestE2EEV2KeyScheduleVectors`: the vector file above, byte for byte, in all three languages.
- `TestReplayedClientHandshakeDerivesFreshKeys`: replay a recorded M1 into a new daemon `Channel`; assert a different
  `th`, different keys, that no `data` frame was written, and that the handshake fails without a valid R_c.
- `TestNoNonceReuseAcrossSessions`: two sessions from the same client ephemeral never produce the same (key, nonce) (sealing the
  same plaintext at counter 0 gives different ciphertexts).
- `TestDaemonEphemeralIsFreshPerHandshake`, `TestClientEphemeralNeverReused`, and `TestTranscriptHashIsTheHKDFSalt`
  (updated: `th` is the HKDF salt of all four outputs and includes `daemon_eph_pub`).
- The negative table in §7, and `TestDowngradeRefused` (v1 hello from either side).

## Compatibility and release

**Recommendation: ship this in the unreleased 0.13.0 together with `device_hello` v2, drop E2EE v1 entirely, one breaking change.**

Why one release: `device_hello` v2 already makes 0.13.0 incompatible with every released client (0.12.0 clients send no
hello and are refused by default, `relay.legacyDevices` false) and with every released daemon (0.12.0 sends no challenge).
A second breaking change in 0.14.0 would ask users to update both ends twice within one release cycle for the same
reason. A dual-stack daemon (v1 and v2) is rejected: it keeps the nonce-reuse path reachable by anyone who forces a v1 handshake
(that is a downgrade by construction), and it needs negotiation that is itself a downgrade surface.

**What breaks** (everything below is already broken by hello v2 except where noted):

| Pair | Result |
|---|---|
| 0.13.0 daemon ↔ 0.13.0 client | works |
| 0.12.0 daemon ↔ 0.13.0 client | refused: E2EE v1 hello → "daemon too old, update it" (already failed before: no challenge) |
| 0.13.0 daemon ↔ 0.12.0 client | refused at the handshake (v1 hello, version byte `1`); also refused today by `device_hello_required` |
| 0.13.0 daemon with `relay.legacyDevices: true` ↔ a 0.12.0 client | **newly breaks** (legacy mode used to carry E2EE v1): the legacy route now speaks E2EE v2, so legacy clients must be updated too. The setting remains for 0.13.0+ clients on the fixed route |
| Any v1 vs v2 pair through the relay | the relay is unaffected: it forwards opaque frames, no relay, proto or admission change; `smind relay` needs no rebuild |

- The desktop's "one attempt on the fixed single-device route without a hello (a daemon from before device identity)"
  fallback can no longer reach such a daemon (it speaks E2EE v1); the clients drop it for E2EE-version errors and say so.
- **Persisted state is unaffected**: pairing offers (daemon static key), device keys, the device registry, the host
  secret and the daemon's static key file keep their formats. No migration. Sessions held by a running daemon end at the
  upgrade (a restart).
- **Release order.** Release PR #352 must not merge until the Go, desktop and mobile PRs have all landed, or 0.13.0
  would ship an E2EE v1 client against an E2EE v2 daemon. Go is not mergeable alone with green cross-language CI: the
  Rust interop suite (`desktop/daemon-client/tests/relay_*_interop.rs`, `go run`s the Go harness) and the mobile node
  integration tests (`integration.*.node.test.ts`, against `internal/relay/bridge/devharness`) speak v1 until their PRs
  land. The plan handles this with a named, temporary gate (an explicit skip with a message pointing at this ADR) in the
  Go PR for those two live suites, removed by the matching client PR; the vector tests are never gated.
- Commits are `feat(relay)!:`/`BREAKING CHANGE:` so release-please records it in the 0.13.0 changelog.

## Open choices

**All four accepted as recommended by the Human, 2026-10-20** ("there are no users yet, so breaking changes are fine — don't add compatibility shims").

1. **One breaking release (0.13.0), no v1 compatibility.** *Recommended and accepted: yes* (above). Alternative: a dual-stack daemon for one
   minor release, which keeps the nonce-reuse path reachable.
2. **Keep the daemon challenge in `device_hello` v2.** *Recommended and accepted: keep* (flow unchanged, one extra round trip per session,
   defense in depth). Alternative: drop it and save a round trip, a `device_hello` v3 and three client state-machine changes.
3. **Landing order with gated live-interop suites** (Compatibility). *Recommended and accepted:* three non-stacked PRs, Go → desktop →
   mobile, the two live suites gated in the Go PR and ungated by their own PRs; hold #352 until all three have merged.
   Alternative: one combined PR with no gate and one large review.
4. **Hand-rolled Noise-NK-shaped handshake rather than a Noise library.** *Recommended and accepted: hand-rolled* (§1). Alternative:
   `Noise_NK_25519_ChaChaPoly_SHA256` through `flynn/noise`, `snow` and a TypeScript Noise library: standard, but three
   new dependencies (one untested under Hermes) and a different wire shape.

## Consequences

- No (key, nonce) pair repeats across sessions even if every client handshake is replayed; a stolen daemon key no longer
  exposes recorded traffic; a bad key is caught inside the handshake with a typed error.
- The handshake costs one extra DH and one X25519 key generation on the daemon, one extra DH on the client, and two
  32-byte ready frames; client latency is unchanged at one round trip (M2 and R_d travel back to back).
- The daemon API loses the ability to be handed an initiator key, and the client `Channel` generates its own: callers
  (relay client, harnesses, `relaytest`, desktop, mobile) change.
- Residuals that remain, unchanged: the relay sees device ids and traffic metadata; an unpinned client trusts whoever answers
  first; counter exhaustion at 2^64 − 1 frames ends a session; the daemon-only control credential and offer-as-secret
  limits of ADR-0031.
