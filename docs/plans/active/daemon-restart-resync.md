# Daemon restart resilience: terminal persistence + client auto-reconnect/resync

## Context

Two gaps found by inspection (2026-09-03), both explicitly deferred by
earlier plans:

- `docs/plans/completed/web-ui-terminal.md`'s Decisions: "Explicitly out of
  scope: ... terminal session persistence across a daemon restart."
- `docs/plans/completed/run-history-persistence.md`'s Decisions: "terminal/
  PTY session persistence (already out of scope per `web-ui-terminal.md`)."

`internal/runs.Registry` already solves this exact problem for agent runs
(reconcile stale `"running"` rows to `"interrupted"`, rehydrate recent
history from `internal/store` at startup — see `internal/runs/registry.go`'s
`New`). `internal/terminal.Registry` has no such thing: zero persistence,
zero restart reconciliation. Separately, neither WS client implementation
(`internal/wsclient.Client`, `web/packages/ui/src/lib/ws-client.ts`) nor
`App.tsx` has any reconnect logic at all — `App.tsx` calls `connectDaemon()`
once on mount; a runtime socket close/error triggers `failAll` internally
but never updates React state, so the UI silently goes stale until a manual
page reload.

This plan closes both gaps: (1) terminal sessions survive a daemon restart
the same honest way runs already do (interrupted, not silently resumed,
recent scrollback rehydrated), and (2) any long-lived client (today's web
UI; the same code will back the Phase 4 Tauri desktop wrapper) automatically
reconnects and resyncs its view of server state after losing the
connection, without a manual reload.

**Implemented in two parallel tracks** (separate worktrees/branches, merged
back together): daemon-side terminal persistence (`internal/store`,
`internal/terminal`, `internal/wsapi`) and client-side reconnect/resync
(`web/packages/ui`). The two tracks share no files and needed zero
coordination beyond the wire contract already specified below, which held
exactly as specified on both sides.

## Acceptance Criteria

### Daemon-side: terminal session persistence (mirrors `internal/runs`)

- New `terminal_sessions` table in `internal/store/schema.sql`: `id` (TEXT
  PK, matches `terminal.Registry`'s existing id shape), `task_id`
  (REFERENCES tasks), `status`, `started_at`, `closed_at`, `scrollback`
  (TEXT — the session's scrollback buffer at last checkpoint, same
  JSON-in-TEXT-column convention as `run_events.event_data`, though this is
  the raw buffer, not JSON), `created_at`, `updated_at`. Index on
  `task_id`, matching `idx_runs_task_id`.
- `internal/store` gains CRUD in the same style as `runs.go`
  (`CreateTerminalSession`, `UpdateTerminalSessionScrollback`,
  `UpdateTerminalSessionStatus`, `ListRecentTerminalSessions`,
  `MarkRunningTerminalSessionsInterrupted`) — same error-wrapping, same
  `scanX` pattern, same UTC timestamps.
- **Write path** — deliberately *not* write-through on every PTY byte
  (unlike `run_events`, which persists each discrete, low-frequency
  `taskrunner.Event`; raw terminal output is a high-frequency byte firehose
  and persisting every chunk would make interactive typing/output pay a
  disk-write tax per keystroke/chunk):
  - `Registry`'s session-create path persists the `terminal_sessions` row
    immediately (`status = "running"`, empty scrollback) — a session
    recorded as started is queryable even if the daemon dies right after,
    matching `runs.Registry.Start`'s immediacy guarantee.
  - A bounded-cadence checkpoint (pick a concrete interval — e.g. hooked
    into the same amortized-compaction pass that already trims
    `scrollbackCap`/`scrollbackHardCap`, or a simple ticker; document
    whichever is chosen and why) persists the current scrollback buffer,
    write-only-if-changed. This bounds how much output a crash (SIGKILL,
    no graceful shutdown) can lose to "since the last checkpoint," not "the
    whole session" — document the concrete bound chosen.
  - `Registry.Close` (graceful, explicit `terminal.close`) persists final
    `status = "closed"`, `closed_at`, and the final scrollback synchronously
    before returning, same as `runs.Registry.finish`.
- **Startup reconciliation + rehydration**, inside a new
  `terminal.New(st *store.Store)` (mirrors `runs.Registry.New` exactly):
  - Any `terminal_sessions` row still `status = "running"` is transitioned
    to `"interrupted"` — the PTY subprocess is a real child of the old
    daemon process and cannot have survived a restart (same reasoning as
    `runs.Registry.New`'s doc comment on why a "running" row can't be
    trusted). This must be honest, not silently reported as `"running"`.
  - Recent interrupted/closed sessions (bounded the same way
    `finishedRetentionCap` bounds `runs.Registry` — reuse or mirror that
    constant/reasoning) are rehydrated into the in-memory registry as
    already-terminal sessions with their last-checkpointed scrollback, so
    `terminal.list`/`terminal.attach` keep working unchanged: attaching to a
    rehydrated session delivers its scrollback as backfill then closes
    immediately (no live tail — there is nothing live to tail), exactly like
    attaching to an already-finished run.
  - `terminal.write`/`terminal.resize` against a rehydrated (interrupted or
    closed) session return a clear error ("session no longer running"), not
    a silent no-op or a hang.
- No changes to the `terminal.*` wire method shapes
  (`create`/`attach`/`write`/`resize`/`close`/`list`) — this is purely a
  persistence layer underneath the existing contract, same as how
  run-history-persistence needed zero wire changes.

### Client-side: reconnect + resync

- **Scope decision, stated up front**: reconnect logic targets the
  browser `WsClient` (`web/packages/ui/src/lib/ws-client.ts`) and
  `App.tsx`, since that is what stays open across a daemon restart today
  (the web UI) and will keep backing the Phase 4 Tauri desktop wrapper
  (same web code, per the roadmap's "UI/mobile/desktop are just clients"
  principle) and, later, the mobile app via the Phase 3 relay. The Go
  `internal/wsclient.Client` is used only by one-shot `smind` CLI
  subcommands (`task new/ls/attach/send/logs/stop`) that already fail
  cleanly and exit if the daemon drops mid-command — there is no long-lived
  CLI process to keep reconnecting for, so it is **explicitly out of
  scope** here (revisit only if a long-lived CLI use case actually shows
  up, per this project's "no feature without real demand" principle).
- `WsClient` (or a thin wrapper around it — implementer's call, document
  which) gains automatic reconnect on an *unexpected* close (the socket's
  own `close`/`error` firing `failAll`) as distinct from an *explicit*
  `.close()` call (e.g. React unmount, task switch) — explicit close must
  never trigger a reconnect attempt. Reconnect re-fetches `/api/token`
  each attempt (the daemon's token is stable across a graceful restart per
  `internal/auth/token.go`'s doc comment, but refetching is what also
  survives the rarer case of the token file changing) and redials with a
  bounded backoff policy (concrete numbers — e.g. starting delay, cap,
  jitter — are the implementer's call; document them and the reasoning).
  Retries indefinitely until success or the app itself tears down (no
  "give up after N tries" — a daemon restart during a deploy/update is
  expected to be seconds, not permanent, and there is no useful fallback
  UI state beyond "still trying").
- `App.tsx` exposes real connection status
  (`"connecting" | "connected" | "reconnecting" | "disconnected"` or
  equivalent) driven by actual socket events, not just the initial
  `connectDaemon()` promise — the header must never show a stale
  "Connected to daemon" once the underlying socket has actually died.
- **On successful reconnect, `App.tsx` sets a genuinely new `WsClient`
  instance into state** (`setClient(newClient)`), not the same object
  mutated in place. This is the resync mechanism, not incidental: every
  existing data-fetching hook already keys its `useEffect` on the `client`
  reference (`useWorkspaceTree`'s `[client]` in `app-sidebar.tsx`,
  `useRunTimeline`'s `[client, taskId]` in `use-run-timeline.ts`, and
  `TerminalPane`'s/`FileExplorerPane`'s/`DiffViewerPane`'s equivalents) —
  a new reference re-runs those effects automatically, re-issuing
  `workspace.list`/`space.list`/`task.list`/`run.list`/`terminal.list`/etc.
  fresh against the new connection. **No bespoke per-component
  invalidation/refetch code should be needed** — if a component turns out
  to need one, that's a sign its effect isn't correctly keyed on `client`
  and should be fixed to match the existing pattern, not special-cased.
- Any component with a live streaming subscription open at disconnect time
  (`TaskDetailPane`'s active `run.attach`, `TerminalPane`'s active
  `terminal.attach`) must visibly reflect the break (not silently freeze
  showing stale "live" output) and, once the new post-reconnect client
  arrives, resume by re-discovering the still-open run/session (`run.list`/
  `terminal.list`) and re-attaching to the same id — not by starting a
  duplicate run/session.
- A terminal session that the daemon reports as `"interrupted"` after a
  restart (see daemon-side section above) must render distinctly in
  `TerminalPane` (e.g. "session ended: daemon restarted" rather than a
  bare/generic error), since this is a real, expected, honest outcome, not
  a bug being surfaced.

## Test Scenarios

### `internal/store`

- Insert a terminal session row, update its scrollback and status, read it
  back; list recent sessions in the right order; a session surviving
  `Store.Close` + `store.Open` on the same file path (mirrors
  `TestStore_RunsSurviveReopen`).
- `MarkRunningTerminalSessionsInterrupted` transitions only `"running"`
  rows, leaves `"closed"`/`"interrupted"` rows untouched.

### `internal/terminal`

- **Checkpoint write path**: drive real PTY output through a session,
  confirm the persisted scrollback matches the in-memory buffer after a
  checkpoint fires (real timing/trigger, not mocked).
- **Graceful close persists final state**: `terminal.close` on a real
  session leaves `status = "closed"`, `closed_at` set, final scrollback
  matching what the in-memory buffer held at close time.
- **Restart simulation** (mirrors
  `TestRegistry_RestartSimulation_HistorySurvivesAcrossRegistries`): build
  a `Registry` against a real temp-file store, run a session to a graceful
  close, discard the `Registry`, construct a new one against the same
  store file, confirm `List`/`Attach` on that session id return the same
  final scrollback as backfill, then close (no live tail).
  - `terminal.write`/`terminal.resize` against this rehydrated session
    return a clear error, not a hang or silent success.
- **Interrupted reconciliation** (mirrors
  `TestRegistry_InterruptedReconciliation_MarksStaleRunningRunsInterrupted`):
  leave a session row `status = "running"` in the store (start a session,
  discard the `Registry` without calling `Close`/`CloseAll` — simulating a
  crash), construct a new `Registry` against that store, confirm the
  rehydrated session comes back `"interrupted"`, with whatever scrollback
  was checkpointed before the simulated crash (and *not* the full
  scrollback, if the test deliberately writes past a checkpoint boundary
  before discarding — this proves the "loses only since-last-checkpoint"
  bound concretely, not just in prose).
- Run the full package suite with `-race -count=3`: checkpoint writes on
  the hot output path must not introduce a new race against the existing
  `subscribe_race_test.go`/`registry_linux_test.go` coverage.

### `internal/wsapi`

- Existing `terminal_test.go` suite passes unchanged with a real store
  threaded through; add a case confirming `terminal.list`/`terminal.attach`
  work identically for a rehydrated (post-restart) session as for one
  closed in the current process, and that `terminal.write`/`terminal.resize`
  against it return the documented error.

### `web/packages/ui` — `WsClient` reconnect

- Unexpected close (fake socket fires `close`/`error` without `WsClient`'s
  own `.close()` having been called) triggers a reconnect attempt; explicit
  `.close()` never does.
- A failed reconnect attempt (fake dial rejects) retries again (backoff);
  a fake clock/timer control is needed here (check what this repo's vitest
  setup already provides, if anything, before adding a new dependency) —
  don't rely on real wall-clock delays in the test.
- A successful reconnect resolves to a usable client (a `call()` issued
  after reconnect succeeds against the new fake socket).
- `/api/token` is refetched on each reconnect attempt (not reused from the
  original connect).

### `web/packages/ui` — `App.tsx` resync

- Initial connect failure still shows the existing disconnected/error
  state (regression check — don't break the current behavior).
- Simulate an unexpected disconnect after initial connect: header status
  updates away from "Connected to daemon" (no stale display).
- After a simulated successful reconnect, `AppSidebar` receives a
  *different* `client` prop reference than before disconnect (proves the
  "new instance, not mutated" contract from Acceptance Criteria) — this is
  the concrete, checkable version of "resync happens for free."
- A task selected before disconnect remains selected after reconnect (no
  UI state is thrown away that doesn't need to be — only the *data*, not
  the *selection*, gets refetched).

### `TaskDetailPane` / `TerminalPane` resync

- A component with an active `run.attach`/`terminal.attach` subscription,
  given a disconnect then a new post-reconnect client, re-issues
  `run.list`/`terminal.list` and re-attaches to the same still-open id —
  not a fresh `run.start`/`terminal.create` (assert on the fake client's
  call log, same pattern `task-detail.test.tsx`/`terminal-pane.test.tsx`
  already use for asserting exact method calls).
- `TerminalPane` given a session that comes back `status: "interrupted"`
  from `terminal.list`/`terminal.attach` post-reconnect renders the
  distinct "session ended: daemon restarted" state, not a generic error.

### Manual/E2E (real built binary, real daemon)

- Real terminal session with real output, graceful daemon `SIGTERM`
  restart, confirm the session shows `"closed"` (not `"running"`) via
  `terminal.list` after restart, with scrollback matching what was written
  before shutdown (graceful close persists final state per Acceptance
  Criteria).
- Real terminal session, `SIGKILL` the daemon (no graceful shutdown, same
  shape as `run-history-persistence.md`'s crash test), restart, confirm
  the session shows `"interrupted"` with scrollback matching the last
  checkpoint (not the full pre-crash output — document the actual gap
  observed).
- Real browser-less WS driver script (this repo's established
  no-real-browser E2E pattern): open a connection, kill and restart the
  daemon underneath it, confirm the script's `WsClient`-equivalent
  reconnects and a fresh `workspace.list`/`task.list` call succeeds
  without the driver script redialing manually.
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race -count=3
  ./...` clean. `bunx tsc -b` clean, `bun run test` passes. `task build`
  succeeds; check `internal/server/dist/.gitkeep`.

## Decisions

- Go `internal/wsclient.Client` reconnect is explicitly out of scope (see
  Acceptance Criteria) — no long-lived CLI use case exists today.
- Terminal output is not write-through persisted per-chunk (unlike
  `run_events`) — bounded-cadence checkpointing instead, trading "a crash
  loses only the tail since last checkpoint" for avoiding a disk write per
  PTY chunk.
- Reconnect resync relies on existing hooks' `useEffect` being keyed on
  `client` reference identity, deliberately reusing that existing pattern
  rather than introducing a new event-bus/invalidation mechanism.

### Daemon-side implementation decisions

- **Checkpoint cadence: a fixed 2-second ticker per session** (`internal/
  terminal.checkpointCadence`), started alongside the existing read loop in
  `Registry.Create`, not hooked into the existing byte-count-driven
  `scrollbackCap`/`scrollbackHardCap` compaction pass. A byte-count trigger
  bounds loss in *bytes since the last compaction*, which for a mostly-idle
  interactive session (a human typing, not a log firehose) could mean
  "since whenever the buffer last happened to cross 256KiB" — effectively
  unbounded in wall-clock terms for typical usage. A fixed wall-clock
  ticker gives a bound that's meaningful for the actual failure mode this
  guards against (a SIGKILL mid-session): at most ~2s of output/typing
  lost, regardless of throughput. 2s specifically: frequent enough that
  the loss is "a couple of lines", while three orders of magnitude cheaper
  than a persist-per-chunk on a session that can emit hundreds of chunks/
  second. The checkpoint write is skip-if-unchanged (tracked via a
  per-session `historyVersion` counter bumped on every append, compared
  without diffing the buffer itself), so an idle session between ticks
  costs nothing beyond one version comparison.
- **Rehydration retention cap: reuses the existing `closedRetentionCap`
  (200)** rather than introducing a second constant, the same way
  `internal/runs` reuses a single cap for both "how many finished runs may
  I hold in memory" and "how many may I load back in after a restart" —
  they're the same in-memory cost, just incurred at a different time, so
  one number serving both is deliberate, not an oversight.
- **New `StatusInterrupted`**, distinct from `StatusClosed`: a "running"
  row surviving to a fresh process start means the PTY subprocess (a real
  child of the old daemon process) is definitely gone, but nothing
  observed *how* it ended — reporting it as `"closed"` would claim more
  than is actually known, the same reasoning `internal/runs.StatusInterrupted`
  already established for runs.
- **`Write`/`Resize` against a non-running session** (closed or
  interrupted, current-process or rehydrated) return
  `"session no longer running"` rather than a silent no-op — `Resize`
  previously had no such guard at all (a rehydrated session has no live
  `ptmx` to `Setsize` against, which would otherwise nil-panic); `Write`
  already had the guard, just a less specific message, now made consistent
  with `Resize`'s.
- **`Registry.finish` persists final status/`closed_at`/scrollback
  synchronously before the in-memory status transition**, mirroring
  `runs.Registry.finish` exactly (including its CI-reproducible race
  rationale): this is what gives `Close`'s "final scrollback persisted
  before returning" contract for free, since `Close` already blocks on
  `closedCh`, which only closes after `finish` has run. `finish` runs for
  *both* an explicit `terminal.close` and the shell exiting on its own
  (e.g. the user typing `exit`) — both are the same "graceful" path from
  persistence's point of view; only a crash (no `finish` call at all)
  leaves a "running" row behind for reconciliation to find.
- **`terminal_sessions.task_id` keeps the `REFERENCES tasks(id)` foreign
  key** (matching the Acceptance Criteria's explicit ask), which meant
  `internal/terminal`'s existing tests — which predate this table and
  hardcode `taskID` literal `1` throughout — needed a real `tasks` row to
  satisfy it. Rather than editing every hardcoded call site, `newTestRegistry`
  now creates exactly one workspace+task (directly via `store.Store`, not
  `workspace.Manager`, since these tests already pass their own worktree
  path straight to `Create` and have no need for a real git worktree) —
  the first task in a fresh store is always id 1
  (`INTEGER PRIMARY KEY AUTOINCREMENT`), so every existing hardcoded `1`
  stays valid unchanged.

### Client-side implementation decisions

- **Reconnect lives in a new wrapper (`web/packages/ui/src/lib/reconnect.ts`'s
  `watchForReconnect`), not inside `WsClient` itself.** `WsClient` gained
  exactly one small addition: a public `onClose(callback)` hook (fires once,
  synchronously if already closed) so an external watcher can learn "this
  connection just died" without knowing *why* — that's the only seam the
  wrapper needs. The reason it can't live inside `WsClient`: the resync
  mechanism this whole plan is built on requires App.tsx to receive a
  *genuinely new* `WsClient` instance on every reconnect (so hooks keyed on
  the `client` reference re-run) — if `WsClient` quietly re-plumbed itself to
  a new socket in place, the reference would never change and nothing would
  resync. `watchForReconnect` also deliberately does **not** perform the
  initial connect — App.tsx keeps doing that itself, exactly as before, so
  an initial-connect failure's behavior (show `connectError`, don't retry)
  is completely unchanged; the wrapper only takes over once a first client
  already exists, and re-arms itself on every successful reconnect so the
  loop runs indefinitely.
- **Backoff policy**: exponential with full jitter — `initialDelayMs: 500`,
  `factor: 2`, `maxDelayMs: 10_000`, delay drawn uniformly from
  `[0, min(maxDelayMs, initialDelayMs * factor^attempt)]`. Reasoning: a
  daemon restart during a graceful deploy/update is expected to take low
  single-digit seconds, so the first one or two retries should land almost
  immediately; the 10s cap keeps a genuinely longer outage from hammering
  the daemon once it comes back. Full jitter (not fixed exponential) avoids
  every open tab/window retrying in lockstep. There is no retry-count limit
  — per the Acceptance Criteria, reconnect retries indefinitely until
  success or the app itself tears down (`ReconnectHandle.close()`).
- **Token refetch**: `watchForReconnect`'s `connect` option defaults to
  `daemon.ts`'s `connectDaemon`, which does a fresh, uncached `fetch("/api/token")`
  every call by construction — so "refetch the token on every reconnect
  attempt" falls out of always calling the same full connect flow again,
  with no special-casing needed.
- **Real connection status** (`"connecting" | "connected" | "reconnecting" | "disconnected"`,
  `web/packages/ui/src/lib/reconnect.ts`'s `ConnectionStatus`) is threaded
  from `App.tsx` down to `TaskDetailPane` and `TerminalPane` as an optional
  `connectionStatus` prop (default `"connected"`, so every pre-existing
  caller/test keeps behaving exactly as before). When it's `"reconnecting"`,
  each pane shows a small additive banner ("Connection lost — reconnecting
  to daemon…") without discarding any already-rendered state (runs, chunks,
  terminal output) — this is what satisfies "must visibly reflect the
  break" without needing to distinguish transport-level failures from
  ordinary RPC errors inside each pane's own call sites (which isn't
  reliably possible today: `WsClient.failAll` reports a dead connection to
  in-flight callers as a plain `RpcError`, indistinguishable by type from a
  genuine server-side error — see that method's implementation).
- **`TaskDetailPane`/`use-run-timeline.ts` needed no logic changes** for
  re-attach-on-reconnect: its effect already keys on `[client, taskId]`,
  calls `run.list` fresh every time that effect runs, and only ever starts
  a *new* run from an explicit user action (`submitPrompt`) — never
  automatically inside the effect. So a reconnect's client-reference change
  naturally re-fetches `run.list` and re-attaches (via `streamRun`) to any
  run still `"running"`, with no risk of spawning a duplicate, for free.
  This is the concrete case the Acceptance Criteria's "if a component needs
  bespoke refetch code, fix its effect keying instead" guidance was written
  for — and it already fit the pattern.
- **`TerminalPane` needed a real code change**, because unlike
  `use-run-timeline.ts` it always called `terminal.create` unconditionally
  inside the same effect that also attaches — a client-reference change
  alone would have called `terminal.create` again, spawning a duplicate
  shell next to the one still running server-side. Fixed by calling
  `terminal.list({ taskId })` first: if a still-`"running"` session for the
  task already exists, attach to it; only call `terminal.create` when none
  does. A `lastTerminalIdRef` (persisted across a client-only effect re-run,
  reset only when `task.ID` itself actually changes) additionally lets the
  effect recognize "this is the same session I was already attached to" on
  reconnect and check *its* specific post-reconnect status — if the daemon
  now reports it `"interrupted"` (the daemon-side persistence work's
  addition — see that section above), it renders the distinct "session
  ended: daemon restarted" state instead of either attaching (which would
  just backfill scrollback then immediately end, looking like a bare error)
  or silently starting a replacement shell.
- `TerminalStatusValue` (`web/packages/ui/src/lib/types.ts`) gained the
  `"interrupted"` value ahead of the daemon-side work landing — purely
  additive to the wire contract (no method shape changed). Verified after
  merging both tracks: the daemon-side `internal/terminal.StatusInterrupted`
  constant is literally `"interrupted"` too, so the two independently-built
  halves agree on the wire value with zero coordination needed.
- `FileExplorerPane`/`DiffViewerPane` needed no changes: neither has a
  create-on-mount step like `TerminalPane`'s old `terminal.create`, and
  both already key their fetch effects on `[client, task.ID]`.

## Progress

- [x] Schema: `terminal_sessions` table
- [x] `internal/store`: CRUD methods + tests
- [x] `internal/terminal`: checkpointed persistence on the write path;
      `New(st *store.Store)` reconciliation + rehydration; write/resize
      error on rehydrated sessions
- [x] `internal/terminal`: tests (checkpoint, graceful close, restart
      simulation, interrupted reconciliation, race)
- [x] `internal/wsapi`: rehydrated-session test coverage
- [x] `web/packages/ui`: `WsClient` reconnect (backoff, token refetch,
      explicit-close vs. unexpected-close distinction) + tests
- [x] `web/packages/ui`: `App.tsx` real connection-status state + swaps in
      a new `WsClient` instance on reconnect + tests
- [x] `web/packages/ui`: `TaskDetailPane`/`TerminalPane` re-attach-on-
      reconnect + `TerminalPane` "interrupted" rendering + tests
- [x] Verification, daemon-side (unit/race tests + live-daemon restart
      E2E, graceful and crash)
- [x] Verification, client-side (typecheck/tests + no-real-browser WS
      driver reconnect E2E)
- [ ] Verification, merged branch (full repo `verify` — build/test/lint
      across both tracks together, plus a combined live-daemon E2E)

## Validation

Both tracks were implemented in parallel worktrees (branches
`terminal-restart-persistence` and `web-client-reconnect`, off `develop`)
and merged into `daemon-restart-resync`. Each track's own validation below
is as reported by its implementing agent; the merge itself only touched
this plan document (a content conflict from both branches editing the same
sections independently — resolved by combining, no code conflicts at all,
consistent with the two tracks sharing no source files). Combined
post-merge verification is recorded in the last subsection.

### Daemon-side (`internal/store`, `internal/terminal`, `internal/wsapi`)

- **Schema + `internal/store` CRUD** (`internal/store/terminal_sessions_test.go`):
  `TestStore_TerminalSessions`, `TestStore_GetTerminalSessionMissing`,
  `TestStore_ListRecentTerminalSessions`,
  `TestStore_MarkRunningTerminalSessionsInterrupted`,
  `TestStore_TerminalSessionsSurviveReopen` (Close + reopen the same db
  file, confirm the session + its checkpointed scrollback are readable) --
  all pass.
- **Write path** (`internal/terminal/persistence_test.go`):
  `TestRegistry_Checkpoint_PersistsScrollback` (real PTY output through a
  session; waits for a real `checkpointCadence` tick to land, confirms the
  persisted scrollback matches the in-memory buffer exactly),
  `TestRegistry_GracefulClose_PersistsFinalState` (`terminal.close` on a
  real session leaves `status = "closed"`, `closed_at` set, final
  scrollback matching what was written) -- both pass.
- **Rehydrate-on-restart** (`TestRegistry_RestartSimulation_ScrollbackSurvivesAcrossRegistries`):
  drives a session to a graceful close on one `Registry` against a real
  temp-file store, discards it, builds a new `Registry` against the same
  store, confirms `List`/`Subscribe` return the identical scrollback as
  backfill with no live tail, and that `Write`/`Resize` against it both
  return the documented "session no longer running" error -- pass.
- **Interrupted reconciliation, concrete crash-loss bound**
  (`TestRegistry_InterruptedReconciliation_LosesOnlySinceLastCheckpoint`):
  a session left "running" in the store (`Registry` discarded without
  `Close`/`CloseAll`, simulating a crash) comes back `StatusInterrupted`
  from a new `Registry`, with scrollback containing output written before
  the last observed checkpoint tick but *not* output written after it and
  before the simulated crash -- proving the "loses only since the last
  checkpoint" bound concretely rather than just in prose -- pass.
- **Full suite**: `go build ./...`, `go vet ./...`, `gofmt -l` clean;
  `go test -race -count=3 ./...` clean across every package (including
  `internal/terminal`'s existing `subscribe_race_test.go`/
  `registry_linux_test.go` coverage, unaffected by the new checkpoint
  goroutine).
- **`internal/wsapi`**: existing `terminal_test.go` suite passes unchanged
  (now threading a real store through, as it already did for `runs`); new
  `TestServer_TerminalAttach_RehydratedSessionAfterRestart` builds a second
  server (and so a second `terminal.Registry`, via its own `New(db)` call)
  against the same `db` a first server used, after closing a session on
  the first -- confirms `terminal.list`/`terminal.attach` on the second
  server work identically for the rehydrated session, and
  `terminal.write`/`terminal.resize` against it both error -- pass.
- **Live-daemon E2E, graceful restart**: real `smind serve` (built binary),
  a real terminal session with real shell output over a real WebSocket
  connection, graceful `SIGTERM` shutdown, fresh daemon restart. Confirmed
  via direct sqlite query that the row flipped `running` -> `closed`
  immediately on shutdown (not just eventually); after restart,
  `terminal.list` reports `status = "closed"` and `terminal.attach`
  delivers the full pre-shutdown scrollback (including a written marker)
  as backfill, then a clean terminal result with no live tail.
- **Live-daemon E2E, crash + reconciliation**: same shape, but the daemon
  `SIGKILL`ed with no graceful shutdown, timed against a real checkpoint
  tick observed by polling the sqlite file directly (rather than a fixed
  sleep, which risks landing close to the next tick's boundary): wrote a
  first marker, waited for it to actually appear in the persisted
  scrollback (proving a real checkpoint landed), wrote a second marker,
  then killed immediately. Confirmed via direct sqlite query, immediately
  after the kill, that the row was left `status = "running"` with the
  first marker persisted but not the second; after restarting the daemon,
  `terminal.list` reports `status = "interrupted"` and `terminal.attach`'s
  backfill contains the first marker but not the second -- the concrete
  gap this design accepts, observed directly rather than assumed.

### Client-side (`web/packages/ui`)

**Unit tests**:
- `lib/ws-client.test.ts` (unchanged, still 8/8 passing) plus the new
  `onClose` hook's behavior exercised indirectly through `reconnect.test.ts`.
- `lib/reconnect.test.ts` (new, 6 tests): unexpected close triggers a
  reconnect attempt; explicit `close()` never does; a failed attempt
  retries again with backoff (verified via vitest's built-in fake timers --
  no new dependency needed); a successful reconnect resolves to a client
  usable for a real `call()` against its new fake socket; the reconnect
  loop re-arms itself on the new client so a second unexpected close also
  reconnects; the default `connect` (real `connectDaemon`) is invoked
  fresh on every attempt, proving the token is refetched each time rather
  than reused.
- `App.test.tsx` (new, 4 tests, driving the real `WsClient` class against
  fake sockets rather than `FakeWsClient`, since this needed genuine
  new-instance-per-reconnect semantics): an initial connect failure still
  shows the pre-existing `Disconnected: <message>` state and never retries
  (regression check); an unexpected disconnect moves the header off
  "Connected to daemon" to "Reconnecting to daemon…", and a successful
  reconnect brings "Connected to daemon" back; after reconnect, a *fresh*
  `workspace.list` fires against the new socket (the only way that could
  happen is if `AppSidebar` actually received a new `client` reference,
  since `useWorkspaceTree`'s effect is keyed on it -- verified behaviorally
  rather than by reaching into React internals); a task selected before
  disconnect is still selected and rendered after reconnect, with its pane
  re-fetching fresh (`run.list`) against the new client rather than being
  unmounted or losing the selection.
- `components/task-detail.test.tsx` (2 new tests added to the existing 13):
  a "Connection lost — reconnecting…" banner appears/disappears with
  `connectionStatus`, additively (an already-rendered run's streamed text
  stays on screen, nothing is discarded); given a new post-reconnect
  client, `useRunTimeline` (no code changes needed) issues a fresh
  `run.list` and re-attaches to the same still-`"running"` run id, never a
  fresh `run.start`.
- `components/terminal-pane.test.tsx` (rewritten for the new
  list-before-create flow, 14 tests total, 3 new): given a new
  post-reconnect client, re-issues `terminal.list` and re-attaches to the
  same still-`"running"` session id instead of a fresh `terminal.create`;
  a session that comes back `status: "interrupted"` post-reconnect renders
  the distinct "session ended: daemon restarted" state (verified: no
  `terminal.attach`/`terminal.create` call happens, and the generic
  `terminal-error` testid is absent); the same additive connection-lost
  banner as `TaskDetailPane`, without disposing the terminal widget.
- Total: `bun run test` (`vitest run`) -- **8 test files, 62/62 passing**.
- `bunx tsc -b` -- clean.
- `bun run build` (`tsc -b && vite build`) -- succeeded;
  `internal/server/dist/.gitkeep` was deleted by the build (the same known
  Vite `--emptyOutDir` behavior every prior web UI task has hit) and
  restored via `git checkout`.

**Manual/E2E, client-side half** (real built `bin/smind`, temp `SMIND_HOME`
+ a real git repo workspace created via the CLI, a from-scratch Bun script
using only built-in `fetch`/`WebSocket`, kept as a throwaway script per
prior plans' precedent, not committed): a hand-rolled client mirroring
`WsClient` plus a `watchForReconnect`-equivalent (unexpected-close
detection, backoff redial, fresh `/api/token` fetch per attempt) connected
to the real daemon and confirmed an initial `workspace.list` succeeded; the
daemon process was then sent `SIGTERM` while a supervisor shell loop
watched for its exit and restarted the same binary against the same
`SMIND_HOME` (same token file, so the daemon-restart-preserves-the-token
assumption in the Acceptance Criteria held); the driver observed the
socket's unexpected close, redialed (the first attempt landed before the
new process had bound its port yet and failed as expected, the next
attempt succeeded), and a fresh `workspace.list`/`task.list` against the
new connection succeeded -- all without the driver script redialing
manually.

**What jsdom could and couldn't exercise**: all of this task's reconnect
logic itself (`reconnect.ts`, `App.tsx`'s status wiring, the
`TaskDetailPane`/`TerminalPane` list-then-attach/banner logic) is plain
JS/React state machinery with no real-browser-only dependency, so jsdom
exercises it faithfully -- nothing here needed the manual E2E step to
prove correctness of the reconnect *logic* itself; the E2E step exists to
prove the logic holds against a *real* daemon's actual socket-close timing
and token persistence. The one genuine jsdom gap touched by this task is
pre-existing and unrelated to reconnect: jsdom's
`HTMLCanvasElement.getContext()` is unimplemented, so `TerminalPane`'s
tests exercise its own wiring logic via the `TerminalHandle` fake, never a
real `@xterm/xterm` `Terminal` instance. Separately, jsdom also doesn't
implement `ResizeObserver`, which `react-resizable-panels`' `<Group>`
(used by `App.tsx`'s layout) needed for the first time once a full
`App.tsx` render tree was under test -- a plain no-op stub was added to
`test/setup.ts`, the same way this file already stubs `matchMedia`; no
test here exercises real panel-resize behavior, so the stub being a no-op
is sufficient.

### Merged branch (post-integration)

(To be filled in after running the full `verify` skill — build/test/lint
across both tracks together, confirming the merge introduced no
integration bugs beyond what each track already validated in isolation.)
