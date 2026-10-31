---
v: 1
milestone: "Gate reasons PR1: internal/gate derivation, provider.list gate/modelGates, typed gated refusal, MCP text"
base: origin/develop
planRef: docs/plans/active/picker-gate-reasons.md
acs:
  - id: AC1
    text: "Gate reasons are the fixed set; a failure class has a twin only where a picker row can prevent it."
    covers: { tests: [TestTwinTable_CoversEveryHumanFixAndLimitClass] }
  - id: AC2
    text: "One derivation: nobody but internal/gate builds a gate.Gate; precedence is deterministic over all 32 combinations."
    covers: { tests: [TestGate_NoSecondDerivation, TestDerive_PrecedenceMatrix, TestDerive_UntilIsLaterOfWindowAndCooldown], mutations: [M4] }
  - id: AC3
    text: "Gates rest on cache-only state: internal/gate imports no quota or accounts package, an account window reaches it only as a routing.Verdict; a transient back-off and a stale or missing report never gate; one usable account ungates the provider."
    covers: { tests: [TestGate_PackageImportsNoFetcher, TestDerive_OutOfPlan_TransientBackoffIsNotAGate, TestDerive_OutOfPlan_OneHealthyAccountUngates, TestDerive_NoGateWhenNothingKnown], mutations: [M1, M3, M5] }
  - id: AC4
    text: "Hard gates are facts the daemon enforces (unset required secret, harness mismatch, cooldown, provider window); gates resting on cached account windows or a recent failed run are advisory and expire (1 h) or clear on a credential change."
    covers: { tests: [TestDerive_NotSignedIn_UnsetSecretIsHard, TestDerive_NotSignedIn_RecentAuthFailureIsAdvisory, TestDerive_ModelNotInPlan_RecentFailureIsAdvisoryPerModel, TestDerive_UnsupportedByHarness_CodexChatOnlyEndpoint, TestProviderGate_AccountChangeClearsRecentFailures], mutations: [M2, M12] }
  - id: AC5
    text: "provider.list entries carry gate and modelGates (additive, absent when nothing is gated); model gates differ from the provider gate only."
    covers: { tests: [TestProviderList_CarriesGateAndModelGates, TestModelGates_OnlyModelSpecificGates, TestProviderList_Gates_OutOfPlanFromCachedWindowsIsAdvisory], mutations: [M10] }
  - id: AC6
    text: "provider.updated is published when any gate changes (account change, failed run, usage event) and not when nothing changed."
    covers: { tests: [TestProviderUpdated_PublishedOnGateEdges, TestFailureHook_CalledForHumanFixAndPlanLimits], mutations: [M11, M9] }
  - id: AC7
    text: "A non-human send to a hard-gated provider is refused with the typed gated error naming the reason and what not to do; the cooldown fast-fail is the same typed error; out_of_plan, advisory gates and human sends are never refused by the gate."
    covers: { tests: [TestRunStart_GatedNonHumanRefusedTyped, TestRunStart_GatedErrorSaysWhatNotToDo, TestProviderCooldownError_IsTypedGate, TestRunStart_AdvisoryGateNotRefused, TestRunStart_OutOfPlanNotRefused, TestRunStart_HumanNotRefusedByGate], mutations: [M6, M7, M8] }
  - id: AC8
    text: "The MCP orchestration guide names every gate reason."
    covers: { tests: [TestMCPInstructions_ListEveryGateReason], mutations: [M13] }
tests:
  - { name: TestTwinTable_CoversEveryHumanFixAndLimitClass, run: "go test ./internal/gate -run '^TestTwinTable_CoversEveryHumanFixAndLimitClass$' -count=1", timeoutSec: 300 }
  - { name: TestGate_NoSecondDerivation, run: "go test ./internal/gate -run '^TestGate_NoSecondDerivation$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_PrecedenceMatrix, run: "go test ./internal/gate -run '^TestDerive_PrecedenceMatrix$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_UntilIsLaterOfWindowAndCooldown, run: "go test ./internal/gate -run '^TestDerive_UntilIsLaterOfWindowAndCooldown$' -count=1", timeoutSec: 300 }
  - { name: TestGate_PackageImportsNoFetcher, run: "go test ./internal/gate -run '^TestGate_PackageImportsNoFetcher$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_OutOfPlan_TransientBackoffIsNotAGate, run: "go test ./internal/gate -run '^TestDerive_OutOfPlan_TransientBackoffIsNotAGate$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_OutOfPlan_OneHealthyAccountUngates, run: "go test ./internal/gate -run '^TestDerive_OutOfPlan_OneHealthyAccountUngates$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_NoGateWhenNothingKnown, run: "go test ./internal/gate -run '^TestDerive_NoGateWhenNothingKnown$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_NotSignedIn_UnsetSecretIsHard, run: "go test ./internal/gate -run '^TestDerive_NotSignedIn_UnsetSecretIsHard$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_NotSignedIn_RecentAuthFailureIsAdvisory, run: "go test ./internal/gate -run '^TestDerive_NotSignedIn_RecentAuthFailureIsAdvisory$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_ModelNotInPlan_RecentFailureIsAdvisoryPerModel, run: "go test ./internal/gate -run '^TestDerive_ModelNotInPlan_RecentFailureIsAdvisoryPerModel$' -count=1", timeoutSec: 300 }
  - { name: TestDerive_UnsupportedByHarness_CodexChatOnlyEndpoint, run: "go test ./internal/gate -run '^TestDerive_UnsupportedByHarness_CodexChatOnlyEndpoint$' -count=1", timeoutSec: 300 }
  - { name: TestProviderGate_AccountChangeClearsRecentFailures, run: "go test ./internal/wsapi -run '^TestProviderGate_AccountChangeClearsRecentFailures$' -count=1", timeoutSec: 300 }
  - { name: TestProviderList_CarriesGateAndModelGates, run: "go test ./internal/wsapi -run '^TestProviderList_CarriesGateAndModelGates$' -count=1", timeoutSec: 300 }
  - { name: TestModelGates_OnlyModelSpecificGates, run: "go test ./internal/gate -run '^TestModelGates_OnlyModelSpecificGates$' -count=1", timeoutSec: 300 }
  - { name: TestProviderList_Gates_OutOfPlanFromCachedWindowsIsAdvisory, run: "go test ./internal/wsapi -run '^TestProviderList_Gates_OutOfPlanFromCachedWindowsIsAdvisory$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUpdated_PublishedOnGateEdges, run: "go test ./internal/wsapi -run '^TestProviderUpdated_PublishedOnGateEdges$' -count=1", timeoutSec: 300 }
  - { name: TestFailureHook_CalledForHumanFixAndPlanLimits, run: "go test ./internal/runs -run '^TestFailureHook_CalledForHumanFixAndPlanLimits$' -count=1", timeoutSec: 300 }
  - { name: TestRunStart_GatedNonHumanRefusedTyped, run: "go test ./internal/runs -run '^TestRunStart_GatedNonHumanRefusedTyped$' -count=1", timeoutSec: 300 }
  - { name: TestRunStart_GatedErrorSaysWhatNotToDo, run: "go test ./internal/runs -run '^TestRunStart_GatedErrorSaysWhatNotToDo$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldownError_IsTypedGate, run: "go test ./internal/runs -run '^TestProviderCooldownError_IsTypedGate$' -count=1", timeoutSec: 300 }
  - { name: TestRunStart_AdvisoryGateNotRefused, run: "go test ./internal/runs -run '^TestRunStart_AdvisoryGateNotRefused$' -count=1", timeoutSec: 300 }
  - { name: TestRunStart_OutOfPlanNotRefused, run: "go test ./internal/runs -run '^TestRunStart_OutOfPlanNotRefused$' -count=1", timeoutSec: 300 }
  - { name: TestRunStart_HumanNotRefusedByGate, run: "go test ./internal/runs -run '^TestRunStart_HumanNotRefusedByGate$' -count=1", timeoutSec: 300 }
  - { name: TestMCPInstructions_ListEveryGateReason, run: "go test ./cmd/smind -run '^TestMCPInstructions_ListEveryGateReason$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: "internal/gate/derive.go", change: "drop the TransientOnly exclusion in accountsExhausted", expectFail: [TestDerive_OutOfPlan_TransientBackoffIsNotAGate] }
  - { id: M2, file: "internal/gate/derive.go", change: "make the recent auth failure gate hard (drop Advisory)", expectFail: [TestDerive_NotSignedIn_RecentAuthFailureIsAdvisory] }
  - { id: M3, file: "internal/gate/derive.go", change: "a single healthy account no longer ungates the provider (continue instead of return)", expectFail: [TestDerive_OutOfPlan_OneHealthyAccountUngates] }
  - { id: M4, file: "internal/gate/derive.go", change: "cooling_down beats the account out_of_plan gate (accountsExhausted ... && !cooling)", expectFail: [TestDerive_PrecedenceMatrix] }
  - { id: M5, file: "internal/gate/gate.go", change: "import internal/quota (a fetch-capable package) into the gate", expectFail: [TestGate_PackageImportsNoFetcher] }
  - { id: M6, file: "internal/runs/queue.go", change: "refuse advisory gates too (g != nil instead of g.Hard())", expectFail: [TestRunStart_AdvisoryGateNotRefused] }
  - { id: M7, file: "internal/runs/queue.go", change: "drop the out_of_plan and cooling_down exclusion from the gated refusal", expectFail: [TestRunStart_OutOfPlanNotRefused] }
  - { id: M8, file: "internal/runs/queue.go", change: "apply the gated refusal to human sends", expectFail: [TestRunStart_HumanNotRefusedByGate] }
  - { id: M9, file: "internal/runs/registry.go", change: "never call the failure hook", expectFail: [TestFailureHook_CalledForHumanFixAndPlanLimits] }
  - { id: M10, file: "internal/wsapi/handlers.go", change: "do not fill gate and modelGates in provider.list", expectFail: [TestProviderList_CarriesGateAndModelGates] }
  - { id: M11, file: "internal/wsapi/provider_gate.go", change: "publish provider.updated on every call (drop the signature compare)", expectFail: [TestProviderUpdated_PublishedOnGateEdges] }
  - { id: M12, file: "internal/wsapi/provider_gate.go", change: "do not clear the recent failures on a credential change", expectFail: [TestProviderGate_AccountChangeClearsRecentFailures] }
  - { id: M13, file: "cmd/smind/mcp_instructions.go", change: "remove the model_not_in_plan line from the guide", expectFail: [TestMCPInstructions_ListEveryGateReason] }
commands:
  - { id: C1, run: "go vet ./... && test -z \"$(gofmt -l $(git ls-files '*.go'))\"", timeoutSec: 900 }
  - { id: C2, run: "go test ./internal/gate ./internal/runs ./internal/wsapi ./cmd/smind ./internal/routing", timeoutSec: 1500 }
  - { id: C3, run: "GOOS=windows go vet ./internal/gate/ ./internal/runs/ ./internal/wsapi/ ./internal/routing/ ./cmd/smind/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---
