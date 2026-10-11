# Phase 4, Item 1: Tauri desktop thin-client shell

## Context

`docs/ROADMAP.md` Phase 4: "Tauri 2 wrapper: tray icon, global shortcuts,
native notifications, auto-update; complete native-call abstraction
layer; full settings UI." Nothing desktop exists yet — no `desktop/`
directory, no Tauri config.

**User decisions (2026-09-24), not to be re-litigated:**

1. **Thin client first, sidecar later.** The app connects to an
   already-running daemon at `http://127.0.0.1:4648` (the user runs
   `smind serve` inside WSL2; Windows reaches it through WSL localhost
   forwarding). Bundling the daemon as a Tauri sidecar is a later item.
2. **Load the UI from the daemon**, not bundled into the app: the main
   webview opens the daemon's own web UI, so UI and API never drift in
   version and the daemon needs no changes.
3. **Local toolchain.** `rustup` is installed (`rustc 1.98.1`,
   `~/.cargo/bin`). Linux system deps for Tauri (`libwebkit2gtk-4.1-dev`,
   `libayatana-appindicator3-dev`, `libxdo-dev`, `libssl-dev`,
   `librsvg2-dev`, `pkg-config`, `build-essential`) are being installed
   by the user via `sudo apt` — **never run `sudo` yourself**. Check with
   `pkg-config --modversion webkit2gtk-4.1`; if still missing, do all the
   code/scaffolding work anyway and report the build as blocked on deps.
   GUI testing on WSL goes through WSLg.

**Research (pplx, this session) — the facts this plan relies on:**

- A remote URL loaded in a Tauri 2 webview gets **no IPC** unless a
  capability lists it under `remote.urls`. So the design below drives
  every native feature **from the Rust side**, and grants the remote
  content no plugin permissions at all.
- Official pieces: the `tray-icon` feature of `tauri` itself (Linux tray:
  right-click menu works, no hover/move events),
  `tauri-plugin-global-shortcut`, `tauri-plugin-notification`,
  `tauri-plugin-single-instance` (initialize it first, before tray), plus
  `tauri-plugin-updater` / `-autostart` (both out of scope here).
- Daemon auth, from `internal/server/server.go`: `GET /api/token` returns
  `{"token": ...}` unauthenticated (same-origin by design); `GET
  /ws?token=...` is the JSON-RPC WebSocket. Event subscription is
  `events.subscribe` with topics like `permission.pending` whose payload
  is `{runId, taskId, requestId, summary, options}`
  (`internal/wsapi/events.go`). **Before writing the Rust WS client,
  check whether `internal/wsapi`'s handler enforces an `Origin` check**
  that a non-browser client would fail — if it does, report it rather
  than weakening the daemon's check.

## Decisions

- New top-level `desktop/` directory, standalone like `mobile/` (not in
  the `web/` bun workspace). Tauri CLI via an npm devDependency
  (`@tauri-apps/cli`) is fine — no bundled frontend beyond a tiny local
  fallback page.
- Native features are driven by a **Rust-side daemon client**: fetch
  `/api/token`, open `/ws`, `events.subscribe` to `permission.pending`,
  and turn events into OS notifications. The webview only displays the
  daemon's UI.
- The remote-URL capability, if any is needed at all, is scoped to
  exactly `http://127.0.0.1:4648/*` with **no** plugin permissions.
- Record the architecture as **ADR-0012** (`docs/decisions/0012-desktop-
  thin-client.md`, same format as the existing ADRs — see the `adr`
  skill): thin client first, UI loaded from the daemon, native features
  from a Rust-side event subscription, sidecar mode deferred.
- Out of scope for this item: auto-update (needs signing keys + release
  infra), autostart, sidecar mode, the native-call abstraction layer,
  the settings UI, Windows/macOS packaging and CI.

## Acceptance Criteria

- **AC1** `desktop/` is a Tauri 2 app whose main window loads the daemon
  URL (default `http://127.0.0.1:4648`, overridable by an env var such as
  `SMIND_DAEMON_URL`). When the daemon is unreachable it shows a local
  fallback page ("smind daemon not reachable at … — retrying") instead of
  a blank/error page, and switches to the real UI once the daemon answers
  `GET /healthz`.
- **AC2** Single instance: a second launch focuses the existing window.
- **AC3** Tray icon with a menu (Open / Quit). Closing the window hides
  it to the tray; only Quit exits.
- **AC4** A `permission.pending` event from the daemon shows a native OS
  notification with the request's `summary`. The Rust client reconnects
  with backoff when the daemon restarts, and resubscribes.
- **AC5** A global shortcut (e.g. `CommandOrControl+Shift+S`) toggles the
  main window's visibility.
- **AC6** ADR-0012 written.

## Test Scenarios

- Rust unit tests (`cargo test` in `desktop/src-tauri`) for the pure
  logic: parsing a `permission.pending` event notification into a
  notification title/body; the reconnect backoff schedule (grows, caps,
  resets after a stable connection); resolving the daemon URL
  (default vs env override).
- `cargo check` / `cargo build` (or `npx tauri build --debug`) succeeds
  on Linux once system deps are present.
- Manual (WSLg): window loads the daemon UI; stop the daemon → fallback
  page → restart → UI returns; tray Open/Quit work; close hides to tray;
  shortcut toggles; a real permission request from a running task shows
  a notification. Record what was actually exercised in Validation.

## Progress

- [x] AC1 window + daemon URL + fallback page (`desktop/src-tauri/src/lib.rs`,
  `desktop/ui/offline.html`; window starts on the local page, a
  /healthz poll loop navigates to the daemon URL once it answers)
- [x] AC2 single instance (tauri-plugin-single-instance, registered
  first, focuses the existing window)
- [x] AC3 tray + hide-on-close (tray menu Open/Quit via
  TrayIconBuilder; CloseRequested is prevented and hides the window)
- [x] AC4 permission.pending notifications + reconnect
  (`desktop/daemon-client`: fetch /api/token, /ws?token=...,
  events.subscribe, notification from payload.summary; exponential
  backoff resetting after 30s of stability; resubscribes each
  reconnect)
- [x] AC5 global shortcut (CommandOrControl+Shift+S toggles the main
  window via tauri-plugin-global-shortcut)
- [x] AC6 ADR-0012 (`docs/decisions/0012-desktop-thin-client.md`)

## Validation

- **Unit tests pass**: `cargo test` in `desktop/daemon-client` — 16
  tests green (URL resolution default/env-override/invalid, ws/token/
  healthz URL derivation, event-notification parsing incl.
  permission.pending title/body from payload.summary, malformed frames,
  other topics, missing payload fields, backoff grow/cap/reset/
  saturation, subscribe-message shape).
- **wsapi Origin check investigated (finding)**: `internal/wsapi/
  server.go` uses gorilla's default upgrader `CheckOrigin`, which
  admits requests with no `Origin` header — exactly what the
  non-browser Rust client sends. Auth is only the `token` query param.
  No daemon change was needed and none was made.
- **Build**: `cargo check` + `cargo build` pass in
  `desktop/src-tauri` on Linux with webkit2gtk-4.1 2.52.6 / dbus-1
  1.16.2 / libxdo / appindicator installed. Two compile-time bugs were
  found and fixed against the real APIs: `tauri-build` has no
  `tray-icon` feature (only the `tauri` crate does), and tokio had to
  be a direct dep of src-tauri for the healthz poll loop.
- **Live run (WSLg, X11 :0)**: app launches without panicking (window
  + tray up; only WSLg-typical libEGL/Mesa warnings and a benign
  libayatana-appindicator deprecation notice in stderr). Against a
  locally started daemon (`smind serve` on 4648): logs show
  `ws connected to ws://127.0.0.1:4648/ws?token=...`,
  `subscribed to permission.pending`, and
  `/healthz ok, switching to daemon UI` — verifying token fetch, WS
  connect, subscribe, and the AC1 URL switch. A live-daemon bug was
  caught and fixed here: tungstenite rejects `http://` upgrade URLs,
  so `ws_url` now translates to `ws://`/`wss://` (unit-tested).
- **Reconnect/backoff**: a first run (daemon temporarily unreachable)
  retried with visibly growing spacing and no crash; the
  reconnect/backoff logic is otherwise unit-tested.
- **Fallback path**: with `SMIND_DAEMON_URL=http://127.0.0.1:4699`
  (nothing listening; real daemon untouched) the app stays on the
  local offline.html page, retries `/api/token` with backoff
  (4 attempts in 15s), no panic.
- **Single instance (AC2)**: first instance running; second launch
  exits 0 immediately with an empty log — it deferred to the first
  instance's focus callback.
- **Screenshot**: no screenshot tool available in this environment
  (no `import`/`grim`/`scrot`/`spectacle`), so no image captured.
- **Not yet exercised (needs human eyes, WSLg)**: tray Open/Quit
  clicks; close-hides-to-tray; CommandOrControl+Shift+S toggle; a real
  permission request showing its notification; the daemon UI visibly
  rendering in the window (the healthz switch is logged but not
  screenshotted). Note: the tray needs an actual desktop session to
  fully work — WSLg supports it.

## Windows build (native, user decision 2026-09-24)

The user picked a native build on Windows over CI or cross-compiling from WSL.

Prep committed on this branch:
- Real icon set generated with `npx tauri icon ../mobile/assets/icon.png`, including `icon.ico` (1024px source).
- `src-tauri/tauri.windows.conf.json` sets the bundle targets to `nsis` + `msi`, with the WebView2 `downloadBootstrapper`. Tauri merges this file only on Windows, so Linux still builds `deb`.
- `cargo check` on Linux still passes.

Steps on Windows (PowerShell):

1. Install the toolchain once:
   `winget install Rustlang.Rustup Git.Git OpenJS.NodeJS.LTS`, then
   `winget install Microsoft.VisualStudio.2022.BuildTools --override "--wait --passive --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"`.
   WebView2 already ships with Windows 10/11.
   After install, open a new shell and run `rustup default stable-msvc`.
2. Clone onto the Windows filesystem, not `\\wsl$`, which is slow and breaks file watching:
   `git clone -b feat/desktop-tauri-shell https://github.com/spacingmind/smind C:\src\smind`.
3. Build:
   `cd C:\src\smind\desktop; npm install; npx tauri build`.
   - Outputs: `src-tauri\target\release\bundle\nsis\smind_0.1.0_x64-setup.exe` and `...\bundle\msi\smind_0.1.0_x64_en-US.msi`.
   - Tauri downloads WiX/NSIS itself.
   - On Windows 11 24H2+, the WiX (MSI) step needs the optional VBScript feature. If only the MSI step fails, `npx tauri build --bundles nsis` still produces the `.exe`.
4. Run: start `smind serve` inside WSL (127.0.0.1:4648). The Windows app reaches it through WSL localhost forwarding. `SMIND_DAEMON_URL` overrides the address.
5. Manual check list (AC1–AC5 on Windows):
   - the daemon UI loads;
   - the fallback page shows while the daemon is stopped;
   - tray Open/Quit;
   - closing the window hides it to the tray;
   - Ctrl+Shift+S toggles the window;
   - a permission request shows a toast;
   - a second launch focuses the running window.

The build is unsigned, so SmartScreen will warn ("More info" → "Run anyway"). Code signing is out of scope.
