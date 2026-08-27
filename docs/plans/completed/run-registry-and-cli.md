# Run registry + smind CLI

## Acceptance Criteria

- A `task.prompt` invocation is tracked server-side as a **Run** with a
  stable ID, independent of the WebSocket connection that started it.
- A second, later WebSocket connection can:
  - list active/recent runs (`run.list`)
  - attach to a still-running run and receive its remaining live events,
    having first been backfilled with everything emitted before it joined
    (`run.attach`)
  - fetch a completed or in-progress run's full event history without
    live-following (`run.logs`, with an optional tail count)
  - stop a run by ID, regardless of which connection started it
    (`run.stop`)
- Detaching (a subscriber disconnecting or explicitly unsubscribing) does
  **not** stop the run — only an explicit `run.stop` does. This matches
  the real precedent (`refs/paseo`'s `attach`: "Ctrl+C to detach", not
  stop).
- The existing `task.prompt` method's behavior for a single connection
  driving a run start-to-finish (Phase 2's PR #18 test coverage) keeps
  working — this is additive, not a breaking change to a method already
  on master.
- `smind` CLI subcommands work against a running daemon over `/ws`:
  `workspace create`, `workspace ls`, `task new`, `task ls`, `task send`
  (starts a run, foreground by default — streams and blocks until done),
  `task attach <runId>`, `task logs <runId>` (with `-f`/`--tail`),
  `task stop <runId>`.
- Explicitly out of scope (deferred, no real demand yet): persistent
  multi-turn sessions where a "send" continues an already-finished run's
  same live agent process ("send to an idle agent" from `refs/paseo`).
  Every `task.prompt`/`run.start` is still a fresh one-shot subprocess
  per `internal/taskrunner`'s existing, deliberate design. A future `send`
  to a *finished* run starts a new run against the same task, it does not
  resume the old subprocess.

## Test Scenarios

Backend (`internal/runs` + `internal/wsapi` additions):
- Starting a run, then a **second** connection attaching mid-run receives
  the backfilled history-so-far plus the remaining live events, in order,
  with no duplicates and no gaps.
- `run.logs` on a still-running run returns everything emitted so far
  without blocking (doesn't wait for completion).
- `run.logs` on a finished run returns the full history including the
  terminal event/status.
- `run.logs --tail N` (or the wire-level equivalent) returns only the
  last N events.
- `run.stop` from a connection that did **not** start the run actually
  cancels it (subprocess exits, no leak) — mirrors the existing
  `task.cancel` regression coverage from PR #18, but cross-connection.
- Detaching (subscriber goes away) does not stop the run; a later
  `run.logs`/`run.attach` on the same run still works.
- `run.list` reflects a run's current status (running vs. finished vs.
  stopped) accurately.
- Two runs on different tasks don't cross-contaminate each other's event
  streams or history.
- The daemon process itself exiting does not leave orphaned agent
  subprocesses running (they're children of the daemon process; verify
  this holds, don't just assume it).

CLI:
- `task send` in foreground mode: real stdout streaming as events arrive
  (not buffered until the end) — proven the same gated way as everywhere
  else in this codebase, not a short-timeout race.
- `task send` foreground, Ctrl+C: detaches (run keeps going server-side),
  confirmed via a subsequent `task logs <runId>` from a fresh CLI
  invocation showing the run completed.
- `task attach <runId>` on an already-finished run: reasonable behavior
  (immediately prints history and exits, or a clear message) rather than
  hanging forever waiting for events that will never come.
- `task stop <runId>` from a fresh CLI invocation (not the one that
  started it) actually stops a running task.
- Auth: CLI reads the token the same way the daemon writes it
  (`~/.spacingmind/token` via `internal/auth`/`internal/config`), no
  separate credential mechanism invented.
- `--host`/remote daemon support is explicitly **not** required for v1
  (smind is single-machine for now, unlike Paseo's `--host`) — don't
  build it speculatively.

## Decisions

- Modeled after `refs/paseo`'s actual CLI (`public-docs/cli.md`) and its
  documented run/attach/logs/stop semantics, not invented from scratch —
  see Acceptance Criteria above for the specific behaviors carried over.
- New backend method `run.start`: like `task.prompt`'s first half (calls
  `Registry.Start`) but returns `{runId}` immediately instead of implicitly
  attaching. `task.prompt` is unchanged — it still couples the run to its
  own request/connection context (stop-on-detach), which is the right
  default for a caller like a future embedded web UI where closing the tab
  should stop the run. The CLI's `task send` foreground mode needs the
  opposite (Ctrl+C detaches, doesn't stop), which is only achievable by
  decoupling "start" from "watch": `run.start` to get the runId back
  immediately (that request then terminates, so it's never in-flight when
  Ctrl+C happens), then `run.attach` to stream — `run.attach`'s own
  cancellation is already detach-only. Driving the foreground stream
  through `task.prompt` itself cannot support detach, since its own
  request context going Done is specifically what stops the run.
- Persistent multi-turn sessions ("send" to an idle finished agent)
  deferred — see Acceptance Criteria. This keeps `internal/taskrunner`'s
  existing one-shot-subprocess-per-call design untouched.
- New package `internal/runs` (registry + broadcast/backfill), consumed
  by `internal/wsapi` for the new `run.*` methods, and by a new
  `cmd/smind` CLI subcommand tree for the `smind` binary itself (it
  already exists as the daemon entrypoint; the CLI verbs become
  subcommands of the same binary, dialing the daemon's own `/ws`).

## Progress

- [x] `internal/runs`: registry, broadcast-with-backfill, cross-connection
  stop
- [x] `internal/wsapi`: `run.list`/`run.attach`/`run.logs`/`run.stop`,
  `task.prompt` starts a tracked Run instead of driving it inline
- [x] `cmd/smind` CLI subcommands (plus the new `run.start` backend method
  they need -- see Decisions)
- [x] End-to-end manual verification against the real built binary

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)

Backend (`internal/runs` + `internal/wsapi`):

- A `task.prompt` invocation is tracked server-side as a Run with a stable
  ID, independent of the WebSocket connection that started it —
  `TestRegistry_Start_RunsToCompletion`, `TestServer_ConnectionClose_StopsTaskPromptRun`
  (a run started by `task.prompt` is still reachable/stoppable after its
  starting connection closes).
- `run.list` — `TestServer_RunAttach_SecondConnectionMidRun`,
  `TestRegistry_List_ReflectsCurrentStatus`.
- `run.attach` to a still-running run: backfill then live, no dupes/gaps —
  `TestRegistry_Subscribe_MidRunAttach_BackfillThenLive`,
  `TestRegistry_Subscribe_ConcurrentBackfillLiveRace` (stress test under
  `-race`, many subscribers joining at staggered times against one
  producer), `TestServer_RunAttach_SecondConnectionMidRun` (wire-level).
- `run.attach` to an already-finished run: immediate backfill + terminal
  response, no hang — `TestRegistry_Subscribe_AlreadyFinished`,
  `TestServer_RunAttach_AlreadyFinished`.
- `run.logs` (full history, and with a tail count) without blocking or
  live-following — `TestRegistry_History_OnRunningRun_ReturnsWithoutBlocking`,
  `TestServer_RunLogs_Tail`.
- `run.stop` by ID, regardless of which connection started the run —
  `TestRegistry_Stop_FromAnyCaller_CancelsTheRun`,
  `TestServer_RunStop_CrossConnection` (subprocess actually killed,
  confirmed via the fake agent's 1h "hang" scenario ending promptly).
- Detaching does not stop the run; a later `run.logs`/`run.attach` still
  works — `TestRegistry_Unsubscribe_DoesNotStopTheRun`,
  `TestServer_RunAttach_DetachDoesNotStopRun`.
- Existing `task.prompt` single-connection start-to-finish behavior (PR
  #18) keeps working unmodified — full existing `internal/wsapi` suite
  (`TestServer_WorkspaceSpaceTaskCRUDRoundTrip`,
  `TestServer_ConcurrentInFlightRequests`,
  `TestServer_TaskPromptStreamsIncrementally`,
  `TestServer_TaskCancel_StopsRunningTurn`) passes unmodified.
- `task.cancel`/`run.stop` coexist without conflict: `task.cancel` on
  `task.prompt`'s own request id still stops the run (back-compat);
  `task.cancel` on an unrelated `run.attach` request id only detaches that
  attach, leaving the run running — `TestServer_TaskCancel_OnlyAffectsItsOwnRequest`.
- Two runs on different tasks don't cross-contaminate —
  `TestRegistry_TwoRuns_DoNotCrossContaminate`.
- The daemon process itself exiting does not leave orphaned subprocesses:
  verified at the level within this task's control (a `task.prompt`-started
  run's subprocess is killed when its starting WebSocket connection
  closes, which is what actually happens on a client-initiated disconnect
  or an abrupt server-side connection teardown) —
  `TestServer_ConnectionClose_StopsTaskPromptRun`. Graceful
  `http.Server.Shutdown` behavior for still-open, non-cooperating
  WebSocket connections is unchanged by this work and was not
  re-verified — that's `internal/server`/`cmd/smind` wiring, out of this
  task's scope.

`run.start` (new backend method the CLI's `task send` needs -- see
Decisions): returns `{runId}` immediately without implicit attach, and a
run it starts is not stopped when its own starting request/connection later
completes/closes --
`TestServer_RunStart_NotStoppedByOwnRequestCompletion` (starts a run via
`run.start`, closes the starting connection entirely, then proves from a
second connection that `run.list`/`run.attach`/`run.logs` all still see it
`running`), plus `wsclient`'s
`TestClient_CallStream_CtxCancel_DetachesPromptlyWithoutStoppingRun` and
`TestClient_CallStream_DeliversEventsBeforeTerminal` (same property
exercised through the real client).

CLI (`cmd/smind` + `internal/wsclient`):

- `task send` foreground streams incrementally in real time, not buffered
  until the end — `internal/wsclient`'s
  `TestClient_CallStream_SlowScenario_ObservesRealGapsBetweenEvents` (gates
  on >=900ms first-to-last-chunk elapsed for 5 chunks separated by real
  300ms server-side delays; buffered delivery would print instantly at the
  very end and fail this gate) and
  `TestClient_CallStream_DeliversEventsBeforeTerminal` (a chunk is observed
  before an hour-long hang would otherwise let the terminal response
  arrive). Manually re-confirmed against the real built binary: `smind task
  send <taskId> glm "..."` against a task whose worktree has a `scenario`
  file containing `slow` (fake agent via `SMIND_ACP_COMMAND`) printed "one
  two three four five" over ~1.2s wall time (4 real ~300ms gaps between the
  5 chunks), then `run <id> finished: end_turn`.
- `task send` foreground, Ctrl+C mid-stream detaches (does not stop the
  run) — `internal/wsclient`'s
  `TestClient_CallStream_CtxCancel_DetachesPromptlyWithoutStoppingRun`.
  Manually re-confirmed: started `task send <taskId> glm "..."` against a
  `hang`-scenario task in the background, waited for its first streamed
  chunk ("before hang"), sent SIGINT directly to the CLI process. It
  printed `detached -- run <id> is still running; see \`smind task logs
  <id>\`` and exited in ~3ms (measured via a poll loop around the `kill
  -INT`/process-exit). A **fresh** `smind task logs <id>` invocation
  afterward showed `run <id>: running` with the fake agent subprocess still
  present in `ps`. `smind task attach <id>` against the same still-running
  run re-attached (immediately printed the backfilled "before hang" chunk),
  and Ctrl+C on that attach also detached cleanly (~4ms) without stopping
  the run.
- `task attach <runId>` on an already-finished run prints history and
  exits cleanly rather than hanging — manually confirmed: attached to a
  finished `slow`-scenario run, got "one two three four five" +
  `run <id> finished: end_turn` and exit code 0 in ~104ms (well under a
  5s timeout wrapper), for both `task attach` and `task logs -f`.
- `task logs <runId>` (no flags), `--tail N`, and `-f` all behave
  correctly on both a still-running and an already-finished run — manually
  confirmed: plain `task logs` on a running `hang`-scenario run returned
  the backfill-so-far without blocking; `--tail N` truncated to the last N
  events (verified both with the flag before and after the positional
  `<runId>`, and with `--tail=N`); `-f` on a running run streamed live and
  then terminated on its own (exit code 0, printing the `stopped` status)
  the moment a **separate** `task stop <runId>` invocation stopped the run,
  confirming a `-f` follower is not itself what keeps a run alive and does
  not hang past the run's actual end; on the already-finished run, plain
  `task logs`, `--tail N`, and `-f` all returned immediately with full
  history (no live-follow needed since there was nothing left to follow).
- `task stop <runId>` from a fresh CLI invocation (not the one that
  started the run) actually stops it — `internal/wsapi`'s
  `TestServer_RunStop_CrossConnection`. Manually re-confirmed twice: once
  against a run left running server-side after its starting CLI process
  was killed abruptly (SIGKILL via a shell timeout, not a graceful
  Ctrl+C) -- `task stop <id>` from a brand-new invocation stopped it and
  the fake agent subprocess disappeared from `ps`; and once against a run
  a `task logs -f` was actively following, where the stop both ended the
  run and caused the follower to exit on its own.
- Auth: the CLI's `dialDaemon` (`cmd/smind/client.go`) calls
  `config.Load()`/`auth.LoadOrCreateToken(config.Dir())` -- the exact same
  functions `cmdServe` calls -- rather than inventing a separate credential
  path; confirmed by reading the code and by every manual command above
  succeeding against a daemon and CLI sharing one `SMIND_HOME`.
- `--host`/remote daemon support: not implemented, by design (confirmed
  absent from `cmd/smind/client.go` and every subcommand's flag handling).
- `workspace create`/`workspace ls`, `task new`/`task ls`: manually
  exercised end-to-end against the real binary (`smind workspace create
  <repoPath> <name> <policy>`, `smind workspace ls`, `smind task new
  <workspaceId> <title>`, `smind task ls <workspaceId>`) -- each produced
  its documented tab-separated output and reflected the daemon's actual
  store state on the following `ls`.
- Bug found and fixed during this task's manual verification: `task logs
  <runId> --tail N` / `-f` (flags *after* the positional `<runId>`, exactly
  the order the command's own usage string documents) failed with a usage
  error, because the interrupted agent's original implementation used
  `flag.FlagSet`, and Go's stdlib `flag` package stops parsing at the first
  non-flag argument -- it does not support flags after a positional
  argument. Rewritten as manual, order-independent argument parsing in
  `cmdTaskLogs` (`cmd/smind/task.go`); re-verified `--tail`/`-f` in every
  argument order (`<runId> --tail N`, `--tail N <runId>`, `<runId>
  --tail=N`) plus the previously-untested already-finished-run and
  currently-running-run cases for both flags.
- Orphan-subprocess check across the whole manual session: `ps aux | grep
  fakeagent` showed zero leftover fake-agent subprocesses after every run
  was either explicitly stopped or allowed to finish, and zero after the
  daemon's own graceful `SIGTERM` shutdown.

Commands run: `go build ./...`, `gofmt -l .`, `go vet ./...`,
`go test -race -count=5 ./internal/wsapi/... ./internal/wsclient/...`,
`task test`, `task lint`, `task build` — all pass. Manual e2e: real
`bin/smind serve` (with `SMIND_ACP_COMMAND` pointed at a built
`internal/taskrunner/fakeagent` binary) against an isolated `SMIND_HOME`
and a real git-initialized workspace repo, driven entirely through the
built `bin/smind` CLI subcommands described above.
