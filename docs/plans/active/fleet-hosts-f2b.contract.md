---
v: 1
milestone: "Fleet F2b: caller client, CLI and MCP read (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-13
    text: "Every outgoing read carries fleet.prov with origin = this host, exactly one hop, a fresh reqId and ts; a run that came from a peer (remote origin) is refused locally with hop_limit before any byte is sent."
    covers: { tests: [TestClientAttachesProvenance, TestRemoteReadRefusesRemoteOriginRunHopLimit] }
  - id: AC-15
    text: "smind host workspaces|tasks|task|status|activity and MCP list_hosts plus an optional host argument on workspace_list, task_list, task_get, task_status and task_logs; omitted host is byte-identical to today."
    covers: { tests: [TestHostReadVerbsSendRemoteReadParams, TestMCPListHostsAndHostParamLocalUnchanged] }
  - id: AC-16
    text: "A refused link (version skew) surfaces as fleet_version_unsupported with guidance; the client never falls back to local."
    covers: { tests: [TestClientRefusalCodesTyped, TestRemoteReadTypedErrorsPassThrough] }
  - id: AC-21-OBS
    text: "Observation: three consecutive failed observations give status unknown, never finished; connecting attempts do not count; a removed host is unavailable at once."
    covers: { tests: [TestObservationThreeFailuresIsStatusUnknownNeverFinished, TestObservationConnectingNotCounted, TestObservationRemovedHostUnavailableAtOnce, TestHostReadStatusUnknownNeverFinished] }
  - id: AC-1
    text: "Fleet off: remote.hosts and remote.read are not registered (golden handler map), the new CLI verbs say fleet is off without dialing."
    covers: { tests: [TestWsapiMethodMapGoldenFleetOff, TestHostReadFleetOffSaysSo] }
tests:
  - { name: TestClientAttachesProvenance, run: "go test ./internal/fleet/client -run '^TestClientAttachesProvenance$' -count=1", timeoutSec: 300 }
  - { name: TestObservationThreeFailuresIsStatusUnknownNeverFinished, run: "go test ./internal/fleet/client -run '^TestObservationThreeFailuresIsStatusUnknownNeverFinished$' -count=1", timeoutSec: 300 }
  - { name: TestObservationConnectingNotCounted, run: "go test ./internal/fleet/client -run '^TestObservationConnectingNotCounted$' -count=1", timeoutSec: 300 }
  - { name: TestRemoteReadRefusesRemoteOriginRunHopLimit, run: "go test ./internal/wsapi -run '^TestRemoteReadRefusesRemoteOriginRunHopLimit$' -count=1", timeoutSec: 300 }
  - { name: TestMCPListHostsAndHostParamLocalUnchanged, run: "go test ./cmd/smind -run '^TestMCPListHostsAndHostParamLocalUnchanged$' -count=1", timeoutSec: 300 }
  - { name: TestHostReadStatusUnknownNeverFinished, run: "go test ./cmd/smind -run '^TestHostReadStatusUnknownNeverFinished$' -count=1", timeoutSec: 300 }
  - { name: TestWsapiMethodMapGoldenFleetOff, run: "go test ./internal/wsapi -run '^TestWsapiMethodMapGoldenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestHostReadVerbsSendRemoteReadParams, run: "go test ./cmd/smind -run '^TestHostReadVerbsSendRemoteReadParams$' -count=1", timeoutSec: 300 }
  - { name: TestHostReadFleetOffSaysSo, run: "go test ./cmd/smind -run '^TestHostReadFleetOffSaysSo$' -count=1", timeoutSec: 300 }
  - { name: TestClientRefusalCodesTyped, run: "go test ./internal/fleet/client -run '^TestClientRefusalCodesTyped$' -count=1", timeoutSec: 300 }
  - { name: TestRemoteReadTypedErrorsPassThrough, run: "go test ./internal/wsapi -run '^TestRemoteReadTypedErrorsPassThrough$' -count=1", timeoutSec: 300 }
  - { name: TestObservationRemovedHostUnavailableAtOnce, run: "go test ./internal/fleet/client -run '^TestObservationRemovedHostUnavailableAtOnce$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/client/client.go, change: "Hops carries [] or two entries instead of exactly [Self]", expectFail: [TestClientAttachesProvenance] }
  - { id: M2, file: internal/fleet/client/observe.go, change: "failure limit off by one (count to 4) or connecting counted", expectFail: [TestObservationThreeFailuresIsStatusUnknownNeverFinished, TestObservationConnectingNotCounted] }
  - { id: M3, file: internal/wsapi/fleet_remote.go, change: "drop the remote-origin hop_limit refusal", expectFail: [TestRemoteReadRefusesRemoteOriginRunHopLimit] }
  - { id: M4, file: cmd/smind/mcp_discovery_tools.go, change: "workspace_list takes the remote branch even when host is omitted", expectFail: [TestMCPListHostsAndHostParamLocalUnchanged] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
  - { id: C3, run: "GOOS=windows go vet ./internal/fleet/... ./internal/wsapi/ ./cmd/smind/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# F2b validation contract

Judge the branch head against these ACs, `docs/decisions/0048-fleet-connected-hosts.md` (sections 4, 5, 8, 9) and the F2b design in `docs/plans/active/fleet-hosts.md`. Attack targets: (1) any way a remote-origin run or a peer seat reaches `remote.*`; (2) a read that returns a local path, a credential URL or an unbounded text; (3) a failed observation reported as finished or done; (4) an omitted `host` argument changing a local tool result; (5) `remote.*` reachable with fleet off; (6) a typed refusal swallowed or retried on another host or locally. Not done by design: write tools (F3), watches (F3b), peer seat tool hiding (F3b AC22).
