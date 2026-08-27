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
- Persistent multi-turn sessions ("send" to an idle finished agent)
  deferred — see Acceptance Criteria. This keeps `internal/taskrunner`'s
  existing one-shot-subprocess-per-call design untouched.
- New package `internal/runs` (registry + broadcast/backfill), consumed
  by `internal/wsapi` for the new `run.*` methods, and by a new
  `cmd/smind` CLI subcommand tree for the `smind` binary itself (it
  already exists as the daemon entrypoint; the CLI verbs become
  subcommands of the same binary, dialing the daemon's own `/ws`).

## Progress

- [ ] `internal/runs`: registry, broadcast-with-backfill, cross-connection
  stop
- [ ] `internal/wsapi`: `run.list`/`run.attach`/`run.logs`/`run.stop`,
  `task.prompt` starts a tracked Run instead of driving it inline
- [ ] `cmd/smind` CLI subcommands
- [ ] End-to-end manual verification against the real built binary

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)
