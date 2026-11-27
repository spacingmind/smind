# UI cleanup B1: header and shortcut surfaces (desktop first, web shares the components)

Size S. Human decisions 2026-11-27 (H1, H2). Web UI only: nothing under `mobile/`, no daemon or wire change, no Rust change.
Contract: `docs/plans/active/ui-cleanup-b1.contract.md`.

## Behaviour

**H1. The Windows/Linux header "Application menu" hamburger goes; its items move into the Cmd/Ctrl+K palette.**

- `components/app-menu.tsx` (and its test) is deleted; `<AppMenu />` leaves `app-header.tsx`; `Icons.menu` is removed from `lib/icons.ts` and its drift test.
- A new `palette/desktop-commands.ts` builds the entries, `useDesktopCommands()` registers them as palette source `shell:desktop` (rank 6) from `ShellCommands` in `App.tsx`.
- Native entries (each calls `desktop.menuAction(<id>)`, the same call the hamburger made): Reload (`reload`), Zoom in / out / reset (`zoom-in`, `zoom-out`, `zoom-reset`), Toggle fullscreen (`fullscreen`), Minimize (`minimize`), Hide window (`hide`), About sMind (`about`), Open log folder (`log-folder`), sMind on GitHub (`github`), Quit sMind (`quit`), Check for updates (`check-updates`, only when `desktopOS === "windows"`, the hamburger's own gate: there is no Linux updater).
- Renderer entries the hamburger also listed (each runs the same keyboard-registry action the hamburger ran, `source: "menu"`): Toggle sidebar (`sidebar.toggle`), Find in pane (`pane.find`), New tab (`tab.new`), Close tab (`tab.close`). Settings and Command palette were already palette routes (`Settings: ...` rows, the Search row), so they are not repeated.
- Gate: every entry above appears ONLY when `isDesktop` (all desktop OSes, including macOS, whose native menu has the same items; the palette is the one place they are reachable by keyboard-search). On web the source registers nothing.
- Rows show their live shortcut where an action id exists (`action`), as other palette rows do.

**H2. Redundant visual shortcut surfaces go; the shortcuts keep working.**

- (a) The header keyboard icon (`app-header.tsx`, testId `app-header-help`) is removed. `shortcuts.help` still opens through the `?` key, the sidebar Help menu, Settings -> Shortcuts and the palette. The terminal key bar's keyboard toggle gets its own registry key `Icons.keyboard` (same glyph, own name) so it no longer shares `Icons.shortcuts` (which stays for the Settings Shortcuts tab).
- (b) The composer hint "<combo> to focus" (`composer-focus-hint`) is removed with its `showFocusHint`/`focusCombo` plumbing; the `composer.focus` binding is untouched.
- (c) Task-list toolbar (`app-sidebar.tsx`, the "Task list" label row). Each control checked:
  - 3 group-mode toggles (tree/status/stage): view toggles, KEEP.
  - "Started by automation" filter (`Icons.automation`, `sidebar-automation-filter`): a filter of the list, not a navigation to Automations (that is the quick-action row), so it is NOT a duplicate: KEEP. (The Human wrote "the schedule icon": this is the one.)
  - "+" (`sidebar-new-workspace-button`, "New workspace"): opens `setCrud({ kind: "workspace" })`, exactly what the sidebar footer "Add workspace" button (`sidebar-footer-add-workspace`, `onAddWorkspace`) opens: a true duplicate, REMOVE. The empty-state "Add project" buttons stay (they are the empty state, not the toolbar).
  - Search toggle (`sidebar-search-toggle`): it is NOT the same as the Search quick-action row. The row opens the command palette (a global jump list); the toggle opens an inline filter of the visible tree, and it is the only entry to that filter (about 20 tests drive it). Removing it would delete the inline filter feature, not a duplicate, so it is KEPT and called out as a Human decision in the report.
- Kept: the single Cmd/Ctrl+K chip on the sidebar Search row, tooltips with shortcuts, the `?` overlay, Settings -> Shortcuts, the native menus.

## Acceptance criteria

- AC1 Palette on desktop lists every native entry above, and each entry calls its own `menuAction` id (and nothing else).
- AC2 `Check for updates` is listed only on Windows; no entry is listed when `isDesktop` is false.
- AC3 The renderer entries run their keyboard action with `source: "menu"`.
- AC4 The Application menu is gone: no `app-menu-trigger` on any OS; `app-menu.tsx` does not exist; `Icons.menu` is not in the registry.
- AC5 The header has no keyboard icon (`app-header-help`); the `?` keystroke still runs `shortcuts.help`; the right cluster order is editor pill, terminal, window controls.
- AC6 The terminal key bar keyboard toggle still renders and uses `Icons.keyboard`.
- AC7 The composer has no focus hint; the `composer.focus` keystroke still focuses the input.
- AC8 The toolbar has no "New workspace +"; the footer "Add workspace" opens the same dialog; the toolbar search toggle and automation filter remain.
- AC9 Frames (REAL-WINDOW where the session is unlocked, WEB-HARNESS otherwise, every frame labelled): light and dark, 1280x800 and 800x520. No `mobile/`, `internal/` or `cmd/` change; hygiene green.

## Named tests

- `palette/desktop-commands.test.ts`: `ui_cleanup_palette_lists_native_entries_on_desktop`, `ui_cleanup_palette_entry_runs_its_own_native_command`, `ui_cleanup_palette_entries_absent_on_web`, `ui_cleanup_check_updates_only_on_windows`, `ui_cleanup_palette_renderer_entries_run_actions`.
- `components/app-header.test.tsx`: `ui_cleanup_no_application_menu_and_no_help_icon` (windows, linux, macos, web), `ui_cleanup_header_right_cluster_order`.
- `components/ui-cleanup-b1-header.test.tsx`: `ui_cleanup_shortcuts_help_keystroke_still_opens_overlay`, `ui_cleanup_terminal_key_bar_keeps_its_keyboard_toggle`.
- `components/ui-cleanup-b1.test.tsx`: `ui_cleanup_composer_focus_keystroke_still_focuses`, `ui_cleanup_composer_has_no_focus_hint`, `ui_cleanup_sidebar_plus_removed_footer_add_workspace_kept`, `ui_cleanup_sidebar_search_toggle_and_automation_filter_kept`.
- `lib/icons.test.ts`: the registry test lists `keyboard` and no longer `menu`.
- `web/e2e/specs/ui-cleanup-b1.spec.ts`: `UC-01` header without keyboard icon, `UC-02` palette without desktop entries on web, `UC-03` sidebar toolbar, `UC-04` composer without hint; frames when `UICLEAN_FRAMES_DIR` is set.
- Existing tests that referenced the removed surfaces are updated or deleted with them (`app-menu.test.tsx`, the `app-header-help` assertions in `app-header.test.tsx` and `App.responsive.test.tsx`, the two focus-hint tests in `composer.test.tsx`, the `sidebar-new-workspace-button` clicks in `app-sidebar-rows.test.tsx` and `app-sidebar-memo.test.tsx`, which switch to the footer button).

## Mutations (committed code; each must fail a named test)

- A palette entry calls the wrong native command (reload -> zoom-in) -> `ui_cleanup_palette_entry_runs_its_own_native_command`.
- The desktop gate is removed (entries show on web) -> `ui_cleanup_palette_entries_absent_on_web`.
- The `?` binding is removed along with the icon -> `ui_cleanup_shortcuts_help_keystroke_still_opens_overlay`.

## Frames

Real macOS desktop window (scripts/desktop-audit-build.sh from this branch, dogfood daemon under `~/.smind/dogfood/ui-b1/`, own port) while the session is unlocked, else web harness; every frame labelled REAL-WINDOW or WEB-HARNESS. Light and dark at 1280x800 and 800x520: main window (header), sidebar toolbar, composer, palette with the moved desktop entries. A Windows/Linux window cannot be captured on this Mac (the Windows-only "Check for updates" row is covered by a unit test only).

## Decisions

- D1 Renderer entries and native entries are gated on `isDesktop` only (not Win/Linux only), so the palette is the same on every desktop OS; only Check for updates is Windows-only.
- D2 The toolbar search toggle is kept (see H2 c): flagged for the Human.

## Progress

- [ ] spec and contract (this file)
- [ ] palette entries and App.tsx mount
- [ ] remove the application menu, header icon, `Icons.menu`; terminal key icon
- [ ] composer hint, sidebar plus
- [ ] e2e and frames
- [ ] review, mutations, validator

## Validation

- `sh scripts/verify.sh`: VERIFY PASS (build, test, lint). Full e2e matrix (`E2E_FULL=1`): 345 passed, 43 skipped. tsc clean.
- Mutations on committed code, each failing its named test: M1 (Reload calls zoom-in) -> `ui_cleanup_palette_entry_runs_its_own_native_command`; M2 (gate removed) -> `ui_cleanup_palette_entries_absent_on_web`; M3 (the `?` binding moved away) -> `ui_cleanup_shortcuts_help_keystroke_still_opens_overlay`.
- Frames: every frame is WEB-HARNESS (headless browser on the built web UI): light and dark at 1280x800, 800x520 and 390, for the header, the sidebar toolbar, the composer and the palette (UC-01 to UC-04). The macOS session was locked when the real-window pass started (an unlocked session was confirmed at the start, then the lock came on while the desktop app was rendering), so NO REAL-WINDOW frame exists for this change: the audit app was built (one Rust build, smoke test passed: it refuses the real daemon) but its window captured blank behind the lock, and it was shut down and its build cache deleted. The palette rows added on desktop are therefore verified by unit tests only, not by a frame: the web harness build is not a desktop build, so it shows the palette WITHOUT those rows (frame UC-04). A real Windows or Linux window cannot be captured on this Mac either.
