# 0017: MCP server for orchestrating agents

## Status

Accepted (2026-09-28). The user approved all recommendations below
(see Resolved decisions).

## Context

Today the only way an orchestrating agent (Claude Code, or any other agent
harness) drives smind is by shelling out to the `smind` CLI --
`smind task send`, then polling `smind task runs`/`task logs`/`task
permissions`/`task approve` in a loop (`cmd/smind/task.go:146-175`). That
CLI is itself a thin client over `internal/wsapi`'s WebSocket RPC protocol,
via `internal/wsclient` (`internal/wsclient/wsclient.go:1-13`): one
persistent `/ws` connection, JSON envelopes with `id`/`method`/`params` vs.
`event`/`result`/`error` (`internal/wsapi/conn.go`), authenticated by a
`?token=` query param checked with `subtle.ConstantTimeCompare`
(`internal/wsapi/server.go:100-106`) against a token
`internal/auth.LoadOrCreateToken` persists under the config dir
(`cmd/smind/serve.go:78-82`, `cmd/smind/client.go:15-35`).

This shell-and-poll loop is the main reason the user still reaches for
Paseo day to day: Paseo exposes agent orchestration natively as MCP tools
(`refs/paseo/public-docs/mcp.md`), so an orchestrating agent calls
`create_agent`/`send_agent_prompt`/`get_agent_status`/
`list_pending_permissions`/`respond_to_permission` directly, with no shell
round-trip or text-parsing of CLI output. Paseo's server-side wiring is
`refs/paseo/packages/server/src/server/agent/mcp-server.ts` (52 lines): it
wraps an internal "tool catalog" (`./tools/paseo-tools.js`) with
`@modelcontextprotocol/sdk`'s `McpServer`, registering each tool's
name/title/description/inputSchema and forwarding `CallToolResult`s -- the
MCP layer is a thin protocol adapter over daemon-internal calls, not a
second implementation of agent lifecycle logic.

smind's daemon-side surface to wrap already exists and covers most of
Paseo's catalog: `task.create`/`task.list`/`task.get` (agent/workspace
creation and listing), `task.prompt`/`run.start` (send, non-blocking --
`internal/wsapi/handlers.go:789-799,819-870`), `run.attach`/`run.logs`
(status/streaming, `cmd/smind/task.go:400-508`), `run.respondPermission`
(`internal/wsapi/handlers.go:1246-1263`), `run.stop`, and the newer
`chat.*` methods for multiple conversations per task (ADR-0016). What's
missing on the daemon side is not agent-lifecycle logic -- it's a
**second transport** into that same `internal/wsapi` surface that speaks
MCP instead of smind's bespoke WS envelope, plus a couple of missing
convenience shapes (a blocking "wait for this run to finish or need a
decision" call; see Tool set below) that the CLI's poll loop currently
fakes with repeated `run.logs` calls.

Go MCP library landscape (checked via `go list -m` / the module proxy,
2026-09-27): the official `github.com/modelcontextprotocol/go-sdk` is
maintained jointly by Anthropic and Google and is the SDK named in
Anthropic's own MCP documentation; it is active and current (`v1.8.0`
latest tagged release, with `v1.0.0` onward following semver and frequent
point releases). The long-standing community alternative,
`github.com/mark3labs/mcp-go`, predates the official SDK and is still
maintained (`v0.58.0` latest), but is not the one referenced by MCP's own
docs going forward, and smind has no existing dependency on it or history
with its API. Neither is currently in `go.mod`
(`github.com/spacingmind/smind/go.mod:1-20`).

## Decision

Add `smind mcp serve`, a new `cmd/smind` subcommand (alongside `serve`/`task`/
`workspace`/etc., `cmd/smind/main.go:32-48`) that speaks MCP over **stdio**,
using `github.com/modelcontextprotocol/go-sdk`. It is a thin client: on
startup it dials the running daemon's `/ws` exactly the way the CLI does
today (`internal/auth.LoadOrCreateToken` + `wsclient.Dial`,
`cmd/smind/client.go:15-35`), and every tool call is implemented as one or
two `wsclient.Client.Call`/`CallStream` invocations against existing (or
minimally extended) `internal/wsapi` methods. No new store access, no
daemon-internal package imports beside `wsclient` -- the daemon stays the
only process that touches `internal/store`/`internal/runs`/
`internal/taskrunner` directly, matching the CLI's own boundary
(`internal/wsclient`'s package doc, `wsclient.go:1-13`) and this repo's
existing thin-client precedent (ADR-0012, desktop as a thin client over the
same daemon).

### Transport: stdio subcommand, not an HTTP endpoint on the daemon

`smind mcp serve` is a **separate process** an orchestrating agent's MCP client
config launches (`command: "smind", args: ["mcp"]`), the same shape Claude
Code, Cursor, etc. already expect for local MCP servers. It dials the
already-running daemon over `/ws` using the same config-derived
`host:port` + token the CLI reads (`config.Dir()`, `auth.LoadOrCreateToken`)
-- no new listening socket, no new port to firewall.

Rejected: an HTTP/SSE (or MCP "Streamable HTTP") endpoint served directly
by the daemon (e.g. `GET /mcp`). This would let a remote/relay-connected
orchestrator skip the stdio hop, but (a) it means the daemon itself has to
speak MCP framing, coupling `internal/wsapi` -- already careful about
protocol/version compatibility (ADR-0005, ADR-0009) -- to a second wire
protocol with its own versioning; (b) MCP stdio is what every mainstream
agent harness (Claude Code, Cursor, Codex CLI) actually expects for a
locally-installed tool today, so stdio has zero integration cost while
HTTP would need per-client testing; (c) it's additive later if a remote
orchestrator need materializes -- `smind mcp serve` could grow an
`--http :port` flag that reuses the same tool implementations, so this
decision doesn't foreclose it.

### Library: `github.com/modelcontextprotocol/go-sdk`

Recommendation: adopt the official SDK (`go get
github.com/modelcontextprotocol/go-sdk@v1.8.0` or later at implementation
time). It ships the stdio transport, JSON schema tool registration, and
typed `CallToolResult` handling `smind mcp serve` needs, mirroring exactly the
role `@modelcontextprotocol/sdk`'s `McpServer` plays in
`refs/paseo/packages/server/src/server/agent/mcp-server.ts`.

**Alternatives considered:**

- **`github.com/mark3labs/mcp-go`.** Older, well-established, still
  maintained, but not the SDK MCP's own docs point implementers to going
  forward, and this repo has no existing familiarity with its API surface.
  Passed over in favor of the official SDK's likely-longer support
  horizon and closer alignment with the spec's own reference
  implementation.
- **Hand-rolled JSON-RPC over stdio** (smind already has a hand-rolled WS
  JSON-RPC envelope in `internal/wsapi/conn.go`, so the pattern is
  familiar). Rejected: MCP's tool-schema/capability-negotiation surface is
  larger than smind's own bespoke envelope, and reimplementing it forfeits
  compatibility guarantees a maintained SDK gives for free as the spec
  evolves.

### Auth: reuse the daemon's existing bearer token, no new secret

`smind mcp serve` reads the same token `auth.LoadOrCreateToken(config.Dir())`
returns and passes it to `wsclient.Dial` exactly as `cmd/smind/client.go`
does today. No new credential is introduced; the MCP process only ever
runs on the same machine (or trusted context) as the daemon, since it needs
filesystem access to the config dir to read the token file. If `smind mcp serve`
is ever taught to dial a *remote* daemon (relay, ADR-0007/0011), the token
would need to travel out-of-band (env var/flag) the same way a remote CLI
invocation would -- not a new mechanism, just the existing one used over a
different network path.

### Tool set

Each tool is a thin wrapper: unmarshal MCP tool-call args, call one
`wsapi` method (or, for `task_wait`, poll `run.attach`/`run.logs`
server-side in a loop the tool call blocks on), marshal the wsapi result
back as MCP structured content.

| Tool | Wraps | Notes |
| --- | --- | --- |
| `task_new` | `task.create` | `{name, workspaceId?}` -> `{taskId, worktreePath, branch}`. |
| `task_list` | `task.list` | `{workspaceId?}` -> `[]Task` (id, name, status, branch). |
| `chat_list` | `chat.list` | `{taskId}` -> `[]Chat` (ADR-0016). Exposed so an orchestrator can address a specific conversation thread rather than always hitting the task's default chat. |
| `chat_new` | `chat.create` | `{taskId, title?}` -> created `Chat`. |
| `task_send` | `task.prompt` / `run.start` | `{taskId, chatId?, prompt, provider?, approvalPolicy?}` -> `{runId}` **immediately**, matching `task.prompt`'s existing non-blocking contract (`handlers.go:789-799`) -- this tool never blocks on the run finishing, unlike today's CLI which streams live via `CallStream`. Streaming isn't meaningful over MCP's request/response tool-call shape, so `task_send` intentionally returns fast and the orchestrator follows up with `task_wait`/`task_logs`. |
| `task_wait` | `run.attach` (server-side loop) | `{runId, timeoutSeconds?}` -> blocks until the run reaches a terminal state (`done`/`error`/`stopped`) **or** a `permission_request` becomes pending **or** the timeout elapses, then returns `{status, pendingPermission?, timedOut}`. This is the one genuinely new capability: today's CLI has no single call that does this, only `task attach`'s raw event stream or manual polling of `task permissions`. Implemented daemon-client-side in the `smind mcp serve` process, not as a new wsapi method — it's just `CallStream`/`run.logs` polling wrapped in one blocking tool call, matching the reasoning that led to `run.attach`'s own event-stream API rather than adding a `run.wait` RPC to `wsapi` itself. |
| `task_status` | `run.logs` | `{runId}` -> non-blocking snapshot: status, last N transcript entries, any pending permission. For a caller that wants to poll cheaply rather than block. |
| `task_logs` | `run.logs` | `{runId, tail?}` -> full or tailed transcript, mirroring `smind task logs`. |
| `task_permissions` | (derived from `run.logs`, per `fetchPendingPermissions`, `cmd/smind/task.go:697-716`) | `{runId}` -> `[]PendingPermission`. |
| `task_stop` | `run.stop` | `{runId}`. |

`task_send`/`task_wait` split (rather than one blocking "send and wait"
tool) mirrors Paseo's own `send_agent_prompt` (fire-and-forget) +
`get_agent_status` (poll) split (`refs/paseo/public-docs/mcp.md:93-94`),
and keeps a long-running turn from occupying an MCP tool-call slot for the
run's entire duration.

### Pending permissions must not be auto-approved by the orchestrator

The user has an existing rule, already enforced for Paseo/`smind task
approve` today: **an orchestrating agent must not auto-approve a
sub-agent's permission requests** -- the human approves, even read-only
ones. `task_approve`/`task_deny` as MCP tools would let an orchestrating
agent (Claude Code) approve a *smind sub-agent's* tool call entirely on
its own initiative, which is exactly the pattern the user has ruled out
for Paseo. **Decided: the approve/deny tools do not exist** (Resolved
decision 1). The orchestrator only ever sees `task_permissions`
read-only; a human always approves via the smind UI/CLI. Rejected
alternatives: shipping them behind a default-off config flag
(Paseo's `paseoTools.disabledTools` model,
`refs/paseo/public-docs/mcp.md:22-67`), or exposing them
unconditionally.

### Surfacing long-running runs and pending permissions

MCP tool calls are synchronous request/response; smind runs are
long-lived and pending permissions are asynchronous events
(`internal/wsapi/events.go:23`, `TopicPermissionPending`). Three
mechanisms, layered:

1. **`task_send` returns a `runId` immediately** (no blocking) -- the
   orchestrator holds this identifier across tool calls, same as a CLI
   session holds it across `task attach`/`task logs` invocations.
2. **`task_wait` blocks with a timeout** (default e.g. 120s, always
   caller-overridable) so an orchestrator can await completion without
   spinning a poll loop itself, returning early the moment a permission
   goes pending so the orchestrator can react by surfacing it to the
   human (there are no approval tools; see Resolved decisions).
3. **MCP resources/notifications are not used for this** in v1 -- no
   server-push of run-status changes over MCP. Every official MCP client
   (Claude Code included) treats tool calls as the primary interaction
   surface; resource subscriptions are a heavier, less broadly supported
   MCP feature. `task_wait`'s blocking-with-timeout shape covers the same
   need with less surface area. Revisit if orchestrators end up wanting
   push notifications for permissions specifically.

## Alternatives considered

- **Keep shelling out to the CLI and parsing stdout**, just documenting
  the pattern better. Rejected: this is the status quo the user is
  actively trying to move off of; text-parsing CLI output is exactly the
  brittleness MCP tool schemas exist to avoid.
- **A `run.wait` RPC added to `internal/wsapi` itself**, so any WS client
  (not just MCP) gets blocking-wait. Deferred, not rejected outright: it
  would let the web UI or other wsapi clients benefit too, but it's a
  wsapi/store-level change out of scope for "wrap the daemon in MCP,
  don't extend it," and the CLI has managed without it so far via
  `run.attach`'s stream. Worth its own follow-up ADR if a second consumer
  wants it.

## Rationale

The daemon already exposes everything an orchestrator needs through
`internal/wsapi`; the gap closing Paseo's parity requires is a transport
and tool-schema adapter, not new agent-lifecycle logic. Keeping `smind
mcp` a thin `wsclient` consumer (same boundary the CLI already respects)
means the daemon remains the single source of truth for run/permission
state, and the MCP surface can be extended by adding tools without ever
letting a second process touch the store directly. Stdio matches how every
mainstream agent harness expects to launch a local MCP server today, and
reusing the existing token avoids inventing a second auth mechanism for no
present need.

## Resolved decisions

Accepted by the user on 2026-09-28, each as recommended:

1. **No approval tools.** `task_approve`/`task_deny` are not part of the
   MCP tool set at all -- no config flag, no gated variant. The
   orchestrator can see pending requests through `task_permissions`
   (read-only) and surface them; the human approves via `smind task
   approve` or the web UI.
2. **Per-profile gating: moot** -- follows from (1).
3. **stdio only for v1.** No HTTP/Streamable-HTTP flag until a concrete
   remote-orchestrator need appears.
4. **`task_wait` defaults to 120s.** A timeout is not a failure; the tool
   description tells the orchestrator to re-issue `task_wait` with the
   same `runId`, and `task_status` stays available as a cheap
   non-blocking check.
5. **Daemon must already be running.** `smind mcp serve` fails fast with a
   clear error if the WS dial fails; it does not manage daemon lifecycle.
6. **CLI name: `smind mcp serve`.** The `smind mcp` command group is
   shared with ADR-0018's `smind mcp add|ls|rm|enable|disable`
   (agent-side MCP server management); `serve` runs this ADR's MCP server.
