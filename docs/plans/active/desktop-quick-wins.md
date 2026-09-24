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

## Progress

- [ ] AC1 window state
- [ ] AC2 app menu
- [ ] AC3 notification click → task
- [ ] AC4 tray attention
- [ ] AC5 offline/splash
- [ ] AC6 deep links
- [ ] AC7 regressions + Windows build

## Validation

To be filled in as items land.
