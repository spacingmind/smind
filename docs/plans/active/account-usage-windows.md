# Account usage windows (plan quota) → routing and UI

Size: L (8) — scope 2, PRs 2, risk 2 (user tokens, outbound calls, refresh races), decisions 1, verify 1 (UI screenshots; one manual live-provider check)

Status: **spec only** (2026-10-11), docs-only PR. Design:
[ADR-0028](../../decisions/0028-account-usage-windows.md) (Proposed). **Do not
start PR-A until the user has answered D1–D4, D9, D10 in the ADR's "Open
decisions for Human"** (answers may change types and fetchers); the rest can
be answered before their PR. Where this plan says "default", it means the
ADR's recommended default.

Goal: smind learns each OAuth account's real plan windows (5-hour, weekly,
per-model weekly; reset times) from the provider, uses them so the proxy's
pool failover stops sending traffic to an exhausted account, and shows them
in the web UI, CLI and MCP. Today `serve.go` wires a `noopQuotaFetcher`, so
`routing.Router.isExhausted` never fires.

Not in scope: routing smind's own runs (ADR-0020), per-run token usage
(unchanged), pace prediction (`runsOutAt`), usage history, API-key balance
fetchers, providers other than Anthropic and OpenAI/ChatGPT OAuth,
least-pressure pool ordering, passive 429-based cooldown, per-account
opt-out, de-duplicating two smind accounts that hold the same login.

## Acceptance Criteria

Data model and storage (PR-A)

- **AC1** `quota.Report` / `Window` / `Balance` / `Problem` exist as in the ADR;
  `quota.Usage` and `noopQuotaFetcher` are gone; `quota.Fetcher.Fetch` takes
  an `accounts.Account` and returns a `Report`.
- **AC2** `quota_snapshots.usage_data` stores `Report` JSON with `"v":2`; no
  schema change and no SQL migration. A legacy payload (no `"v"`) or a
  too-new `v` decodes to "no report" without error and is overwritten by the
  next successful fetch.
- **AC3** The store has `GetLatestQuotaSnapshot(accountID)`; after an insert
  the poller keeps only the latest row for that account (older rows deleted
  in the same transaction). `Poller` no longer lists the whole table.
- **AC4** `quota.AccountKey` returns `<provider>-<id>`, matches
  `^[A-Za-z0-9._-]{1,128}$`, and does not change when the label, credential or
  token changes.

Fetchers (PR-A)

- **AC5** Anthropic OAuth accounts: a successful response yields windows
  `five_hour` and `weekly` (both `summary`), legacy `seven_day_<model>` and
  `limits[]` `weekly_scoped` entries as scoped windows (one window per scope,
  legacy and `limits[]` twins reconciled), and `OverageEnabled` from
  `extra_usage.is_enabled`.
- **AC6** OpenAI OAuth accounts: `primary_window` / `secondary_window` become
  windows named by `limit_window_seconds` (18000 → `five_hour`, 604800 →
  `weekly`, else `<n>s`, unknown duration → `unknown_primary` /
  `unknown_secondary`); `additional_rate_limits` and `code_review_rate_limit`
  become `feature`-scoped windows; `credits.balance` becomes a balance;
  `plan_type` becomes `PlanLabel`; the request carries `ChatGPT-Account-Id`
  from the access token's claim when present and omits it when not.
- **AC7** API-key accounts and unsupported providers return `unavailable /
  no_quota` and make **zero** HTTP requests.
- **AC8** Parsing is lenient: unknown fields ignored; numeric strings
  accepted; a malformed entry is skipped and its siblings survive; a body
  that is HTML, not JSON, or yields no windows is an `error` result
  (`"malformed response"`), never an empty `available`.
- **AC9** 401/403 → `unavailable / rejected` (no forced refresh, no retry);
  429 → transient error carrying `Retry-After`; 5xx, timeout, network error
  → transient error; an expired token is refreshed once via
  `Registry.EnsureFresh`, and a refresh failure → `unavailable / expired`.
- **AC10** `Registry.EnsureFresh` is serialized per account: two concurrent
  callers for the same expired account cause exactly one refresh call.
- **AC11** Fetches use only smind's stored credential, only the two fixed
  hosts (no redirects, 15 s timeout, 1 MiB cap), through the same uTLS
  clients the proxy uses; no config or account field can change the URL.

Poller (PR-A)

- **AC12** `Poller.Peek` does no I/O. `Poller.Get`/refresh serves the cache
  inside the TTL (default 5 min), de-duplicates concurrent fetches per
  account, and never returns an error for a fetch failure (it returns the
  last report marked `stale`, or "no report").
- **AC13** Backoff per account: 429 honors `Retry-After` (seconds and
  HTTP-date); 5xx/timeout/network use 1 → 30 min exponential with jitter;
  success resets it. A forced refresh bypasses the TTL but not an active
  backoff and not a 30 s per-account floor.
- **AC14** A failed fetch never overwrites a stored `available` report.
- **AC15** With `accountUsage.enabled: false` no outbound usage call is ever
  made, `Peek`/`Get` return "no report", and the daemon starts normally.
  `accountUsage.{enabled,ttlSeconds,routing,exhaustThresholdPct}` load with
  the ADR defaults and reject invalid values (`ttlSeconds < 30`,
  threshold outside 1–100).
- **AC16** No token, refresh token, JWT, header or response body appears in
  any log line, stored report, wsapi payload or event; the stored `Error`
  and `Problem.Detail` come from a fixed vocabulary.

Routing (PR-B)

- **AC17** `Router.Route` accepts `routing.WithModel(m)`; the proxy passes
  the model it already determined; with no model, only account-wide windows
  count.
- **AC18** Counted windows: account-wide windows always; `model`-scoped only
  when `AppliesTo(model)` is true; `surface`/`feature` never.
- **AC19** An account is exhausted iff a counted window has `UsedPct >=
  threshold` (or `RemainingPct <= 0`) and `now < ResetsAt`; a window whose
  `ResetsAt` has passed counts as reset without a refetch; a counted
  exhausted window with no `ResetsAt` counts only while the snapshot is
  `<= 2 × ttl` old; `OverageEnabled == true` suppresses exhaustion.
- **AC20** Unknown never exhausts: no snapshot, `error`, `unavailable`,
  legacy/too-new payload, `enabled:false`, `routing:false`. The router never
  returns a poller error, and `Route` makes no network call (a blocked
  fetcher does not delay it).
- **AC21** An exhausted verdict from the last good report stands through
  failing refetches until `ResetsAt`.
- **AC22** Pool: first non-exhausted candidate in order; all exhausted →
  error naming the earliest reset. Hard: exhausted → error naming the reset
  time, unknown → routes. Affinity: a pinned exhausted account is replaced;
  a pinned account under the threshold is kept regardless of other
  candidates' usage.
- **AC23** A stale snapshot triggers at most one asynchronous refresh per
  account at a time; the proxy request is not held up.

Surfaces (PR-C wsapi/CLI/MCP, PR-D web)

- **AC24** `account.usage` returns `{enabled, reports[]}` with the ADR's
  shape for all accounts or the given `accountIds`; an unknown id is an
  error; `refresh:true` triggers a fetch (subject to AC13) and waits up to
  20 s; for a run-scoped caller `refresh` is ignored.
- **AC25** `account.usage` events are emitted when a fetch changes the stored
  report and not when it does not; the topic is accepted by
  `events.subscribe`.
- **AC26** `smind account usage [--refresh] [--json]` prints per account one
  line per window with used % and time to reset, prints unavailable accounts'
  problems in words, and exits 0 when the daemon answered.
- **AC27** The MCP tool (D8: `account_usage`) is read-only, present in the
  catalog and in `mcp_instructions.go`, and takes no refresh for run-scoped
  callers. `usage_summary` and `usage.summary` are unchanged.
- **AC28** Web: each account row in Settings shows one bar per window
  (percent, tone, reset time with an absolute-time tooltip), balances,
  problem text for unavailable, a stale/backoff hint, a refresh button, and
  the polling switch with its one-sentence disclosure. A sidebar footer item
  shows pinned windows (default `summary` windows), a used/remaining
  toggle, and a popover with all windows; both are hidden when there is no
  data; pins/toggle persist in `localStorage`; live updates arrive through
  the event. Verified in light and dark, desktop and mobile width.
- **AC29** The web client treats a missing or reshaped field in a report as
  "not shown", never a crash.

Process

- **AC30** `task test` and `task lint` pass on every PR; the ADR is flipped
  to Accepted (with the user's D1–D13 answers recorded) in PR-A; the plan
  moves to `docs/plans/completed/` when every AC has a Validation line;
  `docs/ROADMAP.md` ("Quota prediction", quota `Fetcher` stub), `README.md`
  (config/CLI mention) and the stale comment in the orchestration-and-metering
  backlog are updated in the closing PR.

## Test Scenarios

Fixtures live in `internal/quota/testdata/` as redacted JSON/HTML files;
tests serve them from `httptest.Server` through an injected transport. **No
test calls a real endpoint.**

### Go table tests: Anthropic parser (`internal/quota/anthropic_test.go`)

| Name | Fixture / input | Expect |
|---|---|---|
| `anthropic_full` | `five_hour`, `seven_day`, `seven_day_opus`, `extra_usage.is_enabled:true` | 3 windows, ids `five_hour`/`weekly`/`weekly_model_opus`, summary on the first two, `OverageEnabled=true` |
| `anthropic_limits_array` | `limits[]` with `weekly_scoped` model + surface entries, no legacy keys | scoped windows with scope kind `model` / `surface`, ids unique and stable |
| `anthropic_legacy_and_limits_twin` | `seven_day_opus` and a `limits[]` Opus entry | exactly one Opus window; limits[] values win, nil fields fall back to legacy |
| `anthropic_null_utilization` | `five_hour:{utilization:null}` | window present with nil `UsedPct`, tone default |
| `anthropic_numeric_string` | `"utilization":"41.5"` | 41.5 |
| `anthropic_one_bad_entry` | one `limits[]` entry of the wrong type | that entry skipped, other windows kept |
| `anthropic_unknown_fields` | extra `email`, `organization`, `foo` keys | ignored; stored JSON contains none of them |
| `anthropic_empty_object` | `{}` | `error` "malformed response" (no windows) |
| `anthropic_truncated_json` | cut-off body | `error` "malformed response" |
| `anthropic_html` | `<html>…` 200 | `error` "malformed response", body not stored |
| `anthropic_401_403` | status 401 / 403 | `unavailable / rejected` with status; one request only (no retry) |
| `anthropic_429_retry_after_seconds` / `_http_date` / `_absent` | 429 | transient error, `RetryAfter` 120 s / parsed / 0 |
| `anthropic_5xx_timeout_network` | 503, slow server, closed conn | transient error each, no panic |
| `anthropic_redirect` | 302 to another host | not followed; `error` |
| `anthropic_oversize_body` | > 1 MiB | `error` |
| `anthropic_request_shape` | any 200 | `Authorization: Bearer <test token>`, `anthropic-beta: oauth-2025-04-20`, GET, fixed host/path, no query |

### Go table tests: OpenAI parser (`internal/quota/openai_test.go`)

| Name | Fixture / input | Expect |
|---|---|---|
| `openai_full` | primary 18000 s, secondary 604800 s, `plan_type`, credits | windows `five_hour`, `weekly` (both summary), `PlanLabel`, credits balance |
| `openai_unknown_duration` | `limit_window_seconds` null | `unknown_primary` / `unknown_secondary` |
| `openai_epoch_reset` | `reset_at` epoch seconds | `ResetsAt` correct UTC |
| `openai_additional_limits` | named and unnamed entries | named → `feature` scope id `limit:<escaped>`; unnamed skipped |
| `openai_code_review` | `code_review_rate_limit` | `feature` scope "Code review" |
| `openai_credits_string` | `"balance":"12.5"` | 12.5 |
| `openai_no_rate_limit` | only `plan_type` | `error` "malformed response" (no windows) |
| `openai_html` / `_401` / `_403` / `_429` / `_5xx` | as Anthropic | same mapping |
| `openai_account_id_header` | access token JWT with `chatgpt_account_id` | header sent; absent claim → header omitted; claim never appears in stored report or logs |
| `openai_email_dropped` | response with `email` | not in stored JSON |
| `openai_request_shape` | any 200 | bearer + UA + fixed host/path |

### Go tests: dispatch, refresh, account key (`internal/quota`, `internal/accounts`)

- `fetcher_apikey_no_call`: anthropic and openai `api_key` accounts, custom `base_url` → `no_quota`, transport saw 0 requests.
- `fetcher_unsupported_provider`: `kimi` oauth account → `no_quota` "provider not supported", 0 requests.
- `fetcher_expired_token_refreshes_once`: expired credential → refresher called once, new token used in the request, credential persisted.
- `fetcher_refresh_failure_expired`: refresher error → `unavailable / expired`, 0 usage requests.
- `fetcher_valid_token_no_refresh`: unexpired → refresher never called.
- `ensurefresh_concurrent_single_refresh`: 8 goroutines, one expired account → refresher called exactly once, all get the same fresh credential (race detector on).
- `ensurefresh_other_accounts_independent`: two accounts refresh in parallel (lock is per account).
- `accountkey_stable`: rename, `updateCredential`, token refresh → same key; regex holds for provider+id inputs; label containing an email never appears in the key.
- `report_roundtrip_and_versioning`: v2 round-trips; legacy `{"tokens_used":0,…}` → no report; `v:3` → no report.
- `tone_thresholds`: nil → default, 70 → warning, 90 → warning, 90.1 → danger, 69.9 → ok.
- `window_appliesto`: table of (scope, model) → bool: Opus vs `claude-opus-4-1` true, vs `claude-sonnet-4-5` false, surface/feature always false, nil scope always true, empty model + model scope false.

### Go tests: poller (`internal/quota/poller_test.go`)

- `poller_ttl_hit_no_fetch`; `poller_ttl_expired_refetches`.
- `poller_singleflight`: 10 concurrent `Get` → 1 fetch.
- `poller_peek_no_io`: `Peek` with a blocking fetcher returns immediately and starts nothing.
- `poller_error_keeps_last_good`: good then failing fetch → stored report unchanged, returned `stale`.
- `poller_backoff_429_retry_after`: 429 with `Retry-After: 120` → no fetch for 120 s (fake clock), then one.
- `poller_backoff_exponential_capped_jittered`: repeated 503 → 1, 2, 4 … capped at 30 min, within jitter bounds; success resets.
- `poller_force_refresh_respects_backoff_and_floor`: forced refresh during backoff → no fetch, cached returned with `nextRetryAt`; two forced refreshes within 30 s → one fetch.
- `poller_prunes_old_snapshots`: after N inserts one row per account remains.
- `poller_disabled_no_calls`: `enabled:false` → 0 requests across `Get`, `Peek`, forced refresh.
- `poller_rejected_cached_for_ttl`: 401 result not refetched inside the TTL.
- `poller_account_deleted`: snapshots gone, in-memory backoff dropped, no panic on a late fetch result.
- `poller_changed_notification`: callback fires on first report and on a window change, not on an identical refetch.
- `config_accountusage_defaults_and_validation` (`internal/config`).

### Go tests: routing (`internal/routing/router_test.go`), fake poller reports

- `route_pool_skips_account_exhausted_on_five_hour`.
- `route_pool_skips_account_exhausted_on_weekly`.
- `route_model_scoped_window_excludes_only_matching_model`: Opus window at 100% → `claude-opus-4-1` goes to the next account, `claude-sonnet-4-5` still uses it.
- `route_unknown_model_ignores_model_scoped_windows`.
- `route_surface_and_feature_windows_never_exhaust`.
- `route_threshold_configurable`: 95% at threshold 90 → exhausted; at 100 → not.
- `route_unknown_report_not_exhausted`: no snapshot / `error` / `unavailable` each → account used.
- `route_poller_error_never_fails_routing` (the old `check quota for account N` path is gone).
- `route_exhausted_stands_through_failed_refetch` until `ResetsAt`, then eligible again without a successful fetch (fake clock).
- `route_exhausted_without_resetsat_expires_after_2ttl`.
- `route_overage_enabled_not_exhausted`; `route_overage_nil_still_exhausted`.
- `route_all_exhausted_error_names_earliest_reset`.
- `route_hard_exhausted_errors_with_reset`; `route_hard_unknown_routes`.
- `route_affinity_exhausted_pinned_replaced`; `route_affinity_pinned_85pct_kept_over_idle_candidate`; `route_affinity_pinned_outside_candidates_ignored` (existing behavior kept).
- `route_does_not_block_on_slow_fetcher`: fetcher blocks 5 s, `Route` returns in < 100 ms and one async refresh is started.
- `route_routing_disabled_never_consults_windows` (`routing:false`) and `route_polling_disabled_same_as_today`.
- Proxy (`internal/server`): `proxy_passes_model_to_router`; `proxy_exhausted_pool_returns_503_with_reset_message`; existing model-aware and fail-closed tests stay green.

### Go tests: secrets (`internal/quota`, `internal/wsapi`)

- `no_secret_in_logs_report_event`: run every fetch outcome with a sentinel access token, refresh token and JWT; capture `log` output, stored `usage_data`, the `account.usage` RPC result and the event payload; none contains a sentinel, a response body fragment, or a provider-returned email.
- `error_vocabulary_is_fixed`: `Report.Error` and `Problem.Detail` are from the allowed set for every failure fixture (HTML body never echoed).
- `usage_account_json_keys_allowlist`: the wsapi `AccountUsage` JSON has only the documented keys.

### Go tests: wsapi, CLI, MCP

- wsapi (`internal/wsapi/account_usage_test.go`): `account_usage_all`, `account_usage_ids`, `account_usage_unknown_id_errors`, `account_usage_disabled_returns_enabled_false`, `account_usage_refresh_waits_and_returns`, `account_usage_refresh_timeout_returns_cached`, `account_usage_refresh_ignored_for_run_scoped_caller`, `account_usage_event_on_change_only`, `account_usage_topic_subscribable`, `account_usage_account_removed_event_flow` (usage state removed with the account).
- CLI (`cmd/smind`): `account_usage_text_output` (golden: window line, reset formatting "resets in 2h 4m", unavailable wording for expired/rejected/no_quota, stale hint), `account_usage_json`, `account_usage_refresh_flag`, `account_usage_daemon_down_exit_code`, `account_usage_no_accounts`.
- MCP: `account_usage_tool_in_catalog_and_instructions`, `account_usage_tool_readonly`, `account_usage_tool_no_refresh_input_for_run_scope`, `usage_summary_unchanged`.

### Web UI tests (vitest, `web/packages/ui`)

- `WindowBar`: percent text, tone class at 69/70/90/91, nil percent shows "—", reset text relative with absolute `title`, past `resetsAt` shows "reset", >24 h shows days.
- `AccountUsageRow`: available with scoped windows ordered account-wide first; `unavailable` expired / rejected / no_quota wording; `error` + stale hint with next-retry; balances line; zero windows; disabled shows the enable hint and disclosure; refresh button disabled while pending and during backoff.
- `account.usage` event updates the row without a refetch; a report with a missing/unknown field renders without throwing (AC29).
- Sidebar item: hidden with no data; default pins are `summary` windows; pin toggle and used/remaining toggle persist to `localStorage` and survive reload; popover lists all accounts; many accounts truncate without layout overflow.
- Settings: polling switch round-trips to the daemon config API/RPC chosen in PR-C (see Decisions).
- **Screenshot pass (required, UI task):** Settings account rows and the sidebar item + popover, each in light and dark, desktop and 390 px width, in states available / near-limit (danger) / unavailable / stale / disabled / empty. Attach to the PR.

### Manual (not in CI)

- One run against a real Anthropic OAuth account and a real ChatGPT OAuth account on the author's machine, comparing the windows with `claude /usage` and the Codex app, with a throwaway log capture checked for token leakage. Redacted real responses refresh the fixtures. Result recorded in Validation.

## Implementation split (non-stacked, each branches from `origin/develop`)

Order: **PR-A → (PR-B ∥ PR-C) → PR-D → close-out.** B and C may be worked in
parallel because their write scopes do not overlap, except one wiring line
each in `cmd/smind/serve.go`; whichever merges second rebases onto develop.
PR-D needs C's RPC merged. Points use the WORKSPACE_PROTOCOL axes.

| PR | Outcome | Write scope (one owner each) | Depends on | Size |
|---|---|---|---|---|
| **PR-A** types, store, fetchers, poller, config | AC1–AC16: `Report` model, Anthropic and OpenAI fetchers with fixtures, API-key/unsupported → no_quota, poller with TTL/singleflight/backoff/prune/`Peek`, per-account `EnsureFresh` lock, `accountUsage` config, serve.go wiring replacing the noop fetcher (display data now flows; routing still ignores it), ADR → Accepted with answers recorded | `internal/quota/**` (+ `testdata/`), `internal/store/quota_snapshots.go` (+ test), `internal/accounts/refresh.go` (lock only), `internal/config/config.go`, `cmd/smind/serve.go`, the ADR and this plan's Decisions/Progress | D1–D4, D9, D10 answered | M (5): scope 1, PRs 1, risk 2, decisions 1, verify 0 |
| **PR-B** routing integration | AC17–AC23: `WithModel`, windows → exhausted rules, async refresh trigger, error text with reset, `isExhausted` rewrite, proxy passes model | `internal/routing/**`, `internal/server/proxy.go` (the `Route` call and its error text only), one line in `serve.go` | PR-A merged; D6, D7, D12 answered | M (4): scope 1, PRs 1, risk 2, decisions 0, verify 0 |
| **PR-C** wsapi, CLI, MCP | AC24–AC27: `account.usage` RPC + event + topic, run-scoped refresh denial, `smind account usage`, MCP tool, instructions, catalog tests, the config read/switch path the UI uses | `internal/wsapi/{account_usage.go,events.go,handlers.go}` (+ tests), `cmd/smind/{account.go,mcp_*.go}` (+ tests), one line in `serve.go` | PR-A merged; D8 answered | M (3): scope 1, PRs 1, risk 1, decisions 0, verify 0 |
| **PR-D** web UI | AC28–AC29: Settings account rows, sidebar item + popover, pins, events, light/dark screenshots | `web/packages/ui/src/{components/settings/providers-section.tsx, components/account-usage/**, components/app-sidebar.tsx (footer slot only), lib/account-usage.ts, hooks/use-account-usage.ts}` (+ tests) | PR-C merged; D5, D11 answered | M (4): scope 1, PRs 1, risk 0, decisions 1, verify 1 |
| **Close-out** docs | AC30: ROADMAP/README/stale backlog text, plan → `completed/` with Validation | `docs/**`, `README.md` | all | S |

Notes for whoever implements:

- Mobile (Expo) reuses the wsapi RPC later; out of this plan.
- If D6 is "display first", PR-B is held one release; nothing else changes.
- PR-A must keep `go test -race ./internal/quota/... ./internal/accounts/...`
  green: the lock and singleflight tests are race tests.
- Do not add history, pace prediction or a background timer to a PR without
  amending the ADR first (AGENTS.md rule d).

## Decisions

- Design and rationale: [ADR-0028](../../decisions/0028-account-usage-windows.md)
  (Proposed). It records D1–D13 with recommended defaults. When the user
  answers, copy the answers into the ADR's Status and into the list below.
- **Answers (fill in):** D1 __ · D2 __ · D3 __ · D4 __ · D5 __ · D6 __ ·
  D7 __ · D8 __ · D9 __ · D10 __ · D11 __ · D12 __ · D13 __
- Decided here, not in the ADR (small, reversible):
  1. Naming: "Plan usage" (windows, `account.usage`) vs "Token usage"
     (`usage.summary`, `run_usage`) in every surface.
  2. Fixtures are redacted copies of real responses; the only live check is
     the manual one above.
  3. The wsapi/web switch for `accountUsage.enabled` reuses whatever
     mechanism the daemon has for reading and writing `config.yaml` at the
     time of PR-C. If none exists, PR-C ships read-only state in
     `account.usage` (`enabled`) and the config file is the switch; a
     write RPC would be a new decision.
  4. Per-run and proxy token usage are untouched; `usage.summary` gains no
     window concepts.
- Superseded: the `orchestration-and-metering.md` Step 4 backlog item
  "feeding the router's `TokensUsed` from locally counted tokens" (ADR-0028
  uses provider-reported windows instead, for the reasons in its
  Alternatives).

## Progress

- 2026-10-11: ADR-0028 (Proposed) and this plan written, docs only, on
  `docs/account-usage-windows`. Source reading done: `internal/quota`,
  `internal/routing/router.go`, `internal/accounts/{refresh,credential}.go`,
  `internal/server/proxy.go`, `cmd/smind/serve.go`, wsapi account/usage
  handlers, Paseo's usage plugin, server registry, app and CLI.
- [ ] D1–D13 answered by the user
- [ ] PR-A types, store, fetchers, poller, config
- [ ] PR-B routing integration
- [ ] PR-C wsapi, CLI, MCP
- [ ] PR-D web UI
- [ ] Manual live check against one real account per provider
- [ ] Close-out docs; plan moved to `completed/`

## Validation

Docs-only PR, nothing built. Checked for this PR: the ADR number 0028 is
free on `origin/develop` (last is 0027) and in open PRs; `git diff
--stat` shows only the three docs files; no code, config, or generated file
changed.

Mapping for the implementation PRs (fill in as each lands):

| AC | How it will be confirmed |
|---|---|
| AC1–AC4 | `internal/quota` report/versioning/accountkey/prune tests (`report_roundtrip_and_versioning`, `accountkey_stable`, `poller_prunes_old_snapshots`) |
| AC5–AC11 | Anthropic/OpenAI/dispatch/refresh table tests with fixtures, `ensurefresh_*` under `-race` |
| AC12–AC16 | `poller_*` tests with a fake clock, config tests, `no_secret_in_logs_report_event` |
| AC17–AC23 | `route_*` tests, proxy tests |
| AC24–AC27 | wsapi, CLI golden, MCP catalog tests |
| AC28–AC29 | vitest component tests + the light/dark/mobile screenshot set attached to the PR-D |
| AC30 | `task test`, `task lint`, ADR/plan/ROADMAP diffs |
| Real endpoints | the manual live check above (not automated; fixtures can drift from real responses) |

Unverified at spec time (the implementing PRs must confirm and record):

- That the Anthropic `/api/oauth/usage` endpoint accepts smind's stored
  OAuth token. smind's login requests the `user:profile` scope
  (`internal/accounts/refresh_providers.go`), which Paseo's reading of the
  endpoint suggests is needed, but the endpoint was not called.
- That an OpenAI token stored by smind's login carries the
  `chatgpt_account_id` claim the `ChatGPT-Account-Id` header needs, and that
  `chatgpt.com` accepts Go's uTLS Chrome fingerprint for this path (Paseo
  uses a plain `fetch` with a browser `User-Agent`).
- Whether Anthropic `extra_usage.is_enabled == true` really means requests
  continue past a full window (D9).
- Whether a plan label is available for Anthropic accounts without an extra
  call (`/api/oauth/profile`); v1 leaves it empty if not.
- How run-scoped callers are identified at the wsapi layer for the
  `refresh` denial (ADR-0027 describes the same need; PR-C must reuse the
  existing mechanism rather than invent one).
