---
v: 1
milestone: "Web relay PR 2: the browser relay client (WebCrypto identity, IndexedDB, RelaySocket, controller)"
base: origin/develop
planRef: docs/plans/active/web-relay-client.md
acs:
  - id: AC4
    text: "The identity key is non-extractable, persists across reload, an unsupported browser fails typed."
    covers: { tests: [generate_key_extractable_argument_is_the_literal_false, identity_key_is_non_extractable, identity_key_survives_reload, unsupported_browser_fails_typed, relay_web_has_no_extractable_key_or_logging], frames: [] }
  - id: AC5
    text: "No secret reaches console or storage; the offer fragment is removed."
    covers: { tests: [no_secret_reaches_console_or_storage, fragment_is_removed_after_pairing, relay_web_has_no_extractable_key_or_logging], frames: [] }
  - id: AC6
    text: "A missing identity is re-pair, never a new identity."
    covers: { tests: [identity_key_missing_means_repair_not_new_identity], frames: [] }
  - id: AC7
    text: "Over a real Go relay and hub: pair, call, event, reconnect and resume, revoke; mobile integration still passes."
    covers: { tests: [integration_pair_call_and_event, reconnect_resumes_after_relay_restart, revoked_device_gets_a_clear_state, mobile_integration_devices_still_passes], frames: [] }
  - id: AC8
    text: "RelaySocket is a SocketLike so the existing WsClient runs unchanged."
    covers: { tests: [relay_socket_is_a_socketlike_for_wsclient], frames: [] }
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
frames: []
commands:
  - { id: C1, run: "cd web/packages/relay-web && bunx tsc --noEmit && bunx vitest run", timeoutSec: 1800 }
  - { id: C2, run: "cd web/packages/relay-client && bunx tsc --noEmit && bunx vitest run", timeoutSec: 1800 }
  - { id: C3, run: "cd mobile && bunx tsc --noEmit && bunx vitest run --exclude \"**/*.node.test.ts\"", timeoutSec: 2400 }
limits: { budgetUsd: 5, maxMinutes: 120 }
---

# Web relay PR 2 validation contract

Spec: `docs/plans/active/web-relay-client.md`. A new browser-adapter package; the integration tests run the real TypeScript client against the real Go relay and hub (the devharness, `go` on PATH, a self-signed relay: `NODE_TLS_REJECT_UNAUTHORIZED=0` for the test process only). No live provider, no public network. Real-browser coverage arrives with PR 3.
