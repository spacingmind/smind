---
v: 1
milestone: "B5: UI switches for the global config keys (ADR-0062)"
base: origin/develop
planRef: docs/plans/active/config-switches-ui.md
acs:
  - id: AC1
    text: "A switch only when writable; read-only copy otherwise; no failed call from a read-only row."
    covers: { tests: [config_switch_shown_only_when_writable, config_read_only_copy_when_not_writable, config_no_call_attempted_from_a_read_only_row, UC5-06], frames: [] }
  - id: AC2
    text: "Env-overridden key: switch disabled, 'Overridden by SMIND_X', never the value."
    covers: { tests: [config_env_overridden_switch_disabled_without_the_value, UC5-07], frames: [] }
  - id: AC3
    text: "Confirm before turning on/resuming; exactly {key,value}; off and pause apply at once."
    covers: { tests: [config_confirm_before_set_no_call_before_the_confirm, config_set_sends_exactly_key_and_value, config_turning_off_and_pausing_need_no_confirm], frames: [] }
  - id: AC4
    text: "Restart notice for fleet and learning only."
    covers: { tests: [config_restart_notice_for_fleet_and_learning, UC5-03, UC5-04], frames: [] }
  - id: AC5
    text: "Refusal is a sentence and the switch returns to its real value."
    covers: { tests: [config_refusal_shown_as_sentence_and_switch_returns, config_get_error_shows_a_read_only_sentence, UC5-08], frames: [] }
  - id: AC6
    text: "Refetch on config.updated and the dependent lists."
    covers: { tests: [config_get_refetches_on_config_updated, config_get_is_one_shared_query], frames: [] }
  - id: AC7
    text: "browser.disabled and read-only keys never get a switch."
    covers: { tests: [config_browser_disabled_never_gets_a_switch], frames: [] }
  - id: AC8
    text: "The four homes carry the switches; Automations page has Pause/Resume."
    covers: { tests: [automations_settings_group_has_enable_and_pause_switches, automations_page_pause_and_resume, automations_off_card_has_the_switch, plugins_home_switch_replaces_the_alert, hosts_off_card_becomes_the_fleet_switch_with_restart_notice, learning_kill_switch_row_replaces_the_alert, UC5-05, hosts_fleet_switch_is_a_normal_card_at_the_top, hosts_state_line_off, hosts_state_line_pending_restart_after_turning_on, hosts_state_line_pending_restart_after_turning_off, hosts_state_line_running, hosts_empty_card_only_when_running_with_no_hosts, hosts_pending_restart_offers_open_daemon_settings_on_desktop_only, config_open_daemon_settings_looks_like_a_link], frames: [] }
  - id: AC9
    text: "Confirm sentences equal the daemon table."
    covers: { tests: [config_confirm_sentences_match_the_daemon_table], frames: [] }
  - id: AC10
    text: "Real daemon end to end: the config file changes and a comment survives."
    covers: { tests: [UC5-01, UC5-02], frames: [] }
  - id: AC12
    text: "No daemon, wire or mobile change; hygiene green."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: config_switch_shown_only_when_writable, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_switch_shown_only_when_writable", timeoutSec: 900 }
  - { name: config_read_only_copy_when_not_writable, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_read_only_copy_when_not_writable", timeoutSec: 900 }
  - { name: config_no_call_attempted_from_a_read_only_row, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_no_call_attempted_from_a_read_only_row", timeoutSec: 900 }
  - { name: config_env_overridden_switch_disabled_without_the_value, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_env_overridden_switch_disabled_without_the_value", timeoutSec: 900 }
  - { name: config_confirm_before_set_no_call_before_the_confirm, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_confirm_before_set_no_call_before_the_confirm", timeoutSec: 900 }
  - { name: config_set_sends_exactly_key_and_value, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_set_sends_exactly_key_and_value", timeoutSec: 900 }
  - { name: config_turning_off_and_pausing_need_no_confirm, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_turning_off_and_pausing_need_no_confirm", timeoutSec: 900 }
  - { name: config_restart_notice_for_fleet_and_learning, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_restart_notice_for_fleet_and_learning", timeoutSec: 900 }
  - { name: config_refusal_shown_as_sentence_and_switch_returns, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_refusal_shown_as_sentence_and_switch_returns", timeoutSec: 900 }
  - { name: config_get_error_shows_a_read_only_sentence, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_get_error_shows_a_read_only_sentence", timeoutSec: 900 }
  - { name: config_get_refetches_on_config_updated, run: "cd web/packages/ui && bunx vitest run src/hooks/use-config.test.tsx -t config_get_refetches_on_config_updated", timeoutSec: 900 }
  - { name: config_get_is_one_shared_query, run: "cd web/packages/ui && bunx vitest run src/hooks/use-config.test.tsx -t config_get_is_one_shared_query", timeoutSec: 900 }
  - { name: config_browser_disabled_never_gets_a_switch, run: "cd web/packages/ui && bunx vitest run src/lib/config-keys.test.ts -t config_browser_disabled_never_gets_a_switch", timeoutSec: 900 }
  - { name: config_confirm_sentences_match_the_daemon_table, run: "cd web/packages/ui && bunx vitest run src/lib/config-keys.test.ts -t config_confirm_sentences_match_the_daemon_table", timeoutSec: 900 }
  - { name: automations_settings_group_has_enable_and_pause_switches, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t automations_settings_group_has_enable_and_pause_switches", timeoutSec: 900 }
  - { name: automations_page_pause_and_resume, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t automations_page_pause_and_resume", timeoutSec: 900 }
  - { name: automations_off_card_has_the_switch, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t automations_off_card_has_the_switch", timeoutSec: 900 }
  - { name: plugins_home_switch_replaces_the_alert, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t plugins_home_switch_replaces_the_alert", timeoutSec: 900 }
  - { name: hosts_off_card_becomes_the_fleet_switch_with_restart_notice, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t hosts_off_card_becomes_the_fleet_switch_with_restart_notice", timeoutSec: 900 }
  - { name: learning_kill_switch_row_replaces_the_alert, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-homes.test.tsx -t learning_kill_switch_row_replaces_the_alert", timeoutSec: 900 }
  - { name: UC5-01, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-01 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-02, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-02 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-03, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-03 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-04, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-04 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-05, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-05 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-06, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-06 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-07, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-07 --project=light-1280", timeoutSec: 1200 }
  - { name: UC5-08, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/config-switches.spec.ts -g UC5-08 --project=light-1280", timeoutSec: 1200 }
  - { name: hosts_fleet_switch_is_a_normal_card_at_the_top, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_fleet_switch_is_a_normal_card_at_the_top", timeoutSec: 900 }
  - { name: hosts_state_line_off, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_state_line_off", timeoutSec: 900 }
  - { name: hosts_state_line_pending_restart_after_turning_on, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_state_line_pending_restart_after_turning_on", timeoutSec: 900 }
  - { name: hosts_state_line_pending_restart_after_turning_off, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_state_line_pending_restart_after_turning_off", timeoutSec: 900 }
  - { name: hosts_state_line_running, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_state_line_running", timeoutSec: 900 }
  - { name: hosts_empty_card_only_when_running_with_no_hosts, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_empty_card_only_when_running_with_no_hosts", timeoutSec: 900 }
  - { name: hosts_pending_restart_offers_open_daemon_settings_on_desktop_only, run: "cd web/packages/ui && bunx vitest run src/components/settings/hosts-section.test.tsx -t hosts_pending_restart_offers_open_daemon_settings_on_desktop_only", timeoutSec: 900 }
  - { name: config_open_daemon_settings_looks_like_a_link, run: "cd web/packages/ui && bunx vitest run src/components/settings/config-switch-row.test.tsx -t config_open_daemon_settings_looks_like_a_link", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal cmd mobile)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/settings/config-switch-row.tsx, change: "the switch is rendered when writable is false", expectFail: [config_switch_shown_only_when_writable, config_read_only_copy_when_not_writable] }
  - { id: M2, file: web/packages/ui/src/components/settings/config-switch-row.tsx, change: "config.set is sent without the confirm", expectFail: [config_confirm_before_set_no_call_before_the_confirm] }
  - { id: M3, file: web/packages/ui/src/lib/config-keys.ts, change: "browser.disabled is accepted as a switch key (guard removed)", expectFail: [config_browser_disabled_never_gets_a_switch] }
  - { id: M4, file: web/packages/ui/src/components/settings/config-switch-row.tsx, change: "the restart notice is dropped", expectFail: [config_restart_notice_for_fleet_and_learning] }
  - { id: M5, file: web/packages/ui/src/components/settings/config-switch-row.tsx, change: "an env-overridden key's switch is enabled", expectFail: [config_env_overridden_switch_disabled_without_the_value] }
  - { id: M6, file: web/packages/ui/src/lib/query/event-bridge.ts, change: "config.updated no longer invalidates", expectFail: [config_get_refetches_on_config_updated] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# B5 validation contract

Spec: `docs/plans/active/config-switches-ui.md`. Web only. e2e names are the UC5-* cases of `web/e2e/specs/config-switches.spec.ts`. Frames are captured by the Lead (REAL-WINDOW when the screen is unlocked), not by the validator.
