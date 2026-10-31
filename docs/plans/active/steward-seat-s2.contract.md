---
v: 1
milestone: "Steward seat S2: read-only review seat and review loop (ADR-0047 D7-D10)"
base: origin/develop
planRef: docs/plans/active/steward-seat.md
acs:
  - id: AC13
    text: "steward.review is accepted only from the Human (direct or relay) or the daemon's own trigger; refused for run-scoped agents, Peers and automation-started tasks; it creates a detached scratch worktree at the exact sha (an ancestor of the branch head) and one run on the steward profile; one open review per task."
    covers: { tests: [TestReview_OnlyHumanOrDaemon, TestReview_RefusedForRunScopedAndAutomationOrigin, TestReview_CreatesScratchWorktreeAtSha, TestReview_RefusesNonAncestorSha, TestReview_SingleFlightPerTask, TestAdmission_LeadCannotLaunchStewardProfile], mutations: [M7] }
  - id: AC14
    text: "The daemon builds the digest deterministically with no agent: binaries, churn, scope, stacking, PR hygiene, CI at head, validator verdict at head, frames evidence and matrix, Peer handoffs, secret counts, tests ratio, policy-audit counts; at most 64 KiB; untrusted free text is capped and kept under the untrusted block."
    covers: { tests: [TestDigest_BinaryDetectedFromNumstat, TestDigest_ChurnGenSchemasLockfile, TestDigest_FilesOutsideWriteScope, TestDigest_StackedBranchDetected, TestDigest_PRBaseNotDevelop, TestDigest_ConventionalCommitsTitle, TestDigest_PRBodyMissingNotDone, TestDigest_CIStatesMapped, TestDigest_ValidatorStaleShaNotPass, TestDigest_FramesCapturedAtOtherShaFlagged, TestDigest_FramesMatrixMissingCell, TestDigest_PeerHandoffNotVerifiedMustBeRepeated, TestDigest_SecretScanCountsOnly, TestDigest_CappedAt64KiB, TestDigest_UntrustedTextDelimitedAndCapped, TestCollectGit_StackedBranchForeignCommits], mutations: [M3] }
  - id: AC15
    text: "The seat's MCP entry carries steward_digest_get and steward_verdict_submit and nothing else; the token is in that entry's env only, hashed at rest, single use; a Lead, Peer, wrong run, reused token or ended run is refused; the Lead catalogue has no steward tool."
    covers: { tests: [TestStewardMCPEntry_OnlyTwoToolsByDefault, TestStewardMCPEntry_TokenOnlyInSeatEnv, TestStewardMCPEntry_TokenFromEnvNotArgument, TestVerdictSubmit_RequiresToken, TestVerdictSubmit_TokenOnceOnly, TestVerdictSubmit_LeadCannotWrite, TestVerdictSubmit_SeatRunSucceedsOnce, TestDigestGet_OnlySeatOwnRun, TestStewardTokenNeverInPromptOrDigest, TestStewardSeatSource_EnvOnlyInSeatTask, TestStewardReviews_TokenSingleUse, TestMCPCatalog_HasNoPolicyTool, TestMCPCalledMethodsAreRunScopedAllowed, TestRunScopedMethods_IncludeStewardSeatCalls], mutations: [M2, M2b, M10] }
  - id: AC16
    text: "The seat has no Bash and no write tool (provider deny list) and its own permission requests resolve through the seat floor: Read, Grep and Glob inside the scratch tree and its own two MCP tools are allowed, everything else is denied, before AutoAccept and even when the policy engine is off."
    covers: { tests: [TestStewardSeat_NoBashNoWriteTool, TestStewardSeatFloor_ReadInScratchAllowedElseDeny, TestStewardSeatFloor_OwnMCPToolsAllowed, TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff, TestDecider_SeatFloorBeforeAutoAccept, TestDecider_SeatFloorNotConsultedForOtherTasks, TestRunContract_DisallowedToolsReachClaudeOptions, TestStewardPrompt_ContainsOnlyDigestAndFixedText], mutations: [M4, M6, M9] }
  - id: AC17
    text: "steward.verdict.submit requires, for ACCEPTED, an acknowledgement of every gate and an observation per frame; CHANGES needs findings each with file, line, why and the check; BLOCKED needs blockedReason; an incomplete verdict is rejected naming the missing item and does not consume the token."
    covers: { tests: [TestVerdictSubmit_AcceptedNeedsAllGatesAndFrameObservations, TestVerdictSubmit_ChangesNeedsFindingsWithCheck, TestVerdictSubmit_BlockedNeedsReason] }
  - id: AC18
    text: "The stored verdict is the minimum of the seat's verdict and the gates (failing gate at most CHANGES; not_stacked and no_secrets BLOCKED); CI pending holds the review pending and publishes nothing; the seat's value is kept as seatVerdict; the daemon never writes ACCEPTED."
    covers: { tests: [TestClamp_FailingGateCapsAtChanges, TestClamp_StackedAndSecretCapAtBlocked, TestClamp_SeatCannotRaiseAboveGates, TestClamp_CIPendingHoldsReviewPending, TestDaemonNeverWritesAccepted], mutations: [M1] }
  - id: AC19
    text: "Daemon-written BLOCKED only for timeout, budget, crashed (and cancelled, scope_violation); the wall clock excludes rate-limit waits; the Lead worktree HEAD and status digest are compared before and after."
    covers: { tests: [TestTimeoutBudgetCrashWriteBlocked, TestRateLimitWaitDoesNotConsumeWallClock, TestPostCheck_LeadWorktreeChangedIsScopeViolation, TestStewardCancel_NotRecordedAsCrash] }
  - id: AC20
    text: "A review is stale when the head moves, the contract version changes or a newer review exists; mergeReady is computed on read; there is no merge, approve or accept RPC."
    covers: { tests: [TestReviewStale_HeadMoves, TestReviewStale_NewContractVersion, TestReviewStale_NewerReview, TestNoStewardMergeRPC, TestCLI_StewardHasNoApproveOrMerge] }
  - id: AC21
    text: "Triggers and rails: steward.autoReview and steward.enabled default off, invalid config is off; the auto trigger fires once for a root Lead done run with a branch; dedupe by (head, contract, CI); 6 per hour, 5 per task, budget and minutes ceilings, auto-pause after 3 consecutive crashed or timeout reviews."
    covers: { tests: [TestAutoReview_OffByDefault, TestAutoReview_FiresOnceForRootLeadDone, TestAutoReview_NotForErrorStoppedOrPeer, TestStewardObserver_MapsRunFinishedToTrigger, TestRails_DedupeSameHeadContractCI, TestRails_MaxReviewsPerHour, TestRails_MaxReviewsPerTask, TestRails_BudgetCeilingRefused, TestRails_AutoPauseAfterThreeCrashes, TestSwitches_EnabledFalseAndInvalidConfigOff, TestStewardConfig_DefaultsOff, TestStewardConfig_ValidateRejectsOverCeilingAndNegative], mutations: [M5, M8] }
  - id: AC22
    text: "The role pack seeds steward as the fourth profile (idempotent, never rewrites an existing one)."
    covers: { tests: [TestSeedRoles_StewardFourthAndIdempotent, TestSeedRoles_ExistingStewardKept] }
  - id: AC23
    text: "CLI: smind steward review|list|show|cancel (--json); no approve or merge subcommand."
    covers: { tests: [TestCLI_StewardHasNoApproveOrMerge] }
tests:
  - { name: TestDigest_BinaryDetectedFromNumstat, run: "go test ./internal/steward -run '^TestDigest_BinaryDetectedFromNumstat$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_ChurnGenSchemasLockfile, run: "go test ./internal/steward -run '^TestDigest_ChurnGenSchemasLockfile$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_FilesOutsideWriteScope, run: "go test ./internal/steward -run '^TestDigest_FilesOutsideWriteScope$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_StackedBranchDetected, run: "go test ./internal/steward -run '^TestDigest_StackedBranchDetected$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_PRBaseNotDevelop, run: "go test ./internal/steward -run '^TestDigest_PRBaseNotDevelop$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_ConventionalCommitsTitle, run: "go test ./internal/steward -run '^TestDigest_ConventionalCommitsTitle$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_PRBodyMissingNotDone, run: "go test ./internal/steward -run '^TestDigest_PRBodyMissingNotDone$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_CIStatesMapped, run: "go test ./internal/steward -run '^TestDigest_CIStatesMapped$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_ValidatorStaleShaNotPass, run: "go test ./internal/steward -run '^TestDigest_ValidatorStaleShaNotPass$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_FramesCapturedAtOtherShaFlagged, run: "go test ./internal/steward -run '^TestDigest_FramesCapturedAtOtherShaFlagged$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_FramesMatrixMissingCell, run: "go test ./internal/steward -run '^TestDigest_FramesMatrixMissingCell$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_PeerHandoffNotVerifiedMustBeRepeated, run: "go test ./internal/steward -run '^TestDigest_PeerHandoffNotVerifiedMustBeRepeated$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_SecretScanCountsOnly, run: "go test ./internal/steward -run '^TestDigest_SecretScanCountsOnly$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_CappedAt64KiB, run: "go test ./internal/steward -run '^TestDigest_CappedAt64KiB$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDigest_UntrustedTextDelimitedAndCapped, run: "go test ./internal/steward -run '^TestDigest_UntrustedTextDelimitedAndCapped$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardPrompt_ContainsOnlyDigestAndFixedText, run: "go test ./internal/steward -run '^TestStewardPrompt_ContainsOnlyDigestAndFixedText$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardMCPEntry_OnlyTwoToolsByDefault, run: "go test ./internal/steward -run '^TestStewardMCPEntry_OnlyTwoToolsByDefault$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardMCPEntry_TokenOnlyInSeatEnv, run: "go test ./internal/steward -run '^TestStewardMCPEntry_TokenOnlyInSeatEnv$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardSeat_NoBashNoWriteTool, run: "go test ./internal/steward -run '^TestStewardSeat_NoBashNoWriteTool$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardSeatFloor_ReadInScratchAllowedElseDeny, run: "go test ./internal/steward -run '^TestStewardSeatFloor_ReadInScratchAllowedElseDeny$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardSeatFloor_OwnMCPToolsAllowed, run: "go test ./internal/steward -run '^TestStewardSeatFloor_OwnMCPToolsAllowed$' -count=1 -race", timeoutSec: 300 }
  - { name: TestVerdictSubmit_RequiresToken, run: "go test ./internal/steward -run '^TestVerdictSubmit_RequiresToken$' -count=1 -race", timeoutSec: 300 }
  - { name: TestVerdictSubmit_TokenOnceOnly, run: "go test ./internal/steward -run '^TestVerdictSubmit_TokenOnceOnly$' -count=1 -race", timeoutSec: 300 }
  - { name: TestVerdictSubmit_AcceptedNeedsAllGatesAndFrameObservations, run: "go test ./internal/steward -run '^TestVerdictSubmit_AcceptedNeedsAllGatesAndFrameObservations$' -count=1 -race", timeoutSec: 300 }
  - { name: TestVerdictSubmit_ChangesNeedsFindingsWithCheck, run: "go test ./internal/steward -run '^TestVerdictSubmit_ChangesNeedsFindingsWithCheck$' -count=1 -race", timeoutSec: 300 }
  - { name: TestVerdictSubmit_BlockedNeedsReason, run: "go test ./internal/steward -run '^TestVerdictSubmit_BlockedNeedsReason$' -count=1 -race", timeoutSec: 300 }
  - { name: TestClamp_FailingGateCapsAtChanges, run: "go test ./internal/steward -run '^TestClamp_FailingGateCapsAtChanges$' -count=1 -race", timeoutSec: 300 }
  - { name: TestClamp_StackedAndSecretCapAtBlocked, run: "go test ./internal/steward -run '^TestClamp_StackedAndSecretCapAtBlocked$' -count=1 -race", timeoutSec: 300 }
  - { name: TestClamp_SeatCannotRaiseAboveGates, run: "go test ./internal/steward -run '^TestClamp_SeatCannotRaiseAboveGates$' -count=1 -race", timeoutSec: 300 }
  - { name: TestClamp_CIPendingHoldsReviewPending, run: "go test ./internal/steward -run '^TestClamp_CIPendingHoldsReviewPending$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDaemonNeverWritesAccepted, run: "go test ./internal/steward -run '^TestDaemonNeverWritesAccepted$' -count=1 -race", timeoutSec: 300 }
  - { name: TestTimeoutBudgetCrashWriteBlocked, run: "go test ./internal/steward -run '^TestTimeoutBudgetCrashWriteBlocked$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRateLimitWaitDoesNotConsumeWallClock, run: "go test ./internal/steward -run '^TestRateLimitWaitDoesNotConsumeWallClock$' -count=1 -race", timeoutSec: 300 }
  - { name: TestPostCheck_LeadWorktreeChangedIsScopeViolation, run: "go test ./internal/steward -run '^TestPostCheck_LeadWorktreeChangedIsScopeViolation$' -count=1 -race", timeoutSec: 300 }
  - { name: TestReviewStale_HeadMoves, run: "go test ./internal/steward -run '^TestReviewStale_HeadMoves$' -count=1 -race", timeoutSec: 300 }
  - { name: TestReviewStale_NewContractVersion, run: "go test ./internal/steward -run '^TestReviewStale_NewContractVersion$' -count=1 -race", timeoutSec: 300 }
  - { name: TestReviewStale_NewerReview, run: "go test ./internal/steward -run '^TestReviewStale_NewerReview$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAutoReview_OffByDefault, run: "go test ./internal/steward -run '^TestAutoReview_OffByDefault$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAutoReview_FiresOnceForRootLeadDone, run: "go test ./internal/steward -run '^TestAutoReview_FiresOnceForRootLeadDone$' -count=1 -race", timeoutSec: 300 }
  - { name: TestAutoReview_NotForErrorStoppedOrPeer, run: "go test ./internal/steward -run '^TestAutoReview_NotForErrorStoppedOrPeer$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRails_DedupeSameHeadContractCI, run: "go test ./internal/steward -run '^TestRails_DedupeSameHeadContractCI$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRails_MaxReviewsPerHour, run: "go test ./internal/steward -run '^TestRails_MaxReviewsPerHour$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRails_MaxReviewsPerTask, run: "go test ./internal/steward -run '^TestRails_MaxReviewsPerTask$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRails_BudgetCeilingRefused, run: "go test ./internal/steward -run '^TestRails_BudgetCeilingRefused$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRails_AutoPauseAfterThreeCrashes, run: "go test ./internal/steward -run '^TestRails_AutoPauseAfterThreeCrashes$' -count=1 -race", timeoutSec: 300 }
  - { name: TestSwitches_EnabledFalseAndInvalidConfigOff, run: "go test ./internal/steward -run '^TestSwitches_EnabledFalseAndInvalidConfigOff$' -count=1 -race", timeoutSec: 300 }
  - { name: TestCollectGit_StackedBranchForeignCommits, run: "go test ./internal/steward -run '^TestCollectGit_StackedBranchForeignCommits$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardReviews_TokenSingleUse, run: "go test ./internal/store -run '^TestStewardReviews_TokenSingleUse$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardReviews_OneOpenPerTask, run: "go test ./internal/store -run '^TestStewardReviews_OneOpenPerTask$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardReviews_VerdictWriteOnce, run: "go test ./internal/store -run '^TestStewardReviews_VerdictWriteOnce$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardConfig_DefaultsOff, run: "go test ./internal/config -run '^TestStewardConfig_DefaultsOff$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardConfig_ValidateRejectsOverCeilingAndNegative, run: "go test ./internal/config -run '^TestStewardConfig_ValidateRejectsOverCeilingAndNegative$' -count=1 -race", timeoutSec: 300 }
  - { name: TestSeedRoles_StewardFourthAndIdempotent, run: "go test ./internal/profiles -run '^TestSeedRoles_StewardFourthAndIdempotent$' -count=1 -race", timeoutSec: 300 }
  - { name: TestSeedRoles_ExistingStewardKept, run: "go test ./internal/profiles -run '^TestSeedRoles_ExistingStewardKept$' -count=1 -race", timeoutSec: 300 }
  - { name: TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff, run: "go test ./internal/runs -run '^TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_SeatFloorBeforeAutoAccept, run: "go test ./internal/runs -run '^TestDecider_SeatFloorBeforeAutoAccept$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDecider_SeatFloorNotConsultedForOtherTasks, run: "go test ./internal/runs -run '^TestDecider_SeatFloorNotConsultedForOtherTasks$' -count=1 -race", timeoutSec: 600 }
  - { name: TestRunContract_DisallowedToolsReachClaudeOptions, run: "go test ./internal/taskrunner -run '^TestRunContract_DisallowedToolsReachClaudeOptions$' -count=1 -race", timeoutSec: 600 }
  - { name: TestReview_OnlyHumanOrDaemon, run: "go test ./internal/wsapi -run '^TestReview_OnlyHumanOrDaemon$' -count=1 -race", timeoutSec: 600 }
  - { name: TestReview_RefusedForRunScopedAndAutomationOrigin, run: "go test ./internal/wsapi -run '^TestReview_RefusedForRunScopedAndAutomationOrigin$' -count=1 -race", timeoutSec: 600 }
  - { name: TestReview_CreatesScratchWorktreeAtSha, run: "go test ./internal/wsapi -run '^TestReview_CreatesScratchWorktreeAtSha$' -count=1 -race", timeoutSec: 600 }
  - { name: TestReview_RefusesNonAncestorSha, run: "go test ./internal/wsapi -run '^TestReview_RefusesNonAncestorSha$' -count=1 -race", timeoutSec: 600 }
  - { name: TestReview_SingleFlightPerTask, run: "go test ./internal/wsapi -run '^TestReview_SingleFlightPerTask$' -count=1 -race", timeoutSec: 600 }
  - { name: TestVerdictSubmit_LeadCannotWrite, run: "go test ./internal/wsapi -run '^TestVerdictSubmit_LeadCannotWrite$' -count=1 -race", timeoutSec: 600 }
  - { name: TestVerdictSubmit_SeatRunSucceedsOnce, run: "go test ./internal/wsapi -run '^TestVerdictSubmit_SeatRunSucceedsOnce$' -count=1 -race", timeoutSec: 600 }
  - { name: TestDigestGet_OnlySeatOwnRun, run: "go test ./internal/wsapi -run '^TestDigestGet_OnlySeatOwnRun$' -count=1 -race", timeoutSec: 600 }
  - { name: TestNoStewardMergeRPC, run: "go test ./internal/wsapi -run '^TestNoStewardMergeRPC$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardSeatSource_EnvOnlyInSeatTask, run: "go test ./internal/wsapi -run '^TestStewardSeatSource_EnvOnlyInSeatTask$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardTokenNeverInPromptOrDigest, run: "go test ./internal/wsapi -run '^TestStewardTokenNeverInPromptOrDigest$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardCancel_NotRecordedAsCrash, run: "go test ./internal/wsapi -run '^TestStewardCancel_NotRecordedAsCrash$' -count=1 -race", timeoutSec: 600 }
  - { name: TestAdmission_LeadCannotLaunchStewardProfile, run: "go test ./internal/wsapi -run '^TestAdmission_LeadCannotLaunchStewardProfile$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardObserver_MapsRunFinishedToTrigger, run: "go test ./internal/wsapi -run '^TestStewardObserver_MapsRunFinishedToTrigger$' -count=1 -race", timeoutSec: 600 }
  - { name: TestCLI_StewardHasNoApproveOrMerge, run: "go test ./cmd/smind -run '^TestCLI_StewardHasNoApproveOrMerge$' -count=1 -race", timeoutSec: 300 }
  - { name: TestStewardMCPEntry_TokenFromEnvNotArgument, run: "go test ./cmd/smind -run '^TestStewardMCPEntry_TokenFromEnvNotArgument$' -count=1 -race", timeoutSec: 300 }
  - { name: TestMCPCatalog_HasNoPolicyTool, run: "go test ./cmd/smind -run '^TestMCPCatalog_HasNoPolicyTool$' -count=1 -race", timeoutSec: 300 }
  - { name: TestMCPCalledMethodsAreRunScopedAllowed, run: "go test ./cmd/smind -run '^TestMCPCalledMethodsAreRunScopedAllowed$' -count=1 -race", timeoutSec: 300 }
  - { name: TestRunScopedMethods_IncludeStewardSeatCalls, run: "go test ./cmd/smind -run '^TestRunScopedMethods_IncludeStewardSeatCalls$' -count=1 -race", timeoutSec: 300 }
mutations:
  - { id: M1, file: "internal/steward/clamp.go", change: "return the seat's verdict unclamped (skip the gate cap)", expectFail: [TestClamp_SeatCannotRaiseAboveGates] }
  - { id: M2, file: "internal/steward/service.go", change: "accept any token (drop the constant-time hash compare in SubmitVerdict)", expectFail: [TestVerdictSubmit_RequiresToken] }
  - { id: M2b, file: "internal/wsapi/steward.go and internal/steward/service.go", change: "remove the handler's task-binding check AND make isSeatRun always true (both layers of the Lead-cannot-write defence)", expectFail: [TestVerdictSubmit_LeadCannotWrite] }
  - { id: M3, file: "internal/steward/gates.go", change: "compare the frame's capturing sha with the base sha instead of the head", expectFail: [TestDigest_FramesCapturedAtOtherShaFlagged] }
  - { id: M4, file: "internal/steward/seat.go", change: "add Bash to SeatBuiltinTools", expectFail: [TestStewardSeat_NoBashNoWriteTool] }
  - { id: M5, file: "internal/steward/rails.go", change: "drop the head sha from the dedupe lookup key", expectFail: [TestRails_DedupeSameHeadContractCI] }
  - { id: M6, file: "internal/steward/floor.go", change: "allow every tool inside the scratch tree (drop the builtin-tool check)", expectFail: [TestStewardSeatFloor_ReadInScratchAllowedElseDeny] }
  - { id: M7, file: "internal/wsapi/steward.go", change: "remove refuseAgent from steward.review", expectFail: [TestReview_RefusedForRunScopedAndAutomationOrigin] }
  - { id: M8, file: "internal/steward/trigger.go", change: "fire without checking cfg.AutoReviewFor", expectFail: [TestAutoReview_OffByDefault] }
  - { id: M9, file: "internal/runs/seat_source.go", change: "make applySeatFloor allow the request whatever the floor says", expectFail: [TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff] }
  - { id: M10, file: "internal/store/steward.go", change: "ConsumeStewardToken never fails on a used token", expectFail: [TestStewardReviews_TokenSingleUse] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
  - { id: C3, run: "GOOS=windows go vet ./internal/steward/ ./internal/store/ ./internal/config/ ./internal/runs/ ./internal/taskrunner/ ./internal/wsapi/ ./internal/profiles/ ./cmd/smind/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# S2 validation contract

Judge the branch head against these ACs, ADR-0047 sections 1, 3, 4 and 5, and `docs/plans/active/steward-seat.md` section S2.
Attack targets beyond the listed tests: (1) any path by which the seat, a Lead, a Peer, an automation or a plugin can write a verdict it does not own, reuse a token, read another review's digest, or
raise a verdict above a failing gate; (2) any RPC, MCP tool or run-scoped method that merges, approves, accepts or resolves; (3) the seat gaining Bash, a write tool, web access or a sub-agent, or a permission request of
the seat that is not decided by the seat floor (including under AutoAccept or with `steward.policy.enabled` false); (4) the token appearing in a prompt, digest, row, log, event or RPC result; (5) free text from a Lead, Peer or
PR reaching the seat's prompt outside the untrusted block; (6) a review published as ACCEPTED while a gate is failing, unknown or stale; (7) the daemon writing a verdict for a crashed, timed-out or cancelled seat other than BLOCKED;
(8) rails bypass (dedupe key, hourly or per-task cap, budget ceiling, auto-pause, steward.enabled false or invalid config); (9) a change to `tasks`, `migrate.go` or an existing wire field (additive only). Note M2b removes two layers on
purpose: the handler checks the seat task before the token is compared and the service checks the run. No real home, no live model call.
