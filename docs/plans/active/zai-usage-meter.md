# Z.AI GLM plan-usage meter (5-hour / weekly windows for a provider instance)

Size: L (8) — scope 3 (quota, wsapi, runs, web), PRs 1, risk 2 (new outbound call with a secret), decisions 1, verify 2.
Human item (Steward 4 "General rules, no epic"). Providers-epic PR3 shipped the `zai-glm` recipe
(`vendor: zai`, claude-code harness, `ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic`, key in the
provider secret store under env `ANTHROPIC_AUTH_TOKEN`). This item shows its plan windows like the
Anthropic/OpenAI accounts do (ADR-0028), and lets a used-up window steer like a cooldown.

## Endpoint (source, NOT live-verified)

`verifiedAt: inferred` — no live call was made (no Human key). Sources:

- ZCode `refs/zcode/docs/bigmodel-coding-plan-usage-quota.md` and `coding-plan-quota-remaining-api.md`
  (Z.AI's own workbench, 2026-05/06): `GET <host>/api/monitor/usage/quota/limit`, host `https://api.z.ai`
  (overseas) or `https://bigmodel.cn` / `https://open.bigmodel.cn`; header `authorization: <full API key>`
  with **no `Bearer` prefix** (the doc says Bearer fails).
- Paseo `refs/paseo/plugins/zai-usage-source/server/usage.ts` only reads `GET https://api.z.ai/api/biz/subscription/list`
  with `Authorization: Bearer` (plan name/status, **no windows**). We do not use it for windows; it is the
  fallback reference for a plan label only if the quota call lacks `level`.
- Response (ZCode doc): `{code:200, msg, success:true, data:{level:"lite|pro|max", limits:[...]}}`;
  `limits[]` entries `{type, unit, number, percentage (USED %), nextResetTime (epoch ms, optional)}`:
  - `TOKENS_LIMIT unit=3 number=5` → five_hour window; `TOKENS_LIMIT unit=6` (number 1) → weekly (may be absent);
  - `TIME_LIMIT unit=5 number=1` → monthly MCP/tool pool `{usage, currentValue, remaining, usageDetails[]}` →
    shown as a Balance (requests), never a routing window.
  Identify by `type+unit+number`, never by array index; a missing item is simply absent.
- Failure shapes assumed: 401/403 → rejected; `code != 200` / `success:false` with an HTML or unknown body →
  malformed (fixed vocabulary); "no coding plan" is `unavailable/no_quota`.

## Design

- `internal/quota/zai.go`: `zaiSource` (url, client injectable in tests only; production host allowlist
  `api.z.ai`, `open.bigmodel.cn`, `bigmodel.cn`, derived from the instance's `ANTHROPIC_BASE_URL`), parse into
  the existing `Report`/`Window` (ids `five_hour`, `weekly`; `Summary: true`; `ResetsAt` from `nextResetTime`;
  `DurationSeconds` 18000 / 604800; plan label = `level`).
- `internal/quota/providerusage.go`: `ProviderUsage`, a per-instance in-memory cache with the SAME budget rules
  as the account `Poller` (reuses `forcedGate`, `appendForced`, `backoffWait`, `retryAfterWait`, `getBody`
  constants; `touchTTLFloor` 5 min TTL, forced Refresh 30 s floor + 6/h, global in-flight cap 2, single-flight
  per instance, failure backoff + Retry-After, a failure never overwrites the last good report). In memory only:
  `quota_snapshots` has an FK to `accounts`, a persisted variant needs a migration → follow-up. A restart resets
  the ledger; acceptable because nothing fetches on its own.
- Triggers: `provider.usage {instanceIds?, mode: cached|touch|refresh}` (human action: Settings → Usage / Providers
  detail open or refresh press). **No background polling at all** (default off; there is no knob, so nothing can be
  misconfigured below 15 min). Run-scoped callers are downgraded refresh → touch, like `account.usage`.
  `accountUsage.enabled=false` disables it entirely (same switch as accounts).
- Key: read inside the daemon from the provider secret store (`GetProviderSecret(instanceID, secretName)` for the
  env `ANTHROPIC_AUTH_TOKEN`, fallback `ANTHROPIC_API_KEY`); never in a Report, event, log, error or the wire.
- Routing / cooldown (cache only): `runs.Registry` gets an optional `ProviderQuota` (`Exhausted(provider, now)
  (time.Time, bool)`, reads the cached report only, honours `accountUsage.routing` and
  `exhaustThresholdPct`). `activeProviderCooldown` and `ProviderCooldowns` return the LATER of the stored 429
  cooldown row and the exhausted window's `resetsAt` (capped by `store.MaxProviderCooldown`). One displayed
  reset: provider.list `cooldownUntil` and the usage row's window reset can differ only when the 429 reset
  (GLM wording, UTC+8, parsed by `internal/runs/ratelimit`) is later than the window reset, and then the
  cooldown wins for both blocking and display. The GLM 429 path itself is untouched.
- UI: web Settings → Usage lists provider-instance plans below the accounts with `UsageWindowBar`/`AccountUsageLines`;
  Providers detail shows the same lines plus the cooldown line. Mobile: follow-up (its usage screen is
  account-only; recorded below).

## Decision for the Human (not part of this item)

Switching the built-in `glm` default path to "Claude Code + Z.AI recipe" needs the Human's Z.AI key and a live
check of the endpoint above; **not done here**. Until a live call confirms the shape, windows are `inferred`.

## Acceptance criteria and tests

1. AC1 parse: `TestParseZai_Fixture` (recorded-shape fixture `testdata/zai_quota_limit.json`: five_hour 42 %, weekly 7 %,
   MCP balance), `TestParseZai_MissingWeekly`, `TestParseZai_NoResetTime`, `TestParseZai_MalformedAndBusinessError`,
   `TestParseZai_NoPlan`.
2. AC2 request: `TestZaiSource_Fetch_RawAuthHeaderNoBearer`, `TestZaiSource_Fetch_Rejected`, `TestZaiSource_Fetch_429RetryAfter`,
   `TestZaiSource_HostAllowlist`.
3. AC3 budget (count-asserting, fake server hit counter): `TestProviderUsage_TouchSingleFlight` (N concurrent = 1 request),
   `TestProviderUsage_TouchWithinTTL` (0 requests), `TestProviderUsage_PeekNeverFetches`, `TestProviderUsage_RefreshFloorAndBudget`,
   `TestProviderUsage_FailureBackoff`, `TestProviderUsage_RetryAfter429`, `TestProviderUsage_FailureKeepsLastGood`,
   `TestProviderUsage_ExhaustedCacheOnly`.
4. AC4 secret: `TestProviderUsageRPC_SentinelKeyNeverLeaks` (sentinel key absent from RPC result, events, log output, error text).
5. AC5 wire: `TestProviderUsageRPC_Modes`, `TestProviderUsageRPC_NoSecretIsUnavailable`,
   `TestProviderUsageRPC_DisabledMakesNoRequest`.
6. AC6 routing: `TestProviderCooldown_WindowExhaustedBlocks`, `TestProviderCooldown_UsesCachedWindowOnly` (0 requests),
   `TestProviderCooldown_LaterOfRowAndWindow`, `TestProviderCooldown_RoutingOffIgnoresWindow`, `TestProviderCooldown_GLM429StillWorks`.
7. AC7 web vitest + frames: usage lines in Settings → Usage and Providers detail, light+dark 1280/390.

## Decisions made while building

- `provider.usage` is human-only (refused for run-scoped callers), not downgraded like `account.usage`: it is an outbound call with a secret.
- No `provider.usage` event yet: the web re-reads after its own touch/refresh. `cooldownUntil` on a usage row is reserved and always null; the cooldown line comes from `provider.list` (merged: later of the 429 row and the cached window reset).
- `provider.clearCooldown` clears the 429 row only. A cached used-up window keeps blocking non-human sends until the next Refresh shows a reset (the daemon cannot know the Human reset the quota by hand). Follow-up if wanted: make clear also invalidate the cached window.
- State is in memory only (`quota_snapshots` has an FK to `accounts`); a restart resets the ledger. No background polling exists, so nothing fetches on its own.
- Mobile: the usage screen is account-only; follow-up.
- Settings -> Usage shows "No accounts with plan usage yet" above a Z.AI card when no Anthropic/OpenAI account exists (visible in frames); cosmetic follow-up.

## Progress

- [x] spec + endpoint notes + contract (id 9)
- [x] quota: zai.go (parse + fetch), providerusage.go (budget), zaiinstance.go (creds seam)
- [x] runs: cached-window cooldown (ProviderQuota)
- [x] wsapi: provider.usage; cmd/smind wiring + adapters
- [x] web: useProviderUsage, ProviderUsageList, ProviderPlanUsage in Settings -> Usage and Providers detail
- [x] frames light+dark 1280/390 on an isolated daemon with a fake Z.AI server (throwaway env seam, never committed)

## Validation

See the PR body: per-AC test names, mutations run (all failed their test), `task lint`, `task test`, `-race` on quota/runs/wsapi/cmd, frames under /tmp/zai-frames/out. The endpoint shape stays `inferred` until a live call with a real key.
