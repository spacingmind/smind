---
v: 1
milestone: "P0 A2: daemon-side git hardening (textconv, external diff, fsmonitor) at one shared helper"
base: origin/develop
planRef: docs/plans/active/p0-git-diff-hardening.md
acs:
  - id: AC1
    text: "gitsafe.Args: overrides first, diff-family flags, no duplicates, other verbs untouched."
    covers: { tests: [gitsafe_args_orders_overrides_and_diff_flags, gitsafe_args_finds_the_subcommand_behind_global_options, gitsafe_args_does_not_duplicate_or_touch_other_verbs, gitsafe_args_leaves_pathspecs_after_double_dash], frames: [] }
  - id: AC2
    text: "gitsafe.Env: external diff dropped, safe defaults, input untouched."
    covers: { tests: [gitsafe_env_drops_external_diff_and_sets_the_safe_defaults, gitsafe_env_does_not_mutate_its_input], frames: [] }
  - id: AC3
    text: "Each fixed git diff call site runs no textconv or external diff and gives the plain-repo output."
    covers: { tests: [taskdiff_does_not_run_textconv_or_external_diff, task_name_status_does_not_run_drivers, task_file_diff_does_not_run_drivers, staged_names_do_not_run_drivers, task_shortstat_does_not_run_drivers, archive_numstat_does_not_run_drivers, pr_shortstat_does_not_run_drivers, sync_conflict_names_do_not_run_drivers, steward_collect_does_not_run_drivers, arm_diff_does_not_run_drivers, gitstate_runner_does_not_run_drivers_or_fsmonitor], frames: [] }
  - id: AC4
    text: "status paths run no fsmonitor program."
    covers: { tests: [workspace_status_does_not_run_fsmonitor, validation_snapshot_does_not_run_fsmonitor, tripwire_snapshot_does_not_run_fsmonitor], frames: [] }
  - id: AC5
    text: "Choke points go through gitsafe."
    covers: { tests: [audit_choke_points_use_gitsafe], frames: [] }
  - id: AC6
    text: "The audit table covers every call site."
    covers: { tests: [audit_every_git_call_site_is_in_the_plan], frames: [] }
  - id: AC7
    text: "Hygiene and scope: daemon Go only."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: gitsafe_args_orders_overrides_and_diff_flags, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_args_orders_overrides_and_diff_flags -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitsafe_args_finds_the_subcommand_behind_global_options, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_args_finds_the_subcommand_behind_global_options -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitsafe_args_does_not_duplicate_or_touch_other_verbs, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_args_does_not_duplicate_or_touch_other_verbs -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitsafe_args_leaves_pathspecs_after_double_dash, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_args_leaves_pathspecs_after_double_dash -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitsafe_env_drops_external_diff_and_sets_the_safe_defaults, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_env_drops_external_diff_and_sets_the_safe_defaults -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitsafe_env_does_not_mutate_its_input, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run gitsafe_env_does_not_mutate_its_input -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: audit_every_git_call_site_is_in_the_plan, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run audit_every_git_call_site_is_in_the_plan -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: audit_choke_points_use_gitsafe, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ -run audit_choke_points_use_gitsafe -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: taskdiff_does_not_run_textconv_or_external_diff, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run taskdiff_does_not_run_textconv_or_external_diff -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: task_name_status_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run task_name_status_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: task_file_diff_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run task_file_diff_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: staged_names_do_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run staged_names_do_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: task_shortstat_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run task_shortstat_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: archive_numstat_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run archive_numstat_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: pr_shortstat_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run pr_shortstat_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: sync_conflict_names_do_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run sync_conflict_names_do_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: workspace_status_does_not_run_fsmonitor, run: "TMPDIR=/tmp/a2-tmp go test ./internal/workspace/ -run workspace_status_does_not_run_fsmonitor -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: steward_collect_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/steward/ -run steward_collect_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: validation_snapshot_does_not_run_fsmonitor, run: "TMPDIR=/tmp/a2-tmp go test ./internal/validation/ -run validation_snapshot_does_not_run_fsmonitor -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: tripwire_snapshot_does_not_run_fsmonitor, run: "TMPDIR=/tmp/a2-tmp go test ./internal/learning/probe/sandbox/ -run tripwire_snapshot_does_not_run_fsmonitor -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: arm_diff_does_not_run_drivers, run: "TMPDIR=/tmp/a2-tmp go test ./internal/learning/probe/arm/ -run arm_diff_does_not_run_drivers -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: gitstate_runner_does_not_run_drivers_or_fsmonitor, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitstate/ -run gitstate_runner_does_not_run_drivers_or_fsmonitor -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- web mobile desktop)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/gitsafe/gitsafe.go, change: "drop the --no-ext-diff insertion from Args (layer one only: the call sites still spell the flag)", expectFail: [gitsafe_args_orders_overrides_and_diff_flags] }
  - { id: M2, file: internal/gitsafe/gitsafe.go, change: "drop the --no-textconv insertion from Args (layer one only)", expectFail: [gitsafe_args_orders_overrides_and_diff_flags] }
  - { id: M3, file: internal/gitsafe/gitsafe.go, change: "drop the -c core.fsmonitor=false prefix", expectFail: [gitsafe_args_orders_overrides_and_diff_flags, workspace_status_does_not_run_fsmonitor, validation_snapshot_does_not_run_fsmonitor, tripwire_snapshot_does_not_run_fsmonitor] }
  - { id: M4, file: internal/gitsafe/gitsafe.go, change: "Env keeps GIT_EXTERNAL_DIFF", expectFail: [gitsafe_env_drops_external_diff_and_sets_the_safe_defaults] }
  - { id: M5, file: internal/validation/digest.go, change: "validation.gitCmd bypasses gitsafe", expectFail: [validation_snapshot_does_not_run_fsmonitor, audit_choke_points_use_gitsafe] }
  - { id: M6, file: internal/workspace/git.go, change: "remove BOTH layers at the exploitable sites: the --no-ext-diff/--no-textconv insertion in gitsafe.Args AND the spelled flags in taskDiff and taskFileDiff", expectFail: [taskdiff_does_not_run_textconv_or_external_diff, task_file_diff_does_not_run_drivers] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./internal/gitsafe/ ./internal/workspace/ ./internal/validation/ ./internal/steward/", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=/tmp/a2-tmp go test ./internal/gitsafe/ ./internal/workspace/ ./internal/steward/ ./internal/validation/ ./internal/gitstate/ ./internal/learning/... ./internal/taskrunner/ -count=1", timeoutSec: 1800 }
limits: { budgetUsd: 4, maxMinutes: 75 }
---

# P0 A2 validation contract

Spec: `docs/plans/active/p0-git-diff-hardening.md`. Each driver test builds a real git repository whose `.gitattributes` names a textconv driver and whose config sets `diff.external` and `core.fsmonitor` to programs that write a marker file; the assertion is the marker file is absent and the output equals a plain repository's.
