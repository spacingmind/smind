# Live-run composer controls, quiet ACP noise, borderless permission dock

Size: M (4) — scope 1 (web UI only), PRs 1, risk 1 (live-mode RPC path, composer tests), decisions 0 (ADR-0019/0025 semantics kept, ADR-0010 log stays lossless), verification 2 (light/dark screenshots with a live run).

Follow-up to #318, requested by the Supervisor from the live-permission screenshot (`dark-desktop-09-live-permission.png`).

## Problems (observed)

1. While a run is live the composer toolbar shows `GLM ⌄ · Ask for permission ⌄ · Auto-accept · Max ⌄ · Ask for permission ⌄ · This run: GLM-5.3 ⌄`: the permission control is duplicated, the live ACP option selects (`Max`, the agent's own `mode` option, `This run: …`) render in a larger font than the pills, and a separate `Mode: [Ask for permission] [Auto-approve edits] [Bypass all permissions] [Auto-accept]` segmented row (`PermissionModeControl`, mounted by `task-detail.tsx`) sits above the composer.
2. The timeline renders `Unrecognised event: available_commands_update` (an ACP notification, preserved as a `raw` event by ADR-0010) as a row.
3. The pending-permission dock keeps a darker full-width block and its own `border-t` behind the card.
4. "Another chat is running in this worktree — <link>" and the "Working" chip stack loosely above the composer.

## Decisions

- **D1 One mode control, live while a run is active.** Paseo's composer has a single mode control (`refs/paseo/packages/app/src/composer/agent-controls/mode-control.tsx`, `useLiveAgentModeControl`) that drives the live agent. smind's toolbar `Mode` pill (+ `Auto-accept` pill) does the same: with a run active on a provider that supports a live switch (`supportsLiveModeSwitch`), the pill shows the running run's effective mode / autoAccept and changing it calls the existing `run.setPermissionMode` RPC (ADR-0019, unchanged). With no live run (or a provider without live switch, e.g. Codex) it edits the next run's config exactly as today. `PermissionModeControl` and its `Mode:` row are removed from the pane.
- **D2 Live ACP config options become pills.** `RunConfigOptions` selects use the shared pill trigger (`PILL_TRIGGER_CLASS`: icon + label, `text-ui-sm`, same height). A `category: "mode"` option is dropped while the Mode pill is live (it is the same session mode); the live `model` option stays a separate, clearly labelled pill (`This run: <model>` with a model icon and the "this session" aria-label/tooltip of ADR-0025) because it switches the running session, unlike the next-run model picker; thinking-like selects get a Brain icon. Boolean options use the same pill button look; the text-field fallback is unchanged in behaviour.
- **D3 Quiet ACP noise, UI-side.** The persisted log stays lossless (ADR-0010 unchanged). `appendTimelineEvent` consumes known-benign ACP session-update kinds silently: `available_commands_update`, `session_info_update`, `current_mode_update`, `config_option_update`, `usage_update`. Other `raw`/unknown events still render but only in the Detailed view (`groupTimeline` drops them in Overview). `plan` and genuinely unknown kinds stay visible in Detailed.
- **D4 Borderless permission dock.** The dock wrapper loses `border-t` and `bg-background`; the card alone remains (tone-tinted by its existing styling), aligned to the reading column.
- **D5 One status-chip row.** The concurrency banner ("Another chat is running in this worktree — <title>") becomes a chip in `ComposerStatusChips` (same chip look as "Working"/diff-stat; click jumps to that chat), so one row sits above the composer. The separate full-width button row is removed.

## Acceptance criteria

- **L-AC1** While a live-switch-capable run is active the toolbar contains exactly one permission-mode control (and, for ACP, one Auto-accept toggle), both reflecting the running run; no `Mode:` segmented row exists in the pane.
- **L-AC2** Changing the pill's mode while running calls `setPermissionMode(runId, { modeId })`; toggling Auto-accept calls it with `{ autoAccept }`; the pill follows the run's updated mode. With no run, the same pills edit the next run's config (no RPC).
- **L-AC3** A provider without live switch (Codex) keeps next-run semantics while running.
- **L-AC4** Live config-option controls render as pills (same trigger class/size as the other pills); an option with `category: "mode"` is not rendered while the Mode pill is live; the model option reads `This run: <name>` with the ADR-0025 aria-label.
- **L-AC5** `available_commands_update`, `session_info_update`, `current_mode_update`, `config_option_update`, `usage_update` raw events produce no timeline item (live and run.logs backfill); `plan` still renders in Detailed; unknown/raw rows are absent in Overview.
- **L-AC6** The dock has no `border-t`/background block; the card renders.
- **L-AC7** The concurrency notice is a chip in `composer-status-chips` next to Working/diff-stat; clicking it calls `onJumpToChat(id)`; the old banner (`concurrency-banner`) is gone.
- **L-AC8** Screenshots, light + dark: live permission, live run after a mode change, idle composer (1440×900 and 390×844).

## Named test scenarios

`composer/composer.test.tsx` / `run-config-toolbar.test.tsx`
- `live run: the mode pill shows the running run's mode and changing it calls setPermissionMode with the run id`
- `live run: Auto-accept toggle calls setPermissionMode with autoAccept`
- `no live run: the mode pill edits the next run's config and makes no RPC`
- `live run on a provider without live switch keeps next-run semantics`
- `live config options use the pill trigger and omit the mode option while the mode pill is live`
- `the live model option is labelled This run and keeps the ADR-0025 aria-label`

`components/composer/composer-status-chips.test.tsx` (new)
- `renders the concurrency chip beside Working and calls onJumpToChat`
- `renders nothing when nothing applies`

`hooks/use-run-timeline` / `timeline/detail-level` tests
- `benign ACP session updates produce no timeline item`
- `a plan raw event still produces an unknown row`
- `overview drops unknown rows, detailed keeps them`

`components/task-detail.test.tsx`
- `pending-permission dock has no border or background block`
- `the Mode: row is gone and the toolbar mode pill drives the live run`
- existing suites stay green (permission dock, queued prompts, concurrency jump now via the chip).

## Progress

- [ ] Spec
- [ ] Step 1 timeline noise (D3)
- [ ] Step 2 composer live controls + status chip row (D1, D2, D5)
- [ ] Step 3 dock (D4)
- [ ] Step 4 screenshots, review, `task test`, `task lint`, PR

## Validation

(filled in as criteria are confirmed)
