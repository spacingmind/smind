# 0030: The Windows desktop app manages a native `smind.exe`, not a WSL2 daemon

## Status

Accepted (2026-10-12, chosen by the user: "replace it outright"). Amends
ADR-0013's app-managed-daemon host matrix: the **Windows + WSL2** host is
removed and **Windows native** becomes the Windows host. Depends on ADR-0029
(the native daemon runtime) and on the Windows release assets of
`docs/plans/completed/windows-daemon-runtime-parity.md` (AC9). Implementation
plan: `docs/plans/completed/desktop-windows-native-daemon.md`.

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
- `docs/plans/completed/desktop-managed-daemon.md`'s WSL2 sections are
  superseded for Windows.

## Amendment (2026-10-13): the installer bundles `smind.exe`

Chosen by the user: "thống nhất theo macOS" (make Windows match macOS).
Implementation plan: `docs/plans/completed/windows-bundled-daemon.md`.

**Install source is the bundled sidecar.** `tauri.windows.conf.json` lists
`binaries/smind` as an `externalBin` (Tauri stages it as
`binaries/smind-x86_64-pc-windows-msvc.exe` and the installers place it next
to the app as `smind.exe`). **Install** and **Update** copy that file to
`%LOCALAPPDATA%\smind\bin\smind.exe` and run the copy, never the bundled
file in place — the same copy-then-run model macOS uses (ADR-0013 D2), so
the identity checks (`exe_path` of the managed record, the live port
owner's image path) keep pointing at one stable location that does not move
with the install directory or get locked by the installer. The copy is
staged beside its destination and renamed into place; a running managed
daemon is stopped first (a running exe cannot be replaced on Windows),
unchanged from the first amendment's stop/restart/identity/take-over/WSL
rules.

**The download path is removed, not kept as a fallback.** Reasons:
1. macOS has no fallback and its tests assert that a missing or empty
   bundled daemon is a clear error ("reinstall smind"), never a download;
   "one behaviour across both desktop OSes" is the point of the change.
2. A fallback masks a broken installer. CI now fails the build if the
   installer lacks a runnable `smind.exe` of the app's version; a silent
   network repair path would hide the same defect on users' machines and
   cannot be exercised without a Windows machine.
3. The bundle guarantees version lockstep (the app and its daemon are built
   from the same commit with the same stamp) and works offline; a download
   can only add skew (a yanked/never-published tag, a dev build, a proxy
   blocking GitHub).
4. It deletes the zip/checksum/`IsWow64Process2` code (and the `zip`
   dependency) from the Windows host. The `smind_<version>_windows_<arch>.zip`
   release assets remain published for manual installs and
   `verify-windows-*`.

**Version lockstep.** The Windows installer is built from the same release
run as the daemon (`build-desktop-windows` needs `build-binaries` and
bundles its `windows_amd64` artifact, exactly as `build-desktop-macos` does
for darwin) and CI asserts the bundled `smind.exe` reports the app version.
"Daemon older than the app" is therefore resolved by **Update from the
bundle**, which is the same Install code path as on macOS.

**Existing downloaded daemon on upgrade.** Nothing to migrate: the layout is
unchanged (`%LOCALAPPDATA%\smind\bin\smind.exe`, `managed.json`,
`smind.log`) and the `managed.json` record stays valid. A daemon installed
by the download flow is replaced by the bundled one the first time the user
runs **Update** (the app's updater offers it as "daemon older than the app"
exactly as before); until then it keeps running untouched. The bundled copy
overwrites it in place after the usual stop.

**Architecture.** The installer is built on `windows-latest` (x64), so it
bundles the `amd64` daemon. On an ARM64 Windows machine the x64 app already
runs under emulation and now runs an x64 daemon under emulation as well; the
previous "daemon follows the OS architecture" rule goes away with the
download. A native ARM64 desktop installer (which would bundle the `arm64`
daemon via the target-triple suffix) is a follow-up; the `arm64` zip stays
published and verified by `verify-windows-arm64`.

**Uninstaller / layout overlap.** The NSIS default install dir is also
`%LOCALAPPDATA%\smind` (the managed layout's root). Tauri's uninstaller (and
in-place update) deletes only the files it installed — now `smind-desktop.exe`,
the bundled `smind.exe` sidecar and `uninstall.exe` — then a non-recursive
`RMDir "$INSTDIR"`, so `bin\`, `managed.json` and `smind.log` survive. The
bundled sidecar (`%LOCALAPPDATA%\smind\smind.exe`) and the managed copy
(`%LOCALAPPDATA%\smind\bin\smind.exe`) are different files in different
directories, so an update never touches the running daemon's image. The
layout is therefore **kept**; the plan's validation records the check of
Tauri's NSIS/WiX templates. A running managed daemon surviving uninstall is
pre-existing behaviour and unchanged.

**Not decided here.** Auto-start of the daemon at app launch is macOS-only
today (`auto_start`); extending it to Windows is a separate product decision.
