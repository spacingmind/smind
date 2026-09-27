# 0018: Agent MCP servers

## Status

Accepted (2026-09-28), with Codex deferred. The user approved all
recommendations in Resolved decisions below.

## Context

smind spawns three kinds of agent backend from `internal/taskrunner.Runner`:
ACP (GLM/Kimi, `internal/acp`), Claude Code native (`claude-agent-sdk-go`,
vendored at `refs/../claude-agent-sdk-go` / locally cloned at
`/home/longnp/Coding/personal/claude-agent-sdk-go`), and Codex native
(`internal/codex`). None of them can hand the spawned agent an MCP server
today:

- **ACP.** `internal/acp/client.go:334,361,383` (`NewSession`/`LoadSession`/
  `ResumeSession`) hardcode `McpServers: []any{}` on every `session/new`,
  `session/load`, and `session/resume` call. The field exists on the wire
  (ACP v2's `NewSessionRequest.mcp_servers` /
  `LoadSessionRequest.mcp_servers` / `ResumeSessionRequest.mcp_servers`,
  `refs/agent-client-protocol/agent-client-protocol-schema/src/v2/agent.rs:
  901,1056,1209`) — smind just never populates it.
- **Claude Code native.** `claude-agent-sdk-go` already has a real,
  general-purpose mechanism: `WithMCPConfig(config string)`
  (`client.go:325-332`) sends `--mcp-config <json-or-path>` to the CLI,
  where `config` is the CLI's own `{"mcpServers": {...}}` shape (the same
  JSON a user's `~/.claude.json` or a project `.mcp.json` would carry —
  stdio/sse/http server entries). `internal/taskrunner.Runner` never calls
  `WithMCPConfig` when constructing a `claudecode.Client`
  (`runner.go:193`).
- **Codex native.** Confirmed by reading `refs/codex/codex-rs/config/src/
  config_toml.rs:270` and `refs/codex/codex-rs/app-server/src/
  config_manager_service.rs`: Codex's app-server has **no per-thread MCP
  parameter at all**. `thread/start`'s only field is `cwd`
  (`internal/codex/client.go:35-37`, confirmed against
  `refs/codex/scripts/mcp_conformance/*.py`'s use of the same RPC). MCP
  servers are process-wide config, read from `$CODEX_HOME/config.toml`'s
  `[mcp_servers.<name>]` tables at app-server startup (`config_toml.rs:270`
  and `config_manager_service_tests.rs`'s TOML fixtures); the only runtime
  lever is `config/mcpServer/reload`, which re-reads that same file — there
  is no "start this thread with these MCP servers" call. `internal/codex`
  spawns `codex app-server` with the process's ambient environment
  (`internal/codex/codex.go:DefaultCommand`, `internal/taskrunner/
  runner.go:188` `codexCommand: codex.DefaultCommand()`) — no `CODEX_HOME`
  override exists today, so a real Codex spawn inherits whatever
  `$CODEX_HOME` (or `~/.codex`) the daemon process itself has.

The motivating use case is giving any spawned agent, across all three
backends, browser control via Playwright's MCP server
(`npx @playwright/mcp@latest`, stdio transport) — plus, generally, any other
MCP server a user configures (a `pplx` MCP server is the other concrete
example in scope). The user runs on WSL2, where a real (non-headless)
browser has no display; Playwright MCP's `--headless` flag (headless
Chromium) is required there, not optional.

`docs/decisions/0014-agent-profiles.md` already established the pattern
this ADR follows for "a named, daemon-stored, CRUD-able list of
configuration smind's runners need to consume," and its own "Deliberately
excluded: `featureValues`" section is directly relevant — that ADR
correctly deferred anything requiring live-session-scoped state until a
concrete need existed. MCP servers are that concrete need's session-scoped
consumer for ACP; this ADR's job is to decide where MCP server *definitions*
live (independent of any one session) and how each of the three backends is
handed them at spawn/session-creation time.

## Decision

### Data model: a new `mcp_servers` store table, global per daemon

A new persisted record, `store.McpServer`, following `store.AgentProfile`'s
shape and storage precedent exactly (a growing list of named CRUD records,
in `internal/store`, not `config.yaml` — the reasoning in ADR-0014's
"Storage" section applies unchanged: live create/update/delete, a wsapi CRUD
+ lifecycle-event surface, no restart-to-edit story).

```sql
CREATE TABLE IF NOT EXISTS mcp_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,             -- human label; also the MCP server's
                                     -- own "name" on the wire (ACP's
                                     -- McpServerStdio.name/McpServerHttp.name,
                                     -- Claude's mcpServers map key, Codex's
                                     -- [mcp_servers.<name>] table key) --
                                     -- see "Name is the join key" below
    transport TEXT NOT NULL,        -- 'stdio' | 'http' | 'sse'
    command TEXT NOT NULL DEFAULT '',   -- stdio only: absolute path or bare
                                         -- command to resolve (see Playwright
                                         -- worked example)
    args TEXT NOT NULL DEFAULT '[]',    -- stdio only: JSON array of strings
    env TEXT NOT NULL DEFAULT '{}',     -- stdio only: JSON object, string->string;
                                         -- secret-bearing (see Secrets below)
    url TEXT NOT NULL DEFAULT '',       -- http/sse only
    headers TEXT NOT NULL DEFAULT '{}', -- http/sse only: JSON object,
                                         -- string->string; secret-bearing
    enabled INTEGER NOT NULL DEFAULT 1, -- disable without deleting
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

Rejected shape: a single JSON blob column instead of `transport`+typed
columns. Rejected because `transport` needs to be queried/validated (the
capability-check gate below needs to filter by transport per-agent without
parsing every row's opaque JSON), matching why `store.Run`/`store.Task`
don't collapse their fields into one JSON blob either.

**Name is the join key, not `id`.** Every backend's own MCP config keys a
server by its `name` string, not a smind-internal integer id (ACP's
`McpServerStdio.name`, Claude's `mcpServers` map key, Codex's
`[mcp_servers.<name>]` TOML table key). `store.McpServer.Name` **must be
unique** (a `UNIQUE` constraint on the column) so it can be used directly as
that key on every wire without a smind-side rename step — unlike
`AgentProfile.Name` (ADR-0014, "a label, not a key"), this name genuinely is
the identity a downstream protocol dereferences by string.

**Scope: global per daemon, not per workspace, matching ADR-0014's
`agent_profiles`.** An MCP server describes a *capability* ("this agent can
browse the web," "this agent can query Perplexity"), not something tied to
one codebase checkout. Recommended: an **optional join table** for
restricting which workspaces may use a given server, following the exact
`workspace_accounts` precedent (`internal/store/schema.sql:37-41`) ADR-0014
itself points to:

```sql
CREATE TABLE IF NOT EXISTS workspace_mcp_servers (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    mcp_server_id INTEGER NOT NULL REFERENCES mcp_servers(id),
    PRIMARY KEY (workspace_id, mcp_server_id)
);
```

No row in this table for a given server = available to every workspace
(matches `workspace_accounts`' own "no restriction row = universally
available" semantics, per its existing callers). This makes "restrict
Playwright MCP to just my two browser-testing workspaces" possible from day
one without a second migration later, at the cost of one small always-empty
table for users who never need the restriction — cheap enough to include
now rather than bolt on. See Resolved decisions for whether v1 actually needs
the join table wired into the UI, or whether an empty-in-practice table (no
join-table UI, always-available) is a fine v1 that still leaves room to grow
into it exactly like `workspace_accounts` did.

**Not scoped to agent profiles.** `agent_profiles` (ADR-0014) already
deliberately excludes anything live-session-scoped (`featureValues`), and an
MCP server list is exactly that kind of thing — a profile says *how
cautious* an agent is, not *what tools* it can reach. Recommended: MCP
servers are attached to a **run** (effectively, to a workspace's available
set, filtered by what the chosen backend's capabilities accept — see below),
not to a profile. A profile's existing exclusion rationale extends
naturally: adding an MCP-server-list field to `agent_profiles` would be
exactly the kind of "inert field with nothing to apply it to" ADR-0014
warned against, since profiles are applied by client-side field-copy before
a run starts, never resolved server-side.

### How each backend receives the configured servers

**ACP.** `internal/acp.Client.NewSession`/`LoadSession`/`ResumeSession` grow
an `mcpServers []any` parameter (replacing today's hardcoded `[]any{}`),
populated by `internal/taskrunner.Runner` from the workspace's enabled
`store.McpServer` rows, filtered by the connected agent's advertised
`agentCapabilities.mcp` (`McpCapabilities.stdio`/`.http` —
`refs/agent-client-protocol/.../agent.rs:4633-4673`; note `.acp` is
`unstable_mcp_over_acp`-gated and out of scope here). Each row is mapped to
the wire shape:

```jsonc
// stdio
{"type": "stdio", "name": "...", "command": "...", "args": [...], "env": [{"name": "...", "value": "..."}]}
// http
{"type": "http", "name": "...", "url": "...", "headers": [{"name": "...", "value": "..."}]}
```

(`env`/`headers` are arrays of `{name, value}` structs on ACP's wire —
`refs/agent-client-protocol/.../agent.rs:2859-3037`'s `McpServerStdio`/
`McpServerHttp` — not a JSON object; the store row's JSON-object `env`/
`headers` columns are converted at this mapping layer, not stored
pre-shaped, so the same row also maps cleanly to Claude's object-shaped
`env`/`headers` and Codex's TOML tables.) `McpServerStdio.command` is ACP's
`AbsolutePath` type (`agent.rs:3007`) — **must** be an absolute path on the
wire; see the Playwright worked example for what this means for
`command: "npx"`.

**Claude Code native.** `internal/taskrunner.Runner` builds a
`{"mcpServers": {...}}` JSON object from the same enabled rows (no
capability gate needed — the CLI accepts whatever transports it supports;
Claude Code's CLI has long supported stdio/sse/http) and passes it via
`claudecode.WithMCPConfig(json)` when constructing the `claudecode.Client`
(`runner.go:193`'s `claudecode.New(worktreePath, opts...)` call gains this
option). This is a real, already-implemented SDK option — the smallest
change of the three backends.

**Codex native.** No per-thread call exists to hand this over — see
Context. The only real lever is `$CODEX_HOME/config.toml`'s
`[mcp_servers.*]` tables, read at `codex app-server` **process startup**.
Recommended mechanism: `internal/codex`'s spawn (`codex.New` /
`codex.DefaultCommand`, wired from `internal/taskrunner/
runner.go:188,196`) gains a `CODEX_HOME` override pointing at a
smind-managed directory (e.g. under `SMIND_HOME`) whose `config.toml` is
(re)materialized from the enabled `store.McpServer` rows before each spawn
— written once per spawn, not mutated live, sidestepping
`config/mcpServer/reload` entirely (that RPC exists for Codex's own
long-lived interactive-session config editing, not a fit for smind's
per-run subprocess lifecycle). This is a materially different mechanism
from the other two backends' per-session parameter and is flagged as an
**resolved decision** below (Codex deferred) — it's the one part of this ADR that changes
Codex's env/config surface rather than just adding a request parameter.

### Capability checks

Only ACP has a capability negotiation for this (`McpCapabilities`,
Context). `internal/taskrunner.Runner` must read
`Client.AgentCapabilities`'s `mcp.stdio`/`mcp.http` (mirroring
`SupportsLoadSession`/`SupportsResumeSession`'s existing decode pattern,
`internal/acp/client.go:174-187`) and silently drop any configured server
whose transport the connected agent didn't advertise support for, rather
than sending it and letting the agent reject the whole `session/new` call.
A dropped-for-capability server should still surface to the user somehow
(see wsapi/UI surface below) — silently doing nothing is the wrong failure
mode for "I configured Playwright MCP and browser tools never showed up."
Claude Code native and Codex native have no equivalent negotiated
capability list at this layer (Claude's CLI just tries whatever's in
`--mcp-config`; Codex's config.toml is validated by Codex itself at
startup) — a bad server there fails at spawn/config-load time instead, an
acceptable difference in failure shape given neither has an ACP-style
pre-flight capability list to check against.

### Secrets in `env`/`headers`

Stored in the same trust model `accounts.credential_data`
(`internal/store/accounts.go`) already uses: plaintext in the SQLite file
under `SMIND_HOME`, no additional at-rest encryption layer. This ADR does
not introduce a new secrets primitive — it reuses the one smind already
accepted for provider credentials. What it must get right, matching that
precedent:

- **Never echo back over wsapi/CLI/logs.** `mcp.list`/`mcp.get` (see wire
  surface below) return `env`/`headers` values redacted (e.g. every value
  replaced with a fixed placeholder once a key is set, the same shape
  `account.list` already needs for `credential_data` — check
  `internal/wsapi/handlers.go`'s existing account-listing redaction, if
  any, and match it; if none exists yet, this ADR's implementation is the
  first to need it and should follow the "full value only accepted on
  write, never returned on read" rule for both resources going forward).
- **Never appear in `internal/acp`'s logWriter capture** (`WithLogWriter`,
  `client.go:265`) or in any structured `taskrunner.Event` — the mapping
  from `store.McpServer` to each backend's wire/config shape happens
  entirely inside `internal/taskrunner`/`internal/codex`, never logged as a
  whole struct.
- **Never appear in a `run_events`-persisted payload.** MCP server
  configuration is session-setup, not a streamed event; no
  `taskrunner.Event` should ever carry a server's `env`/`headers`.

### Permission implications

`internal/taskrunner/permission.go`'s `acpDeciderAdapter.Decide`
(`client.go:106-141`) already turns any ACP `tool_call`/`tool_call_update`
into a `PermissionDecider` call by title/rawInput — an MCP tool call is just
another ACP tool call from this adapter's point of view; `ToolCallUpdate`'s
`kind` for an MCP-server-provided tool is agent-reported like any other (per
live-traffic precedent already established for GLM's own built-in tools,
`client.go:132-140`'s comment), so **no code change is required** for MCP
tool calls to flow through smind's existing manual/auto-safe permission
gate on the ACP path — `autoAllowACPFileEdit`'s worktree-containment check
already fails closed for a tool call whose `kind`/`locations` don't look
like an in-worktree file edit, which an MCP browser-control or search tool
call won't, so it correctly falls through to a human decision under
`ApprovalPolicyAutoSafe`, same as `ApprovalPolicyManual`.

For Claude Code native, an MCP tool arrives as `req.ToolName` of the form
`mcp__<server>__<tool>` (Claude Code's own naming convention for
MCP-provided tools) through `claudeDeciderAdapter.Decide`
(`client.go:326-341`) — `bashCommand`'s `req.ToolName != "Bash"` check
already returns `""` for it, so it's never a candidate for
`ApprovalPolicyAutoSafe`'s allowlist auto-allow and always reaches the
decider as a manual-shaped allow/deny, which is correct default behavior
for a new tool surface (a browser-automation or search call should not be
silently auto-approved just because it isn't a shell command). Codex has no
MCP-tool-specific approval path today either — its two approval kinds
(command execution, file change) don't model "call an MCP tool" as a
distinct request kind at all in `internal/codex`'s current scope; an
MCP-provided Codex tool call's approval shape is genuinely unverified
against a real Codex MCP integration and is called out below as an open
item, not decided here.

### wsapi/CLI/UI surface

Following `profile.*`'s exact convention (ADR-0014):

| Method | Params | Result |
| --- | --- | --- |
| `mcp.create` | `{name, transport, command?, args?, env?, url?, headers?}` | created `store.McpServer`, `env`/`headers` values redacted |
| `mcp.list` | none | `[]store.McpServer`, ordered by `id`, redacted |
| `mcp.get` | `{id}` | the server, redacted |
| `mcp.update` | `{id, ...same fields...}` | updated server, redacted (full-record replace, matching `profile.update`) |
| `mcp.delete` | `{id}` | `{}` |
| `mcp.setEnabled` | `{id, enabled}` | updated server | dedicated toggle, not folded into `update`, matching why `task.archive` is its own RPC rather than a generic patch (ADR-0014's own `profile.update` note) |

Validation: `name` non-empty and unique (a clear conflict error, not a
silent overwrite); `transport` one of `stdio`/`http`/`sse`; `stdio` requires
`command`; `http`/`sse` require `url`. Lives in a new `internal/mcpservers`
package (mirroring `internal/profiles`'s shape exactly — thin `Registry`
wrapping `store.Store`, since `internal/store` must not import
provider-shape validation any more than it imports `internal/taskrunner`).

Lifecycle events, ADR-0009's shape: `mcpServer.created`/`mcpServer.updated`
(full snapshot, redacted) / `mcpServer.deleted` (`{"id": ...}`), published
via a `busMcpServerNotifier` mirroring `busProfileNotifier`
(`internal/wsapi/server.go`).

CLI: `smind mcp add|ls|rm|enable|disable`, matching `smind profile
add|ls|rm`'s shape (ADR-0014's CLI section) — a labeled, addable "thing
referenced by id," not an editable container.

UI: a Settings section (matching Accounts/Profiles) to add/edit/toggle MCP
servers, plus a per-run indicator (in the composer or run header) showing
which configured servers are actually active for the connected agent this
turn — surfacing the capability-drop case above, not silently doing
nothing.

### Playwright MCP worked example

```json
{
  "name": "playwright",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@playwright/mcp@latest", "--headless"],
  "env": {}
}
```

**WSL2 considerations:**
- `--headless` is required, not cosmetic: WSL2 has no display server by
  default, so a real (headed) Chromium launch fails outright. This should
  be the documented/default recommendation for any WSL2 smind install, not
  just an option a user discovers by trial and error.
- **ACP's `command` field is typed `AbsolutePath`** (Context, above) —
  `"npx"` alone is not an absolute path. Two ways to satisfy this,
  neither decided here (see Resolved decisions): (a) smind resolves `npx`
  (or any bare command) to its absolute path via `exec.LookPath` at the
  point it builds the ACP wire payload, transparently, so a user can still
  type `npx` in the UI/CLI; (b) require the user to supply an absolute path
  themselves (`/home/<user>/.nvm/versions/node/.../bin/npx` or similar),
  consistent with the field's literal schema meaning but worse UX and
  fragile across machines/containers. Claude's `--mcp-config` and Codex's
  `config.toml` both accept a bare command name (no `AbsolutePath`
  constraint at that layer), so this is specifically an ACP-transport
  concern, not a general one.
- First run of `npx @playwright/mcp@latest` downloads and installs a
  headless-Chromium build (`npx playwright install` behavior) — this can
  take tens of seconds to minutes on first use per machine, and needs
  network egress from inside WSL2. Not a smind-side problem to solve, but
  worth surfacing in whatever UI/docs recommend this server (a "first run
  may take a while" note), since a naive user could otherwise read a slow
  first turn as smind being broken.

## Alternatives considered

- **MCP servers as a field on `agent_profiles`.** Rejected: profiles are
  applied by client-side field-copy before a run starts (ADR-0014's "How a
  profile is applied"), and an MCP server list needs a capability-gated,
  backend-specific mapping step that field-copy can't do — it has to be
  resolved server-side at session-creation time regardless of which
  profile (if any) is in play. Keeping it a separate global registry also
  lets one server (Playwright) be reused across every profile and provider
  without duplication.
- **Per-workspace-only storage (no daemon-global registry).** Rejected for
  the same reason ADR-0014 rejected per-workspace profiles: a capability
  ("this agent can browse the web") is not tied to one codebase checkout,
  and per-workspace-only storage would force re-adding Playwright MCP once
  per workspace. The optional `workspace_mcp_servers` join table gets
  workspace-scoping back additively, matching `workspace_accounts`.
  Recommended over: making the join table mandatory-from-day-one
  (over-building for a restriction need that hasn't been asked for yet).
- **`config.yaml` persistence.** Rejected for the identical reasoning
  ADR-0014 already gives for profiles (wrong shape for a growing CRUD list,
  no live update-in-place story).
- **Writing to the codex-native user's real, shared `config.toml` in
  their actual `$CODEX_HOME`.** Rejected: mutating a file smind doesn't own
  (the user's own Codex CLI config, potentially containing MCP servers or
  settings they configured outside smind entirely) is a much larger blast
  radius than a smind-managed `CODEX_HOME` override, and risks clobbering
  the user's own Codex setup on every smind spawn. A smind-managed
  `CODEX_HOME` keeps the two entirely separate at the cost of a Codex-only
  auth/session-cache question (see Resolved decisions).
- **Encrypting `env`/`headers` at rest with a new secrets primitive.**
  Rejected for v1: smind has no existing at-rest encryption for
  `accounts.credential_data` either, and inventing one only for this table
  would be an inconsistent, partial security posture (provider OAuth
  tokens stay plaintext, but an MCP server's API key doesn't) rather than
  an actual improvement. Worth revisiting for both tables together, not
  this table alone — noted in Resolved decisions.

## Rationale

The three backends' actual capabilities for "hand a spawned agent an MCP
server" are genuinely different in kind (ACP: a real, well-specified
per-session wire field with capability negotiation; Claude Code native: a
real per-spawn CLI flag; Codex native: no per-thread mechanism at all, only
process-startup file config) — this ADR's job is to record a data model
that's backend-agnostic (one registry, one CRUD surface, matching
ADR-0014's already-proven pattern for exactly this kind of daemon-wide
named-resource-list) while being honest, backend by backend, about how thin
or awkward each integration point actually is, rather than pretending
Codex has a parameter it doesn't. The Playwright/WSL2 worked example is
included because it's the concrete case that will actually get exercised
first, and it surfaces a real interoperability wrinkle (ACP's
`AbsolutePath`-typed `command` field vs. a bare `npx` a user will naturally
want to type) that a purely abstract data-model discussion would miss.

## Resolved decisions

Accepted by the user on 2026-09-28:

1. **Codex: deferred out of v1.** v1 wires ACP and Claude native only.
   A smind-managed `CODEX_HOME` risks breaking Codex-native auth and
   can't be verified live until the Codex quota resets (2026-10-07).
   Codex runs get no configured MCP servers in v1 (documented, not
   silent: the per-run "active MCP servers" signal reports them as
   unsupported for the provider). Revisit in a follow-up ADR.
2. **Bare commands resolved via `exec.LookPath`** when building the ACP
   `mcpServers` payload, so the user can type `npx`; an unresolvable
   command fails the session setup with a clear error.
3. **`workspace_mcp_servers`: schema now, UI later.** The table and its
   filter logic ship in v1; managing it is CLI/wsapi only.
4. **Secrets at rest stay plaintext**, matching `accounts.credential_data`;
   encryption, if wanted, is a later change covering both tables. Every
   read path (wsapi, CLI, UI, logs) redacts `env`/`headers` values.
5. **Codex MCP permission-adapter gap: deferred** with (1).
6. **Redaction convention** (per-value placeholder, full value only
   accepted on write) is established here; whether `account.list`/
   `account.get` need the same retrofit is checked during implementation
   and, if needed, fixed in a separate PR.
7. **Disabled servers are fully invisible** to agents and capability
   checks; they only appear in `ls`/`mcp.list`.
8. **CLI name**: `smind mcp add|ls|rm|enable|disable`, sharing the `smind
   mcp` group with ADR-0017's `smind mcp serve`.
