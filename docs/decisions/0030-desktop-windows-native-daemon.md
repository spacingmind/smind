# 0030: The Windows desktop app manages a native `smind.exe`, not a WSL2 daemon

## Status

Accepted (2026-10-12, chosen by the user: "replace it outright"). Amends
ADR-0013's app-managed-daemon host matrix: the **Windows + WSL2** host is
removed and **Windows native** becomes the Windows host. Depends on ADR-0029
(the native daemon runtime) and on the Windows release assets of
`docs/plans/active/windows-daemon-runtime-parity.md` (AC9). Implementation
plan: `docs/plans/active/desktop-windows-native-daemon.md`.

## Decision

On Windows the desktop app installs, updates, starts, stops and restarts a
**native `smind.exe`** and no longer manages anything inside WSL2.

- **Install / update.** Download `smind_<version>_windows_<arch>.zip` and
  `checksums.txt` from the GitHub Release, verify the SHA-256 (same
  discipline as the macOS flow), and extract `smind.exe` to
  `%LOCALAPPDATA%\smind\bin\`. State (`managed.json`) and the daemon log
  live next to it under `%LOCALAPPDATA%\smind\`. `<arch>` follows the OS
  architecture (amd64 / arm64), not the app's.
- **Start.** Spawn `smind.exe serve` detached with no console window, so it
  survives the app quitting (as on macOS, ADR-0013). The daemon's own data
  directory stays `%USERPROFILE%\.smind`.
- **Stop / restart.** `POST /api/shutdown` with the daemon's token (ADR-0029
  D2), then wait for `/healthz` to stop answering. If the daemon never
  answers, fall back to terminating the recorded pid, but only after
  checking the live port owner is that pid and its image path is the
  managed `smind.exe` (the same identity check as the other hosts).
- **Port owner.** Found through the IP Helper TCP table; its image path
  through `QueryFullProcessImageName`. This replaces `ss` and
  `/proc/<pid>/exe`.
- **Existing WSL daemons.** The app never starts, stops, updates or takes
  over a daemon running in WSL2. If something unmanaged already answers on
  the port, the existing "running, not managed" state applies, but
  *take over* is offered only when the port owner is a Windows process
  whose image is a `smind.exe`; for a WSL daemon (the port is forwarded by
  WSL, so the owner is not `smind.exe`) the app explains that it is running
  in WSL and how to stop it, and refuses **Install** with a message naming
  the port conflict (the app does not choose the port; the daemon reads it
  from its own config) until that daemon is stopped. A WSL daemon can still
  be used as an ordinary *remote
  connection* (the app already supports remote daemons by URL); it is just
  never managed.
- **No data migration.** The native daemon starts with its own empty
  `%USERPROFILE%\.smind`; tasks and worktrees inside a WSL distro are not
  moved. This is stated in the first-run notice.
- **Paths.** Daemon paths are native Windows paths, so the `wslpath` /
  `\\wsl.localhost\...` translation in "Reveal in Explorer" and "Open in
  <editor>" is deleted along with the rest of the WSL code
  (`daemon_manager/wsl.rs`, `Platform::Wsl2`).
- **Prerequisites** the app checks and explains instead of failing
  obscurely: Git for Windows (git worktrees, and the `sh` that runs
  setup/teardown scripts, ADR-0029 D1) and, per provider, the agent CLI
  installed natively on Windows.

## Alternatives considered

- **Keep WSL2 as a second managed host.** The user chose to replace it. It
  would also keep two code paths (640 lines of `wsl.rs`, `wslpath`, Linux
  tooling assumptions) and the confusing "which filesystem is my repo on"
  problem that made every WSL feature (reveal, editors, agents' CLIs) need
  path translation.
- **Auto-migrate WSL data.** Worktrees and the sqlite database hold Linux
  paths; moving them is a project of its own and is not needed to adopt the
  native daemon.
- **A Windows Service or Task Scheduler entry instead of a detached
  process.** More machinery and an elevation prompt for a per-user daemon;
  the detached-process model already matches macOS.

## Rationale

The native daemon is now a supported runtime (ADR-0022, ADR-0029, release
assets), so the WSL indirection only costs complexity: a distro must exist,
agents and git must be installed inside it, and every path crossing the
boundary needs translation. Native also gives the app tree-kill semantics it
controls (Job Objects) and one binary format per OS. Windows 10 1809+ is
expected to work (ConPTY's minimum) but only Windows 11 has been validated.

## Consequences

- WSL-only users must install the native daemon (and Git for Windows) once;
  their WSL data stays where it is.
- ADR-0013's "three hosts" becomes "macOS native and Windows native", and its
  note that Windows native is "not yet a supported daemon host" is retired.
- `docs/plans/active/desktop-managed-daemon.md`'s WSL2 sections are
  superseded for Windows.
