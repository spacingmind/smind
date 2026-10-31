---
v: 1
milestone: "Run failure classes PR1: classifier, wire action, migration, retry/cooldown policy"
base: origin/develop
planRef: docs/plans/active/run-failure-classes.md
acs:
  - id: AC1-6
    text: "failureKind takes auth_expired, plan_window_exhausted, model_not_in_plan, transient_429, cli_exited and the residual rate_limited; precedence is plan window/signal > typed backend code > auth text > model text > generic 429 words (transient only with a Retry-After or an overload body, else residual)."
    covers: { tests: [TestClassify_PlanWindowBeatsAuthAndModelText, TestClassify_BareRateLimitIsResidualNotTransient, TestClassify_TransientNeedsRetryAfterOrOverloadBody, TestClassify_ModelErrorMentioningLimitStaysModel, TestClassify_AuthErrorMentioning429StaysAuth, TestClassify_AuthBeatsModelText, TestClassify_StructuredCodeBeatsText, TestClassify_Fixtures], frames: [] }
  - id: AC7-8
    text: "Run status/summary carry a derived FailureAction {kind,target,label}; it is never stored; wire layers copy it additively (omitempty)."
    covers: { tests: [TestNextAction_PerClass], frames: [] }
  - id: AC9
    text: "A rate_limited row with a window or a reset time becomes plan_window_exhausted by migration (idempotent); one with neither stays rate_limited."
    covers: { tests: [TestMigration_RateLimitedWithWindowBecomesPlanWindow], frames: [] }
  - id: AC12
    text: "auth_expired and model_not_in_plan set no provider cooldown, queue no wait retry and start no fallback successor, under both onRateLimit policies."
    covers: { tests: [TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait, TestFinish_ModelNotInPlanNeverRetriesSameAccount], frames: [] }
  - id: AC13
    text: "transient_429 sets no cooldown; profile/orchestrator runs retry on the same chat at 30 s then 2 min (Retry-After wins, clamped 5 s..5 min), at most twice; a plain human send is not retried; a retry never runs while the provider is in cooldown (not queued, and dropped at delivery); retries are real counted runs."
    covers: { tests: [TestFinish_Transient429RetriesTwiceThenSurfaces, TestFinish_Transient429PlainHumanSendIsNotRetried, TestFinish_Transient429RetryHeldWhileProviderCooldown, TestFinish_Transient429RetryRunsAreCounted], frames: [] }
  - id: AC14
    text: "plan_window_exhausted behaves as rate_limited did: cooldown on a future provider-wide reset and the wait/fallback profile policy."
    covers: { tests: [TestFinish_PlanWindowStillSetsCooldownAndWait], frames: [] }
tests:
  - { name: TestClassify_PlanWindowBeatsAuthAndModelText, run: "go test ./internal/runs/failure -run '^TestClassify_PlanWindowBeatsAuthAndModelText$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_BareRateLimitIsResidualNotTransient, run: "go test ./internal/runs/failure -run '^TestClassify_BareRateLimitIsResidualNotTransient$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_TransientNeedsRetryAfterOrOverloadBody, run: "go test ./internal/runs/failure -run '^TestClassify_TransientNeedsRetryAfterOrOverloadBody$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_ModelErrorMentioningLimitStaysModel, run: "go test ./internal/runs/failure -run '^TestClassify_ModelErrorMentioningLimitStaysModel$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_AuthErrorMentioning429StaysAuth, run: "go test ./internal/runs/failure -run '^TestClassify_AuthErrorMentioning429StaysAuth$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_AuthBeatsModelText, run: "go test ./internal/runs/failure -run '^TestClassify_AuthBeatsModelText$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_StructuredCodeBeatsText, run: "go test ./internal/runs/failure -run '^TestClassify_StructuredCodeBeatsText$' -count=1", timeoutSec: 900 }
  - { name: TestClassify_Fixtures, run: "go test ./internal/runs/failure -run '^TestClassify_Fixtures$' -count=1", timeoutSec: 900 }
  - { name: TestNextAction_PerClass, run: "go test ./internal/runs/failure -run '^TestNextAction_PerClass$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait, run: "go test ./internal/runs -run '^TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_ModelNotInPlanNeverRetriesSameAccount, run: "go test ./internal/runs -run '^TestFinish_ModelNotInPlanNeverRetriesSameAccount$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_Transient429RetriesTwiceThenSurfaces, run: "go test ./internal/runs -run '^TestFinish_Transient429RetriesTwiceThenSurfaces$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_Transient429PlainHumanSendIsNotRetried, run: "go test ./internal/runs -run '^TestFinish_Transient429PlainHumanSendIsNotRetried$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_Transient429RetryHeldWhileProviderCooldown, run: "go test ./internal/runs -run '^TestFinish_Transient429RetryHeldWhileProviderCooldown$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_Transient429RetryRunsAreCounted, run: "go test ./internal/runs -run '^TestFinish_Transient429RetryRunsAreCounted$' -count=1", timeoutSec: 900 }
  - { name: TestFinish_PlanWindowStillSetsCooldownAndWait, run: "go test ./internal/runs -run '^TestFinish_PlanWindowStillSetsCooldownAndWait$' -count=1", timeoutSec: 900 }
  - { name: TestMigration_RateLimitedWithWindowBecomesPlanWindow, run: "go test ./internal/store -run '^TestMigration_RateLimitedWithWindowBecomesPlanWindow$' -count=1", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/runs/failure/classify.go, change: "a generic 429 with no Retry-After classifies transient_429", expectFail: [TestClassify_BareRateLimitIsResidualNotTransient] }
  - { id: M2, file: internal/runs/failure/classify.go, change: "model text is checked before auth text", expectFail: [TestClassify_AuthBeatsModelText] }
  - { id: M3, file: internal/runs/failure/classify.go, change: "the plan-window early return is removed", expectFail: [TestClassify_PlanWindowBeatsAuthAndModelText] }
  - { id: M4, file: internal/runs/failover.go, change: "a human-fix kind reaches the plan-limit wait/fallback branch", expectFail: [TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait, TestFinish_ModelNotInPlanNeverRetriesSameAccount] }
  - { id: M5, file: internal/runs/failover.go, change: "the transient retry cap allows a third retry", expectFail: [TestFinish_Transient429RetriesTwiceThenSurfaces] }
  - { id: M6, file: internal/runs/failover.go, change: "the enqueue-time cooldown check is disabled", expectFail: [TestFinish_Transient429RetryHeldWhileProviderCooldown] }
  - { id: M7, file: internal/runs/queue.go, change: "the delivery-time cooldown check for a transient retry is disabled", expectFail: [TestFinish_Transient429RetryHeldWhileProviderCooldown] }
  - { id: M8, file: internal/runs/failover.go, change: "every run is eligible for a transient retry", expectFail: [TestFinish_Transient429PlainHumanSendIsNotRetried] }
  - { id: M9, file: internal/store/migrate.go, change: "the migration drops its window/reset condition", expectFail: [TestMigration_RateLimitedWithWindowBecomesPlanWindow] }
commands:
  - { id: C1, run: "go vet ./...", timeoutSec: 900 }
  - { id: C2, run: "go test ./internal/runs/... ./internal/store ./internal/wsapi ./internal/tasktree ./internal/fleet/... ./internal/plugins ./internal/codex ./internal/stall ./cmd/smind", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# Run failure classes PR1 validation contract

Go only (the web change is a one-helper compat shim, covered by `src/lib/rate-limit.test.ts`). No live provider calls: every payload is a fixture or a fake error string. Frames: none (UI is PR3).
