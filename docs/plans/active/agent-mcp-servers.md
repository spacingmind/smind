# Agent MCP servers

Spec for `docs/decisions/0018-agent-mcp-servers.md`: letting agents smind
spawns (ACP/GLM/Kimi, Claude Code native, Codex native) receive configured
MCP servers, primary use case Playwright MCP browser control
(`@playwright/mcp`) plus a general-purpose registry (e.g. a `pplx` MCP
server). ADR-0018 is Proposed, not yet Accepted — this plan's Decisions
section tracks what still needs the user's sign-off before implementation
starts on each step; do not start a step whose underlying ADR-0018 open
question is unresolved.

## Acceptance Criteria

1. `store.McpServer` CRUD exists (`internal/store/mcp_servers.go` +
   `schema.sql`'s `mcp_servers` table), with `name` enforced unique at the
   DB level.
2. `internal/mcpservers.Registry` validates `transport`/`command`/`url`
   combinations exactly as ADR-0018 specifies (stdio needs `command`,
   http/sse needs `url`, unique non-empty `name`), mirroring
   `internal/profiles.Registry`'s shape.
3. wsapi exposes `mcp.create`/`mcp.list`/`mcp.get`/`mcp.update`/
   `mcp.delete`/`mcp.setEnabled`, each redacting `env`/`headers` values on
   every read path (list/get, and the lifecycle-event payload).
4. `mcpServer.created`/`mcpServer.updated`/`mcpServer.deleted` lifecycle
   events fire per ADR-0009's shape, redacted the same way.
5. `internal/acp.Client.NewSession`/`LoadSession`/`ResumeSession` accept a
   real `mcpServers []any` argument (no longer hardcoded `[]any{}}`),
   populated by `internal/taskrunner.Runner` from the enabled
   `store.McpServer` rows applicable to the run's workspace, filtered by
   the connected agent's `agentCapabilities.mcp.stdio`/`.http` — a server
   whose transport isn't advertised is silently dropped from the wire
   payload but still visible somewhere as "configured but not active for
   this agent" (surfaced via whatever wsapi/event/UI mechanism the
   implementation step for this picks, per ADR-0018's wsapi/UI section).
6. `internal/taskrunner.Runner`'s claude-native construction path
   (`runner.go`'s `claudecode.New(worktreePath, opts...)` call) passes
   `claudecode.WithMCPConfig(json)` built from the same enabled rows.
7. Codex-native mechanism is implemented per whichever answer Open
   Question 1 in ADR-0018 resolves to (smind-managed `CODEX_HOME` with a
   materialized `config.toml`, or the accepted alternative) — this
   acceptance criterion is intentionally left unspecific until that ADR
   question is answered; do not implement step 6 (Codex) until it is.
8. `smind mcp add|ls|rm|enable|disable` CLI subcommands exist, matching
   `smind profile add|ls|rm`'s shape.
9. No `store.McpServer`'s `env`/`headers` value is ever observable in: a
   wsapi response body (redacted), a lifecycle event payload (redacted), an
   `internal/acp` logWriter capture, or a persisted `run_events` row.
10. A bare command (e.g. `npx`) configured for a stdio server is resolved
    to an absolute path before being sent on ACP's wire (`AbsolutePath`-typed
    `command` field) via `exec.LookPath` (ADR-0018 resolved decision 2).

## Test Scenarios

- **Store CRUD.** `store_test.go`-style table test: create/get/list/update/
  delete an `McpServer` row; duplicate `name` on create returns a clear
  conflict error, not a silent overwrite or a generic SQL error leaking to
  the caller.
- **Registry validation.** `internal/mcpservers/registry_test.go`: reject
  empty name; reject unknown transport; reject stdio with empty command;
  reject http/sse with empty url; accept a valid stdio Playwright-shaped
  row and a valid http row.
- **wsapi redaction.** `mcp.create` with `env={"TOKEN":"secret"}` followed
  by `mcp.get`/`mcp.list`: response's `env.TOKEN` is a fixed placeholder,
  never `"secret"`; the lifecycle event payload (`mcpServer.created`) is
  redacted the same way — assert against the raw JSON-RPC bytes the wsapi
  test harness captures, not just the decoded struct, so a redaction bug
  that only redacts one serialization path doesn't slip through.
- **ACP fakeagent asserts mcpServers passed.** Extend
  `internal/acp/fakeagent` to capture the `mcpServers` field it received on
  `session/new` (and `session/load`/`session/resume`) and echo it back in
  its response (or a new `_test/last_mcp_servers` introspection hook,
  matching the existing `_test/release` pattern) so `internal/acp`'s tests
  can assert the client actually sent the configured stdio/http entries in
  ACP's wire shape (`{"type":"stdio","name":...,"command":...,"args":...,
  "env":[{"name":...,"value":...}]}`), including the array-of-`{name,value}`
  shape for `env`/`headers`, not a JSON object.
- **ACP capability filtering.** A fakeagent scripted to advertise
  `agentCapabilities.mcp: {"http": {}}` only (no `stdio`): a configured
  stdio server is dropped from the wire payload; a configured http server
  is sent. A fakeagent advertising no `mcp` capability at all: every
  configured server is dropped (`mcpServers: []`), matching today's
  hardcoded-empty behavior exactly — this is the regression guard that a
  daemon with zero MCP servers configured, or talking to an agent with no
  MCP support, behaves identically to before this work.
- **Claude SDK flag assertion.** `taskrunner_test.go`'s fake-CLI harness
  (`fakecli_test.go`-style): assert the spawned CLI's argv includes
  `--mcp-config` with the expected merged JSON when one or more enabled
  `McpServer` rows exist for the run's workspace; assert its absence when
  none do (matching `claude-agent-sdk-go`'s own `flags_test.go` "mcp
  config" case's assertion style).
- **Workspace restriction** (schema + filter ship in v1, no UI).
  A server with a `workspace_mcp_servers` restriction row is included only
  for that workspace's runs, dropped for every other workspace's runs; a
  server with no restriction row is included for every workspace.
- **Secret non-leakage.** Grep-style test or manual audit (per Progress)
  confirming no `Event`/`RunEvent`/log line emitted during a run that used
  a configured MCP server contains the literal secret value seeded into
  its `env`/`headers` in the test fixture.
- **`npx` resolution (ACP path).** A stdio server configured with
  `command: "npx"`: the ACP wire payload's `command` field is an absolute
  path (`exec.LookPath("npx")`'s result), not the literal string `"npx"`;
  an unresolvable command fails session setup with a clear error.
- **Codex (deferred out of v1).** A Codex run with MCP servers configured
  starts normally and reports them as unsupported for the provider; no
  servers reach Codex.

## Decisions

- Follows `docs/decisions/0018-agent-mcp-servers.md` for the data model,
  storage scope (global registry + optional workspace-restriction join
  table), and per-backend wiring mechanism. ADR-0018 is **Accepted**
  (2026-09-28); its Resolved decisions section settles every question
  this plan's steps reference: Codex deferred (steps 6 and the Codex half
  of 8 are out of v1), `exec.LookPath` for bare commands, workspace
  restriction table without UI, plaintext + redaction, disabled servers
  invisible.
- No architectural decision should be made by an implementing agent that
  isn't already resolved in ADR-0018 or this section — if a step's ADR
  question is still open when an implementation agent picks it up, it
  should stop and ask rather than guess (AGENTS.md rule d).

## Progress

Suggested step order for a cheaper implementation agent (each step should
be small enough to review independently; steps 1-4 have no open-question
dependency and can start as soon as the ADR's data-model section is
accepted):

1. [x] **Store + registry.** `store.McpServer`, `schema.sql`'s `mcp_servers`
   table (+ `workspace_mcp_servers`, resolved decision 3),
   `internal/store/mcp_servers.go` CRUD, `internal/mcpservers.Registry`
   with validation, unit tests. No wsapi, no runner wiring yet.
2. **wsapi + events + CLI.** `mcp.*` methods in
   `internal/wsapi/handlers.go`, `busMcpServerNotifier` in
   `internal/wsapi/server.go`, lifecycle event topics, redaction on every
   read path, `smind mcp add|ls|rm|enable|disable` in `cmd/smind`. Depends
   on step 1 only.
3. **ACP wiring.** `internal/acp/client.go`'s `NewSession`/`LoadSession`/
   `ResumeSession` gain a real `mcpServers` parameter; `internal/acp`'s ACP
   struct-to-wire mapping (stdio/http entries, `env`/`headers` as
   `[]{name,value}`); capability-flag filtering
   (`agentCapabilityFlags` gains `Mcp.Stdio`/`Mcp.Http`, mirroring
   `LoadSession`/`SessionCapabilities.Resume`'s existing decode pattern);
   `internal/acp/fakeagent` test-introspection hook;
   `internal/taskrunner.Runner` resolves the enabled rows for a run's
   workspace and passes them through. Depends on step 1; independent of
   step 2 (can run in parallel).
4. **Claude native wiring.** `internal/taskrunner.Runner`'s
   `claudecode.New(worktreePath, opts...)` call gains
   `claudecode.WithMCPConfig(json)`, built from the same resolved rows as
   step 3. Small, depends on step 1 only.
5. **`npx`/absolute-path resolution.** `exec.LookPath` resolution at the
   ACP mapping layer (step 3); unresolvable command -> clear session-setup
   error. Small.
6. ~~Codex mechanism~~ -- **deferred out of v1** (ADR-0018 resolved
   decision 1). Codex runs report configured servers as unsupported.
7. **UI.** Settings section for CRUD (mirroring Accounts/Profiles
   settings screens) + per-run "active MCP servers" indicator surfacing
   capability-dropped servers (ACP) per ADR-0018's wsapi/UI section.
   Depends on step 2; can start once step 3's capability-drop signal has
   somewhere to report to.
8. **Secret-redaction audit.** A focused pass confirming acceptance
   criterion 9 end to end for ACP and Claude native (Codex deferred per
   ADR-0018 resolved decisions 1 and 5). Depends on steps 3, 4.

## Validation

- AC1 (store CRUD): `internal/store/mcp_servers_test.go` —
  `TestStore_McpServers` (stdio + http round-trips incl. args/env/url/
  headers), `TestStore_ListMcpServersEmpty` (non-nil empty),
  `TestStore_UpdateMcpServer` (full-record replace, createdAt preserved,
  updatedAt advanced) + `...Missing`, `TestStore_DeleteMcpServer` (incl.
  workspace-restriction-row cascade) + `...Missing`,
  `TestStore_SetMcpServerEnabled` (+`...Missing`, other fields untouched).
  Name uniqueness is enforced by the `mcp_servers.name UNIQUE` constraint
  and surfaced as `store.ErrMcpServerNameConflict` (not a raw SQL error):
  `TestStore_CreateMcpServerDuplicateName`, `TestStore_UpdateMcpServerDuplicateName`.
  Pre-existing databases get both tables from schema.sql's `CREATE TABLE
  IF NOT EXISTS` on next Open — `TestOpen_McpServersTableOnPreExistingDatabase`
  (no migrate.go entry needed; new tables, not new columns — same
  precedent ADR-0014 recorded). Verified green: `task test`, `task lint`.
- AC2 (registry validation): `internal/mcpservers/registry_test.go` —
  rejects empty name / unknown transport / stdio without command / http or
  sse without url (table test), accepts the ADR's Playwright stdio example
  and a valid http row, `Update` applies the same validation,
  duplicate-name create surfaces `store.ErrMcpServerNameConflict`,
  `SetEnabled` round-trips, disabled rows stay in `List` but vanish from
  `ListForWorkspace`. Notifier shape (fired on create/update/delete and
  SetEnabled, silent on nil/failed mutations) mirrors internal/profiles.
- Workspace-restriction test scenario (schema + filter half; the
  run-plumbing half lands with step 3):
  `TestStore_McpServersForWorkspace` — restricted server included only for
  its workspace, unrestricted server included everywhere, disabled servers
  never returned, un-restrict makes a server global again.
- ACs 3-10: not started (steps 2-8).
