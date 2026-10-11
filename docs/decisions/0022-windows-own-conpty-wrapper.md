# 0022: Own ConPTY wrapper on Windows (replaces xpty there)

## Status

Accepted (2026-10-10, chosen by the user). Supersedes the "Library:
`charmbracelet/x/xpty`" decision in
`docs/plans/active/windows-native-terminal.md` for Windows only; Unix keeps
xpty (creack/pty). Implementation plan:
`docs/plans/active/windows-conpty-wrapper.md`.

## Decision

On Windows, `internal/terminal` drives ConPTY through its own small wrapper
on `golang.org/x/sys/windows` (`CreatePseudoConsole` / `ResizePseudoConsole`
/ `ClosePseudoConsole` / `CreateProcess` with a proc-thread attribute list)
instead of `charmbracelet/x/xpty` + `charmbracelet/x/conpty`. The wrapper:

- holds its pipe ends as `*os.File`, so a `Read` racing or following
  `Close` fails cleanly instead of touching a closed, possibly recycled,
  raw handle;
- owns the close ordering: the exit path only closes the pseudoconsole and
  lets the reader drain to EOF; teardown closes our pipe ends before
  `ClosePseudoConsole` so it cannot wait on an undrained pipe;
- passes CreatePseudoConsole flags (`PSEUDOCONSOLE_RESIZE_QUIRK`);
- creates the shell already inside its kill-on-close Job Object
  (`PROC_THREAD_ATTRIBUTE_JOB_LIST`), closing the start-to-assignment gap.

## Alternatives considered

- **Keep xpty, upstream the fixes to `charmbracelet/x`.** Declined by the
  user. It also leaves smind waiting on someone else's release for a
  data-mixing bug.
- **Vendor `charmbracelet/x/conpty` (MIT) and patch it.** Carries code we
  don't need; its `Spawn` still can't take a job list without reshaping it,
  so it ends up as most of a rewrite anyway.
- **Keep xpty plus a "closed" flag in `ptySession`.** Narrows the
  Read-after-Close race but can't close it (check-then-ReadFile window),
  and fixes none of the other three issues.
- **Another Go ConPTY library** (`aymanbagabas/go-pty`,
  `UserExistsError/conpty`). Not evaluated in depth; any library exposing
  raw handles has the same race, and none offers job-list spawning.

## Rationale

Found on Windows 11 26200 while validating PR #239:

1. `xpty.PtyOption` takes `Options` by value, so ConPTY flags can never be
   set through xpty v0.1.4.
2. `conpty.ConPty.Read`/`Close` use raw handles. A Read issued after Close
   can hit a recycled handle value and read another session's pipe — an
   experiment that did exactly that made `internal/terminal`'s parallel
   tests lose echoes in ~10% of runs. smind's own Windows exit path
   (`onExit` closing the pty while `readLoop` is between Reads) has the
   same window.
3. xpty closes our read end only after `ClosePseudoConsole`, which
   Microsoft documents may wait indefinitely on an undrained pipe — a
   hang risk on the no-reader `killAndReap` path.
4. xpty's `Start` does the `CreateProcess`, so the Job Object can only be
   assigned after the process is already running.

All four are properties of the ConPTY layer itself; owning ~200 lines of
Windows-only code fixes them together and removes the dependency for the
platform that needed it. Codex (`refs/codex/codex-rs/utils/pty`) takes the
same approach. The seam from PR #239 (`ptySession`, `pty_windows.go`) keeps
the change confined to Windows files.
