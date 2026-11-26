# DT2 (d) spike: "Open task in new window" and macOS window tabbing

Open: Part 2 (macOS tabbing) probe pending an unlocked screen; recommendation awaits a Supervisor decision

Status: write-up only. No product code. Written 2026-11-15 against `origin/wave/desktop`. Nothing here was tried in a real window (the screen was locked), so every "works" below is a reading of the code and the Tauri 2.11.6 / tao APIs, and the risks list says what only a real window can settle.

## Question

Can the desktop app offer (1) "Open task in new window" on a task row and (2) macOS native window tabs (tabs of whole windows, the system "Show Tab Bar" / "Merge All Windows" behaviour), and what would each cost?

## What the code does today (single-window assumptions)

- One window, label `main` (`MAIN_WINDOW`, `lib.rs`), built in `setup` with `WebviewWindowBuilder`, created hidden and shown on the UI's first paint (`window_chrome::ShowGate`).
- Everything that targets "the window" names `MAIN_WINDOW`: the badge (`lifecycle.rs`), notifications and tray clicks (`notify.rs`, `deeplink.rs`), the app-menu actions and zoom (`menu.rs`), the single-instance handler, the Dock reopen, `daemon_manager.rs`, `window_chrome.rs`, and the context-menu selection event (`context_menu.rs`: `emit_to(MAIN_WINDOW, ...)`).
- Capabilities are scoped to `"windows": ["main"]` (`capabilities/default.json`, `proxy.json`; ADR-0013, exact-list test in `capability_tests.rs`). A window with another label gets no IPC at all.
- Closing the window hides it (`CloseRequested` -> `prevent_close` + `hide`); Cmd+Q is the only quit.
- Geometry persists per window label through `tauri_plugin_window_state` (VISIBLE and DECORATIONS excluded).
- Chrome on macOS: overlay title bar, hidden title, custom traffic-light position, transparent window with sidebar vibrancy.
- The web app is one SPA at `http://127.0.0.1:<port>/?k=<secret>`; the port is kept across launches (`proxy_port.rs`) so the origin, and therefore `localStorage`, is stable. A route lives in the hash (`#/workspace/<w>/task/<t>/<tab>`, `lib/route.ts`), so a new window can be pointed at a task by URL alone. The page state that matters (task tab layout `smind:task-tabs`, drafts, pins, unread, prefs) is in `localStorage` of that one origin.

## Part 1: Open task in a new window

### Feasibility: yes, mostly mechanical

A second `WebviewWindowBuilder` with a unique label (`task-<id>` or `win-<n>`), the same `External(initial_url)` plus a hash (`...#/workspace/<w>/task/<t>/chat/<c>`), the same `window_chrome::configure`, the same navigation guard. The daemon is not involved: each window is another websocket client of the same daemon, which the daemon already supports (a browser and the app, or two browsers, are the same thing).

### What breaks or needs a decision

1. **Per-window targeting (required).** Every `MAIN_WINDOW` use above must say which window it means: events to the window that produced them (`context_menu` already has the `Window` argument: use `window.label()`); notification and tray clicks to the window showing that task, else the last-focused window, else `main`; the badge stays on the app (Dock tile is app-wide on macOS; Windows overlay icon needs a window, use `main`).
2. **Capabilities (required, security-relevant).** `windows: ["main"]` must become an explicit pattern for the new labels (Tauri accepts globs such as `task-*`). The exact-list test must pin the new scope, and the ADR-0013 reasoning (loopback origin only, no wildcard command) stays unchanged; only the window scope widens. This is the one change that needs an ADR note or an amendment to ADR-0013.
3. **Shared `localStorage` across windows (the real design question).** Both windows share one origin, so one `smind:task-tabs` map. `create-preference-store` already listens to the `storage` event, but `use-task-tabs`, drafts (`use-composer-draft`), pins and unread do not all re-read on it. Two windows on the same task would fight over its tab layout and its composer draft. Decision options: (a) one window per task: opening a task that already has a window focuses it (recommended); (b) allow duplicates and accept last-writer-wins on layout and drafts (bad: silent loss of a draft). With (a) the sharing is harmless because each task's layout/draft has exactly one writer; the per-window state that is NOT per task (selected workspace, sidebar width and collapse, route) will be shared prefs, which is acceptable and matches how a second browser tab behaves today.
4. **Close semantics.** `main` hides on close and the app lives on (tray / Dock). A task window should destroy on close (it has no tray identity); the hide-on-close handler and `window_chrome::dismissed` are attached to `main` only, so this is a natural split. Cmd+Q must still quit everything.
5. **Window state plugin.** It persists per label; a label like `task-<id>` leaves one entry per task ever opened in `.window-state.json`. Either use a small fixed pool of labels (`win-1..win-4`) or prune on close.
6. **UI chrome in a task window.** The overlay title bar needs the same header row; the sidebar is probably wanted hidden or collapsed in a task window. That is web work (a `?window=task` / hash flag), not Rust.
7. **Other entry points.** "Open in new window" in the task row context menu (a new item in `taskRowMenu`, desktop-only, beside Pin), a modifier-click on a task row, and a `File > New Window` menu item. The context-menu model already supports a desktop-only item the same way Reveal does (absent otherwise).
8. **Relay/remote connections.** The proxy and `connections_select` are app-wide, not per window: a second window shows the same selected connection. Per-window connection selection is out of scope and should stay out.

### Probe procedure (Part 2, unlocked screen only)

A throwaway build, never committed: the audit build (`scripts/desktop-audit-build.sh`, own identifier and scratch daemon, fake agent, no model calls) from a scratch worktree with a small patch: a real title per task window (`sMind - task N`) and `tabbing_identifier("smind-probe-tasks")` on the task windows; an environment switch also puts `main` in the group (risk 3). Two passes (task windows only, then main included), each saving full-screen shots at: main only; three windows untabbed; after Window > Merge All Windows; after Move Tab to New Window and a resize; after closing a task window and hiding and reopening `main`.

Record one line each: R1 whether the native tab bar overlaps, pushes or double-draws over the 48 px header and the vibrancy; R2 traffic-light placement in the group, after a resize and after a tab is moved out; R3 (main included) what hide-on-close does to a grouped window; R4 the three Window-menu items and the tab titles; R5 restore after Cmd+Q (expected: untabbed); R6 whether the native bar over the in-app workspace tab strip is confusing; R7 the verdict: ship (size S), ship with a named chrome adjustment, or leave windows untabbed. The probe refuses to launch while the screen is locked.

### Cost

- Rust: new `windows.rs` (open/focus/destroy, label pool), ~6 call sites changed from `MAIN_WINDOW` to a resolved label, one new allowlisted command `window_open_task(task_id)` taking an integer only (no URL from the webview: Rust builds the URL), capability glob + test. About 1 PR, M size.
- Web: platform bridge method, the menu item, a header/sidebar variant for a task window, `storage`-event re-reads for the keys that two windows can now touch. About 1 PR, M size.
- Tests: Rust unit (label allocation, one-window-per-task, URL builder), web tables (item present on desktop only); real-window checks needed.

## Part 2: macOS window tabbing

### Feasibility: the API exists; the look is the risk

Tauri exposes `WebviewWindowBuilder::tabbing_identifier(&str)` (tao `with_tabbing_identifier`): windows built with the same identifier can be grouped by AppKit, and without one automatic tabbing is disabled. So the mechanism is a one-liner on each window plus the system menu items.

What it brings for free: AppKit adds "Show Tab Bar", "Merge All Windows", "Move Tab to New Window" to the Window menu, honours the user's System Settings "Prefer tabs when opening documents", and draws the tab bar. What it does not bring: tab titles come from the window title (our title is hidden by `hidden_title(true)`; set a real title per window to label tabs), and there is no per-tab content API.

### Risks that only a real window can settle

1. **Overlay title bar and vibrancy.** The tab bar is a native strip under the title bar. With `TitleBarStyle::Overlay`, the transparent window and the web content drawn up to the top edge, the tab bar may overlap the 48px header row, push the content, or double-draw behind the vibrancy. This is the main unknown and decides whether tabbing is shippable without redoing the chrome.
2. **Traffic-light position.** `traffic_light_position` is applied per window at build time; a tabbed group resizes the title bar and the lights may sit wrong until re-applied.
3. **Hide-on-close with tabs.** `main` hides rather than closes; a hidden window inside a tab group is a state AppKit does not expect (the tab disappears while the group stays). Task windows that destroy on close avoid it; `main` should be excluded from the group (give it no tabbing identifier) or the behaviour tested.
4. **Window-state restore** of a tabbed group is not defined by the plugin; restore would bring windows back un-tabbed.
5. **The 16 px / 48 px header and the in-app tab strip** (workspace tabs) would sit under a native tab bar: two tab bars on screen with different meanings (task windows vs tabs inside a task). Product question: is that confusing enough to prefer "windows only"?

### Cost

Given Part 1, tabbing is small in code (one builder call per task window, a real window title, a Window-menu check) but its value depends on risk 1. Estimate S for the code, plus a probe.

## Recommendation

1. Do Part 1 first, as one-window-per-task, without tabbing: it is useful on its own, its design questions are answerable from the code, and it is where the real work is (per-window targeting and the capability scope).
2. Treat tabbing as a probe after Part 1: build a throwaway window with a `tabbing_identifier` against the audit build and look at the overlay title bar with two windows merged (needs an unlocked screen). Ship it only if risk 1 is a non-issue or a small chrome adjustment; otherwise leave windows untabbed and say so.
3. Needs a Supervisor decision before any code: (a) one window per task vs duplicates (recommended: one per task); (b) whether the ADR-0013 window-scope widening is an amendment or a new ADR; (c) whether `main` stays the only window with the tray/Dock/badge identity (recommended: yes).

## Not verified

Everything about real-window behaviour: tab bar over the overlay title bar, vibrancy with tabs, traffic-light placement, the Window menu items appearing, hide-on-close inside a tab group, and window-state restore of several windows. No code was run for this spike beyond reading the repository and the Tauri 2.11.6 sources in the local cargo registry.
