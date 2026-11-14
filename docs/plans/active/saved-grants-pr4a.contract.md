---
v: 1
milestone: "Saved grants PR4a: the daemon (grantable classification, store, evaluation, scoped answer, list/revoke, CLI)"
base: origin/develop
planRef: docs/plans/active/saved-grants.md
acs:
  - id: AC1
    text: "A grant exists only after a human answered a pending, offered request with scope project and its allow_once option; an agent run cannot create, list or revoke one; the three names are on the human-only table."
    covers: { tests: [TestGrants_OnlyHumanScopeProject, TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest, TestWsapi_GrantMethodsAreHumanOnly, TestWsapi_GrantMethodsRefusedForRunScoped, TestMCP_NoToolSavesOrRevokesGrants], mutations: [M6, M7] }
  - id: AC2
    text: "A grant applies to the same project and the exact raw command only."
    covers: { tests: [TestGrants_MatchIsExactCommandAndProject], mutations: [M2] }
  - id: AC3
    text: "Floor hits, rule denies, non-plain asks and never-grant programs never get the offer and never match a stored grant; the engine decides first."
    covers: { tests: [TestGrants_EvaluatedAfterFloorAndPolicy, TestGrants_PolicyDenyBeatsGrant, TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion, TestGrantable_PlainAskOnly, TestGrantable_NeverGrantPrograms, TestGrantable_EquivalentToDecideAsk, TestGrants_NotUnderAutoAcceptSeatSystemRemote, TestGrants_EngineOffAsksEveryTime], mutations: [M1, M5] }
  - id: AC4
    text: "An expired, revoked or void grant, or a one-byte different command, asks again with the same card."
    covers: { tests: [TestGrants_ExpireAndRevoke, TestGrants_VoidOnMcpChange, TestGrants_AskedAgainAfterEveryInvalidation], mutations: [M3, M4] }
  - id: AC5
    text: "Every creation, use, revoke and skip is audited; a use is a permission_resolved with resolution grant."
    covers: { tests: [TestGrants_UseIsAudited, TestGrants_AuditRows], mutations: [M8] }
  - id: AC6
    text: "Strict classes (Human decision 2026-11-15): a command is read-only, runner or never, decided by the daemon from compiled lists; a runner grant needs the explicit per-grant opt-in at creation and at use; the class is re-checked at use; the caller cannot assert the class."
    covers: { tests: [TestGrantable_AllowListIsPinned, TestGrantable_ClassesArePinned, TestGrantable_NearMissesStayUngrantable, TestGrants_ReadOnlyGrantNeedsNoOptIn, TestGrants_RunnerGrantNeedsOptIn, TestGrants_ClassAndOptInRecheckedAtUse, TestGrants_ClassIsNotTakenFromTheRequest, TestGrants_RunnerNeedsOptInInStore], mutations: [M9, M10, M11, M12, M13] }
tests:
  - { name: TestGrantable_AllowListIsPinned, run: "go test ./internal/permpolicy -run '^TestGrantable_AllowListIsPinned$' -count=1", timeoutSec: 300 }
  - { name: TestGrantable_ClassesArePinned, run: "go test ./internal/permpolicy -run '^TestGrantable_ClassesArePinned$' -count=1", timeoutSec: 300 }
  - { name: TestGrantable_NearMissesStayUngrantable, run: "go test ./internal/permpolicy -run '^TestGrantable_NearMissesStayUngrantable$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_ReadOnlyGrantNeedsNoOptIn, run: "go test ./internal/runs -run '^TestGrants_ReadOnlyGrantNeedsNoOptIn$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_RunnerGrantNeedsOptIn, run: "go test ./internal/runs -run '^TestGrants_RunnerGrantNeedsOptIn$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_ClassAndOptInRecheckedAtUse, run: "go test ./internal/runs -run '^TestGrants_ClassAndOptInRecheckedAtUse$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_ClassIsNotTakenFromTheRequest, run: "go test ./internal/runs -run '^TestGrants_ClassIsNotTakenFromTheRequest$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_RunnerNeedsOptInInStore, run: "go test ./internal/store -run '^TestGrants_RunnerNeedsOptInInStore$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_OnlyHumanScopeProject, run: "go test ./internal/runs -run '^TestGrants_OnlyHumanScopeProject$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest, run: "go test ./internal/runs -run '^TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_MatchIsExactCommandAndProject, run: "go test ./internal/runs -run '^TestGrants_MatchIsExactCommandAndProject$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_EvaluatedAfterFloorAndPolicy, run: "go test ./internal/runs -run '^TestGrants_EvaluatedAfterFloorAndPolicy$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_PolicyDenyBeatsGrant, run: "go test ./internal/runs -run '^TestGrants_PolicyDenyBeatsGrant$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion, run: "go test ./internal/runs -run '^TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_NotUnderAutoAcceptSeatSystemRemote, run: "go test ./internal/runs -run '^TestGrants_NotUnderAutoAcceptSeatSystemRemote$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_EngineOffAsksEveryTime, run: "go test ./internal/runs -run '^TestGrants_EngineOffAsksEveryTime$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_ExpireAndRevoke, run: "go test ./internal/runs -run '^TestGrants_ExpireAndRevoke$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_VoidOnMcpChange, run: "go test ./internal/runs -run '^TestGrants_VoidOnMcpChange$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_AskedAgainAfterEveryInvalidation, run: "go test ./internal/runs -run '^TestGrants_AskedAgainAfterEveryInvalidation$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_UseIsAudited, run: "go test ./internal/runs -run '^TestGrants_UseIsAudited$' -count=1", timeoutSec: 300 }
  - { name: TestGrantable_PlainAskOnly, run: "go test ./internal/permpolicy -run '^TestGrantable_PlainAskOnly$' -count=1", timeoutSec: 300 }
  - { name: TestGrantable_NeverGrantPrograms, run: "go test ./internal/permpolicy -run '^TestGrantable_NeverGrantPrograms$' -count=1", timeoutSec: 300 }
  - { name: TestGrantable_EquivalentToDecideAsk, run: "go test ./internal/permpolicy -run '^TestGrantable_EquivalentToDecideAsk$' -count=1", timeoutSec: 300 }
  - { name: TestGrants_AuditRows, run: "go test ./internal/store -run '^TestGrants_AuditRows$' -count=1", timeoutSec: 300 }
  - { name: TestWsapi_GrantMethodsAreHumanOnly, run: "go test ./internal/wsapi -run '^TestWsapi_GrantMethodsAreHumanOnly$' -count=1", timeoutSec: 300 }
  - { name: TestWsapi_GrantMethodsRefusedForRunScoped, run: "go test ./internal/wsapi -run '^TestWsapi_GrantMethodsRefusedForRunScoped$' -count=1", timeoutSec: 300 }
  - { name: TestMCP_NoToolSavesOrRevokesGrants, run: "go test ./cmd/smind -run '^TestMCP_NoToolSavesOrRevokesGrants$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M9, file: "internal/permpolicy/grantable.go", change: "add push to the git verbs", expectFail: [TestGrantable_AllowListIsPinned, TestGrantable_ClassesArePinned, TestGrantable_NearMissesStayUngrantable] }
  - { id: M10, file: "internal/runs/grants.go", change: "drop the opt-in check at use", expectFail: [TestGrants_ClassAndOptInRecheckedAtUse] }
  - { id: M11, file: "internal/runs/grants.go", change: "drop the opt-in check at creation (RespondPermissionScoped)", expectFail: [TestGrants_RunnerGrantNeedsOptIn] }
  - { id: M12, file: "internal/runs/grants.go", change: "take the class from the request detail instead of the engine", expectFail: [TestGrants_ClassIsNotTakenFromTheRequest] }
  - { id: M13, file: "internal/permpolicy/grantable.go", change: "move make into the read-only class", expectFail: [TestGrantable_AllowListIsPinned, TestGrantable_ClassesArePinned] }
  - { id: M1, file: "internal/runs/decider_policy.go", change: "applyPolicy reports grantable for every ask (drop dec.Grantable)", expectFail: [TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion] }
  - { id: M2, file: "internal/runs/grants.go", change: "look the grant up by command only (workspace id 0)", expectFail: [TestGrants_MatchIsExactCommandAndProject] }
  - { id: M3, file: "internal/runs/grants.go", change: "drop the expiry check", expectFail: [TestGrants_ExpireAndRevoke, TestGrants_AskedAgainAfterEveryInvalidation] }
  - { id: M4, file: "internal/runs/grants.go", change: "drop the MCP fingerprint comparison", expectFail: [TestGrants_VoidOnMcpChange, TestGrants_AskedAgainAfterEveryInvalidation] }
  - { id: M5, file: "internal/permpolicy/grantable.go", change: "add curl to grantAllow", expectFail: [TestGrantable_NeverGrantPrograms] }
  - { id: M6, file: "internal/runs/grants.go", change: "RespondPermissionScoped skips the offer check (pp.grant == nil)", expectFail: [TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest, TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion] }
  - { id: M7, file: "internal/permpolicy/humanonly.go", change: "drop permission.grants.revoke from HumanOnlyRPCs", expectFail: [TestWsapi_GrantMethodsAreHumanOnly] }
  - { id: M8, file: "internal/runs/grants.go", change: "do not record the use", expectFail: [TestGrants_UseIsAudited] }
---

# Contract: saved grants PR4a

The surfaces (card control, web and mobile lists) are PR4b and not judged here.
