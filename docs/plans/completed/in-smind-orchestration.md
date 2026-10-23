# In-smind orchestration: seats, parent notifications, rate-limit resilience

Size: **XL (10)** by the protocol table — scope 2 (daemon, wsapi, MCP, CLI, web, mobile), PRs 2 (about sixteen, with ordering), risk 2 (restart idempotency, concurrency in
`Registry.finish`, a new agent-identity path, a prompt-injection channel), decisions 2 (ADR-0041, twelve open questions), verification 2 (a live provider 429 and a full
Supervisor → Lead → Peer item). Per `docs/WORKSPACE_PROTOCOL.md` an XL is split into L items with one Lead each and disjoint write scopes: **Item R (resilience: R1–R3, N1–N3, F1–F3)** first,
**Item S (seats: S1–S4, T1, M1–M2)** second. Every PR is **non-stacked off `develop`**; a PR that needs an earlier PR's code waits for its merge.

Spec for [ADR-0041](../../decisions/0041-in-smind-orchestration.md) (**Accepted 2026-10-23**, all twelve questions as recommended; read it first). Amends [ADR-0027](../../decisions/0027-profile-as-role-contract.md) and leaves
[`orchestration-roles.md`](../active/orchestration-roles.md) parked for its R5/R6/R8 parts. **Status: CLOSED 2026-10-23.** Item R (resilience) and Item S (seats, role pack, the in-smind dogfood run) are built and merged; the ACs below are confirmed in Validation except the ones listed under "Remaining (parked)" there (AC17 and AC20's web halves, AC27, AC28), which are deferred by decision, not forgotten.

## Goal

The Supervisor, the Leads and the Peers all run as smind tasks, so a real item (the next ADR's PRs) is orchestrated end to end without Paseo, and a provider rate limit does not stall the tree
silently: the failed run says what happened and when it resets, the Lead is woken without polling, and, if the Human configured it, the work continues on another provider.

## Scope and PRs

In scope: the three resilience pieces (typed `rate_limited` reason, parent notification, failover policy) and the seats (profile contract, daemon-built `smind` MCP entry with run identity,
admission, seed pack), plus the web/mobile surfaces for them. **Out of scope:** `handoff_submit`/`handoff_review`, per-run `maxTurns`/budget flags, sizing and brief fields (ADR-0027 R5/R6/R8), a run
token with a method allowlist, write-scope warnings, heartbeats/schedules, a stall detector, account-level failover, anything in the ADR-0038 (attachments), ADR-0039 (git/PR state) and UI-dogfood
Leads' files (`internal/attach`, `internal/forge`, `internal/gitstate`, `mobile/app/new-task.tsx`, `mobile/src/newTask*`, `web/.../diff-viewer*`, `attachment-lightbox.tsx`).

### Item R: resilience (ships first; needs no Item S)

| PR | Content | Size | Executor | Depends on (merge order) |
|---|---|---|---|---|
| R1 | **Classify and store.** `internal/runs/ratelimit` (pure classifier: GLM text, claude-native `rate_limit_event`, Codex `UsageLimitExceeded`), `runs.failure_kind/failure_window/failure_reset_at`, record `rate_limit_event` in the claude-native adapter, call from `Registry.finish`, additive fields on `run.list/status/logs` and the `runs.RunStatus` struct | M | peer-sonnet (parsing, restart reconcile) + peer-glm (store, wire fields, tests) | #529 merged (same function, `finish`/reconcile) |
| R2 | **Surface it.** MCP `task_status`/`task_wait`/`run_list`/`task_tree` fields, CLI `task runs` STATUS text and `task wait` line + exit **6**, `task.tree` node detail, `smind` skill + `smind-leader` text ("a rate-limited child says when it resets") | S | peer-glm | R1 merged |
| R3 | **Web + mobile footers and badges** for `failureKind`/`resetAt` (countdown), light + dark screenshots, `describeRunFooter` cases | M | peer-glm (detailed brief) | R1 merged (may run beside R2: different files) |
| N1 | **Schema and plumbing.** `runs.reply_chat_id`, `runs.parent_notified_at`, `chat_queue.not_before`, queue source and run origin `child_event`; `run.start`/`task.prompt` take `notifyOnFinish` (default true when `source=agent` with from-ids, else off); MCP `task_send {notifyOnFinish}`; delivery honours `not_before` and wakes at it (timer + startup sweep); no notifier yet | M | peer-sonnet (queue, timer) + peer-glm (store) | R1 merged (shared `RunStatus`/`runSummaryRow`: one owner at a time) |
| N2 | **The notifier.** Observer on `finish`, startup outbox scan, quiescence rule, coalescing + debounce, message builder (bounded, untrusted envelope), kill switch `orchestration.notifyParent`, `orchestration.notifyDebounceSeconds`, archived-parent skip, queue-full handling | L | peer-sonnet (concurrency, `-race`) + peer-glm (builder, tests) | N1 merged |
| N3 | **Surfaces and measurement.** Web child-event row in the parent timeline, `task.tree` records the cost of `child_event`-started runs separately, `smind task runs` shows ORIGIN, skill text ("you will be notified; do not poll; `notifyOnFinish:false` for fire-and-forget"), docs; first live measurement of a Lead wake recorded in Validation | M | peer-glm (UI) + Lead (measurement) | N2 merged |
| F1 | **Provider cooldown.** `provider_cooldowns`, set from R1's `resetAt` (cap 6 h), fast-fail for agent-sourced `task_send`, warning for humans, `provider_list.cooldownUntil` | S | peer-glm | R1 merged |
| F2 | **Failover policy data + `none`/`wait`.** `agent_profiles.on_rate_limit`, `fallback_profile_id` (CRUD, validation: different provider, human-authored), `smind profile` flags, web profile editor fields; `wait` = same-chat re-enqueue at `not_before = resetAt+30s`; "Retry on … / Wait" buttons on a rate-limited run when a fallback exists | M | peer-glm + peer-sonnet (delivery) | N1 merged (needs `not_before`), R1 merged |
| F3 | **`fallback`.** New chat bound to the fallback provider, preface + original prompt + ≤ 4 KiB transcript tail, `runs.failover_of`, one hop, `reply_chat_id` copy, `run.failover` event, UI badge ("continued on …"), N2's quiescence sees the successor | L | peer-sonnet | F2 merged, N2 merged |

Order: R1 → (R2 ‖ R3) → N1 → N2 → N3; F1 after R1; F2 after N1; F3 after F2 and N2. The first useful stopping point is **R1 + R2** (the typed reason everywhere); the second is **+ N1–N3** (a Lead wakes).

### Item S: seats

| PR | Content | Size | Executor | Depends on |
|---|---|---|---|---|
| S1 | **Profile contract data.** Nullable columns `orchestration_tools`, `instructions`, `skills`, `child_policy`, `child_profile_ids` on `agent_profiles`; validation in `internal/profiles` (caps, unknown skill, `orchestrationTools=false` forces `none`); `profile.*`, `profile_list` fields, `smind profile add/edit` flags; web editor fields. No runtime effect | M | peer-glm | — (F2 may add columns to the same table; merge S1 and F2 in either order, but one at a time) |
| S2a | **Daemon-side profile.** `run.start`/`task.prompt`/`chat.queue*` take `profileId`; `runs.profile_id`; the daemon applies the contract; per-provider instruction delivery (Claude append-system-prompt, ACP first-prompt preface with hash re-send, Codex developer instructions) | L | peer-sonnet | S1 merged |
| S2b | **Built-in `smind` MCP entry + identity.** The daemon builds the reserved `smind` entry for `orchestrationTools=true` runs (Claude `--mcp-config`, ACP `mcpServers`), env `SMIND_TASK_ID/CHAT_ID/RUN_ID/HOME`; strict MCP on claude-native when false; Codex "not enforced"; `smind mcp serve` autofills `parentTaskId`/`fromTaskId`/`fromChatId` and rejects a disagreeing explicit value | L | peer-sonnet | S2a merged |
| S3 | **Admission.** For identity-bearing callers: forced parent and source, `childPolicy` check against `profileId`, raw provider/mode/model rejected, own-subtree scope, `profile_list` filtered to allowed children; model-readable errors | M | peer-sonnet | S2b merged |
| S4 | **Role pack and skills.** `smind profile seed roles` (human, idempotent), `smind-supervisor` bundled skill, `smind-leader` updated (archive each Peer after accept; briefs per `WORKSPACE_PROTOCOL.md`), `docs/WORKSPACE_PROTOCOL.md` "smind" section, README note; **then one real L item run Supervisor → Lead → 2 Peers entirely in smind, Paseo as the control** | M + a run | Lead + Human | S3 merged, R2 and N3 merged (the item needs the resilience pieces) |
| T1 | **Tree budget** (optional, ADR Q9): `tasks.budget_usd`, admission check, tree header, "N runs reported no cost" | M | peer-glm | S3 merged |
| M1 | **Mobile read-only tree**: nested rows, attention rollup, failure badge | M | peer-glm | R3 merged; coordinate with the mobile Leads (do not edit `new-task.tsx`) |
| M2 | **Push for a stuck tree** (ADR-0040 notifier: a root tree whose latest run is `rate_limited` with no failover pushes the root; new kind, rate limited like `needs_continue`) | S | peer-sonnet | R1 merged, ADR-0040 PR 3 merged (it is) |

Write scopes: Item R owns `internal/runs/ratelimit/` (new), `internal/runs/{registry,queue,observer}.go` and a new `internal/runs/parentnotify*.go`, `internal/store` (run/queue/profile/cooldown columns), `internal/wsapi/{run*,task_tree}.go`,
`internal/taskrunner` (rate-limit event recording only), `cmd/smind/{task_wait,task,mcp_run_tools,mcp_task_send,mcp_tree_tools}.go`, the run footer in `web/packages/ui` and `mobile/src/runFooter.ts`. Item S owns `internal/profiles`, `internal/wsapi/profile*.go`,
`internal/mcpservers` (the built-in entry), `internal/taskrunner/mcp.go`, `cmd/smind/{profile,mcp,mcp_task_send}.go` (identity autofill; **the one shared file with Item R: `mcp_task_send.go`**), `internal/skills/bundle`, `docs/WORKSPACE_PROTOCOL.md`.
A shared struct gets one owner at a time (dogfood entry 34): `runs.RunStatus`, `runSummaryRow` and `runLogsResult` are extended by R1, R2, N1, F3 and S2a in that order, never in parallel; a PR that adds a field to them rebases onto develop
before it opens (memory: two PRs extending one struct merge cleanly and break the build).

## Acceptance criteria

Rate limit, typed reason (R1–R3)

1. A run whose agent ends with GLM's `429 Usage limit reached for 5 hour. Your limit will reset at 17:35:18` ends `error` with `failureKind = rate_limited`, `window = 5h`, and `resetAt` = 17:35:18 read as Asia/Shanghai (UTC+8) wall time (the dated form is absolute; an undated time is today when still ahead, otherwise tomorrow), as an RFC 3339 UTC instant. The five run statuses are unchanged and `disposition` stays empty.
2. A claude-native run that received a `rate_limit_event` with `status = rejected` (`resetsAt` epoch seconds, `rateLimitType` `five_hour|seven_day|seven_day_opus|seven_day_sonnet|overage`) and then ended in error carries that epoch exactly as `resetAt` and that type as `window`; `allowed_warning` alone never classifies a run; a run that sees `rejected` for the *overage* type only, and finishes `done`, is not an error and is not classified.
3. A Codex turn error whose info is `UsageLimitExceeded` classifies as `rate_limited`; `resetAt` is NULL unless the protocol supplies it.
4. **Unknown never classifies:** an error with no recognised pattern, a failed parse of the reset time (`resetAt` NULL, kind still set when the pattern matched), and `429` appearing only in the agent's assistant text (not in the `[error]` cause or the error chain) all leave `failureKind` empty or `resetAt` NULL; nothing is guessed.
5. The fields are stored: `run.list`, `run.status`, `run.logs` return the same values after a daemon restart and for a rehydrated run, and are absent (omitted) when empty so old clients see no change.
6. MCP `task_status`, `task_wait`, `run_list` and `task_tree`, and the `task.tree` node of a task whose latest run was rate limited, carry `failureKind`, `window`, `resetAt`. `task wait` exits **6** and prints `rate limited (5h window), resets 17:35 (in 2h 3m)`; `task runs` STATUS shows the same short text; exit codes 0/1/3/4/5 are unchanged.
7. The web run footer and the task row show "Rate limited · resets 17:35" with a countdown; mobile's footer shows the same text; light and dark screenshots reviewed; a pre-R1 run shows no badge.

Parent notification (N1–N3)

8. A child task's run that was started by an agent send with `fromTaskId` = its parent and `fromChatId` = a chat of that parent, `notifyOnFinish` not false, ends `done` or `error` and its child subtree is quiet → **exactly one** `chat_queue` item appears on `fromChatId` with `source = child_event`, and the parent's chat shows a child-event row, not a user bubble.
9. **No notification** for: a root task's run (no parent); a human-sent prompt in the child's chat; an `orchestrator`-sourced send; `notifyOnFinish:false`; `orchestration.notifyParent:false`; a plain `stopped` run; a `waiting_permission` pause; a system/purpose run (`reflect`, `curate`, `probe`); an archived parent task or chat. A `stopped`/`interrupted` run with stop reason `daemon_restart` **does** notify.
10. **Quiescence:** a Lead task whose run ends while a Peer descendant still runs, or while its own chat has a queued item (including a pending `child_event`), does not notify the Supervisor; after the last Peer ends, the Lead is woken, handles it and ends idle with nothing queued, the Supervisor gets exactly one item.
11. **Coalescing and debounce:** three children of one parent finishing within the debounce window produce one queued item holding three blocks, delivered no earlier than `not_before` (last append + `notifyDebounceSeconds`, capped at 60 s) and started as one parent run; a child finishing after that run started produces a new item, not a second run on the same turn.
12. **Idempotent across restart:** finishing the same run twice, restarting the daemon between the run's terminal write and the enqueue, and running the startup scan twice each yield exactly one item for that run; an `interrupted` run (never through `finish`) is notified once at startup when it finished within 24 h and was never notified; a run older than 24 h is not.
13. **Cost and loops:** delivery is `queue` semantics (never interrupts a running parent turn); a run started from a `child_event` item has `origin = child_event` and no `reply_chat_id`, so its end never notifies upward on its own; a full parent queue (20) drops the notification with a log line, leaves `parent_notified_at` NULL for the scan to retry, and never blocks or fails `finish`.
14. The notification text is bounded: ≤ 2 KiB per child, the child's last assistant text ≤ 1 KiB cut on a rune boundary, wrapped in an element marked `untrusted="true"`, with `<`/`>`/`&` in the child's text escaped so a child cannot close the element; the block names `task_logs {runId}` for the rest.
15. The Lead's `task_wait` on a run that also notifies still returns its normal result (the notification is additive), and `task_wait {childrenOf}` is unchanged.

Failover (F1–F3)

16. After a classified rate limit on provider P with `resetAt` T: an **agent-sourced** `task_send` to P before T fails fast with a message naming T and the configured fallback profile (if any); a **human** send starts with a warning; after T, sends pass; the cooldown never exceeds 6 h; `provider_list` shows `cooldownUntil`; a restart keeps it.
17. `onRateLimit = none` (default): no automatic run; the web offers "Retry on <fallback>" and "Wait until 17:35" only when `fallbackProfileId` is set; the CLI/MCP show the suggestion in the failure text.
18. `onRateLimit = wait`: the same prompt is enqueued on the **same chat** with `not_before = resetAt + 30 s`, the queue strip shows "starts 17:35", it survives a restart, the user can cancel it, and the resumed run keeps the chat's context (same provider, same session).
19. `onRateLimit = fallback`: the daemon creates a new chat in the same task bound to the fallback provider, starts the failed run's prompt there with the preface (worktree holds the changes; inspect `git status`/`git log`) plus the original prompt and a ≤ 4 KiB transcript tail, records `failover_of`, copies `reply_chat_id`, and emits `run.failover {fromRunId, toRunId, toChatId}`. **One hop:** a fallback run that is itself rate limited does not fail over again and notifies. A user-stopped run, a non-rate-limited error and a run whose profile has no fallback never fail over. A fallback profile on the same provider, or not human-authored, is rejected at save.
20. The parent is told about the **successor** (once, "after failover from glm"); the failed run does not notify; the web shows "Rate limited · continued on Claude Sonnet · chat #N"; the per-tree guard counts the successor; the tree budget (T1) counts both.

Seats (S1–S4)

21. A profile without contract fields behaves exactly as before (round trip, run launch); `profile.create/update/delete` from an identity-bearing agent caller is rejected; `orchestrationTools=false` forces `childPolicy = none`; an unknown skill name, an over-cap instructions text and a self-referencing child list are rejected.
22. A run whose profile says `orchestrationTools=true` gets a daemon-built `smind` MCP entry with env `SMIND_TASK_ID`, `SMIND_CHAT_ID`, `SMIND_RUN_ID`, `SMIND_HOME` (the last pointing at the daemon that started it, including a sandbox daemon); `false` gets none (claude-native: strict MCP, a user-level `smind` entry is ignored; ACP: `mcpServers` omits it; Codex: reported "not enforced"). The persisted prompt and transcript stay the user's text; instructions are re-applied on every spawn.
23. `smind mcp serve` fills `parentTaskId`, `fromTaskId`, `fromChatId` from the env; an explicit value that disagrees is rejected; with no env (a human or external orchestrator) behaviour is unchanged.
24. A Peer-seat caller's `task_new`/`task_send`/`chat_new` are rejected (no tools, and the daemon refuses identity-bearing calls of a no-orchestration profile); a `leaf` Lead may launch only profiles with `orchestrationTools=false`; a raw provider, `permissionMode` or `model` from an identity-bearing caller is rejected; sending to a task outside the caller's subtree is rejected; a self-reported `source` is ignored when identity is present.
25. `smind profile seed roles` run twice yields five profiles once, matching the ADR table (no Opus); an agent cannot run it; editing a seeded profile survives a re-run.
26. **Dogfood (manual, recorded under Validation):** one real L item is orchestrated Supervisor (root task, the Human talking) → Lead → at least two Peers entirely in smind, including one forced rate limit (fake agent on an isolated daemon, and, if the Human allows it, a real GLM limit) that exercises `fallback`; the Lead is woken by notifications with no polling; the Human sees and steers the tree in web; Paseo remains the control and the comparison (turns, cost, wall clock, interventions) is written down.

Later (T1, M1, M2)

27. (T1) A root with `budgetUsd` refuses agent-launched `task.create`/`run.start` once the tree's **reported** cost has reached it, with a model-readable error; running runs finish; an agent cannot set or raise it; nil-cost runs are counted in "N reported no cost".
28. (M1) Mobile shows children nested under their parent with the attention rollup and the failure badge, read-only. (M2) A root tree whose latest run is `rate_limited` with no fallback pushes the root once, rate limited like `needs_continue`.

## Test scenarios

Go: classifier and store (R1)

- `TestClassify_GLM429FiveHourFixture` (the exact string from the 2026-10-22 log; the reset is Asia/Shanghai (UTC+8) wall time, rolled to tomorrow when past; zone-parametrised with a fixed clock; `TestClassify_GLMResetIsShanghaiTime` pins the zone against the daemon's own) — AC1; `TestClassify_GLMDataDetailsFallback` (cause only in `error.data.details`); `TestClassify_UnparseableResetKeepsKindNilReset` — AC4.
- `TestClassify_ClaudeRateLimitEventRejected` (exact epoch and type), `TestClassify_ClaudeWarningOnlyNeverClassifies`, `TestClassify_ClaudeOverageRejectedButDoneIsNotAnError` — AC2; `TestClassify_CodexUsageLimitExceeded` — AC3.
- `TestClassify_429InAssistantTextOnlyIsNotClassified`, `TestClassify_UnknownErrorIsNotClassified` — AC4.
- `TestFinish_StoresFailureColumnsAndStatusStaysError`, `TestStatusEnumUnchanged_AndDispositionEmptyForRateLimited` — AC1; `TestRunList_FailureFieldsSurviveRestartAndRehydrate`, `TestRunWire_FailureFieldsOmittedWhenEmpty` — AC5.
- `TestStore_MigrateFailureColumns` (an existing DB gains the columns with defaults).

Go: surfaces (R2)

- `TestMCPTaskStatus_FailureFields`, `TestMCPTaskWait_RateLimitedResult`, `TestMCPRunList_FailureFields`, `TestMCPTaskTree_FailureFieldOnNode`, `TestTaskTree_NodeCarriesLatestRunFailure` — AC6.
- `TestTaskWait_ExitSixOnRateLimited` (+ stdout line), `TestTaskRuns_StatusTextForRateLimited`, `TestTaskWait_OtherExitCodesUnchanged` — AC6.
- `TestMCPServer_InstructionsNameEveryTool` (extended: the new fields/tools are documented) — existing guard.

Go: notification (N1–N2, `-race`)

- `TestNotifyOnFinish_DefaultsFromAgentSendOnly`, `TestRunStart_ReplyChatMustBelongToParent` (a chat of another task is rejected, the run starts without notify) — AC8.
- `TestParentNotify_DoneAndErrorEnqueueOneItem` (item source, chat, header, text), `TestParentNotify_DaemonRestartStopNotifies`, `TestParentNotify_PlainStopDoesNotNotify`, `TestParentNotify_PermissionPauseDoesNotNotify`, `TestParentNotify_SystemPurposeRunDoesNotNotify` — AC8, AC9.
- `TestParentNotify_RootTaskNothing`, `TestParentNotify_HumanPromptNothing`, `TestParentNotify_OrchestratorSourceNothing`, `TestParentNotify_OptOutAndKillSwitch`, `TestParentNotify_ArchivedParentOrChatNothing` — AC9.
- `TestParentNotify_QuiescenceBlocksWhileDescendantRunning`, `TestParentNotify_QuiescenceBlocksWhileQueuedItem`, `TestParentNotify_SupervisorHearsOnceAfterLeadSettles` (fake agents, three tiers) — AC10.
- `TestParentNotify_CoalescesThreeChildrenIntoOneItem`, `TestParentNotify_NotBeforeHoldsDelivery` (fake clock), `TestParentNotify_DebounceCappedAt60s`, `TestParentNotify_LateChildMakesNewItem` — AC11.
- `TestParentNotify_FinishTwiceOneItem`, `TestParentNotify_CrashBetweenStatusAndEnqueueRecoversOnStart`, `TestParentNotify_StartupScanInterruptedOnceOnly`, `TestParentNotify_StartupScanIgnoresRunsOlderThan24h`, `TestParentNotify_ScanTwiceNoDuplicate` — AC12.
- `TestParentNotify_DeliveredAtTurnBoundaryNeverInterrupts`, `TestParentNotify_ChildEventRunHasNoReplyChat`, `TestParentNotify_NoLoopAcrossTiers`, `TestParentNotify_QueueFullLogsAndRetriesNeverBlocksFinish` — AC13.
- `TestParentNotify_TextBoundsAndUntrustedEnvelope` (rune-boundary cut, escaping of `</child-response>`, ≤ 2 KiB per child) — AC14; `TestTaskWait_UnchangedWhenRunAlsoNotifies` — AC15.
- `TestQueue_NotBeforeSurvivesRestartAndStartupSweepDelivers`, `TestQueue_ChildEventSourceCancelRules` (an agent cannot `queue_cancel` a human's item; the Lead may cancel its own `child_event`).
- `TestParentNotify_ConcurrentFinishesAdmitExactlyOnePerRun` (`-race`, 32 goroutines, same shape as `TestTreeGuard_ConcurrentStartsAdmitExactlyLimit`).

Go: failover (F1–F3)

- `TestCooldown_FastFailForAgentSendNamesFallback`, `TestCooldown_HumanSendWarnsAndRuns`, `TestCooldown_ExpiresAtResetAndCapsAtSixHours`, `TestCooldown_SurvivesRestart`, `TestProviderList_CooldownUntil` — AC16.
- `TestOnRateLimitNone_NoAutomaticRun` — AC17; `TestOnRateLimitWait_EnqueuesSameChatAtResetPlus30s`, `TestOnRateLimitWait_UserCancelWins`, `TestOnRateLimitWait_KeepsSessionContext` — AC18.
- `TestOnRateLimitFallback_NewChatBoundToFallbackProvider`, `TestOnRateLimitFallback_PrefaceAndTranscriptTailBounded`, `TestOnRateLimitFallback_OneHopOnly`, `TestOnRateLimitFallback_NeverForStopOrOtherErrors`, `TestProfile_FallbackMustBeOtherProviderAndHumanAuthored`, `TestRunFailoverEventShape` — AC19.
- `TestFailover_ParentHearsSuccessorOnce`, `TestFailover_CopiesReplyChatAndCountsInTreeGuard` — AC20.

Go: seats (S1–S4)

- `TestMigrate_ProfileContractColumns`, `TestProfile_ContractRoundTrip`, `TestProfile_LegacyProfileUnchanged`, `TestProfile_PeerForcesChildPolicyNone`, `TestProfile_InstructionsOverCapRejected`, `TestProfile_UnknownSkillRejected`, `TestProfile_AgentCallerCannotWrite` — AC21.
- `TestRunStart_ProfileResolvedByDaemon`, `TestRunClaude_AppendsInstructions`, `TestRunACP_PrefaceOnNewSessionAndResentWhenHashChanged`, `TestRunCodex_DeveloperInstructions`, `TestRun_PromptAndTranscriptStayUserText` — AC22.
- `TestBuiltinMCP_EntryHasIdentityEnvAndSMINDHome`, `TestRunClaude_PeerHasStrictMCPAndNoSmindServer`, `TestRunACP_PeerMcpServersOmitSmind`, `TestRunCodex_PeerReportedNotEnforced`, `TestMCPServe_AutofillsIdsFromEnv`, `TestMCPServe_DisagreeingExplicitIdRejected`, `TestMCPServe_NoEnvBehaviourUnchanged` — AC22, AC23.
- `TestAdmission_PeerCallerRejected`, `TestAdmission_LeadLeafOnly`, `TestAdmission_RawProviderModeModelRejected`, `TestAdmission_OtherSubtreeRejected`, `TestAdmission_SelfReportedSourceIgnored`, `TestProfileList_LeadSeesOnlyAllowedChildren`, `TestMCPServer_NoApprovalTools` (extended) — AC24.
- `TestSeedRoles_IdempotentAndMatchesPack`, `TestSeedRoles_HumanOnly`, `TestSeedRoles_EditSurvivesRerun`, `TestSeedRoles_NoOpusModel` — AC25.

Web / mobile (vitest, then screenshots)

- `describeRunFooter` cases: rate limited with and without `resetAt`, after reset, failed over (R3, AC7); `task row shows the countdown badge`; `child event row renders as a system row, not a user bubble` (N3); `profile editor saves contract and failover fields` (S1/F2); `Retry on … appears only with a fallback` (F2); mobile `runFooter` and (M1) `taskSections nests children under the parent`.
- Screenshots, light and dark, reviewed by the Lead (memory: UI must be clean; tests alone miss bugs): rate-limited footer, failover footer, child-event row, queued "starts 17:35" strip, profile editor, mobile footer.

Manual (recorded under Validation)

- AC26 end to end on an isolated daemon (`SMIND_HOME`, own port), haiku-pinned for any live call (memory: scripted sends are live calls); a fake agent returning the GLM 429 string for the forced limit; compare with a Paseo control run. A real GLM limit only if one occurs naturally and the Human agrees.

## Decisions

- ADR-0041 is **Accepted** (Human, 2026-10-23, Q1–Q12 as recommended); the PR table is the route. Order: Item R first, then Item S.
- ADR-0027 is amended (status note in the same docs PR); `orchestration-roles.md` stays parked with a progress line, and its R5/R6/R8 keep their trigger.
- Coordination (do not edit their files): ADR-0038 attachments Lead (3a1a5b38), ADR-0039 git/PR Lead (88de2c0a), UI-dogfood Lead (2bc38705). Open PRs that touch the same code: **#529** (`fix/run-restart-interrupted`, `Registry.CloseAll`, `MarkRunningRunsInterrupted`, `DispositionFor`) must merge before R1 and N2; **#528** (claude-native streaming, `internal/taskrunner`) is adjacent to R1's `rate_limit_event` recording (rebase, no overlap in intent).
- Executors: before S2b exists, Peers run as smind tasks the way the dogfood did (`claude-native --model claude-sonnet-5-5 --mode auto --detach`, `task wait`; GLM with step-by-step briefs); never Opus for a Peer; a Peer commits before long test runs and the Lead checks `git status -s` after every send (dogfood entries 32–33).

## Progress

- 2026-10-23: dogfood logs (2026-10-22/23), ADR-0016/0017/0018/0019/0020/0021/0027/0028/0040, the orchestration code (`internal/runs`, `cmd/smind/mcp_*`, bundled skills) and Paseo's notification and tool-policy code read; ADR-0041 and this plan written, docs PR opened. **Stopped for the Human's review.** No code, no live calls.
- 2026-10-23: ADR-0041 **accepted by the Human** (all 12 as recommended); status updated in #530. Implementation starts with R1.
- 2026-10-23: **Item R built through smind** (dogfood daemon, claude-native Sonnet 5.5 `--mode auto`, one Lead, about 45 Peer sends, roughly USD 35 in total) and merged as non-stacked PRs, in this order: R1 #541 (classifier, store columns, `rate_limit_event`, wire), R2 #546 (task wait exit 6, MCP, task.tree, skills), F1 #549 (provider cooldown), N1 #551 (reply chat, notifyOnFinish, held queue items), R3 #550 (web and mobile footer, sidebar dot, lineage), N2 #552 (parent notifier), F2 #555 (failover data and `wait`), zone fix #553. Also merged: F3 #557 (fallback), N3 #558 (child-update surfaces), and the flake fix #556.
- Deviations from the plan as written: (1) a **profile send keeps `source=human`** (ADR-0019 auth) but now forwards `fromTaskId/fromChatId`, otherwise a Lead using seat profiles would never be notified (N1); (2) **GLM's reset text is UTC+8**, not the daemon's zone (#553, ADR correction); (3) a **model-scoped** Claude limit (`weekly-opus`, `weekly-sonnet`) does not cool the provider (F1); (4) there is **no `smind profile edit`** command (only add/ls/rm), so `onRateLimit` is set by `profile.update` or `profile add`; (5) the provider cooldown, `wait` and `fallback` all treat a **missing reset time** as "do nothing automatic" (never guessed); (6) `NotBefore` had to be added to the chat queue wire for the strip (N3).
- 2026-10-23: **Item S built through smind** (same daemon, Sonnet 5.5, `--mode auto`) and merged: S1 #561 (seat contract on profiles: data, validation, CLI flags, MCP `profile_list`), S2a #564 (role text delivered on every spawn), S2b #565 (daemon-built `smind` MCP entry, strict MCP for managed runs, `session.identify` run identity, fail-closed method allowlist), S4a #566 (role pack, `smind profile seed roles`, `task send --profile`, supervisor/peer skills, protocol docs), S3 #567 (admission: childPolicy, profileId required and matched, subtree scope, filtered `profile_list`/`profile_get`), S2w #568 (web composer sends `profileId`).
- 2026-10-23: **AC26, the in-smind dogfood run** (isolated daemon, develop a60a2012): one brief to a Supervisor seat → one Lead → two Peers; one coalesced child event woke the Lead, the Supervisor was woken once when the subtree was quiet; **$0.99, 6 runs, 4 tasks, 11 min 40 s**, no polling, no human input; the Peers' change (task ls PARENT/SETUP, task wait names the task) merged as #570. It found a real bug (a woken Lead lost its seat: #569) and one design gap (a follow-up without a profile also drops the seat), closed with **sticky seats** (#571: an omitted profile inherits the chat's last profile; an explicit id or `0`/`--profile none` overrides; Human-vetoable Supervisor default). A smaller smoke run on Haiku the same day proved Claude Code passes the MCP entry's `env` to the stdio server.
- 2026-10-23: **Closed.** Deviations from the plan as written, in addition to Item R's: (1) the contract's `orchestrationTools` is a **nullable** bool (NULL = unmanaged, today's behaviour; the ADR text said "default true"); (2) identity is `session.identify` on the connection (daemon-side defaults and checks), not `mcp serve` autofilling ids client-side; (3) a **managed claude-native run uses strict MCP config** (it sees only the daemon's registered servers plus `smind` for orchestration seats); (4) ACP role text is a preface on **new** sessions only (no hash-based re-send); (5) the profile registry has no `edit` command (add/ls/rm/seed only); (6) the web composer keeps omitting `profileId` for a plain send so it inherits the seat (to drop a seat in the web: new chat).


## Validation

**Item R (daemon, CLI, MCP, web, mobile), evidence by AC** (all PRs: `task lint` 0, `task test` 0 on the branch, web `tsc` 0 where web was touched; `task test` needed a rerun on some PRs because of two load flakes, `src/lib/attachments/attachments-encode.test.ts` and `TestArchiveFinished_QueuedPromptMakesTaskNotFinished`, the second root-caused and fixed in #556):

| AC | Evidence |
|---|---|
| 1–5 | `TestClassify_*` (ratelimit), `TestFinish_*`, `TestStatusEnumUnchanged`, `TestMigrate_RunFailureColumns`, `TestRunLogs_*` (#541); GLM zone `TestClassify_GLMResetIsShanghaiTime` (#553) |
| 6 | `TestTaskWait_ExitSixOnRateLimited`, `TestTaskRuns_StatusTextForRateLimited`, `TestMCPTaskStatus_FailureFields`, `TestMCPTaskWait_RateLimitedResult`, `TestMCPRunList_FailureFields`, `TestMCPTaskTree_FailureFieldOnNode`, `TestTaskTree_NodeCarriesLatestRunFailure` (#546) |
| 7 | run-footer, use-run-timeline, task-lineage, use-task-attention, app-sidebar and mobile `runFooter` tests; light and dark screenshots at 1280 and 390 px, reviewed by the Lead and spot-checked by the Supervisor (#550) |
| 8–15 | `TestReplyChat_*`, `TestMCPTaskSend_NotifyOnFinishForwarded`, `TestMCPTaskSend_ProfileSendForwardsFromIds` (#551); `TestParentNotify_*` (19 scenarios incl. quiescence, coalescing, restart scan, no loop, queue full, 32-goroutine `-race`), `TestBuildChildEventBlock_*`, `TestSubtreeBusy`, `TestEnqueueChildEvent_*` (#552); the timeline row and `parseChildEventPrompt` tests (#558) |
| 16 | `TestCooldown_*`, `TestProviderList_CooldownUntil`, `TestMCPProviderList_CooldownUntil` (#549) |
| 17 | daemon, CLI and MCP halves: `fallbackProfileId` on `run.logs`, `task_status`, `task_wait`, `task wait` hint (#555). **Web buttons not built** |
| 18 | `TestPolicy_Wait*`, `TestPolicy_UserCancelWins`, `TestPolicy_SurvivesRestart`, `TestPolicy_ParentHearsRetryNotTheFailure` (#555) |
| 19 | `TestOnRateLimitFallback_*`, `TestRunFailoverEventShape`, `TestRunFailoverEventOnBus` (#557) |
| 20 | `TestFailover_ParentHearsSuccessorOnce`, `TestFailover_CopiesReplyChatAndCountsInTreeGuard` (#557). **Web "continued on" badge not built** |

**Item S, evidence by AC** (same checks on every PR; web `tsc` 0 for #561 and #568):

| AC | Evidence |
|---|---|
| 21 | `TestProfile_LegacyProfileUnchanged`, `TestProfile_PeerForcesChildPolicyNone`, `TestProfile_InstructionsOverCapRejected`, `TestProfile_UnknownSkillRejected`, `TestProfile_ChildProfilesRules`, `TestMigrate_ProfileContractColumns` (#561); no MCP tool writes a profile: `TestMCPServer_NoProfileWriteTools` (#561); identified callers cannot reach `profile.create/update/delete`: `TestRunScoped_MethodAllowlistFailsClosed` (#565) |
| 22 | role text: `TestRunClaude_AppendsInstructions`, `TestRunACP_PrefaceOnNewSession`, `TestRunCodex_DeveloperInstructions`, `TestRun_PromptAndTranscriptStayUserText`, `TestBuildInstructions_*` (#564); MCP entry: `TestMcpRowsFor_OrchestrationAddsBuiltinEntry` (identity env + `SMIND_HOME`), `TestRunClaude_PeerHasStrictMCPAndNoSmindServer`, `TestRunACP_PeerMcpServersOmitSmind`, `TestRunCodex_ManagedRunNotEnforcedNote`, `TestDrive_OrchestrationProfileBuildsMcpPolicyWithRunIdentity` (#565); **live:** the Haiku smoke run and the AC26 run showed Claude Code handing the entry's env to the server |
| 23 | identity is `session.identify` on the connection, set from the registry (not params): `TestSessionIdentify_*`, `TestTaskCreate_ParentDefaultsToIdentity`, `TestRunStart_FromIdsFilledFromIdentity`, `TestRunStart_ForeignFromIdsRejectedWhenIdentified`, `TestUnidentifiedConn_BehaviourUnchanged`, `TestMCPServe_IdentifiesWhenRunIdEnvSet`, `TestMCPServe_NoEnvNoIdentify` (#565) |
| 24 | `TestAdmission_PolicyNoneCallerRejected`, `TestAdmission_LeadLeafOnlyLaunchesPeerProfiles`, `TestAdmission_RunStartRequiresProfileId`, `TestAdmission_RawProviderModeModelRejected`, `TestAdmission_OtherSubtreeRejected`, `TestAdmission_ProfileListShowsOnlyAllowedChildren` (#567; the two load-bearing helpers were also mutation-checked) |
| 25 | `TestSeedRoles_*` (five seats in order, idempotent, an edit survives, peer-glm falls back to peer-sonnet, no Opus), `TestProfileSeedRoles_*` (refused inside a run), `TestTaskSend_WithProfileUsesProfileSettings` (#566) |
| 26 | the AC26 run above: dogfood notes entries 54-63 (local), the Peers' PR #570, the bug fix #569; Paseo was not used as a control run (the Human chose to dogfood directly) |
| sticky seats (design addition) | `TestStickySeat_*`, `TestRunStart_ProfileId*`, `TestTaskSend_ProfileNone*`, `TestMCPTaskSend_ProfileZero*` (#571) |

**Remaining (parked), by decision; nothing here is a regression:**
- **AC17 / AC20 web halves:** "Retry on <fallback>" / "Wait until HH:MM" buttons on a rate-limited run and the failed run's "continued on <provider> in chat #N" footer badge (daemon, CLI, MCP and the `run.failover` event are done); mobile's "Wait" label.
- **AC27 (T1) tree budget:** `tasks.budget_usd`, admission check, "N runs reported no cost". Not built; ADR-0041 Q9 said "later, no default number" until two weeks of `task.tree` cost data exist.
- **AC28 (M1, M2):** the mobile read-only task tree and push for a stuck tree (a root tree whose latest run is rate limited with no failover). Not built.
- **ADR-0027's parked parts** (R5 per-run limits, R6 `handoff_submit`/`handoff_review`, R8 sizing/brief/write-scope warnings): still parked behind their original trigger; `orchestration-roles.md` stays in `active/`.
- **Small follow-ups seen on the way:** `smind profile edit`; the web profile editor does not show the contract or failover fields (S1w); a "≈" marker for the inferred GLM zone; the first measured cost of a Lead wake on a real tree (the AC26 run: $0.08 for the Lead's wake, $0.08 for the Supervisor's); a Supervisor-skill line telling it to trust the Lead's evidence instead of re-running every check; stall detection and heartbeats (Phase 5).

**Not verified:** a real Claude `rate_limit_event` (none seen naturally, no quota burned; covered by the SDK decoder and a fake-CLI scenario); the GLM zone (UTC+8) is an inference from third-party reports plus one consistent run, not a vendor statement; Codex's turn error carries no structured reset (text matcher only); the GLM Peer and a real rate-limit failover were not exercised in the AC26 run.

**Docs-only record for the ADR PR (#530):** ADR numbering, cited ADR/plan/PR existence and the code facts cited there were read on `develop` @ `235d22e9` (see the PR); Paseo citations at `refs/paseo` `8d9e7d3a`.
