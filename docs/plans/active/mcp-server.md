# `smind mcp serve` server

## Acceptance Criteria

1. `smind mcp serve` is a new subcommand (`cmd/smind/mcp.go`, dispatched from
   `cmd/smind/main.go`'s `switch` alongside `serve`/`task`/etc.) that
   starts an MCP server over stdio and blocks until stdin closes or it
   receives a termination signal.
2. On startup, `smind mcp serve` dials the daemon's `/ws` the same way the
   existing CLI does (`config.Load` + `auth.LoadOrCreateToken` +
   `wsclient.Dial`, `cmd/smind/client.go:15-35`) and exits with a clear,
   non-zero-exit error (printed to stderr, not swallowed into a generic
   MCP error) if the daemon is not running or the dial fails.
3. The following tools are registered and each round-trips through a real
   `wsapi` method against a real (in-process test) daemon, not a mock of
   `wsapi` itself:
   - `task_new` -> `task.create`
   - `task_list` -> `task.list`
   - `chat_list` -> `chat.list`
   - `chat_new` -> `chat.create`
   - `task_send` -> `task.prompt`/`run.start`, returns `{runId}` without
     blocking on run completion
   - `task_wait` -> blocks until the run reaches a terminal status, a
     permission goes pending, or the timeout elapses; never busy-loops
     (uses `run.attach`'s event stream or bounded polling with backoff,
     not a tight loop)
   - `task_status` -> non-blocking snapshot (status + last N transcript
     entries + pending permission, if any)
   - `task_logs` -> full/tailed transcript
   - `task_permissions` -> list of currently pending permission requests
     for a run
   - `task_stop` -> `run.stop`
   - No `task_approve`/`task_deny`: approval tools are absent from the
     catalog entirely (ADR-0017 resolved decision 1)
4. Every tool's input is validated against its declared JSON schema by the
   MCP SDK before the handler runs; an invalid call gets a schema-shaped
   MCP error, not a Go panic or an opaque `wsapi` error string.
5. Auth: the token is read from the same config-derived location the CLI
   uses; there is no separate `smind mcp serve`-only credential, and no token
   value is logged or echoed in any tool response.
6. `task test` and `task lint` pass with the new code included.
7. `tools/list` never contains an approval tool (no `task_approve`,
   `task_deny`, or any wrapper over `run.respondPermission`).

## Test Scenarios

Go tests, using an in-process daemon (the existing `internal/wsapi` test
harness pattern -- see `internal/wsapi/wsapi_test.go` for how other
handler tests spin up a real `API`/`Store`/`Runner` in-process) plus a
fake/stub `taskrunner.Runner` backend so a run's lifecycle can be driven
deterministically without a real Claude/Codex/ACP process:

- **Happy path end-to-end**: `task_new` -> `task_send` -> `task_wait`
  (run completes normally) -> `task_logs` returns the full transcript
  matching what the fake runner emitted.
- **`task_wait` returns early on pending permission**: fake runner raises
  a permission request mid-turn; `task_wait` returns before the run
  reaches a terminal state, with `pendingPermission` populated;
  `task_permissions` on the same `runId` shows the same request.
- **`task_wait` timeout**: fake runner never finishes within the test's
  timeout window; `task_wait` returns `{timedOut: true}` rather than
  hanging past the requested timeout (bounded by a short test timeout,
  not the tool's real default).
- **`task_approve` disabled by default**: with the config flag unset (or
  explicitly `false`), the MCP tool list returned by the SDK's list-tools
  call does not include `task_approve`/`task_deny` at all.
- **`task_approve` enabled**: with the flag set, `task_approve` resolves
  a pending permission and a subsequent `task_wait`/`task_status` shows
  the run has progressed past it.
- **`task_deny`**: denies a pending permission; run either stops or
  continues per the runner's own deny-handling (assert whichever
  `internal/runs.Registry` already does today -- this is read-through
  behavior, not new logic).
- **Auth failure**: `smind mcp serve` started against a wrong/missing token
  (or with the daemon not running at all) exits non-zero with a message
  identifying the cause, and does not hang.
- **Invalid tool args**: calling `task_send` missing the required
  `prompt` field gets an MCP schema-validation error, not a Go-side nil
  deref or an ambiguous `wsapi` "invalid params" string with no
  MCP-level shape.
- **Chat-scoped send**: `task_send` with an explicit `chatId` (from
  `chat_new`) lands its run under that chat, verified via `chat_list`
  showing the run's `runId` associated with the right chat (per
  ADR-0016's `run.list` chat filter, or equivalent read-back).
- **MCP protocol round-trip**: a real MCP client (either the Go SDK's own
  client type, used purely as a test harness, or a raw stdio
  request/response fixture) exercises `initialize` -> `tools/list` ->
  `tools/call` end-to-end against a `smind mcp serve` process under test, to
  catch schema/framing bugs a pure-Go unit test of the handler functions
  would miss.

## Decisions

- Transport: stdio subcommand (`smind mcp serve`), not an HTTP endpoint on the
  daemon. See ADR-0017.
- Library: `github.com/modelcontextprotocol/go-sdk` (official SDK), not
  `mark3labs/mcp-go`. See ADR-0017.
- Auth: reuse `auth.LoadOrCreateToken` + `wsclient.Dial`, no new
  credential. See ADR-0017.
- Tool set and `task_send`/`task_wait` split: see ADR-0017's Tool set
  section.
- Approval tools: **omitted entirely** (ADR-0017 resolved decision 1,
  2026-09-28). The human approves via `smind task approve`/web UI.
- `task_wait` default timeout 120s; daemon must already be running;
  CLI name `smind mcp serve` (ADR-0017 resolved decisions 4-6).

## Progress

Not started. Suggested breakdown for dispatching to an implementation
agent (small, independently reviewable steps; each should end with
`task test`/`task lint` green):

1. **Scaffold the subcommand**: `smind mcp serve` in `cmd/smind/mcp.go`
   (the `mcp` group is shared with ADR-0018's `add|ls|rm|...`) wired into
   `cmd/smind/main.go`'s dispatch; connects via `wsclient.Dial` and
   starts an MCP server with zero tools registered yet, over stdio, using
   `github.com/modelcontextprotocol/go-sdk`. Add the dependency to
   `go.mod`. Verify manually that `initialize`/`tools/list` round-trip
   with an empty catalog.
2. **Read-only tools**: `task_new`, `task_list`, `chat_list`, `chat_new`,
   `task_status`, `task_logs`, `task_permissions`. Each is a direct
   `wsclient.Call` wrapper; no new `wsapi` methods needed. Tests: happy
   path for each against the in-process daemon harness.
3. **`task_send`**: wraps `task.prompt`/`run.start`, non-blocking. Test:
   returns a `runId` immediately; a separate `run.attach`/`run.logs` call
   confirms the run actually started.
4. **`task_wait`**: the one genuinely new piece of logic -- implements
   the blocking-with-timeout/early-return-on-permission behavior
   client-side in the `smind mcp serve` process (no new `wsapi` RPC). Tests:
   the three `task_wait` scenarios above (happy path, early return on
   permission, timeout).
5. **`task_stop`**: thin wrapper, one test.
6. ~~Config flag + gated approval tools~~ -- dropped (ADR-0017 resolved
   decision 1). Instead: a test asserting `tools/list` contains no
   approval tool.
7. **MCP protocol round-trip test**: add the end-to-end stdio test
   scenario once enough tools exist to make it meaningful (after step 3
   or 4).
8. **Docs**: update `AGENTS.md`'s workspace map / README if `smind mcp serve`
   needs a one-line mention for discoverability (does not need its own
   doc page beyond the ADR + this plan).

## Validation

Not yet started -- to be filled in as each Acceptance Criterion is
confirmed by a specific test or manual check, e.g.:

- AC1/AC2 (subcommand starts, dials daemon, fails clearly) -> manual run
  + a Go test that starts `smind mcp serve` as a subprocess against no daemon
  and asserts non-zero exit / stderr content.
- AC3 (each tool round-trips) -> the corresponding Test Scenario above,
  one per tool.
- AC4 (schema validation) -> "Invalid tool args" scenario.
- AC5 (auth reuse, no credential leak) -> "Auth failure" scenario +
  a grep/assert that no tool response or log line contains the raw token.
- AC6 (`task test`/`task lint` green) -> CI/manual run, recorded here
  with the commit/PR it passed on.
- AC7 (no approval tool in `tools/list`) -> the catalog assertion test.
