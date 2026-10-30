---
v: 1
milestone: "Steward seat S1: permission policy engine and audit (ADR-0047)"
base: origin/develop
planRef: docs/plans/active/steward-seat.md
acs:
  - id: AC1
    text: "With steward.policy.enabled false or absent the permission path is today's: no audit row, no new resolution, no behaviour change."
    covers: { tests: [TestDecider_PolicyOffMatchesGolden, TestDecide_DisabledIsNoop, TestDecider_BypassModeNeverAsksSoPolicyNotConsulted], mutations: [M8] }
  - id: AC2
    text: "The loader is strict (anchors, tags, merge keys, duplicate keys, unknown fields, wildcards, regex, caps, allow without tool, mutating allow without paths.within, allow a floor rule denies) and every rejection names a field path."
    covers: { tests: [TestPolicyLoad_ValidV1, TestPolicyLoad_RejectsDuplicateKeyAnchorTagUnknownField, TestPolicyLoad_RejectsWildcardSeatToolArgv, TestPolicyLoad_RejectsMutatingAllowWithoutPathScope, TestPolicyLoad_RejectsAllowThatFloorDenies, TestPolicyLoad_RejectsRegexAndOverCaps], mutations: [M6] }
  - id: AC3
    text: "Precedence is deny over ask over allow regardless of file order; unmatched is ask; an allow needs a rule for every simple command."
    covers: { tests: [TestDecide_PrecedenceDenyAskAllowIgnoresOrder, TestDecide_UnmatchedIsAsk, TestDecide_AllowNeedsEverySegmentCovered, TestDecide_CompoundWithAllowedThenDisallowedIsAsk, TestDecide_SeatScopedRule, TestDecide_WorkspaceScopedRule], mutations: [M1] }
  - id: AC4
    text: "The shell splitter handles the listed constructs, never matches heredoc bodies as commands (elided in audit), and anything unparseable makes the request ask."
    covers: { tests: [TestSplitCommand_Suite, FuzzSplitCommand, TestSplitCommand_DifferentialAgainstBashN, TestDecide_HeredocBodyNotMatchedAndElided, TestDecide_UnparseableIsAsk], mutations: [M3] }
  - id: AC5
    text: "Paths are realpath-resolved before the within check; a symlink or .. escape is not within; a missing path resolves by its deepest existing parent; a resolution failure is ask."
    covers: { tests: [TestDecide_SymlinkAndDotDotEscapeNotWithin, TestDecide_MissingPathResolvedByParent, TestDecide_PathResolutionFailureIsAsk], mutations: [M4] }
  - id: AC6
    text: "The floor denies with its id (no-stash, no-force-push, no-merge, no-install-state, no-secrets, no-destructive-outside incl. writes outside the worktree, no-curl-sh, no-real-home-tests, no-add-all), beats an allow rule, applies when the policy file is invalid, and does not deny the near misses."
    covers: { tests: [TestFloor_StashDenied, TestFloor_ForcePushVariantsDenied, TestFloor_MergeReleaseTagPublishDenied, TestFloor_InstallStateDenied, TestFloor_SecretsDenied, TestFloor_DestructiveOutsideWorktreeDenied, TestFloor_CurlPipeShellDenied, TestFloor_RealHomeTestsDenied, TestFloor_AddAllDenied, TestFloor_NearMissesNotDenied, TestFloor_AppliesEvenWhenPolicyInvalid, TestFloor_BeatsAllowRule], mutations: [M2, M9] }
  - id: AC7
    text: "git push origin <taskBranch> is allowed by the rule only for the matching task branch; other refs ask; force-with-lease, +ref and :ref are floor denies."
    covers: { tests: [TestPushOwnBranch_AllowedOnlyForTaskBranch, TestPushOwnBranch_OtherRefsAsk, TestFloor_ForcePushVariantsDenied], mutations: [M2] }
  - id: AC8
    text: "An invalid, unreadable or oversize policy file evaluates as floor plus ask, never allow-all, and a valid edit is picked up without a restart."
    covers: { tests: [TestPolicyLoad_InvalidFileMeansFloorPlusAsk, TestPolicyReload_PicksUpEditWithoutRestart], mutations: [M10] }
  - id: AC9
    text: "Every evaluation appends one redacted, capped audit row; the human answer or the timeout appends a second row linked by requestId; there is no update or delete RPC; rows older than 90 days are swept."
    covers: { tests: [TestAudit_EveryEvaluationAppendsRow, TestAudit_HumanAndTimeoutOutcomeLinkedByRequestID, TestAudit_RedactsAndElides, TestAudit_AppendOnlyNoMutatingRPC, TestAudit_RetentionSweep90Days], mutations: [M7] }
  - id: AC10
    text: "permission_resolved carries policy_allow or policy_deny with the rule id; an unanswered escalated request is denied after the timeout, never allowed, and audited; a floor deny's message names the floor id."
    covers: { tests: [TestDecider_PolicyAllowResolvesWithoutHuman, TestDecider_PolicyDenyResolvesToDenyOption, TestDecider_AskFallsThroughToHumanPath, TestDecider_AskTimeoutDeniesAndAudits, TestPermissionResolved_PolicyResolutionsEncodeAndDecode, TestClaudeAdapter_PolicyDenyMessageCarriesFloorID], mutations: [M5] }
  - id: AC11
    text: "No RPC or MCP tool writes the policy; policy.get, policy.test and policy.audit are readable over the relay; policy.test explains each segment without side effects."
    covers: { tests: [TestNoPolicyWriteRPCOrMCPTool, TestMCPCatalog_HasNoPolicyTool, TestPolicyRPCs_ReadOnlyAndRelayCanRead, TestPolicyTest_ExplainsEachSegmentNoSideEffects], mutations: [M11] }
  - id: AC12
    text: "smind policy init writes a commented template with zero active allow rules that loads valid, and refuses to overwrite."
    covers: { tests: [TestCLI_PolicyInitWritesTemplateWithZeroAllowRules, TestCLI_PolicyInitRefusesOverwrite, TestTemplate_LoadsWithZeroAllowRules] }
tests:
  - { name: TestPolicyLoad_ValidV1, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_ValidV1$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_RejectsDuplicateKeyAnchorTagUnknownField, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_RejectsDuplicateKeyAnchorTagUnknownField$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_RejectsWildcardSeatToolArgv, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_RejectsWildcardSeatToolArgv$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_RejectsMutatingAllowWithoutPathScope, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_RejectsMutatingAllowWithoutPathScope$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_RejectsAllowThatFloorDenies, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_RejectsAllowThatFloorDenies$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_RejectsRegexAndOverCaps, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_RejectsRegexAndOverCaps$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyLoad_InvalidFileMeansFloorPlusAsk, run: "go test ./internal/permpolicy -run '^TestPolicyLoad_InvalidFileMeansFloorPlusAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPolicyReload_PicksUpEditWithoutRestart, run: "go test ./internal/permpolicy -run '^TestPolicyReload_PicksUpEditWithoutRestart$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_DisabledIsNoop, run: "go test ./internal/permpolicy -run '^TestDecide_DisabledIsNoop$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_PrecedenceDenyAskAllowIgnoresOrder, run: "go test ./internal/permpolicy -run '^TestDecide_PrecedenceDenyAskAllowIgnoresOrder$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_UnmatchedIsAsk, run: "go test ./internal/permpolicy -run '^TestDecide_UnmatchedIsAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_AllowNeedsEverySegmentCovered, run: "go test ./internal/permpolicy -run '^TestDecide_AllowNeedsEverySegmentCovered$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_CompoundWithAllowedThenDisallowedIsAsk, run: "go test ./internal/permpolicy -run '^TestDecide_CompoundWithAllowedThenDisallowedIsAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_HeredocBodyNotMatchedAndElided, run: "go test ./internal/permpolicy -run '^TestDecide_HeredocBodyNotMatchedAndElided$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_UnparseableIsAsk, run: "go test ./internal/permpolicy -run '^TestDecide_UnparseableIsAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_SymlinkAndDotDotEscapeNotWithin, run: "go test ./internal/permpolicy -run '^TestDecide_SymlinkAndDotDotEscapeNotWithin$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_MissingPathResolvedByParent, run: "go test ./internal/permpolicy -run '^TestDecide_MissingPathResolvedByParent$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_PathResolutionFailureIsAsk, run: "go test ./internal/permpolicy -run '^TestDecide_PathResolutionFailureIsAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_SeatScopedRule, run: "go test ./internal/permpolicy -run '^TestDecide_SeatScopedRule$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecide_WorkspaceScopedRule, run: "go test ./internal/permpolicy -run '^TestDecide_WorkspaceScopedRule$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_StashDenied, run: "go test ./internal/permpolicy -run '^TestFloor_StashDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_ForcePushVariantsDenied, run: "go test ./internal/permpolicy -run '^TestFloor_ForcePushVariantsDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_MergeReleaseTagPublishDenied, run: "go test ./internal/permpolicy -run '^TestFloor_MergeReleaseTagPublishDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_InstallStateDenied, run: "go test ./internal/permpolicy -run '^TestFloor_InstallStateDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_SecretsDenied, run: "go test ./internal/permpolicy -run '^TestFloor_SecretsDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_DestructiveOutsideWorktreeDenied, run: "go test ./internal/permpolicy -run '^TestFloor_DestructiveOutsideWorktreeDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_CurlPipeShellDenied, run: "go test ./internal/permpolicy -run '^TestFloor_CurlPipeShellDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_RealHomeTestsDenied, run: "go test ./internal/permpolicy -run '^TestFloor_RealHomeTestsDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_AddAllDenied, run: "go test ./internal/permpolicy -run '^TestFloor_AddAllDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_NearMissesNotDenied, run: "go test ./internal/permpolicy -run '^TestFloor_NearMissesNotDenied$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_AppliesEvenWhenPolicyInvalid, run: "go test ./internal/permpolicy -run '^TestFloor_AppliesEvenWhenPolicyInvalid$' -count=1 -race", timeoutSec: 300 }
  - { name: TestFloor_BeatsAllowRule, run: "go test ./internal/permpolicy -run '^TestFloor_BeatsAllowRule$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPushOwnBranch_AllowedOnlyForTaskBranch, run: "go test ./internal/permpolicy -run '^TestPushOwnBranch_AllowedOnlyForTaskBranch$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPushOwnBranch_OtherRefsAsk, run: "go test ./internal/permpolicy -run '^TestPushOwnBranch_OtherRefsAsk$' -count=1 -race", timeoutSec: 300 }
  - { name: TestSplitCommand_DifferentialAgainstBashN, run: "go test ./internal/permpolicy -run '^TestSplitCommand_DifferentialAgainstBashN$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAudit_EveryEvaluationAppendsRow, run: "go test ./internal/permpolicy -run '^TestAudit_EveryEvaluationAppendsRow$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAudit_HumanAndTimeoutOutcomeLinkedByRequestID, run: "go test ./internal/permpolicy -run '^TestAudit_HumanAndTimeoutOutcomeLinkedByRequestID$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAudit_RedactsAndElides, run: "go test ./internal/permpolicy -run '^TestAudit_RedactsAndElides$' -count=1 -race", timeoutSec: 300 }
  - { name: TestTemplate_LoadsWithZeroAllowRules, run: "go test ./internal/permpolicy -run '^TestTemplate_LoadsWithZeroAllowRules$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecider_PolicyAllowResolvesWithoutHuman, run: "go test ./internal/runs -run '^TestDecider_PolicyAllowResolvesWithoutHuman$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_PolicyDenyResolvesToDenyOption, run: "go test ./internal/runs -run '^TestDecider_PolicyDenyResolvesToDenyOption$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_AskFallsThroughToHumanPath, run: "go test ./internal/runs -run '^TestDecider_AskFallsThroughToHumanPath$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_AskTimeoutDeniesAndAudits, run: "go test ./internal/runs -run '^TestDecider_AskTimeoutDeniesAndAudits$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_PolicyOffMatchesGolden, run: "go test ./internal/runs -run '^TestDecider_PolicyOffMatchesGolden$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_BypassModeNeverAsksSoPolicyNotConsulted, run: "go test ./internal/runs -run '^TestDecider_BypassModeNeverAsksSoPolicyNotConsulted$' -count=1 -race", timeoutSec: 600 }
  - { name: TestPermissionResolved_PolicyResolutionsEncodeAndDecode, run: "go test ./internal/runs -run '^TestPermissionResolved_PolicyResolutionsEncodeAndDecode$' -count=1 -race", timeoutSec: 600 }
  - { name: TestAudit_RetentionSweep90Days, run: "go test ./internal/store -run '^TestAudit_RetentionSweep90Days$' -count=1 -race", timeoutSec: 300 }
  - { name: TestClaudeAdapter_PolicyDenyMessageCarriesFloorID, run: "go test ./internal/taskrunner -run '^TestClaudeAdapter_PolicyDenyMessageCarriesFloorID$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAudit_AppendOnlyNoMutatingRPC, run: "go test ./internal/wsapi -run '^TestAudit_AppendOnlyNoMutatingRPC$' -count=1 -race", timeoutSec: 600 }
  - { name: TestPolicyRPCs_ReadOnlyAndRelayCanRead, run: "go test ./internal/wsapi -run '^TestPolicyRPCs_ReadOnlyAndRelayCanRead$' -count=1 -race", timeoutSec: 600 }
  - { name: TestNoPolicyWriteRPCOrMCPTool, run: "go test ./internal/wsapi -run '^TestNoPolicyWriteRPCOrMCPTool$' -count=1 -race", timeoutSec: 600 }
  - { name: TestPolicyTest_ExplainsEachSegmentNoSideEffects, run: "go test ./internal/wsapi -run '^TestPolicyTest_ExplainsEachSegmentNoSideEffects$' -count=1 -race", timeoutSec: 600 }
  - { name: TestCLI_PolicyInitWritesTemplateWithZeroAllowRules, run: "go test ./cmd/smind -run '^TestCLI_PolicyInitWritesTemplateWithZeroAllowRules$' -count=1 -race", timeoutSec: 300 }
  - { name: TestCLI_PolicyInitRefusesOverwrite, run: "go test ./cmd/smind -run '^TestCLI_PolicyInitRefusesOverwrite$' -count=1 -race", timeoutSec: 300 }
  - { name: TestMCPCatalog_HasNoPolicyTool, run: "go test ./cmd/smind -run '^TestMCPCatalog_HasNoPolicyTool$' -count=1 -race", timeoutSec: 300 }
  - { name: FuzzSplitCommand, run: "go test ./internal/permpolicy -run '^FuzzSplitCommand$' -count=1", timeoutSec: 300 }
  - { name: TestSplitCommand_Suite, run: "go test ./internal/permpolicy -run '^TestSplitCommand_' -count=1 -race", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/permpolicy/decide.go, change: "new-file: let an allow rule win over a deny rule when both match", expectFail: [TestDecide_PrecedenceDenyAskAllowIgnoresOrder] }
  - { id: M2, file: internal/permpolicy/floor.go, change: "new-file: drop --force-with-lease from the force-push flag set", expectFail: [TestFloor_ForcePushVariantsDenied] }
  - { id: M3, file: internal/permpolicy/split.go, change: "new-file: return no Unparsed reason for an unbalanced quote or eval so the request can be allowed", expectFail: [TestDecide_UnparseableIsAsk] }
  - { id: M4, file: internal/permpolicy/paths.go, change: "new-file: compare Within on the unresolved path (skip realpath)", expectFail: [TestDecide_SymlinkAndDotDotEscapeNotWithin] }
  - { id: M5, file: internal/runs/decider_policy.go, change: "new-file: resolve the 5 minute timeout to the first option instead of the reject option", expectFail: [TestDecider_AskTimeoutDeniesAndAudits] }
  - { id: M6, file: internal/permpolicy/load.go, change: "new-file: accept an allow rule with no paths.within", expectFail: [TestPolicyLoad_RejectsMutatingAllowWithoutPathScope] }
  - { id: M7, file: internal/permpolicy/engine.go, change: "new-file: write the audit target without redaction", expectFail: [TestAudit_RedactsAndElides] }
  - { id: M8, file: internal/permpolicy/engine.go, change: "new-file: evaluate even when Enabled() is false", expectFail: [TestDecide_DisabledIsNoop] }
  - { id: M9, file: internal/permpolicy/floor.go, change: "new-file: skip the floor when the policy has an allow rule for the segment", expectFail: [TestFloor_BeatsAllowRule] }
  - { id: M10, file: internal/permpolicy/engine.go, change: "new-file: treat an invalid policy file as an empty allow-all policy", expectFail: [TestPolicyLoad_InvalidFileMeansFloorPlusAsk] }
  - { id: M11, file: internal/wsapi/policy.go, change: "new-file: register a policy.set handler", expectFail: [TestNoPolicyWriteRPCOrMCPTool] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# S1 validation contract

Judge the branch head against these ACs and the binding text in `docs/decisions/0047-steward-seat.md` section 2 (the floor table 2.1, audit 2.3, timeout 2.4) and `docs/plans/active/steward-seat.md` section S1.
Attack targets beyond the listed tests: (1) a floor entry that a policy file can override or that is skipped when the policy is invalid or the command does not parse; (2) a compound
command (`;`, `&&`, `|`, `$(...)`, backticks, heredoc) that reaches `allow` while one segment is not covered; (3) a path that escapes `paths.within` through a symlink or `..`;
(4) the timeout resolving to anything but a reject option; (5) unredacted secrets or heredoc bodies in `permission_policy_audit`; (6) any RPC, MCP tool or run-scoped method that
writes the policy or the audit; (7) the engine consulted, an audit row written or a new resolution emitted while `steward.policy.enabled` is false; (8) a change to `tasks`, `migrate.go`
or an existing wire field (additive only). No real home, no live model call: tests use `HOME=$(mktemp -d)`.
