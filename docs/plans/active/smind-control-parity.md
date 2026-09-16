# smind control/orchestration parity with Paseo/dsh (Phase 3.1)

Closes two of the three control-plane gaps found while dogfooding
`smind task` as an orchestrator against `refs/paseo`'s `agent-manager.ts`
and `refs/deepseek-harness`'s `subagent`/`interaction` packages (see
[[project_phase3_relay_e2ee]] memory, or ask for the comparison if that
memory doesn't exist): live mid-session config mutation, and a task
parent/child hierarchy. This plan does **not** attempt mid-turn steering
(Paseo's `steerActiveTurn`) — ACP v2's method list
(`refs/agent-client-protocol/agent-client-protocol-schema/src/v2/agent.rs`)
has no verb to inject input into an in-flight `session/prompt` call, and
`claude-agent-sdk-go`'s `Client` (`/home/longnp/Coding/personal/claude-agent-sdk-go/client.go`)
exposes only `Prompt`/`Close`, no interject primitive either. Building
that would mean extending a wire protocol and an external SDK this repo
doesn't own; out of scope here, revisit only if a provider adds real
support.

## Acceptance Criteria

- **ACP config-option discovery**: `internal/acp.Client.NewSession`
  captures and returns the `config_options` array from `session/new`'s
  response (currently discarded — `internal/acp/client.go:218`) instead
  of only the session ID. Each option carries at least an id, a
  human-readable label, and (when the agent sent one) a category and
  current value, mirroring ACP's `SessionConfigOption` shape
  (`refs/agent-client-protocol/agent-client-protocol-schema/src/v2/agent.rs`
  around `SessionConfigOption`/`SessionConfigOptionCategory`).
- **ACP config-option mutation**: `internal/acp.Client` gains a method
  that sends `session/set_config_option` (the real ACP v2 method name --
  see `SESSION_SET_CONFIG_OPTION_METHOD_NAME` in the same file) for a
  live session and returns whatever acknowledgement/error the agent
  sends back. Calling it on GLM's spawned CLI (`internal/acp/glm.go`)
  with a thinking-related option id actually changes the session's
  configured thinking level for the *next* turn on that same session --
  this is the fix for the GLM-thinking-mode dogfood finding (no
  config knob existed anywhere in smind before this).
- **Runner/registry wiring**: `internal/taskrunner.Runner` exposes the
  discovered config options for a run's session (populated once, at
  session creation) and a way to set one; `internal/runs.Registry` gets
  a method that resolves a running (or just-created) run's task ID to
  its live ACP session and calls through. Claude Code (`claude-native`)
  and Codex (`codex-native`) runs simply report zero config options and
  reject a set-option call with a clear "not supported for this
  provider" error -- this is an ACP-specific capability, not a
  cross-provider one, and should say so rather than silently no-op.
- **wsapi + CLI**: two new wsapi methods (`run.listConfigOptions`,
  `run.setConfigOption`) and two new CLI subcommands: `smind task
  options <runId>` (list, empty output + a note for a non-ACP provider)
  and `smind task set-option <runId> <optionId> <value>`. Follows the
  same wire-shape-duplication convention `task permissions`/`task
  approve` already established in `cmd/smind/task.go`.
- **Task hierarchy**: `store.Task` gains an optional `ParentTaskID
  *int64`. `task.create` (and `smind task new`) accepts an optional
  parent task id in the same workspace; a parent from a *different*
  workspace, or a nonexistent parent id, is rejected with a clear error
  --- no cross-workspace or dangling parent references. `task.list` (and
  `smind task ls`) can filter to a single parent's direct children via
  an optional `parentTaskId` argument; omitting it keeps today's
  behavior (all tasks in the workspace, flat). No depth limit, no
  cold-resume/inbox semantics, no automatic cascade-behavior on parent
  archive/delete beyond what already exists for any task -- this is
  intentionally the minimal "tasks form a tree, and you can list a
  node's children" primitive, not dsh's full continuable-child model.
- Out of scope this pass: mid-turn steering (see above); any change to
  `claude-agent-sdk-go` itself (a separate repo); a generic
  cross-provider "mode" abstraction beyond what ACP's config-options
  already model; UI (web) surfacing of either feature -- CLI/wsapi only
  for now, matching how `task permissions`/`task approve` shipped.

## Test Scenarios

- Go: `internal/acp` fake-agent harness (`internal/acp/fakeagent`) --
  `session/new` response carrying two `config_options` (one with a
  category, one without) round-trips through `Client.NewSession` intact,
  in order, with no field silently dropped.
- Go: `internal/acp` fake-agent -- calling the new set-config-option
  client method sends a well-formed `session/set_config_option` request
  (session id + option id + value, matching ACP's real field names) and
  correctly surfaces both a success acknowledgement and an agent-side
  error response (e.g. unknown option id) as a Go error, not a panic or
  silent no-op.
- Go: `internal/runs` -- `ListConfigOptions`/`SetConfigOption` (or
  whatever the chosen method names end up being) on a `claude-native` or
  `codex-native` run return the documented "not supported" outcome
  (empty list / clear error) rather than attempting an ACP call that
  doesn't apply to that provider.
- Go: `internal/store` -- creating a task with a `ParentTaskID` pointing
  at a task in a different workspace is rejected; pointing at a
  nonexistent task id is rejected; pointing at a valid same-workspace
  task succeeds and that parent's task list (filtered by parent id)
  includes the new child.
- CLI (`cmd/smind`): `task options <runId>` against a live GLM run
  prints at least one option with an id and label; against a
  `claude-native` run prints the "not supported" note instead of an
  empty table with no explanation. `task set-option <runId> <optionId>
  <value>` against an unknown `optionId` prints the daemon's rejection
  reason, not a bare non-zero exit with no message.
- Manual/live smoke test (not automatable without a live GLM account):
  start a real `glm` task through smind, run `task options` to find its
  thinking-level option id, `task set-option` it to a lower level mid-
  task, and confirm (via `task logs`) the next turn actually reflects
  the change rather than silently ignoring it -- this is the concrete
  validation that the GLM-thinking-mode dogfood problem is actually
  fixed by this plan, not just plausible from reading the protocol.

## Decisions

No ADR needed -- this wires up an existing accepted protocol capability
(ACP v2's `session/set_config_option`, already part of
`refs/agent-client-protocol`, a dependency this repo already speaks) and
adds an optional nullable column + a filter argument to an existing
table/list call, neither of which changes any decided architecture.
Naming of the two new wsapi methods/CLI subcommands is left to
implementation judgment as long as it's consistent with the
`task permissions`/`task approve` precedent already in `cmd/smind/task.go`.

## Progress

- [ ] `internal/acp.Client.NewSession` captures `config_options` from
      the response
- [x] `internal/acp.Client` gains a `session/set_config_option` method
- [ ] `internal/taskrunner.Runner` + `internal/runs.Registry` wiring
      (list + set, ACP-only, clear "not supported" for other providers)
- [ ] wsapi: `run.listConfigOptions`, `run.setConfigOption`
- [ ] CLI: `smind task options <runId>`, `smind task set-option <runId>
      <optionId> <value>`
- [ ] `store.Task.ParentTaskID` + validation (same-workspace, exists)
- [ ] `task.create`/`smind task new` accept an optional parent task id
- [ ] `task.list`/`smind task ls` accept an optional parent-id filter
- [ ] Tests (fakeagent round-trip, runs "not supported" path, store
      parent validation, CLI option/set-option)
- [ ] Manual smoke test against a real GLM task (see Test Scenarios)
- [ ] Verification

## Validation

- Step 2 (`SetSessionConfigOption`): wire field names taken from
  `SetSessionConfigOptionRequest` in
  `refs/agent-client-protocol/agent-client-protocol-schema/src/v2/agent.rs`
  (`session/set_config_option`, params `sessionId`/`configId` + flattened
  `type`/`value`; the method sends `type: "id"` values). Verified with
  fakeagent tests: `TestClient_SetSessionConfigOptionRequestShape` (echo
  proves the request's wire field names), `...Success` (ack round-trips),
  `...AgentError` (JSON-RPC error becomes a Go `*RPCError`). `go build
  ./...`, `go vet ./...`, gofmt, and `go test ./internal/acp/...` all clean.
