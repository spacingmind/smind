# Mid-turn steering (ADR-0021 amendment 2026-11-09)

Status: **spec, not started.** Nothing is implemented until the Human answers the decisions D1-D10 in the
[ADR-0021 amendment](../../decisions/0021-per-chat-prompt-queue.md#amendment-2026-11-09--mid-turn-steering-proposed). Size: L (wsapi, store event, provider runners, UI on two
surfaces): a validation contract (ADR-0045) per PR.

## Acceptance Criteria

- AC1 `whenBusy: "steer"` on a busy claude-native chat injects the text into the running turn: the run continues, no new run, result `{steered, runId, steerId}`.
- AC2 Same on codex-native through `turn/steer` with the active turn id; a turn-id mismatch falls back to the queue.
- AC3 On a provider without steer (ACP) the call degrades to `queue` with `steerFallback` and a reason in the result (D2).
- AC4 A steer is never lost: write failure, a finished run or a mismatch enqueues the text as a queue item exactly once; the event shows `queued`.
- AC5 `user_steer` event with `sent` / `echoed` / `queued` / `failed`; Claude runs add `--replay-user-messages` and the steer is anchored at the echo; old clients ignore the event.
- AC6 Only human and orchestrator sources steer; agent-source and remote callers are downgraded to `queue`; a steer changes no setting, grants no permission, cannot answer a
  permission or question.
- AC7 Text only, size cap, one steer in flight per run; attachments, model and mode changes are refused with a clear error.
- AC8 Catalog flags `supportsSteer` / `supportsInterrupt`; the composer shows a Steer button only when supported, on web and mobile; CLI `--when-busy steer`; MCP `whenBusy`.
- AC9 `interrupt` semantics unchanged for Claude and ACP; for Codex `turn/interrupt` only if D5 is accepted.
- AC10 The probe's recorded Claude behaviour (injection after the tool result in the same turn, one result; interrupt gives two results) is pinned by fixtures.

## Test Scenarios

PR1 (claude + core; `internal/runs`, `internal/taskrunner`, `internal/wsapi`, `cmd/smind`, fake CLI replaying the probe fixtures):
`TestSteer_ClaudeInjectsWhileTurnRuns`, `TestSteer_ClaudeSingleResultStillOneResult` (fixture of the s1 stream), `TestSteer_EchoAnchorsEvent`, `TestSteer_WriteFailureEnqueuesOnce`,
`TestSteer_RunFinishedInRaceEnqueues`, `TestSteer_NeverDuplicatesOnFallback`, `TestSteer_OneInFlightPerRun`, `TestSteer_RejectsAttachmentsAndSettingChanges`,
`TestSteer_AgentAndRemoteSourcesDowngradedToQueue`, `TestSteer_CannotReachRespondPermission` (walks the handler map and MCP tools), `TestSteer_UnsupportedProviderDegradesToQueue`,
`TestSteer_IdleChatStartsNormally`, `TestSteer_NewEventIgnoredByOldDecoder`, `TestClaudeStdinWritesAreSerialized` (race detector), `TestCatalog_SupportsSteerFlags`,
`TestWhenBusy_InterruptUnchanged`, `TestCLI_TaskSendWhenBusySteer`.
PR2 (codex; fake app-server): `TestCodex_TurnSteerUsesActiveTurnID`, `TestCodex_TurnSteerMismatchFallsBackToQueue`, `TestCodex_TurnIDTrackedFromTurnStart`, and if D5: `TestCodex_InterruptUsesTurnInterrupt`,
`TestCodex_InterruptKeepsThread`.
PR3 (UI): composer tests (button present only when supported, Enter still queues, alternate shortcut still interrupts, fallback copy), timeline marker for each event state, light and
dark frames for every state plus the transition frames, mobile send-sheet action tests.
PR4 (optional, D6): ACP `session/cancel` then prompt, behind its own decision.

Mutations (committed code, one at a time; the named test must then fail): the enqueue-on-failure branch removed → `TestSteer_WriteFailureEnqueuesOnce`; the source downgrade removed →
`TestSteer_AgentAndRemoteSourcesDowngradedToQueue`; `--replay-user-messages` not passed → `TestSteer_EchoAnchorsEvent`; the in-flight guard removed → `TestSteer_OneInFlightPerRun`;
the Codex turn id read from a stale value → `TestCodex_TurnSteerMismatchFallsBackToQueue`.

## PR split and write scopes

1. **PR1:** mode, result shape, event, catalog flags, Claude steer, CLI/MCP/wsapi, `--replay-user-messages`. (`internal/runs`, `internal/taskrunner`, `internal/wsapi`, `cmd/smind`)
2. **PR2:** Codex `turn/steer` (and `turn/interrupt` if D5). (`internal/codex`, small runs glue). Needs the Codex probe first (D8, ask before running).
3. **PR3:** UI on web and mobile after PR1. Parallel with PR2.
4. **PR4:** ACP interrupt-by-cancel, only if D6 is reversed.

One PR at a time, each stops for the Human's review. The extra Haiku probe (D9) runs before PR1; the probe script and results stay under the dogfood directory, not the repo.

## Decisions

Proposed as D1-D10 in the ADR-0021 amendment; none taken yet.

## Progress

- 2026-11-09: amendment drafted (Proposed); probe run (two Haiku runs, about USD 0.013); spec written.

## Validation

Pending implementation.
