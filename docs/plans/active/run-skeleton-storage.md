# Run skeleton storage: keep the skeleton, rebuild the full history from the provider

Status: **accepted, in implementation** (2026-11-05, Human decision: H1–H8 all as recommended, with H7 narrowed below).
Decision record: [ADR-0053](../../decisions/0053-run-skeleton-storage-and-native-history-rebuild.md) (**Accepted**).
Order of work (Human, 2026-11-05): PR-A docs; PR0 read-only measurement of the real data, thresholds frozen from it; PR1 write-time
chunk coalescing (H3); then finalizer + `native_ref`, rebuilders, read contract, UI. One PR at a time, each reviewed by the Human.
Size: XL (10) — scope 3 (store, runs, taskrunner/new history package, wsapi, fleet, UI), PRs 5, risk 3 (data loss, format drift,
reading files outside smind), decisions 1, verify 1 (synthetic db, three provider formats).
Depends on: #823 (lazy history, byte budget; merged), #827 (30-day retention spec) implemented by #829 (merged; PR C below amends it).

## Problem

`run_events` holds one full row per event forever (848 MB; row per streamed chunk for ACP/Codex; every tool input/result in full).
The provider already stores the same session. smind should keep a small, durable **skeleton** of every run and fetch the full
history from the provider when someone asks. Measured/estimated effect and the reasoning are in the ADR (Measurements).

## Out of scope

Compressing rows; changing the live (running) history; terminal scrollback; changing provider settings; the hosted-relay work;
converting legacy runs (opt-in command only, later); UI redesign beyond a source badge and one button.

## Acceptance criteria

AC1 **Skeleton content.** After finalization a run keeps exactly the ADR-0053 section 1 set: prompts, coalesced answers, per tool
call one row (id, name, ≤ 512 B args summary with heredocs elided, status, duration, exit code, bytes, descriptor), permission
requests/decisions, usage/rate-limit rows, `diffStat`, `runs.native_ref`; thinking becomes a descriptor row. The run row,
`run_usage`, failure class/window/reset and git provenance are byte-identical before and after.
AC2 **Thresholds.** Payload ≤ 512 B inline; 512 B–8 KiB whole in `run_excerpts`; > 8 KiB head 4 KiB + tail 4 KiB with `bytes` and
`sha256` of the whole; `sha256` absent for ≤ 512 B; head/tail cut on a rune boundary; the constants are not configurable.
AC3 **Write full, finalize at finish.** A running run's rows are full and byte-identical to today's; the finalizer runs after
`finish`'s callbacks and the #823 LRU insert; one transaction per run (batched over 32 MiB); rows are self-describing (`"v":2`) so a
mixed run reads correctly; an interrupted finalizer is completed by the next pass; it never touches `status='running'`.
AC4 **Equivalence for what the skeleton keeps.** For every reader in the audit table, the output on a finalized run equals the
output on the same run in full form for the fields the skeleton keeps (text answers, tool names/statuses, final text, permission
outcomes, usage).
AC5 **Rebuild (claude-native, codex-native).** For a finished run whose chat has no running run, `detail=full` returns the events
of its turn from the provider session: claude from the JSONL transcript, codex from `thread/read`/`thread/turns/list` (rollout
file fallback). The slice is selected by prompt hash AND `turnWindow ± 2 min`; ambiguity or no match is `slice_not_found`. The
rebuilt events pass the same elider and redaction as live events.
AC6 **ACP rebuild.** Only for an agent that advertised `loadSession`; only on explicit request (`detail=full`); never for
`session/resume`-only agents; bounded (2 concurrent, 10 s, 64 MB, cancellable).
AC7 **Never an error.** Provider session deleted, rotated, archived away, unreadable, in use, drifted, slice not found, timeout,
unsupported provider, or a path outside the provider root: the response is the skeleton + a `session_note` + `historySource:
"skeleton"` + `skeletonReason`; the call does not fail and is never empty.
AC8 **Version drift.** A transcript/rollout whose self-check fails (prompt hash, ≥ 95% tool pairing, tool count ±10%) is
`format_drift`; 3 consecutive failures for a (provider, version) open a 1 h breaker; golden fixtures per supported CLI version pass
in CI; `smind doctor` reports the supported range.
AC9 **Owner only.** Rebuild executes only on the owning daemon. A fleet read of a finished remote run returns the skeleton and
`owned_by_host:<id>`; a mirror re-syncs on `skeletonRev` change; no remote host ever opens the owner's session files.
AC10 **Heredoc reveal.** Pending permission: reveal from the live request, unchanged. Finished run: body from the rebuilt session,
accepted only when `sha256` equals the skeleton's descriptor for that `index`; otherwise typed `ErrHeredocUnavailable{reason}`;
elision/redaction rules and the `index` counting are unchanged (shared `ElideToolCall`).
AC11 **Machine readers stay deterministic.** steward digest/collector, learning executor/collector, parent notification, automations,
fleet read use `detail=skeleton` and never trigger a rebuild or spawn an agent.
AC12 **Restart/upgrade.** A run running at upgrade (rows full) finishes and finalizes normally; a run left running by a crash is
reconciled as today and finalized at next boot; no event of a running run is changed or lost by the new code.
AC13 **Budget cache.** A rebuilt history enters the #823 LRU, counted against the budget, keyed by the session fingerprint, never
written back to the store; a changed session invalidates it; a rebuild bigger than the budget is served uncached.
AC14 **Retention (#827).** Pruning a skeleton-form run deletes only its `run_excerpts`; skeleton rows, `final_text`, usage, run row
survive; readers return the skeleton with `excerpt_pruned` markers; a full-form run keeps #827's behaviour; never-prune rules,
age basis and two-phase crash safety apply to the excerpt table; `smind runs prune --dry-run` counts equal the real run.
AC15 **Migration.** Legacy runs stay `stored-full` and are untouched at upgrade; `historySource` says so; a running run is never
converted.
AC16 **Security.** `native_ref` is written only by the daemon at run end; a read path is canonicalized and must lie under the
provider root (private instance dirs included), symlinks that leave it are refused; `run.logs detail=full` is authorized like
`run.logs` for human callers; agent/orchestrator callers (`task_logs`, run-scoped agents) get the skeleton and never rebuild (H7); logs carry counts, never text.
AC17 **Measured.** PR0 prints the real distribution on the Human's db (read-only); the PR bodies report db size before/after on the
synthetic db (rows and bytes by chunk-size model), finalizer time per run, rebuild latency per provider (resident vs rebuilt, Go).

## Named tests (mutation target in brackets; each load-bearing test gets ONE mutation, which must FAIL it)

internal/store
- `TestFinalizeRun_KeepsSkeletonSet` (AC1) [drop the `permission*` fields in the skeleton row]
- `TestFinalizeRun_RunRowAndUsageByteIdentical` (AC1)
- `TestPayloadThresholds` table: 0, 512, 513, 8192, 8193, multibyte boundary (AC2) [change `<=` to `<` at 512]
- `TestExcerptHeadTailRuneBoundary` (AC2)
- `TestFinalizeCoalescesAdjacentText_IDsStable` (AC3) [renumber surviving row ids]
- `TestFinalizeInterruptedThenResumed` (AC3) [flip `skeleton_at` before the rewrite]
- `TestFinalizeNeverTouchesRunning` (AC3, AC12) [drop the status predicate]
- `TestStatsReportsBytesByTypeAndBucket` (AC17, PR0)
- `TestMigrationAddsSkeletonColumns` (AC15)
- `TestPruneExcerptsOnly_KeepsSkeleton` (AC14) [make prune delete `run_events` too]
- `TestPruneExcerptsDryRunEqualsReal` (AC14)

internal/runs
- `TestSkeletonHistoryEquivalentToFull` over the F1 elider table of #816 (AC4) [skip elision when building the descriptor]
- `TestHistorySourceLabels` (AC7, AC15)
- `TestRebuiltEntersBudgetCache_KeyedByFingerprint_NotWritten` (AC13) [write rebuilt rows back to the store]
- `TestRebuildSkippedWhileChatRunning` (AC5) [drop the chat-busy check]
- `TestMachineReadersNeverRebuild` (AC11) [route `skeleton` to the rebuilder]
- `TestHeredocFinishedRunRebuiltAndHashChecked` (AC10) [accept a body with a wrong hash]
- `TestHeredocPendingUnchanged` (AC10)
- `TestRunningRunAtUpgradeUntouched` (AC12)
- `TestPrunedSkeletonRunReader` (AC14)

internal/history/native (new)
- claude: `TestClaudeSliceByHashAndWindow` [match by hash only; the repeated-prompt case fails];
  `TestClaudeAmbiguousSliceDegrades`; `TestClaudeGoldenFixtures` (one per supported version);
  `TestClaudeUnknownRecordTypesSkipped`; `TestClaudeDriftSelfCheck` (AC8) [raise the pairing threshold check to always pass]
- codex: `TestCodexThreadReadSlice`; `TestCodexRolloutFallback`; `TestCodexGoldenFixtures`; `TestCodexArchivedRollout`
- ACP: `TestACPLoadOnlyWhenAdvertised` (AC6) [call `session/load` without the capability]; `TestACPResumeOnlyAgentDegrades`;
  `TestACPReplayCollectedAndBounded`
- `TestPathOutsideProviderRootRefused`, `TestSymlinkEscapeRefused` (AC16) [skip the canonicalization]
- `TestDeletedSessionGivesSkeletonNotError`, `TestRotatedSessionGivesSkeleton`, `TestTimeoutGivesSkeleton` (AC7)
- `TestBreakerOpensAfterThreeDrifts_ClosesAfterAnHour` (AC8) [off-by-one on the count]
- `TestRebuildConcurrencyAndMemoryBounds` (AC6)

internal/wsapi + internal/fleet
- `TestRunLogsDetailModes` (skeleton/full/auto, `historySource`, `skeletonReason`) (AC5, AC7)
- `TestStewardCollectSkeletonRun`, `TestLearningCollectorSkeletonRun` (id gaps, watermark), `TestLearningExecutorSkeletonRun`,
  `TestParentNotifyLastTextSkeleton`, `TestAutomationFinalTextSkeleton` (AC4, AC11)
- `TestFleetReadSkeletonOwnedByHost` + `TestFleetMirrorResyncOnSkeletonRev` (AC9) [ignore `skeletonRev`]
- `TestRelayAttachFinishedRunBackfillsSkeleton`, `TestMobileDoesNotAutoLoadACP` (AC6)
- `TestRebuildAuthorizedLikeRunLogs` (a run-scoped agent never triggers a rebuild, H7; a human caller is authorized like run.logs) (AC16)

cmd / config / docs
- `TestDoctorReportsProviderSessionRetention` (H8), `TestDocsDriftCoversDetailAndSkeletonKeys`, `TestRunsPruneCommandExcerptOutput`

web (PR D): component tests for the source badge, the "Load full history" button, the marker blockquote; light+dark screenshots at
1280 and 390 of: skeleton, rebuilt, excerpt-pruned, drift, owned-by-host.

## Reader audit (each row gets a test above; re-run as a grep in every PR and list misses in its body)

| # | Reader | Today | After | `detail` |
|---|---|---|---|---|
| 1 | `Registry.History/Subscribe/Tail` (run.logs, run.attach, task_logs, desktop/mobile) | live or full rows | live (running); finished: cache, else skeleton, upgrade by rebuild | `auto` |
| 2 | `ToolCallHeredoc` (reveal) | tool input event | pending: live; finished: rebuilt + hash | `full` internally |
| 3 | `LastAgentText` / `FinalAgentText` | trailing text | skeleton text row / `final_text` | `skeleton` |
| 4 | steward `finalRunText`, handoff parse (`steward_collect.go`) | rows from the store | skeleton rows | `skeleton` |
| 5 | learning executor, delivery, probe arm, prevalence | rows from the store | skeleton rows; excerpt tier only inside 30 d; absence is not an error | `skeleton` |
| 6 | learning collector (`learning_collect.go`, id watermark) | event ids | in-place rows, gaps allowed; test with coalesced ids | `skeleton` |
| 7 | fleet read `ListRunEventsAfterSeq` | rows by seq | skeleton, `skeletonRev`, `owned_by_host` | `skeleton` |
| 8 | relay/mobile attach, backfill | `Subscribe` | row 1 | `auto` (no ACP auto-load) |
| 9 | `agentErrorCause`, `limitRefusal`, classifier, failover tail | running run's memory | unchanged | n/a |
| 10 | `run.list`, inbox, usage rollups, tree | run row, `run_usage` | unchanged | n/a |
| 11 | `DeleteTask`, `ForgetWorkspaceLearning` | delete rows | also `run_excerpts` | n/a |
| 12 | #827 prune, `smind runs prune`, `db compact` | delete rows | excerpts only for skeleton runs (AC14) | n/a |

## Decisions (open ones are the ADR's H1–H8; defaults used until answered)

- D1 skeleton set; D2 thresholds; D3 write full / finalize at finish; D4 rebuilder + slice rule; D5 read contract and `detail`;
  D6 degrade, never error; D7 drift; D8 owner-only; D9 legacy runs left (all in the ADR).
- Human answers (2026-11-05): H1 thinking not kept; H2 skeleton forever (`runs.skeletonRetentionDays` reserved, not built); H3
  coalescing first; H4 (a, left to the 30-day prune #829) plus a later opt-in `smind runs skeletonize`; H5 auto for claude/codex, explicit for ACP; H6 thresholds
  frozen only after PR0; **H7 orchestrators/agents get the skeleton only through `task_logs` and never trigger a rebuild (only a
  human request rebuilds; this narrows AC16's `detail=full` for agents)**; H8 doctor warning only, never edit CLI settings.

## PR split (non-stacked from `origin/develop`; A and B may run in parallel, C after #827, D after C, E after B)

| PR | Scope | Write scope (one owner) | Depends |
|---|---|---|---|
| **A** `perf(runs): coalesce persisted chunk rows` (H3) + **`smind db stats`** read-only report (PR0: bytes/rows by event type, by provider, payload-size histogram, rows per run) | persist buffer in `Registry.record` path (flush on boundary, 250 ms, 4 KiB, Done, shutdown; subscribers still get every chunk); stats RPC + CLI | `internal/runs/persist.go`, `registry.go` record path, `internal/store/stats.go`, `cmd/smind` | none |
| **B** skeleton form | `run_excerpts`, `runs.native_ref/skeleton_at/skeleton_rev`, `v:2` shape, finalizer + boot pass, readers (`historySource`), native_ref capture at run end, thresholds | `internal/store`, `internal/runs`, read-only use of `taskrunner.SessionHandle` | A optional |
| **C** retention amendment | prune excerpts only for skeleton runs, reader contract, dry-run counts, docs | `internal/store` prune, `internal/runs` readers, docs | B and #827 merged |
| **D** rebuilders | `internal/history/native` (claude, codex, ACP), slice rule, drift detector + breaker, fixtures, cache integration, `detail`, heredoc reveal, wsapi/doctor | new package, `internal/wsapi` run.logs/attach/reveal, `internal/runs` cache hook | B |
| **E** consumers + UI | steward/learning/automation/parent-notify on `skeleton`, fleet `skeletonRev`/`owned_by_host` + mirror, relay/mobile, web/mobile badge + "Load full history" | `internal/steward`, `internal/learning`, `internal/fleet`, `web/`, `mobile/` | B, D |

Complexity points: A 2, B 3, C 1, D 3, E 2 (sum 11). A Lead per PR; peers only for the golden-fixture capture (peer-glm, step by
step) and screenshots (peer-sonnet).

## Measurement plan

Isolated home `~/.smind/dogfood/run-skeleton/` (never the real db): extend `TestGenerateSyntheticDB` with the payload-size
distribution sampled from real transcripts (done as a scratch script for the ADR; PR A moves it into the test) and a chunk-size
flag. Record: rows and bytes per chunk-size model (20/40/100 B/block) for current, coalesced, skeleton, skeleton-after-excerpt-prune;
finalizer ms per run and per 1,000 runs; rebuild ms per provider (claude 126 KB / 3 MB / 55 MB files, codex rollout, codex RPC,
ACP fake agent) resident vs rebuilt; startup time; `run.logs` latency skeleton vs rebuilt. The Human runs `smind db stats` once on
the real db and sends the report so H6 is decided on facts.

Already measured for the ADR (scratch script, sizes only from this machine's own transcripts, real db untouched): payload sizes
(Claude p50 295 B / p99 10 KB / max 58 KB; Codex p50 1.2 KB / p99 40 KB), per 60 simulated runs the sizes in the ADR table
(current 24.6–87.9 MB; skeleton+excerpts 19.8 MB; skeleton-only 10.9 MB), and parse time of the largest local transcript
(55.7 MB in ~150 ms, Python).

## Risks and how the plan answers them

- Provider formats change without notice: self-check + degrade + breaker + golden fixtures + doctor (AC8).
- A session is deleted by the provider: the skeleton marker, never an error (AC7); H8 doctor warning.
- Reading outside smind's home: root canonicalization, owner-only, same authorization as `run.logs` (AC9, AC16).
- Silent loss: legacy runs untouched, running runs untouched, finalizer idempotent, self-describing rows (AC3, AC12, AC15).
- Learning/steward regressions: equivalence tests per reader (AC4, AC11).

## Progress

- [x] Research: provider sessions, formats, sizes, #823/#827 (2026-11-05).
- [x] ADR-0053 (Proposed) and this plan written.
- [x] Steward review; Human answers H1–H8 (2026-11-05, accepted).
- [x] PR-A docs (#831).
- [ ] PR0 measurement: `smind debug run-stats --db <snapshot>` (hidden, offline, read-only; `internal/runstats`) built and tested on a synthetic snapshot; real-data run and frozen thresholds pending.
- [ ] PR1 coalescing.
- [ ] PR A, B, C, D, E (not started).

## Validation

(filled in by the implementation PRs; this PR is docs-only and changes no code)
