# app.spacingmind.com becomes a real web client (browser, relay, E2EE v2)

Status: active. Size L, split into four sequential pull requests (not stacked). High-risk for PRs 1-2 (key handling, a shared package both apps ship, the wire client): validation contract and validator on the exact head, two fresh reviewers, the Lead's own mutations on committed code. Resolves [ADR-0052](../../decisions/0052-hosted-web-app-and-official-relay.md) for Phase 1 and supersedes [ADR-0051](../../decisions/0051-web-demo-mode-and-app-spacingmind-com.md).

## Decided by the Human (2026-11-28)

| | Decision |
|---|---|
| H1 | **Yes**: a hosted web app for real daemons. |
| H2 | **Option A**: browser to relay, E2EE v2 in the browser. No direct mode. |
| H3 | Built against the Human's **self-hosted relay** first (the one mobile uses through a Cloudflare Tunnel). `relay.spacingmind.com` stays parked. The relay URL comes from **pairing** (a link or QR), like mobile. |
| H5 | A web device gets **the same capabilities** as a paired mobile device (no narrower tier). |
| H6 | The device key is a WebCrypto **non-extractable** key in IndexedDB. If the browser evicts it, the person **re-pairs**. |
| H7 | **The demo is dropped entirely.** ADR-0051 is superseded. Remove demo mode and its build. The release workflow's `deploy-demo` job deploys the real app bundle to app.spacingmind.com. The demo size gate moves to the app bundle (Lane B's Astryx size-ratchet decision also applies: temporary raised limits, then ratchet, ADR-0064). |

**Still open for the Human, with recommendations** (recorded in the ADR-0052 update):

- **H4 minimum trust measures** (hosted JavaScript holds keys): recommend (1) immutable, versioned builds, (2) a published bundle hash per release (in the release notes and on the app's about screen), (3) a one-line disclosure at pairing ("this page's code is served by app.spacingmind.com; pair only if you trust it; you can host the same bundle yourself").
- **H10 relay Origin policy**: recommend an allowlist: `app.spacingmind.com` plus operator-configured origins (today the relay accepts any Origin because admission is the gate; that is kept as the default for non-browser clients and the allowlist applies to requests that carry an `Origin`).
- **H14 analytics**: recommend none.

## What the code gives us (read first)

| Fact | Where |
|---|---|
| The relay client is pure TypeScript (`@noble/curves`, `@noble/ciphers`, `@noble/hashes`), 3,271 lines in `mobile/src/relay/`, tested against Go-generated vectors (`internal/relay/e2ee/testdata/e2ee_v2_vectors.json`). Only `deviceKeyStore.ts`, `platformKeyStore.ts`, `platformRandom.ts` and `random.ts` touch React Native or Expo (secure store, expo-crypto). | `mobile/src/relay/` |
| The wire client takes a `SocketLike`; `WsClient` already runs over any `SocketLike`. | `web/packages/ui/src/lib/ws-client.ts` |
| The relay's grpc-web listener accepts any `Origin` today (admission is the gate). A browser needs a CA-verified TLS relay; the Cloudflare Tunnel provides it. | `internal/relay/server/grpcweb.go` |
| A pairing offer is a link whose fragment carries `offer=<base64url JSON>` (`internal/relay/pairing/offer.go`); the person gets it from `smind relay offer` (link plus terminal QR). The fragment is never sent to a server. | `cmd/smind/relay.go`, `mobile/src/relay/pairing.ts` |
| The registry's `Device` has no class field. | `internal/relay/devices/registry.go` |
| The demo is a build mode (`vite build --mode demo`, `src/demo/**`, `scripts/check-demo-build.mjs`, `wrangler.demo.jsonc`) plus two workflows (`demo-build.yml`, `demo-deploy.yml`) and a `deploy-demo` job in the release workflow. | `web/packages/ui`, `.github/workflows` |

## Decisions

- **D1 One shared package, no fork.** Move the pure relay client out of `mobile/src/relay/` into `web/packages/relay-client` (a bun workspace member, name `@smind/relay-client`): `admission`, `base64`, `e2ee`, `grpcweb`, `keepalive`, `pairing`, `proto`, `varint`, `connectionStatus`, `RelayConnection`, `deviceIdentity` (the parts that need no platform API), and their tests and the vector tests. The platform seams become interfaces the package defines and each app implements: `KeyStore` (device identity), `RandomSource`, `Storage` (daemon passwords or any per-daemon record), `SocketFactory`. Mobile keeps `platformKeyStore`/`deviceKeyStore`/`platformRandom` as its implementations; web gets WebCrypto/IndexedDB ones. Mobile imports the package (`file:` dependency plus the Metro `watchFolders`/resolver settings); a spike inside PR 1 proves Metro bundles it (`expo export`) before anything else moves. **This is a cross-owner change** (`mobile/` has its own Lead): PR 1 touches mobile imports only, no behaviour, and mobile's own tests must pass unchanged.
- **D2 Crypto split.** noble for everything the existing vectors cover (X25519, HKDF, HMAC, SHA-256, ChaCha20-Poly1305): identical code path on web and mobile, one set of vectors. WebCrypto only for the **device identity key** (Ed25519 sign): generated `extractable: false`, stored as a `CryptoKeyPair` in IndexedDB (a structured-clone of a non-extractable key); the public key is exported raw and the daemon's device id is derived from it as mobile derives it. **No fallback to an extractable key:** a browser without WebCrypto Ed25519 gets "this browser cannot pair" (the floor is stated in the UI and the ADR; verify at build time: Chrome 137+, Firefox 129+, Safari 17+). Session keys live in JS memory regardless; non-extractability protects the identity key at rest and against exfiltration, not against hostile script on the same origin, and the ADR says so.
- **D3 Storage.** One IndexedDB database `smind-relay` (object stores `identity` and `pairings`). A pairing record holds the daemon id, the daemon public key, the relay URL and fingerprint and the admission secret **only as the existing mobile client stores it** (read `deviceIdentity.ts`/`daemonPasswords.ts` before choosing; never in localStorage; never logged). If the identity key is gone (browser eviction, cleared site data) the app says so and offers to re-pair; it never silently makes a new identity.
- **D4 Connection.** `RelayWebConnection` in web adapts `RelayConnection` to the `SocketLike` that `WsClient` takes, so the whole existing UI runs unchanged over the encrypted channel. Reconnect and resume reuse the shared client's keepalive and replay logic; the UI shows connecting, connected, reconnecting (with attempt), re-pair needed, and admission-refused (pending approval, revoked). One relay connection per browser tab; a second tab shows a clear "already open in another tab" state only if a real conflict is measured (not built unless needed).
- **D5 How the app knows it is a relay client.** The hosted bundle is the normal UI built for a host with no daemon: its connect step is the pairing screen and then the relay connection; the daemon-served build keeps its same-origin connection. A build constant `__SMIND_HOSTED__` (replacing `__SMIND_DEMO__`) selects the connect layer; the daemon-embedded and desktop builds drop the hosted code and the hosted build drops the same-origin token code (a build guard test, like the demo's).
- **D6 Pairing UI.** Paste a pairing link, or scan the QR with the camera (a small QR decoder library or the platform `BarcodeDetector` where present; camera permission is requested only on that action). Before connecting the screen shows the daemon fingerprint, the relay host and the one-line trust disclosure (H4). `smind relay offer` also prints the web link (`https://app.spacingmind.com/#offer=...`; the host is configurable with a flag for self-hosted bundles). Showing the web link/QR inside the daemon-served Settings UI is a follow-up, not in these PRs.
- **D7 "Marked web."** No wire change in Phase 1: the web client sends a device label of the form `Web · <browser> on <os>` (editable at pairing); the daemon's device list shows a "Web" chip for labels that start with `Web · `. A typed `class` field in the device hello is a follow-up (it changes the signed transcript, both vectors and the registry; recorded, not done here).
- **D8 Hosted bundle security.** Strict CSP (no inline script, `script-src 'self'`, `connect-src https: wss:` because the relay URL comes from pairing, `frame-ancestors 'none'`, no third-party origins), `Referrer-Policy: no-referrer`, the offer fragment removed from the address bar and history right after it is read (`history.replaceState`), no analytics (H14), a size and forbidden-string gate on the bundle (the demo gate, moved), immutable versioned deploys with the bundle hash published (H4).
- **D9 Out of scope.** A direct (browser to daemon) mode, the public relay, ToS and legal (H11-H13), the landing repository (below), mobile behaviour, Astryx components (Lane B).

## Coordination with Lane B (Astryx, Lead 774)

Lane B owns `web/packages/ui/src/components/ui/*`, theme and tokens, and the Vite and StyleX build. This item puts its UI in **new files** (`src/components/pair/*`, `src/lib/relay-web/*`) using the existing components and tokens. Two touch points that need Lane B or the Steward:

1. **`web/packages/ui/vite.config.ts` and `package.json` scripts** (PR 4: replacing the demo build mode with the hosted mode, moving the size gate). That is Lane B's build file. PR 4 waits for the Steward to route the change (either Lane B makes the two-line edit or approves mine); until then PR 4's spec is written and the removal of `src/demo/**` and the workflows can be prepared.
2. **The size ratchet** (ADR-0064): the hosted bundle's gate uses Lane B's temporary raised limits, then the ratchet.

## Landing repository (not touched here; for the Human to merge)

The landing page's "Try it live" link changes from the demo to the real app (or to a "Connect your daemon" explainer) in a landing-repo PR.

## Pull requests (sequential)

| PR | What | Size |
|---|---|---|
| 1 | The shared package: move the pure relay client to `web/packages/relay-client`, interfaces for the platform seams, mobile imports it (behaviour unchanged), the Go vectors run in the package's tests | M, high-risk (the mobile app ships it) |
| 2 | The browser client: WebCrypto identity key, IndexedDB store, `RelayWebConnection` over `SocketLike`, reconnect and resume, state hook, the real-relay e2e (browser, real relay, real daemon); ADR-0052 updated to Accepted for Phase 1 and ADR-0051 marked superseded (docs ride here) | L, high-risk |
| 3 | The pairing and connection UI (pair screen, the states, re-pair after eviction, the "Web" chip in the devices list), `smind relay offer` prints the web link; browser frames | M |
| 4 | Remove the demo; the hosted build mode and its CSP/size/forbidden-string gate; the release workflow deploys the real bundle to app.spacingmind.com; the ADR-0051 follow-through | M (needs the Lane B routing above) |

## Acceptance criteria

PR 1
- AC1 The package contains every pure relay module and its tests; `mobile/` imports it and has no copy; the mobile unit tests pass unchanged and the Expo bundle resolves (`expo export`).
- AC2 The package's vector tests read `internal/relay/e2ee/testdata/e2ee_v2_vectors.json` and match the Go output byte for byte.
- AC3 The package imports no React Native, Expo or DOM-only API (a source test).

PR 2
- AC4 The identity key is generated non-extractable (`key.extractable === false`, `exportKey` on the private key rejects), persisted in IndexedDB, and reloaded across a page reload; no code path creates an extractable identity key; an unsupported browser fails with a typed error.
- AC5 No key material, session key, admission secret or offer payload is ever written to `console`, `localStorage`, `sessionStorage`, a cookie, an error message or the address bar after the fragment is consumed (a spy test over a full pair, connect, reconnect run).
- AC6 Eviction (the stored identity deleted) is detected and reported as "re-pair needed"; a new identity is never created silently.
- AC7 Over a real relay and a real daemon, the browser pairs from an offer link, connects, calls `task.list`, receives a pushed event, survives a relay restart (reconnect and resume), and is refused with a clear state when the daemon revokes the device.
- AC8 `RelayWebConnection` satisfies `SocketLike` so `WsClient` runs unchanged; the same wsapi calls work over it as over the same-origin socket.

PR 3
- AC9 The pair screen accepts a pasted link and a QR scan, shows fingerprint, relay host and the trust disclosure before connecting, and clears the fragment from history.
- AC10 Every state has a frame in light and dark at 1280 and 390 (pairing, connected, reconnecting, re-pair after eviction, admission pending, revoked).
- AC11 `smind relay offer` prints the web link and still prints the unchanged deep link for mobile; the devices list shows the "Web" chip for `Web · ` labels.

PR 4
- AC12 No demo code, build mode, script, workflow, wrangler config or doc remains; the guard tests that pinned demo behaviour are removed or turned into hosted-build guards.
- AC13 The hosted build passes its CSP, forbidden-string and size gates; the release workflow deploys that bundle; the daemon-embedded and desktop builds contain no hosted code and the hosted build contains no same-origin token code.

## Test scenarios (named; Vitest `it(...)` or Go `Test_...` as noted)

PR 1: `relay_client_package_has_no_platform_imports`, `vectors_match_go_v2`, `mobile_imports_the_shared_package_only`, `mobile_bundle_resolves_the_shared_package` (command: `expo export`).
PR 2: `identity_key_is_non_extractable`, `identity_key_survives_reload`, `identity_key_missing_means_repair_not_new_identity`, `unsupported_browser_fails_typed`, `no_secret_reaches_console_or_storage`, `fragment_is_removed_after_pairing`, `relay_web_connection_is_a_socketlike`, `reconnect_resumes_after_relay_restart`, `revoked_device_gets_a_clear_state`; e2e `WR-01` pair and call, `WR-02` reconnect, `WR-03` revoke, `WR-04` eviction.
PR 3: `pair_screen_accepts_link_and_qr`, `pair_screen_shows_fingerprint_relay_and_disclosure`, `web_chip_for_web_labels`; Go `relay_offer_prints_the_web_link`; frames.
PR 4: `no_demo_code_remains` (a source guard), `hosted_build_has_no_same_origin_token_code`, `embedded_build_has_no_hosted_code`, `hosted_bundle_gates`.

## Mutations (each by the Lead on committed code; the validator and the reviewers repeat them)

M1 generate the identity key extractable -> `identity_key_is_non_extractable`. M2 log the admission secret on connect -> `no_secret_reaches_console_or_storage`. M3 create a fresh identity when the stored one is missing -> `identity_key_missing_means_repair_not_new_identity`. M4 break one byte of the shared HKDF label -> `vectors_match_go_v2`. M5 keep the offer fragment after pairing -> `fragment_is_removed_after_pairing`. M6 re-add an `react-native` import to the package -> `relay_client_package_has_no_platform_imports`.

## Staffing

GLM peers write the code from step-by-step briefs (profile 8, one commit per step, small steps: GLM stops at its output-token cap on big prompts); peer-sonnet only on a logged limit or failure; no Codex. At most two peers at a time. The Lead writes this spec and the contracts, reviews every diff, runs the mutations, takes the frames and writes the PR bodies. Two fresh reviewers and the validator on PR 1 and PR 2.

## Progress

- Spec written (this commit).

## Validation

- Pending.

## Decisions and risks for the Human

1. **A cross-owner change in PR 1** (mobile imports the shared package): behaviour unchanged, but the mobile Lead and the Human should know before it merges.
2. **A hosted page holds E2EE keys.** Non-extractability protects the identity key at rest, not the session in use; the trust measures H4 are the answer; the ADR says it plainly.
3. **Browser floor** (WebCrypto Ed25519). A browser below it cannot pair; there is no weaker fallback by decision (D2).
4. **The self-hosted relay must have CA-verified TLS** (the Cloudflare Tunnel does); a plain `ws://` relay only works from a non-HTTPS page (the e2e).
5. **PR 4 needs the Lane B routing** for `vite.config.ts` and the size gate.
