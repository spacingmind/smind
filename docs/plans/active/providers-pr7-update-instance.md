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

- 2026-10-31: spec + amendment + contract written; config patcher, RPC, list field, CLI, web editors, docs, tests done; frames looked at (two rounds); validator run recorded below.

## Peer ledger

| Peer / run | Step | Result | Failure mode |
|---|---|---|---|
| GLM 163 #1 | config patcher + tests (one brief) | no file written | output budget (max_tokens) before the first write: brief too large |
| GLM 163 #2 | `provider_instance_update.go` only | done, committed; reviewed; one lookup bug (compared the key node) found by the tests | none |
| GLM 163 #3 | tests in 3 commits | 6 tests + 2 debug tests left uncommitted | **rate limited** (5h window, reset 15:20 UTC+8 = 14:20 local); continued on Sonnet |
| GLM 172 #1 | web types, hook, editors, tests | done, 3 clean commits; could not run tests (no node_modules in the Peer worktree), they passed when the Lead ran them | none |
| GLM 172 #2 | CLI `smind provider update` + docs | done, 3 clean commits | none |
| Sonnet 163 x3 | fix lookup + finish tests; tighten two surviving mutations; fix multi-edit ordering bug found by the frames | done | none |
| Sonnet 179 x2 | wsapi RPC, Spec overlay, list field, tests; concurrency + Paseo tests + docs | done | none |
| Sonnet 183, 188 | frame loops (round 1 found the daemon bug and 4 UI defects; round 2 clean) | done | round 1 CLI killed once (memory pressure), resumed |
| Sonnet 172 x2 | UI fixes (layout, error placement, keys, placeholders); a test fix | first done; second stopped on a permission prompt (Bash it could not run unattended) | the Lead wrote that one test fix |

Counts: GLM 5 runs (3 ok, 1 output budget, 1 rate limit), Sonnet 11 runs (10 ok, 1 stopped on a permission prompt); the Lead wrote one test (`toggling inherit keeps an unsaved models draft`). `glm-acp-agent` ran with `--max-turns 500` (seen with `ps`).

## Validation

| AC | Evidence |
|---|---|
| AC1 | `TestUpdateInstance_LeavesOtherEntryByteIdentical` (exact expected file), `_HandWrittenEntryWithComments`, `_LastEntryBeforeOtherSection`, `_NoTrailingNewline`, `_InsertRemoveReplaceFields`, `_SeveralEditsInOneCall` |
| AC2 | `_InvalidFieldsNamed` (first validation, not the safety net), `_UnknownIDAndNonInstance`, `_FlowStyleRefused` |
| AC3 | `_IdempotentNoop` (bytes and mtime), `_NothingToUpdate`, wsapi `_IdempotentNoop` |
| AC4 | wsapi `TestProviderUpdateInstance_RefusesAgentCaller`, `_RecipeMadeUIMadeHandWritten`, `_AppliesLive`, `_InvalidKeepsRegistry`; `provider.updateInstance` is in `humanOnly` of `TestPluginSession_CannotReachHumanOnlyMethods` and not in `RunScopedMethods` (automation and agents use that allowlist) |
| AC5 | `TestProviderList_InstanceEditableFields`, `TestProviderInfo_InstanceOverlay`, `TestInstanceSpec_KeepsOwnOverlay` |
| AC6 | `TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs` (start barrier; fakeclaude has no short-hold scenario, so overlap in time is not proven, the run paths share one Runner), `TestHandWrittenPaseoStyleConfig_LoadsAndListsUnchanged` |
| AC7 | `TestProviderUpdateCLI`, `TestCLIReferenceMatchesHelp` |
| AC8 | `provider-instance-editors.test.tsx` (9 tests); frames on an isolated daemon (no model call, no Send), light + dark at 1280 and 390, with Save and inherit-toggle transition frames, looked at by the Lead (Models + Disallowed tools + toggle at 1280 light, refusal text under Save at 390 dark) |
| AC12 | `public-docs/providers.md` "Edit an instance", ADR-0046 amendment, docs drift test |

Mutations (each made the named test FAIL, file restored with `git checkout --`): drop the run-scoped refusal (`TestProviderUpdateInstance_RefusesAgentCaller`); skip the first `ValidateCustomProviders` (`_InvalidFieldsNamed`, after tightening: it first survived because the post-patch safety net validates again); write on a no-op (`_IdempotentNoop`); replaced range one line too long (`_LeavesOtherEntryByteIdentical`, after tightening: first survived because the blank line was inside the compared region); drop `Instance` from the list entry (`TestProviderList_InstanceEditableFields`); edits assembled in descending order (`_SeveralEditsInOneCall`); instance env taken from claude-a for both (`TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs`); web: drop `additionalModels: []` from the replace save, show the tool editor for every harness, key the Models editor on the whole instance.

Real bug found by the frames, not by tests: with two or more edits in one call the assembler wrote duplicates (the safety net refused the write, so no corruption, but every models switch failed). Fixed and covered by `_SeveralEditsInOneCall`.

Not done: no `smind provider update` support for `label`, `env`, `command`, `endpoint`; a flow-style entry or a key on the `- ` line is refused rather than patched; a duplicate model id typed in the UI is ignored silently; the Models card header still says "Built-in catalog" after a replace.
