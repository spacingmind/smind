---
v: 1
milestone: "C5a: failure classes (Retry-After / RetryInfo, subscription-sharing block, no fallback after streamed output)"
base: origin/develop
planRef: docs/plans/active/quota-auth-hardening.md
acs:
  - id: AC1
    text: "ParseRetryInfo string and object forms, garbage rejected."
    covers: { tests: [retryinfo_string_and_object_forms, retryinfo_rejects_garbage_negative_zero, retryinfo_found_among_other_details], frames: [] }
  - id: AC2
    text: "retryHint order and text forms."
    covers: { tests: [retry_hint_order_and_text_forms], frames: [] }
  - id: AC3
    text: "Usage 429 body RetryInfo sets the wait; Retry-After wins; capped read; one call."
    covers: { tests: [usage_429_retryinfo_body_sets_the_wait, usage_429_retry_after_beats_retryinfo, usage_429_body_read_is_capped_and_one_call], frames: [] }
  - id: AC4
    text: "Transient retry honours a provider delay in [1s,1h]."
    covers: { tests: [transient_retry_honours_provider_delay_between_1s_and_1h], frames: [] }
  - id: AC5
    text: "Subscription sharing text is account_blocked."
    covers: { tests: [subscription_sharing_text_is_account_blocked, subscription_word_alone_is_not_account_blocked, account_blocked_is_human_fix_and_known], frames: [] }
  - id: AC6
    text: "account_blocked never retried, failed over, cooled."
    covers: { tests: [account_blocked_never_retried_failed_over_or_cooled], frames: [] }
  - id: AC7
    text: "No fallback after the first streamed delta; control and wait unchanged."
    covers: { tests: [no_fallback_after_the_first_streamed_delta, fallback_still_starts_without_streamed_output, wait_retry_still_queued_after_streamed_output], frames: [] }
  - id: AC8
    text: "account_blocked in every failure-kind table."
    covers: { tests: [failure_text_covers_account_blocked, account_blocked_is_in_the_web_failure_tables, account_blocked_is_in_the_mobile_failure_tables, account_blocked_class_card_e2e], frames: [] }
  - id: AC12
    text: "Scope: no accounts/server/relay change in C5a."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: retryinfo_string_and_object_forms, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run retryinfo_string_and_object_forms -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: retryinfo_rejects_garbage_negative_zero, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run retryinfo_rejects_garbage_negative_zero -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: retryinfo_found_among_other_details, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run retryinfo_found_among_other_details -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: retry_hint_order_and_text_forms, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run retry_hint_order_and_text_forms -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: subscription_sharing_text_is_account_blocked, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run subscription_sharing_text_is_account_blocked -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: subscription_word_alone_is_not_account_blocked, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run subscription_word_alone_is_not_account_blocked -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: account_blocked_is_human_fix_and_known, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/failure/ -run account_blocked_is_human_fix_and_known -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: usage_429_retryinfo_body_sets_the_wait, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run usage_429_retryinfo_body_sets_the_wait -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: usage_429_retry_after_beats_retryinfo, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run usage_429_retry_after_beats_retryinfo -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: usage_429_body_read_is_capped_and_one_call, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run usage_429_body_read_is_capped_and_one_call -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: transient_retry_honours_provider_delay_between_1s_and_1h, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/ -run transient_retry_honours_provider_delay_between_1s_and_1h -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: account_blocked_never_retried_failed_over_or_cooled, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/ -run account_blocked_never_retried_failed_over_or_cooled -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: no_fallback_after_the_first_streamed_delta, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/ -run no_fallback_after_the_first_streamed_delta -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: fallback_still_starts_without_streamed_output, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/ -run fallback_still_starts_without_streamed_output -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: wait_retry_still_queued_after_streamed_output, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/ -run wait_retry_still_queued_after_streamed_output -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: failure_text_covers_account_blocked, run: "TMPDIR=/tmp/c5-tmp go test ./cmd/smind/ -run failure_text_covers_account_blocked -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: account_blocked_is_in_the_web_failure_tables, run: "cd web/packages/ui && bunx vitest run src/lib/failure-text.test.ts src/lib/failure-class.test.ts -t account_blocked_is_in_the_web_failure_tables", timeoutSec: 1200 }
  - { name: account_blocked_is_in_the_mobile_failure_tables, run: "cd mobile && bunx vitest run -t account_blocked_is_in_the_mobile_failure_tables", timeoutSec: 1200 }
  - { name: account_blocked_class_card_e2e, run: "cd web/e2e && E2E_FULL=1 bunx playwright test specs/failure-classes.spec.ts -g account-blocked --project=light-1280", timeoutSec: 1200 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal/accounts internal/server internal/relay)\"", timeoutSec: 1200 }
mutations:
  - { id: M1, file: internal/runs/failure/classify.go, change: "retryHint ignores a RetryInfo fragment in the text and quota ignores the 429 body", expectFail: [retry_hint_order_and_text_forms, usage_429_retryinfo_body_sets_the_wait] }
  - { id: M2, file: internal/runs/failover.go, change: "restore transientRetryAfterMin 5s / transientRetryAfterMax 5m", expectFail: [transient_retry_honours_provider_delay_between_1s_and_1h] }
  - { id: M3, file: internal/runs/failure/classify.go, change: "drop the subscription-sharing regexp", expectFail: [subscription_sharing_text_is_account_blocked] }
  - { id: M4, file: internal/runs/failure/kinds.go, change: "remove account_blocked from IsHumanFix", expectFail: [account_blocked_is_human_fix_and_known, account_blocked_never_retried_failed_over_or_cooled] }
  - { id: M5, file: internal/runs/failover.go, change: "remove the streamed-output guard before startFallbackSuccessor", expectFail: [no_fallback_after_the_first_streamed_delta] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./internal/runs/... ./internal/quota/ ./cmd/smind/", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=/tmp/c5-tmp go test ./internal/runs/... ./internal/quota/ ./internal/taskrunner/ ./internal/inbox/ ./cmd/smind/ -count=1", timeoutSec: 2400 }
  - { id: C3, run: "cd web/packages/ui && bunx tsc -b && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 5, maxMinutes: 90 }
---

# C5a validation contract

Spec: `docs/plans/active/quota-auth-hardening.md`. Fakes only: no live provider call. The provider-call-count assertions are in the tests themselves.
