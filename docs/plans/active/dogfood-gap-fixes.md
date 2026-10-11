# Dogfood gap fixes (G1, G3–G6, plus the G4 `task_wait` ergonomics)

Spec for the "Dogfood gap fixes" subsection of
[orchestration-and-metering.md](orchestration-and-metering.md). It lives in
its own file because that plan's gap log (the "Step 3 dogfood gap log",
G1–G11) is still on PR #265's branch; appending here avoids a merge conflict
at the end of that file. Fold it back in when #265 lands.

Source: the Step 3 dogfood run (2026-10-11), one GLM peer driven through
`smind mcp serve`. Gap numbers below are that log's.

## Scope

In: G1 (MCP output schema), G4 (`task_wait` while a permission is pending),
G3 (write-permission preview), G6 (CLI reject), G5 (GLM turn cap + stop
reason), G8 (suspicious GLM usage).

Out (not built here): G2 auto-approval policy (ADR-0017/0019 stand; no
approval tools, no auto-approving profiles); G7 review tools; G9–G11.

## Findings from investigation (before any code)

- **G5/G8 root cause is the pinned agent version.** `internal/acp/glm.go`
  pins `glm-acp-agent@1.3.0`. In 1.3.0 `maxTurns` is `options.maxTurns ?? 20`
  with no CLI flag and no env var: `ACP_GLM_MAX_TURNS` never existed there,
  and the cap is **20 model turns** (many tool calls fit in one turn, which
  is how ~76 tool calls ended in `max_turn_requests`). The gap log's "100"
  and `parseMaxTurnsFlag` come from the newest published version (1.14.0),
  not from the pinned one.
- `--max-turns` appears in 1.6.1 (default 20); the default becomes 100 in
  1.9.0; per-prompt summed usage (`totalUsage`) appears in 1.11.0.
- **Measured, not just read** (scratch harness `/tmp/leader-glm-run/harness`:
  the real agent binary against a local fake OpenAI-style SSE server that
  emits tool calls and per-call usage; no Z.AI quota needed):
  - 1.3.0 with `--max-turns 4` and `ACP_GLM_MAX_TURNS=4`: still 20 model
    calls, then `max_turn_requests`. Both are ignored. This is the dogfood's
    cause (the cap was 20 model turns, not 100).
  - 1.14.0: `--max-turns 3` stops after 3 calls; `ACP_GLM_MAX_TURNS=4`
    without a flag also works (the gap log's "the CLI default shadows the
    env var" does not hold for 1.14.0 -- the flag is absent -> env applies).
  - Usage: 1.3.0's `PromptResponse.usage` is the **last model call only**
    (6 calls of 1k..6k prompt tokens -> 6000). 1.14.0's is the **sum of that
    prompt's calls** (21000), and it is **per prompt, not session-cumulative**
    (a second prompt on the same session reports only its own calls: 57000,
    not 78000). The 1.3.0 shape is exactly the dogfood's
    `input=81403 output=97`.
- **ADR-0020 differencing vs this**: ADR-0020 says ACP `PromptResponse.usage`
  is session-cumulative and differences it. For GLM 1.14.0 that is untrue,
  but it does no harm today: smind spawns one agent process per run, the
  snapshot's `instance` differs, so the baseline resets and each run is
  charged the whole figure the agent reports (= that prompt's total, which
  is right). Differencing would under-count if one process ever served two
  prompts. So: no differencing change needed for GLM 1.14.0; the real bug is
  the pinned 1.3.0 (last call only), fixed by the pin bump. We document the
  per-prompt semantics in ADR-0020 and pin it with a test.
- GLM's `accept_edits` mode is **not** flagged `autoApproves`
  (`acpBypassLike` matches only bypass/yolo/full/auto/dangerous/skip), so an
  orchestrator may pick it itself. It only auto-allows `write`-kind tool
  calls; `Run command` (`grep`, `ls`, `sed`) still prompts, so it does not
  remove most of the 43 prompts.

## Decisions

1. **Bump the GLM pin** to a version with `--max-turns` and cumulative usage
   (target `1.14.0`, latest; the harness above already exercises its ACP
   handshake, modes, tool loop, cap and usage against a fake model).
   Smaller than any smind-side workaround; there is no way to honour a turn
   cap on 1.3.0. Recorded as a dependency choice, not an architecture one.
2. **`agents.glm.maxTurns` config key** (int, `0`/unset = the agent's own
   default) passed as `--max-turns N`. No profile field (not trivially
   additive: profiles are provider-agnostic).
3. **Permission detail is an additive optional `detail` object on the
   `permission_request` event** (same additive-field pattern as ADR-0008),
   computed daemon-side so web, CLI and MCP share one source. The existing
   `summary` string also gets the headline (size, `OVERWRITES`), so every
   client benefits with no change. Old clients ignore `detail`.
4. **`task_wait` gets `state`**, a derived field with values
   `waiting_permission | running | done | error | stopped | timed_out`;
   `status` keeps its current values (additive, nothing breaks).
5. **No approval tools, no auto-approve policy** (ADR-0017, ADR-0019 stand,
   confirmed by the user and supervisor).

## Acceptance criteria and named tests

### AC1 (G1 blocker): MCP `task_status`/`task_logs` pass output validation
- `task_status` and `task_logs` return success, with the full event
  content, for a run that has `tool_call` (with `input` and `result`),
  `permission_request`, `permission_resolved`, `chunk`, `thinking`, `raw`
  (with `payload`) and `usage` events.
- The schema is fixed by correcting the event's declared shape (`input`,
  `result`, `payload` are `any`/object-or-scalar JSON), **not** by
  disabling or loosening output validation.
- Tests (stdio or in-memory MCP client against a fake wsapi): 
  `TestMCPRoundTrip_TaskStatusWithEvents`,
  `TestMCPRoundTrip_TaskLogsWithEvents`, and
  `TestMCPRunEventOutputSchema_JSONFieldsAreNotArrays` (inspects the
  advertised schema in `tools/list`).

### AC2 (G4): `task_wait` is unambiguous while a permission is pending
- Returns `state: "waiting_permission"` plus `pendingPermissions` (all
  pending request ids with summaries, not just the first; the existing
  `pendingPermission` field stays as the first one) and a `message` telling
  the driver to surface it to the human and call `task_wait` again.
- A run that is finished returns `state: "done"`; a timeout returns
  `state: "timed_out"`, `timedOut: true`.
- Tool descriptions for `task_wait`/`task_status`/`task_permissions` say
  the loop explicitly: "wait, then (a human approves), then wait again";
  `waiting_permission` is not completion. No approval tool is added
  (`TestMCPCatalog` already pins the name set; it must not grow).
- Tests: `TestTaskWait_PendingPermissionReportsWaitingPermission`,
  `TestTaskWait_DoneReportsDoneState`,
  `TestTaskWait_TimeoutReportsTimedOut`,
  `TestTaskWait_ListsEveryPendingPermission`,
  `TestMCPToolDescriptions_TaskWaitLoopWording`.
- Server instructions (PR #264) get the same wording: delivered as a
  separate small PR stacked on #264, since `mcp_instructions.go` is not on
  develop.

### AC3 (G3): a write permission shows what it will write
- Daemon computes, for ACP tool calls of kind `edit` (and `write`-like
  `Write file`): target path, new size (bytes, lines), existing size when the
  file exists inside the task worktree, `replacesExisting`, a
  `largeShrink` flag (existing file replaced by content under half its line
  count, existing >= 20 lines), a capped preview of the new content, and a
  capped unified diff against the existing file.
- Caps: preview <= 4 KiB / 60 lines, diff <= 8 KiB / 200 lines, command text
  <= 2 KiB; truncation is flagged (`truncated: true`). Existing file read at
  most 1 MiB.
- **No secrets beyond the tool input**: preview text comes from the tool
  input only; the diff (which carries existing-file lines) and existing
  stats are omitted when the path is outside the task worktree (including
  via symlink) or the name looks sensitive (`.env*`, `*.pem`, `*.key`,
  `id_rsa*`, `*credentials*`, `*secret*`), in which case only
  `redacted: true` + counts of new content are given.
- `summary` becomes e.g. `Write file: internal/store/schema.sql
  (replaces 240 -> 7 lines, 3.1 KB -> 0.2 KB) OVERWRITES`.
- Shown in: web permission card (headline warning when `replacesExisting`,
  `largeShrink` styled as a danger callout, collapsible diff/preview), CLI
  `task permissions` (and the `[permission]` stream line), MCP
  `task_permissions`/`task_wait` (`detail` object).
- Tests (Go): `TestPermissionDetail_WriteReplacesExistingFile`,
  `TestPermissionDetail_WriteNewFile`,
  `TestPermissionDetail_LargeShrinkFlag`,
  `TestPermissionDetail_CapsPreviewAndDiff`,
  `TestPermissionDetail_SensitivePathRedactsExisting`,
  `TestPermissionDetail_PathOutsideWorktreeRedactsExisting`,
  `TestPermissionDetail_SymlinkEscapeRedacts`,
  `TestPermissionDetail_NonWriteToolHasNoDetail`,
  `TestRunsPermissionRequestEventCarriesDetail` (event persisted and
  replayed via `run.logs`), `TestRenderPermissionRequest_ShowsDetail`,
  `TestMCPTaskPermissions_IncludesDetail`.
- Tests (web): `renders the overwrite warning for a replacing write`,
  `renders the size delta and a collapsible diff`,
  `renders nothing extra for a permission with no detail`.
- Light + dark screenshot of the card (replacing write, large shrink),
  checked by eye.

### AC4 (G6): CLI can pick a specific option, including reject
- `smind task approve <runId> [requestId] [optionId]` accepts the third
  argument and validates it against the request's options; an unknown id
  lists the valid ones. `reject` / `Skip` works. Also accept `--help`/`-h`
  (prints usage, exit 0) instead of treating it as a run id.
- Tests: `TestTaskApprove_ExplicitRejectOption`,
  `TestTaskApprove_UnknownOptionListsValid`,
  `TestTaskApprove_ThreeArgsAccepted`, `TestTaskApprove_HelpFlag`.
  The existing no-option default (first allow option) keeps passing.

### AC5 (G5): honour a GLM turn cap; do not show a capped run as plain done
- `internal/acp/glm.go` pins the new version; `agents.glm.maxTurns` (config)
  is appended as `--max-turns N` when > 0.
- A run whose stop reason is `max_turn_requests` (or `max_tokens`) is shown
  with a distinct, explicit label: web timeline "Turn limit reached"
  (warning style) instead of the raw word; CLI prints
  `done (max_turn_requests: hit the agent's turn limit; send "continue")`;
  MCP `task_wait`/`task_status`/`task_logs` add `stopNote`.
  `state`/`status` on the wire stay `done` (no new terminal status).
- Tests: `TestGLMCommand_DefaultsPinnedVersion`,
  `TestGLMCommand_MaxTurnsFlag`, `TestConfig_GLMMaxTurns`,
  `TestStopNote_MaxTurnRequests`, `TestMCPTaskWait_StopNoteOnTurnCap`,
  `TestRenderDone_TurnCapHint`; web `labels a max_turn_requests run as
  turn limit reached`.
- Live check: a sandbox daemon with the new pin starts a GLM run and the
  agent reports a cap when `maxTurns` is 1 or 2 (credentials from real HOME).

### AC6 (G8): GLM usage is right
- Raw capture of the real agent (fixture, from the harness above): a
  multi-turn prompt, a second prompt on the same session, and a
  `max_turn_requests` prompt, at 1.14.0.
- Tests: `TestDiffACPUsage_GLMPerPromptTotalsNewInstancePerRun` (two runs,
  different agent instances, per-prompt totals -> each run is charged its own
  total), `TestDiffACPUsage_GLMMaxTurnRunCountsAllTurns` (the capped run's
  usage is the sum of its turns), and a test that decodes the captured raw
  `PromptResponse.usage` JSON (camelCase `inputTokens`, `cachedReadTokens`,
  `thoughtTokens`) through `acp.PromptUsage` into the expected numbers.
- ADR-0020 gets a "GLM agent versions" note: < 1.11 reports the last call
  only (unusable; the pin must stay >= 1.11), >= 1.11 reports the prompt's
  summed total, not session-cumulative, and why that is still right under
  process-per-run.

### AC7 (mode guidance, supervisor addendum)
- Server instructions (stacked PR on #264) and tool descriptions say: to cut
  prompts use `profileId` of a human-authored profile with the right mode,
  or a non-auto-approving mode an orchestrator may pick itself (GLM
  `accept_edits`, which only covers file writes, not `Run command`).
- `acpBypassLike("accept_edits")` is false and pinned by
  `TestModeAutoApproves_GLMAcceptEditsIsNotAutoApproving`.
- The rejection error for an orchestrator that asks for an auto-approving
  mode names the profile path: `runs: start: a non-human caller may not pick
  an auto-approving permission configuration (ADR-0019); ... use a
  human-authored profile (pass its id as profileId; see profile_list)`.
  Test: `TestStartWhenBusy_AutoApprovingErrorPointsToProfiles`.

### AC8: no regressions
`task test`, `task lint`, `cd web && bunx tsc -b packages/ui` all pass.

## Progress

- [ ] spec committed
- [ ] WP1 MCP + CLI (AC1, AC2, AC4)
- [ ] WP2 daemon permission detail (AC3 Go side)
- [ ] WP3 GLM pin/config/usage/stop note (AC5, AC6, AC7 error text)
- [ ] WP4 web card + stop label + screenshots (AC3 web, AC5 web)
- [ ] CLI/MCP rendering of detail (AC3)
- [ ] stacked PR on #264 for instructions (AC2, AC7)

## Validation

(filled in as criteria are confirmed)
