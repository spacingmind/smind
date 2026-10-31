---
v: 1
milestone: "Fleet F2a: callee read handlers, provenance, audit (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-12
    text: "fleet.info, fleet.workspace.list, fleet.task.list|get, fleet.run.status, fleet.task.activity work for a granted peer and return only owner-host data: no local paths, no credential-bearing URLs, assistant text <= 1 KiB per event, cursor-paged, filtered by workspaces.read."
    covers: { tests: [TestFleetReadReturnsNoPathsNoCredentialURLs, TestFleetActivityCappedAndPaged, TestFleetReadGrantFiltersWorkspaces] }
  - id: AC-13
    text: "Provenance: the callee verifies in order principal match, not-in-chain (cycle), len(hops)==1 (hop limit), stale/replay, grant, rate; each failure is a typed refusal with an audit row, before any data is read."
    covers: { tests: [TestFleetProvenanceOrderOfChecks, TestFleetPrincipalMismatchRefused, TestFleetCycleRefused_AtoBtoA, TestFleetHopTwoRefused, TestFleetReplayAndStaleRefused] }
  - id: AC-14
    text: "Callee audit: one fleet_audit row per call (served or refused) with direction in, host, trace, reqId, method, outcome, code, latency, bytes; summary is a hash plus <= 120 redacted characters, never the body; rows older than the retention are swept."
    covers: { tests: [TestFleetAuditBothEndsNoPromptBody, TestFleetAuditRetention] }
  - id: AC-5
    text: "Reverse direction stays disabled by default: with a real pairing A reads B and B cannot read A."
    covers: { tests: [TestFleetReadTwoDaemonsE2E] }
  - id: AC-16
    text: "Version/caps skew is refused on the callee at hello with fleet_version_unsupported and admits no call afterwards."
    covers: { tests: [TestFleetVersionSkewRefused] }
  - id: AC-9
    text: "A host principal reaches only the allowlisted fleet methods; the read methods are not local-only admin, not run-scoped, not plugin methods, and with fleet off nothing is registered."
    covers: { tests: [TestHostPrincipalOnlyFleetMethods, TestWsapiMethodMapGoldenFleetOff, TestFleetReadMethodsNotRunScopedNotPlugin] }
tests:
  - { name: TestFleetProvenanceOrderOfChecks, run: "go test ./internal/fleet/serve -run '^TestFleetProvenanceOrderOfChecks$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPrincipalMismatchRefused, run: "go test ./internal/fleet/serve -run '^TestFleetPrincipalMismatchRefused$' -count=1", timeoutSec: 300 }
  - { name: TestFleetCycleRefused_AtoBtoA, run: "go test ./internal/fleet/serve -run '^TestFleetCycleRefused_AtoBtoA$' -count=1", timeoutSec: 300 }
  - { name: TestFleetHopTwoRefused, run: "go test ./internal/fleet/serve -run '^TestFleetHopTwoRefused$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReplayAndStaleRefused, run: "go test ./internal/fleet/serve -run '^TestFleetReplayAndStaleRefused$' -count=1", timeoutSec: 300 }
  - { name: TestFleetAuditBothEndsNoPromptBody, run: "go test ./internal/fleet/serve -run '^TestFleetAuditBothEndsNoPromptBody$' -count=1", timeoutSec: 300 }
  - { name: TestFleetAuditRetention, run: "go test ./internal/fleet/serve -run '^TestFleetAuditRetention$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReadReturnsNoPathsNoCredentialURLs, run: "go test ./internal/fleet/serve -run '^TestFleetReadReturnsNoPathsNoCredentialURLs$' -count=1", timeoutSec: 300 }
  - { name: TestFleetActivityCappedAndPaged, run: "go test ./internal/fleet/serve -run '^TestFleetActivityCappedAndPaged$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReadGrantFiltersWorkspaces, run: "go test ./internal/fleet/serve -run '^TestFleetReadGrantFiltersWorkspaces$' -count=1", timeoutSec: 300 }
  - { name: TestFleetVersionSkewRefused, run: "go test ./internal/wsapi -run '^TestFleetVersionSkewRefused$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReadMethodsNotRunScopedNotPlugin, run: "go test ./internal/wsapi -run '^TestFleetReadMethodsNotRunScopedNotPlugin$' -count=1", timeoutSec: 300 }
  - { name: TestHostPrincipalOnlyFleetMethods, run: "go test ./internal/wsapi -run '^TestHostPrincipalOnlyFleetMethods$' -count=1", timeoutSec: 300 }
  - { name: TestWsapiMethodMapGoldenFleetOff, run: "go test ./internal/wsapi -run '^TestWsapiMethodMapGoldenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestFleetReadTwoDaemonsE2E, run: "go test ./internal/fleet/link -run '^TestFleetReadTwoDaemonsE2E$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/serve/prov.go, change: "hop check len(hops)==1 becomes len(hops)<=2", expectFail: [TestFleetHopTwoRefused] }
  - { id: M2, file: internal/fleet/serve/prov.go, change: "drop the callee-in-chain (cycle) check", expectFail: [TestFleetCycleRefused_AtoBtoA] }
  - { id: M3, file: internal/fleet/serve/prov.go, change: "accept a replayed reqId", expectFail: [TestFleetReplayAndStaleRefused] }
  - { id: M4, file: internal/fleet/serve/prov.go, change: "skip the principal match", expectFail: [TestFleetPrincipalMismatchRefused] }
  - { id: M5, file: internal/fleet/serve/audit.go, change: "summary keeps the whole body", expectFail: [TestFleetAuditBothEndsNoPromptBody] }
  - { id: M6, file: internal/fleet/serve/read.go, change: "workspace view returns the local path as repoUrl fallback", expectFail: [TestFleetReadReturnsNoPathsNoCredentialURLs] }
  - { id: M7, file: internal/fleet/serve/read.go, change: "activity does not cap assistant text", expectFail: [TestFleetActivityCappedAndPaged] }
---

F2a callee read path: see the plan's "F2a design".
