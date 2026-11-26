---
v: 1
milestone: "ADR-0059 F: ambient quota notice at 90% used above the composer"
base: origin/develop
planRef: docs/plans/active/ambient-quota.md
acs:
  - id: AC1
    text: "A window used by the composer provider account at 90 percent or more shows the notice; 89 percent and below shows none."
    covers: { tests: [ambient_quota_fires_at_90_not_89, ambient_quota_notice_shows_at_92_and_is_dismissible, ambient_quota_no_notice_below_threshold], frames: [] }
  - id: AC2
    text: "The text names the window as the report labels it and the local reset time; full says used up; the percent is floored."
    covers: { tests: [ambient_quota_100_says_used_up, ambient_quota_text_names_window_and_local_reset_time], frames: [] }
  - id: AC3
    text: "No notice for a missing, stale, non-available or too old reading, a window without percent or future reset, an account without windows, or a provider without an account provider."
    covers: { tests: [ambient_quota_none_for_stale_or_missing_reading, ambient_quota_none_without_windows_or_percent], frames: [] }
  - id: AC4
    text: "Only the composer provider accounts count; with several accounts the pool rule applies."
    covers: { tests: [ambient_quota_only_the_providers_accounts, ambient_quota_pool_waits_for_every_account, ambient_quota_no_notice_for_another_providers_account], frames: [] }
  - id: AC5
    text: "Dismissal persists, is keyed by account window reset bucket and level, a new resetsAt shows again, full after a dismissed warn shows once, the stored list is bounded."
    covers: { tests: [ambient_quota_dismissal_persists_across_remounts, ambient_quota_new_resets_at_is_a_new_window, ambient_quota_full_after_dismissed_warn_shows_once, ambient_quota_key_buckets_resets_at, ambient_quota_storage_is_bounded_and_prunes_ended_windows], frames: [] }
  - id: AC6
    text: "With several windows over the threshold one shows at a time in the documented order."
    covers: { tests: [ambient_quota_orders_windows], frames: [] }
  - id: AC7
    text: "Zero added provider calls: no touch, no refresh, no account.usage request when the cache holds the reports."
    covers: { tests: [ambient_quota_adds_no_touch_or_refresh_calls], frames: [] }
  - id: AC8
    text: "The notice sits in the composer flow above the input card and never overlays it; the composer mounts it."
    covers: { tests: [ambient_quota_notice_is_in_flow_not_overlaying_the_composer, ambient_quota_composer_mounts_the_notice], frames: [] }
  - id: AC9
    text: "No daemon wire or mobile change; public text rules hold."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: ambient_quota_fires_at_90_not_89, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_fires_at_90_not_89", timeoutSec: 900 }
  - { name: ambient_quota_100_says_used_up, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_100_says_used_up", timeoutSec: 900 }
  - { name: ambient_quota_text_names_window_and_local_reset_time, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_text_names_window_and_local_reset_time", timeoutSec: 900 }
  - { name: ambient_quota_none_for_stale_or_missing_reading, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_none_for_stale_or_missing_reading", timeoutSec: 900 }
  - { name: ambient_quota_none_without_windows_or_percent, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_none_without_windows_or_percent", timeoutSec: 900 }
  - { name: ambient_quota_only_the_providers_accounts, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_only_the_providers_accounts", timeoutSec: 900 }
  - { name: ambient_quota_pool_waits_for_every_account, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_pool_waits_for_every_account", timeoutSec: 900 }
  - { name: ambient_quota_key_buckets_resets_at, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_key_buckets_resets_at", timeoutSec: 900 }
  - { name: ambient_quota_orders_windows, run: "cd web/packages/ui && bunx vitest run src/lib/ambient-quota.test.ts -t ambient_quota_orders_windows", timeoutSec: 900 }
  - { name: ambient_quota_storage_is_bounded_and_prunes_ended_windows, run: "cd web/packages/ui && bunx vitest run src/lib/quota-notice-store.test.ts -t ambient_quota_storage_is_bounded_and_prunes_ended_windows", timeoutSec: 900 }
  - { name: ambient_quota_notice_shows_at_92_and_is_dismissible, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_notice_shows_at_92_and_is_dismissible", timeoutSec: 900 }
  - { name: ambient_quota_dismissal_persists_across_remounts, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_dismissal_persists_across_remounts", timeoutSec: 900 }
  - { name: ambient_quota_new_resets_at_is_a_new_window, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_new_resets_at_is_a_new_window", timeoutSec: 900 }
  - { name: ambient_quota_full_after_dismissed_warn_shows_once, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_full_after_dismissed_warn_shows_once", timeoutSec: 900 }
  - { name: ambient_quota_no_notice_below_threshold, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_no_notice_below_threshold", timeoutSec: 900 }
  - { name: ambient_quota_no_notice_for_another_providers_account, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_no_notice_for_another_providers_account", timeoutSec: 900 }
  - { name: ambient_quota_adds_no_touch_or_refresh_calls, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_adds_no_touch_or_refresh_calls", timeoutSec: 900 }
  - { name: ambient_quota_notice_is_in_flow_not_overlaying_the_composer, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_notice_is_in_flow_not_overlaying_the_composer", timeoutSec: 900 }
  - { name: ambient_quota_composer_mounts_the_notice, run: "cd web/packages/ui && bunx vitest run src/components/composer/ambient-quota-notice.test.tsx -t ambient_quota_composer_mounts_the_notice", timeoutSec: 900 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop --name-only -- mobile internal cmd)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/lib/ambient-quota.ts, change: "the threshold comparison is > instead of >= (90 hidden)", expectFail: [ambient_quota_fires_at_90_not_89] }
  - { id: M2, file: web/packages/ui/src/hooks/use-quota-notice.ts, change: "the dismissed set is ignored (the notice fires every render)", expectFail: [ambient_quota_dismissal_persists_across_remounts] }
  - { id: M3, file: web/packages/ui/src/lib/ambient-quota.ts, change: "the notice key leaves out resetsAt", expectFail: [ambient_quota_new_resets_at_is_a_new_window] }
  - { id: M4, file: web/packages/ui/src/hooks/use-quota-notice.ts, change: "useUsageReports is called with touch true", expectFail: [ambient_quota_adds_no_touch_or_refresh_calls] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# Ambient quota notice validation contract

Spec: `docs/plans/active/ambient-quota.md`. Web only. Frames are not validator frames: the Lead captures them (light and dark, 1280 and 390) from `web/e2e/specs/ambient-quota.spec.ts` under the dogfood root; the validator checks the tests, mutations and commands.
