---
v: 1
milestone: "UI cleanup B3: Settings as a modal"
base: origin/develop
planRef: docs/plans/active/ui-cleanup-b3.md
acs:
  - id: AC1
    text: "Every trigger opens the modal on the right section."
    covers: { tests: [ui_cleanup_b3_triggers_open_the_modal, ui_cleanup_b3_deep_link_lands_on_the_section, ui_cleanup_b3_overlay_closes_before_settings_opens, ui_cleanup_b3_palette_task_pick_closes_settings], frames: [] }
  - id: AC2
    text: "Esc, the X, a backdrop click and Cmd+comma close it; narrow detail Esc pops to the list."
    covers: { tests: [ui_cleanup_b3_esc_closes, ui_cleanup_b3_backdrop_click_closes, ui_cleanup_b3_x_button_closes, ui_cleanup_b3_cmd_comma_toggles_closed, ui_cleanup_b3_narrow_esc_pops_to_the_list], frames: [] }
  - id: AC3
    text: "It is a labelled modal dialog that traps focus, starts on the requested section and returns focus."
    covers: { tests: [ui_cleanup_b3_dialog_is_modal_and_traps_focus, ui_cleanup_b3_dialog_has_a_labelled_title, ui_cleanup_b3_focus_starts_on_the_requested_section, ui_cleanup_b3_focus_returns_to_the_opener], frames: [] }
  - id: AC4
    text: "The workspace stays mounted and visible and a composer draft survives."
    covers: { tests: [ui_cleanup_b3_workspace_stays_mounted_and_draft_survives], frames: [] }
  - id: AC5
    text: "The keyboard restriction is active while open and released on close."
    covers: { tests: [ui_cleanup_b3_restriction_active_while_open_and_released_on_close], frames: [] }
  - id: AC6
    text: "A task window still refuses Settings."
    covers: { tests: [ui_cleanup_b3_task_window_refuses_settings], frames: [] }
  - id: AC7
    text: "Small sizes are a full-bleed sheet, narrow keeps list to detail, the modal variant drops the page chrome."
    covers: { tests: [ui_cleanup_b3_sheet_classes_at_small_sizes, ui_cleanup_b3_narrow_keeps_list_to_detail, ui_cleanup_b3_modal_variant_has_no_back_row_or_window_controls], frames: [] }
  - id: AC8
    text: "Settings is not a history entry and unmounting while open leaves no page lock."
    covers: { tests: [ui_cleanup_b3_settings_is_not_a_history_entry, ui_cleanup_b3_unmount_while_open_releases_the_page_lock], frames: [] }
  - id: AC9
    text: "No mobile, internal or cmd change; public text rules hold."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: ui_cleanup_b3_dialog_is_modal_and_traps_focus, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_dialog_is_modal_and_traps_focus", timeoutSec: 900 }
  - { name: ui_cleanup_b3_dialog_has_a_labelled_title, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_dialog_has_a_labelled_title", timeoutSec: 900 }
  - { name: ui_cleanup_b3_esc_closes, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_esc_closes", timeoutSec: 900 }
  - { name: ui_cleanup_b3_backdrop_click_closes, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_backdrop_click_closes", timeoutSec: 900 }
  - { name: ui_cleanup_b3_x_button_closes, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_x_button_closes", timeoutSec: 900 }
  - { name: ui_cleanup_b3_focus_starts_on_the_requested_section, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_focus_starts_on_the_requested_section", timeoutSec: 900 }
  - { name: ui_cleanup_b3_focus_returns_to_the_opener, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_focus_returns_to_the_opener", timeoutSec: 900 }
  - { name: ui_cleanup_b3_modal_variant_has_no_back_row_or_window_controls, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_modal_variant_has_no_back_row_or_window_controls", timeoutSec: 900 }
  - { name: ui_cleanup_b3_sheet_classes_at_small_sizes, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_sheet_classes_at_small_sizes", timeoutSec: 900 }
  - { name: ui_cleanup_b3_narrow_keeps_list_to_detail, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_narrow_keeps_list_to_detail", timeoutSec: 900 }
  - { name: ui_cleanup_b3_narrow_esc_pops_to_the_list, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-dialog.test.tsx -t ui_cleanup_b3_narrow_esc_pops_to_the_list", timeoutSec: 900 }
  - { name: ui_cleanup_b3_triggers_open_the_modal, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_triggers_open_the_modal", timeoutSec: 900 }
  - { name: ui_cleanup_b3_deep_link_lands_on_the_section, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_deep_link_lands_on_the_section", timeoutSec: 900 }
  - { name: ui_cleanup_b3_cmd_comma_toggles_closed, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_cmd_comma_toggles_closed", timeoutSec: 900 }
  - { name: ui_cleanup_b3_workspace_stays_mounted_and_draft_survives, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_workspace_stays_mounted_and_draft_survives", timeoutSec: 900 }
  - { name: ui_cleanup_b3_restriction_active_while_open_and_released_on_close, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_restriction_active_while_open_and_released_on_close", timeoutSec: 900 }
  - { name: ui_cleanup_b3_task_window_refuses_settings, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_task_window_refuses_settings", timeoutSec: 900 }
  - { name: ui_cleanup_b3_overlay_closes_before_settings_opens, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_overlay_closes_before_settings_opens", timeoutSec: 900 }
  - { name: ui_cleanup_b3_settings_is_not_a_history_entry, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_settings_is_not_a_history_entry", timeoutSec: 900 }
  - { name: ui_cleanup_b3_palette_task_pick_closes_settings, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_palette_task_pick_closes_settings", timeoutSec: 900 }
  - { name: ui_cleanup_b3_unmount_while_open_releases_the_page_lock, run: "cd web/packages/ui && bunx vitest run src/App.settings-modal.test.tsx -t ui_cleanup_b3_unmount_while_open_releases_the_page_lock", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- mobile internal cmd)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/App.tsx, change: "openSettings also sets a non-workspace activeView so the content region goes invisible and inert", expectFail: [ui_cleanup_b3_workspace_stays_mounted_and_draft_survives] }
  - { id: M2, file: web/packages/ui/src/components/settings/settings-dialog.tsx, change: "the dialog is rendered with modal={false}", expectFail: [ui_cleanup_b3_dialog_is_modal_and_traps_focus] }
  - { id: M3, file: web/packages/ui/src/App.tsx, change: "the keyboard restriction argument ignores settingsOpen going false (always on)", expectFail: [ui_cleanup_b3_restriction_active_while_open_and_released_on_close] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# UI cleanup B3 validation contract

Spec: `docs/plans/active/ui-cleanup-b3.md`. Web only. Frames are not validator frames: the Lead captures them (REAL-WINDOW or WEB-HARNESS, labelled) under the dogfood root and cites them in the PR.
