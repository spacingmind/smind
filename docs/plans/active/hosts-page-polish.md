# Hosts page polish (ZCode visual parity, fleet lane)

Size: S, web only (`components/hosts/*`, `settings/hosts-section*.tsx`). No daemon/wsapi change.

## Acceptance criteria
1. Each host is its own card (`SettingsGroup`) of divided `SettingsRow`s, like other settings pages.
2. Two direction rows, "This host → <alias>" and "<alias> → this host", each with a status pill or the Switch; the grant is muted chips that never wrap mid-unit (non-breaking spaces, `whitespace-nowrap`).
3. The version-skew alert sits in its own row with the row padding.
4. The pending SAS is a prominent monospace block with a copy button (registry icons).
5. Themed Checkbox and Switch only (grant editor, add dialog, approve dialog, rows).
6. At 390 the rows stack. Behaviour unchanged (typed SAS approve, grant editor, reverse confirm, revoke/remove confirms, fleet-off).

## Tests
- TestHostsPage_RowCards, TestHostsPage_DirectionLabels, TestHostsPage_GrantChipsNoWrap, TestHostsPage_SasCopy (new file `hosts-page-polish.test.tsx`); existing `hosts-section.test.tsx` SAS assertion adapted.
- One mutation each.

## Decisions
- Test ids kept; new: `host-header-`, `host-dir-out-`, `host-dir-in-`, `host-out-state-`, `host-in-state-`, `host-sas-code-`, `host-sas-copy-` (+alias).

## Progress / Validation
Progress: components, tests and frames landed (peer-glm x4 runs: 3 steps + 2 small 390 fixes; no peer-sonnet).

Validation (2026-10-31):
- AC1-4,6: frames light+dark at 1280 and 390 (list with every state, pending SAS, skew alert, fleet-off, empty, approve open/typed, reverse confirm, loading->content burst) in /tmp/steward4/after/fleet-ui/ (before-*.png vs after-*.png). At 390 header menu, On/Off pills and Switch stay right of their labels; SAS block and Review button stack.
- AC5: grant editor and add dialog already used Checkbox; approve dialog uses it via GrantEditor; rows use Switch. No raw inputs of those kinds on the page.
- Mutations (each fails exactly its test): SettingsGroup->div (RowCards), "This host" label (DirectionLabels), NBSP->space (GrantChipsNoWrap), copy digits->code (SasCopy).
- vitest full ui suite 4276 passed; task lint ok; task test ok on rerun (one Go flake on the first run).
