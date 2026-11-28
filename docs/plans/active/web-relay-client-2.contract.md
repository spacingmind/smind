---
v: 1
milestone: "Web relay PR 2: the browser relay client (WebCrypto identity, IndexedDB, RelaySocket, controller)"
base: origin/develop
planRef: docs/plans/active/web-relay-client.md
acs:
  - id: AC4
    text: "The identity key is non-extractable, persists across reload, an unsupported browser fails typed."
    covers: { tests: [guard_scanner_catches_known_bypasses, damaged_records_mean_repair_not_connect, generate_key_extractable_argument_is_the_literal_false, identity_key_is_non_extractable, identity_key_survives_reload, unsupported_browser_fails_typed, relay_web_has_no_extractable_key_or_logging], frames: [] }
  - id: AC5
    text: "No secret reaches console or storage; the offer fragment is removed."
    covers: { tests: [no_secret_reaches_console_or_storage, fragment_is_removed_after_pairing, relay_web_has_no_extractable_key_or_logging], frames: [] }
  - id: AC6
    text: "A missing identity is re-pair, never a new identity."
    covers: { tests: [identity_key_missing_means_repair_not_new_identity, pair_failure_keeps_the_previous_identity_and_pairing, savepaired_is_atomic, concurrent_pair_calls_leave_one_live_connection, resume_during_pair_waits_for_the_pair, a_throwing_state_listener_does_not_break_the_others, an_unexpected_connection_close_is_reported_as_failed, invalid_offer_error_never_echoes_the_payload, a_late_open_after_blocked_is_closed], frames: [] }
  - id: AC7
    text: "Over a real Go relay and hub: pair, call, event, reconnect and resume, revoke; mobile integration still passes."
    covers: { tests: [integration_pair_call_and_event, reconnect_resumes_after_relay_restart, revoked_device_gets_a_clear_state, mobile_integration_devices_still_passes], frames: [] }
  - id: AC8
    text: "RelaySocket is a SocketLike so the existing WsClient runs unchanged."
    covers: { tests: [relay_socket_is_a_socketlike_for_wsclient, relay_socket_relays_the_real_events_subscribe_answer, relay_socket_events_subscribe_is_idempotent_per_topic, relay_socket_events_unsubscribe_releases_only_the_named_topics, relay_socket_forwards_event_dropped, relay_socket_delivers_an_event_pushed_before_the_subscribe_answer], frames: [] }
  - id: AC14
    text: "Scope: no Go, desktop or web/packages/ui change."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: identity_key_is_non_extractable, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/identity.test.ts -t identity_key_is_non_extractable", timeoutSec: 900 }
  - { name: unsupported_browser_fails_typed, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/identity.test.ts -t unsupported_browser_fails_typed", timeoutSec: 900 }
  - { name: identity_key_survives_reload, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/store.test.ts -t identity_key_survives_reload", timeoutSec: 900 }
  - { name: identity_key_missing_means_repair_not_new_identity, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/store.test.ts -t identity_key_missing_means_repair_not_new_identity", timeoutSec: 900 }
  - { name: no_secret_reaches_console_or_storage, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/controller.test.ts -t no_secret_reaches_console_or_storage", timeoutSec: 900 }
  - { name: fragment_is_removed_after_pairing, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/fragment.test.ts -t fragment_is_removed_after_pairing", timeoutSec: 900 }
  - { name: relay_socket_is_a_socketlike_for_wsclient, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/relay-socket.test.ts -t relay_socket_is_a_socketlike_for_wsclient", timeoutSec: 900 }
  - { name: relay_web_has_no_extractable_key_or_logging, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/guards.test.ts -t relay_web_has_no_extractable_key_or_logging", timeoutSec: 900 }
  - { name: generate_key_extractable_argument_is_the_literal_false, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run src/__tests__/guards.test.ts -t generate_key_extractable_argument_is_the_literal_false", timeoutSec: 900 }
  - { name: relay_socket_relays_the_real_events_subscribe_answer, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t relay_socket_relays_the_real_events_subscribe_answer", timeoutSec: 900 }
  - { name: relay_socket_events_subscribe_is_idempotent_per_topic, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t relay_socket_events_subscribe_is_idempotent_per_topic", timeoutSec: 900 }
  - { name: relay_socket_events_unsubscribe_releases_only_the_named_topics, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t relay_socket_events_unsubscribe_releases_only_the_named_topics", timeoutSec: 900 }
  - { name: relay_socket_forwards_event_dropped, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t relay_socket_forwards_event_dropped", timeoutSec: 900 }
  - { name: relay_socket_delivers_an_event_pushed_before_the_subscribe_answer, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t relay_socket_delivers_an_event_pushed_before_the_subscribe_answer", timeoutSec: 900 }
  - { name: pair_failure_keeps_the_previous_identity_and_pairing, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t pair_failure_keeps_the_previous_identity_and_pairing", timeoutSec: 900 }
  - { name: savepaired_is_atomic, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t savepaired_is_atomic", timeoutSec: 900 }
  - { name: concurrent_pair_calls_leave_one_live_connection, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t concurrent_pair_calls_leave_one_live_connection", timeoutSec: 900 }
  - { name: resume_during_pair_waits_for_the_pair, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t resume_during_pair_waits_for_the_pair", timeoutSec: 900 }
  - { name: a_throwing_state_listener_does_not_break_the_others, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t a_throwing_state_listener_does_not_break_the_others", timeoutSec: 900 }
  - { name: an_unexpected_connection_close_is_reported_as_failed, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t an_unexpected_connection_close_is_reported_as_failed", timeoutSec: 900 }
  - { name: damaged_records_mean_repair_not_connect, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t damaged_records_mean_repair_not_connect", timeoutSec: 900 }
  - { name: invalid_offer_error_never_echoes_the_payload, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t invalid_offer_error_never_echoes_the_payload", timeoutSec: 900 }
  - { name: a_late_open_after_blocked_is_closed, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t a_late_open_after_blocked_is_closed", timeoutSec: 900 }
  - { name: fragment_helper_scrubs_an_offer_in_the_query_but_does_not_accept_it, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t fragment_helper_scrubs_an_offer_in_the_query_but_does_not_accept_it", timeoutSec: 900 }
  - { name: guard_scanner_catches_known_bypasses, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && bunx vitest run --exclude \"**/*.node.test.ts\" -t guard_scanner_catches_known_bypasses", timeoutSec: 900 }
  - { name: integration_pair_call_and_event, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && NODE_TLS_REJECT_UNAUTHORIZED=0 bunx vitest run src/__tests__/integration.pair.node.test.ts", timeoutSec: 1800 }
  - { name: reconnect_resumes_after_relay_restart, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && NODE_TLS_REJECT_UNAUTHORIZED=0 bunx vitest run src/__tests__/integration.reconnect.node.test.ts", timeoutSec: 1800 }
  - { name: revoked_device_gets_a_clear_state, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && NODE_TLS_REJECT_UNAUTHORIZED=0 bunx vitest run src/__tests__/integration.revoke.node.test.ts", timeoutSec: 1800 }
  - { name: mobile_integration_devices_still_passes, run: "cd mobile && bun install --frozen-lockfile && NODE_TLS_REJECT_UNAUTHORIZED=0 SMIND_REQUIRE_PASSWORD_HARNESS=1 bunx vitest run src/relay/__tests__/integration.devices.node.test.ts", timeoutSec: 1800 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal cmd desktop web/packages/ui)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/relay-web/src/identity.ts, change: "generate the identity key extractable: true", expectFail: [identity_key_is_non_extractable, generate_key_extractable_argument_is_the_literal_false] }
  - { id: M2, file: web/packages/relay-web/src/controller.ts, change: "console.log the admission secret on connect", expectFail: [no_secret_reaches_console_or_storage, relay_web_has_no_extractable_key_or_logging] }
  - { id: M3, file: web/packages/relay-web/src/identity-manager.ts, change: "create a fresh identity when the stored one is missing", expectFail: [identity_key_missing_means_repair_not_new_identity] }
  - { id: M4, file: web/packages/relay-web/src/fragment.ts, change: "leave the offer fragment in the address bar", expectFail: [fragment_is_removed_after_pairing] }
  - { id: M5, file: web/packages/relay-web/src/controller.ts, change: "pair() saves the identity in its own transaction before the atomic savePaired", expectFail: [pair_failure_keeps_the_previous_identity_and_pairing] }
  - { id: M6, file: web/packages/relay-web/src/relay-socket.ts, change: "events.subscribe is answered locally with null instead of the daemon answer", expectFail: [relay_socket_relays_the_real_events_subscribe_answer, relay_socket_delivers_an_event_pushed_before_the_subscribe_answer] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/relay-web && bunx tsc --noEmit && bunx vitest run", timeoutSec: 1800 }
  - { id: C2, run: "cd web/packages/relay-client && bunx tsc --noEmit && bunx vitest run", timeoutSec: 1800 }
  - { id: C3, run: "cd mobile && bunx tsc --noEmit && bunx vitest run --exclude \"**/*.node.test.ts\"", timeoutSec: 2400 }
limits: { budgetUsd: 5, maxMinutes: 120 }
---

# Web relay PR 2 validation contract

Spec: `docs/plans/active/web-relay-client.md`. A new browser-adapter package; the integration tests run the real TypeScript client against the real Go relay and hub (the devharness, `go` on PATH, a self-signed relay: `NODE_TLS_REJECT_UNAUTHORIZED=0` for the test process only). No live provider, no public network. Real-browser coverage arrives with PR 3.
