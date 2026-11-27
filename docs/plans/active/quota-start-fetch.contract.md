---
v: 1
milestone: "Quota follow-up: one usage fetch at app start and the provider named in the notice"
base: origin/develop
planRef: docs/plans/active/quota-start-fetch.md
acs:
  - id: AC1
    text: "At app start exactly one account.usage touch with no account ids, no cached and no refresh."
    covers: { tests: [quota_start_fetch_sends_one_touch_at_app_start, quota_start_fetch_never_sends_refresh_or_cached], frames: [] }
  - id: AC2
    text: "Not repeated by re-render, remount, the notice, a reconnect or a usage surface within the TTL."
    covers: { tests: [quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect, quota_start_fetch_does_not_make_a_usage_surface_send_a_second_touch_within_the_ttl, quota_start_fetch_and_a_touching_surface_mounted_together_send_one_touch_in_either_order, quota_start_fetch_is_retried_only_if_the_first_never_left, ambient_quota_adds_no_touch_or_refresh_calls], frames: [] }
  - id: AC3
    text: "Not sent with a warm cache, a touch in flight, or after usage is disabled or unsupported."
    covers: { tests: [quota_start_fetch_skipped_when_the_cache_is_warm, quota_start_fetch_skipped_while_a_touch_is_in_flight, quota_start_fetch_stops_when_usage_is_disabled_or_unsupported], frames: [] }
  - id: AC4
    text: "A hidden window defers the one request until the window first becomes active."
    covers: { tests: [quota_start_fetch_waits_for_an_active_window_then_fires_once], frames: [] }
  - id: AC5
    text: "One request covers all providers and accounts."
    covers: { tests: [quota_start_fetch_is_one_request_for_all_accounts], frames: [] }
  - id: AC6
    text: "The notice appears after the start fetch without opening a usage surface; the app budget test is updated."
    covers: { tests: [quota_app_start_sends_one_touch_then_the_notice_shows, command_palette_opens_usage_modal], frames: [] }
  - id: AC7
    text: "The notice text names the provider and the account in parentheses when several."
    covers: { tests: [ambient_quota_text_names_the_provider, ambient_quota_text_names_provider_and_account, ambient_quota_notice_names_the_daemon_provider_label], frames: [] }
  - id: AC8
    text: "No daemon, wire or mobile change; ADR-0028 and plan amendments present; public text rules hold."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: quota_start_fetch_sends_one_touch_at_app_start, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_sends_one_touch_at_app_start", timeoutSec: 900 }
  - { name: quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect", timeoutSec: 900 }
  - { name: quota_start_fetch_skipped_when_the_cache_is_warm, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_skipped_when_the_cache_is_warm", timeoutSec: 900 }
  - { name: quota_start_fetch_skipped_while_a_touch_is_in_flight, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_skipped_while_a_touch_is_in_flight", timeoutSec: 900 }
  - { name: quota_start_fetch_waits_for_an_active_window_then_fires_once, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_waits_for_an_active_window_then_fires_once", timeoutSec: 900 }
  - { name: quota_start_fetch_is_one_request_for_all_accounts, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_is_one_request_for_all_accounts", timeoutSec: 900 }
  - { name: quota_start_fetch_stops_when_usage_is_disabled_or_unsupported, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_stops_when_usage_is_disabled_or_unsupported", timeoutSec: 900 }
  - { name: quota_start_fetch_never_sends_refresh_or_cached, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_never_sends_refresh_or_cached", timeoutSec: 900 }
  - { name: quota_start_fetch_and_a_touching_surface_mounted_together_send_one_touch_in_either_order, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_and_a_touching_surface_mounted_together_send_one_touch_in_either_order", timeoutSec: 900 }
  - { name: quota_start_fetch_is_retried_only_if_the_first_never_left, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_is_retried_only_if_the_first_never_left", timeoutSec: 900 }
  - { name: quota_start_fetch_does_not_make_a_usage_surface_send_a_second_touch_within_the_ttl, run: "cd web/packages/ui && bunx vitest run src/hooks/use-usage-start-fetch.test.tsx -t quota_start_fetch_does_not_make_a_usage_surface_send_a_second_touch_within_the_ttl", timeoutSec: 900 }
  - { name: ambient_quota_text_names_the_provider, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_text_names_the_provider", timeoutSec: 900 }
  - { name: ambient_quota_text_names_provider_and_account, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_text_names_provider_and_account", timeoutSec: 900 }
  - { name: ambient_quota_notice_names_the_daemon_provider_label, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_notice_names_the_daemon_provider_label", timeoutSec: 900 }
  - { name: ambient_quota_adds_no_touch_or_refresh_calls, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_adds_no_touch_or_refresh_calls", timeoutSec: 900 }
  - { name: quota_app_start_sends_one_touch_then_the_notice_shows, run: "cd web/packages/ui && bunx vitest run src/App.usage-start.test.tsx -t quota_app_start_sends_one_touch_then_the_notice_shows", timeoutSec: 900 }
  - { name: command_palette_opens_usage_modal, run: "cd web/packages/ui && bunx vitest run src/App.test.tsx -t command_palette_opens_usage_modal", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- mobile internal cmd)\" && grep -q \"Amended 2026-11-27\" docs/decisions/0028-account-usage-windows.md", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/hooks/use-usage-start-fetch.ts, change: "the one-shot guard is not kept, so a remount sends a second fetch", expectFail: [quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect] }
  - { id: M2, file: web/packages/ui/src/hooks/use-usage-start-fetch.ts, change: "the fetch goes out once per account with account ids instead of one request for all", expectFail: [quota_start_fetch_is_one_request_for_all_accounts] }
  - { id: M3, file: web/packages/ui/src/hooks/use-usage-start-fetch.ts, change: "a touch loop: the fetch re-sends on an interval", expectFail: [quota_start_fetch_sends_one_touch_at_app_start] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# Quota start-fetch validation contract

Spec: `docs/plans/active/quota-start-fetch.md`. Web only, no daemon change; the call budget is the point: the tests count account.usage requests. Frames are not validator frames: the Lead captures them (REAL-WINDOW or WEB-HARNESS, labelled) under the dogfood root and cites them in the PR.
