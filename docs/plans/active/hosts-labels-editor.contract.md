---
v: 1
milestone: "Fleet follow-up: Hosts labels editor (Settings -> Hosts): Labels row with chips and an Edit labels dialog over fleet.host.labels"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: LE1
    text: "An approved host's row shows a Labels line: its labels as key:value chips sorted by key, or a hint when it has none, and an Edit labels button. Pending and revoked hosts have neither."
    covers: { tests: [labels_row_shows_chips_and_offers_edit_for_approved_hosts_only], frames: [] }
  - id: LE2
    text: "Save sends only the difference through fleet.host.labels (changed or new pairs in set, dropped keys in remove) and the row shows the result; cancelling or closing sends nothing and drops the edits."
    covers: { tests: [labels_diff_sends_only_changes, labels_edit_sends_only_the_difference, labels_edits_are_dropped_on_close, labels_cancel_sends_nothing], frames: [] }
  - id: LE3
    text: "The dialog mirrors the daemon rules (registry.ValidateLabels): at most 16, key and value patterns, no secret-looking key, no duplicate key. The messages are the daemon's text; an invalid row disables Save; at 16 Add is disabled."
    covers: { tests: [labels_validation_matches_daemon_table, labels_messages_equal_daemon_text, labels_invalid_rows_block_save_and_say_why, labels_cap_of_16_disables_add], frames: [] }
  - id: LE4
    text: "A refusal from the daemon is shown in the dialog as received and the dialog stays open."
    covers: { tests: [labels_daemon_refusal_is_shown_and_keeps_the_dialog], frames: [] }
tests:
  - { name: labels_row_shows_chips_and_offers_edit_for_approved_hosts_only, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_row_shows_chips", timeoutSec: 900 }
  - { name: labels_edit_sends_only_the_difference, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_edit_sends_only_the_difference", timeoutSec: 900 }
  - { name: labels_edits_are_dropped_on_close, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_edits_are_dropped_on_close", timeoutSec: 900 }
  - { name: labels_cancel_sends_nothing, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_cancel_sends_nothing", timeoutSec: 900 }
  - { name: labels_invalid_rows_block_save_and_say_why, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_invalid_rows_block_save", timeoutSec: 900 }
  - { name: labels_cap_of_16_disables_add, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_cap_of_16", timeoutSec: 900 }
  - { name: labels_daemon_refusal_is_shown_and_keeps_the_dialog, run: "cd web/packages/ui && bunx vitest run src/components/hosts/edit-labels-dialog.test.tsx -t labels_daemon_refusal", timeoutSec: 900 }
  - { name: labels_validation_matches_daemon_table, run: "cd web/packages/ui && bunx vitest run src/lib/fleet-labels.test.ts -t labels_validation_matches_daemon_table", timeoutSec: 900 }
  - { name: labels_messages_equal_daemon_text, run: "cd web/packages/ui && bunx vitest run src/lib/fleet-labels.test.ts -t labels_messages_equal_daemon_text", timeoutSec: 900 }
  - { name: labels_diff_sends_only_changes, run: "cd web/packages/ui && bunx vitest run src/lib/fleet-labels.test.ts -t labels_diff_sends_only_changes", timeoutSec: 900 }
mutations:
  - { id: L1, file: web/packages/ui/src/lib/fleet-labels.ts, change: "MAX_HOST_LABELS is 17", expectFail: [labels_validation_matches_daemon_table, labels_cap_of_16_disables_add] }
  - { id: L2, file: web/packages/ui/src/lib/fleet-labels.ts, change: "the duplicate-key check is removed", expectFail: [labels_invalid_rows_block_save_and_say_why] }
  - { id: L3, file: web/packages/ui/src/lib/fleet-labels.ts, change: "the secret-word list is empty", expectFail: [labels_validation_matches_daemon_table] }
  - { id: L4, file: web/packages/ui/src/lib/fleet-labels.ts, change: "diffLabels sends every row in set", expectFail: [labels_diff_sends_only_changes, labels_edit_sends_only_the_difference] }
  - { id: L5, file: web/packages/ui/src/components/hosts/edit-labels-dialog.tsx, change: "a save error is swallowed and the dialog closes", expectFail: [labels_daemon_refusal_is_shown_and_keeps_the_dialog] }
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# Hosts labels editor validation contract

UI only; the daemon side (`fleet.host.labels`, `registry.ValidateLabels`) is merged and unchanged. The daemon stays the authority: the client rules only say what is wrong before sending.
Frames are captured by the Lead with `playwright-core` against an isolated daemon with the fleet methods stubbed.
