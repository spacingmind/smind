# Ambient quota notice at 90 % used (ADR-0059 item F, D5)

Governing: ADR-0059 section F and decision D5 (90 % used, once per window), ADR-0028 (call budget, hard rules). Size M, web only; UI change: validation contract `ambient-quota.contract.md` (ADR-0045), light and dark frames at 1280 and 390. Out of scope (stay as they are): the sidebar footer item, Settings -> Usage, the rate-limited-run "Wait until" reorder, mobile, a configurable threshold.

## Behaviour

One non-blocking, dismissible notice just above the composer's input card when a plan window used by the composer's provider reaches 90 % used: "5-hour window 92% used, resets 20:52". At 100 % the same notice says "5-hour window used up, resets 20:52". The window is named as the report names it (its `label`: 5-hour, weekly, a per-model weekly); the reset time is in the viewer's local time (24-hour clock; another day adds the weekday).

- **Source**: only the reports already in the shared `account.usage` cache (`useUsageReports({ enabled: false })`: it reads the cache and never fetches). It sends NO request of any mode (no `touch`, no `refresh`, not even the `cached` read) and adds no wire change: the app-level call budget test (`App.test.tsx`, "no usage request until a usage surface opens") stays green. Consequence, stated plainly: the notice shows only once reports are in the cache, i.e. after a usage surface (Settings -> Usage, the Usage modal, the sidebar item when turned on) has loaded them or a live `account.usage` event (a completed fetch) has arrived; it never loads them itself. A `cached` peek on mount would make it work from a cold start at the price of changing that call budget rule: a Human decision, not taken here.
- **Which account**: the reports whose `provider` equals the composer provider's account provider (`ProviderInfo.accountProvider`, else the provider catalog identity: claude-native -> anthropic). Another provider's account never counts.
- **Decision D1 (a refinement of "the task's account", taken as the conservative default)**: the web knows no task-to-account binding (a pool may route a run to any account of the provider). When the provider has several accounts, a window warns only when EVERY account of that provider with a fresh reading of that window id is at or over the level (the pool has no headroom left); the text then names the account that is most used. One account: it warns on its own and the text does not name it.
- **Levels and memory**: two levels per window, `warn` (>= 90 % and < 100 %) and `full` (>= 100 %). A notice is keyed by `accountId|windowId|resetsAt|level`, with `resetsAt` rounded to 5 minutes (a vendor jitter of seconds is the same window; a new window differs by hours). Dismissing a notice stores its key in browser storage (`smind:dismissed-quota-notices`, a bounded list: entries whose window ended more than a day ago are dropped, at most 200 kept). A dismissed key never shows again; a new `resetsAt` is a new key; `full` after a dismissed `warn` is a new key and shows once. Without a dismissal the notice stays while the condition holds (it is not a toast).
- **One at a time**: with several windows at or over the threshold the order is `full` before `warn`, then the higher used percent, then the earlier reset, then the window id; after a dismissal the next one shows.
- **No notice** when: the reading is below 90 %; the account's report is missing, `stale`, not `available` or older than 30 minutes; the window has no percent (a status-only observed window) or no `resetsAt`, or its `resetsAt` is not in the future; the account has no windows; the provider has no account provider (a custom provider with no plan).
- **Layout**: a full-width row in the composer's flow above the input card (a `NoticeBanner`, warning tone at `warn`, error tone at `full`, the shared dismiss button, an icon from `lib/icons.ts`, semantic color tokens only); it never overlays the composer, wraps its text at 390 px and adds no horizontal scroll.

## Acceptance criteria

- AC1 A window used by the composer provider's account at 90 % or more used shows the notice; 89 % and below shows none (the threshold is exactly 90).
- AC2 The text names the window as the report labels it and the local reset time; `full` says "used up"; the percent is floored (99.7 reads 99%).
- AC3 Nothing shows for a missing, `stale`, non-`available` or older-than-30-minutes reading, a window without percent or without a future reset, an account without windows, or a provider without an account provider.
- AC4 Only the composer provider's accounts count; with several accounts the pool rule D1 applies and the text names the most used account.
- AC5 Dismissal persists across remounts and reloads (browser storage), is keyed by account, window, reset (5-minute bucket) and level, and a new `resetsAt` shows the notice again; `full` after a dismissed `warn` shows once; the stored list stays bounded.
- AC6 With several windows at or over the threshold one notice shows at a time in the documented order and the next appears after a dismissal.
- AC7 Zero added calls: mounting the notice (with or without reports in the cache, also across a live `account.usage` event) sends no `account.usage` request of any mode (a count-asserting test, like `use-account-rows.test.tsx`), and the app-level usage call budget test still passes.
- AC8 The notice sits in the composer's flow above the input card, never positioned over it, and keeps the composer fully usable at 390 px.
- AC9 No daemon, wire or `mobile/` change; icons from `lib/icons.ts`; semantic tokens only; user text says "sMind" and "your workspace" and names no other product (`go test ./internal/hygiene/` passes).

## Named tests

`src/lib/ambient-quota.test.ts`: `ambient_quota_fires_at_90_not_89`, `ambient_quota_100_says_used_up`, `ambient_quota_text_names_window_and_local_reset_time`, `ambient_quota_none_for_stale_or_missing_reading`, `ambient_quota_none_without_windows_or_percent`, `ambient_quota_only_the_providers_accounts`, `ambient_quota_pool_waits_for_every_account`, `ambient_quota_key_buckets_resets_at`, `ambient_quota_orders_windows`.
`src/lib/quota-notice-store.test.ts`: `ambient_quota_storage_is_bounded_and_prunes_ended_windows`.
`src/components/composer/ambient-quota-notice.test.tsx`: `ambient_quota_notice_shows_at_92_and_is_dismissible`, `ambient_quota_dismissal_persists_across_remounts`, `ambient_quota_new_resets_at_is_a_new_window`, `ambient_quota_full_after_dismissed_warn_shows_once`, `ambient_quota_no_notice_below_threshold`, `ambient_quota_no_notice_for_another_providers_account`, `ambient_quota_adds_no_touch_or_refresh_calls`, `ambient_quota_notice_is_in_flow_not_overlaying_the_composer`, `ambient_quota_composer_mounts_the_notice`.
e2e (`web/e2e/specs/ambient-quota.spec.ts`, frames only with `QUOTA_FRAMES_DIR`): the states below with `account.usage` answered locally through `page.routeWebSocket` (no vendor is ever called).

## Frames (light and dark at 1280 and 390, every pair opened by the Lead)

`01-notice-92`, `02-notice-100`, `03-dismissed` (gone), `04-below-threshold` (none), `05-two-windows` (the higher shows, the other after a dismissal), `06-stale-reading` (none). Contact sheet and README under `~/.smind/dogfood/ambient-quota/`.

## Mutations (committed code; each must fail a named test)

- Threshold off by one (`>` for `>=` so 90 hides; or 89 shows) -> `ambient_quota_fires_at_90_not_89`.
- No once-per-window memory (the dismissal ignored) -> `ambient_quota_dismissal_persists_across_remounts`.
- The key without `resetsAt` -> `ambient_quota_new_resets_at_is_a_new_window`.
- The cache-only read replaced by `useUsageReports({ touch: true })` (or by the default `useUsageReports()`, which sends the `cached` read) -> `ambient_quota_adds_no_touch_or_refresh_calls` (and the app call budget test in `App.test.tsx`). Adding `touch: true` BESIDE `enabled: false` is an equivalent mutant: a disabled reader never touches.

## Progress

- [ ] spec and contract (this file)
- [ ] logic, storage and hook (`lib/ambient-quota.ts`, `lib/quota-notice-store.ts`, `hooks/use-quota-notice.ts`)
- [ ] component and composer mount
- [ ] e2e spec and frames
- [ ] review, mutations, validator

## Validation

### When is the usage cache filled? (can the notice appear without opening a usage surface?)

The notice reads `queryKeys.accountUsage()` and never fills it (`hooks/use-quota-notice.ts`:
`useUsageReports({ enabled: false })`). Code evidence for what fills that entry in a real session:

| Filler | Where | Mode | When |
| --- | --- | --- | --- |
| Settings -> Usage | `components/account-usage/use-usage-surface.ts:54` (`useUsageReports({ touch })`) | touch | while the section is mounted |
| Usage modal | `components/account-usage/usage-modal.tsx` (same hook) | touch | while it is open |
| Sidebar footer summary | `components/account-usage/usage-sidebar-item.tsx:203` (`useUsageReports({ touch: true })`) | touch | only while "Show summary in sidebar" is ON, and not on the collapsed rail. The default is OFF (`lib/account-usage/preferences.ts`: `inSidebar: false`) |
| Providers screen / provider detail / accounts card | `hooks/use-account-rows.ts:29`, `components/providers/accounts-card.tsx:32` (`useUsageReports()`, default) | one `cached` read | when that screen mounts |
| `account.usage` daemon event (the poller) | `lib/query/event-bridge.ts:71-90` | none (no request) | only UPDATES an existing cache: `if (!held) return;`. It never creates it. |

Not fillers: the chat composer, the chat header and any "chat usage pill" (no `useUsageReports` caller outside the
list above; the "chat pill" wording in the hook's doc comment describes a design that has no caller today).

Conclusion, stated plainly: **with the sidebar summary off (the default) the notice can NOT appear until the
person has opened Settings -> Usage, the Usage modal or a Providers screen in that session (that connection's
QueryClient).** After any of those has filled the cache, the daemon's `account.usage` events keep it current and
the notice then appears and updates on its own, in any task, with no further call. With the sidebar summary ON
the cache is filled at app start by that surface's own touch, so the notice works without opening anything. This
PR does not widen the call budget (ADR-0028 AC54; `App.test.tsx command_palette_opens_usage_modal` stays green);
whether the composer should do a one-time `cached` peek is the Human's decision.

Frame of the real flow: `~/.smind/dogfood/ambient-quota/pair-01-notice-92.png` (e2e AQ-01: cold start shows no
notice with `account.usage` requests = 0, Settings -> Usage fills the cache, back in the task the notice shows
at 92 %). The cold-start frame is `pair-00-cold-start.png`.

> Follow-up 2026-11-27: the cold-start limitation described in Validation above is resolved by `docs/plans/active/quota-start-fetch.md` (one `touch` per app start, ADR-0028 amended) and the notice text now names the provider.
