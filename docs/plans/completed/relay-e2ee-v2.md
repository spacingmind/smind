# Relay E2EE v2 — a fresh daemon ephemeral key in every session

Spec for [ADR-0035](../../decisions/0035-relay-e2ee-v2-daemon-ephemeral.md) (read it first: wire layouts, key schedule,
downgrade rules, vector fields). Removes the nonce-reuse residual of ADR-0031 D9. Ships in 0.13.0 with `device_hello` v2;
E2EE v1 is deleted, not kept.

## Scope and PRs

Non-stacked, each off `develop`, merged in this order (the user merges; release PR #352 is held until all three land):

1. **Go core** — `internal/relay/e2ee` (handshake, key schedule, `TranscriptHash`, typed errors), `internal/relay/bridge`
   (+ `harness`, `devharness`), `internal/relay/client`, `internal/relay/relaytest`, `internal/relay/devices`
   (signed bytes take `th`; regenerated `device_hello_vector.json`), the shared vector file
   `internal/relay/e2ee/testdata/e2ee_v2_vectors.json`, ADR-0031 cross-references. Temporarily gates the two live
   cross-language suites (below).
2. **Desktop Rust** — `desktop/daemon-client/src/relay/{crypto,channel,client,device}.rs`, tests against the shared vectors,
   un-gates `relay_harness_interop.rs` and `relay_multidevice_interop.rs`, UI/status text for E2EE-version errors.
3. **Mobile TS** — `mobile/src/relay/{e2ee,client,RelayConnection,deviceIdentity}.ts`, tests against the shared vectors,
   un-gates `integration.*.node.test.ts`, status text for E2EE-version errors.

The gate in PR 1: the Rust interop tests and the mobile node integration tests `skip` with the message
`E2EE v2 pending in this client (ADR-0035)`; PR 2 and PR 3 each delete their own gate in the same change as their
implementation. The same message also gates the client unit tests that read the regenerated `device_hello_vector.json` and still compute the v1
transcript (Rust `device_hello_signature_verifies_against_go_vector`; four tests in mobile `deviceIdentity.test.ts`); the matching client PR un-gates
them. No other unit, vector or negative test is gated.

## Acceptance criteria

1. **Key uniqueness.** Two handshakes from the same recorded client hello yield different `th`, `k_d2m`, `k_m2d` and
   confirmation keys; no (key, nonce) pair is shared between any two sessions.
2. **Replay gets nothing.** A replayed client hello makes the daemon write only M2 and R_d; it writes no `data` frame and
   `Handshake` fails (deadline or `ErrHandshakeConfirm`) without a valid R_c.
3. **Key confirmation.** A tampered `daemon_eph_pub`, a wrong static key, a wrong pin, a wrong R_d/R_c, or an empty `ready`
   fail inside `Handshake` with the typed error from ADR-0035 §7; no application frame is sent or accepted.
4. **Daemon authentication unchanged in kind.** The pin from the pairing offer is checked on M2 before any DH; only the holder of the
   daemon static key produces a valid R_d.
5. **No downgrade.** A v2 client/daemon refuses a v1 hello (`ErrUnsupportedVersion{peer:1}`, closed, nothing derived); no code path in
   any language negotiates, retries with an older protocol, or falls back to the single-device route on an E2EE version error.
6. **Fresh initiator key by construction.** No public API accepts a client keypair for a handshake; every `Handshake` generates a new
   `c_eph`; every daemon `Handshake` generates a new `d_eph`.
7. **Cross-language identity.** One vector file; Go, Rust and TypeScript reproduce `th`, all four keys, M1/M2/R_d/R_c frames and
   the sealed data frames byte for byte, and all reject every `negative` entry the same way.
8. **device_hello bound to the new transcript.** Signed bytes use `th` from the channel; the regenerated vector verifies in all three
   languages; the new "other daemon ephemeral" negative fails with `bad_signature`.
9. **Duplicates and resume unchanged.** A byte-identical repeat of M1 (daemon) or M2/R_d (client) is tolerated; a different one closes with
   `ErrKeyRotation`; transport `Resume` keeps the session and counters.
10. **Replaces `TestE2EECoreUnchanged`.** It is removed with v1; the invariants in ADR-0035 §8 exist and pass, `go test -race ./internal/relay/...` is clean.
11. **User-visible.** A v1 peer produces a specific status ("daemon speaks an older E2EE version, update it" / "update this app"),
    the auto-retry loop stops, and no other route is tried.
12. **Nothing else moves.** Relay server, proto, admission, pairing offer format, device keys, registry and daemon key file are byte-compatible with before.

## Test scenarios

Go (`internal/relay/e2ee`, `-race`): `TestE2EEV2KeyScheduleVectors`, `TestWriteVectors` (`-update` regenerates), `TestReplayedClientHandshakeDerivesFreshKeys`,
`TestReplayedClientHandshakeWritesNoDataFrame`, `TestNoNonceReuseAcrossSessions`, `TestDaemonEphemeralIsFreshPerHandshake`,
`TestClientEphemeralNeverReused`, `TestTranscriptHashIsTheHKDFSalt`, `TestTamperedDaemonEphemeralFailsHandshake`,
`TestWrongStaticKeyFailsHandshake`, `TestPinMismatchBeforeAnyDH`, `TestForgedDaemonReadyFails`, `TestDowngradeRefused` (v1 hello
from either side, version byte before length), `TestEmptyReadyIsProtocolError`, `TestLowOrderEphemeralRejected` (both DH outputs),
`TestDuplicateM1ResendsStoredM2AndReady`, `TestDifferentM1AfterEstablishedIsKeyRotation`, `TestNoDataBeforeClientReady`,
`TestPublicAPIHasNoInitiatorKeyParameter` (compile-level via the exported signature), plus the existing counter/replay/pipe/rotation tests
adapted to v2. `internal/relay/devices`: `TestHelloVectorV2OverNewTranscript`, `TestHelloNegativeOtherDaemonEphemeral`.
`internal/relay/bridge`, `internal/relay/client`, `relaytest`: end-to-end multi-device flows (handshake → challenge → hello → serve) over v2 and a
replayed-transcript attempt that never reaches the challenge.

Rust (`cargo test -p smind-daemon-client`): the same names in snake_case against `e2ee_v2_vectors.json` and `device_hello_vector.json`, a
`downgrade_*` pair, `low_order_*`, and the un-gated live interop tests with a real Go harness (replayed transcript included).

TypeScript (`mobile`, vitest): `e2ee.test.ts` reads the shared vector file (no hand-copied hex), the negative table, `RelayConnection`
status for E2EE-version errors, un-gated `integration.*.node.test.ts` including a replay attempt.

Manual: none beyond an isolated-`SMIND_HOME` daemon plus relay harness on loopback; never the user's daemon.

## Decisions

- ADR-0035 holds the design (Accepted 2026-10-20). The Human accepted all four choices: one breaking 0.13.0 with E2EE v1 dropped and no dual
  stack; the daemon challenge stays (`device_hello` stays v2); three non-stacked PRs Go → desktop → mobile with a temporary, explicit skip of the two
  live cross-language suites in the Go PR (each client PR removes its skip), #352 held until all three land; hand-rolled NK-shaped handshake.
- **No users yet, so breaking changes are fine: no compatibility shims** (no v1 code, no fallback routes, no deprecation paths, no migration code).
- Each PR gets an independent, adversarial, crypto-focused review before the user is asked to merge.

## Progress

- [x] ADR-0035 and this plan (docs PR #429).
- [x] PR 1: Go core, vectors, bridge/harness, gate — #431.
- [x] PR 2: desktop Rust, un-gate — #433.
- [x] PR 3: mobile TS, un-gate — #434. Follow-ups: explicit CSPRNG for mobile keys and nonces (#437); relay-connect test fingerprint pin in the Docker image (#443).
- [x] Release held until all three merged; 0.13.0 / 0.13.1 shipped with E2EE v2 (GitHub releases `v0.13.0` 2026-10-20, `v0.13.1`).
- [ ] CHANGELOG/docs sweep (`docs/homelab.md` E2EE wording, ADR-0007 status note): not done as far as the tree shows — `docs/homelab.md` has no E2EE wording and ADR-0007's header still reads "Accepted — 2026-09-11". Cosmetic; not an acceptance criterion; left for the Human to keep or drop.

Closed 2026-10-22 (plans hygiene): moved to `completed/`; every acceptance criterion has evidence below.

## Validation

Checked on `develop` @ `5b90afae` (2026-10-22). Re-run locally: `go test -race -count=1 ./internal/relay/...`
— all 11 packages `ok` (admission, bridge, bridge/devharness, bridge/harness, client, devices, e2ee, pairing, relaypb,
relaytest, server). The Rust and TypeScript suites were not re-run in this pass; their evidence is the merged PRs' CI
(#433, #434) plus the test names found in the tree.

| AC | Evidence |
|---|---|
| 1 key uniqueness | `TestReplayedClientHandshakeDerivesFreshKeys`, `TestNoNonceReuseAcrossSessions` (`internal/relay/e2ee/v2_test.go`) |
| 2 replay gets nothing | `TestReplayedClientHandshakeWritesNoDataFrame`; Rust `replayed_m1_gets_no_data_and_fresh_keys`, `a_replayed_client_hello_gets_no_challenge_and_fresh_keys`; TS `integration.replay.node.test.ts` ("fresh M2 and R_d but never a data frame") |
| 3 key confirmation | `TestTamperedDaemonEphemeralFailsHandshake`, `TestWrongStaticKeyFailsHandshake`, `TestForgedDaemonReadyFails`, `TestEmptyReadyIsProtocolError`, `TestNoDataBeforeClientReady` |
| 4 daemon auth | `TestPinMismatchBeforeAnyDH` (`handshake_pin_test.go`) |
| 5 no downgrade | `TestDowngradeRefused`; Rust `downgrade_refused_v1_hello_from_daemon`, `downgrade_refused_v1_hello_to_daemon_and_newer_peer_is_distinguished`; TS vector negatives in `e2ee.test.ts` |
| 6 fresh initiator key | `TestPublicAPIHasNoInitiatorKeyParameter`, `TestClientEphemeralNeverReused`, `TestDaemonEphemeralIsFreshPerHandshake` |
| 7 cross-language identity | one vector file `internal/relay/e2ee/testdata/e2ee_v2_vectors.json`; Go `TestE2EEV2KeyScheduleVectors` + `TestWriteVectors`; Rust `crypto.rs` reads it; TS `e2ee.test.ts` ("E2EE v2 vectors: key schedule", "frames through the real client Channel") |
| 8 `device_hello` bound to `th` | `TestHelloVectorV2OverNewTranscript`, `TestHelloNegativeOtherDaemonEphemeral` (`internal/relay/devices/identity_test.go`); Rust `device_hello_signature_verifies_against_go_vector`; TS `deviceIdentity.test.ts` |
| 9 duplicates and resume | `TestDuplicateM1ResendsStoredM2AndReady`, `TestDifferentM1AfterEstablishedIsKeyRotation` (`rotation_test.go`) |
| 10 replaces `TestE2EECoreUnchanged` | `TestE2EECoreUnchanged` no longer exists in the tree; `TestTranscriptHashIsTheHKDFSalt` and the §8 invariants pass under `-race` (run above) |
| 11 user-visible | `mobile/src/relay/connectionStatus.ts` maps an older peer to "Daemon is too old for this app" / "Daemon outdated or on a different version" with the `smind update` command (`connectionStatus.test.ts`). Desktop: `desktop/daemon-client/src/relay/client.rs` returns "The daemon speaks an older E2EE version: update the daemon …" / "… newer E2EE version: update this app." (test section "E2EE version errors (ADR-0035 §6)"). |
| 12 nothing else moves | `git diff` of `internal/relay/{relaypb,admission,pairing,server}` between the parent of #431 and `develop` @ `5b90afae` is empty; their tests pass in the run above. |

The `E2EE v2 pending in this client (ADR-0035)` skip gate is gone from the tree (grep over `*.go`, `*.rs`, `*.ts` finds nothing); the Rust interop tests (`relay_harness_interop.rs`, `relay_multidevice_interop.rs`) and the mobile `integration.*.node.test.ts` suites are present and un-gated.

Unverified: a real phone / real desktop app against a real relay on v2 (only the in-process harnesses and the loopback relay were used).
