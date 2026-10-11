# Windows: own ConPTY wrapper

Replace `charmbracelet/x/xpty` + `charmbracelet/x/conpty` on Windows with a
small ConPTY wrapper in `internal/terminal` (ADR-0022). Unix keeps xpty.
Builds on `feat/windows-native-terminal` (PR #239) via
`fix/windows-terminal-tests`; branch `feat/windows-conpty-wrapper`.

## Acceptance Criteria

1. **AC1 — no xpty/conpty on Windows.** `internal/terminal`'s Windows build
   imports neither `charmbracelet/x/xpty` nor `charmbracelet/x/conpty`
   (`go list -deps` with `GOOS=windows`). Unix code path unchanged.
2. **AC2 — no raw-handle reads after close.** Pipe ends are `*os.File`;
   `Read`/`Write` after or racing `Close` return an error and never touch
   another session's pipe.
3. **AC3 — drain-to-EOF on natural exit.** When the shell exits, only the
   pseudoconsole is closed; `readLoop` receives everything conhost flushes,
   then EOF, then closes the pipes. Trailing output before `exit` is
   persisted.
4. **AC4 — teardown can't hang on an undrained pipe.** `Close` closes our
   pipe ends before `ClosePseudoConsole`; `killAndReap` with a flooding,
   unread child returns promptly.
5. **AC5 — atomic job membership.** The shell is created inside its
   kill-on-close Job Object (`PROC_THREAD_ATTRIBUTE_JOB_LIST`); no
   post-start assignment.
6. **AC6 — flags.** `PSEUDOCONSOLE_RESIZE_QUIRK` is passed.
7. **AC7 — no resize/close use-after-free.** `Resize` after or racing the
   pseudoconsole close returns an error instead of calling
   `ResizePseudoConsole` on a freed HPCON.
8. **AC8 — behaviour parity.** Every existing `internal/terminal` test
   passes on Windows, `-count=20` green; `GOOS=linux|darwin go vet` clean;
   `GOOS=windows GOARCH=arm64` builds; manual daemon checks 1–7 from the
   PR #239 handoff still pass.

## Test Scenarios

Existing (must stay green): all of `internal/terminal` incl.
`TestWindows_*`, `TestKillAndReap_NoZombieLeft`,
`TestKillAndReap_UnreadOutputDoesNotHang`,
`TestWindows_TrailingOutputSurvivesExit`.

New, `//go:build windows`:

- `TestConPty_ReadAfterCloseErrors` — Close, then Read/Write return an
  error promptly (AC2).
- `TestConPty_CloseUnblocksPendingRead` — a Read blocked on an idle shell
  returns once Close is called (AC2, AC4).
- `TestConPty_ExitDrainsToEOF` — after the child exits and only the
  pseudoconsole is closed, Read returns all output then EOF (AC3).
- `TestConPty_ResizeAfterCloseErrors` — Resize after Close returns an
  error (AC7).
- `TestConPty_ChildStartsInJob` — `IsProcessInJob` is true for the shell
  right after spawn (AC5).
- `TestWindows_ManySessionsNoCrossTalk` — N parallel sessions each echo a
  unique marker and close; every session sees only its own marker (AC2).

## Decisions

- ADR-0022 (own wrapper; chosen by the user 2026-10-10 over upstream PRs,
  vendoring, or a closed-flag mitigation).
- Process handle: after `CreateProcess`, `cmd.Process` is set via
  `os.FindProcess(pid)` while we still hold `pi.Process`, so the existing
  wait/kill seam (`cmd.Process.Wait`) is unchanged.
- Env comes from `exec.Cmd.Environ()` (deduped, `SYSTEMROOT` added by
  `os/exec`); argv0 is `cmd.Path` (already absolute from `resolveShell`).
- `STARTF_USESTDHANDLES` with null std handles so the child can't pick up
  the daemon's own std handles instead of the pseudoconsole.

## Progress

- 2026-10-10: ADR-0022 + this spec.
- 2026-10-10: `conpty_windows.go` (wrapper + spawn), `pty_windows.go`
  (startPty/wait/job), `pty.go` backend-neutral (`ptyIO`), `pty_other.go`
  keeps xpty. New tests in `conpty_windows_test.go`.

## Validation

All on Windows 11 25H2 build 26200.8737, amd64, Go 1.27.0.

- **AC1** — `GOOS=windows go list -deps ./internal/terminal/` has no
  `charmbracelet` package; `go mod tidy` moved `conpty` back to indirect
  (xpty still needs it on Unix).
- **AC2** — `TestConPty_ReadAfterCloseErrors` (Read/Write after Close →
  `os.ErrClosed`), `TestConPty_CloseUnblocksPendingRead` (Close returns
  and the parked Read errors, both < 5s), `TestWindows_ManySessionsNoCrossTalk`
  (16 workers × 6 rounds). The cross-talk test was checked against a
  deliberately reintroduced read-after-close bug on the old xpty code: it
  failed 2/10 runs ("never saw its own marker") at this load, 0/10 at
  8×3 — hence the load.
- **AC3** — `TestConPty_ExitDrainsToEOF` (marker, then `io.EOF`, after
  only `closeConsole`); `TestWindows_TrailingOutputSurvivesExit`; manual
  check 3 (`TAILMARK` in scrollback after `exit` and after re-attach).
- **AC4** — `TestKillAndReap_UnreadOutputDoesNotHang` passes; `Close`
  closes `out`/`in` before `closeConsole`.
- **AC5** — `TestConPty_ChildStartsInJob`: shell and the `ping` it starts
  immediately are both in the job (`IsProcessInJob`).
- **AC6** — `newConPty(80, 24, pseudoconsoleResizeQuirk)` in `startPty`.
- **AC7** — `TestConPty_ResizeAfterCloseErrors` (after `closeConsole` and
  after `Close` → `errConsoleClosed`).
- **AC8** — `go test -count=20 ./internal/terminal/...` green;
  `go vet` clean for windows/amd64, windows/arm64, linux/amd64,
  darwin/arm64. Manual daemon checks with a real `smind serve`:
  1 echo round-trip ✅, 2 resize → `SIZE=137x41` ✅, 3 `exit` → closed +
  tail persisted ✅, 4 close tab kills `ping -t` ✅, 5 hard-kill daemon →
  all 9 descendants gone ✅, 6 Ctrl+Break → `smind stopped`, no
  survivors, sessions `closed` ✅, 7 restart → `interrupted` with
  scrollback ✅.
