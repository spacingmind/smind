# Windows desktop app bundles `smind.exe`, like macOS

Size: L (6) — scope 2 (Rust + CI + docs), PRs 1, risk 1 (release pipeline,
daemon replacement), decisions 1 (ADR-0030 amendment), verification 1
(real Windows needed, not available here).

Implements the 2026-10-13 amendment to ADR-0030. The Windows installers
(NSIS + MSI) carry `smind.exe` as a Tauri sidecar; Install/Update copy it to
`%LOCALAPPDATA%\smind\bin\` exactly as macOS copies its bundled daemon into
its data dir. The GitHub-Release download path is removed. Out of scope:
a native ARM64 installer, auto-start on Windows, code signing, the landing
page copy (separate repo).

## Acceptance Criteria

1. **AC1 — Sidecar configured.** `tauri.windows.conf.json` has
   `bundle.externalBin: ["binaries/smind"]`; the Windows build consumes
   `binaries/smind-x86_64-pc-windows-msvc.exe`. A debug/test build on a
   machine without the staged daemon still compiles (empty placeholder, as on
   macOS); a release build never writes a placeholder.
2. **AC2 — Daemon build task.** `task desktop:win:daemon` builds
   `smind.exe` for `windows/amd64` stamped with the desktop version
   (`desktop/src-tauri/tauri.conf.json`) into
   `desktop/src-tauri/binaries/smind-x86_64-pc-windows-msvc.exe`, with the
   same flags as `desktop:mac:daemon`.
3. **AC3 — Release wiring.** In `release-please.yml`, `build-desktop-windows`
   `needs` `build-binaries` and passes `daemon-version`; `desktop-windows.yml`
   reuses that run's `smind_<version>_windows_amd64` artifact instead of
   rebuilding. `verify-windows-*` and `publish` are unchanged in meaning.
4. **AC4 — PR CI still builds.** Without `daemon-version` (push/PR/manual),
   `desktop-windows.yml` builds the daemon in-job (`task desktop:win:daemon`)
   before `tauri build`, mirroring `desktop-macos.yml`.
5. **AC5 — Installer contains the daemon.** A CI step fails the job unless
   (a) the NSIS installer lists `smind.exe` (and it is not a placeholder:
   > 1 MB), (b) the MSI contains it, and (c) the bundled `smind.exe` runs and
   its `version` output matches the app version (unless a `-dev` dry run).
6. **AC6 — Install copies the bundle.** On Windows, Install/Update resolve
   the sidecar next to the app executable (`smind.exe`), copy it via a
   staged file renamed into `%LOCALAPPDATA%\smind\bin\smind.exe`, and start
   that copy. No network access; a missing or empty bundled file is a clear
   error naming the path and never a download.
7. **AC7 — Update from the bundle.** "Daemon older than the app" on Windows
   is resolved by the same Install/Update flow: the running managed daemon
   is stopped first (existing `windows_update_plan`), then the bundled
   binary replaces `bin\smind.exe`, then the daemon starts and the managed
   record is saved only after the identity/version check passes. A failed
   stop aborts before the binary is touched.
8. **AC8 — #311 logic unchanged.** Stop/restart, identity checks, take-over
   rules and the WSL-conflict refusal behave as before (their existing tests
   pass unmodified).
9. **AC9 — Download path gone.** The Windows host no longer references the
   release URL, zip extraction, checksum download or `IsWow64Process2`
   (`install_binary`, `download_and_verify`, `install_from_release`,
   `native_target_for_this_machine`, `windows_target_from_os_arch` and the
   `zip` dependency are removed); `install_source(Windows)` is `Bundled`.
10. **AC10 — Uninstaller overlap checked.** Tauri's generated NSIS (and WiX)
    template is read to confirm uninstall/update cannot delete `bin\`,
    `managed.json` or `smind.log` and cannot collide with the new sidecar
    `%LOCALAPPDATA%\smind\smind.exe`; the layout is changed only if it can.
11. **AC11 — Docs.** ADR-0030 amendment + ADR-0013 note; README's install
    section describes the Windows app bundling the daemon, and no longer says
    Windows users run it via WSL2 or that there is no native Windows binary.
12. **AC12 — Real machine** (needs the Windows session): fresh install of the
    CI installer, click Install -> daemon runs from `bin\`; upgrade over a
    previously downloaded daemon via Update.

## Test Scenarios

- `windows_install_sources_bundled_binary` — copy the bundled file into a
  layout, replacing an existing `smind.exe`, leaves no staging file (AC6, AC7).
- `windows_install_missing_bundled_binary_is_error` — missing and empty
  source: error names the bundled path, mentions "bundled daemon"/"empty",
  never "download", destination untouched (AC6).
- `bundled_daemon_path_is_platform_named` — `smind.exe` on Windows, `smind`
  elsewhere, resolved next to the app exe (AC6).
- `windows_install_uses_bundled_source` — `install_source(Windows) ==
  Bundled` (AC9); replaces `windows_install_downloads_the_windows_release`.
- `windows_update_stops_running_daemon_before_replacing_binary` — existing
  test, unchanged (AC7, AC8).
- Existing `windows_take_over_refused_for_foreign_owner`,
  `install_refused_on_port_conflict_names_the_port`,
  `classify_wsl_forwarded_port_is_not_takeover_eligible` pass unmodified (AC8).
- `ensure_sidecar_placeholder` is a pure fn tested for the windows triple
  file name (`smind-<triple>.exe`) and for "release never writes" (AC1).
- CI: `desktop-windows` job `Verify bundle` step (AC5); `cargo test` and
  `cargo check --target x86_64-pc-windows-msvc` where the host allows.
- Template read recorded in Validation (AC10).

## Decisions

- Download path removed rather than kept as a fallback — ADR-0030
  amendment (macOS parity, no masked broken bundles, lockstep, less
  untestable code).
- Managed layout kept; sidecar and managed copy are different paths.
- Bundled arch is amd64 (installer is x64); ARM64 runs it emulated until an
  ARM64 installer exists.
- No auto-start on Windows in this change.
- Shared `release.rs` / `checksums.rs` / `native::download_and_verify` are
  left in place (still part of the daemon-client API and macOS tests); only
  the Windows-specific download code is removed.

## Progress

- [x] ADR-0030 amendment, ADR-0013 note, this plan.
- [x] Rust: sidecar path, shared copy install, placeholder, remove download (peer-sonnet a8c3c1b4, 26517d2e, e583de74 — ACCEPTED by Lead after reading the full diff).
- [x] Taskfile `desktop:win:daemon`, `desktop-windows.yml`, `release-please.yml` (Lead, 22cedfa5).
- [x] README (peer-glm 0acf8066, ACCEPTED; installer file-name claim corrected by Lead).
- [ ] Adversarial review of workflow + Rust.
- [ ] PR + `desktop-windows` workflow green with the Verify bundle step.

## Validation

(filled in as the work lands)
