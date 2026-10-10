# Windows native terminal backend

First step toward a native-Windows daemon (ADR-0013 "Windows native" host,
currently blocked on `internal/terminal`). Scope here is **only** making
`internal/terminal` cross-platform so `GOOS=windows` builds and the terminal
registry works on Windows, verified by a real `windows-latest` CI job.
Shipping `smind.exe` and having the desktop app manage it without WSL are
follow-up plans (see Decisions).

## Acceptance Criteria

1. **AC1 — Windows builds.** `GOOS=windows GOARCH=amd64 go build ./...` and
   `GOOS=windows GOARCH=arm64 go build ./...` succeed. `go vet ./...` with
   `GOOS=windows` succeeds.
2. **AC2 — one PTY backend, two OSes.** `internal/terminal` no longer
   imports `github.com/creack/pty` directly; it uses
   `github.com/charmbracelet/x/xpty` (`NewPty` → creack/pty on Unix, ConPTY
   on Windows). Platform differences are confined to `_windows.go` /
   `_unix.go` (or `_other.go`) files behind a small internal seam
   (start / resize / wait / kill), not `runtime.GOOS` branches in
   `registry.go`.
3. **AC3 — Unix behaviour unchanged.** Every existing `internal/terminal`
   test passes unmodified on macOS and Linux (`task test`), including the
   Linux-only `/proc` killTree and persistence tests. `task lint` passes.
4. **AC4 — shell exit is detected on Windows.** ConPTY does not EOF the
   output pipe when the child exits. A session whose shell exits on its own
   (user types `exit`) reaches `StatusClosed` with final scrollback
   persisted, same as on Unix — i.e. a waiter on the process closes the
   pseudo console so `readLoop` unblocks. No goroutine leak, no hang.
5. **AC5 — process-tree kill on Windows.** `Close`/`CloseAll` kill the shell
   **and its descendants** (e.g. a `ping -t localhost` started from the
   shell). Implementation: assign the shell to a Job Object with
   `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` immediately after start, and close
   the job handle in killTree. Also covers daemon crash (job handle closes
   with the process).
6. **AC6 — shell resolution on Windows.** `resolveShell` on Windows picks
   `pwsh.exe` if on PATH, else `powershell.exe`, else `%COMSPEC%`, else
   `cmd.exe`. `$SHELL` is ignored on Windows. Unix resolution unchanged.
7. **AC7 — resize works on Windows** via the pty's `Resize` (ConPTY
   `ResizePseudoConsole`); `Resize` on a closed session still returns the
   existing not-running error.
8. **AC8 — Windows CI job.** `.github/workflows/ci.yml` gains a
   `windows-latest` job that runs `go build ./...`, `go vet ./...` and
   `go test ./internal/terminal/...` (blocking), plus `go test ./...` with
   `continue-on-error: true` whose failures are listed in Validation as
   input for the follow-up runtime-parity plan.
9. **AC9 — docs.** ADR-0013's "Windows native … Blocked today" note and
   README's "no native Windows daemon binary yet (blocked on
   internal/terminal)" are updated to say the terminal blocker is lifted
   and point to this plan; nothing claims Windows is a supported daemon
   host yet.

## Test Scenarios

Existing tests (must stay green on Unix, unmodified): all of
`registry_test.go`, `persistence_test.go`, `subscribe_race_test.go`,
`*_linux_test.go`.

Tests that use bash-only syntax (`exec.Command(resolveShell(), "-c",
"sleep 300")`, `forceTestShell` setting `$SHELL=/bin/bash`) get a build tag
or a per-OS helper — do not weaken their Unix assertions. Shell commands
written to the PTY in shared tests (`echo <marker>\n`) work in cmd/pwsh too;
on Windows the tests force `cmd.exe` (deterministic prompt) via a
`forceTestShell` equivalent and send `\r\n`.

New, Windows-tagged (`//go:build windows`), run in the CI job:

- `TestWindows_CreateEchoRoundTrip` — Create, write `echo hello-win\r\n`,
  subscriber sees `hello-win`.
- `TestWindows_ShellExitClosesSession` — write `exit\r\n`; session reaches
  `StatusClosed` within 10s, `Close` afterwards is a no-op, persisted
  history contains output written before exit. (AC4)
- `TestWindows_CloseKillsDescendants` — start `ping -t 127.0.0.1` from the
  shell, capture its PID (e.g. via `wmic`/`Get-CimInstance` or by spawning
  through a helper that prints its PID), `Close`, assert that PID is gone
  within 5s. (AC5)
- `TestWindows_CloseAllConcurrent` — 3 sessions, `CloseAll` returns, all
  `StatusClosed`, no hang (test timeout 30s).
- `TestWindows_Resize` — `Resize(80, 24)` then `Resize(120, 40)` on a live
  session returns nil; on a closed session returns the not-running error.
  (AC7)
- `TestResolveShell_Windows` — table test over PATH/COMSPEC fixtures
  (pwsh present; only powershell; neither → COMSPEC; COMSPEC empty →
  cmd.exe). (AC6)

Cross-platform:

- `TestCreate_FailedPersistKillsShell` (or existing equivalent) still
  passes — the `killAndReap` error path must use the new wait seam, since
  `cmd.Wait` is invalid for ConPTY-started processes (use
  `xpty.WaitProcess`).

## Decisions

- **Library: `charmbracelet/x/xpty`** (v0.1.4, 2026-07-30) over a hand-rolled
  ConPTY wrapper on `golang.org/x/sys/windows`. Its Unix path is
  `creack/pty` v1.1.24 — the exact version already in go.mod — so Unix
  behaviour should not move. ConPTY edge cases (attribute-list process
  creation, pipe close ordering, resize) stay upstream. Chosen by the user
  2026-10-10. Wrapped behind an internal seam so swapping to a hand-rolled
  backend later touches only the `_windows.go` file.
  **Superseded on Windows by ADR-0022** (2026-10-10): own ConPTY wrapper,
  see `docs/plans/active/windows-conpty-wrapper.md`. Unix keeps xpty.
- **Kill on Windows = Job Object**, not `taskkill /T`: survives daemon
  crash, no process-spawn per kill, no PID-reuse race. xpty does not manage
  process trees, so this is ours.
- **Requires Windows 10 1809+** (ConPTY). No winpty fallback.
- **Out of scope (follow-up plans):** (a) runtime parity of the rest of
  the daemon on Windows — agent CLI spawn via `.cmd` shims, path handling,
  git worktrees — driven by AC8's non-blocking full-suite results;
  (b) release `windows/{amd64,arm64}` binaries; (c) desktop app managing
  `smind.exe` natively and whether the WSL2 host stays as an option. (c)
  changes the ADR-0013 host matrix and needs a user decision first.

## Progress

- [x] Spec written (2026-10-10).
- [ ] Implementation.
- [ ] Windows CI green.

## Validation

_(fill per AC: test name or command + result; for AC8 list the non-blocking
full-suite failures verbatim.)_
