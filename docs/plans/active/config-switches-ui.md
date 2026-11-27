# B5: the UI switches for the global config keys (ADR-0062)

Size M, **web UI only**. Spec: `docs/decisions/0062-config-get-set.md` sections 1, 3, 5 and the Human decisions; the shipped RPCs (`internal/wsapi/config_handlers.go`, `internal/config/allowlist.go`). Contract: `docs/plans/active/config-switches-ui.contract.md`. No daemon, wire or `mobile/` change.

## Wire facts (read from the code, not memory)

- `config.get {keys?}` answers `{keys:[{key, value, default, source, restart, writable, env?}]}` (strings for value/default; `source` is `default|file|env`; `env` is the variable NAME only). `writable` is already per connection: true only for a direct local connection, a writable key, and `source != env`. An unknown key is an error, `server.*` rows are absent unless direct local. A run-scoped caller is refused (not our case).
- `config.set {key, value}` takes a string `"true"|"false"` or a JSON bool; answers `{key, value, restart}`; refusals are sentences (`overridden by SMIND_x, unset it first`, `...needs a direct local (loopback) connection...`, `...applied but could not be written to the audit log...`).
- `config.updated {key}` is published on success; an older daemon answers `unknown topic` and the existing tolerant subscribe drops it.
- The table's confirmation sentences are NOT on the wire (`ConfigKey.Confirm` is Go-only). Not a hard gap: the UI keeps its own five sentences in `lib/config-keys.ts` and a source test (`config_confirm_sentences_match_the_daemon_table`) reads `internal/config/allowlist.go` so they cannot drift. Reported to the Human as a possible later wire field.

## Decisions

- D1 One hook file `hooks/use-config.ts`: `useConfigGet()` (TanStack Query, key `queryKeys.configGet()`, one `config.get` with no keys, shared client) and `useConfigSet()` (mutation sending exactly `{key, value}`; success writes the returned value into the cache AND invalidates; the daemon event does the same for other windows). The event bridge maps `config.updated` to invalidating `configGet`, `automationList`, `pluginList`, `learningStatus`, `fleetHosts`.
- D2 One component `components/settings/config-switch-row.tsx`: `ConfigSwitchRow({configKey, label, description, testId})`, `configKey` typed to the five writable keys (a runtime guard also refuses any other key: `browser.disabled` and every read-only key never get a switch). It renders: while loading a row skeleton; on a `config.get` error a read-only row with the daemon's sentence; `writable` true -> `Switch`; otherwise a read-only row (the value as "On"/"Off" and "Change this on the daemon's machine"); `source: "env"` -> the switch disabled with "Overridden by SMIND_X" (name only, never a value) when `writable` is false and the env name is present. `writable` is checked BEFORE any call: a non-writable row never attempts `config.set`.
- D3 Confirm: turning a key ON (`automations.enabled`, `plugins.enabled`, `fleet.enabled`, `learning.enabled`) or RESUMING (`automations.paused` -> false) opens the `ConfirmDialog` (non-destructive style) with the key's sentence; no `config.set` before the confirm. Turning OFF, or pausing (the brake), applies at once. Pending state disables the switch (`aria-busy`). A refusal (`daemonSentence`) is shown in an `Alert` under the row and the switch returns to its real (re-read) value.
- D4 Restart notice: for a key whose `restart` is true (`fleet.enabled`, `learning.enabled`) the row always carries "Takes effect after the daemon restarts." and after a successful change the notice is emphasised ("Saved. Takes effect after the daemon restarts."), with a link-button to Settings -> Daemon (the existing restart lives there; no new restart is invented).
- D5 Homes: Settings -> Automations (a group with the `automations.enabled` switch and a Pause/Resume switch row for `automations.paused`, above the existing open-page row); the Automations page: the off card carries the switch (the CLI command block stays only for a read-only connection) and the paused alert gets a Resume button (and a Pause button in the header when running); Settings -> Plugins (the `plugins-off` alert is replaced by the always-shown switch row); Settings -> Hosts (the "Fleet is off" card becomes the switch card; a fleet row stays on top when on); Settings -> Skills (learned) (the read-only kill-switch alert in `LearningOptIn` is replaced by the switch row, which stays visible). No change to `browser.disabled` or any read-only key.
- D6 Tokens only (B2 primitives `SettingsGroup`/`SettingsRow`, `Switch`, B3 modal), icons through `lib/icons.ts`, no other product names.

## Acceptance criteria

- AC1 A switch is shown only when `writable` is true for this connection; the read-only copy otherwise; no failed call is ever attempted from a read-only row.
- AC2 An env-overridden key shows the switch disabled with "Overridden by SMIND_X" and never the value.
- AC3 Turning on / resuming asks first (the key's sentence) and sends nothing before the confirm; the call is exactly `{key, value}`; turning off and pausing apply at once.
- AC4 `fleet.enabled` and `learning.enabled` show the restart notice; the others do not.
- AC5 A refusal is a sentence under the row and the switch shows the re-read value.
- AC6 The row refetches on `config.updated` (and the dependent lists: automations, plugins, learning status, hosts).
- AC7 `browser.disabled` and every read-only key never get a switch (types + runtime guard + a source test over the homes).
- AC8 The four homes carry the switches as D5 says; the Automations page has Pause/Resume.
- AC9 The confirm sentences equal the daemon's table.
- AC10 Real daemon end to end: from the e2e harness (a direct loopback connection) a switch change writes the temp `SMIND_HOME` config, the file changes and a comment in it survives.
- AC11 Frames: REAL-WINDOW wherever the screen is unlocked else parked; labelled; light and dark, 1280x800 and 800x520, all inside the Settings modal; a relay-style read-only frame (web harness, labelled).
- AC12 No daemon, wire or `mobile/` change; hygiene green.

## Named tests

`components/settings/config-switch-row.test.tsx`: `config_switch_shown_only_when_writable`, `config_read_only_copy_when_not_writable`, `config_no_call_attempted_from_a_read_only_row`, `config_env_overridden_switch_disabled_without_the_value`, `config_confirm_before_set_no_call_before_the_confirm`, `config_set_sends_exactly_key_and_value`, `config_turning_off_and_pausing_need_no_confirm`, `config_restart_notice_for_fleet_and_learning`, `config_refusal_shown_as_sentence_and_switch_returns`, `config_get_error_shows_a_read_only_sentence`.
`hooks/use-config.test.tsx`: `config_get_refetches_on_config_updated` (and the dependent keys), `config_get_is_one_shared_query`.
`lib/config-keys.test.ts`: `config_browser_disabled_never_gets_a_switch`, `config_confirm_sentences_match_the_daemon_table` (source drift against `internal/config/allowlist.go`).
Homes: `automations_settings_group_has_enable_and_pause_switches`, `automations_page_pause_and_resume`, `automations_off_card_has_the_switch`, `plugins_home_switch_replaces_the_alert`, `hosts_off_card_becomes_the_fleet_switch_with_restart_notice`, `learning_kill_switch_row_replaces_the_alert`.
e2e `web/e2e/specs/config-switches.spec.ts`: `UC5-01` automations switch end to end on the real daemon (confirm, the file changes, the comment survives, the page shows On), `UC5-02` plugins real, `UC5-03` fleet real with the restart notice, `UC5-04` learning real, `UC5-05` pause and resume real, `UC5-06` relay-style read-only (stubbed `config.get` writable false: no switch, no `config.set` on the socket), `UC5-07` env-overridden disabled (stub), `UC5-08` refusal sentence and the switch back (stub set error).

## Mutations (committed code; each must fail a named test)

M1 the switch is rendered when `writable` is false; M2 `config.set` is sent without the confirm; M3 `browser.disabled` is given a switch (the guard removed); M4 the restart notice is dropped; M5 an env-overridden key's switch is enabled; M6 `config.updated` no longer refetches.

## Frames

States: Automations off -> switch, confirm dialog, on, paused/resumed; Plugins off/on; Hosts off + switch + restart notice; Learning kill switch + restart notice; relay-style read-only row; env-overridden disabled; refusal error. Light and dark, 1280x800 and 800x520, inside the Settings modal. REAL-WINDOW with the audit app against a dogfood daemon; parked if the screen is locked or disk is short.

## Hosts follow-up (Supervisor, after the real-window pass)

The Fleet switch is a normal card at the top of Hosts (never inside an empty-state card); a state line says where the fleet stands from two facts (the daemon answers `fleet.host.list` = running; `config.get` value = what the file says): "Fleet is off.", "Fleet turns on after the daemon restarts.", "Fleet is running.", "Fleet turns off after the daemon restarts." (never "off" next to an on switch); the dashed "No hosts paired" card only when the fleet is running and has no hosts; "Open Daemon settings" is a link-styled button, desktop only, also next to the pending-restart line; "sMind" capitalised in the Hosts intro. Named tests: `hosts_fleet_switch_is_a_normal_card_at_the_top`, `hosts_state_line_off`, `hosts_state_line_pending_restart_after_turning_on`, `hosts_state_line_pending_restart_after_turning_off`, `hosts_state_line_running`, `hosts_empty_card_only_when_running_with_no_hosts`, `hosts_pending_restart_offers_open_daemon_settings_on_desktop_only`, `config_open_daemon_settings_looks_like_a_link`. Learning has no header or state line, so only its row's restart note applies.

- Real-window review of the Hosts page found one more defect, fixed: with the fleet switch saved, the switch row AND the Hosts state line both said "takes effect after the daemon restarts" and both offered "Open Daemon settings" (twice). Hosts now says it once, in the state line (`noRestartNotes` on its row); test `hosts_pending_restart_is_said_once_not_twice`.

## Staffing

GLM peers write the code from step briefs (A row + hook + keys + bridge + tests; B the homes + tests; C e2e + frames). Lead: spec, contract, review, mutations, frames, PR.

## Progress

- [x] spec + contract
- [x] A, B, C
- [x] review, mutations, validator, frames (web harness)
- [x] REAL-WINDOW frame pass DONE (see Validation): earlier parked state: branch `feat/config-switches-ui`, head `a6567cce9`, validationId 44 (pass, mergeReady), PR #992. Shot list: Automations off/confirm/on/paused/resumed (Settings + page), Plugins off/on, Hosts off + restart notice, Learning kill switch + restart notice, read-only row (web harness if unreachable), env-overridden disabled (daemon started once with SMIND_LEARNING=0), refusal; light/dark, 1280x800 and 800x520, inside the Settings modal. Audit app: `scripts/desktop-audit-build.sh`, CARGO_TARGET_DIR under `~/.smind/dogfood/ui-b5/`, deleted after.

- [x] DONE 2026-11-27 (screen unlocked): REAL-WINDOW re-shoot of Hosts off/confirm/pending-restart (+learning pending-restart if touched), light+dark, 1280x800+800x520, from head c84f120f2, then update the PR body and report. (Audit app: scripts/desktop-audit-build.sh with CARGO_TARGET_DIR under ~/.smind/dogfood/ui-b5/, delete after; dogfood daemon with its own SMIND_HOME; helpers in ~/.smind/dogfood/ui-b5/real/.)

## Validation

- Unit: 20 named tests plus 3 extra (`config_refusal_without_a_dialog_shows_an_alert`, `config_saved_restart_notice_opens_daemon_settings`, `config_open_daemon_settings_is_offered_only_on_desktop`); full UI suite 537 files / 5911 tests; `bunx tsc -b` clean; `sh scripts/verify.sh` PASS; full e2e matrix `E2E_FULL=1`: 427 passed, 49 skipped, 0 failed; UC5-01 and UC5-02 write the real daemon's config.yaml and assert the file changed and `# keep me` survived.
- Mutations by the Lead on committed code and by the validator: M1 (first run killed 2 of its 3 named tests; `config_switch_shown_only_when_writable` only checked the positive case, so a not-writable case was added), M2 to M6 each fail their named test.
- Frame review found two defects, fixed: the notes (restart line, refusal) sat beside the switch because `SettingsRow` children share its flex row (now under the row), and "Open Daemon settings" does nothing on web because the Daemon section is desktop-only (now offered only on desktop).
- Frames: REAL-WINDOW (the Tauri audit build of this branch against a dogfood daemon with its own `SMIND_HOME`, `screencapture -l` of the audit app window, real mouse clicks) for every state below in light and dark at 1280x800 and 800x520 inside the Settings modal, plus the Automations page (1280x800): automations off, confirm, on, paused, resume confirm, resumed; plugins off, confirm, on; hosts off, confirm, restart notice; learning on, off with restart notice, confirm, on again; env-overridden (daemon started with `SMIND_LEARNING=0`, 1280x800); refusal (the audit file made unwritable, 1280x800). The read-only (relay-style) row is WEB-HARNESS only (a relay session is not reachable in the real window). Earlier WEB-HARNESS frames of every state stay in `~/.smind/dogfood/ui-b5/frames-web/`. Real-window review found three defects, all fixed: the resume confirm said "Pause automations"/"Pausing the engine" (now "Resume automations" with its own sentence), a switch row inside the Automations off card had double side padding (`flush`), and (from the web frames) the notes sat beside the switch and "Open Daemon settings" does nothing on web.
- Wire gap noted, not blocking: the confirm sentences are not on the wire; the UI copy is kept equal by a source test.
