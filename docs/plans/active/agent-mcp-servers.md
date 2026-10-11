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
  an unresolvable command is skipped (the run continues) and surfaced as a
  session note naming the server and command, never its secrets.
- **Codex (deferred out of v1).** A Codex run with MCP servers configured
  starts normally and reports them as unsupported for the provider; no
  servers reach Codex.

### Per-run "active MCP servers" indicator (step 7, second half)

Today the capability/transport-dropped servers are visible only as a
session note, which `run.logs` renders as an assistant "chunk" and the live
`run.attach` stream drops entirely; there is no structured "which servers
did this run start with" signal and no UI. This adds one additive run event
and one UI affordance.

Wire shape (names and fixed enums only -- no command, args, url, env, or
header value is ever part of it):

- `taskrunner.EventTypeMcpServers` (appended after `EventTypeUsage`;
  `Event.McpServers []McpServerState`). `McpServerState` =
  `{name, transport, status, reason?}` (JSON keys lowercase).
  `status` is `"active"` | `"dropped"` | `"failed"`. `reason` is set when
  status is not active and is one of `"transport_unsupported"` (the agent
  doesn't advertise the transport), `"command_not_found"` (stdio command
  not resolvable in PATH), `"provider_unsupported"` (Codex, deferred) or
  `"failed_to_start"` (Claude CLI `init` reported `status:"failed"`).
- Sent once per turn when at least one server applies to the run's
  workspace (nothing at all when none do, so a run with no servers behaves
  exactly as before). ACP: after capability/`LookPath` filtering, before
  the session is created. Claude-native: at start with every row `active`,
  then again after the CLI `init` message *only* if it reported failures
  (latest event wins). Codex: every row `dropped`/`provider_unsupported`.
  The existing session notes stay as they are.
- Persisted in `run_events` like any event (additive `mcpServers` field in
  `persistedEvent`), replayed by `run.attach` backfill and `run.logs`.
- `run.attach`/`task.prompt` stream event `mcp_servers` with params
  `{mcpServers: [...]}`; `run.logs` entry `{type:"mcp_servers",
  mcpServers:[...]}`. No `RunSummary` change.

UI: the chat header (next to the run-config pill) shows a small pill
(plug icon + `MCP 2`, or `MCP 2/3` plus a warning dot when any server is
dropped/failed) for the chat's **latest run only** -- an earlier run's
servers are never shown for a later run that reported none, and the pill is
scoped to the chat the pane shows. Clicking it opens a popover listing each
server by name with its transport and either "Active" or "Not active --
<human reason>". No value of any kind is displayed.

Acceptance criteria:

- M1. An ACP run with servers sends one `mcp_servers` event whose entries
  are `active` for the servers put on the wire and `dropped` with the right
  `reason` for transport-unsupported and unresolvable-command servers; a
  run with no applicable servers sends none.
- M2. A Claude-native run sends the initial all-`active` event, and a
  second event marking a CLI-reported failed server `failed` /
  `failed_to_start`; no failures means no second event.
- M3. A Codex run sends all servers `dropped` / `provider_unsupported`.
- M4. The event is persisted and survives replay: `run.logs` after the run
  and a late `run.attach` backfill both carry it; a daemon-restart
  rehydrated run still carries it.
- M5. Redaction (ADR-0018): with secrets seeded into env values, header
  values, args, command and url of every kind of server, the raw bytes of
  the `run.logs` response, the `run.attach` stream frames, and the
  persisted `run_events.event_data` row contain none of them (and none of
  the command/url/args either).
- M6. The UI timeline never renders `mcp_servers` as a transcript row or
  an "unknown event" placeholder; `RunEntry.mcpServers` is the latest
  event's list for that run.
- M7. The header pill appears only when the chat's latest run has
  `mcpServers`, shows the active/total counts, and flags dropped/failed;
  switching to another chat or a newer run without servers hides it.
- M8. The popover lists every server by name + transport + status/reason
  text and renders no env/header/command/url value; light and dark
  screenshots reviewed by hand.

Test scenarios:

- `TestAcpMcpServerStates_ActiveDroppedAndUnresolved` (`internal/taskrunner/mcp_test.go`)
  -- pure mapping of rows + capabilities to `[]McpServerState` (M1).
- `TestRunner_RunPrompt_ACPEmitsMcpServersEvent` /
  `..._ACPNoMcpServersEventWhenNoneConfigured` /
  `..._ClaudeNativeEmitsMcpServersAndFailedUpdate` /
  `..._ClaudeNativeNoSecondMcpEventWithoutFailures` /
  `..._CodexEmitsMcpServersProviderUnsupported` (M1-M3), through the real
  runner with the fakeagent / fake CLI.
- `TestEventCodec_McpServersRoundTrip` (`internal/runs/persist_test.go`) and
  `TestRegistry_McpServersEventSurvivesRehydrate` (M4).
- `TestRunLogs_McpServersEntry`, `TestRunAttach_McpServersStreamEvent`
  (`internal/wsapi`) (M4) and `TestMcpServersEvent_NoSecretReachesWire`
  (`internal/wsapi`, raw bytes of logs + attach frames + the stored row;
  seeds env, header, arg, command and url secrets) (M5).
- `appendTimelineEvent ignores mcp_servers`, `RunEntry.mcpServers folded
  from run.logs backfill and live stream (latest wins)` in
  `use-run-timeline` tests (M6).
- `McpServersIndicator` tests (`mcp-servers-indicator.test.tsx`): hidden
  without servers; `MCP 2` active-only; `MCP 1/3` + warning marker with
  dropped/failed; popover rows with reason text; DOM never contains a
  seeded secret (M7/M8).
- `TaskDetailPane` test: pill reflects the latest run only; a newer run
  with no servers hides it; a different chat's pane doesn't show it (M7).

### Follow-up: keep Claude-native MCP secrets off argv

Step 4 hands servers to the `claude` CLI as inline `--mcp-config <json>`, so
env values / auth headers are visible in `ps`/argv. ACP agents are not
affected (their `mcpServers` travel over the JSON-RPC stdio session, never
argv; the agent launch command in `internal/acp/rpc.go` carries no server
config), so this follow-up is Claude-native only.

Acceptance criteria:

- F1. With one or more applicable servers, `runClaudeNative` writes the
  config JSON to a file inside a fresh `0700` temp directory (file mode
  `0600`) and passes that file path to `--mcp-config`; the JSON itself never
  appears in argv.
- F2. The file contents are exactly the JSON `claudeMcpConfig` builds
  (unchanged format).
- F3. The temp dir and file are removed when the run ends: success, spawn
  failure, prompt failure, and ctx cancellation.
- F4. With no applicable servers no temp file/dir is created and
  `--mcp-config` is absent (behavior unchanged).

Test scenarios (`internal/taskrunner/mcp_test.go`):

- `TestRunner_RunPrompt_ClaudeNativeMcpConfigFileMode` -- file mode 0600,
  parent dir 0700, contents equal the expected JSON (observed by the fake CLI
  at spawn time).
- `TestRunner_RunPrompt_ClaudeNativeMcpConfigSecretNotInArgv` -- seeded env
  and header secrets are absent from every argv element; `--mcp-config` value
  is a path.
- `TestRunner_RunPrompt_ClaudeNativeMcpConfigRemovedAfterRun` -- temp dir
  empty after a successful run, a spawn failure, and a cancelled run.
- `TestRunner_RunPrompt_ClaudeNativePassesMcpConfig` (existing, adapted) and
  `..._WorkspaceRestriction` -- no servers => no `--mcp-config`, no temp
  files created.

## Decisions

- Follows `docs/decisions/0018-agent-mcp-servers.md` for the data model,
  storage scope (global registry + optional workspace-restriction join
  table), and per-backend wiring mechanism. ADR-0018 is **Accepted**
  (2026-09-28); its Resolved decisions section settles every question
  this plan's steps reference: Codex deferred (steps 6 and the Codex half
  of 8 are out of v1), `exec.LookPath` for bare commands, workspace
  restriction table without UI, plaintext + redaction, disabled servers
  invisible.
- Per-run indicator (step 7, second half): an additive run event
  (`EventTypeMcpServers`, enum append-only per ADR-0008) rather than a
  `RunSummary` field, because the event is persisted/replayed by the
  existing `run_events` path for free and `RunSummary` has no persistence of
  its own. Payload is names + transport + fixed status/reason enums only.
  The pill reflects the latest run, not a task-wide union.
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
2. [x] **wsapi + events + CLI.** `mcp.*` methods in
   `internal/wsapi/handlers.go`, `busMcpServerNotifier` in
   `internal/wsapi/server.go`, lifecycle event topics, redaction on every
   read path, `smind mcp add|ls|rm|enable|disable` in `cmd/smind`. Depends
   on step 1 only.
3. [x] **ACP wiring.** `internal/acp/client.go`'s `NewSession`/`LoadSession`/
   `ResumeSession` gain a real `mcpServers` parameter; `internal/acp`'s ACP
   struct-to-wire mapping (stdio/http entries, `env`/`headers` as
   `[]{name,value}`); capability-flag filtering
   (`agentCapabilityFlags` gains `Mcp.Stdio`/`Mcp.Http`, mirroring
   `LoadSession`/`SessionCapabilities.Resume`'s existing decode pattern);
   `internal/acp/fakeagent` test-introspection hook;
   `internal/taskrunner.Runner` resolves the enabled rows for a run's
   workspace and passes them through. Depends on step 1; independent of
   step 2 (can run in parallel).
4. [x] **Claude native wiring.** `internal/taskrunner.Runner`'s
   `claudecode.New(worktreePath, opts...)` call gains
   `claudecode.WithMCPConfig(json)`, built from the same resolved rows as
   step 3. Small, depends on step 1 only.
5. [x] **`npx`/absolute-path resolution.** `exec.LookPath` resolution at the
   ACP mapping layer (step 3); unresolvable command -> skipped with a clear
   session note (review fix: it no longer fails the run). Small.
6. ~~Codex mechanism~~ -- **deferred out of v1** (ADR-0018 resolved
   decision 1). Codex runs report configured servers as unsupported.
7. **UI.** Settings section for CRUD (mirroring Accounts/Profiles
   settings screens) + per-run "active MCP servers" indicator surfacing
   capability-dropped servers (ACP) per ADR-0018's wsapi/UI section.
   Depends on step 2; can start once step 3's capability-drop signal has
   somewhere to report to.
   - [x] **Settings CRUD half** (feat/agent-mcp-settings-ui): Settings ->
     MCP servers (`mcp-servers-section.tsx`). Shipped separately from step 3.
   - [ ] **Per-run "active MCP servers" indicator** (spec above, M1-M8).
     Previously only a session note on the run timeline. Sub-steps:
     7a backend event + persistence + wire (M1-M5); 7b UI pill + popover
     (M6-M8).
8. [x] **Secret-redaction audit.** A focused pass confirming acceptance
   criterion 9 end to end for ACP and Claude native (Codex deferred per
   ADR-0018 resolved decisions 1 and 5). Depends on steps 3, 4.

9. [x] **Follow-up: Claude-native `--mcp-config` via 0600 temp file** (fix/mcp-config-secrets-off-argv), see the Follow-up subsection under Test Scenarios.

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
- AC3 (wsapi CRUD): `internal/wsapi/mcp_test.go` — `TestMcp_
  CreateGetRoundTrip`, `TestMcp_ListOrdering` (id-ordered), `TestMcp_
  UpdateVisibleFromSecondConnection` (full-record replace), `TestMcp_
  DeleteThenGetErrors`, `TestMcp_SetEnabledToggles`, `TestMcp_
  CreateDuplicateNameIsConflictError` (name conflict as a clear RPC error,
  not a silent overwrite).
- AC3/AC9 read-path redaction (wsapi half): `TestMcp_RedactionOnEveryRead
  Result` — mcp.create/get/list/update results asserted against the raw
  JSON bytes, never containing the seeded secret; `TestMcp_
  ListDisabledServersStillVisible` (resolved decision 7: disabled rows
  stay in mcp.list).
- AC4 (lifecycle events): `TestMcp_RedactionOnLifecycleEvents` —
  mcpServer.created/updated asserted against the raw inbound WebSocket
  frame bytes on a watch-only connection, redacted identically to the RPC
  results.
- AC8 (CLI): `cmd/smind/mcp_servers_test.go` — `TestRunMcpAddPrints
  CreatedRow` (add→stored row, canonical args/env JSON, secret never
  printed), `TestRunMcpLsShowsRedactedValues` (ls shows TOKEN key but
  never the raw value), `TestRunMcpRmRemovesServer`, `TestRunMcp
  EnableDisableToggles`, `TestRunMcpAddDuplicateNamePrintsConflictError`
  (daemon's conflict error surfaced), `TestRunMcpAddInvalidTransport
  ExitsNonzero`; dispatch contract in `TestMcp_Usage` (mcp_serve_test.go).
  Verified green: `task test`, `task lint`.
- Step 7 (Settings CRUD half): `web/packages/ui/src/components/settings/
  mcp-servers-section.test.tsx` -- list with transport badge never rendering
  secret values, empty state, create stdio (env as object) / http (url +
  headers, no command), per-transport validation before any RPC, enabled
  toggle -> `mcp.setEnabled`, inline delete confirmation -> `mcp.delete`,
  edit keeps `[redacted]` placeholders (exact `mcp.update` params asserted),
  daemon conflict error inline, live `mcpServer.*` events, Playwright
  quick-fill. Verified: `bunx tsc -b packages/ui`, full UI vitest suite,
  `task lint`. Manually verified against a sandbox daemon
  (`SMIND_HOME` temp, real built UI, headless Chromium): light + dark
  screenshots of list / edit / new / delete-confirm; the page DOM never
  contained the seeded secret; editing one env value in the UI left the
  other stored secret intact (read back from SQLite).
- Update-semantics fixes (code review, post-step-2): `internal/store/`
  `TestStore_UpdateMcpServerPreservesEnabled` — update never touches the
  enabled column (only `SetMcpServerEnabled` owns it): enabled stays
  enabled across an edit, disabled stays disabled. `internal/wsapi/`
  `TestMcp_UpdatePreservesEnabled` pins the same end to end over mcp.update
  + mcp.setEnabled. `TestMcp_UpdatePlaceholderRoundTripKeepsSecrets` —
  round-tripping an mcp.get result back through mcp.update keeps the stored
  secret (asserted via the store), and changing one key while another stays
  `[redacted]` replaces only the changed one. `TestMcp_
  UpdatePlaceholderForNewKeyIsError` — the placeholder on a new key is a
  clear error naming the key, containing no secret, with the stored record
  untouched.
- AC5/AC10 (ACP wiring, steps 3+5): `internal/acp` -- `TestClient_NewSessionSendsMcpServers`,
  `TestClient_LoadSessionAndResumeSendMcpServers`, `TestClient_NilMcpServersSendsEmptyArray`
  (literal `[]`, never `null`), `TestClient_McpCapabilityFlags` (real GLM v1 shape, v2 object
  shape, malformed). `internal/taskrunner/mcp_test.go` -- `TestAcpMcpServers_MapsStdioAndHttpWireShape`,
  `..._DropsTransportAgentDoesNotAdvertise`, `..._ResolvesBareCommandToAbsolutePath`,
  `..._UnresolvableCommandIsSkippedWithoutLeakingSecrets`, `TestRunner_RunPrompt_ACPUnresolvableMcpCommandDegradesToNote`, and end to end through the real
  `acp.Client` + fakeagent: `TestRunner_RunPrompt_ACPSendsConfiguredMcpServers` (absolute
  command; other-workspace server absent), `..._ACPNoMcpWhenNoneConfiguredIsEmptyArray`
  (regression guard), `..._ACPInactiveMcpServerEmitsSessionNote`, `..._ACPResumePathCarriesMcpServers`.
  Mutation-checked by hand: disabling the capability gate, the LookPath resolution, or the
  `--mcp-config` option each fails a test.
  **Spec deviation (capability semantics):** the real `glm-acp-agent` initialize response
  (captured live) is `agentCapabilities.mcpCapabilities: {"http": true}` -- ACP v1, bool-valued,
  stdio never advertised because v1 makes it mandatory. Taken literally, "drop stdio unless
  `mcp.stdio` is advertised" would have dropped Playwright for GLM. Implemented: v1 agents
  (no top-level `mcp` key) support stdio unconditionally and http/sse per `mcpCapabilities`;
  v2 agents (`mcp` object) are gated per transport object. So the "agent advertises no mcp
  capability => every server dropped" scenario became "stdio kept, http/sse dropped".
  The capability-dropped signal is an `EventTypeSessionNote` (server names only).
- AC6 (Claude native, step 4): `TestClaudeMcpConfig_BuildsMergedMcpServersJSON`,
  `TestRunner_RunPrompt_ClaudeNativePassesMcpConfig` (argv has `--mcp-config` + a file path whose
  content is the exact JSON with a row, absent without), `..._ClaudeNativeWorkspaceRestriction`.
  Originally the JSON was inline in argv; superseded by the file-based follow-up (F1-F4 below).
- AC7 (Codex, deferred): `TestRunner_RunPrompt_CodexNoteMcpServersUnsupported` -- starts
  normally, session note names the servers, no secrets in events.
- AC9 (step 8 audit): wsapi responses + lifecycle events covered in step 2; runner side:
  `internal/acp` never logs request params (only the agent's stderr goes to the logWriter);
  `TestRunner_RunPrompt_ACPInactiveMcpServerEmitsSessionNote` and
  `TestRunner_RunPrompt_ACPActiveMcpServerSecretNeverLeaksOutsideWire` (secrets delivered on
  the wire yet absent from every emitted Event -- what `internal/runs` persists as
  run_events -- and from daemon log output). Claude-native argv exposure is closed by
  the F1-F4 follow-up. Not covered: secrets echoed back by a misbehaving agent in its own error text.
- Step 6 (Codex) remains deferred out of v1 per ADR-0018 resolved decision 1.

### Review fixes (PR #251)

- An unresolvable stdio command degrades to a session note instead of failing
  the run (a deliberate refinement of ADR-0018 resolved decision 2's "fails the
  session setup"; the ADR text is unchanged and may want a follow-up edit).
- Claude-native: failed servers reported in the CLI's `init` message
  (`mcp_servers[].status == "failed"`) surface as a session note (names only);
  `TestRunner_RunPrompt_ClaudeNativeFailedMcpServerEmitsNoteWithoutSecrets`
  also audits events/logs for secrets.
- Known env/header/arg values are scrubbed from agent error text in session
  notes and run errors (`TestRunner_RunPrompt_ACPSessionErrorsScrubMcpSecrets`).
- `internal/acp` puts the agent in its own process group (unix) and kills the
  group on close timeout (`TestConnClose_KillsGrandchildrenOnTimeout`).

### Follow-up validation: Claude-native secrets off argv (F1-F4)

- F1/F2: `TestRunner_RunPrompt_ClaudeNativeMcpConfigFileMode` (file 0600, dir
  0700, content == `claudeMcpConfig` output, observed by the fake CLI at
  spawn) and `TestRunner_RunPrompt_ClaudeNativeMcpConfigSecretNotInArgv`
  (no `SUPERSECRET` in any argv element; `--mcp-config` is an absolute path).
- F3: `TestRunner_RunPrompt_ClaudeNativeMcpConfigRemovedAfterRun` (success,
  spawn failure, cancelled run; `TMPDIR` pointed at an empty test dir that must
  be empty afterwards).
- F4: `TestRunner_RunPrompt_ClaudeNativeNoMcpServersCreatesNoTempFile` plus
  the existing no-rows / workspace-restriction assertions.
- ACP agents checked: no argv exposure (servers go over the JSON-RPC session);
  unchanged.
- Implementation: `writeClaudeMcpConfigFile` (`internal/taskrunner/mcp.go`),
  cleanup deferred in `runClaudeNative`.
