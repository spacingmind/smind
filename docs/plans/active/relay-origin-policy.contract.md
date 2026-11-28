---
v: 1
milestone: "Relay Origin policy for browser clients (ADR-0052 H10)"
base: origin/develop
planRef: docs/plans/active/relay-origin-policy.md
acs:
  - id: AC1
    text: "No Origin header: unchanged."
    covers: { tests: [grpcweb_without_an_origin_is_unchanged, no_origin_clients_still_pass_the_real_integration], frames: [] }
  - id: AC2
    text: "The default origin works with matching CORS headers."
    covers: { tests: [grpcweb_unary_from_the_default_origin_works_with_cors_headers, grpcweb_websocket_from_the_default_origin_works, origin_policy_allows_no_origin_and_listed_origins_only], frames: [] }
  - id: AC3
    text: "Any other origin is 403 for unary, preflight and websocket."
    covers: { tests: [grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket, origin_policy_rejects_null_and_lookalikes], frames: [] }
  - id: AC4
    text: "Operator origins accepted exactly; invalid values stop startup."
    covers: { tests: [grpcweb_operator_origin_is_accepted, operator_origins_are_validated, relay_allowed_origin_invalid_stops_startup], frames: [] }
  - id: AC5
    text: "CLI flag and env merged with the default."
    covers: { tests: [relay_allowed_origin_flag_and_env_are_merged_with_the_default], frames: [] }
  - id: AC6
    text: "Scope: relay and CLI only."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: origin_policy_allows_no_origin_and_listed_origins_only, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_origin_policy_allows_no_origin_and_listed_origins_only$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: origin_policy_rejects_null_and_lookalikes, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_origin_policy_rejects_null_and_lookalikes$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: operator_origins_are_validated, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_operator_origins_are_validated$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: grpcweb_unary_from_the_default_origin_works_with_cors_headers, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_grpcweb_unary_from_the_default_origin_works_with_cors_headers$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: grpcweb_websocket_from_the_default_origin_works, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_grpcweb_websocket_from_the_default_origin_works$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: grpcweb_without_an_origin_is_unchanged, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_grpcweb_without_an_origin_is_unchanged$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: grpcweb_operator_origin_is_accepted, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/server/ -run \"^Test_grpcweb_operator_origin_is_accepted$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: relay_allowed_origin_flag_and_env_are_merged_with_the_default, run: "TMPDIR=/tmp/c5-tmp go test ./cmd/smind/ -run \"^Test_relay_allowed_origin_flag_and_env_are_merged_with_the_default$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: relay_allowed_origin_invalid_stops_startup, run: "TMPDIR=/tmp/c5-tmp go test ./cmd/smind/ -run \"^Test_relay_allowed_origin_invalid_stops_startup$\" -timeout 300s -count=1 -v | grep -E \"^(--- PASS|ok)\" | grep -q PASS", timeoutSec: 900 }
  - { name: no_origin_clients_still_pass_the_real_integration, run: "cd web && bun install --frozen-lockfile && cd packages/relay-web && NODE_TLS_REJECT_UNAUTHORIZED=0 bunx vitest run src/__tests__/integration.pair.node.test.ts", timeoutSec: 1800 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- web mobile desktop)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/relay/server/origin.go, change: "the allow function always returns true", expectFail: [grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket, origin_policy_rejects_null_and_lookalikes] }
  - { id: M2, file: internal/relay/server/grpcweb.go, change: "the 403 middleware is removed (only the library origin funcs remain)", expectFail: [grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket] }
  - { id: M3, file: internal/relay/server/origin.go, change: "an empty Origin is refused", expectFail: [grpcweb_without_an_origin_is_unchanged, origin_policy_allows_no_origin_and_listed_origins_only] }
  - { id: M4, file: internal/relay/server/origin.go, change: "suffix match instead of exact match", expectFail: [origin_policy_rejects_null_and_lookalikes] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./internal/relay/... ./cmd/smind/", timeoutSec: 900 }
  - { id: C2, run: "TMPDIR=/tmp/c5-tmp go test ./internal/relay/... ./cmd/smind/ -count=1", timeoutSec: 2400 }
limits: { budgetUsd: 5, maxMinutes: 120 }
---

# Relay Origin policy validation contract

Spec: `docs/plans/active/relay-origin-policy.md`. Real relay and real grpc-web handler in Go tests; no live provider, no public network.
