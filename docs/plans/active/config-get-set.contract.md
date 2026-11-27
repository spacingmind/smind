---
v: 1
milestone: "B4a: config.get and config.set (ADR-0062)"
base: origin/develop
planRef: docs/plans/active/config-get-set.md
acs:
  - id: AC1
    text: "config.get returns the per-key record, source and restart flags; unknown key errors; secrets absent; server.* loopback only."
    covers: { tests: [Test_config_get_returns_source_and_restart_flags, Test_config_get_secret_keys_absent, Test_config_get_hides_server_keys_over_relay, Test_config_allowlist_drift, Test_config_allowlist_defaults_match_the_built_in_defaults], frames: [] }
  - id: AC2
    text: "config.get refused to a run-scoped connection."
    covers: { tests: [Test_config_get_refuses_run_scoped_caller], frames: [] }
  - id: AC3
    text: "config.set refused for run-scoped, relay, non-loopback and plugin callers; human-only, not run-scoped/MCP, not on the fleet link."
    covers: { tests: [Test_config_set_refuses_run_scoped_caller, Test_config_set_refuses_relay_caller, Test_config_set_refuses_non_loopback_login, Test_config_set_refused_for_plugin_session, Test_config_methods_not_in_run_scoped_or_mcp, Test_config_set_not_served_on_the_fleet_link, Test_human_only_rpcs_drift, Test_human_only_table_names_config_set], frames: [] }
  - id: AC4
    text: "Only the five writable keys; read-only, secret, unknown, invalid and env-overridden are refused."
    covers: { tests: [Test_config_set_refuses_key_outside_allowlist, Test_config_set_refuses_read_only_key, Test_config_set_refuses_secret_keys, Test_config_set_refuses_invalid_value, Test_config_set_refuses_env_overridden_key], frames: [] }
  - id: AC5
    text: "Comment-preserving atomic write, serialised, value re-read after write."
    covers: { tests: [Test_config_set_preserves_comments_and_unrelated_keys, Test_config_set_concurrent_calls_do_not_lose_updates, Test_config_set_result_is_reread_after_write, Test_config_concurrent_writers_do_not_lose_updates, Test_config_set_audit_chain_is_consistent_under_concurrency], frames: [] }
  - id: AC6
    text: "Audit line for ok and every refusal with caller class; 0600; rotated; no secrets."
    covers: { tests: [Test_config_set_writes_audit_line_with_old_and_new, Test_config_refusal_is_audited_with_caller_class, Test_config_audit_file_is_private_and_rotates, Test_config_audit_never_contains_a_secret], frames: [] }
  - id: AC7
    text: "config.updated emitted on success only."
    covers: { tests: [Test_config_updated_event_emitted_on_success_only], frames: [] }
  - id: AC8
    text: "CLI known keys match the table."
    covers: { tests: [Test_config_cli_known_keys_match_the_table], frames: [] }
  - id: AC9
    text: "In an agent run config set|unset refused for every key, wrapped or not; get allowed; floor rule; refusal names the alternative."
    covers: { tests: [Test_config_cli_set_refused_in_agent_run, Test_config_cli_get_allowed_in_agent_run, Test_floor_denies_config_set_wrapped, Test_floor_allows_config_get_and_lookalikes, Test_config_cli_refusal_names_the_scratch_daemon_alternative], frames: [] }
  - id: AC10
    text: "agentSafeKeys is empty."
    covers: { tests: [Test_config_cli_key_filter_is_empty_by_default], frames: [] }
  - id: AC12
    text: "Doc comments corrected; hygiene green; no web/mobile/fleet change."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: Test_config_allowlist_drift, run: "go test ./internal/config -run '^Test_config_allowlist_drift$' -count=1", timeoutSec: 600 }
  - { name: Test_config_get_secret_keys_absent, run: "go test ./internal/config -run '^Test_config_get_secret_keys_absent$' -count=1", timeoutSec: 600 }
  - { name: Test_config_audit_file_is_private_and_rotates, run: "go test ./internal/config -run '^Test_config_audit_file_is_private_and_rotates$' -count=1", timeoutSec: 600 }
  - { name: Test_config_audit_never_contains_a_secret, run: "go test ./internal/config -run '^Test_config_audit_never_contains_a_secret$' -count=1", timeoutSec: 600 }
  - { name: Test_config_get_returns_source_and_restart_flags, run: "go test ./internal/wsapi -run '^Test_config_get_returns_source_and_restart_flags$' -count=1", timeoutSec: 600 }
  - { name: Test_config_get_hides_server_keys_over_relay, run: "go test ./internal/wsapi -run '^Test_config_get_hides_server_keys_over_relay$' -count=1", timeoutSec: 600 }
  - { name: Test_config_get_refuses_run_scoped_caller, run: "go test ./internal/wsapi -run '^Test_config_get_refuses_run_scoped_caller$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_run_scoped_caller, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_run_scoped_caller$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_relay_caller, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_relay_caller$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_non_loopback_login, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_non_loopback_login$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refused_for_plugin_session, run: "go test ./internal/wsapi -run '^Test_config_set_refused_for_plugin_session$' -count=1", timeoutSec: 600 }
  - { name: Test_config_methods_not_in_run_scoped_or_mcp, run: "go test ./internal/wsapi -run '^Test_config_methods_not_in_run_scoped_or_mcp$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_key_outside_allowlist, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_key_outside_allowlist$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_read_only_key, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_read_only_key$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_secret_keys, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_secret_keys$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_invalid_value, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_invalid_value$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_refuses_env_overridden_key, run: "go test ./internal/wsapi -run '^Test_config_set_refuses_env_overridden_key$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_preserves_comments_and_unrelated_keys, run: "go test ./internal/wsapi -run '^Test_config_set_preserves_comments_and_unrelated_keys$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_concurrent_calls_do_not_lose_updates, run: "go test ./internal/wsapi -race -run '^Test_config_set_concurrent_calls_do_not_lose_updates$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_result_is_reread_after_write, run: "go test ./internal/wsapi -run '^Test_config_set_result_is_reread_after_write$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_writes_audit_line_with_old_and_new, run: "go test ./internal/wsapi -run '^Test_config_set_writes_audit_line_with_old_and_new$' -count=1", timeoutSec: 600 }
  - { name: Test_config_refusal_is_audited_with_caller_class, run: "go test ./internal/wsapi -run '^Test_config_refusal_is_audited_with_caller_class$' -count=1", timeoutSec: 600 }
  - { name: Test_config_updated_event_emitted_on_success_only, run: "go test ./internal/wsapi -run '^Test_config_updated_event_emitted_on_success_only$' -count=1", timeoutSec: 600 }
  - { name: Test_human_only_rpcs_drift, run: "go test ./internal/wsapi -run '^Test_human_only_rpcs_drift$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_not_served_on_the_fleet_link, run: "go test ./internal/fleet/wire -run '^Test_config_set_not_served_on_the_fleet_link$' -count=1", timeoutSec: 600 }
  - { name: Test_human_only_table_names_config_set, run: "go test ./internal/permpolicy -run '^Test_human_only_table_names_config_set$' -count=1", timeoutSec: 600 }
  - { name: Test_config_cli_key_filter_is_empty_by_default, run: "go test ./internal/permpolicy -run '^Test_config_cli_key_filter_is_empty_by_default$' -count=1", timeoutSec: 600 }
  - { name: Test_floor_denies_config_set_wrapped, run: "go test ./internal/permpolicy -run '^Test_floor_denies_config_set_wrapped$' -count=1", timeoutSec: 600 }
  - { name: Test_config_cli_set_refused_in_agent_run, run: "go test ./cmd/smind -run '^Test_config_cli_set_refused_in_agent_run$' -count=1", timeoutSec: 600 }
  - { name: Test_config_cli_get_allowed_in_agent_run, run: "go test ./cmd/smind -run '^Test_config_cli_get_allowed_in_agent_run$' -count=1", timeoutSec: 600 }
  - { name: Test_config_cli_refusal_names_the_scratch_daemon_alternative, run: "go test ./cmd/smind -run '^Test_config_cli_refusal_names_the_scratch_daemon_alternative$' -count=1", timeoutSec: 600 }
  - { name: Test_config_cli_known_keys_match_the_table, run: "go test ./cmd/smind -run '^Test_config_cli_known_keys_match_the_table$' -count=1", timeoutSec: 600 }
  - { name: Test_config_allowlist_defaults_match_the_built_in_defaults, run: "go test ./internal/config -run '^Test_config_allowlist_defaults_match_the_built_in_defaults$' -count=1", timeoutSec: 600 }
  - { name: Test_config_concurrent_writers_do_not_lose_updates, run: "go test ./internal/config -race -run '^Test_config_concurrent_writers_do_not_lose_updates$' -count=1", timeoutSec: 600 }
  - { name: Test_config_set_audit_chain_is_consistent_under_concurrency, run: "go test ./internal/wsapi -race -run '^Test_config_set_audit_chain_is_consistent_under_concurrency$' -count=1", timeoutSec: 600 }
  - { name: Test_floor_allows_config_get_and_lookalikes, run: "go test ./internal/permpolicy -run '^Test_floor_allows_config_get_and_lookalikes$' -count=1", timeoutSec: 600 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- web mobile)\" && test -z \"$(git diff origin/develop...HEAD --name-only -- internal/fleet ':!*_test.go')\" && ! grep -rn \"no RPC, agent or relay\" internal/config internal/wsapi", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/wsapi/config_handlers.go, change: "the run-scoped guard is removed from config.set", expectFail: [Test_config_set_refuses_run_scoped_caller] }
  - { id: M2, file: internal/wsapi/config_handlers.go, change: "the relay (rc.local) guard is removed from config.set", expectFail: [Test_config_set_refuses_relay_caller] }
  - { id: M3, file: internal/wsapi/config_handlers.go, change: "the loopback channel guard is removed from config.set", expectFail: [Test_config_set_refuses_non_loopback_login] }
  - { id: M4, file: internal/wsapi/plugin_session.go, change: "config. is removed from pluginNeverPrefixes", expectFail: [Test_config_set_refused_for_plugin_session] }
  - { id: M5, file: internal/wsapi/config_handlers.go, change: "config.set accepts a read-only key", expectFail: [Test_config_set_refuses_read_only_key] }
  - { id: M6, file: internal/wsapi/config_handlers.go, change: "the env-overridden check is removed", expectFail: [Test_config_set_refuses_env_overridden_key] }
  - { id: M7, file: internal/wsapi/config_handlers.go, change: "the audit line is skipped on refusal", expectFail: [Test_config_refusal_is_audited_with_caller_class] }
  - { id: M8, file: internal/wsapi/config_handlers.go, change: "the handler mutex around read-old, write and audit is removed", expectFail: [Test_config_set_audit_chain_is_consistent_under_concurrency] }
  - { id: M8b, file: internal/config/edit.go, change: "the mutate mutex (every in-process writer) is removed", expectFail: [Test_config_concurrent_writers_do_not_lose_updates] }
  - { id: M9, file: internal/permpolicy/humanonly.go, change: "agentSafeKeys is made non-empty", expectFail: [Test_config_cli_key_filter_is_empty_by_default] }
  - { id: M10, file: internal/permpolicy/humanonly.go, change: "the config row is removed from humanOnlyCLI", expectFail: [Test_floor_denies_config_set_wrapped, Test_human_only_table_names_config_set, Test_config_cli_set_refused_in_agent_run] }
  - { id: M11, file: internal/wsapi/config_handlers.go, change: "server.* is returned to a non-loopback config.get", expectFail: [Test_config_get_hides_server_keys_over_relay] }
  - { id: M12, file: internal/wsapi/config_handlers.go, change: "the response returns the submitted value instead of the re-read one", expectFail: [Test_config_set_result_is_reread_after_write] }
frames: []
commands:
  - { id: C1, run: "go vet ./...", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=$(mktemp -d /tmp/b4a.XXXXXX) go test ./internal/config ./internal/wsapi ./internal/permpolicy ./internal/fleet/... ./cmd/smind -count=1", timeoutSec: 1800 }
  - { id: C3, run: "go test -race ./internal/config ./internal/permpolicy -count=1", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# B4a validation contract

Spec: `docs/plans/active/config-get-set.md`, ADR-0062. Test names are `Test_<name>` (the ADR names verbatim). HIGH-RISK: the guards are the point; every mutation must fail its named test.
