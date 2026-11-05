# Daemon memory: lazy run history, byte-budget cap, diagnostics

Status: spec (docs-only PR), 2026-11-05. Owner: Lead of task `daemon-memory`.
Size: L (7) — scope 1, PRs 2, risk 2 (data loss, concurrency), decisions 1, verify 1.

## Problem (measured on the Human's Mac, 2026-11-05)

The desktop-managed daemon (pid 79119, v0.18.0, up since 2026-11-03, idle, 19
threads) has `phys_footprint` 1,158 MB; 1,152 MB of it is untagged dirty memory
= the Go heap. `~/.smind/smind.db` is 848 MB (mostly `run_events`). Paseo's
daemon: 337 MB.

## Cause (read in code, 2026-11-05)

`internal/runs/registry.go`:

- `finishedRetentionCap = 200` bounds the **count** of finished user runs kept
  in `reg.runs`; each carries its **full** `history []Event` (line 603).
- `New()` calls `st.ListRecentRuns(200)` and `rehydrateRun` for each; the latter
  runs `st.ListRunEvents(row.ID)` (every row, `event_data` as string) and
  `decodeEvent`s each into `history`. So the heap is high from boot regardless
  of use, and transiently holds each run twice (rows + decoded events).
- The cap's comment ("a small, fixed amount of memory") is false for agent
  runs: hundreds of tool calls with big file reads and test output, ~5 MB each,
  200 x 5 MB ~ 1 GB. A live run's events additionally keep `Event.Raw` (the
  backend-native value), which is not persisted, so a live history is larger
  than the stored one.
- Rehydrating also happens per call: `storedHistory` (an evicted run asked via
  `History`) rehydrates the *whole* run to read `.history`.
- `Subscribe` (run.attach) on an evicted run returns `ErrNotFound` (it only
  consults `reg.runs`). That is a latent gap, fixed here (AC4).

## Acceptance criteria

AC1 **Lazy history at boot.** `New()` rehydrates run *metadata only* (row
fields; no `ListRunEvents`). Heap after `New()` with 200 seeded finished runs of
~5 MB history each stays under a bound independent of history size (AC-T1).
Startup time does not regress (it drops).

AC2 **Complete logs for any non-resident run.** A finished run whose history is
not resident (never loaded, or evicted by the budget) serves its COMPLETE
history from the store through every reader below, byte for byte equal to the
events that were recorded (round-trip through `encodeEvent`/`decodeEvent`, the
same view a restarted daemon has today). "A run's own output is never silently
incomplete" holds: nothing is ever truncated, only unloaded.

AC3 **Byte budget replaces the count as the memory bound.** New config key
`runs.historyBudgetMB` (yaml, default **64**, `0` = default, negative rejected;
env `SMIND_RUN_HISTORY_BUDGET_MB` overrides, as the other daemon env knobs do).
The sum of estimated bytes of resident histories of **finished** runs never
exceeds the budget after any registry operation settles; when it would, the
least-recently-used finished run's history is dropped (history slice nil,
metadata kept) until it fits. **Running runs are never touched** (their history
is neither counted against nor evicted by the budget). A single finished run
larger than the whole budget is not kept resident (served from the store each
time) — it is never truncated. The count cap stays as a secondary bound on
*metadata* entries in `reg.runs` (200 user, 50 system; unchanged semantics for
`run.list`).

Default 64 MB, justified: Go's live-heap target with GOGC=100 is ~2x live, so
64 MB of history gives ~130 MB worst-case history-related heap on top of a
~40-60 MB runtime baseline: inside the 150-200 MB target, and it still holds
the 10-20 most recent big runs warm (typical use is "re-open the run I just
finished"). 128 MB would push the idle worst case past 250 MB. To be confirmed
by measurement (below); the default may move within 64-128 MB with the numbers
in the PR body.

AC4 **`run.attach` works on a non-resident finished run.** `Subscribe` on a
finished run whose history is not resident backfills the full history from the
store, then closes (same observable contract as attaching to a rehydrated run
today). Unknown ids and system runs still `ErrNotFound`.

AC5 **Tail from the store, elider-correct.** `run.logs` with `tail=N` on a
non-resident finished run reads only the last N rows from the store (plus
context for the elider, below), not the whole history. Output equals the output
of "convert the full history, then take the last N" (the F1 elider #816): for a
tool-call event in the tail whose input (carrying the heredoc bodies) was
recorded *before* the tail window, the elider is primed with that input.
Design: the tail query is `WHERE run_id=? ORDER BY seq DESC LIMIT N`; collect
the tail's distinct non-empty `toolCallID`s; scan backwards in blocks of 256
rows until each ID has been seen with a non-empty `ToolInput` (the elider keeps
the *last* input-bearing event per id, but only an event before the first tail
occurrence is missing, and any earlier input-bearing one has the same input in
practice; the equivalence test pins the exact rule), or the start is reached;
feed only the matching earlier events to the elider, then convert the tail. The
scan is bounded by the run's own history and runs only when the tail contains
such an ID with no input in the tail. `tail=0` (full) loads the whole history
(inherently O(history) for one response) but through the streaming decoder
(AC6) and without caching it when over budget.

AC6 **Hidden O(history) costs removed from hot paths.** Each consumer in the
audit table below reads the store (tail/last-text query) or the budgeted
cache, never forces a full rehydrate of an evicted run just to answer a small
question. `ListRunEvents` gets a streaming sibling (decode while scanning, one
row in flight) so a load holds one copy, not two.

AC7 **Diagnostics.** (a) Optional pprof: OFF by default; enabled by config
`debug.pprof: true` or env `SMIND_PPROF=1`. Mounted at `/debug/pprof/` on the
daemon's HTTP server only when enabled; every request must (i) carry the daemon
token (`auth.RequireToken`), (ii) come from a loopback peer (`r.RemoteAddr`,
checked on the TCP peer, never from `X-Forwarded-For`/`Host`), (iii) not arrive
over a relay or a password session. Refusal is 404 when disabled, 401/403
otherwise. It is never reachable through `/ws`, so run-scoped agents cannot call
it (not in `RunScopedMethods`; walk-test entry). If the daemon listens on a
non-loopback address, pprof still answers only loopback peers. (b) `smind debug
memory` prints Go `runtime.MemStats` (HeapAlloc, HeapInuse, HeapSys, NumGC,
GC pause total), process RSS and, on macOS, `phys_footprint`, plus registry
stats (runs in memory, resident-history runs, resident bytes, budget, loads,
evictions, store-served reads). Transport: a new wsapi method `debug.memory`,
admin-only (loopback + not run-scoped + not plugin-callable + not relay), so it
works without pprof being enabled. (c) docs: `docs/` section + config
reference.

AC8 **Measured result.** Idle daemon with 200 large (~5 MB) finished runs:
`phys_footprint` <= 150-200 MB (target), startup time not worse, `run.logs`
latency for resident vs evicted reported, lazy-history SQL indexed and short
(below). Numbers in the PR body.

## Consumer audit (every reader of in-memory history, found by grepping `r.history`, `.History(`, `.Subscribe(`, `ListRunEvents`)

"Now" is the behaviour of the code today; "After" is what this change makes it.

| # | Consumer | File | Now | After |
|---|---|---|---|---|
| 1 | `Registry.Subscribe` (run.attach, task.prompt streaming, executor, probe arm) | registry.go:1799 | copies `r.history` under `r.mu`; evicted run: `ErrNotFound` | running: live history (unchanged). Finished: `historyOf(r)` (cache or store); evicted/metadata-only run: also served (AC4) |
| 2 | `Registry.History` | registry.go:1844 | live history copy; evicted: `storedHistory` rehydrates the whole run | running: live. Finished: budgeted cache or streaming store load, no rehydrate of metadata |
| 3 | run.logs `runLogEvents` | wsapi/handlers.go:1976 | `History` then converts the FULL history, then slices tail (O(history) per call) | new `Registry.Tail(runID, n)`; AC5 tail-with-elider-context |
| 4 | heredoc reveal #813 `ToolCallHeredoc` | runs/heredoc_reveal.go:42 | `History` then scans for the input | store query for the toolCallID's input-bearing event (indexed, newest first); live run: memory. Same bytes as before, equivalence test (AC-T6) |
| 5 | `agentErrorCause(r.history)` in `finish` | registry.go:1592 | memory, only on a running run | unchanged (run is live, history resident) |
| 6 | `trailingAgentText(r.history)` in failover | failover.go:255 | memory, run just ended | unchanged (just ended, still resident; budget evicts only after `finish` completes its callbacks) |
| 7 | `trailingAgentText` in `limitRefusal` (registry.go:2285) | | memory, run just ended | unchanged |
| 8 | failure class/classifier | runs/failure, finish | fed from `agentErrorCause`/rate signal captured live | unchanged; no history read after finish |
| 9 | `PendingPermissions` | pending.go:33 | scans `r.history` only when `len(pendingPermissions) > 0` (so only running runs) | unchanged |
| 10 | parent notification `parentnotify.go:199` (child events) | | `History(run.ID)` then `trailingAgentText` | `Registry.LastText(runID)`: store query for the trailing text window (cache hit when resident) |
| 11 | automation `automation_wire.go:101` | | `History` then `FinalText` | `Registry.FinalText`-style store/cache read of the last events; no full load |
| 12 | steward digest / collector `wsapi/steward_collect.go:269` | | `db.ListRunEvents(runID)` straight from the store (already store-based) | streaming variant; audited only (does not use the registry) |
| 13 | `task_logs` MCP (cmd/smind) | | calls `run.logs` | inherits #3 |
| 14 | learning executor/probe `ListRunEvents`, delivery, prevalence | learning/* | read the store directly | unchanged (already store); executor's `reg.History`/`Subscribe` follow #1/#2 |
| 15 | desktop/mobile attach, timeline backfill | wsapi `run.attach` | via `Subscribe` | inherits #1 (AC4) |
| 16 | `run.list` / `List` | list.go | metadata + `withStoredUsage` | unchanged (metadata only, no history) |
| 17 | usage rollups | store | `run_usage` rows | unchanged (never read history) |
| 18 | `devharness` (relay/bridge) | | `History` | dev-only, follows #2 |

The audit is re-run as a grep in the implementation PR and the table in the PR
body lists any consumer found that this one missed.

## Design

State on `*run` (all guarded by `r.mu`): `history []Event` (nil when not
resident), `histLoaded bool`, `histBytes int64`, `histElem` (LRU list node). A
rehydrated run starts `histLoaded=false`; a running run is always loaded.
`reg.hist` (a small struct under `reg.mu`): LRU list of finished, loaded runs,
`resident int64`, `budget int64`, counters.

- **record** (running): append, add to `r.histBytes`; never evicts anything.
- **finish** (after all the callbacks above): the run enters the LRU as most
  recent with its `histBytes`; `reg.enforceBudget()` drops LRU entries until
  `resident <= budget` (an entry bigger than the budget drops itself).
- **historyOf(r)** for a finished run: if loaded, touch LRU and return a copy;
  else stream-load from the store (outside `reg.mu` and `r.mu`), then insert
  under both locks (re-check `histLoaded` to dedupe racing loaders), enforce the
  budget. Never called with `reg.mu` held while doing I/O.
- **Lock order**: `reg.mu` then `r.mu` (existing). Store reads happen with
  neither held.
- Byte estimate `eventBytes(e)`: `unsafe.Sizeof(Event)` + len of every string
  and `json.RawMessage` + a fixed allowance for slices/pointers. It is an
  estimate (documented as such, tested against `runtime.MemStats` within a
  generous factor); `Raw` is not counted when nil (finished, rehydrated runs);
  a live run's `Raw` is dropped from the resident copy at finish (it is not
  persisted, so a store reload would lack it anyway — equivalence with the
  restart view).
- **Subscribe/History on a finished run** copy the slice (as today); the cost
  of a copy is proportional to the history and short-lived.
- **Store**: `run_events(run_id, seq)` has `UNIQUE(run_id, seq)` (an implicit
  index), so `WHERE run_id=? ORDER BY seq [DESC] LIMIT n` is an index range
  scan, no sort. `EXPLAIN QUERY PLAN` asserted in a store test (no
  `USE TEMP B-TREE`, uses the autoindex). `idx_run_events_run_id` is redundant
  with it; left alone (not part of this change). Reads use the reader pool
  (WAL: readers do not block the single writer; the full load is streamed so no
  long-lived large `[]RunEvent`).
- **Out of scope**: pruning `run_events` / DB compaction (848 MB on disk is a
  separate disk question, not RAM), changing wire fields, GOMEMLIMIT/GOGC
  tuning (reported as a follow-up if the numbers need it), terminal scrollback
  (already bounded separately).

## Test scenarios (names are the contract; mutation target in brackets)

internal/runs:
- T1 `TestNewRehydratesMetadataOnly_HeapBounded`: seed 200 finished runs with
  ~5 MB histories directly in the store (synthetic big events); `GC` + `ReadMemStats`
  before and after `New`; delta `HeapAlloc` < 64 MB (generous: full load is ~1 GB);
  `reg.runs` has 200 entries, none resident. [mutation: re-add `ListRunEvents` to
  `rehydrateRun` — fails]
- T2 `TestEvictedRunHistoryComplete`: after budget eviction `History` returns events
  byte for byte equal (via `encodeEvent`) to the seeded ones. [mutation: stop at
  the first N rows in the stream loader — fails]
- T3 `TestSubscribeEvictedRunBackfillsAll`: attach to a non-resident finished run:
  receives the full history in order, channel closes. [mutation: return
  ErrNotFound for non-resident — fails]
- T4 `TestHistoryBudget_OldestFirstAndBounded`: finish runs of known size over the
  budget; `resident <= budget` after every finish; the evicted ones are the LRU
  ones; touching a run (History) moves it to most-recent. [mutation: evict newest
  first — fails]
- T5 `TestBudgetNeverEvictsRunningRun`: a running run with history far over the
  budget keeps all events resident and complete while others are evicted;
  finishing it then makes it evictable. [mutation: include running runs in the
  eviction sweep — fails]
- T6 `TestOversizedRunServedFromStoreNotTruncated`: one run bigger than the budget
  is not resident and still returns complete history.
- T7 `TestConcurrentLoadsDeduped`: 32 goroutines `History` of one evicted run:
  one store load, identical results, `-race` clean. [mutation: remove the
  `histLoaded` re-check — resident bytes double counted, test fails]
- T8 `TestTailMatchesFullConversion` (wsapi, table over the F1 elider cases: no
  heredoc, heredoc call+result both in tail, call before tail and result in tail,
  result in tail with input in an earlier update, call with two bodies, `tail`
  > len, `tail=0`): for each case, resident run output == evicted run output ==
  "convert all then slice". [mutation: skip the elider priming — the
  call-before-tail case fails]
- T9 `TestHeredocRevealEquivalentResidentAndEvicted` (runs): whole #813 reveal table
  against a resident and an evicted run; identical text/errors. [mutation: store
  query picks the first instead of the last input-bearing update — fails]
- T10 `TestLastTextAndFinalText_EvictedEqualsResident`: parent notification and
  automation final text for resident vs evicted.
- T11 `TestFinishCallbacksSeeHistoryBeforeEviction`: with a tiny budget the failover
  tail, `agentErrorCause` and limit refusal still see the just-finished run's
  history (eviction runs after finish's consumers).
- T12 `TestRaceStartHistoryEvict` under `-race`: concurrent record/finish/History/
  Subscribe/eviction.

internal/store:
- T13 `TestRunEventsTailQueryPlan`: `EXPLAIN QUERY PLAN` of tail and stream queries
  uses the (run_id, seq) index, no temp b-tree. [mutation: change the query to
  `ORDER BY id` — plan test fails]
- T14 `TestStreamRunEventsMatchesList`: streaming iterator yields the same rows as
  `ListRunEvents`, stops on callback error.

wsapi/server/cmd:
- T15 `TestPprofOffByDefault`: 404 without config/env.
- T16 `TestPprofRequiresTokenAndLoopback`: enabled; no token → 401; token +
  non-loopback `RemoteAddr` → 403; spoofed `X-Forwarded-For: 127.0.0.1` from a
  non-loopback peer → 403; token + loopback → 200. [mutation: trust
  `X-Forwarded-For` — fails]
- T17 `TestDebugMemoryNotRunScopedNotPlugin`: `debug.memory` absent from
  `RunScopedMethods` and plugin/relay lists; the cmd walk-test lists it; a
  run-scoped connection calling it gets forbidden.
- T18 `TestDebugMemoryReportsRegistryStats`: after a load, counters move.
- T19 config tests: default 64, negative rejected, env override; docs drift test
  (`docs_drift_test.go`) covers the new config key and command.

## Measurement plan (AC8)

Isolated daemon under `~/.smind/dogfood/daemon-memory/home` (own `SMIND_HOME`,
own port; never the real `~/.smind`). A generator (Go test helper / small
program in the plan dir) writes a **synthetic** `smind.db`: ~850 MB, 200 recent
user runs with ~5 MB histories (long tool results, test output) plus older
chunk history. Record for **before** (develop `ba127b23`) and **after**:

- `phys_footprint` (`footprint <pid>` / `vmmap -summary`) and `ps -o rss`,
  idle after boot + 60 s;
- `runtime.MemStats` via `smind debug memory` (after) / pprof heap (before);
- startup time to `/healthz` ready;
- `run.logs` latency: resident vs evicted, full and `tail=50`;
- `EXPLAIN QUERY PLAN` output and the longest write-lock wait seen while a full
  load runs concurrently with appends on a running run (WAL).

## Decisions

1. Budget unit = **bytes of resident history of finished runs**, default 64 MB,
   config key above. Rejected: lower the count (does not bound bytes), TTL
   (idle daemons keep the heap), mmap tricks.
2. LRU, not oldest-finished, for eviction order: the Human's brief says oldest
   first; LRU is a superset of that (untouched runs leave in finish order) and
   keeps a just-reopened old run warm. If the Steward wants strictly
   finish-ordered, it is a one-line change; both pass T4.
3. Serve from the store, never re-register an evicted run in `reg.runs` beyond
   the metadata cap.
4. pprof is HTTP on the existing listener (needs the `net/http/pprof` handler
   set), `debug.memory` is a wsapi method so the CLI needs no extra port.
5. No wire change, no store migration (no schema change).

## PR plan

PR 1 (this): spec only. PR 2: implementation in small commits (store
streaming + tail queries; registry lazy/budget; consumers; diagnostics;
tests; docs; measurements). Both non-stacked from `origin/develop`.

## Progress

- [x] Spec written (this commit).
- [ ] Spec reviewed by the Steward.

## Validation

(filled in by the implementation PR: per-AC evidence, mutations, measurements)
