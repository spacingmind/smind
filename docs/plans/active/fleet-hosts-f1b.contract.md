---
v: 1
milestone: "Fleet F1b: hello, transports, pairing, CLI (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-1
    text: "Fleet off: no fleet.* method registered (golden over the whole wsapi method map), every smind host subcommand says fleet is off without dialing."
    covers: { tests: [TestWsapiMethodMapGoldenFleetOff, TestHostCommandsSayFleetOffWhenDisabled] }
  - id: AC-3
    text: "Invite payload is in the URL fragment only and carries no daemon password or provider secret; pairing window has one slot taken only by a host first seen at/after open."
    covers: { tests: [TestInvitePayloadFragmentOnlyNoSecrets, TestPairingWindowOneSlotFirstSeenAfterOpen] }
  - id: AC-4
    text: "Mutual hello with pinned key; SAS equal on both sides; responder records pending, only human approve makes inbound approved."
    covers: { tests: [TestHelloMutualPinnedIdentity, TestPairingSASMatchesBothSides, TestFleetPairDirectE2E] }
  - id: AC-5
    text: "One pairing, two directed links; reverse off by default and refused not_granted on the callee; enable-reverse then read-only; disable-reverse closes the connection."
    covers: { tests: [TestFleetReverseDisabledByDefault, TestFleetPairDirectE2E] }
  - id: AC-7
    text: "Pinned identity change refused peer_identity_changed; unapproved/revoked caller refused; replay on a new connection and stale ts refused; version skew yields guidance."
    covers: { tests: [TestHelloMutualPinnedIdentity, TestHelloUnapprovedRefused, TestHelloReplayOnNewConnectionRefused, TestHelloStaleRefused, TestHelloVersionSkewRefusedWithGuidance] }
  - id: AC-8
    text: "Cleartext ws:// to a non-loopback non-overlay address refused without allowCleartext; relay link fails closed on fingerprint mismatch; both end in the same hello."
    covers: { tests: [TestDirectCleartextRefusedOutsideOverlay, TestRelayFingerprintMismatchFailsClosed, TestFleetPairRelayE2E] }
  - id: AC-9
    text: "A host principal can call only fleet allowlisted methods; fleet.host.* are local-only, not run-scoped, not plugin; no fleet param struct has grant/link/policy/secret fields."
    covers: { tests: [TestHostPrincipalOnlyFleetMethods, TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin, TestFleetParamsHaveNoGrantOrSecretFields] }
  - id: AC-10
    text: "host ls --json never prints key, secret, relay secret or password; fleet-secrets is 0600."
    covers: { tests: [TestHostLsNeverPrintsSecrets, TestFleetSecretsFile0600AndRedacted] }
tests:
  - { name: TestHelloMutualPinnedIdentity, run: "go test ./internal/fleet/hello -run '^TestHelloMutualPinnedIdentity$' -count=1", timeoutSec: 300 }
  - { name: TestHelloReplayOnNewConnectionRefused, run: "go test ./internal/fleet/hello -run '^TestHelloReplayOnNewConnectionRefused$' -count=1", timeoutSec: 300 }
  - { name: TestHelloStaleRefused, run: "go test ./internal/fleet/hello -run '^TestHelloStaleRefused$' -count=1", timeoutSec: 300 }
  - { name: TestHelloVersionSkewRefusedWithGuidance, run: "go test ./internal/fleet/hello -run '^TestHelloVersionSkewRefusedWithGuidance$' -count=1", timeoutSec: 300 }
  - { name: TestInvitePayloadFragmentOnlyNoSecrets, run: "go test ./internal/fleet/pairing -run '^TestInvitePayloadFragmentOnlyNoSecrets$' -count=1", timeoutSec: 300 }
  - { name: TestPairingWindowOneSlotFirstSeenAfterOpen, run: "go test ./internal/fleet/pairing -run '^TestPairingWindowOneSlotFirstSeenAfterOpen$' -count=1", timeoutSec: 300 }
  - { name: TestPairingSASMatchesBothSides, run: "go test ./internal/fleet/pairing -run '^TestPairingSASMatchesBothSides$' -count=1", timeoutSec: 300 }
  - { name: TestDirectCleartextRefusedOutsideOverlay, run: "go test ./internal/fleet/link -run '^TestDirectCleartextRefusedOutsideOverlay$' -count=1", timeoutSec: 300 }
  - { name: TestRelayFingerprintMismatchFailsClosed, run: "go test ./internal/fleet/link -run '^TestRelayFingerprintMismatchFailsClosed$' -count=1", timeoutSec: 300 }
  - { name: TestFleetSecretsFile0600AndRedacted, run: "go test ./internal/fleet/link -run '^TestFleetSecretsFile0600AndRedacted$' -count=1", timeoutSec: 300 }
  - { name: TestHelloUnapprovedRefused, run: "go test ./internal/wsapi -run '^TestHelloUnapprovedRefused$' -count=1", timeoutSec: 300 }
  - { name: TestHostPrincipalOnlyFleetMethods, run: "go test ./internal/wsapi -run '^TestHostPrincipalOnlyFleetMethods$' -count=1", timeoutSec: 300 }
  - { name: TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin, run: "go test ./internal/wsapi -run '^TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin$' -count=1", timeoutSec: 300 }
  - { name: TestFleetParamsHaveNoGrantOrSecretFields, run: "go test ./internal/wsapi -run '^TestFleetParamsHaveNoGrantOrSecretFields$' -count=1", timeoutSec: 300 }
  - { name: TestWsapiMethodMapGoldenFleetOff, run: "go test ./internal/wsapi -run '^TestWsapiMethodMapGoldenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestHostLsNeverPrintsSecrets, run: "go test ./cmd/smind -run '^TestHostLsNeverPrintsSecrets$' -count=1", timeoutSec: 300 }
  - { name: TestHostCommandsSayFleetOffWhenDisabled, run: "go test ./cmd/smind -run '^TestHostCommandsSayFleetOffWhenDisabled$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReverseDisabledByDefault, run: "go test ./internal/fleet/link -run '^TestFleetReverseDisabledByDefault$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPairDirectE2E, run: "go test ./internal/fleet/link -run '^TestFleetPairDirectE2E$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPairRelayE2E, run: "go test ./internal/fleet/link -run '^TestFleetPairRelayE2E$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/hello/hello.go, change: "VerifyHello skips the nonceC term in the signed bytes", expectFail: [TestHelloReplayOnNewConnectionRefused] }
  - { id: M2, file: internal/fleet/hello/hello.go, change: "caller does not compare the callee pub to the pinned pub", expectFail: [TestHelloMutualPinnedIdentity] }
  - { id: M3, file: internal/fleet/link/route.go, change: "allow ws:// to any address", expectFail: [TestDirectCleartextRefusedOutsideOverlay] }
  - { id: M4, file: internal/wsapi/fleet_host.go, change: "gate lets a host principal call any method", expectFail: [TestHostPrincipalOnlyFleetMethods] }
  - { id: M5, file: internal/wsapi/fleet_host.go, change: "drop the rc.local check from the admin wrapper", expectFail: [TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin] }
  - { id: M6, file: internal/wsapi/fleet_host.go, change: "hello accepts a caller whose inbound link is disabled", expectFail: [TestFleetReverseDisabledByDefault] }
  - { id: M7, file: internal/fleet/pairing/window.go, change: "window slot is not consumed", expectFail: [TestPairingWindowOneSlotFirstSeenAfterOpen] }
  - { id: M8, file: internal/fleet/hello/hello.go, change: "skip the fleetV equality check", expectFail: [TestHelloVersionSkewRefusedWithGuidance] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
  - { id: C3, run: "GOOS=windows go vet ./internal/fleet/... ./internal/wsapi/ ./internal/wsclient/ ./cmd/smind/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# F1b validation contract

Judge the branch head against these ACs, `docs/decisions/0048-fleet-connected-hosts.md` (sections 2, 3, 8, 9) and the F1b design in `docs/plans/active/fleet-hosts.md`. Attack targets: (1) any fleet.* method reachable with fleet.enabled false; (2) a host principal reaching a non-fleet method or fleet.host.*; (3) any path where a link message writes a grant, link state it should not, a policy or a secret; (4) a password, relay secret or key in an invite, host ls output, log or API result; (5) a hello that accepts a replayed or stale transcript, or accepts a callee whose pub is not the pinned one; (6) cleartext routing outside the overlay without the flag; (7) reverse direction enabled by pairing alone; (8) Windows build break. Not done by design: transcript binding to the E2EE hash, audit writer, fleet.host.get/audit, UI.
