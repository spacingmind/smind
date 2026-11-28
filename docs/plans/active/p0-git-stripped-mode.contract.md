---
v: 1
milestone: "P0: daemon-initiated git runs stripped (hooks, filters, merge drivers, credential helpers)"
base: origin/develop
planRef: docs/plans/active/p0-git-stripped-mode.md
acs:
  - id: AC1
    text: "Stripped mode: no armed program runs for any daemon-initiated operation."
    covers: { tests: [real_git_daemon_mode_equals_in_a_subsection_never_runs_the_program, real_git_daemon_mode_never_runs_an_ext_transport_program, real_git_daemon_mode_does_not_run_a_submodule_filter, real_git_daemon_mode_a_required_filter_does_not_fail_add, gitstate_runner_runs_no_armed_program, validation_digest_runs_no_armed_program, execgit_picks_the_mode_from_the_context, real_git_daemon_mode_runs_no_armed_program, checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo, diff_snapshot_runs_no_filter, worktree_add_runs_no_post_checkout_hook, runner_agent_commit_runs_no_hook], frames: [] }
  - id: AC2
    text: "Human mode: the armed programs run for the operations the Human starts."
    covers: { tests: [real_git_human_mode_runs_the_armed_programs, human_commit_runs_the_pre_commit_hook, human_stage_runs_the_clean_filter, human_push_runs_the_pre_push_hook, human_update_from_base_runs_the_merge_driver], frames: [] }
  - id: AC3
    text: "Only repository-controlled config is neutralised; global config and exact LFS lines are kept."
    covers: { tests: [stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack, stripped_keeps_global_filter_and_credential_helper, stripped_local_override_of_a_global_filter_is_reasserted, stripped_trusts_exact_lfs_command_lines, stripped_credential_helper_reset_keeps_trusted_helpers, stripped_scan_parse_scope_and_origin, real_git_daemon_mode_keeps_a_global_filter], frames: [] }
  - id: AC4
    text: "The scan fails closed, exit 1 is no match, ref-only subcommands skip it, unknown ones are scanned."
    covers: { tests: [stripped_key_with_equals_in_the_subsection_fails_closed, stripped_scans_describe, stripped_scan_ignores_inherited_git_dir, stripped_scans_submodule_config_files, stripped_submodule_cap_fails_closed, stripped_neutralised_filter_is_not_required, stripped_remote_update_is_scanned_but_remote_get_url_is_not, stripped_cat_file_filters_is_scanned, stripped_neutralises_a_local_gpg_program, stripped_adds_pack_program_flags_for_fetch_ls_remote_push, stripped_scan_failure_fails_closed, stripped_scan_exit_one_is_no_match, stripped_skips_the_scan_for_ref_only_subcommands, stripped_scans_an_unknown_subcommand, stripped_args_static_prefix], frames: [] }
  - id: AC5
    text: "eol attributes show no spurious change in stripped mode."
    covers: { tests: [real_git_eol_attributes_do_not_show_spurious_changes], frames: [] }
  - id: AC6
    text: "The classification table covers every spawn; the audit catches a new one."
    covers: { tests: [audit_every_git_spawn_is_classified, audit_daemon_files_use_stripped_args, audit_human_variants_only_in_named_functions, audit_flags_a_new_unclassified_spawn], frames: [] }
  - id: AC7
    text: "The human RPCs mark the context only for a non-run-scoped caller; the runner commit is daemon-initiated."
    covers: { tests: [task_commit_handler_runs_the_pre_commit_hook_for_a_human_caller, task_stage_handler_runs_the_clean_filter_for_a_human_caller, task_commit_marks_human_only_for_a_non_run_scoped_caller, runner_agent_commit_runs_no_hook], frames: [] }
  - id: AC8
    text: "No UI, wire or mobile change; hygiene green."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: stripped_args_static_prefix, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_args_static_prefix$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scan_parse_scope_and_origin, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scan_parse_scope_and_origin$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_keeps_global_filter_and_credential_helper, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_keeps_global_filter_and_credential_helper$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_local_override_of_a_global_filter_is_reasserted, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_local_override_of_a_global_filter_is_reasserted$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_trusts_exact_lfs_command_lines, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_trusts_exact_lfs_command_lines$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_credential_helper_reset_keeps_trusted_helpers, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_credential_helper_reset_keeps_trusted_helpers$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scan_failure_fails_closed, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scan_failure_fails_closed$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scan_exit_one_is_no_match, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scan_exit_one_is_no_match$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_skips_the_scan_for_ref_only_subcommands, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_skips_the_scan_for_ref_only_subcommands$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scans_an_unknown_subcommand, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scans_an_unknown_subcommand$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_runs_no_armed_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_runs_no_armed_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_human_mode_runs_the_armed_programs, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_human_mode_runs_the_armed_programs$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_keeps_a_global_filter, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_keeps_a_global_filter$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_eol_attributes_do_not_show_spurious_changes, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_eol_attributes_do_not_show_spurious_changes$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: audit_every_git_spawn_is_classified, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_audit_every_git_spawn_is_classified$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: audit_daemon_files_use_stripped_args, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_audit_daemon_files_use_stripped_args$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: audit_human_variants_only_in_named_functions, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_audit_human_variants_only_in_named_functions$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: audit_flags_a_new_unclassified_spawn, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_audit_flags_a_new_unclassified_spawn$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: diff_snapshot_runs_no_filter, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_diff_snapshot_runs_no_filter$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: worktree_add_runs_no_post_checkout_hook, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_worktree_add_runs_no_post_checkout_hook$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: human_commit_runs_the_pre_commit_hook, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_human_commit_runs_the_pre_commit_hook$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: runner_agent_commit_runs_no_hook, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_runner_agent_commit_runs_no_hook$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: human_stage_runs_the_clean_filter, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_human_stage_runs_the_clean_filter$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: human_push_runs_the_pre_push_hook, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_human_push_runs_the_pre_push_hook$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: human_update_from_base_runs_the_merge_driver, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_human_update_from_base_runs_the_merge_driver$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: task_commit_marks_human_only_for_a_non_run_scoped_caller, run: "TMPDIR=/tmp/c5-tmp go test ./internal/wsapi/ -run \"^Test_task_commit_marks_human_only_for_a_non_run_scoped_caller$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: gitstate_runner_runs_no_armed_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitstate/ -run \"^Test_gitstate_runner_runs_no_armed_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: validation_digest_runs_no_armed_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/validation/ -run \"^Test_validation_digest_runs_no_armed_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: execgit_picks_the_mode_from_the_context, run: "TMPDIR=/tmp/c5-tmp go test ./internal/workspace/ -run \"^Test_execgit_picks_the_mode_from_the_context$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_remote_update_is_scanned_but_remote_get_url_is_not, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_remote_update_is_scanned_but_remote_get_url_is_not$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_cat_file_filters_is_scanned, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_cat_file_filters_is_scanned$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_neutralises_a_local_gpg_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_neutralises_a_local_gpg_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_adds_pack_program_flags_for_fetch_ls_remote_push, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_adds_pack_program_flags_for_fetch_ls_remote_push$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_key_with_equals_in_the_subsection_fails_closed, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_key_with_equals_in_the_subsection_fails_closed$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_equals_in_a_subsection_never_runs_the_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_equals_in_a_subsection_never_runs_the_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_never_runs_an_ext_transport_program, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_never_runs_an_ext_transport_program$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scans_describe, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scans_describe$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scan_ignores_inherited_git_dir, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scan_ignores_inherited_git_dir$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_scans_submodule_config_files, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_scans_submodule_config_files$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_submodule_cap_fails_closed, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_submodule_cap_fails_closed$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_does_not_run_a_submodule_filter, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_does_not_run_a_submodule_filter$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: stripped_neutralised_filter_is_not_required, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_stripped_neutralised_filter_is_not_required$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: real_git_daemon_mode_a_required_filter_does_not_fail_add, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/ -run \"^Test_real_git_daemon_mode_a_required_filter_does_not_fail_add$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: task_commit_handler_runs_the_pre_commit_hook_for_a_human_caller, run: "TMPDIR=/tmp/c5-tmp go test ./internal/wsapi/ -run \"^Test_task_commit_handler_runs_the_pre_commit_hook_for_a_human_caller$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: task_stage_handler_runs_the_clean_filter_for_a_human_caller, run: "TMPDIR=/tmp/c5-tmp go test ./internal/wsapi/ -run \"^Test_task_stage_handler_runs_the_clean_filter_for_a_human_caller$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- web mobile desktop)\"", timeoutSec: 1200 }
mutations:
  - { id: M1, file: internal/gitsafe/stripped.go, change: "drop core.hooksPath from the static prefix", expectFail: [real_git_daemon_mode_runs_no_armed_program] }
  - { id: M2, file: internal/gitsafe/stripped.go, change: "treat local scope as trusted (neutralise nothing)", expectFail: [real_git_daemon_mode_runs_no_armed_program, stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack] }
  - { id: M3, file: internal/workspace/git.go, change: "the human context is ignored (execGit is always stripped)", expectFail: [human_commit_runs_the_pre_commit_hook] }
  - { id: M4, file: internal/workspace/git.go, change: "execGit uses the baseline gitsafe.Args instead of the stripped mode", expectFail: [checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo] }
  - { id: M5, file: internal/gitsafe/stripped.go, change: "a scan error is swallowed (fail open)", expectFail: [stripped_scan_failure_fails_closed] }
  - { id: M6, file: docs/plans/active/p0-git-stripped-mode.md, change: "a classification row is removed from the table", expectFail: [audit_every_git_spawn_is_classified] }
  - { id: M7, file: internal/gitsafe/stripped.go, change: "the submodule config scan is skipped (submoduleEntries returns nothing)", expectFail: [real_git_daemon_mode_does_not_run_a_submodule_filter, stripped_scans_submodule_config_files] }
  - { id: M8, file: internal/gitsafe/stripped.go, change: "no protocol.ext.allow=never in the static prefix", expectFail: [real_git_daemon_mode_never_runs_an_ext_transport_program] }
  - { id: M9, file: internal/gitsafe/stripped.go, change: "a neutralised filter keeps required=true (no required=false override)", expectFail: [real_git_daemon_mode_a_required_filter_does_not_fail_add, stripped_neutralised_filter_is_not_required] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./internal/gitsafe/... ./internal/workspace/ ./internal/wsapi/ ./internal/gitstate/ ./internal/steward/ ./internal/validation/", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=/tmp/c5-tmp go test ./internal/gitsafe/... ./internal/workspace/ ./internal/gitstate/ ./internal/steward/ ./internal/validation/ ./internal/taskrunner/ ./internal/hygiene/ -count=1", timeoutSec: 2400 }
limits: { budgetUsd: 6, maxMinutes: 120 }
---

# P0 stripped git mode validation contract

Spec: `docs/plans/active/p0-git-stripped-mode.md`. Real git against a lab repository armed with marker-writing programs; no live provider call, no network (an unreachable ssh host and a local bare remote only).
