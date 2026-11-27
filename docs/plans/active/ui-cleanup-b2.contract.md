---
v: 1
milestone: "UI cleanup B2: Settings consistency, one layout and one column"
base: origin/develop
planRef: docs/plans/active/ui-cleanup-b2.md
acs:
  - id: AC1
    text: "Every registered Settings section renders in the same max-w-3xl column."
    covers: { tests: [ui_cleanup_b2_one_column_width_for_every_section], frames: [] }
  - id: AC2
    text: "The nav has Agents and Safety & usage with the D2 members and order; no Profiles & providers; Connection unchanged."
    covers: { tests: [ui_cleanup_b2_nav_groups], frames: [] }
  - id: AC3
    text: "SettingsList and SettingsListRow exist with the D3 classes and Profiles use them (MCP and Plugins covered by their Switch tests)."
    covers: { tests: [ui_cleanup_b2_settings_list_is_one_divided_card, ui_cleanup_b2_settings_list_row_padding, ui_cleanup_b2_profiles_use_settings_list], frames: [] }
  - id: AC4
    text: "Policy, Saved grants and Usage use the shared primitives."
    covers: { tests: [ui_cleanup_b2_policy_uses_shared_primitives, ui_cleanup_b2_saved_grants_one_group_row_per_grant, ui_cleanup_b2_usage_uses_shared_primitives], frames: [] }
  - id: AC5
    text: "GitHub, Hosts, Daemon, Browser, Shortcuts and Automations follow the per-tab list."
    covers: { tests: [ui_cleanup_b2_github_rows_use_settings_rows, ui_cleanup_b2_hosts_row_is_responsive, ui_cleanup_b2_daemon_gap_is_six, ui_cleanup_b2_browser_gap_is_six, ui_cleanup_b2_shortcuts_in_card_shell, ui_cleanup_b2_automations_is_a_group_and_row], frames: [] }
  - id: AC6
    text: "Connections toggle, MCP and Plugins enabled state are Switch controls; the removal confirm is a ConfirmDialog."
    covers: { tests: [ui_cleanup_b2_connections_uses_switch_and_confirm_dialog, ui_cleanup_b2_mcp_enabled_is_a_switch, ui_cleanup_b2_plugins_enabled_is_a_switch], frames: [] }
  - id: AC7
    text: "The drift test passes on the final tree and fails on a hand-rolled card or raw h3."
    covers: { tests: [ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3], frames: [] }
  - id: AC9
    text: "No mobile, internal or cmd change; public text rules hold."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: ui_cleanup_b2_settings_list_is_one_divided_card, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-layout.test.tsx -t ui_cleanup_b2_settings_list_is_one_divided_card", timeoutSec: 900 }
  - { name: ui_cleanup_b2_settings_list_row_padding, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-layout.test.tsx -t ui_cleanup_b2_settings_list_row_padding", timeoutSec: 900 }
  - { name: ui_cleanup_b2_one_column_width_for_every_section, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-screen.test.tsx -t ui_cleanup_b2_one_column_width_for_every_section", timeoutSec: 900 }
  - { name: ui_cleanup_b2_nav_groups, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-screen.test.tsx -t ui_cleanup_b2_nav_groups", timeoutSec: 900 }
  - { name: ui_cleanup_b2_policy_uses_shared_primitives, run: "cd web/packages/ui && bunx vitest run src/components/settings/policy-section.test.tsx -t ui_cleanup_b2_policy_uses_shared_primitives", timeoutSec: 900 }
  - { name: ui_cleanup_b2_saved_grants_one_group_row_per_grant, run: "cd web/packages/ui && bunx vitest run src/components/settings/saved-grants-section.test.tsx -t ui_cleanup_b2_saved_grants_one_group_row_per_grant", timeoutSec: 900 }
  - { name: ui_cleanup_b2_usage_uses_shared_primitives, run: "cd web/packages/ui && bunx vitest run src/components/settings/usage-section.test.tsx -t ui_cleanup_b2_usage_uses_shared_primitives", timeoutSec: 900 }
  - { name: ui_cleanup_b2_github_rows_use_settings_rows, run: "cd web/packages/ui && bunx vitest run src/components/settings/github-section.test.tsx -t ui_cleanup_b2_github_rows_use_settings_rows", timeoutSec: 900 }
  - { name: ui_cleanup_b2_hosts_row_is_responsive, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t ui_cleanup_b2_hosts_row_is_responsive", timeoutSec: 900 }
  - { name: ui_cleanup_b2_connections_uses_switch_and_confirm_dialog, run: "cd web/packages/ui && bunx vitest run src/components/settings/connections-section.test.tsx -t ui_cleanup_b2_connections_uses_switch_and_confirm_dialog", timeoutSec: 900 }
  - { name: ui_cleanup_b2_daemon_gap_is_six, run: "cd web/packages/ui && bunx vitest run src/components/settings/daemon-section.test.tsx -t ui_cleanup_b2_daemon_gap_is_six", timeoutSec: 900 }
  - { name: ui_cleanup_b2_browser_gap_is_six, run: "cd web/packages/ui && bunx vitest run src/components/settings/browser-section.test.tsx -t ui_cleanup_b2_browser_gap_is_six", timeoutSec: 900 }
  - { name: ui_cleanup_b2_shortcuts_in_card_shell, run: "cd web/packages/ui && bunx vitest run src/components/settings/shortcuts-section.test.tsx -t ui_cleanup_b2_shortcuts_in_card_shell", timeoutSec: 900 }
  - { name: ui_cleanup_b2_automations_is_a_group_and_row, run: "cd web/packages/ui && bunx vitest run src/components/settings/automations-link-section.test.tsx -t ui_cleanup_b2_automations_is_a_group_and_row", timeoutSec: 900 }
  - { name: ui_cleanup_b2_mcp_enabled_is_a_switch, run: "cd web/packages/ui && bunx vitest run src/components/settings/mcp-servers-section.test.tsx -t ui_cleanup_b2_mcp_enabled_is_a_switch", timeoutSec: 900 }
  - { name: ui_cleanup_b2_plugins_enabled_is_a_switch, run: "cd web/packages/ui && bunx vitest run src/components/settings/plugins-section.test.tsx -t ui_cleanup_b2_plugins_enabled_is_a_switch", timeoutSec: 900 }
  - { name: ui_cleanup_b2_profiles_use_settings_list, run: "cd web/packages/ui && bunx vitest run src/components/settings/profiles-section.test.tsx -t ui_cleanup_b2_profiles_use_settings_list", timeoutSec: 900 }
  - { name: ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3, run: "cd web/packages/ui && bunx vitest run src/components/settings/settings-drift.test.ts -t ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- mobile internal cmd)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/settings/saved-grants-section.tsx, change: "a hand-rolled rounded-xl border bg-card div is added around the grants", expectFail: [ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3] }
  - { id: M2, file: web/packages/ui/src/components/settings/settings-screen.tsx, change: "the content column class is narrowed to max-w-[510px] for the general section", expectFail: [ui_cleanup_b2_one_column_width_for_every_section] }
  - { id: M3, file: web/packages/ui/src/components/settings/mcp-servers-section.tsx, change: "the enabled Switch is replaced by a Checkbox", expectFail: [ui_cleanup_b2_mcp_enabled_is_a_switch] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# UI cleanup B2 validation contract

Spec: `docs/plans/active/ui-cleanup-b2.md`. Web only. Frames are not validator frames: the Lead captures them (REAL-WINDOW or WEB-HARNESS, labelled) under the dogfood root and cites them in the PR.
