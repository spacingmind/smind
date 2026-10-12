# Desktop app: manage a native Windows daemon (replace WSL2)

Implements ADR-0030. On Windows the Tauri app installs, starts, stops and
updates a native `smind.exe` and drops every WSL2 code path. Depends on
`docs/plans/active/windows-daemon-runtime-parity.md` (the daemon runtime,
`POST /api/shutdown`, and the `smind_<version>_windows_<arch>.zip` release
assets). Out of scope: an ARM64 desktop installer (the app is still an x64
installer; the daemon asset follows the OS architecture), code signing,
and migrating data from WSL.

## Acceptance Criteria

1. **AC1 — Windows is one host.** `Platform` has no `Wsl2` variant; on a
   Windows target `detect_platform` returns `WindowsNative` without ever
   probing `wsl.exe`. No desktop crate invokes `wsl.exe` or `wslpath`
   anywhere (a test greps the Rust sources to keep it that way), and
   `daemon_manager/wsl.rs` is deleted.
2. **AC2 — Install from the release.** The app downloads
   `smind_<version>_windows_<arch>.zip` and `checksums.txt`, verifies the
   SHA-256, and extracts `smind.exe` to `%LOCALAPPDATA%\smind\bin\`.
   `<arch>` follows the OS architecture (amd64 or arm64, including an x64
   app running under emulation on ARM64). A checksum mismatch, a zip without
   `smind.exe` at its root, and an entry that escapes the destination
   directory (zip-slip) are all refused and leave any previously installed
   binary untouched.
3. **AC3 — Detached start.** The app starts `smind.exe serve` with no console
   window and detached from the app, so it keeps running after the app
   quits. A `managed.json` under `%LOCALAPPDATA%\smind\` records the pid,
   version, install time and image path; stdout/stderr go to `smind.log`
   beside it.
4. **AC4 — Graceful stop and restart.** Stop and restart call
   `POST /api/shutdown` with the token from `%USERPROFILE%\.smind\token`
   (or `SMIND_HOME`), then wait until `/healthz` stops answering. Only if
   the daemon does not answer within the timeout does the app terminate the
   recorded pid, and only after verifying that the live port owner is that
   pid and its image path is the managed `smind.exe`.
5. **AC5 — Port owner and identity.** The pid listening on the daemon port
   comes from the IP Helper TCP table and its image path from
   `QueryFullProcessImageName`; neither shells out to `netstat` or `tasklist`.
6. **AC6 — Unmanaged daemons.** If something answers on the port that the
   app did not start: a `smind.exe` owner is offered *take over* exactly as
   on macOS; any other owner (notably the WSL relay that forwards a WSL
   daemon's port) is reported as "a daemon is running in WSL" with
   instructions to stop it, *take over* is NOT offered, and *install* is
   refused with a message naming the port conflict. The app never signals a
   process it cannot positively identify as `smind.exe`.
7. **AC7 — Native paths in the shell integration.** "Reveal in Explorer" and
   "Open in <editor>" use the daemon's native Windows paths as given;
   `wslpath`, `\\wsl.localhost\...` translation, and the distro argument are
   gone from `editors.rs`.
8. **AC8 — Prerequisites are surfaced.** The daemon status reports whether
   Git for Windows (`git.exe`) is found, with a message when it is not
   (worktrees need it; setup scripts need its `sh`); install/start still
   work without it.
9. **AC9 — UI text.** Settings → Daemon shows Windows paths for the log file
   and a "running in WSL" explanation per AC6; no user-facing string mentions
   WSL except that explanation and the first-run notice that WSL data is not
   migrated.
10. **AC10 — Verified on a real machine.** On this Windows 11 machine, with a
    freshly built `smind.exe` standing in for the release, the real app:
    installs, starts, shows Running + Managed, restarts (pid changes), survives
    an app quit and relaunch, and stops via the API; and it refuses to take
    over a WSL-hosted daemon.

## Test Scenarios

Rust unit tests (mirroring `native.rs`'s style: fakes behind small traits, a
local HTTP server for the release download):

- `windows_install_extracts_smind_exe_and_verifies_checksum`
- `windows_install_rejects_checksum_mismatch_and_keeps_old_binary`
- `windows_install_rejects_zip_without_root_smind_exe`
- `windows_install_rejects_zip_slip_entry`
- `asset_name_windows_amd64_and_arm64`; `windows_target_from_os_arch`
  (including x64-on-ARM64 emulation reporting arm64)
- `windows_spawn_detached_has_no_console_and_survives_parent` (spawn a
  helper, kill the parent, assert the child lives)
- `windows_stop_uses_api_shutdown_then_waits_for_healthz_down`
- `windows_stop_falls_back_to_terminate_only_with_identity_match`
- `windows_stop_refuses_to_terminate_unidentified_process`
- `port_owner_via_iphelper_finds_listening_pid` (a real listener in-process)
- `exe_path_for_pid_returns_image_path`
- `classify_wsl_forwarded_port_is_not_takeover_eligible`
- `daemon_status_reports_missing_git`
- `no_wsl_exe_in_desktop_sources` (the AC1 grep)
- editors: the existing reveal/open argv tests keep passing with native
  paths; new `windows_paths_pass_through_untranslated`; the WSL-translation
  tests are deleted with the code.

Manual (AC10), by the coordinator, with screenshots: install, restart,
quit/relaunch, stop; and an unmanaged WSL daemon on the port.

## Decisions

- ADR-0030 (the user chose to replace WSL2 outright, 2026-10-12).
- No data migration from WSL; a WSL daemon remains usable as a plain remote
  connection.
- The app stays an x64 installer for now; only the daemon asset follows the OS
  architecture.

## Slices

- **R1** `daemon-client`: a `windows_native` module (install, spawn, stop,
  port owner, identity), `release.rs` windows assets (AC2-AC5).
- **R2** `src-tauri/daemon_manager.rs`: `Platform::WindowsNative`, wiring,
  unmanaged-WSL handling, git prerequisite, delete `wsl.rs` and every WSL
  branch (AC1, AC6, AC8, AC9). Depends on R1.
- **R3** `src-tauri/editors.rs`: native paths, delete the `wslpath` code
  (AC7). Independent of R1/R2.

## Progress

- 2026-10-12: ADR-0030 and this spec. R1 and R3 start first; R2 after R1.
- 2026-10-12: Rust toolchain unblocked on the dev machine (Windows SDK
  installed; cargo runs through an MSVC env wrapper). Baseline fix
  `fix/desktop-client-windows-tests` (unix-only tests gated). Work is
  integrated on `integration/desktop-windows-native`.
- 2026-10-12: R3 merged (`editors.rs` has no WSL code; 50 src-tauri tests
  pass, the 3 baseline failures were Unix-fixture bugs). R1 merged
  (`windows_native.rs`, Windows release assets; 206 daemon-client tests
  pass). Coordinator hardening on top: `GetExtendedTcpTable` retried on
  size growth and read from a u32-aligned buffer. Deviations accepted: the
  daemon takes no port argument, so `spawn_detached(layout, version)`; the
  TCP table is read with `TCP_TABLE_OWNER_PID_ALL` and filtered to LISTEN
  because the LISTENER class did not list the loopback listener.
- 2026-10-12: R2 merged (wiring, `wsl.rs` and every WSL branch deleted,
  DTO + UI text). Review found one regression the agent missed: the AC6
  take-over gate classified by the `smind.exe` file name on every platform,
  which would have refused every macOS take-over; fixed by taking the
  platform into `take_over_refusal`, with a regression test
  (`take_over_refusal_never_applies_off_windows`). Also repaired a doc
  comment in `menu.rs`. macOS code still cannot be compiled or run on this
  machine; the macOS arms are verified by reading and by their unchanged
  tests.
- Open: `auto_start` stays macOS-only (no bundled Windows daemon); AC10 needs
  a real release asset or a local stand-in (see Validation).

## Validation

State at `integration/desktop-windows-native` (769d32f8), run on the Windows
11 dev machine: daemon-client 187 lib + 2 integration tests, src-tauri 56
tests, web ui 1743 tests and `tsc -b` all green.

- AC1: `platform_for_windows_is_native_and_never_probes_wsl`,
  `no_wsl_exe_in_desktop_sources`; `wsl.rs` deleted. Covered by tests.
- AC2: `windows_install_*` (checksum, mismatch keeps old binary, no root
  exe, zip-slip), `asset_name_windows_amd64_and_arm64`,
  `windows_target_from_os_arch`. Unit-covered; not yet exercised against a
  real GitHub release (none carries a Windows zip until slice E ships).
- AC3: `windows_spawn_detached_has_no_console_and_survives_parent`.
- AC4: `windows_stop_*` (API shutdown then healthz wait; terminate only on
  identity match; refuse unidentified).
- AC5: `port_owner_via_iphelper_finds_listening_pid`,
  `exe_path_for_pid_returns_image_path`.
- AC6: `classify_wsl_forwarded_port_is_not_takeover_eligible`,
  `windows_take_over_refused_for_foreign_owner`,
  `install_refused_on_port_conflict_names_the_port`,
  `take_over_refusal_never_applies_off_windows`; UI test hides Take over.
- AC7: `windows_paths_pass_through_untranslated`, existing reveal/open argv
  tests.
- AC8: `daemon_status_reports_missing_git`,
  `git_lookup_finds_program_files_cmd_git`; UI prerequisite notice test.
- AC9: web tests for unsupported text, Windows paths, WSL notice.
- AC10 (2026-10-12, debug app `tauri build --debug --no-bundle` driven over
  WebView2 CDP, isolated `SMIND_HOME`/`LOCALAPPDATA`/`APPDATA`, a freshly
  built `smind.exe` placed at `%LOCALAPPDATA%\smind\bin\` as the stand-in):
  - Status on a clean machine: platform `windows`, Windows log path, git
    found, binary installed, not running.
  - A native `smind.exe` started outside the app: Unmanaged and take-over
    eligible; Take over -> Managed; Restart -> pid 25508 -> 31372, the old
    daemon's log ends `smind stopped` (graceful `POST /api/shutdown`, not a
    kill); `managed.json` and `smind.log` under `%LOCALAPPDATA%\smind`.
  - App quit: daemon stayed up. Relaunch: still Managed with the same pid;
    a second Restart worked. Daemon later stopped with `smind stop`.
  - WSL-held port (a listener inside Ubuntu answering `/healthz`): found a
    real defect (below), fixed; afterwards Unmanaged + `wslDaemonSuspected`,
    take-over and install both refused with the WSL/port message, restart
    refused, nothing signalled, Settings -> Daemon shows the explanation
    and no Take over (screenshot `ac10-wsl-notice.png`).
  - Defect 1: WSL forwards localhost in the kernel, so the port answered with
    no Windows owner at all; the app said NotRunning and would have offered
    Install onto it. Fixed (`effective_managed_state`,
    `answering_with_no_visible_owner_is_unmanaged_on_windows_only`). While
    the Ubuntu VM was still alive after `wsl --terminate`, `smind.exe` could
    not bind the port either, so this case is real.
  - Defect 2: `DETACHED_PROCESS` overrides `CREATE_NO_WINDOW`, leaving the
    daemon with no console so each git/agent child allocated its own
    (`conhost.exe` per call). Now `CREATE_NO_WINDOW` alone; the daemon owns a
    hidden console (`AttachConsole` False -> True). A flashing window cannot
    be observed from this non-interactive session, so that part is verified
    structurally only.
  - NOT covered: Install from a real GitHub release (no release carries a
    Windows zip until the release workflow ships; the path is unit-tested
    against a local fixture), ARM64, the NSIS installer upgrade path.
  - Layout note: the NSIS installer's default dir is also
    `%LOCALAPPDATA%\smind`, so `bin\`, `managed.json` and `smind.log` sit
    next to `smind-desktop.exe`. Checked by reading the installer script
    Tauri generates (`target/<profile>/nsis/x64/installer.nsi`, built with
    `tauri build --bundles nsis`; the installer itself was NOT run, because
    it would overwrite the real app's `HKCU` uninstall entry): install only
    writes the main exe and `uninstall.exe` (and deletes a renamed old main
    binary); uninstall and in-place update delete only those two files and
    call a non-recursive `RMDir "$INSTDIR"`; the only recursive deletes are
    `$APPDATA\dev.spacingmind.desktop` and `$LOCALAPPDATA\dev.spacingmind.desktop`,
    behind the "delete app data" checkbox. So `bin\`, `managed.json` and
    `smind.log` survive uninstall and update, and a running `smind.exe` does
    not block an update (the installer only looks for the main exe). Not
    checked: the MSI target (also built by the release workflow) and its
    uninstall behaviour. Re-check this when Tauri is upgraded.
