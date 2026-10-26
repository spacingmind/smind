# Automations (signal to task) — implementation plan for ADR-0044

Binding text: [ADR-0044](../../decisions/0044-automations.md) (Accepted 2026-10-27, all twelve answers as recommended). This file is the spec for the code; the ADR owns the *why*.
Size: L (8) — scope 2 (daemon + web + mobile read-only), PRs 2 (four, with ordering), risk 2 (unattended agent launch, security rails), decisions 0 (ADR accepted), verification 2 (scripted-`gh` live daemon run, UI screenshots).

## Acceptance criteria

Rails and authority (daemon, PR 1):
- AC1 `automations.enabled` defaults false; with it false the engine does not run, nothing polls, nothing fires; `automations.paused: true` records every signal as `paused` and never fires. Both are read from `config.yaml` on every tick; an unreadable file is OFF. No RPC changes either.
- AC2 Cron: five-field UTC grammar (lists, ranges, steps, names, `@hourly/@daily/@weekly`, dom/dow OR rule); a spec firing more often than every 5 minutes is rejected at create. Next-fire computation is exact across month ends and leap days.
- AC3 Missed-run: a scheduled instant late by <= 15 min fires once with `catchup=true`; later instants collapse into one `missed` audit row and no task; a double tick of one instant yields one firing.
- AC4 Fingerprints: cron `cron:<id>:<scheduled instant>`; CI `gh:<repo>:<workflow>:<branch>:<conclusion>:<headSha>`.
- AC5 Dedupe: a signal whose fingerprint has a `fire` row inside `dedupeWindowMinutes` (default 1440 for CI, 0 cron) is `deduped`; a `deduped` row does not extend the window; the window survives an engine restart.
- AC6 Runs/hour cap: default 2, ceiling 12 (create rejects more); the (cap+1)th fire in a rolling hour is `capped` with reason `rate`, not retried; manual fires count.
- AC7 Single flight: while a task the automation started is running or `waiting_permission`, the next signal is `capped` (reason `running`).
- AC8 Auto-pause: after `pauseAfterFailures` (default 3) consecutive failed runs (first run `error`, wall-clock stop, or a `fire` row that never got a task) the automation is `paused` with a one-sentence reason and `automation.paused` is published; a done run resets the counter; only `automation.resume` clears it.
- AC9 Wall clock: a run older than `maxRunMinutes` (default 45, ceiling 240) is stopped with reason `automation_timeout` and counts as failed.
- AC10 Budget: the new root task gets `budget_usd = template.budgetUsd` (required, default 3, ceiling `automations.maxBudgetUsd` default 10); the next firing is `capped` (reason `budget`) when the last run's reported tree cost reached it, until a human resumes/raises.
- AC11 Audit: every evaluation writes exactly one `automation_audit` row with decision in {fire, deduped, capped, paused, missed, disabled_kill_switch, failed}; the row is written before the task starts; `taskId/outcome/finishedAt` backfilled; payload redacted and capped 4 KiB; retention 90 days but never below the last 100 rows per automation.
- AC12 Authority: `automation.create/update/delete/enable/resume/fire/memory.clear` are refused (`forbidden`) for run-scoped callers and for any task whose run origin is `automation`; create/update/delete/enable/resume/fire are refused over the relay and on a non-loopback direct connection unless `automations.allowRemoteManage`; a relay session may list/get/audit/memory.get and **disable** (pause). No automation RPC is in `RunScopedMethods`, none in the MCP catalogue (a test walks both).
- AC13 Create always starts `enabled=false`; validation: name rule, workspace and profile exist, mode allowed (`autoAccept`/`full-access`/bypass refused unless `automations.allowUnattendedModes`), prompt template <= 8 KiB with documented scalar placeholders only, cron >= 5 min, budget/runs ceilings.
- AC14 The started task: origin `automation` on the run; permission mode exactly the template's; never `autoAccept`; seat policy, depth and tree guard apply; `tasks.automation_id/automation_audit_id` set; a permission request waits for a human.
- AC15 Prompt injection: the prompt is template text + a `<signal untrusted="true">` JSON block; strings capped 512, control chars stripped, JSON-encoded so the envelope cannot be closed; placeholders resolve only to validated scalars (ids, sha, branch after ref-format check); no log text in prompt, events or audit.
- AC16 Memory: `$SMIND_HOME/automations/<id>/MEMORY.md` (0600/0700, never in a worktree), read-only projection (claude `AddDirs`; others inlined as quoted data inside the untrusted envelope); written only by the daemon from the run's final `automation-memory` fenced block; <= `memoryMaxBytes` (16 KiB) and 400 lines, UTF-8, no NUL, no token-shaped strings; atomic replace; oversize/malformed block dropped, old memory kept, audit note; symlinked dir/file refused; no path field exists. Off by default.
- AC17 Fire now: human, local, rail-bound (counts toward runs/hour, budget, single flight), skips only dedupe, recorded `manual=true`.

Source and surfaces (later PRs):
- AC18 (PR 2) `github.workflow_run`: one `gh run list` per distinct (repo, workflow, branch) among *enabled* automations every 5 min (floor 2 min, per-automation), through `forge.Provider`; booked in `forge_budget` with an automations ceiling of 60 of the 240 hourly calls plus the 15 s per-repo floor; zero enabled = zero calls; first poll primes the cursor and fires nothing; a signal is a completed run with a listed conclusion and id > cursor; the cursor advances after the audit row; `forge_unauthenticated`/`rate_limited` suspend with backoff; cursor and next-allowed survive restart.
- AC19 (PR 2) manual fire for a workflow source uses the latest matching run (one budgeted call).
- AC20 (PR 3) Web Settings -> Automations: list, create/edit form with rails and defaults, cron next-three preview (UTC + local), enable switch, Fire now, Resume with reason banner, run history with decision filter and task links; light+dark at 1280 and 390.
- AC21 (PR 3) Mobile: read-only list and history plus Pause; push on `automation.paused`; no create/edit/enable/resume/fire.
- AC22 (PR 4) Pilot: the documented automation JSON and P1-P9 of ADR-0044 §11 pass on an isolated daemon with a scripted `gh` on `PATH`; one Haiku-pinned live run; never real GitHub, `:4648`, `:4870` or `~/.smind`.
- AC23 CLI `smind automation list|show|create|enable|disable|resume|fire|audit|memory|delete` with `--json`; docs in `public-docs/`; events `automation.fired/paused/updated` in `knownTopics`.

## Test scenarios (named)

`internal/automation` (fake clock, fake forge, fake starter):
- `TestCron_Grammar`, `TestCron_RejectsUnderFiveMinutes`, `TestCron_NextFire_MonthEndLeapDomDow` (AC2), `TestEngine_MissedRun_CatchupOnceWithinGrace`, `..._BacklogCollapsesToOneMissed`, `..._DoubleTickOneFire` (AC3)
- `TestFingerprint_Cron`, `TestFingerprint_CI_SameShaDifferentRunID` (AC4)
- `TestDedupe_WithinWindow`, `..._DedupedRowDoesNotExtend`, `..._SurvivesRestart`, `..._NewShaIsNewSignal` (AC5)
- `TestCap_RunsPerHour_BurstOf50`, `TestCap_ManualCounts`, `TestCap_CeilingRejectedAtCreate` (AC6)
- `TestSingleFlight_RunningAndWaitingPermission` (AC7)
- `TestAutoPause_TripsAtN`, `..._SuccessResets`, `..._FireRowWithoutTaskCounts`, `..._ResumeClears`, `..._ReasonSentence` (AC8)
- `TestWallClock_StopsRun` (AC9), `TestBudget_SetOnRoot`, `TestBudget_NextFireCappedAfterOverspend` (AC10)
- `TestAudit_OneRowPerEvaluation`, `..._WrittenBeforeStart_CrashLeavesFireRow`, `..._PayloadRedactedAndCapped`, `..._RetentionKeepsLast100` (AC11)
- `TestKillSwitch_GlobalOffNoTickNoPoll`, `TestPausedGlobal_RecordsPaused` (AC1)
- `TestValidate_Template*` incl. `..._RefusesUnattendedModes`, `..._PlaceholderOnlyScalars`, `..._CronTooFast` (AC13)
- `TestPrompt_SignalIsQuotedData`, `..._InjectionStringCannotCloseEnvelope`, `..._NoLogTextEver`, `..._ControlCharsStripped`, `..._PlaceholderRejectsFreeText` (AC15)
- `TestMemory_ReadOnlyProjection`, `..._WriteFromFencedBlockOnly`, `..._OversizeDroppedKeepsOld`, `..._PathEscapeRejected`, `..._SymlinkRefused`, `..._TokenShapedRefused`, `..._AtomicReplace`, `..._PerAutomationIsolation` (AC16)
- `TestStarter_OriginAutomation_ModeNeverAuto` (AC14)

`internal/store`: `TestMigrate_AutomationTables`, `TestAutomationAudit_Queries`, `TestTasks_AutomationColumns`.
`internal/wsapi`: `TestAutomationRPC_RefusedForRunScoped`, `..._RefusedForAutomationOriginTask`, `..._RelayMutationsRefusedDisableAllowed`, `..._NotInRunScopedMethods`, `TestMCPCatalogue_HasNoAutomationTool`, `TestAutomationRPC_ListGetAuditRoundTrip`, `TestAutomationEvents_Topics` (AC12, AC23).
`cmd/smind`: `TestAutomationCLI_*` (create starts disabled, enable, resume, audit `--json`).
PR 2: `TestGHWorkflowRuns_Parse`, `TestPoll_PrimesCursorFiresNothing`, `TestPoll_CountsCalls_ZeroWhenNoneEnabled`, `TestPoll_BudgetCeilingAndFloor`, `TestPoll_BackoffOnAuthError`, `TestPoll_CursorSurvivesRestart`, `TestPoll_FilterBranchWorkflowConclusion`, `TestManualFire_LatestRunOneCall` (AC18, AC19).
Web (PR 3): `automations-page.test.tsx` (list, form validation, cron preview, paused banner, resume, history filter, relay-readonly). Mobile: `automations.test.ts` (read-only RPC set, Pause only).
PR 4: `TestPilot_ScriptedGH_P1..P9` (e2e with a fake `gh` script) and the live run script.

Mutation checks (one each; `git status --short` first; test must FAIL, build failure proves nothing): `>=`->`>` on the runs/hour cap; drop the dedupe window comparison; auto-pause counter not reset on success; human-only guard skipped for the `automation` origin; relay may enable; envelope not JSON-escaped; memory path join without Lstat; cursor advanced before audit write; kill switch not consulted in Tick; poll when zero automations enabled.

## PRs (non-stacked, each from `origin/develop`; a PR starts only after its predecessor is merged)

| PR | Content | Write scope | Depends on |
|---|---|---|---|
| 1 daemon core | engine, cron, rails, memory, prompt envelope, store tables, config keys, `automation` origin, `automation.*` RPCs + human-only guards, events, CLI | `internal/automation/**` (new), `internal/store/automation*.go` (new) + additive tail of `schema.sql`/`migrate.go`, `internal/config/config.go` (one struct), `internal/wsapi/automation*.go` (new) + one registration line in `handlers.go`/`events.go`/`session.go` untouched, `internal/runs` (origin constant only), `cmd/smind/automation*.go` (new) + one wiring block in `serve.go`, `public-docs/automations.md` | ADR on develop |
| 2 GitHub source | `forge.Provider.WorkflowRuns`, `ghctx` impl, source, poll cursor/ledger, budget ceiling, manual fire for workflow sources | `internal/forge/types.go`, `internal/ghctx/workflow_runs*.go` (new), `internal/automation/source_github*.go` (new), `internal/store/automation_poll.go` | PR 1 |
| 3 UI | web Settings -> Automations + history; mobile read-only list/history/pause | `web/packages/ui/src/**/automations*`, settings registry entry, `mobile/src/**/automations*` | PR 1 (PR 2 for workflow fields) |
| 4 pilot | example JSON, docs, scripted-`gh` live runner, e2e test, plan closure | `docs/**`, `internal/automation/pilot_test.go`, `scripts/automation-pilot-live.sh` | PR 1, 2 |

Disjointness: the **V lane** (validator/inbox: reviewer seat, verdicts, console UI) owns reviewer/verdict tables, `internal/wsapi` review handlers and the console web area; this plan adds no verdict code and touches no console UI. **Plugins Wave B** owns `internal/plugins/host`; this plan never touches it. **Shared files, unavoidable:** `internal/store/schema.sql` and `migrate.go` (additive tail only; if the V lane merges first, rebase before opening, append after their block), `internal/wsapi/handlers.go` handler map and `events.go` `knownTopics` (one line each), `cmd/smind/serve.go` (one wiring block), `internal/config/config.go` (one struct). Each is a pure addition; whichever PR lands second rebases.

## Decisions

- Origin `automation` is a new `ChatQueueSource*`-style string on the run (the run origin is opaque to the tree budget, which only refuses `agent`); the human-only guard keys on run identity origin, not on "is a seat".
- The starter is an injected interface in `internal/automation`; the daemon adapter (`cmd/smind`) creates the task through `workspace.Manager.CreateTask` + `runs.Registry.Start` with `WithOrigin("automation")` and `WithProfile`, the same path as the RPCs.
- **PR 1 deviations from the ADR text (recorded, no change of meaning):** (a) the task-to-automation link is `automation_audit.task_id` (`Store.AutomationAuditForTask`) instead of two new `tasks` columns, which keeps `store.Task` and `scanTask` untouched; (b) the memory is inlined into the prompt as quoted data for every provider (a 16 KiB cap makes the claude `AddDirs` projection unnecessary), so there is no new add-dir path; (c) the wall-clock stop and the budget block are applied on the engine's tick (30 s), not by a per-run timer; (d) the budget/wall-clock/failure accounting follows the *root* run of the automation's task, Peers the Lead launched are bounded by the tree budget and the tree guard, not tracked separately.
- `maxConcurrent` is fixed at 1; per-automation poll interval is a rail field, clamped to the 2-minute floor.

## Progress

- 2026-10-27 PR 1 (daemon core: engine, cron, rails, memory, prompt envelope, store, config keys, `automation.*` RPCs with the human-only guards, events, CLI, public docs) implemented on this branch; PR 2-4 not started.

## Validation

PR 1 (evidence is the named tests; `go test -race` on `internal/automation`, `internal/store`, `internal/config`, `internal/wsapi`, `cmd/smind` green; full `go test ./...` green except one unrelated timing flake in `internal/accounts` (`TestOpenAIDeviceLogin_Vendor`, passes on rerun)):

| AC | Evidence |
|---|---|
| AC1 | `TestKillSwitch_GlobalOffNoTickNoPoll`, `TestPausedGlobal_RecordsPaused`, `TestPoll_ZeroEnabledZeroCalls`, `TestConfig_AutomationsDefaults` |
| AC2 | `TestCron_Grammar`, `TestCron_RejectsUnderFiveMinutes`, `TestCron_NextFire_MonthEndLeapDomDow` |
| AC3 | `TestCron_CatchupOnce_BacklogCollapses_DoubleTickOneFire`, `TestCron_FiresOnTime_NeverFiresThePastOnEnable` |
| AC4 | fingerprints asserted in `TestCron_FiresOnTime_...` and `ciSignal` tests (CI fingerprint built by the PR 2 source) |
| AC5 | `TestFire_ThenDedupe_SameShaDifferentRun`, `TestDedupe_DedupedRowDoesNotExtendWindow`, `TestDedupe_NewShaIsNewSignal`, `TestDedupe_SurvivesRestart`, `TestAutomationAudit_Queries` |
| AC6 | `TestCap_RunsPerHour_BurstOf50`, `TestFireNow_CountsTowardRunsPerHour`, `TestValidate_Template` (ceiling) |
| AC7 | `TestSingleFlight_RunningAndWaitingPermission` |
| AC8 | `TestAutoPause_TripsAtN_SuccessResets_ResumeClears`, `TestAutoPause_FireRowWithoutTaskCounts`, `TestStartFailed_CountsAsFailure` |
| AC9 | `TestWallClock_StopsRun` |
| AC10 | `TestBudget_NextFireCappedAfterOverspend`; the budget is set on the root by `automationStarter.Start` (live check in PR 4) |
| AC11 | `TestAudit_FireRowLinkedToTask_AndOutcomeBackfilled`, `TestAudit_PayloadCappedAt4KiB`, `TestAudit_RetentionKeepsLast100`, `TestAutomationAudit_Queries` |
| AC12 | `TestAutomationRPC_RefusedForRunScoped_NilAndAutomationOriginTask`, `TestAutomationRPC_NotInRunScopedMethods`, `TestAutomationRPC_RelayMutationsRefused_DisableAndReadsAllowed`, `TestAutomationRPC_NonLoopbackNeedsAllowRemote`, `TestMCPTools_NoAutomationTool` |
| AC13 | `TestMigrate_AutomationTables`, `TestCreate_StartsDisabled_RefusesUnattendedProfile`, `TestValidate_Template`, `TestValidate_RefusesUnattendedModes`, `TestAutomationRPC_CreateStartsDisabled_AndRoundTrip` |
| AC14 | `TestStarter_ReceivesTemplateModeAndOrigin` (engine side); the daemon adapter passes `AutoAccept:false` and `runs.WithOrigin("automation")` (live check in PR 4) |
| AC15 | `TestPrompt_SignalIsQuotedData`, `TestPrompt_InjectionStringCannotCloseEnvelope`, `TestPrompt_PlaceholderRejectsFreeText_AndValidatesScalars`, `TestPrompt_ControlCharsStrippedAndCapped`, `TestPrompt_MemoryIsQuotedData` |
| AC16 | `TestMemory_*` (`WriteFromFencedBlockOnly`, `RoundTrip_AtomicReplace_PerAutomationIsolation`, `Rejections`, `PathEscapeRejected`, `SymlinkRefused`), `TestMemory_StoredFromBlock_AndOversizeKeepsOld` |
| AC17 | `TestFireNow_BypassesDedupeOnly`, `TestFireNow_CountsTowardRunsPerHour`, `TestEvaluate_KindSwitchRowForManualFireWhenDisabled` |
| AC23 | `TestAutomationCLI_*`, `TestAutomationEvents_Topics`, `public-docs/automations.md`, `TestCLIReferenceMatchesHelp` |

Mutations run (one each, test had to fail, restored with `git checkout`; log `/tmp/automation-mutations.log`): runs/hour `>=`->`>`; dedupe check disabled; failure counter not reset on success; kill switch not consulted in `Tick`; single-flight check dropped; agent guard skipped in `automationHumanLocal`; relay `!rc.local` guard removed (this one first survived because the channel check also refused, so the test now adds a relay session claiming a loopback channel); payload JSON not HTML-escaped; workflow placeholder unvalidated; memory directory symlink followed (`os.Stat` for `os.Lstat`); memory size cap dropped. Not run in PR 1: cursor-before-audit order (PR 2), live daemon (PR 4).
