# UI cleanup B2: Settings consistency (one layout, one column)

Size M. Human decision 2026-11-27: the reference style is Browser plus General/Appearance. Web UI only (Settings sections are shared by desktop and web): nothing under `mobile/`, no daemon, wire or Rust change. Behaviour does not change: every existing test passes or is updated for the new markup, and every existing `data-testid` is kept.
Contract: `docs/plans/active/ui-cleanup-b2.contract.md`. Source: the Settings audit notes (class-level fixes per tab), checked against the code before each edit.

## Decisions

- **D1 One content column.** Every Settings tab uses the same column: `mx-auto w-full max-w-3xl px-6` (48rem; it still shrinks with the window, so it stays responsive and the mobile layout is unchanged). Today the column is 510px for 9 tabs, 3xl for 9 and 5xl for Shortcuts, so the content jumped left and right between tabs. Why 3xl and not 510: the lists, tables and rows with long monospace text (Saved grants, Hosts chips, MCP and Plugins rows, Policy and Usage tables) need the room; General, Appearance and Notifications rows (label left, control right) stay readable at 3xl because a `SettingsRow` already justifies its control to the right edge. Why no exception for Shortcuts: its table (action, shortcut, scope) fits 48rem inside the card shell, and one rule means the column never shifts; the frames decide, and a documented exception is only added if the 800px or 1280px frame shows clipping (none expected). `WIDE_SECTION_IDS` and `EXTRA_WIDE_SECTION_IDS` are deleted; the rule is a single constant.
- **D2 Nav groups.** "Profiles & providers" is replaced by two groups, ordered so each is contiguous: **Agents** = Profiles (175), Providers (176), MCP servers (177), Skills (learned) (178), Plugins (179), Automations (180); **Safety & usage** = Permission policy (190), Saved grants (191), Browser (192), Usage (193). Ungrouped: General, Appearance, Notifications, Shortcuts, GitHub, Hosts. The desktop **Connection** group (Daemon server, Daemon) stays, since both are about how this computer reaches or runs the daemon.
- **D3 One list primitive.** `settings-layout.tsx` gains `SettingsList` (a `ul` card: `overflow-hidden rounded-xl border border-border bg-card`, rows divided by `border-border`) and `SettingsListRow` (an `li`: `px-4 py-3`, caller adds layout classes). Profiles, MCP servers and Plugins use it (they had three list shells with `px-2 py-1.5`, `p-3 gap-1` and divider variants).
- **D4 One way to say "enabled".** `Switch`, as in Browser and Notifications. MCP servers (was a Checkbox) and Plugins (was an Enable/Disable button pair) use it; where enabling a plugin needs a consent step today, the Switch opens that same step instead of enabling directly.
- **D5 Automations** keeps its link page but as a `SettingsGroup` with a `SettingsRow` (state line as description, "Open Automations" as the control). The on/off switch is item B5 and is NOT added here: the row is left clean for a `control`.
- **D6 Drift guard.** A source test fails when a Settings section file hand-rolls a `rounded-xl border ... bg-card` box or a raw `<h3` outside `settings-layout.tsx` (allowlist only with a written reason).

## Per-tab changes (each verified against the code first)

Policy: raw `<h3>` and hand-built boxes -> `SettingsGroup` ("Rules", "Recent decisions") + `SettingsEmptyCard`, tables inside the group card, `Alert` for load errors; keep the narrow card fallback. Saved grants: one `SettingsGroup` with a `SettingsRow` per grant, `SettingsEmptyCard` for empty. Usage: `gap-8` -> `gap-6`, `BlockTitle` -> `SettingsGroup`, `EmptyState` -> `SettingsEmptyCard`. GitHub: intro text moves to the registry description, bare lists and `px-3` rows -> `SettingsGroup` + `SettingsRow` (`px-4`), inline red text -> `FormError`, `gap-4` -> `gap-6`. Hosts: drop the forced `flex-row` overrides. Daemon server / Connections: the hand-rolled `role=switch` button -> `Switch`, the hand-built `alertdialog` div -> `ConfirmDialog`. MCP servers: Checkbox -> `Switch`; list -> `SettingsList`. Plugins: Enable/Disable buttons -> `Switch`; list -> `SettingsList`. Profiles: list -> `SettingsList`. Shortcuts: the table inside the card shell. Automations: see D5. Daemon `gap-3` and Browser `gap-5` -> `gap-6`. The workspace `Select` duplicated in Skills and Browser -> one `SettingsWorkspacePicker` (only if it stays a pure extraction).

## Acceptance criteria

- AC1 One column: every registered Settings section renders in the same `max-w-3xl` column; no `max-w-[510px]` or `max-w-5xl` remains in the screen.
- AC2 The nav has the groups Agents and Safety & usage with the members and order of D2; no group is named "Profiles & providers"; the Connection group is unchanged.
- AC3 `SettingsList`/`SettingsListRow` exist with the D3 classes; Profiles, MCP servers and Plugins render through them.
- AC4 Policy, Saved grants and Usage use the shared primitives (group titles, one row per grant, empty cards, `gap-6`).
- AC5 GitHub, Hosts, Daemon, Browser, Shortcuts and Automations follow the per-tab list above.
- AC6 Daemon server toggle and MCP/Plugins enabled state are `Switch` controls and the removal confirm is a `ConfirmDialog`; their behaviour (calls, testids) is unchanged.
- AC7 The drift test passes on the final tree and fails when a hand-rolled card or raw `<h3` is added.
- AC8 Frames (REAL-WINDOW where the screen is unlocked, else WEB-HARNESS, every frame labelled): every Settings tab light and dark at 1280x800, a subset at 800x520, and a before/after contact sheet of the tab sequence showing the column no longer shifts. No `mobile/`, `internal/` or `cmd/` change; hygiene green.

## Named tests

- `settings-layout.test.tsx`: `ui_cleanup_b2_settings_list_is_one_divided_card`, `ui_cleanup_b2_settings_list_row_padding`.
- `settings-screen.test.tsx`: `ui_cleanup_b2_one_column_width_for_every_section`, `ui_cleanup_b2_nav_groups`.
- `policy-section.test.tsx`: `ui_cleanup_b2_policy_uses_shared_primitives`, `ui_cleanup_b2_policy_file_row_wraps_long_path` (a long path wraps under its label; the same defect was fixed for the Daemon log path and the Hosts label chips, test `ui_cleanup_b2_long_value_rows_wrap`). `saved-grants-section.test.tsx`: `ui_cleanup_b2_saved_grants_one_group_row_per_grant`. `usage-section.test.tsx`: `ui_cleanup_b2_usage_uses_shared_primitives`.
- `github-section.test.tsx`: `ui_cleanup_b2_github_rows_use_settings_rows`. `hosts-section.test.tsx`: `ui_cleanup_b2_hosts_row_is_responsive`. `connections-section.test.tsx`: `ui_cleanup_b2_connections_uses_switch_and_confirm_dialog`. `daemon-section.test.tsx`: `ui_cleanup_b2_daemon_gap_is_six`. `browser-section.test.tsx`: `ui_cleanup_b2_browser_gap_is_six`. `shortcuts-section.test.tsx`: `ui_cleanup_b2_shortcuts_in_card_shell`. `automations-link-section` (new `automations-link-section.test.tsx`): `ui_cleanup_b2_automations_is_a_group_and_row`.
- `mcp-servers-section.test.tsx`: `ui_cleanup_b2_mcp_enabled_is_a_switch`. `plugins-section.test.tsx`: `ui_cleanup_b2_plugins_enabled_is_a_switch`. `profiles-section.test.tsx`: `ui_cleanup_b2_profiles_use_settings_list`.
- `settings-drift.test.ts`: `ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3`.
- `web/e2e/specs/ui-cleanup-b2.spec.ts`: `UC2-01` the content column starts at the same x on every tab (measured), `UC2-02` the nav groups; frames when `UICLEAN2_FRAMES_DIR` is set.

## Mutations (committed code; each must fail a named test)

- A section re-hand-rolls a card (`rounded-xl border bg-card` div added in saved-grants-section.tsx) -> `ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3`.
- The width rule is removed (a section id gets the old narrow class back in settings-screen.tsx) -> `ui_cleanup_b2_one_column_width_for_every_section`.
- The MCP `Switch` is replaced by a `Checkbox` -> `ui_cleanup_b2_mcp_enabled_is_a_switch`.

## Frames

Real macOS desktop window (scripts/desktop-audit-build.sh from this branch, dogfood daemon under `~/.smind/dogfood/ui-b2/`, own port) while the session is unlocked, else the web harness; every frame labelled REAL-WINDOW or WEB-HARNESS. Every Settings tab light and dark at 1280x800; at 800x520 General, Shortcuts, Automations, Permission policy, Saved grants, Usage, Hosts, Browser. Before (develop) and after contact sheets of the same tab sequence.

## Progress

- [ ] spec and contract (this file)
- [ ] primitives, width rule, nav groups, Policy, Saved grants, Usage (peer A)
- [ ] GitHub, Hosts, Connections, Daemon, Browser, Shortcuts, Automations (peer B)
- [ ] lists: Profiles, MCP, Plugins, workspace picker, drift test (peer B2)
- [ ] e2e and frames
- [ ] review, mutations, validator

## Validation

- `sh scripts/verify.sh`: VERIFY PASS (build, test, lint). Full e2e matrix (`E2E_FULL=1`): 361 passed, 47 skipped. tsc clean.
- Mutations on committed code, each failing its named test: M1 (a hand-rolled `rounded-xl border bg-card` div in saved-grants) -> `ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3`; M2 (column narrowed to `max-w-[510px]`) -> `ui_cleanup_b2_one_column_width_for_every_section`; M3 (the MCP `Switch` replaced by a checkbox input) -> `ui_cleanup_b2_mcp_enabled_is_a_switch`.
- Column jump, measured (WEB-HARNESS, 1280x800): before (develop) the page title started at x=663 on General, Appearance, Notifications, GitHub, Hosts, Skills, Saved grants and Browser, and at x=536 on the other eight tabs; after, it starts at x=536 on all 16 web tabs (`UC2-01` asserts it, and asserts one column width). Shortcuts fits the same 48rem column, so no exception was needed.
- Frames: every frame is WEB-HARNESS (headless browser on the built web UI). All 16 web tabs light and dark at 1280x800, the 8-tab subset (General, Shortcuts, Automations, Permission policy, Saved grants, Usage, Hosts, Browser) at 800x520, a before/after contact sheet of the same tab sequence, and Profiles with two profiles seeded on a dogfood daemon (light and dark). The macOS session was locked at the time of the frame pass, so there are NO REAL-WINDOW frames and the audit app was not built. The two desktop-only tabs (Daemon server, Daemon) are not in the web harness: their changes (Switch, ConfirmDialog, `gap-6`) are covered by unit tests only.
- Review fix (long values): at 800x520 the Permission policy File path ran into its label and was clipped (the path sat in a `shrink-0` control box). It is now the row description (`min-w-0 break-all`), so it wraps under the label; the same defect was fixed for the Daemon log path and the Hosts label chips (`ui_cleanup_b2_policy_file_row_wraps_long_path`, `ui_cleanup_b2_long_value_rows_wrap`). Policy 800x520 light and dark re-shot and checked; verify PASS and the full e2e matrix (361 passed, 47 skipped) re-run on the fix. The Daemon and Hosts rows are not in the web harness (unit tests only).
- Not shown in any frame: rows with data on Saved grants, Permission policy rules and decisions, MCP servers, Plugins (a switch row), and GitHub signed-in hosts; the harness and dogfood daemon had none of those, so they are covered by the unit tests named above.
