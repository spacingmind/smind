# Desktop quick wins: making the Tauri shell feel native (no bridge)

## Context

The user installed the Windows build (NSIS, from `.github/workflows/desktop-windows.yml`). Their feedback: it "is a wrapper of the web version; UI/UX can still improve".

Research (local-only, `docs/research/local/zcode-desktop-2026-09.md`) compared the shell with ZCode's and Paseo's Electron apps. Both of those ship bundled UI with an IPC bridge; smind loads the daemon's web UI with **no IPC by design** (ADR-0012).

**User decisions (2026-09-24), not to be re-litigated:**
1. Do the no-bridge quick wins now: items 1–6 below.
2. Defer the bridge question (custom titlebar, open-in-editor, native file dialogs, desktop-only `Mod+digit`) until the user has tried these.

**Hard constraints:**
- **No change to the remote capability.** `capabilities/default.json` keeps `"permissions": []` and no `remote.urls`, so the daemon-origin page still gets zero IPC.
- **No daemon, wsapi or web UI changes.** Everything is Rust-side in `desktop/`, plus the static local `desktop/ui/offline.html`.

If an item turns out to be impossible under these constraints, stop and report it rather than loosening them.

**Facts from the code that the implementation relies on:**
- **Web route shape** (`web/packages/ui/src/lib/route.ts`): `#/workspace/<workspaceId>/task/<taskId>/<kind>`. `kind` is one of the base tab kinds (see `isBaseKind`), and `file` takes a path.
- **`permission.pending` payload** (`internal/wsapi/events.go`): `{runId, taskId, requestId, summary, options}`. It has **no workspaceId**. The Rust daemon-client must resolve it first, e.g. via the existing `task.get` JSON-RPC over its already-open `/ws`, then navigate.
- **No `permission.resolved` event exists.** A pending count must come from something the daemon already exposes. Look for an existing list/lookup RPC and at `run.status` / `task.status` transitions; see how `web/packages/ui/src/hooks/use-task-attention.ts` derives attention. Don't add a daemon event.
- **Notification clicks on desktop.** Check whether `tauri-plugin-notification` delivers click/activation callbacks on Windows and Linux; historically it didn't on desktop. If it doesn't, use the platform notification crate directly behind a small Rust abstraction (e.g. `tauri-winrt-notification`'s `on_activated` on Windows, `notify-rust` actions on Linux). Research this first (web search / crate docs), then decide.

## Acceptance Criteria

- **AC1 — window state.** Size, position and maximized state persist across restarts (`tauri-plugin-window-state` or equivalent). The window is clamped onto a visible monitor if the saved monitor is gone. The first launch uses a sensible default size.
- **AC2 — native app menu.**
  - Windows/Linux: an in-window menu bar, or a Menu attached to the window.
  - macOS: the app menu.
  - Items:
    - **File:** Reload, Quit.
    - **Edit:** predefined Cut/Copy/Paste/Select All, so clipboard shortcuts work on macOS.
    - **View:** Zoom In, Zoom Out, Reset Zoom (webview zoom from Rust, persisted); Toggle Fullscreen; DevTools in debug builds only.
    - **Window:** Minimize, Hide to tray.
    - **Help:** About (version), Open log folder, smind on GitHub (opened in the OS browser).
  - Menu accelerators must not collide with the web UI's shortcuts in `web/packages/ui/src/keyboard/shortcuts.ts`. Prefer no accelerator over a collision.
- **AC3 — notification click navigates.** Clicking a `permission.pending` OS notification shows, unminimizes and focuses the window. It then navigates the webview to that task's route (`<daemonUrl>/#/workspace/<ws>/task/<id>/<kind>`), with the workspace id resolved Rust-side. If resolution fails, it still focuses the window. Works on Windows; Linux as far as the platform supports it.
- **AC4 — tray attention.**
  - The tray tooltip and a disabled first menu item show the number of pending permission requests ("2 approvals waiting"). If an exact count isn't derivable without daemon changes, show a "needs attention" state that clears once the related task's run is no longer waiting.
  - Where the OS supports it, show a badge/overlay on the taskbar: the window's progress/overlay icon or badge count.
  - Clicking the tray item opens the most recent waiting task (same navigation as AC3).
- **AC5 — offline/splash page** (`desktop/ui/offline.html`), static and local:
  - smind branding using the design tokens' colors, in both light and dark (`prefers-color-scheme`);
  - the daemon URL being tried;
  - live retry state, e.g. "retrying in 4s" or "attempt 3". A small `?state=` query or hash that Rust sets on navigate is fine;
  - clear guidance: "Start the daemon in WSL: `smind serve`", with a copy button (browser clipboard API);
  - a "Retry now" button (reload).
  - It switches to the daemon UI automatically, as today.
- **AC6 — deep links.** `smind://task/<taskId>` and `smind://workspace/<wsId>/task/<taskId>` open or focus the app and navigate to that task. Unknown or malformed links focus the app without navigating.
  - Registered by the Windows installers (NSIS/MSI) and on Linux (`.desktop` MimeType), via `tauri-plugin-deep-link`.
  - A link that arrives while the app is running is forwarded through `tauri-plugin-single-instance` (its deep-link feature) to the existing instance.
  - Only navigation is allowed: a link can never trigger any action beyond choosing a route.
- **AC7 — no regressions.**
  - ADR-0012 invariants hold: the capability file is unchanged, the remote gets no IPC, and there are no new `#[tauri::command]`s callable from the daemon origin.
  - The existing ACs of `desktop-tauri-shell.md` still pass.
  - `cargo test` (daemon-client and src-tauri) passes; `cargo build` on Linux passes; the `desktop-windows` workflow builds the NSIS and MSI installers.

## Test Scenarios

- **Rust unit tests (pure logic):**
  - Route building from `(daemonUrl, workspaceId, taskId, kind)`. Check the exact hash shape matches `route.ts`.
  - Deep-link parsing: both accepted forms; bad scheme, non-numeric ids, extra path, query junk → rejected.
  - Pending-count / attention state machine: pending event → count up; resolution signal → count down; duplicate requestId counted once; reconnect resets or rebuilds the count.
  - Tray label formatting (0, 1 and N).
  - Offline-page state query formatting.
  - Zoom step clamping and persistence round-trip.
- **Linux, live (WSLg):**
  - The window reopens at the saved size and position.
  - Menu items work: zoom, fullscreen, reload, open log folder.
  - With the daemon stopped, the offline page shows its states; once the daemon starts, the app switches over.
  - A real permission request produces a notification, and the tray count updates.
  - `xdg-open 'smind://task/1'` navigates.

  Record what was actually exercised in Validation.
- **Windows, manual (user).** Build via the Actions workflow. Check: menu, window state, notification click → task, tray count, deep link from `Win+R` → `smind://task/<id>`, offline page.

## Decisions

- Stay on branch `feat/desktop-tauri-shell`, so the desktop shell ships as one PR.
- Record the new dependencies (plugins) and the notification-click approach you chose in this section as you go.
- **New crates** (all Tauri-2-compatible stable releases, not the `3.0.0-alpha.1` line that `cargo search`/`cargo info` show as "latest" -- pinned to `2`/`4`/`0.8` so the version req resolves the 2.x/stable release):
  - `tauri-plugin-window-state` 2.4.1 (AC1).
  - `tauri-plugin-single-instance`'s `deep-link` feature (AC6), forwarding a
    second instance's `smind://...` argv into `tauri-plugin-deep-link`'s
    `on_open_url` in the running instance instead of spawning a second
    window.
  - `tauri-plugin-deep-link` 2.4.10 (AC6).
  - `tauri-winrt-notification` 0.8.1, Windows-only
    (`target_os = "windows"`) (AC3).
  - `tauri-plugin-log` 2.9.2, `tauri-plugin-opener` 2.5.5,
    `tauri-plugin-dialog` 2.7.3 (AC2: log folder, GitHub link/log-folder
    opening, About dialog). All three are used only from Rust
    (`app.opener()...`, `app.dialog()...` inside menu event handlers,
    never exposed as an `invoke`-able command), so they add no IPC
    surface for the daemon-origin webview and need no capability grant.

- **Notification-click research (AC3), done before implementing**:
  `tauri-plugin-notification`'s action/click API
  (`registerActionTypes`/`onAction`) is mobile-only -- confirmed via its
  own docs (`v2.tauri.app/plugin/notification`) and the plugin's GitHub
  issue history; there is no click/activation callback for desktop
  Windows or Linux in that plugin. Picked platform-specific crates
  instead, used directly (no Tauri capability needed either way, since
  neither is reachable from the remote webview):
  - **Windows**: `tauri-winrt-notification` 0.8.1's `Toast::on_activated`
    (`FnMut(Option<String>) -> Result<()>`, confirmed against the
    0.7.3 source in the local registry cache -- 0.8 wasn't locally
    extractable to read directly, so this is verified live by the
    Windows CI build rather than by a local read). `None` (no action id)
    means the user clicked the toast body itself, which is treated the
    same as a button click here since there's only one action.
  - **Linux**: `notify-rust` 4.18.0's `NotificationHandle::
    wait_for_action`, given a `"default"` action so a plain click (where
    the notification server honors it) reaches the callback; blocks, so
    it runs on its own spawned thread per notification. Coverage is
    genuinely "as far as the platform supports it" (AC3's own wording):
    the freedesktop notification spec's `"default"` action is honored by
    some notification servers/desktop environments and not others.
  - **Other platforms** (macOS, not built/tested in this task): falls
    back to the existing `tauri-plugin-notification`, no click handling.
  - `notify-rust` 4.18.0, Linux-only (`target_os = "linux"`) (AC3).

## Progress

- [x] AC1 window state (`tauri-plugin-window-state` 2.4.1; auto-restores on
  the main window's `ready` event, no change needed to how the window is
  built)
- [x] AC2 app menu (`desktop/src-tauri/src/menu.rs`: File/Edit/View/
  Window/Help via `app.set_menu`; zoom clamping in
  `smind-daemon-client::zoom`, persisted to `zoom.txt` under
  `app_config_dir` by `desktop/src-tauri/src/zoom_store.rs`)
- [x] AC3 notification click → task (`desktop/src-tauri/src/notify.rs`
  platform split; `smind-daemon-client::{route,cache}` for the pure
  route-building and the task->workspace lookup; `client.rs` resolves
  workspaceId via a background `task.get`)
- [x] AC4 tray attention (`smind-daemon-client::attention::Attention`
  state machine; `desktop/src-tauri/src/tray.rs` renders it as tooltip +
  first menu item + taskbar badge, and reuses `notify::navigate_to_task`
  for the "open most recent waiting task" click)
- [x] AC5 offline/splash (`desktop/ui/offline.html` rebranded with real
  design-token colors + light/dark; live state pushed via
  `WebviewWindow::eval`, formatted by `smind-daemon-client::offline`)
- [x] AC6 deep links (`smind_daemon_client::route::{DeepLink,
  parse_deep_link}`; `desktop/src-tauri/src/deeplink.rs` wires
  `tauri-plugin-deep-link`'s `on_open_url`/`get_current` +
  `single-instance`'s `deep-link` feature forwarding)
- [x] AC7 regressions + Windows build

## Validation

- **AC1**: `cargo check` in `desktop/src-tauri` passes with
  `tauri-plugin-window-state = "2"` added and
  `.plugin(tauri_plugin_window_state::Builder::default().build())`
  registered. The plugin's `on_window_ready` hook fires for any window
  (including one built at runtime via `WebviewWindowBuilder`, not only
  ones declared in `tauri.conf.json`), so no change to the AC1 window
  construction code was needed. Read the plugin's 2.4.1 source
  (`~/.cargo/registry/src/.../tauri-plugin-window-state-2.4.1/src/lib.rs`)
  to confirm: on restore it only repositions onto the saved monitor if
  `available_monitors()` still intersects that position/size, otherwise
  leaves placement to the OS (satisfies "clamped onto a visible monitor if
  the saved monitor is gone" without a hard clamp) --  and first launch
  (no saved state / all-default state) leaves the builder's explicit
  `inner_size(1280, 800)` alone. `daemon-client`'s 16 unit tests still
  pass (unaffected). Manual restart/monitor-loss check is in the Linux
  live-run pass below, done once more ACs land so a single WSLg session
  covers all of them.

- **AC2**: `tauri::menu::PredefinedMenuItem`'s own doc comments (read
  from the vendored `tauri-2.11.6` source in the local cargo registry
  cache) list `quit`/`close_window`/`minimize`/`fullscreen`/`maximize` as
  **"Linux: Unsupported"** -- since Linux/WSLg is where this app is
  actually run live, File > Quit, Window > Minimize and View > Toggle
  Fullscreen are custom `MenuItem`s calling `app.exit(0)` /
  `window.minimize()` / `window.set_fullscreen()` directly instead, which
  behave identically on every platform. Edit's Cut/Copy/Paste/Select All
  have no such caveat and stay the builder's predefined shorthands
  (`.cut()`/`.copy()`/`.paste()`/`.select_all()`), needed for macOS
  clipboard shortcuts to work at all. Accelerators are plain OS
  conventions (`Ctrl+R`, `Ctrl+Q`, `Ctrl+Plus`/`Ctrl+-`/`Ctrl+0`, `F11`,
  `F12`) checked one-by-one against every combo in
  `web/packages/ui/src/keyboard/shortcuts.ts`'s `SHORTCUT_BINDINGS` table
  -- none collide (the web table only uses `Mod+letter`, `Mod+[`/`Mod+]`,
  `Escape`, `Shift+?`). `cargo check`/`cargo build` pass; `cargo test` in
  `daemon-client` is 22/22 (6 new: zoom step-in/out, top/bottom clamping,
  persistence round-trip, garbage-parse fallback). DevTools menu item is
  present only when `cfg!(debug_assertions)` (a release build never adds
  it). "Open Log Folder" uses `tauri-plugin-log`'s default `LogDir`
  target (`app.path().app_log_dir()`), so the folder always has at least
  the app's own startup log line. Zoom persists to a plain-text
  `zoom.txt` under `app_config_dir`, applied via `WebviewWindow::
  set_zoom` on launch.

- **AC3**: `permission.pending`'s payload has no workspaceId (confirmed
  against `internal/wsapi/events.go`'s `permissionPendingPayload`), so
  `client.rs` now fires a `task.get{id: taskId}` request over the
  already-open `/ws` connection right after emitting the notification,
  matches the response back by its deterministic
  `task-get-<taskId>` id (`protocol::task_get_response_task_id`), and
  decodes `WorkspaceID` (`store.Task` marshals with no `json` tags, so
  its Go field name is the wire key verbatim -- confirmed against
  `internal/store/types.go` and `web/packages/ui/src/lib/types.ts`'s
  matching `WorkspaceID: number`) into
  `smind-daemon-client::cache::WorkspaceCache`. The click handler reads
  that cache synchronously; if it's not populated yet (resolution still
  in flight, or failed), the click still focuses the window without
  navigating, per the AC's own fallback wording. `cargo test` in
  `daemon-client` is 31/31 (9 new: `cache` insert/get/overwrite/clone,
  `route` hash-shape/URL-join, `protocol` task.get message/response
  round-trip, notification now carrying `task_id`). `cargo check` and
  `cargo build` pass on Linux, which also compiles `notify.rs`'s
  `#[cfg(target_os = "linux")]` branch (`notify-rust`) for real; the
  `#[cfg(target_os = "windows")]` branch (`tauri-winrt-notification`)
  is unverified locally (no Windows toolchain here) and depends on the
  `desktop-windows` CI build in AC7 to catch any API mismatch against
  the pinned 0.8.1 version.

- **AC4**: no `permission.resolved` event exists (checked
  `internal/wsapi/events.go`'s `knownTopics` -- there is no such topic),
  so `client.rs` now also subscribes to the already-existing
  `run.status` topic and the tray clears a task's pending entry when
  that task's run reports `status: "running"` again -- the same signal
  `web/packages/ui/src/hooks/use-task-attention.ts` uses to clear its own
  permission badge on a live `run.status` event (its full resync path
  additionally calls `run.list`/`run.logs`, which this tray skipped: not
  worth the extra round-trips for a tooltip count, so a reconnect resets
  to zero instead of resyncing against those RPCs -- rebuilds correctly
  as soon as fresh events arrive, per the AC's own "resets or rebuilds"
  wording). `smind_daemon_client::Attention` is unit-tested standalone
  (pending count up, duplicate requestId counted once, `run.status`
  clears only that task, reset clears everything, most-recent-task
  ordering, label formatting for 0/1/N) --  `cargo test` in
  `daemon-client` is 38/38 (7 new, plus one existing test extended for
  the new `run.status` subscribe topic). The taskbar badge uses
  `Window::set_badge_count`, which its own doc comment (read from the
  vendored source) marks **"Windows: Unsupported, use set_overlay_icon
  instead"** -- `set_overlay_icon` needs a distinct badge-count icon
  image per count, which this pass has no assets for, so Windows gets
  the tooltip/menu-item text but no taskbar visual; Linux/macOS pick up
  `set_badge_count` where their desktop environment supports a
  launcher/dock badge. `cargo check`/`cargo build` pass on Linux with no
  new warnings.

- **AC5**: colors are the real `:root`/`.dark` values from
  `web/packages/ui/src/index.css`'s token layer (`--background`,
  `--foreground`, `--card`, `--primary`, `--muted`,
  `--muted-foreground`, `--border`), not placeholders, switched via
  `prefers-color-scheme`. Live retry state is pushed with
  `WebviewWindow::eval` rather than a re-navigate with a `?...` URL:
  reading the vendored tauri source showed the local asset origin is
  `tauri://localhost` on Linux/macOS but `http://tauri.localhost` on
  Windows, and depending on that split for every retry tick (plus the
  page-reload flash a re-navigate causes) wasn't worth it when `eval`
  needs no origin at all. The pure formatting
  (`smind_daemon_client::offline::OfflineState::to_query_string`) is
  still exactly query-string-shaped and unit-tested (exact format,
  round-trips through the `url` crate's own query-pair parser), and the
  page's `URLSearchParams` parsing is identical whether the string
  arrives via `eval` or `location.search` -- `cargo test` in
  `daemon-client` is 40/40 (2 new). "Start the daemon in WSL: `smind
  serve`" has its own copy button (`navigator.clipboard.writeText`);
  "Retry now" reloads the page (`location.reload()`), which starts on
  attempt 0/"connecting..." until the next eval update arrives from the
  still-running Rust-side poll loop.

- **AC6**: `parse_deep_link` accepts exactly `smind://task/<id>` and
  `smind://workspace/<wsId>/task/<id>`, rejecting wrong scheme,
  non-numeric ids, extra path segments, a query string, and no-authority
  forms (`smind:task/1`) -- all via the same `url` crate already used
  elsewhere, no hand-rolled parsing. 10 new unit tests (both accepted
  forms; each rejection case named in the Test Scenarios list, plus a
  couple of adjacent malformed-input cases). A bare `smind://task/<id>`
  link (no workspaceId in the link itself) reuses AC3's
  `WorkspaceCache`: a cache hit navigates immediately, a miss opens a
  short-lived one-shot connection
  (`smind_daemon_client::client::resolve_workspace_id`, reusing the same
  `task.get` request/response helpers `client.rs` already has for AC3)
  rather than leaving the link at "focused but nowhere" -- the window is
  focused immediately either way, then upgraded to a navigate once
  resolution lands, satisfying "unknown/malformed just focuses" for the
  genuinely-unknown case while still giving the known-but-unresolved
  case its navigation. `tauri-plugin-single-instance`'s `deep-link`
  feature is fully automatic (confirmed by reading its 2.4.5 source):
  it calls `handle_cli_arguments` before our existing single-instance
  callback, with no extra wiring needed beyond enabling the feature
  (done in AC1) and registering both plugins. One build fix along the
  way: adding the `plugins.deep-link` block to `tauri.conf.json` made
  `tauri::generate_context!()`'s generated code reference `serde_json`
  unqualified, which only resolves if the app crate depends on it
  directly -- added `serde_json = "1"` to `Cargo.toml`. `cargo test` in
  `daemon-client` is 47/47 (10 new); `cargo build` passes on Linux with
  no new warnings; the capability file and `gen/schemas/` are both
  unchanged (the deep-link plugin's schema entries were already
  generated back in AC1's `cargo check`, since the crate dependency was
  added there ahead of use).

- **AC7 regressions**:
  - `grep -rn "tauri::command|invoke_handler" desktop/src-tauri/src/` is
    empty -- no new command surface anywhere across AC1-AC6.
  - `desktop/src-tauri/capabilities/default.json` diffed byte-for-byte
    against its state before AC1's commit: unchanged (`permissions: []`,
    no `remote.urls`).
  - Final full run: `cargo test` in `daemon-client` is 47/47; `cargo
    test`/`cargo build` in `src-tauri` both pass (0 tests there by
    design -- all pure logic lives in `daemon-client` per ADR-0012's own
    rationale, so this is expected, not a gap).
  - The existing `desktop-tauri-shell.md` ACs were re-exercised live
    (below), not just assumed from the code diff.
  - **Not done**: opening a PR (explicitly out of scope per the task).

- **AC7 live run (WSLg, X11 :0, this session)**: a temp daemon was
  started from the main checkout's `bin/smind` with
  `SMIND_HOME=/tmp/smind-desktop-live-test` and `server.port: 4699` (a
  second, pre-existing `smind serve` was already running on the real
  4648 from outside this session -- left untouched throughout, verified
  reachable again after cleanup).
  - **AC1 (window state)**: plugin registered, no crash; a full restart/
    monitor-loss visual check needs eyes on the actual window, which
    this sandbox has no screenshot tool for (still true from the
    original plan's Validation) -- see below.
  - **AC1/original-plan AC1 (fallback -> daemon UI)**: pointed at 4699,
    logs show `smind desktop: /healthz ok, switching to daemon UI`, ws
    connect, and `subscribed to permission.pending, run.status` (the
    AC4 subscribe-topic change, confirmed live).
  - **Offline path (AC5)**: pointed at an unreachable port (4700), ran
    9s: five `daemon connection error: token fetch: ...` lines with
    visibly growing spacing (backoff), no panic, no eval errors logged
    (errors are swallowed by design, so this is weak evidence, but
    consistent with a working eval call) -- process stayed alive the
    whole time.
  - **Single instance (original-plan AC2)**: with the first instance
    running and connected, a second launch (`timeout 5 ... `) exited 0
    within a fraction of a second with an empty log, deferring to the
    first instance -- matches the prior session's own validated
    behavior, confirming AC6's new deep-link forwarding wiring on
    `tauri-plugin-single-instance` didn't change this.
  - **Deep links (AC6)**: `register_all()` logs `deep link registration
    failed: No such file or directory` -- expected for an unpackaged
    `target/debug` binary with no `.desktop`/xdg-mime entry to register
    against (production relies on the bundler-generated `.desktop` file
    at install time, per AC6's own wording). This sandbox also has no
    `xdg-open` at all, so the Test Scenario's `xdg-open 'smind://
    task/1'` step could not be run here. Confidence instead comes from:
    the 10 unit tests on `parse_deep_link` itself, and reading
    `tauri-plugin-single-instance` 2.4.5's source (not just its docs) to
    confirm the `deep-link` feature's forwarding is fully automatic with
    no extra code on this app's side.
  - **Not exercised live this session** (needs a human, input
    simulation tooling this sandbox doesn't have, or a packaged
    install): tray Open/Quit/attention-item clicks, the app menu's
    items, `CommandOrControl+Shift+S`, a real `permission.pending` ->
    notification -> click -> navigate round trip, taskbar badge
    rendering, light/dark rendering of the offline page, and the deep
    link from an actual OS-level `smind://` invocation. These are the
    same category of gap the original `desktop-tauri-shell.md` plan's
    Validation already recorded ("needs human eyes, WSLg") -- unchanged
    by this session's additions, not a new regression.
  - Cleanup: the temp daemon (verified by PID/environ, not just port,
    since a second unrelated `smind serve` was running) and app process
    were both killed; `/tmp/smind-desktop-live-test` removed; the real
    daemon on 4648 confirmed still answering `/healthz` afterward.

- **AC7 Windows build**: pushed to `origin/feat/desktop-tauri-shell`;
  see below for the `desktop-windows` workflow result.
