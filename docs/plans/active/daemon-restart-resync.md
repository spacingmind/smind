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

(To be filled in as implementation makes concrete choices — checkpoint
cadence/trigger, backoff policy numbers, exact rehydration retention cap,
whether reconnect lives inside `WsClient` itself or a wrapper — each with
its reasoning, following this repo's existing plan-doc convention of
recording *why*, not just *what*.)

- Go `internal/wsclient.Client` reconnect is explicitly out of scope (see
  Acceptance Criteria) — no long-lived CLI use case exists today.
- Terminal output is not write-through persisted per-chunk (unlike
  `run_events`) — bounded-cadence checkpointing instead, trading "a crash
  loses only the tail since last checkpoint" for avoiding a disk write per
  PTY chunk. Exact cadence is an implementation-time choice to document
  here.
- Reconnect resync relies on existing hooks' `useEffect` being keyed on
  `client` reference identity, deliberately reusing that existing pattern
  rather than introducing a new event-bus/invalidation mechanism.

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
  additive to the wire contract (no method shape changed), needed so the
  client's own tests can exercise the daemon's future behavior; the current
  `develop` backend never actually returns it yet.
- `FileExplorerPane`/`DiffViewerPane` needed no changes: neither has a
  create-on-mount step like `TerminalPane`'s old `terminal.create`, and
  both already key their fetch effects on `[client, task.ID]`.

## Progress

- [ ] Schema: `terminal_sessions` table
- [ ] `internal/store`: CRUD methods + tests
- [ ] `internal/terminal`: checkpointed persistence on the write path;
      `New(st *store.Store)` reconciliation + rehydration; write/resize
      error on rehydrated sessions
- [ ] `internal/terminal`: tests (checkpoint, graceful close, restart
      simulation, interrupted reconciliation, race)
- [ ] `internal/wsapi`: rehydrated-session test coverage
- [x] `web/packages/ui`: `WsClient` reconnect (backoff, token refetch,
      explicit-close vs. unexpected-close distinction) + tests
- [x] `web/packages/ui`: `App.tsx` real connection-status state + swaps in
      a new `WsClient` instance on reconnect + tests
- [x] `web/packages/ui`: `TaskDetailPane`/`TerminalPane` re-attach-on-
      reconnect + `TerminalPane` "interrupted" rendering + tests
- [ ] Verification (unit/race tests + live-daemon restart E2E, graceful
      and crash) — client-side no-real-browser WS driver scenario done
      (see Validation below); daemon-side crash/restart E2E and the Go
      test suite are the other agent's half of this plan.

## Validation

(Filled in as each Acceptance Criterion is confirmed. This section covers
only the client-side half of the plan — `web/packages/ui`'s reconnect/resync
work. The daemon-side terminal-persistence half's Validation is the other
agent's to fill in.)

**Unit tests** (`web/packages/ui`):
- `lib/ws-client.test.ts` (unchanged, still 8/8 passing) plus the new
  `onClose` hook's behavior exercised indirectly through `reconnect.test.ts`.
- `lib/reconnect.test.ts` (new, 6 tests): unexpected close triggers a
  reconnect attempt; explicit `close()` never does; a failed attempt
  retries again with backoff (verified via vitest's built-in fake timers —
  no new dependency needed, per the plan's own suggestion to check first);
  a successful reconnect resolves to a client usable for a real `call()`
  against its new fake socket; the reconnect loop re-arms itself on the new
  client so a second unexpected close also reconnects; the default
  `connect` (real `connectDaemon`) is invoked fresh on every attempt,
  proving the token is refetched each time rather than reused.
- `App.test.tsx` (new, 4 tests, driving the real `WsClient` class against
  fake sockets rather than `FakeWsClient`, since this needed genuine
  new-instance-per-reconnect semantics): an initial connect failure still
  shows the pre-existing `Disconnected: <message>` state and never retries
  (regression check); an unexpected disconnect moves the header off
  "Connected to daemon" to "Reconnecting to daemon…", and a successful
  reconnect brings "Connected to daemon" back; after reconnect, a *fresh*
  `workspace.list` fires against the new socket (the only way that could
  happen is if `AppSidebar` actually received a new `client` reference,
  since `useWorkspaceTree`'s effect is keyed on it — this is the concrete,
  checkable proxy for "AppSidebar received a different client prop
  reference" the plan's test scenario asks for, verified behaviorally
  rather than by reaching into React internals); a task selected before
  disconnect is still selected and rendered after reconnect, with its pane
  re-fetching fresh (`run.list`) against the new client rather than being
  unmounted or losing the selection.
- `components/task-detail.test.tsx` (2 new tests added to the existing 13):
  a "Connection lost — reconnecting…" banner appears/disappears with
  `connectionStatus`, additively (an already-rendered run's streamed text
  stays on screen, nothing is discarded); given a new post-reconnect
  client, `useRunTimeline` (no code changes needed — see Decisions) issues
  a fresh `run.list` and re-attaches to the same still-`"running"` run id,
  never a fresh `run.start`.
- `components/terminal-pane.test.tsx` (rewritten for the new
  list-before-create flow, 14 tests total, 3 new): given a new
  post-reconnect client, re-issues `terminal.list` and re-attaches to the
  same still-`"running"` session id instead of a fresh `terminal.create`;
  a session that comes back `status: "interrupted"` post-reconnect renders
  the distinct "session ended: daemon restarted" state (verified: no
  `terminal.attach`/`terminal.create` call happens, and the generic
  `terminal-error` testid is absent); the same additive connection-lost
  banner as `TaskDetailPane`, without disposing the terminal widget.
- Total: `bun run test` (`vitest run`) — **8 test files, 62/62 passing**.
- `bunx tsc -b` — clean.
- `bun run build` (`tsc -b && vite build`) — succeeded; `internal/server/dist/.gitkeep`
  was deleted by the build (the same known Vite `--emptyOutDir` behavior
  every prior web UI task has hit) and restored via `git checkout`.

**Manual/E2E verification, client-side half** (real built `bin/smind`,
temp `SMIND_HOME` + a real git repo workspace created via the CLI, a
from-scratch Bun script using only built-in `fetch`/`WebSocket` — this
repo's established no-real-browser E2E pattern, kept as a throwaway script
per prior plans' precedent, not committed): a hand-rolled client mirroring
`WsClient` plus a `watchForReconnect`-equivalent (unexpected-close
detection, backoff redial, fresh `/api/token` fetch per attempt) connected
to the real daemon and confirmed an initial `workspace.list` succeeded;
the daemon process was then sent `SIGTERM` while a supervisor shell loop
watched for its exit and restarted the same binary against the same
`SMIND_HOME` (same token file, so the daemon-restart-preserves-the-token
assumption in the Acceptance Criteria held); the driver observed the
socket's unexpected close, redialed (the first attempt landed before the
new process had bound its port yet and failed as expected, the next
attempt succeeded), and a fresh `workspace.list`/`task.list` against the
new connection succeeded — all without the driver script redialing
manually, confirming the same reconnect contract `reconnect.ts` implements
holds against a real daemon, not just fake sockets.

**What jsdom could and couldn't exercise** (honesty note, per this
project's standing practice — see `docs/plans/completed/web-ui-terminal.md`'s
Validation section): all of this task's reconnect logic itself
(`reconnect.ts`, `App.tsx`'s status wiring, the `TaskDetailPane`/
`TerminalPane` list-then-attach/banner logic) is plain JS/React state
machinery with no real-browser-only dependency, so jsdom exercises it
faithfully — nothing here needed the manual E2E step to prove correctness
of the reconnect *logic* itself; the E2E step exists to prove the logic
holds against a *real* daemon's actual socket-close timing and token
persistence, which no amount of faking sockets can substitute for. The one
genuine jsdom gap touched by this task is pre-existing and unrelated to
reconnect: jsdom's `HTMLCanvasElement.getContext()` is unimplemented (the
familiar "Not implemented: ... without installing the canvas npm package"
console noise in every `terminal-pane.test.tsx` run), so `TerminalPane`'s
tests exercise its own wiring logic via the `TerminalHandle` fake, never a
real `@xterm/xterm` `Terminal` instance — unchanged by this task, since
`terminal-pane.test.tsx`'s reconnect-specific new tests use the same fake.
Separately, jsdom also doesn't implement `ResizeObserver`, which
`react-resizable-panels`' `<Group>` (used by `App.tsx`'s layout) now
needed for the first time once a full `App.tsx` render tree was under
test (no prior test file rendered `<App>` at all) — a plain no-op stub was
added to `test/setup.ts`, the same way this file already stubs
`matchMedia` for the sidebar; no test here exercises real panel-resize
behavior, so the stub being a no-op is sufficient.
