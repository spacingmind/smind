---
v: 1
milestone: "Web relay PR 1: the shared relay-client package (web and mobile)"
base: origin/develop
planRef: docs/plans/active/web-relay-client.md
acs:
  - id: AC1
    text: "The package holds every pure relay module and its tests; mobile keeps one-line shims only; mobile tests pass and the Expo bundle resolves."
    covers: { tests: [mobile_imports_the_shared_package_only, mobile_bundle_resolves_the_shared_package], frames: [] }
  - id: AC2
    text: "The package vector tests match the Go output."
    covers: { tests: [vectors_match_go_v2], frames: [] }
  - id: AC3
    text: "The package imports no React Native, Expo or DOM-only API."
    covers: { tests: [relay_client_package_has_no_platform_imports], frames: [] }
  - id: AC4
    text: "A device identity may sign asynchronously."
    covers: { tests: [buildDeviceHello_awaits_an_async_signer], frames: [] }
  - id: AC5
    text: "No Go, desktop or behaviour change."
    covers: { tests: [hygiene_and_scope], frames: [] }
tests:
  - { name: relay_client_package_has_no_platform_imports, run: "cd web && bun install --frozen-lockfile && cd packages/relay-client && bunx vitest run src/__tests__/package-guards.test.ts -t relay_client_package_has_no_platform_imports", timeoutSec: 900 }
  - { name: vectors_match_go_v2, run: "cd web && bun install --frozen-lockfile && cd packages/relay-client && bunx vitest run src/__tests__/package-guards.test.ts src/__tests__/e2ee.test.ts", timeoutSec: 900 }
  - { name: buildDeviceHello_awaits_an_async_signer, run: "cd web && bun install --frozen-lockfile && cd packages/relay-client && bunx vitest run src/__tests__/deviceIdentity.test.ts -t buildDeviceHello_awaits_an_async_signer", timeoutSec: 900 }
  - { name: mobile_imports_the_shared_package_only, run: "cd mobile && bun install --frozen-lockfile && bunx vitest run src/__tests__/relayShims.test.ts", timeoutSec: 900 }
  - { name: mobile_bundle_resolves_the_shared_package, run: "cd mobile && bun install --frozen-lockfile && npx expo export --platform ios --output-dir /tmp/wr1-export 2>&1 | tail -3 | grep -q Exported", timeoutSec: 1800 }
  - { name: hygiene_and_scope, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop...HEAD --name-only -- internal cmd desktop)\"", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/relay-client/src/e2ee.ts, change: "change one byte of an HKDF info label", expectFail: [vectors_match_go_v2] }
  - { id: M2, file: web/packages/relay-client/src/admission.ts, change: "add `import \"react-native\"`", expectFail: [relay_client_package_has_no_platform_imports] }
  - { id: M3, file: mobile/src/relay/e2ee.ts, change: "replace the shim by a copy of the module", expectFail: [mobile_imports_the_shared_package_only] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/relay-client && bunx tsc --noEmit && bunx vitest run", timeoutSec: 1800 }
  - { id: C2, run: "cd mobile && bunx tsc --noEmit; bunx vitest run --exclude \"**/*.node.test.ts\"", timeoutSec: 2400 }
limits: { budgetUsd: 5, maxMinutes: 120 }
---

# Web relay PR 1 validation contract

Spec: `docs/plans/active/web-relay-client.md`. A mechanical move of the pure relay client into a shared package: no behaviour change, no Go change, no network, no live provider.
