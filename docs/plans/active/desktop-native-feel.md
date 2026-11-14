# Desktop native feel, round two (DT2): native context menus, validated menu state, window layout, window tabbing

Status: active (2026-11-15). Lead of DT2 on `wave/desktop`. Sequel to [`completed/desktop-native-feel.md`](../completed/desktop-native-feel.md) (chrome, de-webview, shortcuts and menu, scoped integrations) and `desktop-macos-app.md`. Size: L, four PRs one at a time, each from a fresh `origin/wave/desktop`, base `wave/desktop`, never stacked: (a) native context menus, (b) validated menu enable/disable state, later (c) layout saved and restored per window on Cmd+Q, (d) a SPIKE (write-up only) on "Open task in new window" and macOS window tabbing.

## Checked against origin/wave/desktop: already shipped (not gaps)

The macOS app-name menu and File/Edit/View/Window/Help with the accelerator table (`menu.rs`), the renderer actions through `menu-action` and `app_menu_action`, the tray with the attention count (`tray.rs`), the Dock badge on macOS and the taskbar overlay on Windows (`lifecycle.rs`, `overlay_icon.rs`), deep links (`deeplink.rs`, single instance), the overlay title bar on macOS and the drawn caption buttons elsewhere, sidebar vibrancy and Mica, a hidden-until-painted window, window geometry restore (`tauri-plugin-window-state`), zoom persistence, reveal-in-Finder and open-in-editor (`editors.rs`), the auto-updater, and the document-level policy that replaces the webview's default right-click menu with Cut/Copy/Paste only in editable fields and on a selection (`lib/desktop-context-menu.ts`, D2.2).

## What is genuinely missing (the gaps this plan closes)

1. **Context menus are in-page DOM menus** (Radix, `components/ui/context-menu.tsx`): the sidebar workspace and task rows, the workspace tabs, and the file and folder rows in the explorer. They are clipped by the window, do not follow the OS look, type-ahead or accessibility, and do not close on app blur. A real Mac app pops a native menu.
2. **Menu items never reflect state.** The native app menu is built once; "Close Tab" is enabled with no tab, "Find" with no pane, and so on. Only the in-page Radix menus disable items.
3. **Quit does not keep the layout.** `tauri-plugin-window-state` keeps size and position only; open tabs, splits and the sidebar state are not saved per window, so a Cmd+Q and relaunch starts from a blank layout.
4. **One window.** There is no "Open task in new window" and no macOS window tabbing; whether either is worth building is unknown, hence the spike.

## PR (a): native context menus

### Acceptance criteria

- AC1 In the desktop build, a right-click on a sidebar workspace row, a task row (including the same item set as its "more" menu), a workspace tab, a file row and a folder row pops a NATIVE menu at the pointer (Tauri `Menu::popup_at`), with the same items, order, separators, disabled state and "Move to space" submenu as the in-page menu has today. Choosing an item runs exactly the handler the in-page item would.
- AC2 In the browser build (and whenever the desktop bridge is unavailable) the existing Radix menu renders exactly as today: same test ids, same behaviour. No `@tauri-apps/api` import is reachable in the browser build.
- AC3 Menus are data, not JSX: each of the five menus is built by one pure function into a menu model (`id`, `label`, `enabled`, `destructive`, `separator`, `children`), and BOTH renderers (Radix and native) consume that same model, so the two cannot drift.
- AC4 The IPC carries descriptors only: the webview sends labels, opaque item ids and enabled flags and gets item ids back. It cannot make Rust run a path, a command or a URL through this channel. Rust validates the model (at most 64 entries and depth 2, ids 1-64 chars of `[A-Za-z0-9._:-]` and unique per menu, labels at most 120 characters with no control characters, a menu with no actionable item is refused) and refuses one that fails. The new command is added to `capabilities/proxy.json` by name (ADR-0013 section 2), no wildcard.
- AC5 A native menu's selection reaches only the menu that opened it: a stale selection (an earlier menu, a duplicate) is ignored, opening a second menu replaces the first, and a dismissed menu leaves no handler that fires later.
- AC6 The web webview's own default menu stays suppressed everywhere else (D2.2 unchanged); editable fields keep their native Cut/Copy/Paste menu, which the new handler never intercepts.
- AC7 Keyboard parity: the same menus open from the keyboard (Shift+F10 and the Menu key) on the focused row, natively on desktop.
- AC8 Windows and Linux use the same path (the menu API is cross-platform); only macOS is verified here (stated limit).

### Named tests

Rust (`cargo test`, unit-level): `context_menu_validate_*` (table: empty, only separators, too many, too deep, duplicate id, bad id, long or control-character label, disabled-only), `context_menu_ids_are_namespaced_per_open_and_roundtrip`, `context_menu_selection_of_a_stale_open_is_ignored`, `proxy_capability_names_context_menu_show_and_no_wildcard`, `context_menu_command_takes_no_path_or_command` (the argument type has only the descriptor fields).
Web (vitest): `menu-models.test.ts` (one table-driven test per menu: workspace row, task row, tab, file row, folder row: for each state the exact entries, order, separators, enabled flags), `app-context-menu.test.tsx` (the Radix fallback renders the model with the existing test ids; the desktop path calls the bridge with descriptors and not the DOM; selection resolves the handler once; a stale id, a dismissed menu and a second menu behave per AC5; the browser path never loads the bridge), `native-context-menu.test.ts` (the bridge: descriptor mapping, event matching, cleanup), the existing sidebar, tab and file-explorer suites unchanged and green.
Real window: frames of each native menu with `screencapture -l` from the isolated audit build (`scripts/desktop-audit-build.sh`, its scratch daemon, never the real daemon) when the screen is unlocked; while it is locked the PR states so and carries web frames of the Radix menus (unchanged) and the unit tests.

### Decisions

- **One bridge, descriptors only** (AC4): the same shape as `app_menu_action` (an allowlisted command, ids not actions); the renderer owns the handlers.
- **No icons in native menus**: the model carries a label only; the Radix menu keeps its icons.
- **Dismissal is not observable** in Tauri's menu API: a handler is armed by the open and discarded by the next open, a selection, a window blur or a short timeout (30 s), never kept.
- **Shared model, two renderers** (AC3) rather than patching the DOM menu: the one place the menus' contents live, which is also what (b) tests.

## PR (b): validated menu enable/disable state

### Acceptance criteria

- AC9 The native app menu items whose action cannot run are disabled and re-enabled as the renderer's state changes: Close Tab and Find with no workspace tab or pane, Close Tab on a tab that is not closable, Toggle Sidebar never disabled, New Tab with no workspace open, Reload while a connection is not ready, and so on, from ONE table (item id, the state it needs). The renderer reports a small state snapshot (`app_menu_set_state`, an allowlisted command, booleans only) on change; Rust applies it to the existing `MenuItem`s.
- AC10 Every context menu's enabled flags come from the same pure model functions as AC3 and are table-tested per menu (no task selected, no run, run finished, tab not closable, no other tabs, remote connection hides reveal and open-in-editor, ...).
- AC11 The snapshot cannot enable a native item the table does not allow: unknown keys are ignored, the table decides, and a stale snapshot after a window closes is dropped.

### Named tests

Rust: `menu_state_table_*` (one per item), `app_menu_set_state_ignores_unknown_keys`. Web: one table-driven test per menu (app menu snapshot builder and the five context menus): `app-menu-state.test.ts`, `menu-models.test.ts`.

## PR (c): layout per window on Cmd+Q (later; needs a decision)

Open question for the Supervisor before it is specified: where a window's layout lives (the daemon, which also serves the web build; the Tauri store file; or the renderer's `localStorage`). It touches architecture (a new persisted state and who owns it), so no spec or code until it is decided.

## PR (d): spike (write-up only)

"Open task in new window" (a second Tauri window on the same loopback proxy, the single-instance and deep-link behaviour, shared connection state, per-window zoom and layout) and macOS window tabbing (`tabbingIdentifier`, the native Window menu items). Output: a note in this plan with findings and a recommendation, no product code.

## Progress

- 2026-11-15: spec written against `origin/wave/desktop` cb8593cce. The screen was locked, so real-window frames wait.

## Validation

(filled as each PR lands)
