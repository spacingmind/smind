---
v: 1
milestone: "C1: the Add project dialog restructure"
base: origin/develop
planRef: docs/plans/active/create-project-c1.md
acs:
  - id: AC1
    text: "The Add project modal replaces the form; all six entry points open it."
    covers: { tests: [add_project_all_six_entry_points_open_the_new_modal, UC-C1-01], frames: [] }
  - id: AC2
    text: "Host chip for one host; rows with disabled fleet hosts and non-switchable saved connections otherwise."
    covers: { tests: [add_project_host_chip_shows_the_current_connection, add_project_one_host_shows_only_the_chip, add_project_fleet_rows_are_disabled_with_the_reason, add_project_saved_connections_are_listed_not_switchable], frames: [] }
  - id: AC3
    text: "Only Open a folder is live."
    covers: { tests: [add_project_only_open_a_folder_is_live], frames: [] }
  - id: AC4
    text: "Recents per host, persisted, keep removed projects, remove does not delete."
    covers: { tests: [project_recents_are_kept_per_host, project_recents_keep_removed_projects, project_recents_remove_does_not_delete_the_project, UC-C1-03], frames: [] }
  - id: AC5
    text: "Non-git hint before submit with no git-init action; missing folder said."
    covers: { tests: [add_project_non_git_hint_before_submit_and_no_git_init_action, add_project_missing_folder_is_said_before_submit, UC-C1-04], frames: [] }
  - id: AC6
    text: "Duplicate path finds and opens the existing project; no workspace.create."
    covers: { tests: [add_project_duplicate_path_switches_instead_of_creating, UC-C1-05], frames: [] }
  - id: AC7
    text: "~ expanded client-side; other relative paths hinted."
    covers: { tests: [add_project_tilde_is_expanded, add_project_relative_path_gets_a_hint, UC-C1-06], frames: [] }
  - id: AC8
    text: "After create: expanded, in view, Start a task offered."
    covers: { tests: [add_project_after_create_selects_and_offers_start_a_task, UC-C1-02], frames: [] }
  - id: AC9
    text: "Errors are sentences, dialog stays open."
    covers: { tests: [add_project_errors_are_sentences_and_the_dialog_stays_open, UC-C1-07], frames: [] }
  - id: AC10
    text: "800x520 is the full-bleed sheet."
    covers: { tests: [add_project_small_window_is_a_full_bleed_sheet, UC-C1-08], frames: [] }
  - id: AC12
    text: "No daemon, wire or mobile change; hygiene green."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: add_project_host_chip_shows_the_current_connection, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_host_chip_shows_the_current_connection", timeoutSec: 900 }
  - { name: add_project_one_host_shows_only_the_chip, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_one_host_shows_only_the_chip", timeoutSec: 900 }
  - { name: add_project_fleet_rows_are_disabled_with_the_reason, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_fleet_rows_are_disabled_with_the_reason", timeoutSec: 900 }
  - { name: add_project_saved_connections_are_listed_not_switchable, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_saved_connections_are_listed_not_switchable", timeoutSec: 900 }
  - { name: add_project_only_open_a_folder_is_live, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_only_open_a_folder_is_live", timeoutSec: 900 }
  - { name: add_project_non_git_hint_before_submit_and_no_git_init_action, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_non_git_hint_before_submit_and_no_git_init_action", timeoutSec: 900 }
  - { name: add_project_missing_folder_is_said_before_submit, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_missing_folder_is_said_before_submit", timeoutSec: 900 }
  - { name: add_project_duplicate_path_switches_instead_of_creating, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_duplicate_path_switches_instead_of_creating", timeoutSec: 900 }
  - { name: add_project_tilde_is_expanded, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_tilde_is_expanded", timeoutSec: 900 }
  - { name: add_project_relative_path_gets_a_hint, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_relative_path_gets_a_hint", timeoutSec: 900 }
  - { name: add_project_after_create_selects_and_offers_start_a_task, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_after_create_selects_and_offers_start_a_task", timeoutSec: 900 }
  - { name: add_project_errors_are_sentences_and_the_dialog_stays_open, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_errors_are_sentences_and_the_dialog_stays_open", timeoutSec: 900 }
  - { name: add_project_small_window_is_a_full_bleed_sheet, run: "cd web/packages/ui && bunx vitest run src/components/add-project/add-project-dialog.test.tsx -t add_project_small_window_is_a_full_bleed_sheet", timeoutSec: 900 }
  - { name: project_recents_are_kept_per_host, run: "cd web/packages/ui && bunx vitest run src/lib/project-recents.test.ts -t project_recents_are_kept_per_host", timeoutSec: 900 }
  - { name: project_recents_keep_removed_projects, run: "cd web/packages/ui && bunx vitest run src/lib/project-recents.test.ts -t project_recents_keep_removed_projects", timeoutSec: 900 }
  - { name: project_recents_remove_does_not_delete_the_project, run: "cd web/packages/ui && bunx vitest run src/lib/project-recents.test.ts -t project_recents_remove_does_not_delete_the_project", timeoutSec: 900 }
  - { name: add_project_all_six_entry_points_open_the_new_modal, run: "cd web/packages/ui && bunx vitest run src/components/add-project/entry-points.test.tsx -t add_project_all_six_entry_points_open_the_new_modal", timeoutSec: 900 }
  - { name: UC-C1-01, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-01 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-02, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-02 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-03, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-03 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-04, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-04 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-05, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-05 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-06, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-06 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-07, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-07 --project=light-1280", timeoutSec: 1200 }
  - { name: UC-C1-08, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/create-project-c1.spec.ts -g UC-C1-08 --project=light-1280", timeoutSec: 1200 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal cmd mobile)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/add-project/add-project-dialog.tsx, change: "a duplicate path calls workspace.create (find-or-switch removed)", expectFail: [add_project_duplicate_path_switches_instead_of_creating] }
  - { id: M2, file: web/packages/ui/src/components/add-project/open-folder-step.tsx, change: "the non-git hint is never shown", expectFail: [add_project_non_git_hint_before_submit_and_no_git_init_action] }
  - { id: M3, file: web/packages/ui/src/lib/project-recents.ts, change: "recents are not keyed per host", expectFail: [project_recents_are_kept_per_host] }
  - { id: M4, file: web/packages/ui/src/components/add-project/host-step.tsx, change: "a fleet row is enabled", expectFail: [add_project_fleet_rows_are_disabled_with_the_reason] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# C1 validation contract

Spec: `docs/plans/active/create-project-c1.md`. Web only. Frames are taken by the Lead (REAL-WINDOW), not by the validator.
