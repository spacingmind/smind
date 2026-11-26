# Run events retention (30 days)

Status: spec (docs-only PR), 2026-11-05. Owner: Lead of task `daemon-memory`.
Size: L (7) — scope 2 (store, registry, wsapi, CLI), PRs 2, risk 2 (data
deletion, write locks), decisions 1, verify 1 (synthetic 1 GB db).
Follows `daemon-memory.md` (#822/#823): that work bounded RAM; this bounds the
disk. `smind.db` is 848 MB, almost all `run_events`.

**HUMAN DECISION (2026-11-05):** keep detailed run history for **30 days**.

## Problem

`run_events` rows (tool calls, chunks, outputs) are written for every run and
never deleted. The file only grows, startup and backups pay for it, and nobody
re-reads the detail of a month-old run.

## Decisions (written down so the implementation does not guess)

1. **What is deleted.** Only `run_events` rows of a *finished* run past the
   cutoff. The `runs` row (prompt, status, stop reason, failure class, window,
   reset time, git provenance, profile, origin, ...), `run_usage` (cost,
   tokens), chats, tasks and everything `run.list`, usage rollups and the inbox
   read are kept untouched.
2. **Age basis.** `runs.finished_at < now - historyRetentionDays`. A run with
   NULL `finished_at` is never pruned. `now` is read once per job pass.
3. **Never pruned** (each has a test):
   - runs not finished (`status = 'running'`, and any run with NULL
     `finished_at`); queued prompts are not runs;
   - runs of a **live task**: `tasks.archived_at IS NULL` and `tasks.kind =
     'task'`. A live task keeps its full history whatever its age (the Human's
     rule). Read from the task row at prune time, per run, inside the same
     query that selects candidates (no cache, so archiving or un-archiving
     between passes is honoured). Task *status* is not consulted: `archived_at`
     is what `ArchiveTask` sets and what the UI calls archived.
   - runs of `kind = 'system'` tasks and runs with `purpose != 'user'`
     (learning probes, reflect/curate) are **not** protected by the live-task
     rule: they are never "live" in the user's sense and their task is never
     archived, so protecting them would leak. They are pruned by age like any
     other.
   - pending permissions: a pending request exists only on a running run, so it
     is covered by the first rule; a test pins that a run with an open request
     (still running) is skipped, and that the heredoc reveal for it still works.
   **Open question for the Human (recommend: keep the rule as decided):** many
   users never archive tasks, so the live-task rule may prune little in practice.
   `smind runs prune --dry-run` reports the numbers on a real db so the Human
   can judge; a later `runs.pruneLiveTasks` knob is possible but is NOT in this
   change.
4. **Summary kept on the run row** (so readers keep working): new columns
   `runs.final_text TEXT NOT NULL DEFAULT ''` (the run's trailing assistant
   text, same rule as `trailingAgentText`, capped at 64 KiB on a rune
   boundary) and `runs.history_pruned_at TIMESTAMP` (NULL = detail present).
   Migration: `addColumnIfMissing`, no data backfill (old runs get `final_text`
   when pruned, from their events, before deletion).
5. **Prune is two-phase per run, so a crash leaves a consistent state.**
   Tx 1: compute `final_text` from the run's events, set `final_text` and
   `history_pruned_at` (only if still NULL). From this commit on every reader
   treats the run as pruned and ignores any event rows that remain. Tx 2..n:
   delete the run's `run_events` in bounded batches. A restart (or the next
   pass) finds runs with `history_pruned_at` set that still have events and
   finishes them. A run is therefore never partly visible: it is either full
   or summary-only.
6. **Reader contract on a pruned run.** `History`/`Subscribe`/`Tail` return a
   synthetic history: one `EventTypeSessionNote` "detailed history pruned after
   N days (on YYYY-MM-DD)", then (if non-empty) one `EventTypeText` with
   `final_text`, then `EventTypeDone` with the stored stop reason. `session_note`
   already renders as its own blockquote in `run.logs`, attach, mobile and web,
   so no client change is needed and the result is never empty or an error.
   `run.logs` additionally returns `historyPruned: true` and `historyPrunedAt`
   (additive fields). `ToolCallHeredoc` returns a typed `ErrHistoryPruned`
   (the wsapi reveal maps it to a "history pruned" message, never "no
   heredoc"). `LastAgentText`/`FinalAgentText` return `final_text`.
7. **Space reclaim.** Pruning frees pages SQLite reuses (the file stops
   growing). To shrink the file:
   - the prune job ends each pass with `PRAGMA wal_checkpoint(TRUNCATE)` and,
     when the db is in `auto_vacuum = INCREMENTAL`, `PRAGMA
     incremental_vacuum(N)` in bounded steps (no long lock);
   - new databases are created with `auto_vacuum = INCREMENTAL`;
   - an existing database (auto_vacuum NONE, as today) is converted by
     **`smind db compact`**, which the Human runs: it takes the write lock for
     a full `VACUUM` (this also applies `auto_vacuum=INCREMENTAL`). It refuses
     unless (a) free disk >= 1.2 x db size (VACUUM needs about 2x), (b) no run
     is running, (c) the connection is loopback and not run-scoped. It never runs
     automatically. After the first compact, later passes shrink the file
     incrementally.
   Rejected: automatic VACUUM at idle (an unattended 850 MB rewrite and write
   lock is a surprise); `VACUUM INTO` (doubles disk and needs a swap).
   Freed bytes reported = pages freed x page size (reusable) and, for compact,
   file size before/after.
8. **Mechanics.** Job started by the daemon: first pass 2 minutes after start,
   then every 24 h, one pass at a time (a mutex; `runs prune` while a pass runs
   waits or reports "already running"). Batch: delete at most **2,000 rows per
   transaction** (or stop the transaction after 50 ms), then sleep 20 ms, so a
   live writer waits at most one batch. The job stops promptly on daemon
   shutdown and logs COUNTS only (runs, rows, bytes; never text).
9. **Config.** `runs.historyRetentionDays` (default **30**; `0` = keep forever;
   negative rejected); env `SMIND_RUN_HISTORY_RETENTION_DAYS` (an integer >= 0;
   a bad value is ignored, as the budget env is). Human edit only.
10. **CLI.** `smind runs prune [--dry-run]` (new top-level `runs` command; the
    existing `smind task runs` is unchanged) and `smind db compact`. Both call
    local-only RPCs (`runs.prune {dryRun}`, `db.compact`): not run-scoped, not
    relay, loopback, on the human-only walk lists.
11. **Release note.** The PR title is `perf(store): ...` with a `BREAKING
    CHANGE:` footer so release-please prints it; `public-docs/configuration.md`
    says in a box: **detailed run history older than 30 days is deleted on
    upgrade** (first pass 2 minutes after the daemon starts) and how to turn it
    off (`runs.historyRetentionDays: 0`).

## Reader audit (checklist for the implementation; each gets a test on a pruned run)

| # | Reader | Reads | On a pruned run |
|---|---|---|---|
| 1 | `Registry.History/Subscribe/Tail` (run.logs, run.attach, task_logs MCP, desktop/mobile attach) | events | synthetic summary (decision 6) |
| 2 | `ToolCallHeredoc` (run.revealToolHeredoc) | tool input event | `ErrHistoryPruned` |
| 3 | `LastAgentText` (parent notify, failover, remote notify) | trailing text | `final_text` |
| 4 | `FinalAgentText` (automations) | trailing text | `final_text` |
| 5 | steward `finalRunText` / handoff parse (`wsapi/steward_collect.go`) | events straight from the store | `final_text` (pruned check first) |
| 6 | fleet read `ListRunEventsAfterSeq` (remote hosts) | events | empty page plus `historyPruned` in the read result |
| 7 | learning executor / delivery / probe arm / prevalence | events of runs it just ran, or events joined to runs | zero rows: no error, no panic; a pruned run is simply not a learning input |
| 8 | learning collector watermark (`learning_collect.go`: `EXISTS ... e.id > ?`) | event ids | a pruned run has no events above the watermark: skipped; the watermark logic never assumes contiguous ids (test) |
| 9 | `agentErrorCause`, `limitRefusal`, classifier | a running run's history | unchanged (never pruned) |
| 10 | `run.list`, inbox, usage rollups, tree view | run row, `run_usage` | unchanged |
| 11 | `DeleteTask`/`ForgetWorkspaceLearning` | delete events | unchanged (deleting zero rows is fine) |

## Acceptance criteria

AC1 Config: `runs.historyRetentionDays` default 30, 0 = forever, negative
rejected, env override (`SMIND_RUN_HISTORY_RETENTION_DAYS`) with parity to the
budget env.
AC2 Cutoff: a finished run older than the cutoff (by `finished_at`) loses its
events; one exactly at / younger than the cutoff keeps them (boundary test).
AC3 Never-prune rules of decision 3 each hold.
AC4 The run row, usage, failure class/window/reset, stop reason and provenance
are byte-identical before and after; `final_text` and `history_pruned_at` are
set; `run.list`/inbox output for the run is unchanged.
AC5 Pruned-run readers: run.logs/attach/task_logs return the summary + marker
(never an error, never empty); `historyPruned` fields present; heredoc reveal
is typed `pruned`; tail/elider paths work; the audit table's readers each have a
test (equivalence discipline: a resident-full run and the same data pruned give
the same output for everything the summary keeps).
AC6 Batches: no transaction deletes more than the batch limit; a concurrent
writer is not blocked beyond a bound (test with a live writer measuring wait,
generous margin); two-phase crash safety (interrupt after phase 1 and after
each batch: runs are consistent and the next pass completes).
AC7 `smind runs prune --dry-run` reports exactly the counts the real run then
deletes (run count, event rows, estimated bytes); `smind runs prune` runs now;
log line has counts only.
AC8 Space: pass ends with a WAL TRUNCATE checkpoint and bounded
`incremental_vacuum` when enabled; `smind db compact` guards (disk, running
runs, loopback) and converts to INCREMENTAL; freed bytes reported.
AC9 Measured on a synthetic ~850-990 MB db (`TestGenerateSyntheticDB`, with
runs aged by a generator flag): file size before/after (prune, then compact),
pruning time, writer latency during pruning, startup time. Real `smind.db` and
running daemons are never touched.
AC10 Docs: configuration page, CLI reference, troubleshooting; BREAKING
section in the PR and the release-note footer.

## Named tests (mutation target in brackets)

store: `TestPruneRunEvents_CutoffBoundary` [off-by-one on `<` vs `<=`];
`TestPruneRunEvents_NeverPrunesRunningOrOpenTask` [drop the archived_at
predicate]; `TestPruneRunEvents_SystemRunsPrunedWhenTaskOpen`;
`TestPruneRunEvents_BatchBound` [remove LIMIT]; `TestPruneRunEvents_TwoPhaseCrash`
[set `history_pruned_at` after the deletes];
`TestPruneRunEvents_WriterNotBlocked` (live writer, max wait);
`TestPruneDryRunEqualsReal`; `TestFinalTextCapAndRuneBoundary`;
`TestMigrationAddsPruneColumns`; `TestNewDBIsIncrementalVacuum`;
`TestCompactGuards`.
runs: `TestHistoryOfPrunedRun_SummaryAndMarker` [return empty history];
`TestSubscribePrunedRun`; `TestTailPrunedRun`; `TestHeredocPrunedIsTyped`;
`TestLastFinalTextPruned`; `TestPrunedRunRowUnchanged`.
wsapi: `TestRunLogsPrunedMarker` (fields + session note);
`TestStewardCollectPrunedRun`; `TestFleetReadPrunedRun`; one test per learning
reader on a run with zero events; `TestPruneRPCNotRunScoped`.
config/cmd: `TestConfig_RetentionDays`; `TestRunsPruneCommand` (dry-run
output, usage); `TestDocsDrift` covers the new commands and keys.
daemon: `TestPruneJobStartupAndShutdown` (first pass delayed, stops promptly,
one pass at a time).

## Measurement plan

Isolated home `~/.smind/dogfood/daemon-memory/`: reuse `TestGenerateSyntheticDB`
with a `SMIND_GEN_AGE_DAYS` spread so about 60% of the runs are past the cutoff
and a few belong to an archived vs an open task. Record: file size before;
`smind runs prune --dry-run` counts; a real pass (time, rows/s); writer latency
(a goroutine inserting events at 200/s, p50/p99/max wait) with and without the
pass; size after the pass (expected: unchanged until compact) and after `smind
db compact` (and time, peak disk); incremental passes after the conversion;
daemon startup time before/after.

## PR plan

PR A (this): spec only. PR B: implementation in commits: store (migration,
prune, compact, queries), registry readers, wsapi/steward/fleet readers, job +
config, CLI + docs, tests, measurements. Non-stacked from `origin/develop`.

## Progress

- [x] Spec written and accepted.
- [x] Store: columns + migration, two-phase prune primitives, preview, compact,
  incremental vacuum, new databases created `auto_vacuum=INCREMENTAL`.
- [x] Runs: `Pruner` (daily job + on demand), summary readers, typed
  `ErrHistoryPruned`, registry drops resident copies.
- [x] wsapi: `run.logs` marker fields, `runs.prune`, `db.compact`, steward and
  fleet readers; learning window/prevalence queries skip pruned runs.
- [x] CLI: `smind runs prune [--dry-run]`, `smind db compact`; config + env;
  docs with the upgrade warning.
- [x] Measured on a synthetic ~990 MB database.

## Validation

Synthetic database (`TestGenerateSyntheticDB` with `SMIND_GEN_OLD_PCT=60`: 200
runs x ~4.6 MB, 120 of them 60 days old on an archived task, 80 recent on an
open task), measured with `TestMeasureRetention` (in-process, real file, a live
writer appending an event every 5 ms) and the real binary, macOS arm64. The real
`~/.smind/smind.db` and running daemons were not touched.

| | legacy file (auto_vacuum NONE) | new-style file (INCREMENTAL) |
|---|---|---|
| file before | 939.9 MiB | 944.2 MiB |
| `runs prune --dry-run` | 120 runs, 352,200 rows, 486.7 MiB | same |
| pass duration | 8.3 s | 30.6 s (includes returning 565 MiB to the OS in 200-page steps) |
| file right after the pass | 940.0 MiB (563.4 MiB reusable inside) | 378.6 MiB (565.1 MiB freed) |
| `smind db compact` | 1.7 s, 940.0 -> 377.0 MiB, file becomes INCREMENTAL | not needed |
| live writer, baseline p50 / p99 / max | 36 us / 0.2 ms / 2.1 ms | 242 us / 0.7 ms / 3.4 ms |
| live writer during the pass p50 / p99 / max | 83 us / 11.6 ms / 17.0 ms | 105 us / 22.3 ms / 22.9 ms |
| daemon startup to /healthz (990 MB db) | 0.68 s | |

An earlier run with 2,000-page vacuum steps finished in 18 s but one step held
the write lock for 348 ms; 200 pages keeps the worst wait near 23 ms. End to end
through the binary: `runs prune --dry-run` counts equal the real pass
(120 / 352,200 / 486.7 MiB), `task logs` on a pruned run shows the marker and the
final answer, a second pass prunes 0, `db compact` works.

AC evidence (tests): AC1 `TestConfig_RetentionDays`; AC2
`TestPruneRunEvents_CutoffBoundary`; AC3 `TestPruneRunEvents_NeverPrunesRunningOrOpenTask`,
`TestPruneRunEvents_SystemRunsPrunedWhenTaskOpen`, `TestMarkRunPruned_RechecksEligibility`,
`TestPrune_NeverTouchesOpenTaskOrRunningRuns` (a running run with pending
permissions, and an open task, untouched; archiving the task makes only the
finished runs eligible); AC4 `TestHistoryOfPrunedRun_SummaryAndMarker` (run row
and usage deep-equal before/after except the marker; final text equals the full
history's trailing text); AC5 `TestPrunedRun_EveryReaderServesTheSummary`
(History, Subscribe, Tail, heredoc, Last/FinalAgentText on the same registry,
after a restart and for an unlisted run), `TestRunLogsPrunedMarker`,
`TestStewardFinalRunText_PrunedEqualsFull`, `TestFleetRunEvents_PrunedRunGetsSummary`,
`TestLoadRun_PrunedRunHasNoEvents`, `TestCollectEvents_PrunedRunHasNoEvents`,
`TestCountLoads_PrunedRunIsANoOp`, `TestWait_PrunedRunReturns`,
`TestLearningReadersSkipPrunedRuns`; AC6 `TestPruneRunEvents_BatchBound`,
`TestPruneRunEvents_TwoPhaseCrash`, `TestPrune_CrashResume`,
`TestPruneRunEvents_WriterNotBlocked`, `TestPrune_OnePassAtATime`,
`TestPruneJobStopsOnCancel`; AC7 `TestPrunePreviewEqualsReal`,
`TestPrune_DryRunEqualsReal`, `TestRunsPruneCommand`; AC8 `TestNewDBIsIncrementalVacuum`,
`TestCompactConvertsAndShrinks`, `TestCompactGuards`, `TestRetentionRPCs_RegisteredAndGuarded`
(db.compact refused while a run runs, RPCs refused to agents/relay/non-loopback);
AC9 the table above; AC10 public-docs configuration, CLI reference,
troubleshooting.

Mutations (each killed by an assertion): cutoff `<=`; drop the archived
predicate; drop the running predicate; system runs need an archived task;
remove the batch LIMIT; delete rows of an unmarked run; mark without
re-checking eligibility; resume requires a live task; learning queries include
pruned runs; History returns nothing for a pruned run; heredoc on a pruned run
not typed; keep the resident copy after pruning; steward reader ignores the
marker; fleet reader ignores the marker.

Decisions made while implementing: `final_text` is read with `trailingAgentText`
semantics, so `FinalAgentText` on a pruned run returns the same text (the two
rules differ only when the last events are non-text tool calls, and the
summary then reads as the last text run); the reader contract uses the existing
`session_note` event so no client change is needed; `smind runs prune` with
retention 0 reports "retention is off".

Not done: the live-task exemption means a user who never archives tasks keeps
everything (by decision); no automatic VACUUM (rejected); the web/mobile UI does
not show a dedicated "pruned" badge (the note renders as a paragraph).
