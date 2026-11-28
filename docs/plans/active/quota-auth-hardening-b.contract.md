---
v: 1
milestone: "C5b: account refresh (terminal refresh token, sleep/wake spread)"
base: origin/develop
planRef: docs/plans/active/quota-auth-hardening.md
acs:
  - id: AC9
    text: "A terminal refresh error is remembered: the refresher is called once; a new credential allows one more."
    covers: { tests: [ensurefresh_terminal_error_is_remembered_and_calls_the_refresher_once, ensurefresh_transient_error_is_not_remembered, ensurefresh_new_credential_clears_the_mark, is_terminal_refresh_error_table], frames: [] }
  - id: AC10
    text: "The proxy routes around a dead account, answers 401 with the sentence when none is left, never re-hits the token endpoint."
    covers: { tests: [proxy_routes_around_a_needs_relogin_account, proxy_answers_401_sentence_when_every_account_needs_relogin, proxy_does_not_hit_the_token_endpoint_again], frames: [] }
  - id: AC11
    text: "The usage fetcher makes no provider call for a needs-sign-in account."
    covers: { tests: [fetcher_needs_relogin_makes_no_provider_call], frames: [] }
  - id: AC12
    text: "resumeWatch reports a resume from the wall/monotonic gap only."
    covers: { tests: [resumewatch_detects_wall_gap_not_idle, resumewatch_default_wall_clock_has_no_monotonic_reading], frames: [] }
  - id: AC13
    text: "After a resume non-forced fetches are spread, forced ones are not, call count unchanged, no false expiry."
    covers: { tests: [poller_spreads_fetches_after_a_resume, poller_forced_refresh_is_not_delayed_after_a_resume, poller_expired_token_after_resume_is_not_an_expiry_error], frames: [] }
  - id: AC14
    text: "Scope: daemon accounts/server/quota only."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: ensurefresh_terminal_error_is_remembered_and_calls_the_refresher_once, run: "TMPDIR=/tmp/c5-tmp go test ./internal/accounts/ -run ensurefresh_terminal_error_is_remembered_and_calls_the_refresher_once -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: ensurefresh_transient_error_is_not_remembered, run: "TMPDIR=/tmp/c5-tmp go test ./internal/accounts/ -run ensurefresh_transient_error_is_not_remembered -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: ensurefresh_new_credential_clears_the_mark, run: "TMPDIR=/tmp/c5-tmp go test ./internal/accounts/ -run ensurefresh_new_credential_clears_the_mark -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: is_terminal_refresh_error_table, run: "TMPDIR=/tmp/c5-tmp go test ./internal/accounts/ -run is_terminal_refresh_error_table -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: proxy_routes_around_a_needs_relogin_account, run: "TMPDIR=/tmp/c5-tmp go test ./internal/server/ -run proxy_routes_around_a_needs_relogin_account -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: proxy_answers_401_sentence_when_every_account_needs_relogin, run: "TMPDIR=/tmp/c5-tmp go test ./internal/server/ -run proxy_answers_401_sentence_when_every_account_needs_relogin -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: proxy_does_not_hit_the_token_endpoint_again, run: "TMPDIR=/tmp/c5-tmp go test ./internal/server/ -run proxy_does_not_hit_the_token_endpoint_again -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: fetcher_needs_relogin_makes_no_provider_call, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run fetcher_needs_relogin_makes_no_provider_call -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: resumewatch_detects_wall_gap_not_idle, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run resumewatch_detects_wall_gap_not_idle -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: resumewatch_default_wall_clock_has_no_monotonic_reading, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run resumewatch_default_wall_clock_has_no_monotonic_reading -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: poller_spreads_fetches_after_a_resume, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run poller_spreads_fetches_after_a_resume -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: poller_forced_refresh_is_not_delayed_after_a_resume, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run poller_forced_refresh_is_not_delayed_after_a_resume -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: poller_expired_token_after_resume_is_not_an_expiry_error, run: "TMPDIR=/tmp/c5-tmp go test ./internal/quota/ -run poller_expired_token_after_resume_is_not_an_expiry_error -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 1200 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- web mobile desktop internal/runs)\"", timeoutSec: 1200 }
mutations:
  - { id: M1, file: internal/accounts/refresh.go, change: "EnsureFresh calls the refresher after a terminal error (no memory)", expectFail: [ensurefresh_terminal_error_is_remembered_and_calls_the_refresher_once] }
  - { id: M2, file: internal/accounts/refresh.go, change: "a transient refresh error is also remembered as dead", expectFail: [ensurefresh_transient_error_is_not_remembered] }
  - { id: M3, file: internal/server/proxy.go, change: "the proxy no longer filters needs-sign-in accounts", expectFail: [proxy_routes_around_a_needs_relogin_account] }
  - { id: M4, file: internal/quota/poller.go, change: "remove the post-resume spread (no sleep before the fetch)", expectFail: [poller_spreads_fetches_after_a_resume] }
  - { id: M5, file: internal/quota/poller.go, change: "delay a forced refresh after a resume too", expectFail: [poller_forced_refresh_is_not_delayed_after_a_resume] }
  - { id: M7, file: internal/quota/resume.go, change: "the default wall clock keeps its monotonic reading (time.Now without Round(0))", expectFail: [resumewatch_default_wall_clock_has_no_monotonic_reading] }
  - { id: M6, file: internal/quota/resume.go, change: "the gap test compares only wall time (an idle counts as a resume)", expectFail: [resumewatch_detects_wall_gap_not_idle] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./internal/accounts/ ./internal/server/ ./internal/quota/", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=/tmp/c5-tmp go test ./internal/accounts/ ./internal/server/ ./internal/quota/ ./internal/routing/ ./internal/wsapi/ -count=1", timeoutSec: 2400 }
limits: { budgetUsd: 5, maxMinutes: 90 }
---

# C5b validation contract

Spec: `docs/plans/active/quota-auth-hardening.md`. Fakes only: no live provider call; every provider-call assertion counts calls in the test itself.
