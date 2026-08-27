# Web UI: embedded terminal (xterm + real PTY)

## Acceptance Criteria

- A new backend package `internal/terminal` gives each task a real,
  interactive shell running inside that task's git worktree (cwd =
  `store.Task.WorktreePath`), spawned via a real pseudo-terminal (add
  `github.com/creack/pty` — the standard, widely-used Go PTY library; no
  such dependency exists in `go.mod` yet, confirm before assuming
  otherwise) so interactive programs (an editor, `less`, shell job
  control, ANSI colors) work correctly, not just a plain
  `exec.Command`+pipes shell-out.
- **Model this closely on `internal/runs.Registry`'s already-proven
  design** — read `internal/runs/registry.go` and `internal/runs/subqueue.go`
  in full before writing anything. The shape is the same problem in a
  different domain: a long-lived thing (there, a Run's subprocess; here, a
  PTY session) whose lifetime is independent of any one WebSocket
  connection, with backfill-then-live delivery to however many
  connections/tabs are currently attached, and no gap/no duplicate between
  the two. Reuse the same locking discipline (or the `subQueue` type
  itself, if it's a clean fit without domain-specific baggage — your
  judgment) rather than reinventing it with subtly different, unreviewed
  concurrency logic. The scrollback buffer (backfill) should be bounded
  (pick a reasonable cap, e.g. a fixed byte/line count) rather than
  growing forever for a long-lived terminal session — unlike a Run's
  history, which is naturally bounded by the turn ending.
- New `internal/wsapi` methods, following the `run.start`/`run.attach`
  split's already-established reasoning (a session must outlive any one
  connection/tab, so "create" and "watch" are separate calls, and
  detaching — closing a tab, switching away — must not kill the shell):
  - `terminal.create {taskId}` → `{terminalId}`, spawns the PTY
    immediately and returns without blocking.
  - `terminal.attach {terminalId}` → streams `"data"` events (raw
    output chunks) with backfill-then-live semantics, exactly like
    `run.attach`'s contract; detaching (the request's own context going
    Done) only detaches, never kills the shell.
  - `terminal.write {terminalId, data}` → one-shot, writes `data` to the
    PTY's stdin (this is how keystrokes/input reach the shell).
  - `terminal.resize {terminalId, cols, rows}` → one-shot, resizes the
    PTY (real terminal programs need real resize events, not just a fixed
    80x24).
  - `terminal.close {terminalId}` → actually kills the shell process and
    its PTY, analogous to `run.stop`.
  - `terminal.list {taskId}` → existing terminal session(s) for a task
    (mirrors `run.list`'s reasoning: a UI reconnecting or opening a second
    tab needs to discover an already-running session rather than always
    creating a new one).
- The shell process is a real child of the daemon process — verify (don't
  just assume) that it's cleanly killed when `terminal.close` is called
  (no orphaned process/PTY fd) and when the daemon itself shuts down.
- Frontend: a self-contained `TerminalPane` component (new file(s) under
  `web/packages/ui/src/components/`) taking `{ client, task }` props,
  using `xterm.js` (add `@xterm/xterm` + `@xterm/addon-fit`, the current
  maintained package names — the old `xterm`/`xterm-addon-fit` names are
  deprecated, use the `@xterm/*` scoped ones) wired to
  `terminal.create`/`terminal.attach`/`terminal.write`/`terminal.resize`.
  Keystrokes in the terminal go out via `terminal.write`; resizing the
  pane (or the browser window) calls `terminal.resize` with the real
  new dimensions (xterm's fit addon gives you this).
- **Do NOT wire `TerminalPane` into `App.tsx` or `task-detail.tsx`.**
  Deliberate, not an oversight — a file explorer and a diff viewer are
  being built in parallel against the same shell files; three agents
  editing `App.tsx` independently would conflict. Prove `TerminalPane`
  works as a fully self-contained, independently mountable component
  (your own component tests rendering it standalone, plus real-daemon
  manual verification below); actual integration into the app shell
  happens in one follow-up step after all three parallel features merge.

## Test Scenarios

- Go: real tests for `internal/terminal` (spawn a real shell, e.g. `sh -c
  "echo hi"` or an interactive `sh`, feed it input via the write path,
  read real output via the backfill+live subscribe path, confirm resize
  actually reaches the PTY — `creack/pty`'s `Setsize` or equivalent).
  Concurrency: adapt (don't skip) the same class of stress test
  `internal/runs/subscribe_race_test.go` already proved for Runs —
  multiple subscribers attaching at staggered times against a stream of
  real output, asserting gapless/duplicate-free delivery, run with
  `-race -count>1`. A second connection attaching to an existing terminal
  sees backfill + live output correctly. `terminal.close` actually kills
  the process (verify via the process no longer existing, not just "the
  call returned").
- `internal/wsapi` tests for the full `terminal.*` method set, wire-level,
  same pattern as `internal/wsapi/run_test.go`.
- Frontend component tests (jsdom + Testing Library + `FakeWsClient`) for
  `TerminalPane`: mounting calls `terminal.create` then
  `terminal.attach`; incoming `"data"` events render in the terminal;
  typing sends `terminal.write`; unmount/detach doesn't call
  `terminal.close` (matches the detach-not-kill requirement); an explicit
  "close terminal" action (if you add one) does call `terminal.close`.
  jsdom + a full xterm.js render is unusual territory — if xterm.js
  doesn't render meaningfully under jsdom, test at the level that's
  actually meaningful (the component's calls to the client / props it
  passes to the xterm instance), and say plainly in your report what
  jsdom could and couldn't exercise here, same honesty standard as every
  prior web UI task's browser-verification gap.
- Manual/E2E verification against the real built binary: a from-scratch
  Node script (no real browser available in this environment, confirmed
  in every prior web UI task) driving the actual wire sequence —
  `terminal.create`, `terminal.write` a real command (e.g. `echo hello\n`),
  confirm the real output comes back via `terminal.attach`'s backfill/live
  stream, `terminal.resize`, a second independent connection observing the
  same session via backfill, `terminal.close` actually terminating the
  shell process (check via `ps`, same as prior tasks' subprocess-cleanup
  checks).
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  clean. `bunx tsc -b` clean, `bun run test` passes. `task build`
  succeeds; check `internal/server/dist/.gitkeep`.

## Decisions

- Models `internal/runs.Registry`'s backfill+live design rather than
  inventing new concurrency logic — that design has already been through
  two rounds of adversarial review this project (see
  `docs/plans/completed/run-registry-and-cli.md`) and the same fundamental
  correctness property applies here (a subscriber joining mid-stream must
  see every byte exactly once, no gap, no duplicate).
- `github.com/creack/pty` for real PTY support, `@xterm/xterm` +
  `@xterm/addon-fit` for the frontend terminal emulator — both are the
  current, actively-maintained standard choices for this.
- A bounded scrollback buffer (unlike Runs' naturally-bounded-by-turn-
  ending history) — pick and document a concrete cap.
- Explicitly NOT wiring into `App.tsx`/`task-detail.tsx` — see Acceptance
  Criteria; built in parallel with a file explorer and a diff viewer
  against the same shell.
- Explicitly out of scope: multiple simultaneous terminal sessions per
  task in the UI (the backend's `terminal.list`/multi-session support is
  fine to build since it falls out of the Registry-style design almost
  for free, but the frontend only needs to drive one at a time this pass),
  terminal session persistence across a daemon restart.

## Progress

- [ ] `internal/terminal`: PTY-backed session registry, backfill+live,
  bounded scrollback, verified process cleanup
- [ ] `internal/wsapi`: `terminal.create`/`attach`/`write`/`resize`/
  `close`/`list` + tests
- [ ] `TerminalPane` component (xterm.js) + tests
- [ ] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
