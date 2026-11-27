---
v: 1
milestone: "UI cleanup B1: application menu into the palette, redundant shortcut surfaces removed"
base: origin/develop
planRef: docs/plans/active/ui-cleanup-b1.md
acs:
  - id: AC1
    text: "On desktop the palette lists every native entry and each calls its own menuAction id."
    covers: { tests: [ui_cleanup_palette_lists_native_entries_on_desktop, ui_cleanup_palette_entry_runs_its_own_native_command], frames: [] }
  - id: AC2
    text: "Check for updates is Windows only; no entry when isDesktop is false."
    covers: { tests: [ui_cleanup_check_updates_only_on_windows, ui_cleanup_palette_entries_absent_on_web], frames: [] }
  - id: AC3
    text: "The renderer entries run their keyboard action with source menu."
    covers: { tests: [ui_cleanup_palette_renderer_entries_run_actions], frames: [] }
  - id: AC4
    text: "The Application menu is gone on every OS and Icons.menu is not in the registry."
    covers: { tests: [ui_cleanup_no_application_menu_and_no_help_icon, icon_registry_binds_each_concept], frames: [] }
  - id: AC5
    text: "The header has no keyboard icon; the ? keystroke still opens the shortcuts overlay action; the right cluster order holds."
    covers: { tests: [ui_cleanup_header_right_cluster_order, ui_cleanup_shortcuts_help_keystroke_still_opens_overlay], frames: [] }
  - id: AC6
    text: "The terminal key bar keeps its keyboard toggle with its own icon key."
    covers: { tests: [ui_cleanup_terminal_key_bar_keeps_its_keyboard_toggle], frames: [] }
  - id: AC7
    text: "The composer has no focus hint and its focus keystroke still works."
    covers: { tests: [ui_cleanup_composer_has_no_focus_hint, ui_cleanup_composer_focus_keystroke_still_focuses], frames: [] }
  - id: AC8
    text: "The toolbar plus is gone, the footer Add workspace opens the same dialog, the toolbar search toggle and automation filter remain."
    covers: { tests: [ui_cleanup_sidebar_plus_removed_footer_add_workspace_kept, ui_cleanup_sidebar_search_toggle_and_automation_filter_kept], frames: [] }
  - id: AC9
    text: "No mobile, internal or cmd change; public text rules hold."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: ui_cleanup_palette_lists_native_entries_on_desktop, run: "cd web/packages/ui && bunx vitest run src/palette/desktop-commands.test.ts -t ui_cleanup_palette_lists_native_entries_on_desktop", timeoutSec: 900 }
  - { name: ui_cleanup_palette_entry_runs_its_own_native_command, run: "cd web/packages/ui && bunx vitest run src/palette/desktop-commands.test.ts -t ui_cleanup_palette_entry_runs_its_own_native_command", timeoutSec: 900 }
  - { name: ui_cleanup_palette_entries_absent_on_web, run: "cd web/packages/ui && bunx vitest run src/palette/desktop-commands.test.ts -t ui_cleanup_palette_entries_absent_on_web", timeoutSec: 900 }
  - { name: ui_cleanup_check_updates_only_on_windows, run: "cd web/packages/ui && bunx vitest run src/palette/desktop-commands.test.ts -t ui_cleanup_check_updates_only_on_windows", timeoutSec: 900 }
  - { name: ui_cleanup_palette_renderer_entries_run_actions, run: "cd web/packages/ui && bunx vitest run src/palette/desktop-commands.test.ts -t ui_cleanup_palette_renderer_entries_run_actions", timeoutSec: 900 }
  - { name: ui_cleanup_no_application_menu_and_no_help_icon, run: "cd web/packages/ui && bunx vitest run src/components/app-header.test.tsx -t ui_cleanup_no_application_menu_and_no_help_icon", timeoutSec: 900 }
  - { name: ui_cleanup_header_right_cluster_order, run: "cd web/packages/ui && bunx vitest run src/components/app-header.test.tsx -t ui_cleanup_header_right_cluster_order", timeoutSec: 900 }
  - { name: icon_registry_binds_each_concept, run: "cd web/packages/ui && bunx vitest run src/lib/icons.test.ts", timeoutSec: 900 }
  - { name: ui_cleanup_shortcuts_help_keystroke_still_opens_overlay, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1-header.test.tsx -t ui_cleanup_shortcuts_help_keystroke_still_opens_overlay", timeoutSec: 900 }
  - { name: ui_cleanup_composer_focus_keystroke_still_focuses, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1.test.tsx -t ui_cleanup_composer_focus_keystroke_still_focuses", timeoutSec: 900 }
  - { name: ui_cleanup_composer_has_no_focus_hint, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1.test.tsx -t ui_cleanup_composer_has_no_focus_hint", timeoutSec: 900 }
  - { name: ui_cleanup_terminal_key_bar_keeps_its_keyboard_toggle, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1-header.test.tsx -t ui_cleanup_terminal_key_bar_keeps_its_keyboard_toggle", timeoutSec: 900 }
  - { name: ui_cleanup_sidebar_plus_removed_footer_add_workspace_kept, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1.test.tsx -t ui_cleanup_sidebar_plus_removed_footer_add_workspace_kept", timeoutSec: 900 }
  - { name: ui_cleanup_sidebar_search_toggle_and_automation_filter_kept, run: "cd web/packages/ui && bunx vitest run src/components/ui-cleanup-b1.test.tsx -t ui_cleanup_sidebar_search_toggle_and_automation_filter_kept", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- mobile internal cmd)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/palette/desktop-commands.ts, change: "the Reload entry calls the zoom-in native command", expectFail: [ui_cleanup_palette_entry_runs_its_own_native_command] }
  - { id: M2, file: web/packages/ui/src/palette/desktop-commands.ts, change: "the isDesktop gate is removed so the entries show on web", expectFail: [ui_cleanup_palette_entries_absent_on_web] }
  - { id: M3, file: web/packages/ui/src/keyboard/shortcuts.ts, change: "the shortcuts-help binding Shift+? is removed (as if it left with the icon)", expectFail: [ui_cleanup_shortcuts_help_keystroke_still_opens_overlay] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# UI cleanup B1 validation contract

Spec: `docs/plans/active/ui-cleanup-b1.md`. Web only. Frames are not validator frames: the Lead captures them (REAL-WINDOW or WEB-HARNESS, labelled) under the dogfood root and cites them in the PR.
