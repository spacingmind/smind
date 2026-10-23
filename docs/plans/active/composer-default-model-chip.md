# Composer chip resolves "Default" from the last run

Size: M (3) — scope 1, PRs 0, risk 0, decisions 0, verification 2 (UI screenshots, light + dark, 1280 and 390 px).

## Goal

Provider/model picker chip with model = Default, no `providers.<id>.defaultModel`, no catalog default currently falls back to the provider name
("Claude Code"). Resolve it instead from the model the most recent run on this chat reported (`RunSummary.usage.model`), or, when the chat has no run,
the most recent run of the same provider in the shared run list. Shown with the explicit-model label rules and provider icon; tooltip marks it
"Claude Code · Sonnet 5.5 (default)". Nothing known keeps the provider-name fallback. No bespoke fetch: shared run store only (ADR-0032).

## Design

- `resolveTriggerLabel` / `resolveTriggerTitle` stay pure; both gain an optional `observedModel?: string` argument, used only in the `!selectedModel`
  branch after named-row / default-row / `resolvedDefaultLabel` all miss. The label goes through the same label formatting as an explicit model id.
- A small selector `selectObservedModel(runs, { chatId, provider })` over the shared `run.list` query (`queryKeys.runList()`): newest run on the chat with
  `usage.model` set; else newest run with the same `Provider` and `usage.model` set; else undefined.
- The "(default)" marker is added by `resolveTriggerTitle` only when the observed model was used; never in the label.
- Label stays hidden below `sm` (icon-only at 390 px, no wrap).

## Acceptance criteria and named tests

1. Observed model from this chat's last run — `resolveTriggerLabel observed model fills an unresolved default`, `selectObservedModel prefers the chat's newest run`.
2. Fallback to the newest same-provider run when the chat has none — `selectObservedModel falls back to the newest run of the same provider`.
3. Explicit, configured default and catalog default still win — `resolveTriggerLabel observed model never overrides an explicit, configured or catalog default`.
4. Nothing known → provider name — `resolveTriggerLabel falls back to the provider name without an observed model`; `selectObservedModel ignores runs without usage.model`.
5. Tooltip carries "(default)" — `resolveTriggerTitle marks an observed model (default)`; label has no marker; an explicit model's title unchanged.
6. Picker wiring — `ProviderModelPicker trigger shows the observed model` and a toolbar test that a chat's last run feeds the chip.
7. Screenshots (no model run), light + dark, 1280 and 390 px: chip with observed model, picker open/close frames, chip changing when a model is picked.

## Decisions

None needed; no ADR.

## Progress

- [ ] spec committed
- [ ] steps 1–3 by Peer
- [ ] review, checks, screenshots, PR

## Validation

(filled in at the end)
