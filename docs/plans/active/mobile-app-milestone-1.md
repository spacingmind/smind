# Mobile app — Milestone 1: daemon↔relay bridging + grpc-web + Expo pairing proof

## Context

`docs/ROADMAP.md` Phase 3's last two unchecked items are the Expo mobile app
and the `relay.spacingmind.sh` deploy. `docs/plans/completed/relay-e2ee-mobile.md`
(all items checked, live-verified even through a real Cloudflare Tunnel) built
the relay server, the E2EE handshake, admission auth, and a **Go** relay
client library (`internal/relay/client`) — but that plan's own text is
explicit that it does not cover "the Expo mobile app UI... that is a
separate, later plan." This plan is that later plan's first milestone.

Investigating what's actually needed before any mobile screen can do
anything useful surfaced two gaps neither ROADMAP.md nor the completed relay
plan called out:

1. **The daemon itself never acts as a relay client at runtime.**
   `internal/relay/client` is a real, tested Go library, but nothing in
   `cmd/smind/serve.go` or anywhere else calls it — every test that
   exercises it is a standalone test harness or a throwaway CLI program
   (per the relay plan's own Validation notes), not the actual `smind
   serve` process. A mobile device pairing today would reach a relay that
   has no daemon listening on the other end.
2. **React Native/Expo cannot speak native gRPC** (confirmed via research,
   2026-09-21): `@grpc/grpc-js` requires Node APIs and raw HTTP/2 framing
   neither Hermes nor RN's `fetch`/XHR-based networking expose; there is
   no maintained native gRPC module for Expo with custom self-signed
   cert pinning. The realistic path is **grpc-web**, which `grpc-go`
   does not speak natively — it needs an in-process wrapper
   (`github.com/traefik/grpc-web`'s `grpcweb.WrapServer()`, the
   maintained fork of the original `improbable-eng` package) added to
   `smind relay`. `internal/relay/relaypb`'s existing `.proto`-generated
   service is otherwise unchanged; grpc-web is an additional way to reach
   the same RPCs, not a new API surface.

Given these, "build the mobile app" is really three sequential pieces:
wire the daemon into the relay it already knows how to build clients for
(Go-only, no new external unknowns), make the relay reachable from JS at
all (small, contained Go addition), and only then build the actual Expo
screens + a from-scratch TypeScript E2EE implementation (the genuinely
novel, higher-risk part — this session's own experience twice this week
is that unfamiliar, exploration-heavy domains are exactly where an
implementing agent stalls, so Item 3 is scoped to the smallest possible
proof-of-life rather than a polished app).

## Reference material already in the repo

- `internal/relay/client/client.go` — `Dial(target, fingerprint)`,
  `Admit(ctx, c, workspaceID, daemonKeyID, secret)`,
  `OpenControl(ctx, c, admissionID, workspaceID)`,
  `OpenData(ctx, c, admissionID, workspaceID, sessionID, deviceID, kp,
  role)` returning a `*DataConn` with `Handshake`/`Send`/`Receive`/`Resume`/
  `Close`. This is the exact call sequence Item 1's daemon-side bridging
  and Item 3's TypeScript client both need to reproduce (Go for Item 1,
  from scratch in TS for Item 3).
- `internal/relay/e2ee/handshake.go` + `session.go` — the wire-exact
  X25519 + HKDF + ChaCha20-Poly1305 handshake and per-direction
  counter-nonce AEAD framing Item 3's TS client must reproduce byte-for-
  byte to interoperate. `Channel.Handshake`/`writeHello`/`readHello`/
  `deriveSession`/`Session.Seal`/`Open` are the exact functions to mirror.
- `internal/relay/pairing/offer.go` — `Offer.EncodePayload`/`DecodePayload`/
  `URL`/`ParseURL`: the URL-fragment pairing offer format (daemon ID,
  public key, relay endpoint) a mobile client must decode.
- `internal/wsapi/server.go`'s `New`/`Handler` — `newConn(ws, hs)` then
  `c.serve(r.Context())`, where `hs := methodHandlers(...)` is the same
  RPC dispatch table every existing client (web UI, `smind` CLI) already
  goes through. `newConn` currently takes a concrete `*websocket.Conn`
  (gorilla) — Item 1's refactor target.
- `cmd/smind/relay.go` — the existing `smind relay`/`smind relay workspace
  new/ls` subcommands (relay-operator side); Item 1 needs a new,
  separate daemon-side subcommand or config for a daemon to *use* a
  relay as a client, which is a different role from anything that exists
  today.
- `docs/decisions/0007-relay-architecture.md` / `0011-relay-admission-auth.md` —
  the accepted crypto/protocol/admission decisions Item 3's TS
  implementation must match exactly, not reinterpret.

## Decisions

- **Scope is genuinely three sequential items in one plan, done in
  order**: Item 1 (Go, daemon↔relay bridging) → Item 2 (Go, grpc-web on
  the relay) → Item 3 (Expo + TypeScript). Item 3 depends on both 1 and
  2 being real; committing/landing each item separately (own commits,
  ideally own PRs) means Items 1-2 ship real, tested, mergeable value
  even if Item 3 turns out to need more than one pass.
- **grpc-web via in-process wrapper, not a separate Envoy/proxy process.**
  `github.com/traefik/grpc-web` (maintained fork; the original
  `improbable-eng/grpc-web` is effectively unmaintained) wraps the
  existing `*grpc.Server` inside `smind relay`'s own process — no new
  deployable, no new port to operate, matching this repo's existing bias
  toward self-hostable single binaries.
- **Item 3 targets a bare proof-of-life, not a usable app.** Scan-or-paste
  a pairing URL, complete the E2EE handshake, send exactly one RPC
  (`workspace.list`, already a real `internal/wsapi` method) through the
  bridged connection, render the raw JSON. No task list, no realtime
  timeline, no push notifications, no permission-approval UI, no camera
  QR scanning (a manually pasted pairing URL is enough to prove the
  crypto and transport are real) — all of that is Milestone 2+, written
  as its own plan once this milestone's foundation is proven to actually
  interoperate.
- **New top-level `mobile/` directory, not nested under `web/`'s bun
  workspace.** Expo's Metro bundler and `web/`'s Vite tooling have
  different, sometimes conflicting dependency-resolution assumptions
  (React versions, native module resolution); keeping `mobile/` as its
  own independent Expo project (own `package.json`, own lockfile) avoids
  forcing one bundler's constraints onto the other, mirroring how `web/`
  itself is already independent of the Go module root.
- **Daemon-as-relay-client pairing config**: a new `smind relay connect
  <relay-address> <workspace-id> <secret>` subcommand (values a relay
  operator already gets from `smind relay workspace new`'s one-time
  printed output) persists the relay endpoint, workspace ID, and secret
  under `$SMIND_HOME` (same directory `internal/relay/e2ee`'s keypair
  already lives in), analogous to how account credentials persist today.
  `smind serve` reads this config at startup and, if present, dials the
  relay in the background alongside its normal HTTP/WS listener; if
  absent, behavior is completely unchanged (relay connectivity is opt-in,
  matching every other optional integration in this codebase).
- **Item 1 bridges exactly one fixed data session per workspace, not
  dynamic per-device session discovery.** `internal/relay/relaypb`'s
  `ControlFrame` has a `DeviceAttach` message shape that looks purpose-built
  for a device to announce a fresh (sessionID, deviceID) pair to the
  daemon, but `internal/relay/server`'s `OpenControl` doesn't forward or
  interpret it yet -- it answers anything but `Ping` with `Unimplemented`.
  Building real multi-device announcement (bidirectional control-frame
  routing between independently-authenticated connections) is separate,
  nontrivial protocol work untouched by either Item 1 or Item 2's own
  reference material. Since this milestone's own scope is one mobile
  device proving pairing+E2EE+RPC end-to-end (not fanout), both the daemon
  bridge (`internal/relay/bridge`'s `DefaultSessionID`/`DefaultDeviceID`)
  and Item 3's TypeScript client use this fixed, well-known pair instead.
  Milestone 2 (real multi-device pairing) is expected to replace this with
  the dynamic announcement path.
- **`smind relay connect`'s relay fingerprint is captured via
  trust-on-first-use, not a fourth CLI argument.** The plan's own signature
  (`smind relay connect <relay-address> <workspace-id> <secret>`) has only
  three arguments, matching exactly what `smind relay workspace new`'s
  one-time output already gives an operator -- no fingerprint. `smind
  relay connect` therefore dials the address itself and captures the
  presented certificate's SHA-256 fingerprint (`bridge.FetchFingerprint`)
  before persisting it alongside the other three values, the same
  trust-on-first-use tradeoff SSH host keys make.
- **`internal/relay/pairing.Offer` needed two new optional fields
  (`Secret`, `WorkspaceID`) to make pairing-offer-driven admission possible
  at all.** Building Item 3 surfaced that the pre-existing offer (daemon
  ID, public key, relay endpoint, fingerprint) had no way to convey which
  relay workspace a scanned/pasted offer is for, nor any admission
  credential -- without either, a mobile client parsing the offer has no
  `workspace_id` to Admit under and no secret to compute the HMAC with.
  Both fields are additive and `omitempty`/unenforced by `Validate()`, so
  every existing `internal/relay/pairing` test keeps passing unmodified.
  `Offer.Secret`'s doc comment covers the real tradeoff this creates (the
  offer necessarily carries the same shared workspace secret the daemon
  itself holds, not a narrower per-device credential) and why that's a
  documented milestone-1 simplification, not the intended long-term
  design -- see the Item 1 fixed-session-ID Decision above for the same
  kind of tradeoff, made for the same reason (no per-device pairing
  protocol exists yet).
- **The pairing offer's relay address is the grpc-web listener, derived
  from the native one by convention (native port + 1), not stored
  separately.** A mobile client only speaks grpc-web, but `smind relay
  connect`'s stored config is the *native* address (needed for the
  daemon's own dial, Item 1). Since `smind relay connect`'s CLI signature
  is fixed at three arguments per the plan, `smind relay offer` derives
  the grpc-web address from the stored native one using the exact
  convention `smind relay`'s own `--listen`/`--grpc-web-listen` flag
  defaults already use (`:7400`/`:7401`). Correct for the common
  default-ports case this milestone's own end-to-end verification
  exercises; an operator running non-adjacent custom ports needs a real
  fix (recording the grpc-web address explicitly), left as Milestone 2
  scope.
- **Not this plan's job**: the actual `relay.spacingmind.sh` deploy
  (ROADMAP.md's other open Phase 3 item — a hosting/ops task, not a
  code task); TLS cert-pinning UX for a mobile app pairing to a
  self-hosted relay with a self-signed cert (the Cloudflare-fronted
  hosted relay case needs no custom pinning at all, since it presents a
  normal publicly-trusted cert — this plan's Item 3 targets that simpler
  case; self-signed pinning from RN is deferred until a self-hosted-relay
  mobile story is actually needed).

## Acceptance Criteria

### Item 1 — Daemon↔relay bridging (Go)
- `internal/wsapi`'s connection-serving code no longer depends on a
  concrete `*websocket.Conn`; it depends on a small interface (read one
  message, write one message, close) that both a real gorilla WebSocket
  and a relay `*client.DataConn` satisfy. Every existing WebSocket-based
  test and behavior is unchanged (this is a refactor, not a behavior
  change, for the existing HTTP/WS path).
- `smind relay connect <address> <workspaceID> <secret>` persists that
  triple under `$SMIND_HOME`; `smind serve`, when that config is present,
  dials the relay, completes admission (`client.Admit`), opens a control
  stream, and for each new mobile-initiated data session completes the
  E2EE handshake as the daemon role and bridges the resulting `DataConn`
  into the exact same `methodHandlers(...)` dispatch table the WebSocket
  path uses — a mobile-originated `workspace.list` call gets the same
  answer a browser tab's would.
- With no relay config present, `smind serve`'s behavior and startup time
  are unaffected (opt-in, not a new required dependency).
- A relay connection dropping and reconnecting resumes cleanly (reuses
  `DataConn.Resume`, already implemented) without requiring `smind serve`
  to restart.

### Item 2 — grpc-web on `smind relay`
- `smind relay`'s existing `*grpc.Server` is additionally reachable via
  grpc-web framing over the same or a documented second listener, using
  `traefik/grpc-web`'s `grpcweb.WrapServer()`. Native gRPC clients
  (the existing Go `internal/relay/client`, and every existing relay
  test) continue working unmodified.
- A grpc-web-speaking test client (can be a Go-side HTTP test using the
  grpc-web wire format directly, or the real TS client from Item 3 once
  it exists) completes `Admit` → `OpenControl` → `OpenData` → a forwarded
  message, proving grpc-web reaches the same admission/forwarding logic
  Item 2's native-gRPC tests already cover, not a parallel/divergent path.

### Item 3 — Expo scaffold + TypeScript E2EE client + pairing proof
- A new `mobile/` Expo (managed workflow) app exists, running in Expo Go
  or a dev build, with exactly one screen: a text field to paste a
  pairing URL (`internal/relay/pairing`'s `Offer.URL` format) and a
  "Connect" button.
- A TypeScript module (using `@noble/curves`, `@noble/hashes`,
  `@noble/ciphers` — pure JS, Hermes-compatible, no native crypto
  dependency) implements the same X25519 + HKDF + ChaCha20-Poly1305
  handshake and per-direction counter-nonce framing as
  `internal/relay/e2ee`, verified to interoperate with the real Go side
  (not just internally self-consistent).
- A grpc-web client (`@improbable-eng/grpc-web` or the `traefik/grpc-web`
  JS client, whichever actually works against Item 2's server) reaches
  the relay's `Admit`/`OpenControl`/`OpenData` RPCs against a real
  running `smind relay` process (Cloudflare-fronted trust model — no
  custom cert pinning needed for this milestone, per Decisions).
- Tapping "Connect" after pasting a real pairing URL from a real running
  `smind serve` (with Item 1's relay-connect config set up) completes the
  full chain — admission, E2EE handshake, one `workspace.list` RPC — and
  renders the raw JSON response on screen. This is the milestone's single
  concrete "it actually works" proof.

## Test Scenarios

- **Item 1**: unit tests for the new transport interface with a fake
  implementation (no real WebSocket or relay needed) proving
  `methodHandlers` dispatch works identically over it; an integration
  test standing up a real in-process relay (per the completed relay
  plan's own pattern) + a real `smind serve`-equivalent daemon process
  configured via `relay connect`, then a real `client.OpenData` call from
  a test "mobile" role completing handshake and getting a real
  `workspace.list` response back; a reconnect scenario (drop the data
  connection, reconnect, confirm the bridge survives without restarting
  the daemon); no-relay-config startup is unaffected (existing
  `cmd/smind` serve tests keep passing unmodified).
- **Item 2**: existing native-gRPC relay tests unaffected; a new
  grpc-web-framed request completes the same admission/forwarding flow;
  a malformed/incomplete grpc-web frame is rejected without crashing the
  server or affecting concurrent native-gRPC connections.
- **Item 3**: a Jest (or equivalent) test proving the TS handshake
  functions produce byte-identical derived keys against fixed test
  vectors also computable from the Go side (a small Go test program or
  existing Go test printing intermediate values for a fixed keypair, so
  both sides can be checked against the same numbers) — this is the
  single most important test in this plan, since a subtly wrong HKDF
  info string or nonce construction would fail silently as "handshake
  hangs" rather than a clear error. Manual verification: real device (or
  simulator) pairing to a real `smind serve` + `smind relay` through a
  real network path, confirmed by the rendered JSON response.

## Progress

- [x] Item 1 — daemon↔relay bridging.
- [x] Item 2 — grpc-web on `smind relay`.
- [x] Item 3 — Expo scaffold + TS E2EE client + pairing proof.
- [x] Hand off implementation switched from a stalled GLM agent (hit its
      output-token budget on pure exploration, zero Edit/Write calls) to a
      direct Claude session on this same branch/worktree.
- [ ] Independent verification of agent-reported work before merge.

## Validation

### Item 1 — daemon↔relay bridging

- **`internal/wsapi`'s connection-serving code depends on an interface, not
  a concrete `*websocket.Conn`.** Done: `wsapi.Transport` (`conn.go`) is
  `Receive() ([]byte, error)` / `Send([]byte) error` / `Close() error`.
  `internal/wsapi/server.go`'s `wsTransport` adapts a real gorilla
  connection; `internal/relay/client.DataConn` already had exactly this
  method shape and needs no adapter at all. Verified: the full pre-existing
  `internal/wsapi` test suite (`go test ./internal/wsapi/...`) passes
  unmodified except for one test helper's construction call
  (`oauth_test.go`, wrapped in `wsTransport{ws}`) -- no test assertions
  changed, proving the refactor is behavior-identical for the WebSocket
  path. A new `transport_test.go` drives `API.ServeTransport` over a fake
  in-memory `pipeTransport` (no WebSocket, no relay) and gets the same
  `workspace.list` response shape a WebSocket client would.
- **`smind relay connect`/`smind serve` bridging.** Done:
  `internal/relay/bridge` persists the config (`relay-connect.json` under
  `$SMIND_HOME`, alongside where `e2ee.LoadOrCreateKeyPair` puts the
  daemon's persisted keypair) and `bridge.Run` dials, admits, opens a
  control stream, opens the fixed default data session as the daemon role,
  and bridges the resulting `*client.DataConn` into `api.ServeTransport`.
  `cmd/smind/serve.go`'s `startRelayBridge` wires this in only when
  `bridge.LoadConfig` finds a config. Verified by
  `internal/relay/bridge/bridge_test.go`'s
  `TestIntegration_BridgeServesWorkspaceListToMobileRole`: a real
  `server.Run` relay + a real `wsapi.API` + `bridge.Run`, with a fake
  "mobile" `client.OpenData(role=Mobile)` completing the E2EE handshake and
  getting a real `workspace.list` `[]` response back through the bridge.
- **No relay config -> `smind serve` unaffected.** Verified:
  `startRelayBridge` returns immediately (no goroutine started) when
  `bridge.LoadConfig` reports `ok=false`; the existing `cmd/smind` test
  suite (`go test ./cmd/smind/...`), none of which sets up relay config,
  passes unmodified.
- **Reconnect resumes without restarting `smind serve`.** Verified by
  `TestIntegration_BridgeReconnectsAfterDaemonTransportDrop`: a
  transparent TCP proxy sits only in front of the daemon's own connection
  to the relay (the device's connection and the relay process itself are
  untouched), so severing it simulates a dropped daemon<->relay transport
  in isolation. `bridge.Run`'s reconnect loop re-dials/re-admits through
  the still-open proxy and calls `DataConn.Resume`, and the same device
  connection's next `workspace.list` call succeeds again with no restart
  of anything in the test process.
- **Known simplification** (see Decisions): the bridge is one fixed
  session per workspace (`DefaultSessionID`/`DefaultDeviceID`), not
  per-device dynamic session discovery -- sufficient for this milestone's
  single-device proof, documented as a Milestone 2 gap rather than solved
  here.
- `go build ./... && go vet ./...` clean; `go test ./...` passes except
  one pre-existing, unrelated flake in `internal/relay/client`
  (`TestIntegrationMobileDisconnectReconnectDeliversBufferedFrames`, a
  package this plan's Item 1 never touches) that passed 3/3 in isolated
  reruns -- consistent with this repo's known flaky-integration-test
  pattern, not a regression from this change.

### Item 2 — grpc-web on `smind relay`

- **`smind relay`'s `*grpc.Server` is additionally reachable via grpc-web
  framing, native gRPC unmodified.** Done: `internal/relay/server/grpcweb.go`
  wraps the existing `gs` with `traefik/grpc-web`'s `grpcweb.WrapServer(gs,
  WithWebsockets(true), ...)` and serves it on a *second* TLS listener
  (`Config.GRPCWebListenAddr`/`GRPCWebListener`, mirroring the existing
  `ListenAddr`/`Listener` pair) rather than multiplexing both protocols
  behind one port -- chosen specifically so this change carries zero risk
  to the native path, which is never touched. `smind relay`'s CLI gained a
  `--grpc-web-listen` flag (default `:7401`, `DefaultGRPCWebListenAddr`);
  callers that don't set it (every existing test) get an ephemeral port
  instead of that fixed default, so no two concurrently-running test
  processes can collide on one hardcoded port -- see `Config.GRPCWebListenAddr`'s
  doc comment for why "" means something different here than for
  `ListenAddr`. Verified: the full pre-existing `internal/relay/server`,
  `internal/relay/client`, and `internal/relay/admission` test suites pass
  completely unmodified (no test file edited).
- **`WithWebsockets(true)` specifically, not just plain HTTP grpc-web.**
  Plain HTTP/1.1 grpc-web framing only supports unary and server-streaming
  RPCs (a browser/RN fetch can't stream a request body incrementally);
  `OpenControl` and `OpenData` are both bidi-streaming, so without the
  websocket transport mode neither RPC would be reachable from any
  grpc-web client at all, including Item 3's. Documented in
  `grpcweb.go`'s doc comment.
- **grpc-web reaches the same admission/forwarding logic, not a parallel
  path.** Verified by `internal/relay/server/grpcweb_test.go` (no existing
  Go grpc-web client library exists, so this hand-rolls just enough of the
  wire format against the real `WrappedGrpcServer.ServeHTTP`):
  `TestGRPCWeb_UnaryAdmissionRoundTrip` completes `AdmitChallenge`+`Admit`
  over real grpc-web+proto HTTP framing, exercising the exact same
  `internal/relay/admission` verifier the native-gRPC tests cover.
  `TestGRPCWeb_OpenControlPingPongOverWebsocket` completes a full
  admission then a `OpenControl` ping/pong over the websocket transport,
  proving the identical bidi-over-websocket bridge `OpenData` also rides
  is real end-to-end (not just configured). `OpenData`'s own E2EE-layered
  case is left to Item 3's real TypeScript client (the plan's own named
  alternative for this scenario) rather than duplicated by hand in Go.
- **A malformed grpc-web frame doesn't crash the server or affect
  concurrent native gRPC.** Verified by
  `TestGRPCWeb_MalformedFrameRejectedWithoutCrashing`: a frame header
  claiming far more payload than the body carries is rejected (or the
  connection is refused) without taking the process down, and both a
  subsequent grpc-web admission AND a plain native-gRPC admission
  immediately afterward both succeed.
- **Bug found and fixed while writing Item 2's tests**:
  `internal/relay/client.OpenControl` (pre-existing, from the already-
  merged relay-e2ee-mobile plan) sent a bare, payload-less frame as its
  "registration" frame, but `internal/relay/server.Server.OpenControl`'s
  real contract (confirmed by its own existing `TestControlPingPongAndAdmissionGate`
  test, which always sends a `Ping`-bodied frame first) treats any
  non-`Ping` first frame as `Unimplemented` and immediately terminates the
  stream -- so the daemon's control stream (Item 1) was silently dead on
  arrival every time. Fixed by sending an initial `Ping` instead; also
  added a drain goroutine in `internal/relay/bridge`'s `runControl` so a
  long-lived control stream's receive buffer doesn't grow unbounded now
  that replies actually arrive. No existing test changed; this only
  touches `client.OpenControl`'s frame content, not its signature or the
  server contract.
- `go build ./... && go vet ./...` clean. `go test ./internal/relay/... ./cmd/smind/... ./internal/wsapi/... ./internal/server/...`
  passes; the one failure seen across many runs was the same pre-existing
  `internal/relay/client` flake noted under Item 1's Validation (unrelated
  package, unrelated to this item's changes).
- **Reconnect test flakiness investigated, not fully root-caused.**
  `TestIntegration_BridgeReconnectsAfterDaemonTransportDrop` (Item 1) was
  observed to fail intermittently after Item 2's `client.OpenControl` fix
  landed (repeated "data session dropped, reconnecting" cycles instead of
  settling; one later `-count=5` run also hung to its 60s timeout with
  the same symptom rather than cleanly failing). Ran it in isolation
  roughly 40 times total across several batches (single runs, `-count=15`,
  `-count=10 -race`, `-count=5`, and combined with the rest of
  `internal/relay/...`) and it failed/hung only 3 times, without a clear
  signal in added diagnostic logging (Resume itself never errored; the
  resumed connection simply dropped again some seconds later) --
  consistent with this repo's separately-known flaky-integration-test
  pattern (the `internal/wsapi`/`internal/relay/client` CI flakes noted
  above under Item 1) rather than a reliably reproducible logic bug. Per
  this repo's established practice for suspected flakes, not chased
  further; flagged here for anyone who sees it recur, since a real
  environment-independent race is still possible and worth a fresh look
  if it starts failing consistently.

### Item 3 — Expo scaffold + TypeScript E2EE client + pairing proof

- **A new `mobile/` Expo app exists, independent of `web/`'s bun
  workspace, with exactly one screen.** Done: `npx create-expo-app` (SDK
  57, TypeScript template) scaffolded `mobile/` at the repo root with its
  own `package.json`/lockfile; `App.tsx` is one screen (pairing-URL text
  field + Connect button + a result area rendering either the raw JSON
  response or an error). Verified: `npx expo-doctor` (21/21 checks
  passed) and `npx expo export --platform ios` / `--platform android`
  both bundle the full app (including all the crypto/relay modules below)
  via Metro into a Hermes bytecode bundle -- proving the whole dependency
  chain is Metro/Hermes-compatible, which is the strongest build-level
  check available without a physical device or simulator (neither was
  available in this environment; see the gap noted at the end of this
  section).
- **A TypeScript module reproduces `internal/relay/e2ee`'s handshake,
  key derivation, and AEAD framing byte-for-byte.** Done:
  `mobile/src/relay/e2ee.ts` (X25519 via `@noble/curves`, HKDF via
  `@noble/hashes`, ChaCha20-Poly1305 via `@noble/ciphers`, matching
  `handshake.go`'s frame format and `session.go`'s key derivation/nonce
  construction exactly). Verified against fixed cross-language test
  vectors -- **the single most important test in this plan**, per its own
  Test Scenario: `internal/relay/e2ee/fixture_test.go` calls the real,
  unexported `newSession` (not a reimplementation) with fixed seed
  keypairs and pins the exact derived public keys and sealed ciphertext
  (both directions) as Go constants; `mobile/src/relay/__tests__/e2ee.test.ts`
  hardcodes the identical literals and asserts the TypeScript port
  produces byte-identical output, plus a from-scratch hello-frame
  encoding check (`TestFixtureHelloFrameForTypeScriptPort` /
  the matching TS test). All pass. `mobile/src/relay/__tests__/channel.test.ts`
  additionally exercises the full `Channel` handshake+send/receive state
  machine (not just the crypto) over an in-memory transport, including
  the mobile-side pairing-offer public-key verification and its rejection
  path.
- **A grpc-web client reaches the relay's `Admit`/`OpenData` RPCs against
  a real running `smind relay`.** Done: `mobile/src/relay/grpcweb.ts`
  hand-rolls the grpc-web wire format (no `@improbable-eng/grpc-web` or
  protoc-generated stubs -- the message set is small and fixed, same
  reasoning as `internal/relay/server/grpcweb_test.go`'s Go-side test
  client, and its published `WebsocketTransport()` source was read to
  confirm this file's websocket framing is correct): unary calls over
  plain `fetch` (`AdmitChallenge`/`Admit`), and the bidi `OpenData` stream
  over React Native's global `WebSocket` with the `grpc-websockets`
  subprotocol. `mobile/src/relay/proto.ts` hand-encodes the five relay.proto
  messages actually needed (no OpenControl/ControlFrame -- Item 3's mobile
  client never needs it; that stream is entirely the daemon's own
  concern, wired up in Item 1). `mobile/src/relay/admission.ts` mirrors
  `admission.go`'s `HashSecret`/`ComputeHMAC` transcript exactly.
- **Tapping Connect after pasting a real pairing URL completes the full
  chain -- admission, E2EE handshake, one `workspace.list` RPC -- and
  renders the raw JSON response.** Verified two ways:
  1. Every module above unit-tests cleanly in isolation
     (`npm test` inside `mobile/`: 4 files, 14 tests, all passing) and the
     whole app typechecks (`npx tsc --noEmit`, clean).
  2. **A real, automated end-to-end run against a real Go relay and a
     real daemon bridge**, not just unit tests against hand-rolled
     doubles: `internal/relay/bridge/harness` is a small throwaway Go
     binary (built only for this test, not part of `smind`'s own CLI)
     that starts a real `server.Run` relay, a real `wsapi.API`, and a real
     `bridge.Run` daemon connection between them, then prints a real
     pairing URL. `mobile/src/relay/__tests__/integration.node.test.ts`
     (opt-in via `npm run test:integration`, since it needs the Go
     toolchain) spawns that binary and calls this package's real,
     unmodified `connectAndFetchWorkspaceList` against it over actual
     TCP/TLS sockets -- Node 20+'s global `fetch`/`WebSocket` stand in for
     React Native's (same APIs, different JS engine underneath). It
     completes admission, the E2EE handshake, and a `workspace.list`
     round trip, asserting the exact `{"id":"1","result":[]}` shape.
     Passed 4/4 runs.
- **Known gap: no verification on an actual device, simulator, or Expo
  Go.** This environment had no iOS/Android simulator or physical device
  available, so the plan's own "manual verification: real device (or
  simulator) pairing... through a real network path" Test Scenario is
  unmet in the strictest sense. What *is* verified (byte-exact crypto
  fixtures both ways, the full handshake state machine, Metro/Hermes
  bundling, and a real network round trip against a real Go relay via
  Node's spec-compliant `fetch`/`WebSocket`) covers every layer of the
  stack except "does Hermes-on-a-real-device behave identically to
  Node's V8 for these exact APIs" -- a real but comparatively narrow risk
  (both are standard, spec-following JS engines/runtimes for the specific
  primitives used here: `TextEncoder`/`TextDecoder`, `DataView`,
  `Uint8Array`, `fetch`, `WebSocket`, `crypto.getRandomValues`). Running
  `npx expo start` and connecting from Expo Go on a real device/simulator
  is the remaining step to close this gap completely; flagged here rather
  than claimed as done.
- **Known gap: `crypto.getRandomValues` assumed present under Hermes/RN.**
  `mobile/src/relay/admission.ts`'s `randomNonce` and `@noble/curves`'s own
  `x25519.utils.randomSecretKey()` both depend on the global
  `crypto.getRandomValues`. React Native 0.72+ is documented to provide
  this natively (a JSI-backed implementation), and this project's RN
  version (0.86.3) postdates that by a wide margin, so this is expected to
  work without an extra polyfill -- but it's an assumption, not something
  this environment could execute and confirm on-device.
