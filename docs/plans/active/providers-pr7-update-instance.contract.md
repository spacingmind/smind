---
v: 1
milestone: "Providers PR7: provider.updateInstance (daemon, CLI, web editors)"
base: origin/develop
planRef: docs/plans/active/providers-pr7-update-instance.md
acs:
  - id: AC1
    text: "updateInstance on one providers.custom entry leaves every other byte of config.yaml (other entries, comments, blank lines, ordering) identical."
    covers: { tests: [TestUpdateInstance_LeavesOtherEntryByteIdentical, TestUpdateInstance_HandWrittenEntryWithComments, TestUpdateInstance_LastEntryBeforeOtherSection], frames: [] }
  - id: AC2
    text: "Invalid input is refused with the config-load field-named error and nothing is written; unknown id and non-instance are refused."
    covers: { tests: [TestUpdateInstance_InvalidFieldsNamed, TestUpdateInstance_UnknownIDAndNonInstance], frames: [] }
  - id: AC3
    text: "Same values are a no-op that does not touch the file."
    covers: { tests: [TestUpdateInstance_IdempotentNoop, TestUpdateInstance_NothingToUpdate], frames: [] }
  - id: AC4
    text: "provider.updateInstance is human-only (agent, plugin, automation refused) and works for recipe-made, UI-made and hand-written instances, applied live."
    covers: { tests: [TestProviderUpdateInstance_RefusesAgentCaller, TestPluginSession_CannotReachHumanOnlyMethods, TestProviderUpdateInstance_RecipeMadeUIMadeHandWritten, TestProviderUpdateInstance_AppliesLive, TestProviderUpdateInstance_InvalidKeepsRegistry], frames: [] }
  - id: AC5
    text: "provider.list serves the editable overlay of an instance only."
    covers: { tests: [TestProviderList_InstanceEditableFields], frames: [] }
  - id: AC6
    text: "Two claude-native instances with different CLAUDE_CONFIG_DIR run concurrently and each process sees its own dir; a hand-written Paseo-style config loads and lists unchanged."
    covers: { tests: [TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs, TestHandWrittenPaseoStyleConfig_LoadsAndListsUnchanged], frames: [] }
  - id: AC7
    text: "CLI smind provider update sends the right params and the CLI reference stays in sync."
    covers: { tests: [TestProviderUpdateCLI, TestCLIReferenceMatchesHelp], frames: [] }
  - id: AC8
    text: "The Providers detail edits an instance's models (replace vs add), disallowedTools and inherit toggle; light+dark at 1280 and 390."
    covers: { tests: [], frames: [] }
tests:
  - { name: TestUpdateInstance_LeavesOtherEntryByteIdentical, run: "go test ./internal/config -run '^TestUpdateInstance_LeavesOtherEntryByteIdentical$' -count=1", timeoutSec: 120 }
  - { name: TestUpdateInstance_InvalidFieldsNamed, run: "go test ./internal/config -run '^TestUpdateInstance_InvalidFieldsNamed$' -count=1", timeoutSec: 120 }
  - { name: TestUpdateInstance_IdempotentNoop, run: "go test ./internal/config -run '^TestUpdateInstance_IdempotentNoop$' -count=1", timeoutSec: 120 }
  - { name: TestProviderUpdateInstance_RefusesAgentCaller, run: "go test ./internal/wsapi -run '^TestProviderUpdateInstance_RefusesAgentCaller$' -count=1", timeoutSec: 180 }
  - { name: TestPluginSession_CannotReachHumanOnlyMethods, run: "go test ./internal/wsapi -run '^TestPluginSession_CannotReachHumanOnlyMethods$' -count=1", timeoutSec: 180 }
  - { name: TestProviderUpdateInstance_RecipeMadeUIMadeHandWritten, run: "go test ./internal/wsapi -run '^TestProviderUpdateInstance_RecipeMadeUIMadeHandWritten$' -count=1", timeoutSec: 180 }
  - { name: TestProviderList_InstanceEditableFields, run: "go test ./internal/wsapi -run '^TestProviderList_InstanceEditableFields$' -count=1", timeoutSec: 180 }
  - { name: TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs, run: "go test ./internal/taskrunner -run '^TestInstances_TwoClaudeInstancesRunConcurrentlyWithOwnConfigDirs$' -count=1", timeoutSec: 180 }
mutations:
  - { id: M1, file: internal/wsapi/provider_instances.go, change: "remove the run-scoped refusal in handleProviderUpdateInstance", expectFail: [TestProviderUpdateInstance_RefusesAgentCaller] }
  - { id: M2, file: internal/config/provider_instance_update.go, change: "skip ValidateCustomProviders on the next list", expectFail: [TestUpdateInstance_InvalidFieldsNamed] }
  - { id: M3, file: internal/config/provider_instance_update.go, change: "write the file even when no field changed", expectFail: [TestUpdateInstance_IdempotentNoop] }
  - { id: M4, file: internal/config/provider_instance_update.go, change: "widen the replaced line range by one line", expectFail: [TestUpdateInstance_LeavesOtherEntryByteIdentical] }
commands:
  - { id: C1, run: "go test ./internal/config ./internal/wsapi ./internal/taskrunner ./cmd/smind -count=1", timeoutSec: 900 }
  - { id: C2, run: "cd web && bun run --cwd packages/ui test provider", timeoutSec: 600 }
frames: []
---

Contract for the validator: judge the branch head against the ACs above. AC8 is visual and is judged from the PR's frame list and the component test, not from pixels.
