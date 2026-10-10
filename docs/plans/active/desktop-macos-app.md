# macOS desktop app (packaging, bundled daemon, Dock lifecycle)

Goal (user, 2026-10-10): "đang xài mac — làm app macOS luôn, refs từ
zcode, paseo". Today there is no macOS desktop build:

- `desktop/src-tauri/tauri.conf.json` bundles only `deb`.
- CI builds only Windows (`.github/workflows/desktop-windows.yml`, called
  from `release-please.yml`).
- Releases carry no `.app` or `.dmg`.

The macOS-native daemon management code already exists
(`desktop/src-tauri/src/daemon_manager.rs`, `Platform::Macos`, plan
`desktop-managed-daemon` AC5). Today it **downloads** the darwin binary
from the GitHub Release.

This plan is a sibling of `desktop-native-feel.md`. That plan covers the
look: chrome, de-webview, shortcuts. This one covers shipping and
behaving like a Mac app.

References:

- **Paseo** `refs/paseo/packages/desktop/electron-builder.yml` (`mac:`
  section):
  - `dmg` + `zip`
  - `minimumSystemVersion: 13.0.0`
  - category `public.app-category.developer-tools`
  - per-arch artifact names
  - the daemon binary shipped inside the app via
    `extraResources: bin/paseo`
- **ZCode** `refs/zcode/packages/desktop/electron-builder.config.js`
  (`mac:` / `dmg:`):
  - signing is opt-in by env var, and unsigned builds still work
  - `CFBundleURLTypes` for the deep-link scheme
  - DMG background/layout
- **ZCode** `desktopWindowChrome.ts`: no tray on macOS, only a Dock badge.

## Acceptance Criteria

### M1 — Build a macOS app

1. A new `desktop/src-tauri/tauri.macos.conf.json` (the platform config
   Tauri merges automatically, like `tauri.windows.conf.json`) sets the
   bundle targets to `app` + `dmg`, `minimumSystemVersion` `13.0`, and
   category `DeveloperTool`. The base config and the Windows config keep
   their current targets.
2. `task desktop:mac` (new Taskfile target) builds on this Mac for the
   host arch, end to end:
   - the darwin `smind` binary, with the web UI embedded, exactly as
     the release build does;
   - the bundled desktop UI;
   - `smind.app` + `.dmg` under
     `desktop/src-tauri/target/**/bundle/{macos,dmg}/`.

   It runs from a clean checkout with only the toolchains the repo
   already documents (Go, bun, Rust).
3. `task desktop:mac:install` copies the built `smind.app` into
   `/Applications`, replacing any previous copy. A locally built app
   carries no quarantine attribute and opens without a Gatekeeper
   prompt.
4. The app is **unsigned/ad-hoc** (user decision). No Apple credentials
   are needed anywhere. The README "Install" section documents the
   one-time `xattr -dr com.apple.quarantine /Applications/smind.app` for
   a DMG downloaded from GitHub.
5. App name, icon and identifier are correct:
   - Finder, the Dock and the About panel show "smind" with the smind
     logo (`icons/icon.icns`);
   - the identifier stays `dev.spacingmind.desktop`.
6. The `smind://` deep-link scheme is registered in the bundled
   `Info.plist`. `open smind://…` from Terminal routes into the app via
   the existing `deeplink.rs`.
7. The desktop version-fields check (`task` target at
   `Taskfile.yml:99`) still passes. The new config adds no version
   field, or is covered by the check if it does.

### M2 — Bundled daemon (sidecar, like Paseo)

1. The darwin `smind` binary for the build arch ships **inside**
   `smind.app` (Tauri `externalBin` or `resources`, whichever the
   implementation proves works with the current managed-daemon code).
   The app version and the bundled daemon version are always the same
   release.
2. On macOS, install/update in `daemon_manager.rs` takes the binary
   from the app bundle instead of downloading from GitHub. It copies the
   binary into the existing managed layout
   (`<app_data_dir>/managed-daemon/…`, `macos_layout`) and then follows
   the existing spawn/record/verify path unchanged.
   - Why copy: an unsigned app run from Downloads is App-Translocated to
     a random read-only path, which would break the exe-path match
     checks if the daemon ran in place.
   - WSL2 keeps downloading. Nothing changes there.
3. **First launch with no daemon reachable:** the app installs and
   starts the bundled daemon automatically, then the UI connects. No
   "Install daemon" click is needed. The daemon banner and Settings →
   Daemon still show status, and Restart still works.
4. **An older managed daemon is running** (`comparison == "older"`,
   managed): the app replaces it with the bundled binary and restarts
   it automatically on launch, and shows progress through the existing
   `DaemonProgress` events. An **unmanaged** daemon (one the user
   started from a terminal) is never killed automatically. The existing
   take-over flow stays the only way to adopt it.
5. Quitting the app leaves the daemon running (current detached-spawn
   behaviour), so agents keep working. Reopening the app reconnects
   without restarting the daemon.
6. The release build still builds the daemon once per arch. The
   desktop job reuses that artifact rather than compiling Go a second
   time with different flags. Version stamping via ldflags must match,
   so `/healthz` `version` equals the app version.

### M3 — Mac lifecycle (Dock, not tray)

1. **No tray/menu-bar icon on macOS** (user decision, matching ZCode and
   Paseo). Windows and Linux keep the tray exactly as today.
2. Closing the window (red traffic light, `Cmd+W` when no tab is left,
   or the D1 close path) hides the window, and the app stays in the
   Dock. Clicking the Dock icon (`RunEvent::Reopen`) shows and focuses
   the window. `Cmd+Q`, app menu → Quit, and Dock → Quit really quit.
3. The attention count that drives the tray on Windows
   (`smind_daemon_client::attention`, `desktop-quick-wins` AC4) drives
   the **Dock badge** on macOS. It shows the count when > 0 and clears
   at 0. This delivers the macOS half of `desktop-native-feel` D4.4.
4. Notification click still focuses and navigates to the task
   (`notify.rs`). It works when the window was hidden via close.
5. The global shortcut (`CommandOrControl+Shift+S`) still toggles the
   window on macOS.

### M4 — CI + release

1. A new reusable workflow `.github/workflows/desktop-macos.yml`
   (mirroring `desktop-windows.yml`) runs on a macOS runner and builds
   `.dmg` for **arm64 and x86_64**, one artifact per arch, named
   `smind-desktop-<version>-macos-<arch>.dmg`. It also runs on PRs that
   touch `desktop/**` or the workflow, as an artifact-only build.
2. `release-please.yml` calls it next to `build-desktop-windows` and
   attaches both DMGs to the GitHub Release. The release job's `needs`
   and `if` cover the new job, so a failed macOS build fails the
   release the same way a failed Windows build does.
3. No signing secrets are referenced (M1.4). The workflow comment says
   signing and notarization are out of scope, like the Windows one.

## Test Scenarios

### Rust (`cargo test`, no webview)

- `macos_install_sources_bundled_binary` — given a fake bundle resource
  path and an app data dir, install copies the bundled binary into
  `macos_layout().bin_path`, marks it executable, and never calls the
  release downloader.
- `macos_install_missing_bundled_binary_is_error` — a missing resource
  gives a clear error string, with no download fallback.
- `macos_autostart_when_unreachable` — the launch decision logic: no
  daemon reachable → install + start; managed and `older` → update +
  restart; managed and `same` or `newer` → no-op; unmanaged (any
  comparison) → no-op plus a banner state. Test the extracted pure
  decision function.
- `wsl_install_still_downloads` — a regression: the WSL2 path still
  resolves release URLs.
- `badge_follows_attention_count_macos` — 0 → 3 → 0 gives set(3), then
  a clear, through the badge-setter abstraction.
- `tray_not_built_on_macos` — the tray-setup gate returns false for
  macOS and true for Windows/Linux. Test it as a pure function of the
  target OS.

### Build / CI checks

- `task-desktop-mac-builds` — `task desktop:mac` on a clean worktree
  produces `smind.app` and a `.dmg`. Record the paths and sizes.
- `bundled-daemon-version-matches` —
  `smind.app/Contents/…/smind --version` equals the app's
  `CFBundleShortVersionString`.
- `info-plist-url-scheme` — `plutil -p Info.plist` shows
  `CFBundleURLTypes` containing `smind`, plus the bundle identifier and
  `LSMinimumSystemVersion` 13.0.
- `ci-macos-both-arches` — the workflow run on the PR uploads arm64 and
  x86_64 DMGs. `lipo -info` on each embedded `smind` binary and app
  executable shows the right arch.

### Manual on this Mac (record in Validation, with screenshots)

- `fresh-install-autostarts-daemon` — stop any daemon and delete
  `<app_data_dir>/managed-daemon`. Launching the app installs and starts
  the daemon and lands in the UI, with no clicks.
- `older-managed-daemon-auto-updated`.
- `unmanaged-daemon-left-alone` — a daemon started from a terminal
  stays running, and the banner offers take-over.
- `close-keeps-dock-reopen` — the red button hides the window, the
  Dock icon brings it back, and `Cmd+Q` quits while the daemon keeps
  running (`curl /healthz`).
- `no-menubar-icon` — the macOS menu bar shows no smind tray icon.
- `dock-badge-on-permission`.
- `deeplink-open` — `open "smind://…"` routes to the task.
- `downloaded-dmg-quarantine` — the CI DMG downloaded via a browser is
  blocked until the documented `xattr` command, then opens.

## Decisions

- **Unsigned / ad-hoc, personal use (user, 2026-10-10).** No Developer
  ID or notarization for now. Add it later behind an opt-in env, the
  way ZCode does (`ZCODE_ENABLE_MAC_SIGN`), if the app gets distributed.
- **Bundled daemon sidecar on macOS (user, 2026-10-10).** This replaces
  the release download for macOS. It is one of the two options ADR-0013
  already lists for macOS native ("a launchd agent or a sidecar"), so no
  new ADR is needed. Copying into the managed layout (M2.2) keeps
  `daemon_manager.rs`'s exe-match and take-over logic unchanged and
  avoids App Translocation paths. Supersedes `desktop-managed-daemon`'s
  download path **for macOS only**.
- **No tray on macOS (user, 2026-10-10).** Use the Dock with reopen
  semantics and a Dock badge, as ZCode does. Windows and Linux are
  unchanged.
- **Per-arch DMGs, not a universal binary.** This is simpler with a
  Go sidecar, because universal would need `lipo` on both the sidecar
  and the app, and it matches Paseo's per-arch artifacts.
- **Coordination with `desktop-native-feel`.** D1 (Sonnet 5.5, branch
  `feat/desktop-chrome`) is editing the window builder in
  `desktop/src-tauri/src/lib.rs` in parallel. This plan's `lib.rs`
  edits are limited to tray gating, `RunEvent::Reopen`, and the
  auto-start hook. Put macOS bundle settings in
  `tauri.macos.conf.json`, not the base config. Whichever PR lands
  second rebases.
- **No backend changes** (standing constraint). The daemon binary is
  built exactly as today.

## Progress

- [ ] M1 — macOS app build (`tauri.macos.conf.json`, `task desktop:mac`, install, README)
- [ ] M2 — bundled daemon sidecar + auto-start/auto-update
- [ ] M3 — Dock lifecycle, no tray, Dock badge
- [ ] M4 — CI workflow + release assets

## Validation

_Not started._
