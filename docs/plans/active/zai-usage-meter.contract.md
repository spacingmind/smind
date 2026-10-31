---
v: 1
milestone: "Z.AI GLM plan-usage meter (provider.usage, cached-window cooldown, web)"
base: origin/develop
planRef: docs/plans/active/zai-usage-meter.md
acs:
  - id: AC1
    text: "The Z.AI quota/limit body parses into five_hour/weekly windows (used %, reset from nextResetTime) and a monthly tool balance; one odd entry never drops the rest; malformed bodies are errors."
    covers: { tests: [TestParseZai_Fixture, TestParseZai_MissingWeekly, TestParseZai_NoResetTime, TestParseZai_MalformedAndBusinessError, TestParseZai_NoPlan], frames: [] }
  - id: AC2
    text: "The request sends the raw key as Authorization (no Bearer), maps 401/403 to rejected and 429 to a rate-limited error with Retry-After, and only allowlisted Z.AI hosts are ever called."
    covers: { tests: [TestZaiSource_Fetch_RawAuthHeaderNoBearer, TestZaiSource_Fetch_Rejected, TestZaiSource_Fetch_429RetryAfter, TestZaiSource_Fetch_Malformed, TestZaiSource_HostAllowlist], frames: [] }
  - id: AC3
    text: "Call budget: N concurrent touches make one request, a touch inside the TTL none, peek none, forced refresh obeys the 30 s floor and 6/hour budget, failures back off and honour Retry-After and never replace the last good report."
    covers: { tests: [TestProviderUsage_TouchSingleFlight, TestProviderUsage_TouchWithinTTL, TestProviderUsage_PeekNeverFetches, TestProviderUsage_RefreshFloorAndBudget, TestProviderUsage_FailureBackoff, TestProviderUsage_RetryAfter429, TestProviderUsage_FailureKeepsLastGood, TestProviderUsage_ExhaustedCacheOnly, TestProviderUsage_DisabledMakesNoRequest, TestProviderUsage_CloseStopsFetching], frames: [] }
  - id: AC4
    text: "The API key never appears in a RPC result, a report, an error or the log."
    covers: { tests: [TestProviderUsageRPC_SentinelKeyNeverLeaks, TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary, TestZaiSource_Fetch_RawAuthHeaderNoBearer], frames: [] }
  - id: AC5
    text: "provider.usage is human-only, modes cached/touch/refresh, disabled makes no request, unknown instance/mode are errors, a missing key is unavailable/no_quota."
    covers: { tests: [TestProviderUsageRPC_Modes, TestProviderUsageRPC_RefusedForAgents, TestProviderUsageRPC_UnknownInstanceAndMode, TestProviderUsageRPC_DisabledMakesNoRequest, TestProviderUsageRPC_SentinelKeyNeverLeaks, TestProviderUsageRPC_SingleFlightCounts, TestZaiInstanceFetcher_Success, TestZaiInstanceFetcher_NoKey, TestZaiInstanceFetcher_Unsupported, TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary, TestZaiInstanceFetcher_Rejected401], frames: [] }
  - id: AC6
    text: "A used-up cached Z.AI window blocks like a cooldown (later of the 429 row and the window reset, capped), reads the cache only, and is off with accountUsage.routing=false."
    covers: { tests: [TestProviderCooldown_WindowExhaustedBlocks, TestProviderCooldown_LaterOfRowAndWindow, TestProviderCooldown_NoQuotaSetUnchanged, TestProviderCooldown_WindowPastIgnored, TestProviderCooldown_CappedAtMax, TestProviderCooldown_NonHumanSendFastFailsOnWindow, TestZaiInstances_EligibleOnlyZaiVendorCustom, TestZaiInstances_FromRecipeConfig, TestZaiInstances_CredsUseSecretStoreAndAllowlistedURL, TestZaiInstances_NoKey, TestProviderQuotaAdapter_CacheOnlyNoRequests, TestProviderQuotaAdapter_RoutingOffIgnoresWindow, TestProviderQuotaAdapter_ThresholdRespected, TestProviderQuotaAdapter_NonZaiProviderIgnored], frames: [] }
  - id: AC7
    text: "Web: Settings -> Usage and the Providers detail of a zai instance show the plan windows with the existing components; a non-zai instance asks nothing; touch is once per TTL; refresh only from the button."
    covers: { tests: [web-provider-usage], frames: [] }
tests:
  - { name: TestParseZai_Fixture, run: "go test ./internal/quota -run '^TestParseZai_Fixture$' -count=1", timeoutSec: 300 }
  - { name: TestParseZai_MissingWeekly, run: "go test ./internal/quota -run '^TestParseZai_MissingWeekly$' -count=1", timeoutSec: 300 }
  - { name: TestParseZai_NoResetTime, run: "go test ./internal/quota -run '^TestParseZai_NoResetTime$' -count=1", timeoutSec: 300 }
  - { name: TestParseZai_MalformedAndBusinessError, run: "go test ./internal/quota -run '^TestParseZai_MalformedAndBusinessError$' -count=1", timeoutSec: 300 }
  - { name: TestParseZai_NoPlan, run: "go test ./internal/quota -run '^TestParseZai_NoPlan$' -count=1", timeoutSec: 300 }
  - { name: TestZaiSource_Fetch_RawAuthHeaderNoBearer, run: "go test ./internal/quota -run '^TestZaiSource_Fetch_RawAuthHeaderNoBearer$' -count=1", timeoutSec: 300 }
  - { name: TestZaiSource_Fetch_Rejected, run: "go test ./internal/quota -run '^TestZaiSource_Fetch_Rejected$' -count=1", timeoutSec: 300 }
  - { name: TestZaiSource_Fetch_429RetryAfter, run: "go test ./internal/quota -run '^TestZaiSource_Fetch_429RetryAfter$' -count=1", timeoutSec: 300 }
  - { name: TestZaiSource_Fetch_Malformed, run: "go test ./internal/quota -run '^TestZaiSource_Fetch_Malformed$' -count=1", timeoutSec: 300 }
  - { name: TestZaiSource_HostAllowlist, run: "go test ./internal/quota -run '^TestZaiSource_HostAllowlist$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_TouchSingleFlight, run: "go test ./internal/quota -run '^TestProviderUsage_TouchSingleFlight$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_TouchWithinTTL, run: "go test ./internal/quota -run '^TestProviderUsage_TouchWithinTTL$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_PeekNeverFetches, run: "go test ./internal/quota -run '^TestProviderUsage_PeekNeverFetches$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_RefreshFloorAndBudget, run: "go test ./internal/quota -run '^TestProviderUsage_RefreshFloorAndBudget$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_FailureBackoff, run: "go test ./internal/quota -run '^TestProviderUsage_FailureBackoff$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_RetryAfter429, run: "go test ./internal/quota -run '^TestProviderUsage_RetryAfter429$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_FailureKeepsLastGood, run: "go test ./internal/quota -run '^TestProviderUsage_FailureKeepsLastGood$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_ExhaustedCacheOnly, run: "go test ./internal/quota -run '^TestProviderUsage_ExhaustedCacheOnly$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_DisabledMakesNoRequest, run: "go test ./internal/quota -run '^TestProviderUsage_DisabledMakesNoRequest$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsage_CloseStopsFetching, run: "go test ./internal/quota -run '^TestProviderUsage_CloseStopsFetching$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstanceFetcher_Success, run: "go test ./internal/quota -run '^TestZaiInstanceFetcher_Success$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstanceFetcher_NoKey, run: "go test ./internal/quota -run '^TestZaiInstanceFetcher_NoKey$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstanceFetcher_Unsupported, run: "go test ./internal/quota -run '^TestZaiInstanceFetcher_Unsupported$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary, run: "go test ./internal/quota -run '^TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstanceFetcher_Rejected401, run: "go test ./internal/quota -run '^TestZaiInstanceFetcher_Rejected401$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_Modes, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_Modes$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_RefusedForAgents, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_RefusedForAgents$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_UnknownInstanceAndMode, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_UnknownInstanceAndMode$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_DisabledMakesNoRequest, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_DisabledMakesNoRequest$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_SentinelKeyNeverLeaks, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_SentinelKeyNeverLeaks$' -count=1", timeoutSec: 300 }
  - { name: TestProviderUsageRPC_SingleFlightCounts, run: "go test ./internal/wsapi -run '^TestProviderUsageRPC_SingleFlightCounts$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_WindowExhaustedBlocks, run: "go test ./internal/runs -run '^TestProviderCooldown_WindowExhaustedBlocks$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_LaterOfRowAndWindow, run: "go test ./internal/runs -run '^TestProviderCooldown_LaterOfRowAndWindow$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_NoQuotaSetUnchanged, run: "go test ./internal/runs -run '^TestProviderCooldown_NoQuotaSetUnchanged$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_WindowPastIgnored, run: "go test ./internal/runs -run '^TestProviderCooldown_WindowPastIgnored$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_CappedAtMax, run: "go test ./internal/runs -run '^TestProviderCooldown_CappedAtMax$' -count=1", timeoutSec: 300 }
  - { name: TestProviderCooldown_NonHumanSendFastFailsOnWindow, run: "go test ./internal/runs -run '^TestProviderCooldown_NonHumanSendFastFailsOnWindow$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstances_EligibleOnlyZaiVendorCustom, run: "go test ./cmd/smind -run '^TestZaiInstances_EligibleOnlyZaiVendorCustom$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstances_FromRecipeConfig, run: "go test ./cmd/smind -run '^TestZaiInstances_FromRecipeConfig$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstances_CredsUseSecretStoreAndAllowlistedURL, run: "go test ./cmd/smind -run '^TestZaiInstances_CredsUseSecretStoreAndAllowlistedURL$' -count=1", timeoutSec: 300 }
  - { name: TestZaiInstances_NoKey, run: "go test ./cmd/smind -run '^TestZaiInstances_NoKey$' -count=1", timeoutSec: 300 }
  - { name: TestProviderQuotaAdapter_CacheOnlyNoRequests, run: "go test ./cmd/smind -run '^TestProviderQuotaAdapter_CacheOnlyNoRequests$' -count=1", timeoutSec: 300 }
  - { name: TestProviderQuotaAdapter_RoutingOffIgnoresWindow, run: "go test ./cmd/smind -run '^TestProviderQuotaAdapter_RoutingOffIgnoresWindow$' -count=1", timeoutSec: 300 }
  - { name: TestProviderQuotaAdapter_ThresholdRespected, run: "go test ./cmd/smind -run '^TestProviderQuotaAdapter_ThresholdRespected$' -count=1", timeoutSec: 300 }
  - { name: TestProviderQuotaAdapter_NonZaiProviderIgnored, run: "go test ./cmd/smind -run '^TestProviderQuotaAdapter_NonZaiProviderIgnored$' -count=1", timeoutSec: 300 }
  - { name: web-provider-usage, run: "cd web/packages/ui && bunx vitest run src/hooks/use-provider-usage.test.tsx src/components/account-usage/provider-usage-list.test.tsx src/components/account-usage/provider-plan-usage.test.tsx", timeoutSec: 600 }
mutations:
  - { id: M1, file: internal/quota/zai.go, change: "send Bearer prefix in Authorization", expectFail: [TestZaiSource_Fetch_RawAuthHeaderNoBearer] }
  - { id: M2, file: internal/quota/zai.go, change: "drop the host allowlist check", expectFail: [TestZaiSource_HostAllowlist] }
  - { id: M3, file: internal/quota/providerusage.go, change: "do not join an in-flight fetch", expectFail: [TestProviderUsage_TouchSingleFlight] }
  - { id: M4, file: internal/quota/providerusage.go, change: "ignore the TTL on touch", expectFail: [TestProviderUsage_TouchWithinTTL] }
  - { id: M5, file: internal/quota/providerusage.go, change: "skip the forced gate on refresh", expectFail: [TestProviderUsage_RefreshFloorAndBudget] }
  - { id: M6, file: internal/quota/providerusage.go, change: "overwrite the last good report on failure", expectFail: [TestProviderUsage_FailureKeepsLastGood] }
  - { id: M7, file: internal/quota/zaiinstance.go, change: "put the creds error text into the report", expectFail: [TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary] }
  - { id: M8, file: internal/wsapi/provider_usage.go, change: "drop the runScoped refusal", expectFail: [TestProviderUsageRPC_RefusedForAgents] }
  - { id: M9, file: internal/runs/cooldown.go, change: "take the window reset even when earlier than the 429 row", expectFail: [TestProviderCooldown_LaterOfRowAndWindow] }
  - { id: M10, file: cmd/smind/provider_usage.go, change: "ignore accountUsage.routing in the adapter", expectFail: [TestProviderQuotaAdapter_RoutingOffIgnoresWindow] }
  - { id: M11, file: web/packages/ui/src/components/providers/provider-detail.tsx, change: "drop the vendor===zai gate", expectFail: [web-provider-usage] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "go test ./... -skip '^TestServe_DaemonLogAndManagedBy$' -count=1", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 45 }
---

# Z.AI usage meter validation contract

No live call and no real key: every test uses an httptest server and a sentinel key. The endpoint shape is `inferred` (ZCode docs), see the plan.
Frames are captured by the Lead on an isolated daemon with a fake Z.AI server behind a throwaway, never-committed env seam (production has a host allowlist), so they are not validator frames; evidence under /tmp/zai-frames/out and in the PR body.
