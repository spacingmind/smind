# B4a: `config.get` and `config.set` (ADR-0062)

Size L, **HIGH-RISK (security)**: a new human-only write path to the daemon's config. Spec: `docs/decisions/0062-config-get-set.md` (Accepted 2026-11-27, with the Human decisions Q1 to Q6); this plan does not re-decide it. Contract: `docs/plans/active/config-get-set.contract.md`. Daemon, CLI, floor and docs only: **no web/UI change** (B5 does the switches after this merges).

## Decisions (made here, within the ADR)

- **D1 One table.** `internal/config/allowlist.go` holds the one table (`ConfigKeys`): key, kind, default, `Writable`, `Restart`, `LoopbackOnly` (the `server.*` rows), the overriding environment variable, the confirmation sentence. `config.get`, `config.set` and the CLI key list all read it; `knownKeys` in `edit.go` is built from it plus `providers.<id>.defaultModel` and the secret `server.passwordHash` (CLI only: never in the RPC table). A drift test walks it.
- **D2 `learning.enabled` is restart.** The learning wiring captures its config at daemon start (`wireLearning`), so the table says `restart: true` and `config.get` reports it. Not re-read per decision: the ADR allows either, and a restart label that is true beats a live path that needs its own proof. ADR-0026's "instantly" is already amended to say so.
- **D3 Audit file.** `config-audit.jsonl` in the daemon home (`config.Home()`), mode 0600, append-only (`O_APPEND`), one JSON object per line `{time, key, old, new, outcome, channel, caller}`, rotated by size (1 MiB: `config-audit.jsonl` becomes `.1`, one generation kept). `caller` is a class: `human`, `run`, `relay`, `plugin`, `remote`. Values are logged as they are (no secret exists in the table; a value-redactor still runs, so a mistaken future key cannot log a credential).
- **D4 Guards before the handler body.** Order: not run-scoped, then `rc.local`, then channel loopback, then the table lookup. Each refusal is audited with its caller class. The plugin refusal comes from `pluginNeverPrefixes` (`config.`); that path logs through the same audit function.
- **D5 Mutex.** One package-level `sync.Mutex` in `internal/config` around `Set`/`Unset` read-modify-write of config.yaml for the daemon's own RPC (`SetLocked`); the handler takes it for read old, check env, write, re-read. The CLI process has its own file, an editor is the documented separate writer.
- **D6 `config.get` guard.** Any authenticated, non-run-scoped connection (relay and password sessions included); `server.*` rows only on a loopback channel (omitted from the default list for others, an error when named). Not in `RunScopedMethods`, not on the fleet link, `config.` under the plugin never-prefix.
- **D7 CLI in an agent run (Q6).** `humanOnlyCLI` gains `{path: [config], verbs: [set, unset], keyFilter: agentSafeKeys, bare: false}`; `agentSafeKeys` is an empty `map[string]string` (key to reason). `config get` and a bare `config` stay allowed. Refusal text names the scratch-daemon alternative: `SMIND_HOME=<dir> SMIND_LISTEN=127.0.0.1:<port> smind serve` (`smind serve` takes no flags; the port and address are environment).
- **D8 Event.** `config.updated {key}` is broadcast after a successful write, through the existing event bus.
- **D9 ADR-0056 limit** is stated in `public-docs/configuration.md` and the ADR's consequences, not claimed closed.

## Acceptance criteria

- AC1 `config.get` returns `{key, value, default, source, restart, writable}` per allowlisted key; `source` is `default`, `file` or `env` (variable name, never its value); unknown key is an error; secrets are absent; `server.*` only to loopback.
- AC2 `config.get` is refused to a run-scoped connection and allowed to relay and password sessions (minus `server.*`).
- AC3 `config.set` is refused for: run-scoped, relay, non-loopback (password session, plain or TLS), plugin session; it is in `HumanOnlyRPCs`, in no `RunScopedMethods`, not an MCP tool, not in the fleet wire namespace.
- AC4 `config.set` accepts only the five writable keys; read-only keys, secrets and unknown keys are refused; an invalid value is refused; an env-overridden key is refused with "overridden by SMIND_x, unset it first".
- AC5 The write is comment-preserving and atomic, re-reads inside a mutex, concurrent calls do not lose updates, and the response carries the value re-read after the write.
- AC6 Every attempt that reaches the guard or the handler appends one audit line (ok and each refusal class with caller class), 0600, rotated by size, no secrets.
- AC7 `config.updated {key}` is emitted on success only.
- AC8 `smind config get|set|unset` knownKeys include `fleet.enabled`, `plugins.enabled`, `learning.enabled`, `automations.paused` (parity with the table).
- AC9 In an agent run `smind config set|unset` is refused for every key, wrapped or not (`bash -c`, `env X=1`, `sh` heredoc, `xargs`, `command`, quoted binary); `config get` and bare `config` / `--help` stay allowed; the floor rule names it with `bare: false`; the refusal tells the scratch-daemon alternative.
- AC10 `agentSafeKeys` is empty and a test keeps it empty by default.
- AC11 Dogfood recipe sweep: nothing that tells an agent or Lead to run `smind config set|unset` remains (docs, role packs, skills, scripts); what changed is listed in Validation.
- AC12 Doc comments in `internal/config/{config,fleet}.go` and `internal/wsapi/{plugins,automations}.go` no longer say "no RPC".
- AC13 Hygiene green; no web, mobile or fleet-wire change.

## Named tests

Allowlist and get (`internal/config`, `internal/wsapi`): `config_allowlist_drift`, `config_get_returns_source_and_restart_flags`, `config_get_hides_server_keys_over_relay`, `config_get_refuses_run_scoped_caller`, `config_get_secret_keys_absent`.

Set and guards (`internal/wsapi/config_handlers_test.go`): `config_set_refuses_run_scoped_caller`, `config_set_refuses_relay_caller`, `config_set_refuses_non_loopback_login`, `config_set_refused_for_plugin_session`, `config_set_not_served_on_the_fleet_link`, `config_methods_not_in_run_scoped_or_mcp`, `config_set_refuses_key_outside_allowlist`, `config_set_refuses_read_only_key`, `config_set_refuses_secret_keys`, `config_set_refuses_invalid_value`, `config_set_refuses_env_overridden_key`, `config_set_preserves_comments_and_unrelated_keys`, `config_set_concurrent_calls_do_not_lose_updates` (run with `-race`), `config_set_result_is_reread_after_write`, `config_set_writes_audit_line_with_old_and_new`, `config_refusal_is_audited_with_caller_class`, `config_audit_file_is_private_and_rotates`, `config_audit_never_contains_a_secret`, `config_updated_event_emitted_on_success_only`.

Floor and CLI: `human_only_table_names_config_set` (`internal/permpolicy`, plus the `HumanOnlyRPCs` drift test in wsapi), `config_cli_set_refused_in_agent_run` (every `knownKeys` key, wrapped forms), `config_cli_get_allowed_in_agent_run`, `config_cli_key_filter_is_empty_by_default`, `config_cli_refusal_names_the_scratch_daemon_alternative`, `config_cli_known_keys_match_the_table`, `floor_denies_config_set_wrapped` (bash -c, env prefix, sh heredoc, quoted binary, `command`, `xargs`, computed binary, `npm config set` still allowed).

## Mutations (committed code; each must fail a named test)

M1 run-scoped guard removed from `config.set`; M2 relay guard removed; M3 loopback guard removed; M4 plugin never-prefix `config.` removed; M5 `config.set` accepts a read-only key; M6 env-overridden check removed; M7 audit skipped on refusal; M8 handler mutex removed (audit-chain concurrency test); M8b the `mutate` mutex of every in-process writer removed (writers race test); M9 `agentSafeKeys` made non-empty; M10 the floor row removed; M11 `server.*` returned to a non-loopback `config.get`; M12 the re-read after write replaced by the submitted value.

Lead findings from running them on committed code: M1 and M2 were only killed by an error-message mismatch because the tests used an agent and a relay context with no loopback channel (a later guard caught them); the tests now use an agent and a relay context ON loopback so each guard is isolated. M4 SURVIVED (a method outside the plugin table is refused anyway as unknown): the test now requires the refusal text "never available to plugins". M8 (handler mutex) and M8b: with the lock only in the handler a direct `config.Set` writer (provider.setDefaultModel, set-password) could still race, so the lock moved into `mutate` and a config-package test drives `Set` concurrently; M8b SURVIVED until that test existed. Each is disclosed in the PR body.

Two adversarial reviewers then read the exact commit `25ff93f64` (ADR and diff only): reviewer 1 on the guards found a P1 (a remote password login behind a same-host reverse proxy is a loopback peer and passed the guard), fixed by `requestContext.strictLocal` (`auth.IsTrueLoopback` at the upgrade) and an end-to-end test over a real upgrade with proxy headers (MP1); reviewer 2 on the write path found no P0/P1 and P2/P3s (alias data loss, unreadable file reported as defaults, audit spam, yes/on spelling, mutex not deferred, unaudited change) fixed with tests (MP2 to MP4). A third fresh reviewer then read the fix commits: no P0/P1; P2s (config.get did not resolve aliases and merge keys; a merge key above the path let a fresh section shadow siblings) fixed with tests (MP6), generic error text, the post-write failure audited as applied, `serveLocal` no longer claims strict. Residuals (audit eviction by refusals is costlier, not impossible; a same-host proxy that rewrites Host and adds no marks is indistinguishable from a local client) are disclosed in the PR body and the public docs.

## Staffing

Lead (Sonnet 5.5) writes spec, contract, review, mutations. GLM peers write the code from step briefs: A table + get, B set + guards + audit + event, C CLI parity + agent-run refusal + floor rule, D doc and recipe sweep + comment corrections. Then two fresh adversarial reviewers (guards; write path) on the exact commit.

## Progress

- [ ] spec and contract
- [ ] A table + get; B set + guards + audit + event
- [ ] C CLI + floor; D docs sweep
- [ ] own review, mutations, validator, two reviewers, fixes

## Validation

(filled in at PR time)
