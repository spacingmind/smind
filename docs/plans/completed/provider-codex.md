# Add Codex (OpenAI) as a native third-party provider

## Acceptance Criteria

- New `internal/codex` package implementing OpenAI Codex CLI's own
  **"app-server" JSON-RPC-over-stdio protocol** (`codex app-server`) --
  **not** ACP. Architecturally mirrors `internal/acp` (same
  conn/readLoop/pending-request/dispatch-ordering design, already proven
  correct through two rounds of adversarial review per
  `docs/plans/completed/run-registry-and-cli.md`-adjacent history) but as
  an **independent package**, not a shared abstraction: the wire dialects
  have real, documented differences (below), and forcing one shared engine
  across two genuinely different protocols now, with only two data points,
  would fit neither cleanly -- matches this project's stated
  anti-premature-abstraction stance.
- `internal/codex/rpc.go`: newline-delimited JSON over a spawned
  subprocess's stdin/stdout, mirroring `internal/acp/rpc.go`'s `conn` type
  closely, with two confirmed real differences:
  1. **No `"jsonrpc"` field** emitted or required on the wire -- Codex's own
     protocol crate states verbatim it does not send or expect
     `jsonrpc: "2.0"` (`refs/codex/codex-rs/app-server-protocol/src/rpc.rs`).
     `codex`'s `rpcMessage` struct omits this field entirely (not just
     `omitempty` on `"2.0"` -- genuinely absent).
  2. **No short default request timeout.** Turns can run for a very long
     time; a real production client's default request timeout is 14 days
     (`refs/paseo/.../codex/app-server-transport.ts`, `DEFAULT_TIMEOUT_MS`).
     `codex.Client.Prompt` must wait on the caller's own `ctx`, never impose
     a short built-in default the way a naive port of ACP's request/response
     model might.
  Message discriminator logic (request = has `method`+`id`; notification =
  has `method`, no `id`; response = has `id`, no `method`) is identical to
  ACP's and needs no change.
- `internal/codex/client.go`: `Client` with:
  - `Initialize(ctx) error` -- sends `initialize` (params: `clientInfo`),
    then a fire-and-forget `initialized` notification. Confirmed as the
    real required handshake order from a working production client
    (`refs/paseo/.../codex-app-server-agent.ts`).
  - `NewSession(ctx, cwd) (threadID string, err error)` via `thread/start`
    (params include `cwd`).
  - `Prompt(ctx, threadID, text string, updates chan<- Update) (stopReason
    string, err error)` via `turn/start`.
  - **Key structural difference from ACP, the crux of this package**: in
    ACP, `session/prompt`'s *response* is the completion signal. In Codex,
    `turn/start`'s response only ACKs the turn was accepted; the real
    completion signal is a **later, asynchronous `turn/completed`
    notification**, correlated by `threadId`/`turn.id`. `Prompt` must
    register interest in that notification (by thread/turn id) before
    returning, and block on it, not on `turn/start`'s response. `Turn.status`
    (`Completed`/`Interrupted`/`Failed`) is the stop-reason equivalent.
  - Streamed text arrives via `item/agentMessage/delta` notifications
    (`{itemId, delta}`), forwarded onto `updates` incrementally -- same
    ordering guarantee ACP already relies on (notifications dispatched
    inline in the read loop, never from a separate goroutine, so
    `turn/completed` can never race ahead of the deltas that precede it;
    see `internal/acp/rpc.go`'s `handleLine` comment for why this matters).
- `internal/codex/permission.go`: a `PermissionPolicy` interface mirroring
  `internal/acp`'s (`Decide(ctx, summary, options) (optionID string, err
  error)`), wired to the two real inbound (agent→client) approval-request
  methods: `item/commandExecution/requestApproval` and
  `item/fileChange/requestApproval`. Start with the minimum viable decision
  set per method (an accept/decline equivalent), not the full documented
  enum (`AcceptForSession`, `AcceptWithExecpolicyAmendment`, ...) -- extend
  later if a real caller needs more, per this project's stated
  anti-premature-implementation stance.
- `internal/codex/fakeagent`: a scripted fake `codex app-server` binary
  (mirrors `internal/acp/fakeagent`'s existing shape/purpose exactly) for
  offline tests -- implements `initialize`/`initialized`, `thread/start`,
  `turn/start`, streams `item/agentMessage/delta`, sends `turn/completed`,
  and a scripted `item/commandExecution/requestApproval` scenario.
- `internal/taskrunner`: new `ProviderCodexNative` constant, a
  `codexBackend` interface (single dedicated interface like
  `claudeBackend`, **not** folded into the `acpCommands` map Kimi's plan
  just introduced -- Codex isn't ACP, it doesn't belong there) +
  `runCodexNative` in `runner.go`, wired into `RunPrompt`'s `switch`.
- Explicitly out of scope (mirrors `docs/plans/completed/provider-kimi.md`):
  - Credential injection from `internal/accounts`'s pooled OpenAI OAuth into
    the spawned `codex app-server` subprocess -- same pre-existing gap as
    every other provider today, not introduced or fixed here.
  - The legacy `codex/event/<snake_case_type>` notification dialect
    (older `codex` builds) -- target a recent, pinned `codex` binary only.
  - Sub-agent/child-thread routing -- no smind caller needs it yet.

## Test Scenarios

- `internal/codex`: unit tests against the fake app-server agent, mirroring
  `internal/acp/client_test.go`'s shape -- full
  initialize→thread/start→turn/start→streamed deltas→turn/completed flow;
  a permission-request scenario (command-execution approval) proving
  `Decide` is called with the real summary/options and its answer reaches
  the fake agent's response; an ordering/concurrency test analogous to
  ACP's, proving a delta notification is never missed or reordered
  relative to the terminal `turn/completed` signal. Run with `-race`.
- `internal/taskrunner`: `TestRunner_RunPrompt_CodexNative`, mirroring
  `TestRunner_RunPrompt_GLM`'s shape, against the new fake app-server agent.
- `internal/wsapi`: one `run.start` test with `provider: "codex-native"`
  (or whatever the final constant string is), mirroring
  `TestServer_RunStart_KimiProvider`, proving the wire path end to end.
- `go build ./...`, `gofmt -l`, `go vet ./...`, `go test -race -count=3
  ./...` clean.
- Manual: attempt a real `codex app-server` check if a usable Codex CLI
  binary/install is actually available in this environment (`refs/codex`
  is a source checkout, not necessarily built/installed) -- flag plainly in
  Validation if not performed, matching `provider-kimi.md`'s honesty
  precedent rather than assuming success.

## Decisions

- Independent `internal/codex` package rather than extending
  `internal/acp`'s `conn` to also support Codex's dialect -- see Acceptance
  Criteria's two confirmed wire differences (no `jsonrpc` field, async
  turn-completion vs synchronous). Architecturally mirrors `internal/acp`'s
  file layout (`rpc.go`/`client.go`/`permission.go`/`fakeagent/`) for
  consistency and reviewability, without sharing code between them.
- Protocol design grounded entirely in two real sources, not invented:
  `refs/codex/codex-rs/app-server-protocol` (the canonical Rust schema) and
  `refs/paseo/packages/server/src/server/agent/providers/codex-app-server-agent.ts`
  + `codex/app-server-transport.ts` (an already-working, production
  TypeScript client, confirming actual call sequence/order and real quirks
  a schema alone wouldn't reveal -- e.g. the `initialized` notification
  requirement, the 14-day default timeout, dual-dialect notification
  handling on older builds).
- `codexBackend` as its own dedicated interface (like `claudeBackend`), not
  added to the `acpCommands` map -- that map is specifically for
  ACP-speaking providers (see `docs/plans/completed/provider-kimi.md`);
  Codex speaks a different protocol entirely, so `RunPrompt`'s `switch`
  simply grows a third case rather than overloading the ACP path.
- Approval-decision enum scoped to the minimum viable (accept/decline)
  rather than the full set Codex documents -- avoids implementing decision
  variants (`AcceptForSession`, execpolicy/network-policy amendments) with
  no real caller yet.

## Progress

- [x] `internal/codex`: `rpc.go` (transport)
- [x] `internal/codex`: `client.go` (Initialize/NewSession/Prompt,
  async turn-completion correlation)
- [x] `internal/codex`: `permission.go`
- [x] `internal/codex/fakeagent`
- [x] `internal/codex`: tests (handshake/streaming, permission, ordering)
- [x] `internal/taskrunner`: `ProviderCodexNative`, `codexBackend`,
  `runCodexNative`, tests
- [x] `internal/wsapi`: wire-level test
- [x] Verification (race suite; manual real-CLI check attempted, partial --
  see Validation)

## Validation

- **Protocol correctness (schema-level)**: every method name and message
  field used in `internal/codex` (`initialize`/`initialized`,
  `thread/start`, `turn/start`, `item/agentMessage/delta`,
  `turn/completed`, `item/commandExecution/requestApproval`,
  `item/fileChange/requestApproval`, and the `CommandExecutionApprovalDecision`/
  `FileChangeApprovalDecision`/`TurnStatus` wire enum values) was verified
  field-by-field directly against
  `refs/codex/codex-rs/app-server-protocol`'s real Rust source (`#[serde(rename_all
  = "camelCase")]` structs), not inferred from a summary -- see this
  session's research trail for exact file:line citations.
- **internal/codex unit tests**: `TestClient_HandshakeAndStreamingPrompt`
  (full initialize→thread/start→turn/start flow, proves the *async*
  `turn/completed` notification -- not `turn/start`'s own response -- is
  what unblocks `Prompt`), `TestClient_CommandExecutionApproval_OrderingAndDecision`
  (proves the real command/cwd reach `PermissionPolicy.DecideCommandExecution`,
  and that no delta is delivered before the approval round-trip actually
  resolves). Both pass with `-race`.
- **internal/taskrunner**: `TestRunner_RunPrompt_CodexNative` drives
  `ProviderCodexNative` through `runCodexNative` against the fake app-server
  agent, mirroring `TestRunner_RunPrompt_GLM`'s shape.
- **internal/wsapi**: `TestServer_RunStart_CodexNativeProvider` proves the
  wire path (`run.start` with `provider: "codex-native"`) works end to end.
- **Full suite**: `go test -race -count=3 ./...` clean across every
  package including the two new ones. `gofmt -l`, `go vet ./...`, `task
  build` clean; `git status` after `task build` showed exactly the
  expected changed/new files.
- **Manual real-CLI check: attempted, partial, honestly incomplete.** A
  real `codex` CLI (v0.149.1, brew-installed) was available in this
  session's environment. Attempting to verify `DefaultCommand()`
  (`["codex", "app-server"]`) against it surfaced a real, material finding:
  the actual installed CLI's `app-server` subcommand has moved to a
  **daemon + Unix-socket-proxy architecture** (`codex app-server daemon
  start`, then `codex app-server proxy --sock <path>`), not the direct
  "spawn once, speak JSON-RPC over its own stdio" model this package (and
  the research it was grounded in, based on the same `refs/codex` checkout)
  assumed bare `codex app-server` would do. Confirmed via direct
  experimentation: bare `codex app-server` exits immediately with no
  output; `codex app-server daemon start` fails in this environment with
  "managed standalone Codex install not found" (this sandbox's brew
  install isn't the officially "standalone"-installed CLI the daemon
  subcommand requires) -- so a full live E2E turn against the real binary
  could **not** be completed here. This is a genuine environment/deployment
  limitation, not an assumption papered over: `internal/codex/codex.go`'s
  `DefaultCommand` doc comment now states this finding plainly, including
  what a real deployment may need instead (`daemon start` once out of band,
  then `proxy --sock` per turn). The message-protocol layer itself is
  schema-verified and independent of this transport-level question; only
  the spawn command is unconfirmed against a real running agent.
