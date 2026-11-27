# UI cleanup B3: Settings as a modal (desktop first, web shares)

Size M. Human decision H4 (2026-11-27). Web UI only (the desktop shell and the web build share App.tsx): nothing under `mobile/`, no daemon, wire or Rust change, no router and no URL. Contract: `docs/plans/active/ui-cleanup-b3.contract.md`. Source: the Settings audit, section 4 (the design) and the code facts below, re-read before editing.

## What is true today (read from the code)

- Settings is view state: `activeView` is `"workspace" | "settings" | "inbox" | "automations" | "plugin"` in `App.tsx`; `openSettings(sectionId?)` sets the initial section, bumps `settingsOpenNonce` (the screen's key, so asking for a section while open remounts onto it) and sets `activeView = "settings"`. A task window refuses it (`isTaskWindow`).
- While any non-workspace view is open `useActionRestriction(SETTINGS_SAFE_ACTIONS)` is on, `viewOpen` hides and makes inert the content region, and below the mobile breakpoint `shellCovered` hides and makes inert the whole shell. The settings screen is drawn inside the shell's content region (`pageCover`), beside the sidebar.
- Triggers, all through `openSettings`: `settings.open` (Cmd/Ctrl+,), the native menu, the sidebar gear, the palette "Settings: ..." rows, the `?` overlay's "Customize in Settings", the `smind:open-settings` window event, and deep links (`openSettings("connections")` from the desktop-unreachable screen, the provider and login failure cards).
- `NavEntry.view` can be `"settings"`, so Settings is a back/forward history entry today.

## Decisions

- **D1 Settings becomes a modal.** `activeView` loses `"settings"`; a separate boolean `settingsOpen` renders a `SettingsDialog` (the existing `Dialog` primitive: focus trap, focus restore, `aria-modal`, labelled title) above the shell. `inbox`, `automations` and `plugin` stay full-pane views. `viewOpen` and `shellCovered` no longer include Settings, so the workspace stays mounted, visible and unchanged behind the dim backdrop (a composer draft, the scroll position and the selected task survive open and close).
- **D2 Sizes.** At desktop widths: centred, `min(960px, 90vw)` by `min(680px, 90vh)`, `rounded-2xl`, dimmed backdrop. At the minimum window (800x520, anything at or below 860px wide or 560px tall) and in the narrow layout: a full-bleed sheet from the top of the window, no rounded corners, nothing of the app showing around it (review fix: starting the sheet below the title bar left the app header half visible above it). On macOS the sheet clears the traffic lights INSIDE itself (38px of top padding on a solid band) and that band is a window drag region. The narrow layout (below 768px) keeps today's list to detail drill-down inside the sheet; between 768 and 860px the existing icon rail applies. Close: Esc, an X in the top-right corner of the content pane, backdrop click (at dialog sizes). In narrow detail, Esc first pops to the list, as today.
- **D3 Keyboard.** `useActionRestriction(SETTINGS_SAFE_ACTIONS)` is on while `settingsOpen` or a non-workspace view is open and released the moment both are closed, so shortcuts never act on the workspace behind the modal. `settings.open` toggles: pressing Cmd/Ctrl+, while open closes. Opening Settings again with a section while open (a deep link, `?` then "Customize in Settings") remounts onto that section, as today.
- **D4 Focus.** Initial focus lands on the dialog container itself, with no focus ring (review fix: focusing the nav item programmatically drew a heavy ring on the selected row even for a mouse or Cmd+, open); the requested section is shown by the nav item's own selected style (`aria-current`), and the first Tab moves into the nav with the normal focus ring. Focus returns to the opener on close (the Dialog primitive's job; Cmd+, has no trigger element, so it returns to the previously focused element).
- **D5 History.** Settings is no longer a back/forward history entry: it is an overlay, not a place. `NavView` drops `"settings"`; opening or closing it never pushes an entry.
- **D6 Nested dialogs.** A surface that opens Settings from inside its own dialog closes that dialog first (the `?` overlay's "Customize in Settings"; any other caller found by grepping `openSettings`). Pressing `?` while Settings is open opens the overlay above it (a Radix stacked dialog) and its Customize button closes it and lands on the Shortcuts tab.
- **D7 Reconnect.** The Daemon and Connections sections restart or reconnect the daemon. If that swaps or unmounts the shell while the modal is open, unmounting the dialog releases the page lock (no stuck backdrop, no `pointer-events: none` left on `body`). A test pins it.
- **D8 Chrome inside the modal.** `SettingsScreen` gains `variant: "page" | "modal"` (default `page`, used by nothing after this PR but kept for the tests). In `modal` there is no "Back to app" row, no window controls, no title-bar drag region and no left inset for the traffic lights; the nav header is the title "Settings", and the X sits in the content pane's header.
- **D9 Not doing.** No router, no `/settings/<tab>` URL, no change to the desktop multi-window model (task windows still refuse Settings), no change to the section contents (B2 is final), and no behaviour change in any section.

## Acceptance criteria

- AC1 Every trigger opens the modal on the right section: `settings.open`, the `smind:open-settings` event with and without a section, the sidebar gear, a palette Settings row, the `?` overlay's Customize in Settings (which closes the overlay first), a deep link.
- AC2 Esc, the X and a backdrop click close it; Cmd/Ctrl+, while open closes it; in narrow detail Esc pops to the list first.
- AC3 It is a modal dialog with a labelled title, traps focus, puts initial focus on the requested section's nav item, and returns focus to the opener.
- AC4 The workspace stays mounted and visible: the content region is neither `invisible` nor `inert`, and a composer draft survives open and close.
- AC5 The keyboard restriction is active while Settings is open (a task or tab shortcut does nothing) and is released on close.
- AC6 A task window still refuses Settings.
- AC7 At and below 860px wide or 560px tall the dialog is a full-bleed sheet (no rounded corners, inset to the window), at 768px and below it keeps the list to detail drill-down; at larger sizes it is the centred 960x680 dialog.
- AC8 Settings is not a history entry; unmounting the app with it open leaves no page lock.
- AC9 Frames (REAL-WINDOW where the session is unlocked, else WEB-HARNESS, every frame labelled): see Frames. No `mobile/`, `internal/` or `cmd/` change; hygiene green.

## Named tests

- `components/settings/settings-dialog.test.tsx`: `ui_cleanup_b3_dialog_is_modal_and_traps_focus`, `ui_cleanup_b3_dialog_has_a_labelled_title`, `ui_cleanup_b3_esc_closes`, `ui_cleanup_b3_backdrop_click_closes`, `ui_cleanup_b3_x_button_closes`, `ui_cleanup_b3_requested_section_is_selected_on_open`, `ui_cleanup_b3_no_focus_ring_on_open`, `ui_cleanup_b3_tab_moves_focus_into_the_nav`, `ui_cleanup_b3_focus_returns_to_the_opener`, `ui_cleanup_b3_modal_variant_has_no_back_row_or_window_controls`, `ui_cleanup_b3_sheet_classes_at_small_sizes`, `ui_cleanup_b3_sheet_clears_the_traffic_lights_inside_itself`, `ui_cleanup_b3_narrow_keeps_list_to_detail`, `ui_cleanup_b3_narrow_esc_pops_to_the_list`.
- `App.settings-modal.test.tsx`: `ui_cleanup_b3_triggers_open_the_modal` (table over the triggers), `ui_cleanup_b3_deep_link_lands_on_the_section`, `ui_cleanup_b3_cmd_comma_toggles_closed`, `ui_cleanup_b3_workspace_stays_mounted_and_draft_survives`, `ui_cleanup_b3_restriction_active_while_open_and_released_on_close`, `ui_cleanup_b3_task_window_refuses_settings`, `ui_cleanup_b3_overlay_closes_before_settings_opens`, `ui_cleanup_b3_settings_is_not_a_history_entry`, `ui_cleanup_b3_palette_task_pick_closes_settings`, `ui_cleanup_b3_unmount_while_open_releases_the_page_lock`.
- Existing tests that assert "Settings replaces the workspace" (App.test.tsx, settings-screen tests, nav-history tests) are updated, never weakened.
- `web/e2e/specs/ui-cleanup-b3.spec.ts`: `UC3-01` open and close by every route, `UC3-02` the workspace and a draft stay, `UC3-03` the keyboard restriction and deep links, `UC3-04` sheet at 800x520 and the narrow layout at 390, `UC3-05` open and close transition frames; frames when `UICLEAN3_FRAMES_DIR` is set.

## Mutations (committed code; each must fail a named test)

- `openSettings` also sets a non-workspace `activeView` (the workspace content region goes `invisible` and `inert` again) -> `ui_cleanup_b3_workspace_stays_mounted_and_draft_survives`.
- The dialog is rendered non-modal (`modal={false}`: no focus trap, no outside `aria-hidden`) -> `ui_cleanup_b3_dialog_is_modal_and_traps_focus`.
- The keyboard restriction ignores `settingsOpen` going false (stays on after close) -> `ui_cleanup_b3_restriction_active_while_open_and_released_on_close`.

## Frames

Real macOS desktop window (scripts/desktop-audit-build.sh from this branch, dogfood daemon under `~/.smind/dogfood/ui-b3/`) when the session is unlocked, else the web harness; every frame labelled REAL-WINDOW or WEB-HARNESS. Closed workspace; modal open on General, Shortcuts (the wide table), Automations and Browser; at 1280x800 and 800x520; light and dark; open and close TRANSITION frames (frames taken during the animation, reviewed for glitches); the nested case (opened from the `?` overlay). A contact sheet and every pair opened by the Lead.

## Progress

- [ ] spec and contract (this file)
- [ ] SettingsDialog, modal variant, sheet and narrow layout (peer A)
- [ ] App.tsx wiring: settingsOpen, restriction, history, callers, App tests (peer B)
- [ ] e2e and frames
- [ ] review, mutations, validator

## Validation

- `sh scripts/verify.sh`: VERIFY PASS. Full e2e matrix (`E2E_FULL=1`): 395 passed, 49 skipped (after updating the existing specs that assumed Settings was a page). tsc clean. UI suite 531 files / 5870 tests.
- Mutations on committed code, each failing its named test: M1 (`openSettings` also sets a non-workspace `activeView`) -> `ui_cleanup_b3_workspace_stays_mounted_and_draft_survives`; M2 (`modal={false}`) -> `ui_cleanup_b3_dialog_is_modal_and_traps_focus`; M3 (the restriction ignores `settingsOpen` going false) -> `ui_cleanup_b3_restriction_active_while_open_and_released_on_close`.
- **REAL-WINDOW frames** (the macOS desktop app built from this branch with `scripts/desktop-audit-build.sh`, a dogfood daemon under the dogfood root, smoke check passed: the app refuses to start without a scratch daemon; the screen was unlocked, rechecked before each pass): closed workspace, the modal open on General, Shortcuts, Automations and Browser, light and dark, at 1280x800 and at the 800x520 minimum (a full-bleed sheet that starts below the native title bar band so the traffic lights stay uncovered), and the nested case (`?` overlay, then Customize in Settings: the overlay closes and Settings lands on Shortcuts). The audit app, its daemon and the Rust build cache were removed afterwards.
- **Defect found by the real window and fixed:** in the modal's content pane (about 655px at 1280, about 460px in the sheet) the Shortcuts table was wider than the pane, so its Scope and Actions columns were cut off. The table now collapses by container width (container query) in the Settings tab; the `?` overlay is unchanged. The four Shortcuts frames after the fix replace the superseded ones (kept under `superseded/`).
- **WEB-HARNESS frames** (headless browser): closed, the four sections, the nested case and TRANSITION frames with the animations paused at 0, 40, 80 and 120 ms of both the open and the close animation, light and dark at 1280x800, 800x520 and 390x844; six contact sheets, each opened. No glitch in any phase: the dialog and backdrop fade and zoom together, nothing flashes, the workspace never moves behind it.
- Not captured in the real window: the transition itself. The animation is about 150 ms and a screenshot takes longer, so the real-window open and close frames show before and after only (no ghost frames, no leftover backdrop); mid-animation phases are the web-harness frames above.
- Harness artifacts, not product: a `Theme` tooltip stays on the footer button in some real-window closed frames (the pointer rested there after a scripted click), and scripted accessibility clicks reach elements under a modal (so keyboard navigation was used to move between tabs).
