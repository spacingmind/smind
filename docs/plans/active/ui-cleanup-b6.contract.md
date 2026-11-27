---
v: 1
milestone: "B6: UI for existing RPCs (relay devices, doctor, push, automations relay tests)"
base: origin/develop
planRef: docs/plans/active/ui-cleanup-b6.md
acs:
  - id: AC1
    text: "Device list renders every state, connected flag, last seen, push flag; empty state; no key bytes."
    covers: { tests: [relay_devices_list_renders_all_states, relay_devices_empty_state, relay_devices_never_render_key_bytes, UC6-02], frames: [] }
  - id: AC2
    text: "Approve and Revoke confirm by device name, then call exactly once; Cancel sends nothing."
    covers: { tests: [relay_devices_approve_confirms_then_calls, relay_devices_approve_cancel_sends_nothing, relay_devices_revoke_confirms_then_calls, UC6-02], frames: [] }
  - id: AC3
    text: "Rename sends {id,label}; empty label refused."
    covers: { tests: [relay_devices_rename_sends_label, relay_devices_rename_empty_label_refused], frames: [] }
  - id: AC4
    text: "Pairing window control shows remaining slots and expiry."
    covers: { tests: [relay_devices_pairing_window_opens_and_shows_remaining, relay_devices_one_list_call_on_mount_and_after_a_change], frames: [] }
  - id: AC5
    text: "Relay connection and forbidden answer: read-only copy, no buttons, no call from a known relay connection."
    covers: { tests: [relay_devices_relay_connection_is_read_only_and_makes_no_call, relay_devices_forbidden_answer_becomes_read_only_copy, relay_devices_no_bridge_says_relay_not_set_up, relay_devices_error_is_a_sentence, UC6-03], frames: [] }
  - id: AC6
    text: "Doctor: once per click, six counts, error sentence, no polling."
    covers: { tests: [doctor_runs_once_per_click, doctor_shows_the_six_counts, doctor_error_is_a_sentence, doctor_does_not_poll, doctor_button_disabled_while_running, doctor_relay_connection_is_read_only_and_makes_no_call, UC6-01], frames: [] }
  - id: AC7
    text: "Push: count, no-device is not an error, code sentences, no secrets, one call per click, read-only over relay."
    covers: { tests: [push_group_counts_registered_devices, push_test_with_no_device_is_not_an_error, push_test_results_map_codes_to_sentences, push_test_shows_no_secrets, push_test_one_call_per_click, push_group_relay_connection_is_read_only_and_makes_no_call, push_disabled_is_a_sentence, UC6-04], frames: [] }
  - id: AC8
    text: "A relay client keeps Disable; Enable/Resume refused with a sentence."
    covers: { tests: [automation_relay_client_can_disable_but_enable_and_resume_are_refused_with_a_sentence], frames: [] }
  - id: AC12
    text: "No daemon, wire or mobile change; hygiene green."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: relay_devices_list_renders_all_states, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_list_renders_all_states", timeoutSec: 900 }
  - { name: relay_devices_empty_state, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_empty_state", timeoutSec: 900 }
  - { name: relay_devices_never_render_key_bytes, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_never_render_key_bytes", timeoutSec: 900 }
  - { name: relay_devices_approve_confirms_then_calls, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_approve_confirms_then_calls", timeoutSec: 900 }
  - { name: relay_devices_approve_cancel_sends_nothing, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_approve_cancel_sends_nothing", timeoutSec: 900 }
  - { name: relay_devices_revoke_confirms_then_calls, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_revoke_confirms_then_calls", timeoutSec: 900 }
  - { name: relay_devices_rename_sends_label, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_rename_sends_label", timeoutSec: 900 }
  - { name: relay_devices_rename_empty_label_refused, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_rename_empty_label_refused", timeoutSec: 900 }
  - { name: relay_devices_pairing_window_opens_and_shows_remaining, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_pairing_window_opens_and_shows_remaining", timeoutSec: 900 }
  - { name: relay_devices_relay_connection_is_read_only_and_makes_no_call, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_relay_connection_is_read_only_and_makes_no_call", timeoutSec: 900 }
  - { name: relay_devices_forbidden_answer_becomes_read_only_copy, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_forbidden_answer_becomes_read_only_copy", timeoutSec: 900 }
  - { name: relay_devices_no_bridge_says_relay_not_set_up, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_no_bridge_says_relay_not_set_up", timeoutSec: 900 }
  - { name: relay_devices_error_is_a_sentence, run: "cd web/packages/ui && bunx vitest run src/components/settings/relay-devices-group.test.tsx -t relay_devices_error_is_a_sentence", timeoutSec: 900 }
  - { name: doctor_runs_once_per_click, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_runs_once_per_click", timeoutSec: 900 }
  - { name: doctor_shows_the_six_counts, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_shows_the_six_counts", timeoutSec: 900 }
  - { name: doctor_error_is_a_sentence, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_error_is_a_sentence", timeoutSec: 900 }
  - { name: doctor_does_not_poll, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_does_not_poll", timeoutSec: 900 }
  - { name: doctor_button_disabled_while_running, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_button_disabled_while_running", timeoutSec: 900 }
  - { name: doctor_relay_connection_is_read_only_and_makes_no_call, run: "cd web/packages/ui && bunx vitest run src/components/settings/diagnostics-group.test.tsx -t doctor_relay_connection_is_read_only_and_makes_no_call", timeoutSec: 900 }
  - { name: push_group_counts_registered_devices, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_group_counts_registered_devices", timeoutSec: 900 }
  - { name: push_test_with_no_device_is_not_an_error, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_test_with_no_device_is_not_an_error", timeoutSec: 900 }
  - { name: push_test_results_map_codes_to_sentences, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_test_results_map_codes_to_sentences", timeoutSec: 900 }
  - { name: push_test_shows_no_secrets, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_test_shows_no_secrets", timeoutSec: 900 }
  - { name: push_test_one_call_per_click, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_test_one_call_per_click", timeoutSec: 900 }
  - { name: push_group_relay_connection_is_read_only_and_makes_no_call, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_group_relay_connection_is_read_only_and_makes_no_call", timeoutSec: 900 }
  - { name: push_disabled_is_a_sentence, run: "cd web/packages/ui && bunx vitest run src/components/settings/mobile-push-group.test.tsx -t push_disabled_is_a_sentence", timeoutSec: 900 }
  - { name: relay_devices_one_list_call_on_mount_and_after_a_change, run: "cd web/packages/ui && bunx vitest run src/hooks/use-relay-devices.test.tsx -t relay_devices_one_list_call_on_mount_and_after_a_change", timeoutSec: 900 }
  - { name: automation_relay_client_can_disable_but_enable_and_resume_are_refused_with_a_sentence, run: "cd web/packages/ui && bunx vitest run src/components/automations/automations-page.test.tsx -t automation_relay_client_can_disable_but_enable_and_resume_are_refused_with_a_sentence", timeoutSec: 900 }
  - { name: UC6-01, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/ui-cleanup-b6.spec.ts -g UC6-01 --project=light-1280", timeoutSec: 1200 }
  - { name: UC6-02, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/ui-cleanup-b6.spec.ts -g UC6-02 --project=light-1280", timeoutSec: 1200 }
  - { name: UC6-03, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/ui-cleanup-b6.spec.ts -g UC6-03 --project=light-1280", timeoutSec: 1200 }
  - { name: UC6-04, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/ui-cleanup-b6.spec.ts -g UC6-04 --project=light-1280", timeoutSec: 1200 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal cmd mobile)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/settings/relay-devices-group.tsx, change: "approve is sent without the confirm", expectFail: [relay_devices_approve_confirms_then_calls] }
  - { id: M2, file: web/packages/ui/src/components/settings/relay-devices-group.tsx, change: "the read-only (relay) view renders the Approve button / makes the call", expectFail: [relay_devices_relay_connection_is_read_only_and_makes_no_call] }
  - { id: M3, file: web/packages/ui/src/components/settings/diagnostics-group.tsx, change: "doctor polls (a re-run on an interval)", expectFail: [doctor_does_not_poll] }
  - { id: M4, file: web/packages/ui/src/components/settings/relay-devices-group.tsx, change: "a key byte (device_pub) is rendered", expectFail: [relay_devices_never_render_key_bytes] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# B6 validation contract

Spec: `docs/plans/active/ui-cleanup-b6.md`. Web only, no new RPC. e2e names are the UC6-* cases of `web/e2e/specs/ui-cleanup-b6.spec.ts` (relay.device.* and push are stubbed there through `page.routeWebSocket`, labelled in the spec). Frames are captured by the Lead and labelled REAL-WINDOW or WEB-HARNESS in the PR body.
