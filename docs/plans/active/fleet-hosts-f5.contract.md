---
v: 1
milestone: "Fleet F5: capability routing by host labels (AC26, ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-1
    text: "Host labels (<=16, lowercase key/value charset and length caps, no secret-looking keys) are stored in one new fleet_hosts.labels column (migration fleet_host_labels), set by the operator via fleet.host.labels / smind host label, and are caller-local: no wire type carries them."
    covers: { tests: [TestFleetLabelsValidate, TestFleetLabelsSetAndList, TestFleetHostLabelsRoundTrip, TestFleetHostLabels_ValidationRefused, TestFleetHostLabels_NeverOnWire, TestFleetHostLabels_LocalOnlyNotHostPrincipal] }
  - id: AC-2
    text: "The selector resolves to exactly one enabled, connected host; zero matches and ambiguity are errors naming the candidate aliases; any:true takes the least live agents, ties by alias; a disabled or disconnected host never matches; an empty selector is refused."
    covers: { tests: [TestFleetSelector_ExactOne, TestFleetSelector_Zero, TestFleetSelector_Ambiguous, TestFleetSelector_AnyTieByAlias, TestFleetSelector_DisabledSkipped, TestFleetSelector_DisconnectedSkipped, TestFleetSelector_EmptyLabelsInvalid] }
  - id: AC-3
    text: "The daemon bridge remote.select is local-only, refused for a peer-created run, returns only alias and hostId (no labels), counts live remote agents from the mirror, and never retries on another host."
    covers: { tests: [TestFleetRemoteSelect_ExactOne, TestFleetRemoteSelect_AmbiguousNamesCandidates, TestFleetRemoteSelect_AnyPicksLeastLive, TestFleetRemoteSelect_DisconnectedAndDisabledSkipped, TestFleetRemoteSelect_RunScopedPeerRefused, TestFleetRemoteSelect_ResultHasNoLabels] }
  - id: AC-4
    text: "MCP host accepts an alias string or {labels, any}; a selector costs one remote.select and the typed error text names the candidates; a failed write is not retried; a string alias makes no select call."
    covers: { tests: [TestFleetSelectorMCP_ResolvesToAliasThenReads, TestFleetSelectorMCP_AliasStringUnchanged, TestFleetSelectorMCP_ErrorNamesCandidates, TestFleetSelectorMCP_BadShape, TestFleetSelectorMCP_NoRetryOnWriteFailure] }
  - id: AC-5
    text: "CLI: smind host label set|rm|ls, smind host select, and --labels/--any on info/workspaces/tasks, one remote.select, refusals printed verbatim."
    covers: { tests: [TestHostLabelSetSendsPairs, TestHostSelectPrintsAlias, TestHostSelectAmbiguousPrintsCandidates, TestHostReadWithLabelsSelectsFirst] }
tests:
  - { name: selector, run: "go test ./internal/fleet/selector -race -count=1", timeoutSec: 300 }
  - { name: registry-store, run: "go test ./internal/fleet/registry ./internal/store -count=1", timeoutSec: 600 }
  - { name: wsapi-fleet, run: "go test ./internal/wsapi -count=1 -run 'FleetRemoteSelect|FleetHostLabels|FleetAdmin|MethodMapGolden|HostViewShape'", timeoutSec: 900 }
  - { name: mcp-cli, run: "go test ./cmd/smind -count=1 -run 'FleetSelectorMCP|HostLabel|HostSelect|HostRead|Drift'", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/fleet/selector/selector.go, change: "eligible if Enabled || Connected instead of &&", expectFail: [TestFleetSelector_DisabledSkipped] }
  - { id: M2, file: internal/fleet/selector/selector.go, change: "tie-break picks the larger alias", expectFail: [TestFleetSelector_AnyTieByAlias] }
  - { id: M3, file: internal/fleet/registry/labels.go, change: "label cap raised to 17", expectFail: [TestFleetLabelsValidate] }
  - { id: M4, file: internal/wsapi/fleet_remote_select.go, change: "Connected always true", expectFail: [TestFleetRemoteSelect_DisconnectedAndDisabledSkipped] }
  - { id: M5, file: cmd/smind/mcp_hosts.go, change: "resolved alias not written into remote.read params", expectFail: [TestFleetSelectorMCP_ResolvesToAliasThenReads] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "GOOS=windows go vet ./cmd/smind ./internal/fleet/... ./internal/wsapi ./internal/store", timeoutSec: 600 }
limits: { budgetUsd: 2, maxMinutes: 40 }
writeScope:
  - internal/fleet/selector
  - internal/fleet/registry
  - internal/store
  - internal/wsapi
  - cmd/smind
  - public-docs/hosts.md
---

Judge the branch head against the ACs. Labels are local operator config and must never be sent to the peer; selection never retries on another host.
