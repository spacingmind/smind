# Run/conversation history: survive a daemon restart

## Acceptance Criteria

- New tables in `internal/store/schema.sql`: `runs` (mirrors `runs.RunStatus`:
  id, task_id, provider, prompt, status, started_at, finished_at,
  stop_reason, err_msg) and `run_events` (run_id, a per-run strictly
  increasing `seq`, a JSON `event_data` TEXT column, created_at) — following
  this repo's existing convention of storing structured payloads as
  JSON-in-TEXT (`accounts.credential_data`, `quota_snapshots.usage_data`,
  `spaces.env_data`) rather than a normalized column per `Event` field.
  `run_events` has a `UNIQUE(run_id, seq)` and an index on `run_id`.
- `internal/store` gains CRUD for both tables, in the same style as
  `tasks.go`/`workspaces.go` (`CreateRun`, `UpdateRunStatus`,
  `AppendRunEvent`, `ListRecentRuns`, `GetRun`, `ListRunEvents`) — same
  error-wrapping (`fmt.Errorf("...: %w", err)`), same `scanX(rowScanner)`
  pattern, same UTC timestamp stamping.
- `runs.Registry` takes a `*store.Store` directly at construction
  (`runs.New(st *store.Store)`), matching `workspace.Manager`'s existing
  directness convention — no new interface, since no second implementation
  exists or is anticipated.
- **Write path**, all synchronous (write-through, not batched/async — see
  Decisions):
  - `Registry.Start` persists the new `runs` row before returning the run
    ID, so a run recorded as started is queryable even if the daemon dies
    immediately after.
  - `Registry.record` persists each `Event` as a `run_events` row
    (`Event.Raw` excluded — see Decisions) alongside its existing in-memory
    append, with a per-run monotonically increasing `seq`.
  - `Registry.finish` persists the terminal status, `finished_at`, and
    `stop_reason`/`err_msg`.
- **Startup reconciliation + rehydration**, both inside `runs.New`, before
  the Registry is handed to any caller:
  - Any `runs` row still `status='running'` is transitioned in the DB to a
    new terminal status `StatusInterrupted` ("interrupted") — its
    subprocess is definitely gone (see
    `internal/runs/registry.go`'s `CloseAll` doc comment on why nothing
    ties a `run.start`-originated subprocess to the daemon's lifetime) and
    cannot be resumed, matching `internal/taskrunner`'s one-shot-subprocess-
    per-turn design.
  - The most recent `finishedRetentionCap` (200) persisted runs (each with
    its full event history) are loaded into the in-memory map as
    already-terminal runs. This means `run.list`/`run.attach`/`run.logs`
    (the existing wire methods — no new ones needed) keep serving recent
    run history exactly as before a restart, with zero frontend changes:
    `run.attach` on a rehydrated run immediately delivers backfill (its
    full history) then closes, same as any other already-finished run
    today.
- Existing in-memory eviction (`finishedRetentionCap`) is unchanged —
  persistence changes what survives a restart, not the memory bound.
- Explicitly out of scope (see Decisions): reaching persisted runs beyond
  the rehydration window once evicted from memory; the separately-noted,
  undecided `workspace.Manager.RunTask` dead-code / stuck `tasks.status`
  finding; terminal/PTY session persistence.

## Test Scenarios

- `internal/store`: insert a run, append several events, read them back in
  `seq` order; update status to each terminal value; list recent runs in
  the right order; a run and its events surviving `Store.Close` +
  `store.Open` on the same file path (mirrors
  `TestStore_ReopenExistingDatabase`).
- `internal/runs`:
  - Starting a run makes it queryable via the store immediately (before any
    event is recorded).
  - Events recorded during a real run (`fakeagent`, as used throughout this
    package's existing tests) are persisted in order — verify by reading
    directly from the store, not just from `Registry.History`.
  - Finishing a run (done/error/stopped) persists the matching terminal
    status + `finished_at` + `stop_reason`/`err_msg`.
  - **Restart simulation**: build a `Registry` against a real (temp-file,
    not `:memory:`) store, drive a run to completion, discard the
    `Registry`, construct a *new* `Registry` against the same store file,
    confirm `List`/`History` return that run with identical history.
  - **Interrupted reconciliation**: leave a run row at `status='running'` in
    the store (drive a `"hang"`-scenario run, then discard the `Registry`
    without calling `CloseAll`/`Stop` — simulating a crash, not a graceful
    shutdown), construct a new `Registry` against that store, confirm the
    rehydrated run comes back as `StatusInterrupted`, not `StatusRunning`,
    and that it behaves like any other terminal run (no live `closedCh`
    wait needed for it — see Decisions on rehydrated runs never being
    "running" in the new Registry's eyes).
  - Run the full package suite with `-race -count=3`: persistence writes on
    the `record`/`finish` hot path must not introduce a new race against
    the existing `subscribe_race_test.go` coverage.
- `internal/wsapi`: existing `run_test.go` suite passes unchanged with a
  real store threaded through; add a case confirming `run.list`/
  `run.attach`/`run.logs` work identically for a rehydrated (post-restart)
  run as for a run that finished in the current process.
- Manual/E2E, extending the same live-binary shape used for the
  `CloseAll` fix (`docs/plans/completed/...` once that lands): start the
  real daemon, `run.start` a turn to completion, restart the daemon,
  confirm `run.logs` for that ID still returns full history. Separately:
  `run.start` a `"hang"` scenario, `SIGKILL` the daemon (no graceful
  `CloseAll`), restart, confirm that run now shows `"interrupted"`.
- `go build ./...`, `gofmt -l`, `go vet ./...`, `go test -race -count=3
  ./...` clean.

## Decisions

- Direct `*store.Store` dependency on `Registry`, not an interface — same
  reasoning as `workspace.Manager`'s existing shape.
- Synchronous write-through persistence, not batched/async: this project's
  stated preference against premature optimization, and self-hosted/
  personal-scale event volume doesn't warrant it. Revisit only with a real
  measured bottleneck.
- Rehydrate-into-`Registry` at startup rather than a separate DB-backed
  query path or new wire methods: keeps `run.list`/`run.attach`/`run.logs`
  as the single surface for both live and historical runs, so the frontend
  needs no changes at all for this feature.
- New `StatusInterrupted`, distinct from `StatusStopped` (deliberate,
  successful cancellation) and `StatusError` (a real backend failure) — an
  interrupted run's fate is genuinely unknown, and collapsing it into
  either existing status would misrepresent what happened.
- `Event.Raw` is not persisted: already documented on `taskrunner.Event` as
  backend-native/debug-only, and holds provider-specific values (an
  `acp.SessionUpdate`, a `claudecode.Message`) that aren't meaningfully
  JSON-roundtrippable across a restart.
- Reconciliation runs once, synchronously, inside `runs.New` — not a
  background job or separate CLI command.
- Explicitly out of scope: runs beyond the rehydration window once evicted
  from memory (same "ample for recent runs" philosophy the existing
  `finishedRetentionCap` doc comment already states); the
  `workspace.Manager.RunTask`/`tasks.status` dead-code finding (separate,
  undecided); terminal/PTY session persistence (already out of scope per
  `web-ui-terminal.md`).

## Progress

- [x] Schema: `runs` + `run_events` tables
- [x] `internal/store`: CRUD methods + tests
- [x] `internal/runs`: `Registry` takes `*store.Store`; persists on
      Start/record/finish; rehydrates + reconciles in `New`
- [x] `internal/runs`: tests (persistence, restart simulation, interrupted
      reconciliation, race)
- [x] Thread `*store.Store` through `wsapi.New` / `server.New` /
      `cmd/smind/serve.go` (and every test call site: `internal/wsapi`,
      `internal/wsclient`, `internal/server`)
- [x] Verification (unit/race tests + live-daemon restart E2E)

## Validation

- **Schema + `internal/store` CRUD** (`internal/store/runs_test.go`):
  `TestStore_Runs`, `TestStore_GetRunMissing`, `TestStore_ListRecentRuns`,
  `TestStore_MarkRunningRunsInterrupted`, `TestStore_RunEvents`,
  `TestStore_RunsSurviveReopen` (Close + reopen the same db file, confirm
  run + events readable) -- all pass.
- **Write path (Start/record/finish persist)**
  (`internal/runs/runs_test.go`): `TestRegistry_Start_PersistsRunRowImmediately`
  (row queryable via the store before the turn does anything),
  `TestRegistry_Record_PersistsEventsInOrder` (persisted `run_events` match
  in-memory `History` exactly, same order), `TestRegistry_Finish_PersistsTerminalStatus`
  (`done` and `stopped` subtests) -- all pass.
- **Rehydrate-on-restart** (`TestRegistry_RestartSimulation_HistorySurvivesAcrossRegistries`):
  drives a run to completion on one `Registry`, discards it, builds a new
  `Registry` against the same store, confirms `List`/`History`/`Subscribe`
  all behave identically to the original -- pass.
- **Interrupted reconciliation** (`TestRegistry_InterruptedReconciliation_MarksStaleRunningRunsInterrupted`):
  a run left "running" in the store (no Stop/CloseAll, simulating a crash)
  comes back `StatusInterrupted` from a new `Registry` -- pass.
- **Full suite**: `go test -race -count=3 ./...` clean across every
  package. `gofmt -l`, `go vet ./...`, `task build` clean; `git status`
  after `task build` showed exactly the expected changed/new files.
- **Live-daemon E2E, graceful restart**: real `bin/smind serve`, a real
  `run.start` turn driven to completion over a real WebSocket connection,
  graceful SIGTERM shutdown, fresh daemon restart, `run.logs` for that ID
  still returns the full 3-event history (`Hello, ` / `world!` / done,
  `end_turn`) -- confirmed via a Node driver script.
- **Live-daemon E2E, crash + reconciliation**: same shape, but a `"hang"`
  scenario run left genuinely in-flight, daemon killed with SIGKILL (no
  graceful `CloseAll`). Confirmed via direct sqlite query that the row was
  left `status = "running"` immediately after the kill; after restarting
  the daemon, both a direct sqlite query and `run.logs` over the wire
  report `status = "interrupted"`, with the one event recorded before the
  hang still present.
