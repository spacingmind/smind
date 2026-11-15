# DT-UI Lane B: desktop shell chrome (window size, full-page views keep the sidebar, native pass)

Source: the desktop design review (ranks 2 and 10; local-only research note, not committed). Branch flow: every PR targets `wave/desktop`, each from a fresh `origin/wave/desktop`, never stacked.

## Checked against origin/wave/desktop (already shipped, not gaps)

- Window geometry (size, position, maximised) is saved and restored per window by `tauri_plugin_window_state` (`lib.rs`; VISIBLE and DECORATIONS excluded on purpose). So "saved frame" exists; what is missing is a minimum size.
- Overlay title bar, traffic lights inside the UI's 48 px header, sidebar vibrancy, native app menu, Dock lifecycle, badge, deep links (D1-D4, DT1).
- Settings, Inbox, Automations and plugin pages are `absolute inset-0 z-40` overlays that COVER the whole shell including the sidebar (`App.tsx`: the shell is `invisible` and `inert` behind them). That is the "sidebar lost" gap.
- No `prefers-contrast`, `prefers-reduced-transparency` handling in the UI CSS; reduced motion is handled per component (`motion-reduce:` classes) and in the demo clock only.
- No window focus styling: the UI does not know whether its window is the key window.
- Shortcut tooltips: shortcuts are shown in the palette and the shortcuts overlay (`keyboard/shortcuts.ts`, `formatShortcut`), not on the sidebar/header buttons.
- Dock menu: neither Tauri 2.11.6 nor muda 0.19.3 exposes an API for it (no `dock_menu`); see "Open decision".

## PR 2a: minimum window size (saved frame already exists)

- AC1 The main window has a minimum inner size of 800 x 520 logical px on every platform, set on the builder (`window_chrome::configure`). The width is above the web mobile breakpoint (768), so a desktop window never falls into the sheet-sidebar layout.
- AC2 A size saved before the minimum existed is corrected: on every resize the shell puts a window smaller than the minimum back to it (belt and braces over the OS enforcement; a minimised window, reported as 0 x 0 on Windows, is left alone).
- AC3 The saved frame keeps working: nothing here changes the window-state flags.
- Tests: `the_minimum_width_keeps_the_desktop_layout` (800 >= 768), `clamp_to_minimum_table` (at, below, zero), `the_main_window_builder_sets_the_minimum`.
- Not verifiable here: that AppKit/Windows enforce the minimum while dragging and that a restored small frame is corrected on screen (needs a real window; the screen is locked).

## PR 2b: Settings, Inbox, Automations and plugin pages keep the sidebar

- AC4 On desktop widths the four pages render INSIDE the shell's content region (next to the sidebar), not over the whole window. The sidebar stays visible and usable; the traffic lights stay over the sidebar header, so they never sit over page content.
- AC5 Choosing something in the sidebar while a page is open leaves the page and does what the click means (select a task, new task, open another page).
- AC6 The pages' own back rows no longer reserve space for the traffic lights when the sidebar is visible; with the sidebar collapsed (icon rail) the lights bar is unchanged.
- AC7 Below the mobile breakpoint nothing changes (the pages stay full-screen overlays, the sidebar is a sheet).
- AC8 The browser build gets the same layout (it is one shared layout; no desktop flag).
- Tests: App-level tests for AC4/AC5 (page in content region, shell not inert, sidebar click leaves the page), the settings screen test for AC6, Playwright frames light and dark at 1280 and 390 for settings, inbox and automations.

## PR 10: native pass

- AC9 Inactive-window styling: the UI sets `data-window-active="false"` on the root while the window is not focused; sidebar selection and icons dim in that state (CSS tokens, both themes). Web-side only (`window` focus/blur), no IPC.
- AC10 `prefers-reduced-motion: reduce` removes the remaining transitions the sidebar width animation and menus use; `prefers-contrast: more` raises the border and muted-text tokens in both themes; `prefers-reduced-transparency: reduce` makes the sidebar surface opaque (the native vibrancy follows the system setting itself). Each one has a media-query test over the token sheet.
- AC11 Shortcut tooltips: the sidebar toggle, New task, Search, Settings and the back/forward buttons show their live shortcut (from the resolved bindings, so a customised shortcut is shown as customised) in the tooltip.
- AC12 Dock recent items: see the open decision; not built without it.

### Open decision (asked of the Supervisor)

Dock recent items need an Objective-C `applicationDockMenu:` method on the app delegate, which Tauri and muda do not expose; the only route is a small `objc2` runtime hook, which is `unsafe` code and a new dependency. Options: (1) add the hook, behind a macOS-only module with the menu content built in safe Rust (task ids and titles from the daemon, activation through the existing deep link); (2) skip the Dock menu and use the tray's "recent" list on Windows/Linux only. Recommendation: (2) for now, revisit with a real-window session; the Dock menu is the least visible item of rank 10.

## Progress / Validation

### PR 2a

- AC1-AC3: `window_chrome::min_size_tests` (4 tests); `cargo test` 191 pass, clippy `-D warnings` and fmt clean.
- Mutations (committed code, each failed its named test): `.min_inner_size` removed from the builder, `clamp_to_minimum` always returning None, the Resized handler no longer calling the clamp.
- Not verified: AppKit/Windows enforcing the minimum while dragging, and a restored small frame being corrected on screen (the screen was locked, so no real-window frames); Windows and Linux unbuilt.
