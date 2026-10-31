# Providers PR7: `provider.updateInstance`

Status: active (2026-10-31). Binding text: ADR-0046, section "Amendment 2026-10-31: provider.updateInstance" (Human-approved 2026-10-31). Parent plan: [providers-epic.md](providers-epic.md).

Size: L (7), scope 2 (daemon, CLI, web), PRs 1, risk 2 (writes config.yaml, human-only authority), decisions 1 (amendment written), verification 1 (UI frames).

## Wire contract

`provider.updateInstance {id, models?, additionalModels?, disallowedTools?, inheritClaudeConfig?}`; `models`/`additionalModels` are `[{id, label?, description?, default?}]`.
A field that is **present** is applied; an **empty** list (or `false` for the toggle) removes the key from the entry; an **absent** field is left alone. The whole resulting
entry is then validated with the config-load rules (`ValidateCustomProviders`), so `models` + `additionalModels` together fail with `providers.custom[i].additionalModels: ...`
(a client switching replace/add mode sends the new list and `[]` for the other). Nothing present: error `provider.updateInstance: nothing to update`. Same values as stored:
success, file bytes unchanged. Result: the `provider.updated` payload (like `provider.addInstance`). Human-only: refused for run-scoped (agent) callers, on the plugin
never-list. Keys stay on `provider.setSecret`/`clearSecret`; `env`, `command`, `endpoint`, `label` are not editable here.

`provider.list` entries of an instance gain `instance: {models, additionalModels, disallowedTools, inheritClaudeConfig}` (the entry's own overlay as configured, not the
merged catalog), additive.

config.yaml edit: a byte-level patch of only the changed `key: value` blocks of the one entry (`config.UpdateProviderInstance`); every other byte of the file (other entries,
comments, blank lines, ordering) is identical. Post-conditions are checked before the atomic write (the patched file must decode to the expected entry and still load).

CLI: `smind provider update <id> [--models a,b] [--add-models a,b] [--disallow T,U] [--inherit-claude-config=true|false]`.

## Acceptance criteria and named tests

| AC | Statement | Tests |
|---|---|---|
| AC1 | Editing one entry leaves the other entry's bytes and the file's comments identical (incl. a hand-written entry with comments, blank lines, odd quoting) | `TestUpdateInstance_LeavesOtherEntryByteIdentical`, `TestUpdateInstance_HandWrittenEntryWithComments`, `TestUpdateInstance_InsertRemoveReplaceFields`, `TestUpdateInstance_LastEntryBeforeOtherSection`, `TestUpdateInstance_NoTrailingNewline` |
| AC2 | Same validation and field-named errors as config load; nothing is written on error | `TestUpdateInstance_InvalidFieldsNamed` (models+additionalModels, disallowedTools on codex, bad tool name, bad model id, duplicate model id, inheritClaudeConfig on codex, endpoint instance without models), `TestUpdateInstance_UnknownIDAndNonInstance` |
| AC3 | Idempotent no-op: same values leave the file byte-identical and mtime untouched; empty request is an error | `TestUpdateInstance_IdempotentNoop`, `TestUpdateInstance_NothingToUpdate` |
| AC4 | Works for a recipe-made, a UI-made (`addInstance`) and a hand-written instance | `TestProviderUpdateInstance_RecipeMadeUIMadeHandWritten` |
| AC5 | Human-only: refused for run-scoped, plugin and automation callers; on the plugin never-list; wsapi schema/drift tests know it | `TestProviderUpdateInstance_RefusesAgentCaller`, `TestPluginSession_CannotReachHumanOnlyMethods` (entry added), automation caller case in `TestProviderUpdateInstance_RefusesAgentCaller` |
| AC6 | Applied live and atomically (registry reloaded as `provider.reload`; invalid config leaves the registry as it was); `provider.updated` published | `TestProviderUpdateInstance_AppliesLive`, `TestProviderUpdateInstance_InvalidKeepsRegistry` |
| AC7 | `provider.list` serves the editable fields, additively, for instances only | `TestProviderList_InstanceEditableFields` |
| AC8 | Two claude-native instances with different CLAUDE_CONFIG_DIR run concurrently, each process sees its own dir | `TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs` (taskrunner) |
| AC9 | A hand-written Paseo-style config (two claude-native instances + an ACP entry) loads and lists unchanged | `TestHandWrittenPaseoStyleConfig_LoadsAndListsUnchanged` |
| AC10 | CLI `smind provider update` sends the right params, exit codes, usage, docs drift | `TestProviderUpdateCLI_*`, `TestCLIReferenceMatchesHelp` |
| AC11 | Web: models editor (add/remove, replace-vs-add), disallowedTools chips, inherit toggle on the Providers detail; shared primitives; light+dark frames 1280/390 incl. transitions | `provider-instance-editors.test.tsx`, frames in the validation section |
| AC12 | Docs: `public-docs/providers.md` "Editing an instance", ADR-0046 amendment | docs drift test |

Mutations (one per load-bearing test, recorded in Validation): drop the run-scoped refusal; patch splice touching the wrong entry; skip validation; make the no-op write; drop `instance`
from the list entry; share the env between the two instances; hide the toggle for non-claude.

## Decisions

- Targeted byte patch instead of `mutate` (which re-marshals the whole file and loses blank lines): see the amendment.
- `inheritClaudeConfig: false` removes the key (no `false` written).
- An entry in flow style (`- {id: x, ...}`) or a key on the `- ` line is refused with a message to edit by hand: the patcher only splices block-style fields it can locate exactly.

## Progress

- 2026-10-31: spec + amendment written.

## Peer ledger

(filled as work proceeds)

## Validation

(filled at the end)
