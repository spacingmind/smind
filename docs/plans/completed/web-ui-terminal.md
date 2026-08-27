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

- [x] `internal/terminal`: PTY-backed session registry, backfill+live,
  bounded scrollback, verified process cleanup
- [x] `internal/wsapi`: `terminal.create`/`attach`/`write`/`resize`/
  `close`/`list` + tests
- [x] `TerminalPane` component (xterm.js) + tests
- [x] Verification (typecheck/tests/build + real-daemon E2E script)

## Validation

**`internal/terminal`** (`internal/terminal/registry.go`, `terminal.go`,
`subqueue.go`, `kill_linux.go`/`kill_other.go`) models
`internal/runs.Registry` closely: `Registry.Subscribe` holds the same
session mutex across the backfill push and live-subscriber registration
as `runs.Registry.Subscribe`, for the same gapless/duplicate-free
guarantee, adapted from discrete `taskrunner.Event`s to a raw PTY byte
stream (backfill is the session's accumulated scrollback buffer pushed as
one `Event`, not N discrete events). `subQueue` is `runs.subQueue` copied
verbatim (adapted to `terminal.Event`) rather than imported across
packages, since it's an unexported type tied to its own package's `Event`.

Genuine differences from Runs, and why:
- **Bounded scrollback** (`scrollbackCap` = 256KiB, `scrollbackHardCap` =
  2x, amortized-compaction trim) since a terminal session, unlike a Run,
  has no natural end to bound its history by.
- **Bidirectional I/O**: `Write`/`Resize` have no Run analog (a Run is
  server->client only).
- **`Close` actually kills something**: unlike a Run's `Stop` (cancels a
  context, the subprocess's own goroutine tears itself down),
  `Registry.Close` calls `killTree` (a real OS-level process kill) and
  blocks on the session's `closedCh` until the background read loop has
  actually observed the process exiting and reaped it (`cmd.Wait()`) --
  so a caller can rely on "the process is gone" by the time `Close`
  returns, not just "we asked it to die".
- **`killTree`'s /proc-walk (`kill_linux.go`)**: a plain
  `kill(-pid, SIGKILL)` on the shell's own process group is not enough --
  an interactive shell with job control (which bash/zsh auto-enable for a
  PTY session) puts each foreground/background command into its *own*
  process group, so a background job (e.g. `sleep 300 &`) would survive a
  group-only kill as an orphan. `killTree` walks `/proc` to find every
  real descendant process and kills each directly, regardless of process
  group. `TestRegistry_Close_KillsBackgroundJobsToo`
  (`registry_linux_test.go`) proves this concretely: starts a background
  job, confirms its real pid is alive, closes the session, confirms both
  the shell's pid and the background job's pid are gone. A non-Linux
  fallback (`kill_other.go`) does a best-effort group-kill only, documented
  as weaker.

**Verification commands run (all clean):**
- `go build ./...`, `go vet ./...`, `gofmt -l $(git ls-files '*.go')` --
  clean.
- `go test -race -count=1 ./...` -- all packages pass, including
  `internal/terminal` (real spawned-shell tests: write/read round trip,
  second-connection backfill, real `stty size` resize verification,
  `Close` process-actually-gone check via signal-0, background-job-kill
  check via `/proc`, a `-race` concurrent-subscribers stress test adapted
  from `internal/runs/subscribe_race_test.go`'s exact shape) and
  `internal/wsapi` (wire-level `terminal.*` round trip tests, including a
  cross-connection backfill test and a detach-does-not-close test).
  `internal/terminal`'s and `internal/wsapi`'s tests force `$SHELL=/bin/bash`
  for determinism (a developer's own interactive shell's dotfiles/theme
  otherwise make output assertions flaky/slow under concurrent spawns --
  discovered via real flakiness while writing these tests, see below).
  Re-ran `go test -race -count=3..15` on the terminal-specific tests
  repeatedly with no failures after fixing two test-harness bugs (not
  implementation bugs): (1) a `task.cancel`-race test needs to read at
  least one event first to synchronize with the server actually having
  dispatched the request, same reason `run_test.go`'s analogous tests do;
  (2) `terminal.close`'s own response and a still-open `terminal.attach`'s
  terminal response it ends both become ready around the same time, so
  their wire arrival order isn't guaranteed -- fixed by reading for both
  ids without assuming order, instead of assuming `close`'s response
  arrives first.
- Web: `bunx tsc -b` clean. `bun run test` (`vitest run`) -- 26/26 tests
  pass across all 4 web test files, including 10 new `TerminalPane` tests.
- `task build` succeeded; `internal/server/dist/.gitkeep` was deleted by
  the build (the known Vite `--emptyOutDir` issue from every prior web UI
  task) and restored via `git checkout`.

**Manual/E2E verification against the real built binary** (`bin/smind`,
temp `SMIND_HOME`, real git repo workspace + task created via the CLI,
a from-scratch Node 26 script using only built-in `fetch`/`WebSocket`,
no test framework): confirmed all of the following against the actual
running daemon:
- `terminal.create` returns a `terminalId` immediately.
- `terminal.write` of `echo hello-e2e-marker\n` produces the real command
  output, observed via `terminal.attach`'s backfill+live "data" stream,
  base64-decoded.
- `terminal.resize` reaches the real PTY: confirmed via the shell's own
  `stty size` output changing to match, not a shell-side cached
  `$COLUMNS`/`$LINES`.
- A second, independent WebSocket connection attaching to the same
  `terminalId` sees the earlier output via backfill, then continues
  receiving new output live.
- `terminal.list` reflects the session's status (`running`, then
  `closed`).
- `terminal.close` actually terminates the shell process: captured the
  shell's own real OS pid via `echo $$` sent through `terminal.write`,
  confirmed alive via `kill -0` before close, confirmed **gone** via
  `kill -0` after `terminal.close` returned.
- Both attached connections' `terminal.attach` calls ended with a clean
  terminal result (not a hang, not an error) once the session closed.
- **Daemon graceful shutdown** (`SIGTERM`) actually kills a still-running
  terminal session's shell process: created a session, captured its real
  pid, sent `SIGTERM` to the daemon process, confirmed (via `kill -0`)
  the shell process was gone after the daemon logged `smind stopped` --
  proving `internal/server.Server.Close` -> `terminal.Registry.CloseAll`
  (wired into `cmd/smind/serve.go`'s `cmdServe`, after
  `httpSrv.Shutdown`) works end-to-end, not just in a unit test.

**What jsdom could and couldn't exercise** (frontend, honesty note per
every prior web UI task's standard): jsdom's `HTMLCanvasElement.getContext()`
is unimplemented ("Not implemented: ... without installing the canvas npm
package"), so a real `@xterm/xterm` `Terminal.open()` under jsdom produces
DOM structure (`.xterm` class etc.) but not real cell/canvas rendering,
and there's no way to simulate real keyboard focus/input reliably driving
xterm's own key-handling. Rather than fight that boundary, `TerminalPane`
factors the terminal-widget dependency out behind a small `TerminalHandle`
interface (`open`/`onData`/`onResize`/`write`/`fit`/`dispose`) -- the same
pattern `WsClientLike`/`FakeWsClient` already established for the
WebSocket boundary -- so the 10 component tests
(`terminal-pane.test.tsx`) exercise the component's own real wiring logic
(calls `terminal.create` then `terminal.attach`; incoming base64 "data"
events are decoded and passed to the handle's `write`; the handle's
`onData` callback firing sends `terminal.write`; the handle's `onResize`
firing sends `terminal.resize`; unmount/task-switch aborts the attach
signal without calling `terminal.close`, and disposes the handle; the
explicit "Close terminal" button does call `terminal.close`; a
`terminal.create` failure surfaces as a visible error) against a
`FakeTerminalHandle` test double, deterministically and without depending
on jsdom's incomplete canvas emulation. What this suite does **not**
verify: real xterm.js rendering/ANSI parsing, real keyboard-driven
`onData` firing, real focus behavior, or real pixel-level terminal
appearance -- those are exactly the browser-only gaps every prior web UI
task in this repo's history has reported, confirmed unavailable in this
sandbox (no real browser).

**Deviations from the spec, and judgment calls:**
- `internal/wsapi.Handler`'s signature is unchanged (existing callers,
  including `wsclient_test.go`, are untouched), but a new
  `wsapi.New`/`wsapi.API` pair was added so `internal/server.Server` can
  reach the `*terminal.Registry` directly, purely to support
  `Server.Close()` -> `terminal.Registry.CloseAll()` on graceful daemon
  shutdown (a spec requirement: "verify no orphaned process/fd survives
  daemon shutdown"). This is the one piece of backend wiring the spec
  didn't explicitly describe the shape of; the plan's `run.start`/
  `run.attach` split reasoning didn't need an analogous shutdown hook
  since Run subprocess lifetime was never claimed to survive daemon exit,
  but a terminal session explicitly is required to not survive it.
- Wire format for `terminal.attach`'s "data" event payload and
  `terminal.write`'s input are asymmetric: output (PTY -> client) is
  base64-encoded (`terminalDataParams`), input (client -> PTY, via
  `terminal.write`) is a plain JSON string. This wasn't explicitly
  specified. Reasoning: PTY output is an arbitrary byte stream that isn't
  guaranteed valid UTF-8 at arbitrary chunk boundaries (a multi-byte
  character split across two `Read()`s, or genuinely binary output from a
  program running in the shell) -- `encoding/json` silently mangles
  invalid UTF-8 in a plain string rather than erroring, corrupting exactly
  the bytes a terminal emulator needs byte-exact, so output goes over the
  wire as base64. Input always originates as a JS string from the
  browser's own keyboard/paste handling, which is always valid UTF-8 on
  the wire, so there's no equivalent risk there and no need for the
  extra encoding overhead.
- `terminal.list` filters server-side by `taskId` (unlike `run.list`,
  which returns everything and is filtered client-side, per
  `docs/plans/active/web-ui-task-detail.md`'s Decisions). Judgment call,
  not specified either way: a terminal session inherently belongs to
  exactly one task's worktree (there's no cross-task terminal use case
  the way a UI might want a global run history), so filtering server-side
  avoids the client fetching every task's sessions just to find its own.
- Added a "Close terminal" button to `TerminalPane` (the spec says "if you
  add one" for this) since a real way to kill a hung/no-longer-needed
  shell seemed necessary for a usable terminal feature, not just nice to
  have.

**Worth a second look:** `killTree`'s `/proc`-walk (`kill_linux.go`) is
the most novel piece of concurrency/OS-interaction code in this change and
the one most worth independent scrutiny -- it's genuinely necessary (see
above) but is also the one place this diverges furthest from directly
mirroring `internal/runs`, since Runs never had to solve "kill an entire
process tree, not just one pid or one process group." The non-Linux
fallback (`kill_other.go`) is untested (no non-Linux CI/dev environment
available here) and intentionally documented as weaker.
