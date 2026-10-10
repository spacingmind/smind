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

- **Bundle mechanism: Tauri `externalBin` (M2.1).** Proven on a real
  build: the sidecar lands at `smind.app/Contents/MacOS/smind`, next to the
  app executable, is covered by the ad-hoc bundle signature, and is located
  at runtime via `current_exe()`'s directory. tauri-build requires the file
  to exist for *every* macOS cargo build (even `cargo test`), so `build.rs`
  writes an empty placeholder for non-release macOS builds; release builds
  never do, and the install path rejects an empty bundled binary with a
  clear error.
- **Ad-hoc signing is explicit (`signingIdentity: "-"`).** Without it the
  bundle is only linker-signed (Info.plist unbound, identifier not the
  bundle id). `"-"` is not a credential; notarization is still skipped.
- **Auto-start only while the built-in `local` connection is selected.**
  Someone who picked a remote daemon didn't ask for a local one. (Judgment
  call beyond the spec, easy to relax.)
- **Auto-start decision table adds two rows the spec didn't name:**
  unreachable + we own the port (it may still be starting) -> no-op;
  unreachable + something we don't manage holds the port -> banner state,
  never touched.
- **After a successful auto-start the main window is reloaded** (same call
  as View -> Reload). The UI has no auto-retry, so a window that loaded
  before the daemon was serving otherwise sits on "Can't reach Local".
- **macOS notification click uses `mac-notification-sys` directly** (the
  crate the notification plugin's own backend already depends on), since
  the plugin has no click callback on desktop. This is new work: the spec's
  "*still* focuses and navigates" assumed a macOS click path that did not
  exist in `notify.rs`. Each waiter parks an OS thread until clicked or
  dismissed, so waiters are capped at 4 (`should_wait_for_click`, unit test
  `click_waiters_are_capped`); beyond the cap a notification is shown without
  click-to-navigate and the fact is logged.

## Progress

- [x] M1 — macOS app build (`tauri.macos.conf.json`, `task desktop:mac`, install, README)
- [x] M2 — bundled daemon sidecar + auto-start/auto-update (Rust, built and run for real; release-path artifact reuse not yet exercised, see Validation)
- [~] M3 — Dock lifecycle, no tray, Dock badge (implemented and partly verified; four interactions need a human, see "Not verified")
- [x] M4 — CI workflow + release assets (PR run green for both arches; the release-please call path is wired and actionlint-clean but not run)

Commits on `feat/desktop-macos-app` (PR #238): `feat(desktop): macOS app
build` (M1), `feat(desktop): bundled daemon sidecar` (M2), `fix(desktop):
macOS managed daemon actually starts` (three bugs found by running the real
app), `feat(desktop): macOS Dock lifecycle` (M3), `ci(desktop): macOS
workflow` (M4), plus a diagnostics-logging commit.

## Validation

Environment: this Mac (Apple Silicon, macOS 27). It had **no Rust
toolchain and no go-task**; I installed both with `brew install rust
go-task` (rust 1.99, go-task 3.54) and later `actionlint`. All runtime
scenarios ran against an **isolated** `HOME`, `SMIND_HOME` and port
(`/tmp/smind-e2e`, port 4749/4750). The user's `~/.spacingmind`, the real
app-data dir and port 4648 were never touched (4648 was free throughout).
Another agent's (D1) build with the same identifier was running for part of
the session, and its single-instance socket (`/tmp/<identifier>_si.sock`)
would have swallowed my launches, so runtime tests used an `.e2e`
identifier build of the same code (`tauri build --config
'{"identifier":"dev.spacingmind.desktop.e2e",...}'`); the final installed
bundle was then also launched under the real identifier (below).

### Suite results

- `task test` exit 0 (Go all `ok`; web 106 files / 1346 tests passed).
- `task lint` exit 0. `task check:versions` passes.
- `cargo test` in `desktop/src-tauri`: 7 passed (the 6 spec tests plus
  `daemon_start_waits_for_slow_binder`). `desktop/daemon-client`: 183 passed.
  `cargo clippy --all-targets -- -D warnings` clean for `src-tauri`;
  `daemon-client` has 4 pre-existing warnings (`backoff.rs`, `wsl.rs`), none
  in code I touched.
- PR #238 CI: `ci`, `desktop-windows`, `lint-pr-title`, `desktop-macos`
  arm64 and x86_64 all green (run 38042618948).

### Build artifacts (`task desktop:mac`, host arch, from this worktree)

- `desktop/src-tauri/target/aarch64-apple-darwin/release/bundle/macos/smind.app` — 61 MB
- `desktop/src-tauri/target/aarch64-apple-darwin/release/bundle/dmg/smind_0.9.1_aarch64.dmg` — 28 MB (29,182,452 bytes)
- `Contents/MacOS/smind` (bundled daemon) 37.0 MB, `smind-desktop` 25.9 MB, both arm64.
- Full build ~1.5 min warm. CI built the same on a clean macos-latest runner
  (arm64 10m40s, x86_64 6m32s incl. rust cache misses).

`plutil -p Info.plist` (excerpt): `CFBundleIdentifier` =
`dev.spacingmind.desktop`, `CFBundleDisplayName`/`CFBundleName` = `smind`,
`CFBundleShortVersionString` = `0.9.1`, `CFBundleIconFile` = `icon.icns`,
`CFBundleURLTypes[0].CFBundleURLSchemes` = `["smind"]`,
`LSMinimumSystemVersion` = `13.0`, `LSApplicationCategoryType` =
`public.app-category.developer-tools`. `codesign -dv`: `Identifier=
dev.spacingmind.desktop`, `Signature=adhoc`, Info.plist bound, sealed
resources; `codesign --verify --deep --strict` passes.
`Contents/MacOS/smind --version` -> `smind 0.9.1 (334f58c)`.

CI artifacts downloaded and inspected (mounted read-only, not executed):
`smind-desktop-0.9.1-macos-arm64.dmg` (29.2 MB) and
`smind-desktop-0.9.1-macos-x86_64.dmg` (31.2 MB): `lipo -archs` on both the
bundled `smind` and `smind-desktop` = `arm64` / `x86_64` respectively;
version 0.9.1, bundle id, min OS 13.0, `smind` scheme, ad-hoc signature
valid; each DMG has `smind.app` + an `Applications` link.

### Acceptance criteria -> evidence

M1
1. `tauri.macos.conf.json` — file + built output (`app` and `dmg` only,
   min 13.0, category). `tauri.conf.json` / `tauri.windows.conf.json`
   untouched (git diff). **Done.**
2. `task desktop:mac` — ran for real; artifacts above. CI ran the
   equivalent steps on a clean runner. **Done.** (Toolchain caveat above.)
3. `task desktop:mac:install` — ran twice; a stale marker file inside the
   old copy was gone after the second run (replace works); result has no
   quarantine attribute (only the system's `com.apple.provenance`);
   `codesign --verify` OK. I then removed `/Applications/smind.app` (it was
   only installed to test, and none existed before). **Done**, except
   "opens without a Gatekeeper prompt" which I could only observe indirectly:
   the installed app was launched via exec and `open -a` with no dialog
   visible in the screenshot taken right after.
4. Unsigned/ad-hoc, no credentials — `Signature=adhoc`; workflow references
   no secrets; README documents the `xattr` command. **Done.**
5. Name / icon / identifier — plist above; the Dock shows the smind icon in
   the screenshots. The About *panel* needs a menu click: **not verified**.
6. `smind://` — in plist (also in the CI-built DMGs); `open -a smind.app
   "smind://task/42"` routed to `deeplink.rs` -> navigation log
   `task 42, workspace Some(7)`, and `smind://workspace/9/task/77` ->
   `workspace Some(9)` (e2e build; also confirmed on the installed
   real-identifier app: `task 5, workspace Some(3)`). **Done.**
7. `task check:versions` passes; the new config has no version field. **Done.**

M2
1. Bundled at `Contents/MacOS/smind`; `smind --version` 0.9.1 ==
   `CFBundleShortVersionString`. **Done.**
2. Copy into `<app_data_dir>/managed-daemon/bin/smind` — unit tests
   `macos_install_sources_bundled_binary`,
   `macos_install_missing_bundled_binary_is_error`; live: `cmp` shows the
   installed daemon byte-identical to the app's sidecar; path with spaces
   works (see bug 1 below). WSL: `wsl_install_still_downloads`. **Done.**
3. First launch, nothing reachable -> installs and starts with no clicks,
   record saved, UI reloaded to "Connected to daemon" (screenshot
   `03-fresh-reload.png`; before the reload fix the same run sat on "Can't
   reach Local"). Verified on both the e2e and real-identifier bundle.
   **Done**; the daemon banner / Settings -> Daemon / Restart button were
   not exercised in the UI (Restart's start path now uses
   `wait_for_healthz`, covered only by the unit test and the shared code).
4. Older managed daemon (a real 0.8.0 build, recorded in `managed.json`) was
   replaced on launch: pid 67796 -> 68113, `/healthz` 0.8.0 -> 0.9.1, record
   and binary updated. Unmanaged daemon started from a "terminal" path was
   left alone in both cases: same version (`Unmanaged, Same`) and older
   (`Unmanaged, Older`) — same pid after launch, no managed record written;
   the UI showed the plain notice "Daemon v0.8.0 is older than this app
   (v0.9.1)" (screenshot `09-unmanaged-older.png`). **Done**, with a spec
   wording note: the existing banner only renders when the daemon is
   *older*, and for an unmanaged one it is a plain, non-actionable notice;
   take-over lives in Settings -> Daemon (unchanged, not opened here). The
   `DaemonProgress` events are emitted by the same code, but I did not
   capture them rendering during an auto-start.
5. Quitting (Apple-event quit) left the daemon running (`/healthz` ok, same
   pid); relaunching logged `NoOp (reachable=true, Managed, Same)` and the
   pid did not change. **Done** (Cmd+Q / Dock -> Quit are the same terminate
   path but were not physically clicked).
6. `desktop-macos.yml` reuses the release run's per-arch daemon artifact
   when called with `daemon-version`; the PR path builds the daemon with
   `task desktop:mac:daemon` and CI's "Verify bundle" step asserted the
   daemon reports the app version (arm64) and the right arch for both.
   **Partly verified:** the reuse path is only reachable from
   `release-please.yml`, which I did not run (see Not verified).

M3
1. No tray on macOS — `tray_not_built_on_macos`; menu-bar screenshots with
   (D1's tray build, teal icon at 16:45) and without (my app only, 16:56)
   the icon. Windows/Linux: unchanged code path, compiled on Windows by the
   `desktop-windows` CI job; not run there. **Done on macOS.**
2. Hide on close / reopen / quit — `RunEvent::Reopen` is delivered and
   handled (an `open -a` on the running app logs `dock reopen
   (has_visible_windows=true)` and the window comes to front); Apple-event
   quit really quits. The red button, `Cmd+W` and a physical Dock click
   could **not** be driven (this session has no accessibility permission).
   The hide-on-close handler itself is pre-existing, unchanged code.
   **Partly verified.**
3. Dock badge — `badge_follows_attention_count_macos` (set(3), then clear);
   live: a fake daemon pushing 3 `permission.pending` events made the app
   call `set_badge_count` 1, 2, 3 with `Ok(())`. **But the Dock did not
   draw the badge.** A plain bundled probe app shows its badge; the same
   probe stops showing it once it has posted a notification and the user
   has not answered macOS's "Notifications may include alerts, sounds, and
   icon badges" prompt. So on this macOS the badge appears only after the
   user clicks **Allow** on smind's first notification prompt. **Not
   verified visually; needs user.**
4. Notification click — delivery works (macOS raised the first-run
   notification-permission prompt for the app); click handler wired to the
   shared `navigate_to_task`, which the deep-link test above exercised.
   A real click needs a human: **not verified, needs user.**
5. Global shortcut — registration happens in setup with `?`, and the app
   started, so it registered; the toggle itself was not pressed, and a
   second-process `RegisterEventHotKey` probe was inconclusive (duplicates
   are allowed across processes). **Not verified, needs user.**

M4
1. `desktop-macos.yml` — PR run: both arches built, uploaded
   `smind-desktop-macos-arm64` / `-x86_64` artifacts named as specified;
   `lipo` checks in the job and re-done locally on the downloaded DMGs.
   **Done.**
2. `release-please.yml` calls it after `build-binaries`, adds it to
   `publish`'s `needs`/`if`, and adds `*.dmg` to checksums, artifact and
   release-upload globs. actionlint-clean. **Wired, not run end to end.**
3. No secrets referenced; workflow comment says signing/notarization are
   out of scope. **Done.**

### Test scenarios

Rust: all six spec-named tests pass (`macos_install_sources_bundled_binary`,
`macos_install_missing_bundled_binary_is_error`,
`macos_autostart_when_unreachable`, `wsl_install_still_downloads`,
`badge_follows_attention_count_macos`, `tray_not_built_on_macos`), plus
`daemon_start_waits_for_slow_binder` (src-tauri) and
`parse_ps_comm_keeps_spaces_in_the_path` (daemon-client).
Build/CI: `task-desktop-mac-builds`, `bundled-daemon-version-matches`,
`info-plist-url-scheme`, `ci-macos-both-arches` — done (above).
Manual: `fresh-install-autostarts-daemon`, `older-managed-daemon-auto-updated`,
`unmanaged-daemon-left-alone`, `no-menubar-icon`, `deeplink-open` — done;
`close-keeps-dock-reopen` — partial (reopen + quit + daemon survives
verified, red button / Dock click not); `dock-badge-on-permission` — setter
verified, Dock rendering blocked by the notification-permission prompt;
`downloaded-dmg-quarantine` — not verified.

### Bugs found by running the real app (fixed in `286e35d`)

1. `ps -o args=` was split on whitespace, but the managed binary lives under
   `~/Library/Application Support/…`, so the managed-binary identity check
   could never match on a real Mac: the whole macOS managed-daemon flow
   (existing since `desktop-managed-daemon`) had only ever been exercised
   with Linux temp dirs. Now `ps -o comm=` on macOS.
2. A fixed 500 ms sleep after spawning was shorter than a first start, so no
   managed record was saved and a healthy daemon looked unmanaged. Now a
   bounded poll of `/healthz` (~10 s).
3. The window loaded before the daemon was up and stayed on "Can't reach
   Local" (no UI auto-retry): the auto-start now reloads the window.

### Not verified, needs user

- **Red button / `Cmd+W` hides, Dock click shows, `Cmd+Q` and Dock -> Quit
  quit; `Cmd+Shift+S` toggles.** (~30 s: open the built app, try each.)
- **Dock badge and notification click.** On the first permission request
  macOS shows a notification-permission prompt for smind; click **Allow**,
  then trigger a permission request: the Dock should show the count, and
  clicking the notification should focus the window and open the task,
  including after closing the window. (While testing I left two pending
  prompts, "smind-e2e" and "probe2", in Notification Center; dismiss them.)
- **Light-theme screenshot.** Only dark was captured: the web UI ignores a
  per-process appearance override and switching theme needs a click.
- **Downloaded-DMG quarantine flow** (`downloaded-dmg-quarantine`): download
  the CI DMG in a browser, confirm Gatekeeper blocks, run the documented
  `xattr -dr com.apple.quarantine`, confirm it opens.
- **Release path** (M2.6 reuse of the release daemon artifact, M4.2): a
  `workflow_dispatch` dry run of `release-please.yml` on this branch
  (`gh workflow run release-please.yml --ref feat/desktop-macos-app`) would
  exercise it and publish only workflow artifacts, never a Release. My
  attempt to start it was denied by the session's permission classifier, so
  I did not run it.
- Settings -> Daemon, the daemon banner's Restart, and take-over were not
  opened in the UI.

### Notes for D1 / D3 (not changed by this plan)

- The Window menu item reads "Hide" on macOS (was "Hide to Tray"); other platforms unchanged. D3 still owns the rest of the macOS menu.
- `tauri_plugin_log` runs at its default (TRACE) level, so the log file
  rotates away INFO lines within seconds of a connection being open;
  this made the app's own diagnostics hard to read. Pre-existing.
