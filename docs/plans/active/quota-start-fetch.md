# Quota follow-up: one usage fetch at app start, and the provider named in the notice

Size S to M, web only, **HIGH attention: it touches the usage call budget (ADR-0028 section 3 hard rules).** Human decision 2026-11-27, on the finding of the ambient quota PR (ADR-0059 F): the notice reads only the shared usage cache, and nothing fills that cache until a usage surface opens, so with the sidebar summary off (the default) the notice cannot appear. Decision: **one usage fetch at app start**, single-flight per provider, inside the daemon's call budget, never repeated by the notice itself; and name the provider in the notice text. Contract: `docs/plans/active/quota-start-fetch.contract.md`. No daemon, wire or `mobile/` change.

## Decisions

- **D1 One `touch` per app start.** On the first usable connection of an app start the web UI sends exactly ONE `account.usage` request with `mode:"touch"` (no `accountIds`, so the daemon answers every account), through the same touch machinery the usage surfaces use (the `usageMutationKeys.touch` mutation, so the cadence controllers see it as the last touch and a Usage surface opened right after does not send a second one inside the TTL). It fills the shared cache; the live `account.usage` events keep it current. It is never `refresh`, never `cached`-then-`touch`, and it is not repeated by anything: not by the notice (which stays `useUsageReports({ enabled: false })`, no request of any mode), not by a re-render or remount, not by a reconnect (a reconnect still re-lists `cached`, as ADR-0028 rule 4 says).
- **D2 What "app start" means.** One per `QueryClient` (the connection's query client lives for the page, so a page load or reload is one app start; a reconnect to the same daemon is not). Guarded by a `WeakSet<QueryClient>` set at the moment the request is sent, so it holds across remounts and strict-mode double effects.
- **D3 Single-flight, and inside the budget.** The request goes out only if no touch is in flight (`queryClient.isMutating({ mutationKey: touch })` is 0) and the cache is not warm (no held data with a touch inside the TTL, `lastTouchAt`). It waits for a visible and focused window like every touch (`windowIsActive()`): if the window is hidden at start, it fires once when the window first becomes active, still only once. The daemon is the real guarantee: per account at most one fetch per TTL (5 min), single-flight per account across all callers, floor and backoff persisted; a request over the floor is served from the cache (`stale`, `refreshBlocked`), never queued. The client adds no loop, no timer and no retry. `account.usage` answering `enabled:false` or "unknown method" ends it (the existing handling in the touch mutation: no retry).
- **D3b One sender at a time (review finding).** A usage surface that mounts at the same time as the start fetch (the sidebar summary, when it is on, is mounted at app start) must not send a second touch. Every sender of a touch (the start fetch and the surfaces' cadence controller) first records a **claim** (`touchClaims`, a `WeakMap<QueryClient, number>` set synchronously at the moment of sending, before the mutation's own state exists), `lastTouchAt` counts the claim, and every sender skips when `lastTouchAt` is inside the TTL: so whichever runs second, in either order, sends nothing. A touch that never left the machine (the socket was already closed: `ConnectionClosedError`) clears its claim, and the start fetch's one-shot mark, so the next connection of the same app start may send the one request; any other failure does not retry.
- **D4 Per provider.** The daemon's `Touch` is per account, so one request for all accounts is one single-flight fetch per provider account and nothing per window: a provider with several windows is still one fetch.
- **D5 Where it runs.** In the main window's app shell only (not in a desktop task window, which would repeat the same request; the daemon would dedupe it anyway).
- **D6 The text names the provider.** The notice reads "Claude · 5-hour window 92% used, resets Fri 01:58" (and "... used up, resets ..." at 100 %): the provider's display name as the UI shows it elsewhere (`accountProviderLabel(accountProvider, providers)`: the daemon's own label wins, then the identity table, then the raw id) then " · " then today's text. With several accounts the account name follows the provider in parentheses: "Claude (work) · 5-hour window 92% used, ...". "5-hour window" alone was ambiguous with several providers.
- **D7 ADR and plan amendments.** ADR-0028 section 3 rule 4 ("No fetch on daemon startup, ... or on client reconnect") gets an amendment note: the web client sends one `touch` per app start; the daemon rule is unchanged (it never fetches on its own startup or on reconnect, and the floor, budget and single-flight hold). The plan `account-usage-windows.md` AC54 is amended in place with the same sentence ("with the footer item off and no modal open the web UI sends no `touch`" becomes "... sends no `touch` except one per app start (ADR-0028 amendment 2026-11-27)"), and `ambient-quota.md` records that its cold-start limitation is resolved by this plan.

## Acceptance criteria

- AC1 At app start the web UI sends exactly one `account.usage` request, `mode:"touch"` with no account ids, and no `cached` and no `refresh`, once the window is visible and focused.
- AC2 It is not repeated by a re-render, a remount, the notice, a reconnect, or a Usage surface opened within the TTL.
- AC3 It is not sent when the cache is warm (held data and a touch inside the TTL), when a touch is already in flight, or after the daemon said usage is disabled or unsupported.
- AC4 A hidden window at start defers the one request until the window first becomes active.
- AC5 One request covers all providers: several accounts and windows never produce more than one request.
- AC6 The notice appears after the start fetch without any usage surface being opened (an app-level count-asserting test: one `touch`, then the notice).
- AC7 The notice text names the provider (and the account in parentheses when several accounts), at `warn` and `full`.
- AC8 `ADR-0028` rule 4 and `account-usage-windows.md` AC54 carry the amendment; no daemon, wire or `mobile/` change; hygiene green.
- AC9 Frames (REAL-WINDOW where the screen is unlocked, else WEB-HARNESS, every frame labelled): the notice with the provider at 92 % and 100 %, light and dark, at 1280 and 390.

## Named tests

- `hooks/use-usage-start-fetch.test.tsx`: `quota_start_fetch_sends_one_touch_at_app_start`, `quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect`, `quota_start_fetch_skipped_when_the_cache_is_warm`, `quota_start_fetch_skipped_while_a_touch_is_in_flight`, `quota_start_fetch_waits_for_an_active_window_then_fires_once`, `quota_start_fetch_is_one_request_for_all_accounts`, `quota_start_fetch_stops_when_usage_is_disabled_or_unsupported`, `quota_start_fetch_never_sends_refresh_or_cached`, `quota_start_fetch_does_not_make_a_usage_surface_send_a_second_touch_within_the_ttl`, `quota_start_fetch_and_a_touching_surface_mounted_together_send_one_touch_in_either_order`, `quota_start_fetch_is_retried_only_if_the_first_never_left`.
- `lib/ambient-quota.test.ts`: `ambient_quota_text_names_the_provider`, `ambient_quota_text_names_provider_and_account`. `components/composer/ambient-quota-notice.test.tsx`: `ambient_quota_notice_names_the_daemon_provider_label`, and the existing `ambient_quota_adds_no_touch_or_refresh_calls` stays (the notice itself still sends nothing).
- `App.test.tsx`: `command_palette_opens_usage_modal` is updated (exactly one `touch` at start, never `refresh`, and the modal adds none inside the TTL); `App.usage-start.test.tsx`: `quota_app_start_sends_one_touch_then_the_notice_shows`.
- `web/e2e/specs/ambient-quota.spec.ts` is updated (the notice appears after the start fetch, no Settings detour): `AQ-00` cold start sends exactly one touch and then the notice, `AQ-01` to `AQ-06` as before with the provider in the text.

## Mutations (committed code; each must fail a named test)

- A second fetch on remount (the one-shot guard is not kept) -> `quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect`.
- A fetch per window or per account (the request carries account ids and goes out once per account) -> `quota_start_fetch_is_one_request_for_all_accounts`.
- A touch loop (an interval or a re-send after the answer) -> `quota_start_fetch_sends_one_touch_at_app_start` (its count after advancing the fake clock past the TTL).

## Frames

Notice with the provider at 92 % and at 100 %, light and dark, 1280 and 390. REAL-WINDOW where the screen is unlocked (the audit app against a dogfood daemon with a fake usage report: no real vendor endpoint is ever called, the daemon's account has no real credential); else WEB-HARNESS; every frame labelled.

## Progress

- [ ] spec and contract (this file)
- [ ] the start fetch hook and its tests
- [ ] the notice text, the updated app and e2e tests
- [ ] ADR-0028 and plan amendments
- [ ] review (two checks by the Lead), mutations, validator, frames

## Validation

(filled in when the work is done)
