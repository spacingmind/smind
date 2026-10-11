# Account usage windows (plan quota) → routing and UI

Size: L (8) — scope 2, PRs 2, risk 2 (user tokens, outbound calls, refresh races), decisions 0 (all answered), verify 2 (UI screenshots; manual live-provider check)

Status: **spec, ADR accepted** (2026-10-11), docs-only so far. Design:
[ADR-0028](../../decisions/0028-account-usage-windows.md) (**Accepted**).
D1–D4 and D6–D13 were accepted as the recommended defaults; **D5 changed**:
the web UI ports Paseo's Usage UX (PR-D1/D2 below). PR-A may start, but its
live-endpoint probes need Human go-ahead first (see Validation); until then
it is fixtures-only.

Goal: smind learns each OAuth account's real plan windows (5-hour, weekly,
per-model weekly; reset times) from the provider, uses them so the proxy's
pool failover stops sending traffic to an exhausted account, and shows them
in the web UI, CLI and MCP. Today `serve.go` wires a `noopQuotaFetcher`, so
`routing.Router.isExhausted` never fires.

Not in scope: routing smind's own runs (ADR-0020), per-run token usage
(unchanged), pace prediction (`runsOutAt`; the UI port omits Paseo's
"at risk / runs out" row), usage history, API-key balance
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

Surfaces: wsapi, CLI, MCP (PR-C)

- **AC24** `account.usage` returns `{enabled, reports[]}` with the ADR's
  shape for all accounts or the given `accountIds`; an unknown id is an
  error; `refresh:true` triggers a fetch (subject to AC13) and waits up to
  20 s; for a run-scoped caller `refresh` is ignored.
- **AC25** `account.usage` events are emitted after every completed network
  fetch (success, `unavailable` or error), so `fetchedAt` advances even when
  the numbers did not change, and not for a cache hit; a refresh without
  `accountIds` fetches all eligible accounts in parallel, one event each as
  it settles; the topic is accepted by `events.subscribe`.
- **AC26** `smind account usage [--refresh] [--json]` prints per account one
  line per window with used % and time to reset, prints unavailable accounts'
  problems in words, and exits 0 when the daemon answered.
- **AC27** The MCP tool (D8: `account_usage`) is read-only, present in the
  catalog and in `mcp_instructions.go`, and takes no refresh for run-scoped
  callers. `usage_summary` and `usage.summary` are unchanged.
Web: Paseo's Usage UX ported (PR-D1 core, PR-D2 sidebar and pins, PR-E optional)

- **AC28** (D2) Sidebar footer Usage item: in the existing footer, each
  account's provider logo followed by its default windows as "31% 5h",
  "54% wk" (windows flagged `summary`; the first window with a percent when
  none is flagged); unavailable and error accounts are left out; with no
  summary data the row reads plain "Usage"; the row is hidden while
  `accountUsage.enabled` is false or the daemon lacks `account.usage`; the
  icon-collapsed rail shows the icon only.
- **AC29** (D2) The footer line shows the richest layout that fits one line
  (meters + percent + short label, else percent + short label, else percent
  alone), shows labels until widths are measured, never overflows or wraps,
  and a window with an empty short label shows its percent alone.
- **AC30** (D2) Pins: while "Summary in sidebar" is on, every window row is
  a checkbox that pins it (whole row clickable, pin glyph with tooltip,
  half-strength hover highlight, accessible name `Pin <Provider> <Window>,
  <value> · <reset>` with the checked state); the first toggle snapshots the
  displayed defaults and then toggles one window; pins render in report
  order grouped by provider; a pin matches the window on every account of
  that provider; an explicit empty selection persists across reload and does
  not fall back to defaults; with the switch off rows are plain bars.
- **AC31** (D2) "Percentages" Used / Remaining switches the footer, cards and
  Settings together ("69% left"), without touching pins, and persists.
- **AC32** (D1) Usage opens as a modal over the current screen: a dialog on
  wide layouts, a bottom sheet (at most 80% of the height, backdrop visible)
  on narrow ones; opening and closing do not change the route or the open
  task, tabs or scroll underneath; the modal mounts on first open and
  unmounts after it closes; a long list scrolls to its last window at
  1440×900 and 390×844.
- **AC33** (D1) Account filter in the modal title row: hidden with one or
  zero accounts; "All accounts" by default, otherwise one account; the pick
  persists in `localStorage`, is the account the footer item shows and the
  modal opens on after a reload, and falls back to All when that account no
  longer exists.
- **AC34** (D1) Cards: provider logo and name, plan badge, a status dot with
  "Unavailable" / "Error" when not available, one bar per window (label,
  percent used or "N% left", reset countdown "resets 2h" / "resetting now",
  a meter coloured ok / warning / danger at the 70 / 90 thresholds, muted
  when the tone is default), balances ("$x / $y" or "x left"), detail rows,
  the account label in the footer, and the report's freshness ("Updated 3m
  ago") in the refresh button's tooltip, and printed on the card on narrow
  or touch layouts.
- **AC35** (D1) Problem states are explained in words: expired ("Login
  expired 2h ago. Reconnect it in Providers."), rejected ("Login rejected
  (HTTP 401). Reconnect it in Providers."), no_quota (the report's detail),
  error (fixed-vocabulary text plus "retry in 3 min" while backing off),
  disabled (names `accountUsage.enabled`, nothing to refresh); a failed
  refresh keeps the previous report on screen and adds "Unable to refresh
  usage"; an account that recovers replaces its message with windows.
- **AC36** (D1) Refresh: a card's button sends `account.usage {accountIds:[id],
  refresh:true}` for that account only; Refresh all sends one refresh for
  every account; both show a spinner and are disabled while pending; other
  cards are untouched; a report with a later `fetchedAt` is never replaced
  by an earlier one when a refresh races a list or an event; plain list
  loads never force.
- **AC37** (D1) Settings → Usage: a **Plan usage** block above the existing
  table, which is retitled **Token usage** and otherwise unchanged; the
  block header has Refresh all and a visible **Settings** control (the
  "Summary in sidebar" switch and the Used/Remaining control) above the
  reports; it shares pins, toggle and account pick with the footer and the
  modal; disabled shows the disabled message; a daemon without
  `account.usage` shows "Update the daemon to see plan usage" and the UI
  makes no `account.usage` call.
- **AC38** (D1) Live updates: an `account.usage` event upserts that account's
  report in every open surface without a refetch (a new account is
  appended); `account.removed` drops it; a reconnect re-lists.
- **AC39** (D1) Robustness: a missing, null or reshaped field renders as "not
  shown" (percent "—", window left out of the summary), never throws; unknown
  providers get a generic gauge logo and their provider id as the name.
- **AC40** (D1+D2) No new web dependency and no data-fetching library; the
  Used/Remaining control is a small shared segmented control component;
  keyboard works (rows focus and toggle with Space, modal traps focus and
  closes on Escape, controls have accessible names).
- **AC41** (D1+D2) Required screenshot pass: light and dark, 1440×900 and
  390×844, for footer (defaults, pinned, remaining, collapsed rail, no
  data), modal (one and several accounts, long list, each problem state,
  refresh in flight, options open) and Settings → Usage; looked at and fixed
  by the implementer before the PR is opened; attached to the PR.
- **AC42** (E, optional) The run usage pill's popover shows the plan card
  (no pins, no refresh) of the run provider's account only when exactly one
  OAuth account of that provider exists, labelled with that account; nothing
  otherwise (zero or several accounts, API key, disabled, old daemon).

Process

- **AC43** `task test` and `task lint` pass on every PR; the ADR's D-list is
  not reopened inside a PR (a change needs the Human, AGENTS.md rule d); the plan
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
- `poller_notifies_after_every_completed_fetch`: callback fires after each network fetch (including an unchanged result and an error), never on a cache hit.
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

- wsapi (`internal/wsapi/account_usage_test.go`): `account_usage_all`, `account_usage_ids`, `account_usage_unknown_id_errors`, `account_usage_disabled_returns_enabled_false`, `account_usage_refresh_waits_and_returns`, `account_usage_refresh_timeout_returns_cached`, `account_usage_refresh_ignored_for_run_scoped_caller`, `account_usage_event_after_every_fetch_not_on_cache_hit`, `account_usage_refresh_all_emits_one_event_per_account_as_each_settles`, `account_usage_topic_subscribable`, `account_usage_account_removed_event_flow` (usage state removed with the account).
- CLI (`cmd/smind`): `account_usage_text_output` (golden: window line, reset formatting "resets in 2h 4m", unavailable wording for expired/rejected/no_quota, stale hint), `account_usage_json`, `account_usage_refresh_flag`, `account_usage_daemon_down_exit_code`, `account_usage_no_accounts`.
- MCP: `account_usage_tool_in_catalog_and_instructions`, `account_usage_tool_readonly`, `account_usage_tool_no_refresh_input_for_run_scope`, `usage_summary_unchanged`.

### Web UI tests (vitest + Testing Library, `web/packages/ui`)

smind's web has no in-repo Playwright, so Paseo's three e2e specs
(`packages/app/e2e/browser/usage-{modal,settings,sidebar-item}.spec.ts`) are
mirrored as component-level tests against a fake `WsClient` (the existing
`App.test.tsx` style) that answers `account.usage` and can emit the
`account.usage` / `account.removed` events and record every request. Logic
tests port Paseo's unit tests (`pinned`, `model`, `preferences`, `format`,
`copy`). The fixtures reuse Paseo's: a Claude account at 31% 5h / 54% wk and a
Codex account at 7% 5h / 12% wk.

Logic (`lib/account-usage/*.test.ts`):

- `pinned.test.ts` ← `pinned.test.ts`: `choosePinnedUsageLayout` shows meters
  only when every meter fits beside the labels, drops labels before
  overflowing, shows labels until measured; `resolvePinnedUsage` defaults to
  provider-wide pins from each account, pins replace defaults and removing all
  restores defaults only when the selection is `null`, groups by provider in
  report order not pin order, names a window by its label when the source
  gives no short label, defaults to every `summary` window when any is marked,
  keeps an empty short label empty, formats "N% left" under Remaining, leaves
  out a pinned window no report has or with no percent, shows a pin once per
  account of the provider, and keeps unavailable/error reports out.
- `preferences.test.ts` ← `preferences.test.ts`: pins in the order pinned;
  unpin keeps order; Used/Remaining does not touch pins; the account pick does
  not touch pins; preferences saved before the pick existed read as no pick;
  default windows read as pinned and the first toggle removes only that row;
  an explicit empty selection survives a write/read round trip; garbage in
  storage falls back to defaults.
- `model.test.ts` ← `model.test.ts`: `displayPercent` (used, left, none);
  `usageWindowRowLabel`; `formatUsageFreshness`; `resolveUsageRefresh`
  (pending beats an earlier failure, failed, idle); `replaceReport`,
  `upsertReport` (starts a list, appends, replaces in place, keeps a copy
  fetched later), `settleReports` (drops accounts the daemon no longer has,
  keeps a copy refreshed while the list loaded); `resolveUsageView`
  (daemon too old, loading → ready → error); `resolveAccountFilter`
  (picked account, else All, unknown pick falls back).
- `format.test.ts` / `copy.test.ts`: amounts per locale and unit, percent
  rounding and clamping, "resets 2h" / "resetting now", reset days, and every
  problem sentence (expired with relative time, rejected with status,
  no_quota detail, error with retry, disabled).
- `tone.test.ts`: 69.9 ok, 70 and 90 warning, 90.1 danger, null default, the
  report's own tone wins.

Surfaces (`components/account-usage/*.test.tsx`, `settings/usage-section.test.tsx`):

From `usage-sidebar-item.spec.ts`:
- `narrow_footer_entry_points_open_usage_as_sheet` (both the row and, in the
  collapsed rail, the icon).
- `wide_usage_opens_as_dialog_over_the_workspace_and_closes_back_to_it`
  (route, open task and tab state unchanged; mounts on open, unmounts after
  close).
- `long_reports_scroll_to_the_final_window` at wide and narrow sizes.
- `pinned_windows_show_in_footer_follow_used_remaining_and_persist`: defaults
  "31% 5h", "54% wk", "7% 5h", "12% wk" with logos; pin and unpin steps;
  Remaining flips footer, accessible name ("Claude … 69% left, Codex …") and
  cards; narrow sheet checks; reload keeps pins and toggle; Settings → Usage
  shares them; unpin everything stays empty after reload and the row reads
  "Usage".
- `without_summary_data_the_row_reads_plain_usage_and_disabled_hides_it`.
- `summary_in_sidebar_switch_turns_pins_and_summary_on_and_off`.
- `provider_logos_render_and_unknown_provider_falls_back_to_gauge`.

From `usage-modal.spec.ts`:
- `opens_from_the_footer_on_the_accounts_reports_with_no_filter_for_one_account`.
- `refreshes_one_report_from_its_card`: request is `accountIds:[id],
  refresh:true`; freshness text updates; other cards untouched.
- `shows_accounts_once_the_connection_is_ready_after_a_cold_load_on_a_phone`.
- `tells_the_user_to_update_a_daemon_without_account_usage` (no call made).
- `account_picked_in_the_modal_is_the_footers_account_after_a_reload`, plus
  `vanished_account_pick_falls_back_to_all`.
- `expired_login_refreshes_to_windows_with_visible_pin_toggles`.
- `narrow_rows_always_show_the_pin_glyph`.
- `codex_window_shapes_use_provider_durations_on_card_and_footer` (five_hour,
  weekly, `unknown_primary` / `unknown_secondary`, feature-scoped windows).
- `each_problem_state_is_explained` (expired, rejected, no_quota, error with
  retry, disabled) and `an_account_that_recovers_shows_only_its_windows`.
- `refresh_failure_keeps_previous_report_and_says_so`.
- `settings_control_opens_with_switch_and_percentages` at wide and narrow.

From `usage-settings.spec.ts`:
- `settings_usage_renders_every_account_returned`.
- `refresh_all_forces_a_fresh_report` (23% → 64%; exactly one forced
  request; plain loads never force).
- `asks_to_update_a_daemon_without_account_usage_and_never_calls_it`.
- `plan_usage_block_sits_above_token_usage_and_token_table_is_unchanged`.
- `settings_control_is_visible_above_the_reports`.

smind-specific:
- `account_usage_event_upserts_without_refetch_and_removal_drops_the_card`.
- `older_fetchedat_never_replaces_newer` (refresh racing an event).
- `reshaped_report_renders_without_throwing` (missing windows, null percent,
  unknown tone, unknown provider).
- `no_account_usage_call_while_modal_is_closed_and_footer_hidden`.
- `keyboard_rows_toggle_with_space_modal_closes_on_escape`.
- `segmented_control_a11y` (`components/ui/segmented-control.test.tsx`).
- (PR-E) `run_pill_shows_single_oauth_account_plan_card_only`,
  `run_pill_shows_nothing_for_zero_or_several_accounts`.

**Screenshot pass (required, AC41).** Playwright from the mobile workspace's
`node_modules`, against the built web UI and a temp `SMIND_HOME` daemon on a
spare port (never :4648); the `account.usage` responses and events are
supplied by the browser (`routeWebSocket`/injected stub) because the fetchers'
URLs are deliberately not overridable. Light and dark at 1440×900 and
390×844 for: footer defaults, pinned, Remaining, collapsed rail, no data;
modal with one account, several accounts, long list, every problem state,
refresh in flight, Settings control open; Settings → Usage with both blocks.
The implementer looks at them and fixes issues before reporting; the
supervisor looks at a couple before the PR is opened.

### Manual (not in CI)

- One run against a real Anthropic OAuth account and a real ChatGPT OAuth account on the author's machine, comparing the windows with `claude /usage` and the Codex app, with a throwaway log capture checked for token leakage. Redacted real responses refresh the fixtures. Result recorded in Validation.

## Implementation split (non-stacked, each branches from `origin/develop`)

Order: **PR-A → (PR-B ∥ PR-C) → PR-D1 → PR-D2 → (PR-E optional) → close-out.**
B and C may be worked in parallel because their write scopes do not overlap,
except one wiring line each in `cmd/smind/serve.go`; whichever merges second
rebases onto develop. D1 needs C's RPC and event merged; D2 is sequenced after
D1 is merged (not stacked on it). Points use the WORKSPACE_PROTOCOL axes.

| PR | Outcome | Write scope (one owner each) | Depends on | Size |
|---|---|---|---|---|
| **PR-A** types, store, fetchers, poller, config | AC1–AC16: `Report` model, Anthropic and OpenAI fetchers with fixtures, API-key/unsupported → no_quota, poller with TTL/singleflight/backoff/prune/`Peek`, per-account `EnsureFresh` lock, `accountUsage` config, serve.go wiring replacing the noop fetcher (display data now flows; routing still ignores it). **Fixtures only; live probes after Human go-ahead.** | `internal/quota/**` (+ `testdata/`), `internal/store/quota_snapshots.go` (+ test), `internal/accounts/refresh.go` (lock only), `internal/config/config.go`, `cmd/smind/serve.go`, this plan's Progress/Validation | ADR accepted (done) | M (5): scope 1, PRs 1, risk 2, decisions 0, verify 1 |
| **PR-B** routing integration | AC17–AC23: `WithModel`, windows → exhausted rules, async refresh trigger, error text with reset, `isExhausted` rewrite, proxy passes model | `internal/routing/**`, `internal/server/proxy.go` (the `Route` call and its error text only), one line in `serve.go` | PR-A merged | M (4): scope 1, PRs 1, risk 2, decisions 0, verify 0 |
| **PR-C** wsapi, CLI, MCP | AC24–AC27: `account.usage` RPC + per-fetch event + topic, run-scoped refresh denial, `smind account usage`, MCP `account_usage`, instructions, catalog tests, the `enabled` state the UI reads | `internal/wsapi/{account_usage.go,events.go,handlers.go}` (+ tests), `cmd/smind/{account.go,mcp_*.go}` (+ tests), one line in `serve.go` | PR-A merged | M (3): scope 1, PRs 1, risk 1, decisions 0, verify 0 |
| **PR-D1** web core: store, cards, modal, Settings → Usage | AC32–AC40 (all of them except footer item, pins and the layout chooser): types/model/format/copy/tone, preferences (`displayAs`, account pick, "Summary in sidebar"), report store + event wiring, cards, window bars (plain), balance bars, meter, refresh buttons, problem copy, modal (dialog/sheet), account filter, options popover, segmented control, Plan usage block in Settings → Usage, provider logo marks; screenshots for the modal and Settings | `web/packages/ui/src/{lib/account-usage/**, hooks/use-account-usage.ts, hooks/use-usage-preferences.ts, components/account-usage/** (cards, modal, controls, options), components/ui/segmented-control.tsx, components/settings/usage-section.tsx}` (+ tests), `lib/storage.ts` (one key) | PR-C merged | M (4): scope 1, PRs 1, risk 0, decisions 0, verify 1 |
| **PR-D2** web sidebar footer item and pins | AC28–AC31, AC41 (footer part): footer Usage row with logos/defaults/pinned percents, layout chooser (`ResizeObserver` measuring), pinnable window rows, pin glyph, "Summary in sidebar" switch wiring, hidden/plain states, collapsed rail, footer screenshots | `web/packages/ui/src/components/{app-sidebar.tsx (footer slot + provider only), account-usage/{usage-sidebar-item,usage-sidebar-root,usage-window-bar}.tsx}`, `lib/account-usage/pinned.ts` (+ tests) | PR-D1 merged | M (3): scope 1, PRs 1, risk 0, decisions 0, verify 1 |
| **PR-E** (optional) per-run plan usage | AC42: plan card in the run usage pill's popover for a single matching OAuth account | `web/packages/ui/src/components/{run-usage-pill.tsx, account-usage/run-plan-usage.tsx}` (+ tests) | PR-D1 merged | S (2) |
| **Close-out** docs | AC43: ROADMAP/README/stale backlog text, plan → `completed/` with Validation | `docs/**`, `README.md` | all | S |

### PR-D: Paseo → smind file map

Paseo is React Native + Unistyles + react-query + a plugin registry with a
host (machine) concept; smind's web is React 19 + Tailwind v4 + shadcn/Radix
(`web/packages/ui`), no react-query, one daemon, and an accounts table rather
than harness-login discovery. **Ported** = same behaviour and structure, **adapted** =
same idea, rewritten for smind's stack/data, **dropped** = no smind
equivalent. All smind paths are under `web/packages/ui/src/`. Read Paseo's file
before writing its counterpart (memory: prefer porting over writing UI from
scratch).

| Paseo (`packages/app/src/usage/`) | smind | How |
|---|---|---|
| `types.ts` | `lib/account-usage/types.ts` | Adapted: hand-written TS types for the `AccountUsage` wire shape (no zod protocol package); `UsageView` kept; guards tolerate missing fields (AC39) |
| `model.ts` | `lib/account-usage/model.ts` | Ported: `usedPercent`, `displayPercent`, `usageWindowRowLabel`, `formatUsageFreshness`, `resolveUsageRefresh`, `replaceReport`, `upsertReport` (laterFetched wins), `settleReports`, `resolveUsageView`. **Adapted:** `resolveUsageHostId` / `resolveUsageModalHostId` → `resolveAccountFilter` (picked account, else All); "host not connected / needs update" → "daemon not connected / lacks `account.usage`". **Deferred to PR-E:** `resolveAgentUsageView` |
| `preferences.ts` | `lib/account-usage/preferences.ts` | Ported rules (null pins = defaults, array incl. empty = selection, first toggle snapshots defaults, pins match a provider's window on every account, `effectiveUsagePins`, `toggleUsagePin`); **adapted:** `serverId` → `accountKey: string \| null`, `sourceId` → `provider`, zod → `readStored`/`writeStored` validators with a new `STORAGE_KEYS.usage` (`smind:usage-preferences`); the COMPAT legacy-`pinned` shim is dropped (no legacy) |
| `pinned.ts` | `lib/account-usage/pinned.ts` | Ported: `choosePinnedUsageLayout` (constants `MIN_METER_WIDTH` 16, `METER_GAP` 4), `resolvePinnedUsage` |
| `tone.ts` | `lib/account-usage/tone.ts` | Ported (daemon also sends `tone`; this is the fallback) |
| `format.ts` | `lib/account-usage/format.ts` | Ported: `formatPct`, `formatDisplayPct`, `formatResetLabel`, `formatAmount` (`Intl`); token amounts reuse `lib/usage-format.ts` `formatTokens` instead of Paseo's `formatTokenCount` |
| `copy.ts` | `lib/account-usage/copy.ts` | Ported single-file copy; **adapted:** remedies say "Reconnect it in Providers." (smind owns the login; Paseo's "Run <harness> to refresh it" does not apply); adds the disabled and old-daemon sentences; relative time via `lib/relative-time.ts` |
| `queries.ts` | `hooks/use-account-usage.ts` | **Adapted:** react-query is replaced by a module store + `useSyncExternalStore`: `account.usage` list (stale after 60 s, daemon cache is 5 min), `refreshOne(accountId)`, `refreshAll()`, `account.usage` / `account.removed` events through `useDaemonEvents`, reconnect re-list, per-account upsert with the same race rules as Paseo's `upsertReport`/`settleReports`. Streaming list becomes events (the RPC itself is not streamed) |
| `display.ts` | `hooks/use-usage-preferences.ts` | Ported (`useUsageDisplay`) over a small `localStorage` store shared by every surface (same pattern as `use-notification-sound-preference.ts`) |
| `hosts.ts`, `in-sidebar.ts` | `hooks/use-usage-preferences.ts` (account pick, `inSidebar`) | **Adapted:** host pick → account pick; Paseo's `useSidebarNavItems("footer")` registry does not exist in smind, so "Summary in sidebar" is a stored boolean (default on) |
| `sidebar-item.tsx` | `components/account-usage/usage-sidebar-item.tsx`, `usage-sidebar-root.tsx`; footer row in `components/app-sidebar.tsx` | Ported structure (`UsageSidebarRoot` owns open state, mounts the modal on first open and unmounts after close; `PinnedUsageTrigger`/`PinnedUsageLine` with hidden measuring copies); **adapted:** RN `onLayout` → `ResizeObserver`, `SidebarMenuButton` row in `SidebarFooter` (icon-only in the collapsed rail) |
| `usage-modal.tsx` | `components/account-usage/usage-modal.tsx` | **Adapted:** `AdaptiveModalSheet` → `components/ui/dialog.tsx` on wide, `components/ui/sheet.tsx` (bottom) when `useIsMobile()`; max width 640; sheet ≤ 80% height; body scrolls; keyed by account filter so a refresh in flight on one pick never shows on another |
| `controls.tsx` | `components/account-usage/usage-controls.tsx` | Ported layout (filter, Refresh all, Settings); **adapted:** `HostFilter` → a `components/ui/select.tsx` account filter ("All accounts" + each account label), hidden with ≤ 1 account |
| `options.tsx` | `components/account-usage/usage-options.tsx`, `components/ui/segmented-control.tsx` | **Adapted:** `DropdownMenu` + `SettingsSwitch` + `SegmentedControl` → `components/ui/popover.tsx` (sheet on narrow) with a Switch "Summary in sidebar" and a new small Radix `ToggleGroup`-based segmented control for Used / Remaining. In Settings → Usage the same control is rendered as a visible labelled **Settings** button in the block header (Paseo #5921: discoverable above reports) |
| `usage-section.tsx`, `host-usage-section.tsx` | `components/settings/usage-section.tsx` (existing file) | **Adapted:** one block, no host: Plan usage (`UsageBody`, `UsageMessage`, error `Alert` with Try again) above the existing token table, retitled Token usage. Registered section id `usage` stays |
| `list.tsx` | `components/account-usage/usage-list.tsx` | Ported (one card per account; rows pinnable only while the summary switch is on) |
| `card.tsx` | `components/account-usage/usage-card.tsx` | Ported: header (logo, name, plan `status-badge`, status dot/label, per-card refresh with freshness tooltip), messages, bars, balances, details, footer (account label + inline freshness on narrow/touch), "Unable to refresh usage". **Adapted:** `loginErrors` (several harness logins per account) is dropped: one credential per smind account |
| `window-bar.tsx` | `components/account-usage/usage-window-bar.tsx` | Ported: checkbox row when pinnable, pinned/hover highlight layers, pin glyph + tooltip, `usageWindowRowLabel` accessible name. **Dropped:** the "runs out" at-risk line (no `runsOutAt` in v1) |
| `balance-bar.tsx` | `components/account-usage/usage-balance-bar.tsx` | Ported (`Intl` language from the browser) |
| `meter.tsx` | `components/account-usage/usage-meter.tsx` | Ported: 4 px track, tone fills (`ok`/`warning`/`danger`/default) using smind's existing status colour tokens, `quiet` variant for the footer |
| `refresh-button.tsx` | `components/account-usage/usage-refresh-button.tsx` | Ported ("Refresh all", spinner via `components/ui/inline-spinner.tsx`) |
| `source-icon.tsx` | `components/account-usage/usage-source-icon.tsx` + inline Claude and OpenAI marks | **Adapted:** Paseo ships catalog SVGs per source; smind has no provider icons yet, so PR-D1 adds two inline `currentColor` marks keyed by `provider` and a lucide `Gauge` fallback |
| `agent-usage.tsx` + `useAgentUsage` | `components/account-usage/run-plan-usage.tsx`, `components/run-usage-pill.tsx` | **Optional (PR-E):** only for a single matching OAuth account (AC42) |
| `*.test.ts` (`pinned`, `model`, `preferences`, `format`, `copy`) | `lib/account-usage/*.test.ts` | Ported (names above) |
| e2e `usage-modal/-settings/-sidebar-item.spec.ts` | `components/account-usage/*.test.tsx`, `settings/usage-section.test.tsx` | Adapted to Testing Library with a fake `WsClient` (names above) |
| plugin registry, `useHosts`, host runtime, `HostFilter`, `AdaptiveModalSheet`, Unistyles themes | — | Dropped / replaced by smind's own sidebar, dialog/sheet and Tailwind tokens |

## Decisions

- Design and rationale: [ADR-0028](../../decisions/0028-account-usage-windows.md)
  (**Accepted 2026-10-11**). Answers recorded there: D1–D4 and D6–D13 as the
  recommended defaults (D1 100%, D2 on and demand-driven, D3 5 min TTL and
  1→30 min backoff, D4 Anthropic + OpenAI OAuth, D6 routing on, D7 hard keeps
  its error, D8 new `account_usage` MCP tool, D9 overage suppresses, D10
  poller refreshes only expired tokens, D11 `localStorage`, D12 503 with reset
  time, D13 global switch only). **D5 changed:** the web UI ports Paseo's
  Usage UX (PR-D1/D2, optional PR-E). Not reopened inside a PR.
- Decided here, not in the ADR (small, reversible):
  1. Naming: "Plan usage" (windows, `account.usage`) vs "Token usage"
     (`usage.summary`, `run_usage`) in every surface. Provider display names:
     `anthropic` → "Claude", `openai` → "Codex" (as in Paseo's fixtures).
  2. Fixtures are redacted copies of real responses; the live probe is the
     manual step below, gated on Human go-ahead.
  3. The wsapi/web switch for `accountUsage.enabled` reuses whatever
     mechanism the daemon has for reading and writing `config.yaml` at the
     time of PR-C. If none exists, PR-C ships read-only state in
     `account.usage` (`enabled`) and the config file is the switch; a
     write RPC would be a new decision.
  4. Per-run and proxy token usage are untouched; `usage.summary` gains no
     window concepts.
  5. "Remembers the picked account" is an account filter ("All accounts" or
     one), the smind analogue of Paseo's remembered host.
  6. The footer Usage row is present whenever polling is enabled and the
     daemon supports `account.usage` (it is not a user-toggled sidebar nav
     item as in Paseo); "Summary in sidebar" only controls whether pinned
     percents show in it and whether window rows are pinnable.
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
- 2026-10-11: Human approved ADR-0028; D1–D4, D6–D13 as recommended, D5
  changed to "port Paseo's Usage UX". ADR set to Accepted; plan updated: web
  ACs rewritten (AC28–AC42), PR-D split into D1/D2 (+ optional E) with the
  Paseo → smind file map, test scenarios mirror Paseo's e2e specs. Paseo
  read: `packages/app/src/usage/*`, `e2e/browser/usage-*.spec.ts`, history
  of #5685/#5786/#5805/#5876/#5903/#5921/#6216/#5975.
- [x] D1–D13 answered by the user
- [ ] PR-A types, store, fetchers, poller, config (fixtures only)
- [ ] Human go-ahead for the manual live probe (see Validation)
- [ ] PR-B routing integration
- [ ] PR-C wsapi, CLI, MCP
- [ ] PR-D1 web core (store, cards, modal, Settings → Usage)
- [ ] PR-D2 web footer item and pins
- [ ] PR-E (optional) per-run plan usage
- [ ] Manual live check against one real account per provider
- [ ] Close-out docs; plan moved to `completed/`

## Validation

Docs-only PRs so far (#296), nothing built. Checked: the ADR number 0028 was
free on `origin/develop` (last 0027) and in open PRs; the diff touches only
`docs/**`; no code, config, or generated file changed.

**Live endpoint probes are a PR-A manual step and need the Human's go-ahead
before they run; until then PR-A is fixtures-only.** They call real provider
endpoints with a real token (outward-facing), so they are not run by an agent
on its own. The probes settle four unknowns, each recorded here with the
date and a yes/no (never a token, email or body):
1. Anthropic: does `GET /api/oauth/usage` accept the token smind stores for a
   login done through smind's own flow (scope `user:profile`)? Which
   top-level keys come back (names only)?
2. OpenAI: does a token stored by smind's login carry the
   `chatgpt_account_id` claim the `ChatGPT-Account-Id` header needs?
3. OpenAI: does `chatgpt.com` accept the uTLS Chrome fingerprint on
   `/backend-api/wham/usage` (Paseo uses a plain `fetch` with a browser
   `User-Agent`)?
4. Anthropic: what `extra_usage.is_enabled` means for serving past a full
   window (D9).
Procedure when approved: a local, untracked helper behind a build tag that
reads the account from the user's own smind store, makes one request per
provider, and prints only the HTTP status, top-level JSON key names, window
ids, and the booleans above. No headers, no values, no argv secrets. Redacted
responses then refresh the fixtures.

Mapping for the implementation PRs (fill in as each lands):

| AC | How it will be confirmed |
|---|---|
| AC1–AC4 | `internal/quota` report/versioning/accountkey/prune tests (`report_roundtrip_and_versioning`, `accountkey_stable`, `poller_prunes_old_snapshots`) |
| AC5–AC11 | Anthropic/OpenAI/dispatch/refresh table tests with fixtures, `ensurefresh_*` under `-race` |
| AC12–AC16 | `poller_*` tests with a fake clock, config tests, `no_secret_in_logs_report_event` |
| AC17–AC23 | `route_*` tests, proxy tests |
| AC24–AC27 | wsapi, CLI golden, MCP catalog tests |
| AC28–AC31 | PR-D2 vitest tests (`pinned_windows_show_in_footer…`, `summary_in_sidebar_switch…`, logic tests) + footer screenshots |
| AC32–AC40 | PR-D1 vitest tests mirroring Paseo's modal and settings specs + `pinned`/`model`/`preferences` logic tests |
| AC41 | light/dark × 1440×900 / 390×844 screenshot set attached to PR-D1 and PR-D2, viewed by the implementer and the supervisor |
| AC42 | PR-E tests (optional) |
| AC43 | `task test`, `task lint`, ADR/plan/ROADMAP diffs |
| Real endpoints | the manual live probe above, after Human go-ahead (not automated; fixtures can drift from real responses) |

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
